package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of CSV import runs (m4-plan §3.7, §5.3) and the lookups of the customers/vessels and deadlines imports.

// Import run statuses.
const (
	ImportDryRun  = "dry_run"
	ImportApplied = "applied"
	ImportUndone  = "undone"
	ImportFailed  = "failed"
)

// InsertImportRun inserts a run.
func InsertImportRun(ctx context.Context, db bun.IDB, r *ImportRun) error {
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// GetImportRun returns a run of a marina.
func GetImportRun(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ImportRun, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(ImportRun)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// ListImportRuns returns the runs of a marina (entity "" = all), newest first.
func ListImportRuns(ctx context.Context, db bun.IDB, s MarinaScope, entity, cursor string, limit int) (Page[ImportRun], error) {
	limit = ClampLimit(limit)
	var rows []ImportRun
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).OrderExpr("id DESC").Limit(int64(limit + 1))
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[ImportRun]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.Scan(ctx); err != nil {
		return Page[ImportRun]{}, err
	}
	return pageOf(rows, limit, func(r ImportRun) string { return r.ID }), nil
}

// MarkImportUndone moves an applied run to undone when its version equals ifMatch; false otherwise.
func MarkImportUndone(ctx context.Context, db bun.IDB, s MarinaScope, id string, ifMatch int, at time.Time) (bool, error) {
	q := db.NewUpdate().Table("import_runs").Set("status = ?", ImportUndone).Set("undone_at = ?", UTC(at)).
		Set("version = version + 1").Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status = ?", ImportApplied)
	if ifMatch > 0 {
		q = q.Where("version = ?", ifMatch)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// InsertImportRunItems records what an applied run created.
func InsertImportRunItems(ctx context.Context, db bun.IDB, items []ImportRunItem) error {
	if len(items) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&items).Exec(ctx)
	return err
}

// ImportRunItems returns the records created by a run, in creation order.
func ImportRunItems(ctx context.Context, db bun.IDB, s MarinaScope, runID string) ([]ImportRunItem, error) {
	var out []ImportRunItem
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("run_id = ?", runID).OrderExpr("id").Scan(ctx)
	return out, err
}

// CountReferences counts the rows of table whose column equals id in a marina (undo guards).
func CountReferences(ctx context.Context, db bun.IDB, s MarinaScope, table, column, id string) (int, error) {
	return toInt(db.NewSelect().Table(table).Where("marina_id = ?", s.ID()).Where("? = ?", bun.Ident(column), id).Count(ctx))
}

// DeleteRow deletes one row of a marina by id when its version equals version (0 = no check); false otherwise.
func DeleteRow(ctx context.Context, db bun.IDB, s MarinaScope, table, id string, version int) (bool, error) {
	q := db.NewDelete().Table(table).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if version > 0 {
		q = q.Where("version = ?", version)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}
