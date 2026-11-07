package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of price lists and their children (m2-plan §3, §5.1). Writes on rows of a list that is not a draft
// fail in the immutability triggers (INV-22); every write here passes through MapImmutable so callers see
// ErrImmutable.

// PriceListFilter narrows ListPriceLists; zero values mean any.
type PriceListFilter struct {
	Status       string
	ContractType string
	On           calendar.Date // lists valid on this night
}

// PriceListRow is a price list with its rule count.
type PriceListRow struct {
	PriceList `bun:",extend"`
	RuleCount int `bun:"rule_count"`
}

// ListPriceLists returns the price lists of a marina, newest validity first.
func ListPriceLists(ctx context.Context, db bun.IDB, s MarinaScope, f PriceListFilter) ([]PriceListRow, error) {
	rows := []PriceListRow{}
	q := db.NewSelect().Model(&rows).ColumnExpr("price_list.*").
		ColumnExpr("(SELECT COUNT(*) FROM price_rules AS pr WHERE pr.price_list_id = price_list.id) AS rule_count").
		Where("price_list.marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("price_list.status = ?", f.Status)
	}
	if !f.On.IsZero() {
		q = q.Where("price_list.valid_from <= ?", f.On).Where("price_list.valid_to >= ?", f.On)
	}
	if err := q.OrderExpr("price_list.valid_from DESC, price_list.code").Scan(ctx); err != nil {
		return nil, err
	}
	if f.ContractType == "" {
		return rows, nil
	}
	out := rows[:0]
	for _, r := range rows {
		for _, ct := range r.ContractTypes {
			if ct == f.ContractType {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

// CountPriceLists returns how many price lists a marina has.
func CountPriceLists(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*PriceList)(nil)).Where("marina_id = ?", s.ID()).Count(ctx))
}

// GetPriceList returns one price list of a marina.
func GetPriceList(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PriceList, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	l := new(PriceList)
	if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return l, nil
}

// PriceListByCode returns the price list of a marina with a code.
func PriceListByCode(ctx context.Context, db bun.IDB, s MarinaScope, code string) (*PriceList, error) {
	l := new(PriceList)
	if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("code = ?", code).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return l, nil
}

// ActivePriceLists returns the active lists of a marina by valid_from.
func ActivePriceLists(ctx context.Context, db bun.IDB, s MarinaScope) ([]PriceList, error) {
	out := []PriceList{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", PriceListActive).
		OrderExpr("valid_from, id").Scan(ctx)
	return out, err
}

// InsertPriceList inserts a price list; a code already taken is ErrConflict.
func InsertPriceList(ctx context.Context, db bun.IDB, s MarinaScope, l *PriceList) error {
	l.MarinaID = s.ID()
	_, err := db.NewInsert().Model(l).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdatePriceListHeader writes the editable header of a draft (code, name, validity, contract types, settings,
// next_rule_seq) when the stored version equals expected, and bumps the version.
func UpdatePriceListHeader(ctx context.Context, db bun.IDB, s MarinaScope, l *PriceList, expected int) error {
	return updatePriceList(ctx, db, s, l, expected,
		"code", "name", "valid_from", "valid_to", "contract_types", "settings", "next_rule_seq", "updated_at")
}

// UpdatePriceListStatus writes a status change (activation or archive: status, validity, hash, activation and
// archive stamps) when the stored version equals expected, and bumps the version.
func UpdatePriceListStatus(ctx context.Context, db bun.IDB, s MarinaScope, l *PriceList, expected int) error {
	return updatePriceList(ctx, db, s, l, expected,
		"status", "valid_from", "valid_to", "content_sha256", "activated_at", "activated_by_user_id", "archived_at", "updated_at")
}

func updatePriceList(ctx context.Context, db bun.IDB, s MarinaScope, l *PriceList, expected int, cols ...string) error {
	res, err := db.NewUpdate().Model(l).Column(cols...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", l.ID).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return MapImmutable(err)
	}
	if affected(res) == 0 {
		if _, err := GetPriceList(ctx, db, s, l.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	l.Version = expected + 1
	return nil
}

// TruncatePriceList shortens the validity of an active list (a successor's activation) and bumps its version.
func TruncatePriceList(ctx context.Context, db bun.IDB, s MarinaScope, id string, validTo calendar.Date, now time.Time) error {
	res, err := db.NewUpdate().Table("price_lists").Set("valid_to = ?", validTo).Set("updated_at = ?", now).
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, MapImmutable(err))
}

// DeletePriceList deletes a price list (children cascade) when its version equals expected.
func DeletePriceList(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("price_lists").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if IsForeignKeyViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return MapImmutable(err)
	}
	if affected(res) == 0 {
		if _, err := GetPriceList(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ClearPriceListSource detaches the lists derived from a list (source_price_list_id) before it is deleted. Only
// drafts are detached: a published list keeps its lineage (and then the source cannot be deleted).
func ClearPriceListSource(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	_, err := db.NewUpdate().Table("price_lists").Set("source_price_list_id = NULL").
		Where("marina_id = ?", s.ID()).Where("source_price_list_id = ?", id).Where("status = ?", PriceListDraft).Exec(ctx)
	return err
}

// PriceListChildren are the periods, bands and rules of a list, in document order.
type PriceListChildren struct {
	Periods []RatePeriod
	Bands   []PriceListBand
	Rules   []PriceRule
}

// LoadPriceListChildren returns the periods (by sort order), bands (by sort order) and rules (by stage order then
// sort by seq) of a list of the marina.
func LoadPriceListChildren(ctx context.Context, db bun.IDB, s MarinaScope, listID string) (*PriceListChildren, error) {
	out := &PriceListChildren{Periods: []RatePeriod{}, Bands: []PriceListBand{}, Rules: []PriceRule{}}
	if err := db.NewSelect().Model(&out.Periods).Where("marina_id = ?", s.ID()).Where("price_list_id = ?", listID).
		OrderExpr("sort_order, code").Scan(ctx); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Bands).Where("marina_id = ?", s.ID()).Where("price_list_id = ?", listID).
		OrderExpr("sort_order").Scan(ctx); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Rules).Where("marina_id = ?", s.ID()).Where("price_list_id = ?", listID).
		OrderExpr("CASE stage WHEN 'base' THEN 0 WHEN 'modifier' THEN 1 ELSE 2 END, seq").Scan(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplacePriceListChildren replaces the periods, bands and rules of a draft: rows whose id is not kept are
// deleted, kept rows are updated, new rows inserted. Bands are deleted and re-inserted as a whole, so a reorder
// never collides with the (price_list_id, sort_order) unique index; band ids are preserved.
func ReplacePriceListChildren(ctx context.Context, db bun.IDB, s MarinaScope, listID string, c *PriceListChildren, existing *PriceListChildren) error {
	keepP := map[string]bool{}
	for _, p := range c.Periods {
		keepP[p.ID] = true
	}
	keepR := map[string]bool{}
	for _, r := range c.Rules {
		keepR[r.ID] = true
	}
	oldP := map[string]bool{}
	for _, p := range existing.Periods {
		oldP[p.ID] = true
		if !keepP[p.ID] {
			if _, err := db.NewDelete().Table("rate_periods").Where("marina_id = ?", s.ID()).Where("id = ?", p.ID).Exec(ctx); err != nil {
				return MapImmutable(err)
			}
		}
	}
	oldR := map[string]bool{}
	for _, r := range existing.Rules {
		oldR[r.ID] = true
		if !keepR[r.ID] {
			if _, err := db.NewDelete().Table("price_rules").Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx); err != nil {
				return MapImmutable(err)
			}
		}
	}
	if _, err := db.NewDelete().Table("price_list_bands").Where("marina_id = ?", s.ID()).Where("price_list_id = ?", listID).Exec(ctx); err != nil {
		return MapImmutable(err)
	}
	for i := range c.Periods {
		p := &c.Periods[i]
		p.MarinaID, p.PriceListID = s.ID(), listID
		var err error
		if oldP[p.ID] {
			_, err = db.NewUpdate().Model(p).Column("code", "name", "kind", "color", "ranges", "sort_order", "updated_at").
				Where("marina_id = ?", s.ID()).Where("id = ?", p.ID).Exec(ctx)
		} else {
			_, err = db.NewInsert().Model(p).Exec(ctx)
		}
		if err != nil {
			return mapChildErr(err)
		}
	}
	for i := range c.Bands {
		b := &c.Bands[i]
		b.MarinaID, b.PriceListID = s.ID(), listID
		if _, err := db.NewInsert().Model(b).Exec(ctx); err != nil {
			return mapChildErr(err)
		}
	}
	// Rule seq is unique per list: move kept rules whose seq changes out of the way first (negative seq), then
	// write the final values.
	for i := range c.Rules {
		r := &c.Rules[i]
		if oldR[r.ID] {
			if _, err := db.NewUpdate().Table("price_rules").Set("seq = ?", -r.Seq).Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx); err != nil {
				return mapChildErr(err)
			}
		}
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		r.MarinaID, r.PriceListID = s.ID(), listID
		var err error
		if oldR[r.ID] {
			_, err = db.NewUpdate().Model(r).Column("seq", "stage", "name", "item_id", "priority", "stackable", "active", "body", "updated_at").
				Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx)
		} else {
			_, err = db.NewInsert().Model(r).Exec(ctx)
		}
		if err != nil {
			return mapChildErr(err)
		}
	}
	return nil
}

// InsertPriceListChildren inserts the periods, bands and rules of a new list.
func InsertPriceListChildren(ctx context.Context, db bun.IDB, s MarinaScope, listID string, c *PriceListChildren) error {
	return ReplacePriceListChildren(ctx, db, s, listID, c, &PriceListChildren{})
}

func mapChildErr(err error) error {
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if IsForeignKeyViolation(err) {
		return ErrNotFound
	}
	return MapImmutable(err)
}

// InsertPriceListItems writes the item snapshot of a list; called by the activation while the list is still a
// draft (the triggers freeze the rows of a published list, INV-22).
func InsertPriceListItems(ctx context.Context, db bun.IDB, s MarinaScope, listID string, rows []PriceListItem) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].MarinaID, rows[i].PriceListID = s.ID(), listID
		if rows[i].ID == "" {
			rows[i].ID = NewID()
		}
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return mapChildErr(err)
}

// PriceListItems returns the item snapshot of a list of the marina, by code.
func PriceListItems(ctx context.Context, db bun.IDB, s MarinaScope, listID string) ([]PriceListItem, error) {
	out := []PriceListItem{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("price_list_id = ?", listID).
		OrderExpr("code").Scan(ctx)
	return out, err
}
