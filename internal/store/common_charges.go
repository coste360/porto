package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Queries of the M4 millesimal tables, common charge budgets, items, runs and allocations (m4-plan §4.5).
// Entries of a non-draft table and allocations of an issued run are frozen by triggers (INV-41, INV-44).

// ---- millesimal_tables ----

// MillesimalTableStats are the entry count and Σ millis of a table.
type MillesimalTableStats struct {
	TableID string `bun:"table_id"`
	Count   int    `bun:"n"`
	Sum     int64  `bun:"sum_millis"`
}

// InsertMillesimalTable inserts a table; a code already taken is ErrConflict.
func InsertMillesimalTable(ctx context.Context, db bun.IDB, s MarinaScope, t *MillesimalTable) error {
	t.MarinaID = s.ID()
	_, err := db.NewInsert().Model(t).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetMillesimalTable returns a table of the marina.
func GetMillesimalTable(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*MillesimalTable, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(MillesimalTable)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// LockMillesimalTable locks a table row (PostgreSQL) and returns it.
func LockMillesimalTable(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*MillesimalTable, error) {
	if err := lockRow(ctx, tx, "millesimal_tables", s, id); err != nil {
		return nil, err
	}
	return GetMillesimalTable(ctx, tx, s, id)
}

// UpdateMillesimalTable writes name, status and instants when the version equals expected.
func UpdateMillesimalTable(ctx context.Context, db bun.IDB, s MarinaScope, t *MillesimalTable, expected int) error {
	if err := versionedUpdate(ctx, db, s, t, "millesimal_tables", t.ID, expected, "name", "status", "activated_at", "archived_at", "updated_at"); err != nil {
		return err
	}
	t.Version = expected + 1
	return nil
}

// BumpMillesimalTable bumps the version of a draft table (entries replaced) when it equals expected.
func BumpMillesimalTable(ctx context.Context, db bun.IDB, s MarinaScope, t *MillesimalTable, expected int) error {
	return UpdateMillesimalTable(ctx, db, s, t, expected)
}

// DeleteMillesimalTable deletes a draft table when its version equals expected; a table used by an item is
// ErrConflict.
func DeleteMillesimalTable(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	n, err := db.NewSelect().Model((*CommonChargeItem)(nil)).Where("marina_id = ?", s.ID()).Where("table_id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	res, err := db.NewDelete().Model((*MillesimalTable)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return MapRightsImmutable(err)
	}
	if affected(res) == 0 {
		if _, err := GetMillesimalTable(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListMillesimalTables returns the tables of a marina by code.
func ListMillesimalTables(ctx context.Context, db bun.IDB, s MarinaScope) ([]MillesimalTable, error) {
	out := []MillesimalTable{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("code").Scan(ctx)
	return out, err
}

// MillesimalStats returns, per table id, the entry count and Σ millis.
func MillesimalStats(ctx context.Context, db bun.IDB, s MarinaScope, tableIDs []string) (map[string]MillesimalTableStats, error) {
	out := map[string]MillesimalTableStats{}
	ids := validIDs(tableIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []MillesimalTableStats
	if err := db.NewSelect().Model((*MillesimalEntry)(nil)).ColumnExpr("table_id, count(*) AS n, coalesce(sum(millis), 0) AS sum_millis").
		Where("marina_id = ?", s.ID()).Where("table_id IN (?)", bun.List(ids)).GroupExpr("table_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TableID] = r
	}
	return out, nil
}

// ReplaceMillesimalEntries replaces the entries of a table (refused by the trigger when the table is not draft).
func ReplaceMillesimalEntries(ctx context.Context, db bun.IDB, s MarinaScope, tableID string, es []MillesimalEntry) error {
	if _, err := db.NewDelete().Model((*MillesimalEntry)(nil)).Where("marina_id = ?", s.ID()).Where("table_id = ?", tableID).Exec(ctx); err != nil {
		return MapRightsImmutable(err)
	}
	for start := 0; start < len(es); start += 500 {
		chunk := es[start:min(start+500, len(es))]
		for i := range chunk {
			chunk[i].MarinaID, chunk[i].TableID = s.ID(), tableID
		}
		if _, err := db.NewInsert().Model(&chunk).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			return MapRightsImmutable(err)
		}
	}
	return nil
}

// EntriesOfTables returns the entries of tables, by table and berth id.
func EntriesOfTables(ctx context.Context, db bun.IDB, s MarinaScope, tableIDs []string) (map[string][]MillesimalEntry, error) {
	out := map[string][]MillesimalEntry{}
	ids := validIDs(tableIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []MillesimalEntry
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("table_id IN (?)", bun.List(ids)).OrderExpr("table_id, berth_id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TableID] = append(out[r.TableID], r)
	}
	return out, nil
}

// ---- common_charge_budgets and items ----

// InsertBudget inserts a budget; a second budget of the same year is ErrConflict.
func InsertBudget(ctx context.Context, db bun.IDB, s MarinaScope, b *CommonChargeBudget) error {
	b.MarinaID = s.ID()
	_, err := db.NewInsert().Model(b).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetBudget returns a budget of the marina.
func GetBudget(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CommonChargeBudget, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	b := new(CommonChargeBudget)
	if err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return b, nil
}

// LockBudget locks a budget row (PostgreSQL) and returns it.
func LockBudget(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*CommonChargeBudget, error) {
	if err := lockRow(ctx, tx, "common_charge_budgets", s, id); err != nil {
		return nil, err
	}
	return GetBudget(ctx, tx, s, id)
}

// UpdateBudget writes name, status and instants when the version equals expected.
func UpdateBudget(ctx context.Context, db bun.IDB, s MarinaScope, b *CommonChargeBudget, expected int) error {
	if err := versionedUpdate(ctx, db, s, b, "common_charge_budgets", b.ID, expected, "name", "status", "approved_at", "closed_at", "updated_at"); err != nil {
		return err
	}
	b.Version = expected + 1
	return nil
}

// DeleteBudget deletes a budget without runs when its version equals expected.
func DeleteBudget(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	n, err := db.NewSelect().Model((*CommonChargeRun)(nil)).Where("marina_id = ?", s.ID()).Where("budget_id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	res, err := db.NewDelete().Model((*CommonChargeBudget)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBudget(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListBudgets returns the budgets of a marina, newest year first.
func ListBudgets(ctx context.Context, db bun.IDB, s MarinaScope) ([]CommonChargeBudget, error) {
	out := []CommonChargeBudget{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("year DESC").Scan(ctx)
	return out, err
}

// InsertBudgetItem inserts a budget item.
func InsertBudgetItem(ctx context.Context, db bun.IDB, s MarinaScope, it *CommonChargeItem) error {
	it.MarinaID = s.ID()
	_, err := db.NewInsert().Model(it).Exec(ctx)
	return err
}

// GetBudgetItem returns an item of a budget.
func GetBudgetItem(ctx context.Context, db bun.IDB, s MarinaScope, budgetID, id string) (*CommonChargeItem, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	it := new(CommonChargeItem)
	if err := db.NewSelect().Model(it).Where("marina_id = ?", s.ID()).Where("budget_id = ?", budgetID).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return it, nil
}

// UpdateBudgetItem writes the editable fields of an item when the version equals expected.
func UpdateBudgetItem(ctx context.Context, db bun.IDB, s MarinaScope, it *CommonChargeItem, expected int) error {
	if err := versionedUpdate(ctx, db, s, it, "common_charge_items", it.ID, expected, "name", "table_id", "budget_cents", "actual_cents", "sort", "updated_at"); err != nil {
		return err
	}
	it.Version = expected + 1
	return nil
}

// DeleteBudgetItem deletes an item when its version equals expected (an item with allocations is ErrConflict).
func DeleteBudgetItem(ctx context.Context, db bun.IDB, s MarinaScope, budgetID, id string, expected int) error {
	n, err := db.NewSelect().Model((*CommonChargeAllocation)(nil)).Where("marina_id = ?", s.ID()).Where("item_id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	res, err := db.NewDelete().Model((*CommonChargeItem)(nil)).Where("marina_id = ?", s.ID()).Where("budget_id = ?", budgetID).
		Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBudgetItem(ctx, db, s, budgetID, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ItemsOfBudget returns the items of a budget by sort and name.
func ItemsOfBudget(ctx context.Context, db bun.IDB, s MarinaScope, budgetID string) ([]CommonChargeItem, error) {
	out := []CommonChargeItem{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("budget_id = ?", budgetID).OrderExpr("sort, name, id").Scan(ctx)
	return out, err
}

// SetItemActual writes the actual amount of an item (no version check: actuals are a bulk replace under the
// budget version).
func SetItemActual(ctx context.Context, db bun.IDB, s MarinaScope, it *CommonChargeItem) error {
	return expectOne(db.NewUpdate().Model(it).Column("actual_cents", "updated_at").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", it.ID).Exec(ctx))
}

// ---- runs and allocations ----

// InsertCommonRun inserts a run; a (budget, kind, seq) already taken is ErrConflict.
func InsertCommonRun(ctx context.Context, db bun.IDB, s MarinaScope, r *CommonChargeRun) error {
	r.MarinaID = s.ID()
	_, err := db.NewInsert().Model(r).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetCommonRun returns a run of the marina.
func GetCommonRun(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CommonChargeRun, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(CommonChargeRun)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// UpdateCommonRun writes status, totals and issued_at when the version equals expected. A second issued balance
// run of a budget violates common_charge_runs_balance_uidx: ErrConflict.
func UpdateCommonRun(ctx context.Context, db bun.IDB, s MarinaScope, r *CommonChargeRun, expected int) error {
	if err := versionedUpdate(ctx, db, s, r, "common_charge_runs", r.ID, expected, "status", "totals", "issued_at", "updated_at"); err != nil {
		return err
	}
	r.Version = expected + 1
	return nil
}

// RunsOfBudget returns the runs of a budget by kind and seq.
func RunsOfBudget(ctx context.Context, db bun.IDB, s MarinaScope, budgetID string) ([]CommonChargeRun, error) {
	out := []CommonChargeRun{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("budget_id = ?", budgetID).OrderExpr("kind, seq").Scan(ctx)
	return out, err
}

// NextCommonRunSeq returns 1 + the highest seq of a kind in a budget.
func NextCommonRunSeq(ctx context.Context, db bun.IDB, s MarinaScope, budgetID, kind string) (int, error) {
	var n int
	err := db.NewSelect().Model((*CommonChargeRun)(nil)).ColumnExpr("coalesce(max(seq), 0)").Where("marina_id = ?", s.ID()).
		Where("budget_id = ?", budgetID).Where("kind = ?", kind).Scan(ctx, &n)
	return n + 1, err
}

// InsertCommonAllocations inserts the allocations of a draft run.
func InsertCommonAllocations(ctx context.Context, db bun.IDB, s MarinaScope, rows []CommonChargeAllocation) error {
	for start := 0; start < len(rows); start += 500 {
		chunk := rows[start:min(start+500, len(rows))]
		for i := range chunk {
			chunk[i].MarinaID = s.ID()
		}
		if _, err := db.NewInsert().Model(&chunk).Exec(ctx); err != nil {
			return MapRightsImmutable(err)
		}
	}
	return nil
}

// AllocationFilterCommon narrows the allocations of a run.
type AllocationFilterCommon struct {
	ItemID     string
	CustomerID string
}

// AllocationsOfRun returns the allocations of a run by id.
func AllocationsOfRun(ctx context.Context, db bun.IDB, s MarinaScope, runID string, f AllocationFilterCommon) ([]CommonChargeAllocation, error) {
	out := []CommonChargeAllocation{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID)
	if f.ItemID != "" {
		q = q.Where("item_id = ?", f.ItemID)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	err := q.OrderExpr("id").Scan(ctx)
	return out, err
}

// PageAllocationsOfRun returns a page of the allocations of a run by id.
func PageAllocationsOfRun(ctx context.Context, db bun.IDB, s MarinaScope, runID string, f AllocationFilterCommon, cursor string, limit int) (Page[CommonChargeAllocation], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[CommonChargeAllocation]{}, err
	}
	limit = ClampLimit(limit)
	var rows []CommonChargeAllocation
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID)
	if f.ItemID != "" {
		q = q.Where("item_id = ?", f.ItemID)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[CommonChargeAllocation]{}, err
	}
	return pageOf(rows, limit, func(r CommonChargeAllocation) string { return r.ID }), nil
}

// ItemRunSum is Σ allocations of an item in issued runs of a kind.
type ItemRunSum struct {
	ItemID string `bun:"item_id"`
	Sum    int64  `bun:"total"`
}

// IssuedSumsByItem returns, per item of a budget, Σ allocations of its issued runs of a kind (the advances).
func IssuedSumsByItem(ctx context.Context, db bun.IDB, s MarinaScope, budgetID, kind string) (map[string]int64, error) {
	var rows []ItemRunSum
	err := db.NewRaw(`SELECT a.item_id, coalesce(sum(a.amount_cents), 0) AS total FROM common_charge_allocations AS a
		JOIN common_charge_runs AS r ON r.id = a.run_id
		WHERE a.marina_id = ? AND r.budget_id = ? AND r.kind = ? AND r.status = 'issued' GROUP BY a.item_id`, s.ID(), budgetID, kind).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.ItemID] = r.Sum
	}
	return out, nil
}

// LockCommonRun locks a common charge run row (PostgreSQL): issue and discard of one run are serialised.
func LockCommonRun(ctx context.Context, tx bun.IDB, s MarinaScope, id string) error {
	return lockRow(ctx, tx, "common_charge_runs", s, id)
}

// RightsBerthCodes returns the berths of the marina by code (CSV imports of millesimal entries and rights).
func RightsBerthCodes(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]Berth, error) {
	var rows []Berth
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]Berth, len(rows))
	for _, b := range rows {
		out[b.Code] = b
	}
	return out, nil
}

// RightsBerthsByIDs returns berths of the marina by id.
func RightsBerthsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]Berth, error) {
	out := map[string]Berth{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Berth
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, b := range rows {
		out[b.ID] = b
	}
	return out, nil
}
