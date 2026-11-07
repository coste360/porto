package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Refunds and payouts (m5-plan §4.3). Written by internal/domain/payment only; the amount reserved on the payment
// (refunded_cents) moves in the same transaction through AddPaymentRefunded.

// InsertRefund inserts a refund or payout.
func InsertRefund(ctx context.Context, db bun.IDB, r *Refund) error {
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// GetRefund returns a refund of the marina.
func GetRefund(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Refund, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(Refund)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// LockRefund takes a row lock on a refund (PostgreSQL; SQLite serialises writers) and returns it.
func LockRefund(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Refund, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(Refund)
	q := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// RefundByIdempotencyKey returns the refund created with an Idempotency-Key.
func RefundByIdempotencyKey(ctx context.Context, db bun.IDB, s MarinaScope, key string) (*Refund, error) {
	r := new(Refund)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("idempotency_key = ?", key).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// RefundFilter selects refunds.
type RefundFilter struct {
	PaymentID  string
	Status     string
	Kind       string
	CustomerID string
	MandateID  string
	SessionID  string
	From, To   calendar.Date // business_date, inclusive
}

func (f RefundFilter) apply(q *bun.SelectQuery) *bun.SelectQuery {
	if f.PaymentID != "" {
		q = q.Where("payment_id = ?", f.PaymentID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if f.MandateID != "" {
		q = q.Where("mandate_id = ?", f.MandateID)
	}
	if f.SessionID != "" {
		q = q.Where("cash_session_id = ?", f.SessionID)
	}
	if !f.From.IsZero() {
		q = q.Where("business_date >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("business_date <= ?", f.To)
	}
	return q
}

// ListRefunds returns one page of refunds, newest first.
func ListRefunds(ctx context.Context, db bun.IDB, s MarinaScope, f RefundFilter, cursor string, limit int) (Page[Refund], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Refund]{}, err
	}
	limit = ClampLimit(limit)
	rows := []Refund{}
	q := f.apply(db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()))
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Refund]{}, err
	}
	return pageOf(rows, limit, func(r Refund) string { return r.ID }), nil
}

// AllRefunds returns every refund matching f, oldest first.
func AllRefunds(ctx context.Context, db bun.IDB, s MarinaScope, f RefundFilter) ([]Refund, error) {
	rows := []Refund{}
	err := f.apply(db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())).OrderExpr("business_date, id").Scan(ctx)
	return rows, err
}

// RefundsOfPayments returns the refunds of some payments, oldest first.
func RefundsOfPayments(ctx context.Context, db bun.IDB, s MarinaScope, paymentIDs []string) ([]Refund, error) {
	out := []Refund{}
	if len(paymentIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("payment_id IN (?)", bun.List(paymentIDs)).
		OrderExpr("created_at, id").Scan(ctx)
	return out, err
}

// FinishRefund moves a pending refund to succeeded or failed (conditional on pending). providerRef is stored when
// set and the row has none. False when the refund was not pending any more.
func FinishRefund(ctx context.Context, db bun.IDB, s MarinaScope, id, status, providerRef, failureCode string, now time.Time) (bool, error) {
	q := db.NewUpdate().Model((*Refund)(nil)).Set("status = ?", status).Set("completed_at = ?", UTC(now)).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", RefundPending)
	if providerRef != "" {
		q = q.Set("provider_ref = COALESCE(provider_ref, ?)", providerRef)
	}
	if failureCode != "" {
		q = q.Set("failure_code = ?", failureCode)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}
