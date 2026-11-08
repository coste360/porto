package store

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the M10 yard catalogue (m10-plan §3; owned by QUOTE): yard settings, work types, qualifications, item
// yard settings, formula variables, band tables, work packages and their insert-only versions. Every query is
// scoped by marina; a duplicate code or name is a unique violation the service maps to code_taken.

// yardVersionedUpdate updates the given columns of model when the stored version equals expected and bumps it; no row means
// ErrNotFound when the row is missing, else ErrVersionMismatch.
func yardVersionedUpdate(ctx context.Context, tx bun.IDB, s MarinaScope, model any, idCol, id string, expected int, cols ...string) error {
	res, err := tx.NewUpdate().Model(model).Column(cols...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("? = ?", bun.Ident(idCol), id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) > 0 {
		return nil
	}
	n, err := tx.NewSelect().Model(model).Where("marina_id = ?", s.ID()).Where("? = ?", bun.Ident(idCol), id).Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrVersionMismatch
}

// ---- yard settings ----

// GetYardSettings returns the yard settings row of the marina (ErrNotFound when never saved).
func GetYardSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*YardSettings, error) {
	row := new(YardSettings)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// SaveYardSettings inserts the row (expected 0) or updates it when the stored version equals expected.
func SaveYardSettings(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardSettings, expected int, now time.Time) error {
	row.MarinaID = s.ID()
	row.UpdatedAt = UTC(now)
	if expected == 0 {
		row.Version, row.CreatedAt = 1, UTC(now)
		_, err := tx.NewInsert().Model(row).Exec(ctx)
		if IsUniqueViolation(err) {
			return ErrVersionMismatch
		}
		return err
	}
	if err := yardVersionedUpdate(ctx, tx, s, row, "marina_id", s.ID(), expected,
		"test_vessel", "quote_validity_days", "deposit_bp", "advance_on_accept", "external_markup_bp", "external_item_id",
		"fixed_includes_external", "workers_issue_materials", "lift_from_slot", "lift_to_slot", "lift_request_on_move",
		"updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// ---- work types ----

// ListWorkTypes returns the work types of the marina by sort order and code.
func ListWorkTypes(ctx context.Context, db bun.IDB, s MarinaScope, active *bool) ([]WorkType, error) {
	out := []WorkType{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	return out, q.OrderExpr("sort_order, code").Scan(ctx)
}

// GetWorkType returns a work type of the marina.
func GetWorkType(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WorkType, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(WorkType)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertWorkType inserts a work type.
func InsertWorkType(ctx context.Context, tx bun.IDB, s MarinaScope, row *WorkType) error {
	row.MarinaID, row.Version = s.ID(), 1
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateWorkType writes the mutable columns of a work type (optimistic version).
func UpdateWorkType(ctx context.Context, tx bun.IDB, s MarinaScope, row *WorkType, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected, "code", "name", "revenue_class_id", "active", "sort_order", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// ---- qualifications ----

// ListYardQualifications returns the qualifications of the marina by sort order and code.
func ListYardQualifications(ctx context.Context, db bun.IDB, s MarinaScope, active *bool) ([]YardQualification, error) {
	out := []YardQualification{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	return out, q.OrderExpr("sort_order, code").Scan(ctx)
}

// GetYardQualification returns a qualification of the marina.
func GetYardQualification(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*YardQualification, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(YardQualification)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertYardQualification inserts a qualification.
func InsertYardQualification(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardQualification) error {
	row.MarinaID, row.Version = s.ID(), 1
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateYardQualification writes the mutable columns of a qualification (optimistic version).
func UpdateYardQualification(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardQualification, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected,
		"code", "name", "labour_item_id", "default_cost_cents", "lift_operator", "active", "sort_order", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// ---- item yard settings ----

// YardItemRow is an item of the marina with its yard settings (Settings nil = not configured).
type YardItemRow struct {
	Item     Item
	Settings *YardItemSettings
}

// YardItemFilter filters ListYardItems.
type YardItemFilter struct {
	Query       string
	Category    string
	ItemType    string
	YardEnabled *bool
	ItemTypes   []string // allowed item types (yard: service, labour, material, product, fee)
}

// ListYardItems returns a page of items (by code, cursor = code) with their yard settings.
func ListYardItems(ctx context.Context, db bun.IDB, s MarinaScope, f YardItemFilter, cursor string, limit int) (Page[YardItemRow], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[YardItemRow]{}, err
	}
	limit = ClampLimit(limit)
	var items []Item
	q := db.NewSelect().Model(&items).Where("marina_id = ?", s.ID())
	if len(f.ItemTypes) > 0 {
		q = q.Where("item_type IN (?)", bun.List(f.ItemTypes))
	}
	if f.ItemType != "" {
		q = q.Where("item_type = ?", f.ItemType)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := likePattern(strings.ToLower(t))
		q = q.Where("(LOWER(code) LIKE ? ESCAPE '!' OR LOWER(CAST(name AS TEXT)) LIKE ? ESCAPE '!')", like, like)
	}
	if f.Category != "" || f.YardEnabled != nil {
		sub := db.NewSelect().TableExpr("yard_item_settings AS y").Column("y.item_id").Where("y.marina_id = ?", s.ID())
		if f.Category != "" {
			sub = sub.Where("y.category = ?", f.Category)
		}
		if f.YardEnabled != nil {
			sub = sub.Where("y.yard_enabled = ?", *f.YardEnabled)
		}
		q = q.Where("id IN (?)", sub)
	}
	if after != "" {
		q = q.Where("code > ?", after)
	}
	if err := q.OrderExpr("code").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[YardItemRow]{}, err
	}
	page := pageOf(items, limit, func(it Item) string { return it.Code })
	ids := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		ids = append(ids, it.ID)
	}
	settings, err := YardItemSettingsByIDs(ctx, db, s, ids)
	if err != nil {
		return Page[YardItemRow]{}, err
	}
	out := Page[YardItemRow]{Items: make([]YardItemRow, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, it := range page.Items {
		r := YardItemRow{Item: it}
		if st, ok := settings[it.ID]; ok {
			st := st
			r.Settings = &st
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}

// GetYardItemSettings returns the yard settings of an item of the marina (ErrNotFound when not configured).
func GetYardItemSettings(ctx context.Context, db bun.IDB, s MarinaScope, itemID string) (*YardItemSettings, error) {
	if !ValidID(itemID) {
		return nil, ErrNotFound
	}
	row := new(YardItemSettings)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("item_id = ?", itemID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// YardItemSettingsByIDs returns the yard settings of the given items, by item id.
func YardItemSettingsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]YardItemSettings, error) {
	out := map[string]YardItemSettings{}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if ValidID(id) {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return out, nil
	}
	var rows []YardItemSettings
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("item_id IN (?)", bun.List(valid)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ItemID] = r
	}
	return out, nil
}

// AllYardItemSettings returns every yard settings row of the marina with a formula (for re-validation).
func AllYardItemSettings(ctx context.Context, db bun.IDB, s MarinaScope) ([]YardItemSettings, error) {
	out := []YardItemSettings{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("(qty_formula <> '' OR price_formula <> '')").
		OrderExpr("item_id").Scan(ctx)
	return out, err
}

// SaveYardItemSettings inserts the row (expected 0) or updates it when the stored version equals expected.
func SaveYardItemSettings(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardItemSettings, expected int, now time.Time) error {
	row.MarinaID = s.ID()
	row.UpdatedAt = UTC(now)
	if expected == 0 {
		row.Version, row.CreatedAt = 1, UTC(now)
		_, err := tx.NewInsert().Model(row).Exec(ctx)
		return err
	}
	if err := yardVersionedUpdate(ctx, tx, s, row, "item_id", row.ItemID, expected,
		"yard_enabled", "cost_cents", "qualification_id", "qty_formula", "price_formula", "category", "test_case", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// ---- formula variables ----

// ListFormulaVariables returns the derived and job variables of the marina by name.
func ListFormulaVariables(ctx context.Context, db bun.IDB, s MarinaScope) ([]FormulaVariable, error) {
	out := []FormulaVariable{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("name").Scan(ctx)
	return out, err
}

// GetFormulaVariable returns a variable of the marina.
func GetFormulaVariable(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*FormulaVariable, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(FormulaVariable)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertFormulaVariable inserts a variable.
func InsertFormulaVariable(ctx context.Context, tx bun.IDB, s MarinaScope, row *FormulaVariable) error {
	row.MarinaID, row.Version = s.ID(), 1
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateFormulaVariable writes the mutable columns of a variable (optimistic version).
func UpdateFormulaVariable(ctx context.Context, tx bun.IDB, s MarinaScope, row *FormulaVariable, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected,
		"unit", "expression", "min_milli", "max_milli", "default_milli", "default_enum", "enum_values", "label", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// DeleteFormulaVariable deletes a variable when the stored version equals expected.
func DeleteFormulaVariable(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := tx.NewDelete().Model((*FormulaVariable)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) > 0 {
		return nil
	}
	if _, err := GetFormulaVariable(ctx, tx, s, id); err != nil {
		return err
	}
	return ErrVersionMismatch
}

// ---- band tables ----

// ListFormulaTables returns the band tables of the marina by name.
func ListFormulaTables(ctx context.Context, db bun.IDB, s MarinaScope) ([]FormulaTable, error) {
	out := []FormulaTable{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("name").Scan(ctx)
	return out, err
}

// GetFormulaTable returns a band table of the marina.
func GetFormulaTable(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*FormulaTable, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(FormulaTable)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertFormulaTable inserts a band table.
func InsertFormulaTable(ctx context.Context, tx bun.IDB, s MarinaScope, row *FormulaTable) error {
	row.MarinaID, row.Version = s.ID(), 1
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateFormulaTable writes the label and the rows of a band table (optimistic version).
func UpdateFormulaTable(ctx context.Context, tx bun.IDB, s MarinaScope, row *FormulaTable, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected, "label", "band_rows", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// ---- work packages ----

// WorkPackageFilter filters ListWorkPackages.
type WorkPackageFilter struct {
	Active     *bool
	WorkTypeID string
}

// ListWorkPackages returns a page of packages by code (cursor = code).
func ListWorkPackages(ctx context.Context, db bun.IDB, s MarinaScope, f WorkPackageFilter, cursor string, limit int) (Page[WorkPackage], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[WorkPackage]{}, err
	}
	limit = ClampLimit(limit)
	var rows []WorkPackage
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if f.WorkTypeID != "" {
		if !ValidID(f.WorkTypeID) {
			return Page[WorkPackage]{Items: []WorkPackage{}}, nil
		}
		q = q.Where("work_type_id = ?", f.WorkTypeID)
	}
	if after != "" {
		q = q.Where("code > ?", after)
	}
	if err := q.OrderExpr("code").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[WorkPackage]{}, err
	}
	return pageOf(rows, limit, func(p WorkPackage) string { return p.Code }), nil
}

// AllWorkPackages returns every package of the marina (active only when activeOnly), by code.
func AllWorkPackages(ctx context.Context, db bun.IDB, s MarinaScope, activeOnly bool) ([]WorkPackage, error) {
	out := []WorkPackage{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if activeOnly {
		q = q.Where("active = ?", true)
	}
	return out, q.OrderExpr("code").Scan(ctx)
}

// GetWorkPackage returns a package of the marina; lock takes a row lock on PostgreSQL.
func GetWorkPackage(ctx context.Context, db bun.IDB, s MarinaScope, id string, lock bool) (*WorkPackage, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(WorkPackage)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// WorkPackagesByIDs returns the packages of the marina among ids, by id.
func WorkPackagesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]WorkPackage, error) {
	out := map[string]WorkPackage{}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if ValidID(id) {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return out, nil
	}
	var rows []WorkPackage
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(valid)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// InsertWorkPackage inserts a package (current_version as given).
func InsertWorkPackage(ctx context.Context, tx bun.IDB, s MarinaScope, row *WorkPackage) error {
	row.MarinaID, row.Version = s.ID(), 1
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateWorkPackage writes the name, work type, active flag and current version (optimistic version).
func UpdateWorkPackage(ctx context.Context, tx bun.IDB, s MarinaScope, row *WorkPackage, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected, "name", "work_type_id", "active", "current_version", "updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// InsertWorkPackageVersion inserts an immutable package version (unique (package_id, version)).
func InsertWorkPackageVersion(ctx context.Context, tx bun.IDB, s MarinaScope, row *WorkPackageVersion) error {
	row.MarinaID = s.ID()
	row.CreatedAt = UTC(row.CreatedAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// GetWorkPackageVersion returns a version of a package of the marina.
func GetWorkPackageVersion(ctx context.Context, db bun.IDB, s MarinaScope, packageID string, version int) (*WorkPackageVersion, error) {
	if !ValidID(packageID) {
		return nil, ErrNotFound
	}
	row := new(WorkPackageVersion)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("package_id = ?", packageID).
		Where("version = ?", version).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// WorkPackageVersionBrief is a version number with its creation time.
type WorkPackageVersionBrief struct {
	Version   int       `bun:"version"`
	CreatedAt time.Time `bun:"created_at"`
}

// ListWorkPackageVersions returns the versions of a package, newest first.
func ListWorkPackageVersions(ctx context.Context, db bun.IDB, s MarinaScope, packageID string) ([]WorkPackageVersionBrief, error) {
	out := []WorkPackageVersionBrief{}
	err := db.NewSelect().TableExpr("work_package_versions").Column("version", "created_at").
		Where("marina_id = ?", s.ID()).Where("package_id = ?", packageID).OrderExpr("version DESC").Scan(ctx, &out)
	return out, err
}
