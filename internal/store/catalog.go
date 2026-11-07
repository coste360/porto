package store

import (
	"context"
	"strings"

	"github.com/uptrace/bun"
)

// Queries of the M2 pricing catalogue (m2-plan §5.1): pricing_settings, revenue_classes, items, berth_categories
// and the usage counts that keep referenced rows from being deleted. Every query is scoped by the marina.

// ---- pricing_settings ----

// GetPricingSettings returns the settings of a marina, or ErrNotFound when none were stored yet.
func GetPricingSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*PricingSettings, error) {
	p := new(PricingSettings)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// EnsurePricingSettings inserts p as the settings row of a marina unless one exists (ON CONFLICT DO NOTHING, so
// concurrent callers never abort their transaction), then returns the stored row.
func EnsurePricingSettings(ctx context.Context, db bun.IDB, s MarinaScope, p *PricingSettings) (*PricingSettings, error) {
	p.MarinaID = s.ID()
	if _, err := db.NewInsert().Model(p).On("CONFLICT (marina_id) DO NOTHING").Exec(ctx); err != nil {
		return nil, err
	}
	return GetPricingSettings(ctx, db, s)
}

// UpdatePricingSettings writes the defaults when the stored version equals expected, and bumps the version.
func UpdatePricingSettings(ctx context.Context, db bun.IDB, s MarinaScope, p *PricingSettings, expected int) error {
	res, err := db.NewUpdate().Model(p).Column("default_vat_rate_bp", "default_price_includes_vat", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetPricingSettings(ctx, db, s); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	p.Version = expected + 1
	return nil
}

// BumpActivationSeq increments pricing_settings.activation_seq of the marina. Run first in an activation
// transaction, it serialises activations (row lock on PostgreSQL; SQLite has one writer). The row must exist.
func BumpActivationSeq(ctx context.Context, db bun.IDB, s MarinaScope) error {
	return expectOne(db.NewUpdate().Table("pricing_settings").Set("activation_seq = activation_seq + 1").
		Where("marina_id = ?", s.ID()).Exec(ctx))
}

// ---- revenue_classes ----

// ListRevenueClasses returns the revenue classes of a marina by sort order and code.
func ListRevenueClasses(ctx context.Context, db bun.IDB, s MarinaScope, includeArchived bool) ([]RevenueClass, error) {
	out := []RevenueClass{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	err := q.OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// CountRevenueClasses returns how many revenue classes a marina has (archived included).
func CountRevenueClasses(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*RevenueClass)(nil)).Where("marina_id = ?", s.ID()).Count(ctx))
}

// GetRevenueClass returns one revenue class of a marina.
func GetRevenueClass(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RevenueClass, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(RevenueClass)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// InsertRevenueClass inserts a revenue class; a code already taken is ErrConflict.
func InsertRevenueClass(ctx context.Context, db bun.IDB, s MarinaScope, r *RevenueClass) error {
	r.MarinaID = s.ID()
	_, err := db.NewInsert().Model(r).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateRevenueClass writes the editable fields when the stored version equals expected, and bumps the version.
func UpdateRevenueClass(ctx context.Context, db bun.IDB, s MarinaScope, r *RevenueClass, expected int) error {
	res, err := db.NewUpdate().Model(r).Column("code", "name", "parent_id", "sort_order", "archived_at", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRevenueClass(ctx, db, s, r.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	r.Version = expected + 1
	return nil
}

// DeleteRevenueClass deletes a revenue class when its version equals expected.
func DeleteRevenueClass(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("revenue_classes").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRevenueClass(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// RevenueClassItemCounts returns, per revenue class id, the number of items classified under it.
func RevenueClassItemCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	var rows []struct {
		ID string `bun:"revenue_class_id"`
		N  int    `bun:"n"`
	}
	err := db.NewSelect().Table("items").Column("revenue_class_id").ColumnExpr("COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Group("revenue_class_id").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.ID] = r.N
	}
	return out, nil
}

// CountRevenueClassChildren returns the number of subclasses of a revenue class.
func CountRevenueClassChildren(ctx context.Context, db bun.IDB, s MarinaScope, id string) (int, error) {
	return toInt(db.NewSelect().Model((*RevenueClass)(nil)).Where("marina_id = ?", s.ID()).Where("parent_id = ?", id).Count(ctx))
}

// ---- items ----

// ItemFilter narrows ListItems; zero values mean any.
type ItemFilter struct {
	Type   string
	Active *bool
	Q      string // code or Italian/English name substring
}

// ListItems returns a page of the items of a marina ordered by sort order and code (the cursor carries the id of
// the last item of the previous page).
func ListItems(ctx context.Context, db bun.IDB, s MarinaScope, f ItemFilter, cursor string, limit int) (Page[Item], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Item]{}, err
	}
	limit = ClampLimit(limit)
	var rows []Item
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Type != "" {
		q = q.Where("item_type = ?", f.Type)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if f.Q != "" {
		like := "%" + escapeLike(f.Q) + "%"
		q = q.Where("(LOWER(code) LIKE ? OR LOWER(CAST(name AS TEXT)) LIKE ?)", like, like)
	}
	if after != "" {
		last, err := GetItem(ctx, db, s, after)
		if err != nil {
			return Page[Item]{}, ErrInvalidCursor
		}
		q = q.Where("(sort_order > ? OR (sort_order = ? AND code > ?))", last.SortOrder, last.SortOrder, last.Code)
	}
	if err := q.OrderExpr("sort_order, code").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Item]{}, err
	}
	return pageOf(rows, limit, func(r Item) string { return r.ID }), nil
}

// AllItems returns every item of a marina ordered by sort order and code.
func AllItems(ctx context.Context, db bun.IDB, s MarinaScope) ([]Item, error) {
	out := []Item{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// GetItem returns one item of a marina.
func GetItem(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Item, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	it := new(Item)
	if err := db.NewSelect().Model(it).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return it, nil
}

// ItemByCode returns the item of a marina with a code.
func ItemByCode(ctx context.Context, db bun.IDB, s MarinaScope, code string) (*Item, error) {
	it := new(Item)
	if err := db.NewSelect().Model(it).Where("marina_id = ?", s.ID()).Where("code = ?", code).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return it, nil
}

// ItemsByIDs returns the items of a marina among ids (unknown and invalid ids are skipped), by code.
func ItemsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Item, error) {
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if ValidID(id) {
			valid = append(valid, id)
		}
	}
	out := []Item{}
	if len(valid) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(valid)).OrderExpr("code").Scan(ctx)
	return out, err
}

// InsertItem inserts an item; a code already taken is ErrConflict.
func InsertItem(ctx context.Context, db bun.IDB, s MarinaScope, it *Item) error {
	it.MarinaID = s.ID()
	_, err := db.NewInsert().Model(it).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateItem writes the editable fields when the stored version equals expected, and bumps the version.
func UpdateItem(ctx context.Context, db bun.IDB, s MarinaScope, it *Item, expected int) error {
	res, err := db.NewUpdate().Model(it).Column("code", "name", "description", "item_type", "unit", "vat_rate_bp", "vat_nature",
		"price_includes_vat", "revenue_class_id", "external_code", "active", "online_visible", "sort_order", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", it.ID).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetItem(ctx, db, s, it.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	it.Version = expected + 1
	return nil
}

// DeleteItem deletes an item when its version equals expected; an item still referenced by a rule is
// ErrConflict (foreign key).
func DeleteItem(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("items").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if IsForeignKeyViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetItem(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ItemPriceListRefs returns the ids of the price lists of a marina that reference an item: as the item of a base
// rule, inside a rule body (targets, includes) or in the list settings (primary items). Ids are quoted in the
// JSON text, so a substring match on the quoted id is exact.
func ItemPriceListRefs(ctx context.Context, db bun.IDB, s MarinaScope, itemID string) ([]string, error) {
	quoted := `%"` + itemID + `"%`
	var ids []string
	err := db.NewSelect().Table("price_lists").Column("id").Where("marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("CAST(settings AS TEXT) LIKE ?", quoted).
				WhereOr("id IN (SELECT price_list_id FROM price_rules WHERE marina_id = ? AND (item_id = ? OR CAST(body AS TEXT) LIKE ?))", s.ID(), itemID, quoted)
		}).OrderExpr("id").Scan(ctx, &ids)
	return ids, err
}

// ---- berth_categories ----

// ListBerthCategories returns the berth categories of a marina by sort order and code.
func ListBerthCategories(ctx context.Context, db bun.IDB, s MarinaScope, includeArchived bool) ([]BerthCategory, error) {
	out := []BerthCategory{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}
	err := q.OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// GetBerthCategory returns one berth category of a marina.
func GetBerthCategory(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*BerthCategory, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(BerthCategory)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// InsertBerthCategory inserts a berth category; a code already taken is ErrConflict.
func InsertBerthCategory(ctx context.Context, db bun.IDB, s MarinaScope, c *BerthCategory) error {
	c.MarinaID = s.ID()
	_, err := db.NewInsert().Model(c).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateBerthCategory writes the editable fields (never the code) when the stored version equals expected.
func UpdateBerthCategory(ctx context.Context, db bun.IDB, s MarinaScope, c *BerthCategory, expected int) error {
	res, err := db.NewUpdate().Model(c).Column("name", "applies_to", "sort_order", "archived_at", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBerthCategory(ctx, db, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// DeleteBerthCategory deletes a berth category when its version equals expected.
func DeleteBerthCategory(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("berth_categories").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBerthCategory(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// Element tables that carry a price_class, by resource type.
var priceClassTables = map[string]string{"berth": "berths", "dynamic_area": "dynamic_areas", "land_resource": "land_resources"}

// PriceClassCounts returns, per resource type (berth, dynamic_area, land_resource), the number of elements per
// non-empty price_class code. Inactive elements are counted too: they keep their class.
func PriceClassCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	for kind, table := range priceClassTables {
		var rows []struct {
			Code string `bun:"price_class"`
			N    int    `bun:"n"`
		}
		err := db.NewSelect().Table(table).Column("price_class").ColumnExpr("COUNT(*) AS n").
			Where("marina_id = ?", s.ID()).Where("price_class IS NOT NULL").Where("price_class <> ''").
			Group("price_class").Scan(ctx, &rows)
		if err != nil {
			return nil, err
		}
		m := make(map[string]int, len(rows))
		for _, r := range rows {
			m[r.Code] = r.N
		}
		out[kind] = m
	}
	return out, nil
}

// ElementsWithoutPriceClass returns, per resource type, the number of active elements with an empty price_class.
func ElementsWithoutPriceClass(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	out := map[string]int{}
	for kind, table := range priceClassTables {
		n, err := db.NewSelect().Table(table).Where("marina_id = ?", s.ID()).Where("active = ?", true).
			Where("(price_class IS NULL OR price_class = '')").Count(ctx)
		if err != nil {
			return nil, err
		}
		out[kind] = int(n)
	}
	return out, nil
}

// CategoryRuleRefs returns the number of price rules and price list bands of a marina whose JSON mentions a
// category code as a quoted string (rule scopes, per_berth amounts, band links). The match is textual and can
// over-count a rule that uses the same text as another code, which only makes deletion more conservative.
func CategoryRuleRefs(ctx context.Context, db bun.IDB, s MarinaScope, code string) (int, error) {
	quoted := `%"` + strings.ReplaceAll(strings.ReplaceAll(code, `%`, ``), `_`, `\_`) + `"%`
	n, err := db.NewSelect().Table("price_rules").Where("marina_id = ?", s.ID()).
		Where(`CAST(body AS TEXT) LIKE ? ESCAPE '\'`, quoted).Count(ctx)
	if err != nil {
		return 0, err
	}
	b, err := db.NewSelect().Table("price_list_bands").Where("marina_id = ?", s.ID()).Where("category_code = ?", code).Count(ctx)
	return int(n + b), err
}
