package store

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the opening balance imports (spec 11.6; m5-plan §3.8).

// InsertChargeImport inserts an import run.
func InsertChargeImport(ctx context.Context, db bun.IDB, r *ChargeImport) error {
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// LockChargeImport locks an import run of the marina and returns it.
func LockChargeImport(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ChargeImport, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(ChargeImport)
	q := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// MarkChargeImportUndone marks an applied import undone at version expected; false when it moved.
func MarkChargeImportUndone(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*ChargeImport)(nil)).Set("status = 'undone'").Set("undone_at = ?", UTC(now)).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = 'applied'").Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// ListChargeImports returns a page of import runs, newest first.
func ListChargeImports(ctx context.Context, db bun.IDB, s MarinaScope, cursor string, limit int) (Page[ChargeImport], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ChargeImport]{}, err
	}
	var rows []ChargeImport
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ChargeImport]{}, err
	}
	return pageOf(rows, limit, func(r ChargeImport) string { return r.ID }), nil
}

// ChargesOfImport returns the opening balance charges of an import run.
func ChargesOfImport(ctx context.Context, db bun.IDB, s MarinaScope, importID string) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("origin_type = ?", ChargeOriginOpeningBalance).
		Where("origin_id = ?", importID).OrderExpr("number").Scan(ctx)
	return out, err
}

// CustomerKeys maps a customer key (fiscal code, VAT number or id) to the ids of the live customers of the marina
// having it (more than one id = ambiguous). Keys are compared upper case.
func CustomerKeys(ctx context.Context, db bun.IDB, s MarinaScope, field string, keys []string) (map[string][]string, error) {
	out := map[string][]string{}
	col := map[string]string{"fiscal_code": "fiscal_code", "vat_number": "vat_number", "customer_code": "id"}[field]
	if col == "" || len(keys) == 0 {
		return out, nil
	}
	norm := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if col != "id" {
			k = strings.ToUpper(k)
		} else if !ValidID(k) {
			continue
		}
		if k != "" {
			norm = append(norm, k)
		}
	}
	for start := 0; start < len(norm); start += 500 {
		end := min(start+500, len(norm))
		var rows []struct {
			ID  string `bun:"id"`
			Key string `bun:"key"`
		}
		expr := "UPPER(" + col + ")"
		if col == "id" {
			expr = "id"
		}
		if err := db.NewSelect().Table("customers").ColumnExpr("id, "+expr+" AS key").Where("marina_id = ?", s.ID()).
			Where("merged_into_id IS NULL").Where("anonymized_at IS NULL").
			Where(expr+" IN (?)", bun.List(norm[start:end])).OrderExpr("id").Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.Key] = append(out[r.Key], r.ID)
		}
	}
	return out, nil
}
