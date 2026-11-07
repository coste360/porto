package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Mandate compensation (spec 14.3, m5-plan §4.4): the owner shares of mandate_revenues become credit or a payout.
// Written by internal/domain/payment only.

// LockMandate takes a row lock on a management mandate (PostgreSQL; SQLite serialises writers) so that two
// settlements of the same mandate never pay the guaranteed minimum twice.
func LockMandate(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ManagementMandate, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(ManagementMandate)
	q := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// MandateRevenues returns the revenue rows of a mandate with through_on ≤ through (zero = all); unsettledOnly
// keeps the rows not settled yet.
func MandateRevenues(ctx context.Context, db bun.IDB, s MarinaScope, mandateID string, through calendar.Date, unsettledOnly bool) ([]MandateRevenue, error) {
	out := []MandateRevenue{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("mandate_id = ?", mandateID)
	if !through.IsZero() {
		q = q.Where("through_on <= ?", through)
	}
	if unsettledOnly {
		q = q.Where("settled_at IS NULL")
	}
	err := q.OrderExpr("through_on, id").Scan(ctx)
	return out, err
}

// MarkMandateRevenuesSettled sets settled_at on unsettled rows (conditional WHERE settled_at IS NULL) and returns
// how many changed.
func MarkMandateRevenuesSettled(ctx context.Context, db bun.IDB, s MarinaScope, ids []string, now time.Time) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := db.NewUpdate().Model((*MandateRevenue)(nil)).Set("settled_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Where("settled_at IS NULL").Exec(ctx)
	if err != nil {
		return 0, err
	}
	return int(affected(res)), nil
}

// MandateSettledCents returns what a mandate has already paid to its owners: credit payments (source mandate)
// plus payouts that did not fail.
func MandateSettledCents(ctx context.Context, db bun.IDB, s MarinaScope, mandateID string) (int64, error) {
	var credited, paidOut int64
	if err := db.NewSelect().TableExpr("payments").ColumnExpr("COALESCE(SUM(amount_cents), 0)").
		Where("marina_id = ?", s.ID()).Where("mandate_id = ?", mandateID).Where("source = ?", PaymentSourceMandate).
		Scan(ctx, &credited); err != nil {
		return 0, err
	}
	if err := db.NewSelect().TableExpr("refunds").ColumnExpr("COALESCE(SUM(amount_cents), 0)").
		Where("marina_id = ?", s.ID()).Where("mandate_id = ?", mandateID).Where("kind = ?", RefundKindPayout).
		Where("status <> ?", RefundFailed).Scan(ctx, &paidOut); err != nil {
		return 0, err
	}
	return credited + paidOut, nil
}
