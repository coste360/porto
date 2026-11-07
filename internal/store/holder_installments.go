package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the M4 holder instalments (m4-plan §4.6, §15 D19): fees, common charges and transfer fees due by
// the holders of rights. Paid only through rights.SettleHolderInstallment (INV-32).

// HolderInstallmentFilter narrows ListHolderInstallments. Overdue = issued and due before Today.
type HolderInstallmentFilter struct {
	From, To   calendar.Date
	Status     string // a status or "overdue"
	Today      calendar.Date
	CustomerID string
	RightID    string
	SourceType string
	RunID      string
}

func (f HolderInstallmentFilter) apply(q *bun.SelectQuery) *bun.SelectQuery {
	if !f.From.IsZero() {
		q = q.Where("due_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("due_on <= ?", f.To)
	}
	switch f.Status {
	case "":
	case "overdue":
		q = q.Where("status IN ('issued','partially_paid') AND due_on < ?", f.Today)
	default:
		q = q.Where("status = ?", f.Status)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if f.RightID != "" {
		q = q.Where("right_id = ?", f.RightID)
	}
	if f.SourceType != "" {
		q = q.Where("source_type = ?", f.SourceType)
	}
	if f.RunID != "" {
		q = q.Where("run_id = ?", f.RunID)
	}
	return q
}

// InsertHolderInstallments inserts holder instalments.
func InsertHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, rows []HolderInstallment) error {
	for start := 0; start < len(rows); start += 500 {
		chunk := rows[start:min(start+500, len(rows))]
		for i := range chunk {
			chunk[i].MarinaID = s.ID()
		}
		if _, err := db.NewInsert().Model(&chunk).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
	}
	return nil
}

// GetHolderInstallment returns a holder instalment of the marina.
func GetHolderInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*HolderInstallment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	h := new(HolderInstallment)
	if err := db.NewSelect().Model(h).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return h, nil
}

// ListHolderInstallments returns a page of holder instalments by due date and id.
func ListHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, f HolderInstallmentFilter, cursor string, limit int) (Page[HolderInstallment], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[HolderInstallment]{}, err
	}
	limit = ClampLimit(limit)
	var rows []HolderInstallment
	q := f.apply(db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()))
	if after != "" {
		last, err := GetHolderInstallment(ctx, db, s, after)
		if err != nil {
			return Page[HolderInstallment]{}, ErrInvalidCursor
		}
		q = q.Where("(due_on > ? OR (due_on = ? AND id > ?))", last.DueOn, last.DueOn, last.ID)
	}
	if err := q.OrderExpr("due_on, id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[HolderInstallment]{}, err
	}
	return pageOf(rows, limit, func(r HolderInstallment) string { return r.ID }), nil
}

// AllHolderInstallments returns every holder instalment matching f (CSV exports, statements, checks).
func AllHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, f HolderInstallmentFilter) ([]HolderInstallment, error) {
	out := []HolderInstallment{}
	err := f.apply(db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())).OrderExpr("due_on, id").Scan(ctx)
	return out, err
}

// HolderInstallmentsOfSources returns the instalments of sources (fee lines, runs, transfers).
func HolderInstallmentsOfSources(ctx context.Context, db bun.IDB, s MarinaScope, sourceIDs []string) ([]HolderInstallment, error) {
	out := []HolderInstallment{}
	ids := validIDs(sourceIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("source_id IN (?)", bun.List(ids)).OrderExpr("source_id, customer_id, seq").Scan(ctx)
	return out, err
}

// SettleHolderInstallmentRow marks an issued instalment paid with its settlement, when its version equals
// expected and it is still unpaid (conditional update: concurrent settles pay it once). The row must be scheduled,
// issued or partially paid; anything else is ErrConflict.
func SettleHolderInstallmentRow(ctx context.Context, db bun.IDB, s MarinaScope, h *HolderInstallment, expected int) error {
	res, err := db.NewUpdate().Model(h).Column("status", "settlement_source", "settlement_method", "settlement_ref_enc",
		"settled_on", "settled_at", "settled_by_user_id", "updated_at").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", h.ID).Where("version = ?", expected).
		Where("status IN ('scheduled','issued','partially_paid')").Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		cur, err := GetHolderInstallment(ctx, db, s, h.ID)
		if err != nil {
			return err
		}
		if cur.Version != expected {
			return ErrVersionMismatch
		}
		return ErrConflict
	}
	h.Version = expected + 1
	return nil
}

// DueHolderInstallments returns up to limit scheduled instalments with issue_on ≤ today.
func DueHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date, limit int) ([]HolderInstallment, error) {
	out := []HolderInstallment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = 'scheduled'").Where("issue_on <= ?", today).
		OrderExpr("issue_on, id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// IssueHolderInstallment marks a scheduled instalment issued (conditional: issued once).
func IssueHolderInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*HolderInstallment)(nil)).Set("status = 'issued'").Set("issued_at = ?", at).
		Set("updated_at = ?", at).Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status = 'scheduled'").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// CancelHolderInstallments cancels the scheduled instalments among ids and returns how many changed.
func CancelHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, ids []string, at time.Time) (int, error) {
	ids = validIDs(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.NewUpdate().Model((*HolderInstallment)(nil)).Set("status = 'cancelled'").Set("cancelled_at = ?", at).
		Set("updated_at = ?", at).Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).
		Where("status = 'scheduled'").Exec(ctx)
	if err != nil {
		return 0, err
	}
	return int(affected(res)), nil
}

// MaxHolderSeq returns the highest seq of a (source, customer) pair (0 when none).
func MaxHolderSeq(ctx context.Context, db bun.IDB, s MarinaScope, sourceType, sourceID, customerID string) (int, error) {
	var n int
	err := db.NewSelect().Model((*HolderInstallment)(nil)).ColumnExpr("coalesce(max(seq), 0)").Where("marina_id = ?", s.ID()).
		Where("source_type = ?", sourceType).Where("source_id = ?", sourceID).Where("customer_id = ?", customerID).Scan(ctx, &n)
	return n, err
}

// UnpaidOverdueHolderInstallments returns the issued instalments due before today of some customers.
func UnpaidOverdueHolderInstallments(ctx context.Context, db bun.IDB, s MarinaScope, customerIDs []string, today calendar.Date) ([]HolderInstallment, error) {
	out := []HolderInstallment{}
	ids := validIDs(customerIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(ids)).
		Where("status IN ('issued','partially_paid')").Where("due_on < ?", today).Where("amount_cents > 0").OrderExpr("due_on, id").Scan(ctx)
	return out, err
}

// RightsWithUnpaidFees returns the ids of rights (among rightIDs, nil = all) with an issued holder instalment due
// before today.
func RightsWithUnpaidFees(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date) (map[string]bool, error) {
	var ids []string
	err := db.NewSelect().Model((*HolderInstallment)(nil)).ColumnExpr("DISTINCT right_id").Where("marina_id = ?", s.ID()).
		Where("right_id IS NOT NULL").Where("status IN ('issued','partially_paid')").Where("due_on < ?", today).
		Where("amount_cents > 0").Scan(ctx, &ids)
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}
