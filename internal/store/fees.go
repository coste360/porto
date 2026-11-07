package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Queries of the M4 management fee tables and runs (m4-plan §4.4). Lines of an issued run are frozen by the
// triggers of 00008_rights (INV-44); a write on them is reported as ErrImmutable through MapRightsImmutable.

// MapRightsImmutable wraps an INV-41/INV-44 trigger error as ErrImmutable and returns other errors unchanged.
func MapRightsImmutable(err error) error {
	if err != nil && (IsTriggerViolation(err, MillesimalImmutableMessage) || IsTriggerViolation(err, AllocationsImmutableMessage)) {
		return ErrImmutable
	}
	return err
}

// InsertFeeTable inserts a fee table with its rows.
func InsertFeeTable(ctx context.Context, db bun.IDB, s MarinaScope, t *ManagementFeeTable, rows []ManagementFeeTableRow) error {
	t.MarinaID = s.ID()
	if _, err := db.NewInsert().Model(t).Exec(ctx); err != nil {
		return err
	}
	return ReplaceFeeTableRows(ctx, db, s, t.ID, rows)
}

// ReplaceFeeTableRows replaces the rows of a fee table.
func ReplaceFeeTableRows(ctx context.Context, db bun.IDB, s MarinaScope, tableID string, rows []ManagementFeeTableRow) error {
	if _, err := db.NewDelete().Model((*ManagementFeeTableRow)(nil)).Where("marina_id = ?", s.ID()).Where("table_id = ?", tableID).Exec(ctx); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].MarinaID, rows[i].TableID = s.ID(), tableID
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// GetFeeTable returns a fee table of the marina.
func GetFeeTable(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ManagementFeeTable, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(ManagementFeeTable)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateFeeTable writes name, istat and status when the version equals expected.
func UpdateFeeTable(ctx context.Context, db bun.IDB, s MarinaScope, t *ManagementFeeTable, expected int) error {
	if err := versionedUpdate(ctx, db, s, t, "management_fee_tables", t.ID, expected, "name", "istat_bp", "status", "updated_at"); err != nil {
		return err
	}
	t.Version = expected + 1
	return nil
}

// DeleteFeeTable deletes a fee table when its version equals expected; a table used by a run is ErrConflict.
func DeleteFeeTable(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	n, err := db.NewSelect().Model((*ManagementFeeRun)(nil)).Where("marina_id = ?", s.ID()).Where("table_id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	res, err := db.NewDelete().Model((*ManagementFeeTable)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetFeeTable(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListFeeTables returns the fee tables of a marina (year 0 = every year), newest year first.
func ListFeeTables(ctx context.Context, db bun.IDB, s MarinaScope, year int) ([]ManagementFeeTable, error) {
	out := []ManagementFeeTable{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if year != 0 {
		q = q.Where("year = ?", year)
	}
	err := q.OrderExpr("year DESC, name, id").Scan(ctx)
	return out, err
}

// RowsOfFeeTables returns the rows of fee tables by table and sort.
func RowsOfFeeTables(ctx context.Context, db bun.IDB, s MarinaScope, tableIDs []string) (map[string][]ManagementFeeTableRow, error) {
	out := map[string][]ManagementFeeTableRow{}
	ids := validIDs(tableIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []ManagementFeeTableRow
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("table_id IN (?)", bun.List(ids)).OrderExpr("table_id, sort, id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TableID] = append(out[r.TableID], r)
	}
	return out, nil
}

// ---- runs ----

// InsertFeeRun inserts a fee run.
func InsertFeeRun(ctx context.Context, db bun.IDB, s MarinaScope, r *ManagementFeeRun) error {
	r.MarinaID = s.ID()
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// GetFeeRun returns a fee run of the marina.
func GetFeeRun(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ManagementFeeRun, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(ManagementFeeRun)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// UpdateFeeRun writes status, totals and instants when the version equals expected. The partial unique index
// management_fee_runs_issued_uidx makes a second issued run of the same year and table ErrConflict.
func UpdateFeeRun(ctx context.Context, db bun.IDB, s MarinaScope, r *ManagementFeeRun, expected int) error {
	if err := versionedUpdate(ctx, db, s, r, "management_fee_runs", r.ID, expected, "status", "totals", "simulated_at", "issued_at", "updated_at"); err != nil {
		return err
	}
	r.Version = expected + 1
	return nil
}

// ListFeeRuns returns the fee runs of a marina (year 0 = every year), newest first.
func ListFeeRuns(ctx context.Context, db bun.IDB, s MarinaScope, year int) ([]ManagementFeeRun, error) {
	out := []ManagementFeeRun{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if year != 0 {
		q = q.Where("year = ?", year)
	}
	err := q.OrderExpr("year DESC, id DESC").Scan(ctx)
	return out, err
}

// IssuedFeeRun returns the issued run of a year and table, or ErrNotFound.
func IssuedFeeRun(ctx context.Context, db bun.IDB, s MarinaScope, year int, tableID string) (*ManagementFeeRun, error) {
	r := new(ManagementFeeRun)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("year = ?", year).Where("status = 'issued'").
		Where("table_id = ?", tableID).Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// LatestIssuedFeeRun returns the newest issued run of a year (any table), or ErrNotFound.
func LatestIssuedFeeRun(ctx context.Context, db bun.IDB, s MarinaScope, year int) (*ManagementFeeRun, error) {
	r := new(ManagementFeeRun)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("year = ?", year).Where("status = 'issued'").
		OrderExpr("issued_at DESC, id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// ReplaceFeeLines replaces the lines of a run (simulation re-run).
func ReplaceFeeLines(ctx context.Context, db bun.IDB, s MarinaScope, runID string, lines []ManagementFeeLine) error {
	if _, err := db.NewDelete().Model((*ManagementFeeLine)(nil)).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID).Exec(ctx); err != nil {
		return MapRightsImmutable(err)
	}
	for start := 0; start < len(lines); start += 500 {
		end := min(start+500, len(lines))
		chunk := lines[start:end]
		for i := range chunk {
			chunk[i].MarinaID, chunk[i].RunID = s.ID(), runID
		}
		if _, err := db.NewInsert().Model(&chunk).Exec(ctx); err != nil {
			return MapRightsImmutable(err)
		}
	}
	return nil
}

// FeeLinesOfRun returns every line of a run by id.
func FeeLinesOfRun(ctx context.Context, db bun.IDB, s MarinaScope, runID string) ([]ManagementFeeLine, error) {
	out := []ManagementFeeLine{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID).OrderExpr("id").Scan(ctx)
	return out, err
}

// PageFeeLines returns a page of the lines of a run by id.
func PageFeeLines(ctx context.Context, db bun.IDB, s MarinaScope, runID, cursor string, limit int) (Page[ManagementFeeLine], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ManagementFeeLine]{}, err
	}
	limit = ClampLimit(limit)
	var rows []ManagementFeeLine
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID)
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ManagementFeeLine]{}, err
	}
	return pageOf(rows, limit, func(r ManagementFeeLine) string { return r.ID }), nil
}

// GetFeeLine returns one fee line of the marina.
func GetFeeLine(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ManagementFeeLine, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	l := new(ManagementFeeLine)
	if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return l, nil
}

// LockFeeRun locks a fee run row (PostgreSQL): issue, simulate and discard of one run are serialised.
func LockFeeRun(ctx context.Context, tx bun.IDB, s MarinaScope, id string) error {
	return lockRow(ctx, tx, "management_fee_runs", s, id)
}
