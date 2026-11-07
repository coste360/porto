package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Payments ledger (m5-plan §4.1). Only internal/domain/payment calls the writers of this file: allocated_cents and
// refunded_cents change only through the conditional updates below (INV-50, INV-52), always on a payment locked
// with LockPayment first.

// NextLedgerNumber returns the next number of a billing series (payment, refund, ...) in a year and advances the
// counter of billing_sequences. It runs inside the transaction that inserts the numbered row, so a rolled-back
// transaction rolls the counter back too.
func NextLedgerNumber(ctx context.Context, tx bun.IDB, s MarinaScope, series string, year int) (int64, error) {
	period := fmt.Sprintf("%04d", year)
	seed := &BillingSequence{MarinaID: s.ID(), Series: series, Period: period, NextNumber: 1}
	if _, err := tx.NewInsert().Model(seed).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return 0, fmt.Errorf("billing sequence seed: %w", err)
	}
	var n int64
	err := tx.NewRaw(
		"UPDATE billing_sequences SET next_number = next_number + 1 WHERE marina_id = ? AND series = ? AND period = ? RETURNING next_number - 1",
		s.ID(), series, period,
	).Scan(ctx, &n)
	if err != nil {
		return 0, fmt.Errorf("billing sequence next: %w", err)
	}
	return n, nil
}

// InsertPayment inserts a payment row (allocated and refunded start at 0).
func InsertPayment(ctx context.Context, db bun.IDB, p *Payment) error {
	if len(p.VATSplit) == 0 {
		p.VATSplit = JSON(`[]`)
	}
	_, err := db.NewInsert().Model(p).Exec(ctx)
	return err
}

// GetPayment returns a payment of the marina.
func GetPayment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Payment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(Payment)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// PaymentsByIDs returns the payments of the marina with the given ids (any order).
func PaymentsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Payment, error) {
	out := []Payment{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// PaymentByIdempotencyKey returns the payment recorded with an Idempotency-Key.
func PaymentByIdempotencyKey(ctx context.Context, db bun.IDB, s MarinaScope, key string) (*Payment, error) {
	p := new(Payment)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("idempotency_key = ?", key).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// PaymentByProviderRef returns the payment of a provider reference.
func PaymentByProviderRef(ctx context.Context, db bun.IDB, s MarinaScope, provider, ref string) (*Payment, error) {
	p := new(Payment)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("provider_code = ?", provider).
		Where("provider_ref = ?", ref).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// PaymentFilter selects payments (ListPayments).
type PaymentFilter struct {
	Method, Kind, Source string
	CustomerID           string
	From, To             calendar.Date // received_on, inclusive
	Unallocated          bool
	SessionID            string
	MandateID            string
}

func (f PaymentFilter) apply(q *bun.SelectQuery) *bun.SelectQuery {
	if f.Method != "" {
		q = q.Where("method = ?", f.Method)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if !f.From.IsZero() {
		q = q.Where("received_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("received_on <= ?", f.To)
	}
	if f.Unallocated {
		q = q.Where("allocated_cents + refunded_cents < amount_cents")
	}
	if f.SessionID != "" {
		q = q.Where("cash_session_id = ?", f.SessionID)
	}
	if f.MandateID != "" {
		q = q.Where("mandate_id = ?", f.MandateID)
	}
	return q
}

// ListPayments returns one page of payments, newest first (ids are UUIDv7).
func ListPayments(ctx context.Context, db bun.IDB, s MarinaScope, f PaymentFilter, cursor string, limit int) (Page[Payment], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Payment]{}, err
	}
	limit = ClampLimit(limit)
	rows := []Payment{}
	q := f.apply(db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()))
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Payment]{}, err
	}
	return pageOf(rows, limit, func(p Payment) string { return p.ID }), nil
}

// AllPayments returns every payment matching f (statements, exports, seed checks), oldest first.
func AllPayments(ctx context.Context, db bun.IDB, s MarinaScope, f PaymentFilter) ([]Payment, error) {
	rows := []Payment{}
	err := f.apply(db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())).OrderExpr("received_on, id").Scan(ctx)
	return rows, err
}

// AddPaymentAllocated moves allocated_cents of a payment by delta, conditional on the limits still holding
// (allocated + refunded ≤ amount, allocated ≥ 0). False means the limit would be exceeded: nothing changed.
func AddPaymentAllocated(ctx context.Context, db bun.IDB, s MarinaScope, id string, delta int64, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Payment)(nil)).
		Set("allocated_cents = allocated_cents + ?", delta).Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("allocated_cents + refunded_cents + ? <= amount_cents", delta).
		Where("allocated_cents + ? >= 0", delta).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// AddPaymentRefunded moves refunded_cents of a payment by delta (a failed provider refund releases it with a
// negative delta), conditional on the limits. False means the limit would be exceeded.
func AddPaymentRefunded(ctx context.Context, db bun.IDB, s MarinaScope, id string, delta int64, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Payment)(nil)).
		Set("refunded_cents = refunded_cents + ?", delta).Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("allocated_cents + refunded_cents + ? <= amount_cents", delta).
		Where("refunded_cents + ? >= 0", delta).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetPaymentVATSplit stores the frozen VAT split of what a payment paid.
func SetPaymentVATSplit(ctx context.Context, db bun.IDB, s MarinaScope, id string, split JSON) error {
	_, err := db.NewUpdate().Model((*Payment)(nil)).Set("vat_split = ?", split).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// UnallocatedPayments returns the payments of a customer with unallocated money of the given kinds, oldest first
// (credit application order).
func UnallocatedPayments(ctx context.Context, db bun.IDB, s MarinaScope, customerID string, kinds []string) ([]Payment, error) {
	out := []Payment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Where("kind IN (?)", bun.List(kinds)).Where("allocated_cents + refunded_cents < amount_cents").
		OrderExpr("received_on, number").Scan(ctx)
	return out, err
}

// CustomerCreditTotals is the credit (unallocated payment and credit rows) and the unallocated deposits of a
// customer.
type CustomerCreditTotals struct {
	CustomerID    string `bun:"customer_id"`
	CreditCents   int64  `bun:"credit_cents"`
	DepositsCents int64  `bun:"deposits_cents"`
}

const creditColumns = "customer_id, " +
	"COALESCE(SUM(CASE WHEN kind IN ('payment','credit') THEN amount_cents - allocated_cents - refunded_cents ELSE 0 END), 0) AS credit_cents, " +
	"COALESCE(SUM(CASE WHEN kind = 'deposit' THEN amount_cents - allocated_cents - refunded_cents ELSE 0 END), 0) AS deposits_cents"

// CreditOf returns the credit totals of one customer.
func CreditOf(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) (CustomerCreditTotals, error) {
	rows, err := CreditsOfCustomers(ctx, db, s, []string{customerID})
	if err != nil {
		return CustomerCreditTotals{}, err
	}
	if t, ok := rows[customerID]; ok {
		return t, nil
	}
	return CustomerCreditTotals{CustomerID: customerID}, nil
}

// CreditsOfCustomers returns the credit totals of some customers (nil ids = every customer with a payment).
func CreditsOfCustomers(ctx context.Context, db bun.IDB, s MarinaScope, customerIDs []string) (map[string]CustomerCreditTotals, error) {
	var rows []CustomerCreditTotals
	q := db.NewSelect().TableExpr("payments").ColumnExpr(creditColumns).Where("marina_id = ?", s.ID()).
		Where("customer_id IS NOT NULL").GroupExpr("customer_id")
	if customerIDs != nil {
		if len(customerIDs) == 0 {
			return map[string]CustomerCreditTotals{}, nil
		}
		q = q.Where("customer_id IN (?)", bun.List(customerIDs))
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]CustomerCreditTotals, len(rows))
	for _, r := range rows {
		out[r.CustomerID] = r
	}
	return out, nil
}

// CustomersWithCredit returns one page of customers with credit or unallocated deposits, by customer id.
func CustomersWithCredit(ctx context.Context, db bun.IDB, s MarinaScope, cursor string, limit int) (Page[CustomerCreditTotals], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[CustomerCreditTotals]{}, err
	}
	limit = ClampLimit(limit)
	rows := []CustomerCreditTotals{}
	q := db.NewSelect().TableExpr("payments").ColumnExpr(creditColumns).Where("marina_id = ?", s.ID()).
		Where("customer_id IS NOT NULL").Where("allocated_cents + refunded_cents < amount_cents")
	if after != "" {
		q = q.Where("customer_id > ?", after)
	}
	if err := q.GroupExpr("customer_id").OrderExpr("customer_id").Limit(int64(limit+1)).Scan(ctx, &rows); err != nil {
		return Page[CustomerCreditTotals]{}, err
	}
	return pageOf(rows, limit, func(r CustomerCreditTotals) string { return r.CustomerID }), nil
}

// Instalment follow-up of the charge payment status (m5-plan §8.2). The contract and rights services call these
// through their settlement entry points only (INV-32); the row must be locked by the caller (contract or right).

// FollowInstallment sets the status of an instalment to partially_paid or issued (unsettle) from one of from,
// clearing the settlement columns. False when the row was not in from.
func FollowInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string, from []string, to string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Installment)(nil)).Set("status = ?", to).
		Set("settlement_source = NULL").Set("settlement_method = NULL").Set("settlement_ref_enc = NULL").
		Set("settled_on = NULL").Set("settled_at = NULL").Set("settled_by_user_id = NULL").
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status IN (?)", bun.List(from)).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// PayInstallment marks an instalment paid from issued, scheduled or partially_paid (settlement through a payment
// or the historical manual path), conditional on the status and the expected version (0 = any).
func PayInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int, st Settlement) (bool, error) {
	q := db.NewUpdate().Model((*Installment)(nil)).
		Set("status = ?", InstallmentPaid).Set("settlement_source = ?", st.Source).Set("settlement_method = ?", st.Method).
		Set("settlement_ref_enc = ?", installmentNull(st.RefEnc)).Set("settled_on = ?", st.SettledOn).Set("settled_at = ?", UTC(st.At)).
		Set("settled_by_user_id = ?", installmentNull(st.UserID)).Set("updated_at = ?", UTC(st.At)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status IN (?)", bun.List([]string{InstallmentIssued, InstallmentScheduled, InstallmentPartiallyPaid}))
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// FollowHolderInstallment sets the status of a holder instalment to partially_paid or issued from one of from,
// clearing the settlement columns. False when the row was not in from.
func FollowHolderInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string, from []string, to string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*HolderInstallment)(nil)).Set("status = ?", to).
		Set("settlement_source = NULL").Set("settlement_method = NULL").Set("settlement_ref_enc = NULL").
		Set("settled_on = NULL").Set("settled_at = NULL").Set("settled_by_user_id = NULL").
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status IN (?)", bun.List(from)).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// InstallmentByCharge returns the instalment whose charge is chargeID.
func InstallmentByCharge(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) (*Installment, error) {
	r := new(Installment)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("charge_id = ?", chargeID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// HolderInstallmentByCharge returns the holder instalment whose charge is chargeID.
func HolderInstallmentByCharge(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) (*HolderInstallment, error) {
	r := new(HolderInstallment)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("charge_id = ?", chargeID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// AddReleaseCredit adds a release credit to a resold release of a contract of the customer (spec 10.6, m5-plan
// §4.4). False when the release does not exist, is not resold or belongs to another customer.
func AddReleaseCredit(ctx context.Context, db bun.IDB, s MarinaScope, releaseID, customerID string, cents int64, now time.Time) (bool, error) {
	if !ValidID(releaseID) || !ValidID(customerID) {
		return false, nil
	}
	res, err := db.NewRaw(`UPDATE releases SET credit_cents = credit_cents + ?, version = version + 1, updated_at = ?
		WHERE marina_id = ? AND id = ? AND resold_allocation_id IS NOT NULL AND EXISTS (SELECT 1 FROM contracts AS c
		WHERE c.id = releases.contract_id AND c.marina_id = releases.marina_id AND (c.customer_id = ? OR c.billing_customer_id = ?))`,
		cents, UTC(now), s.ID(), releaseID, customerID, customerID).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// LedgerBillingSettings returns the billing settings row of the marina (ErrNotFound before it is created).
func LedgerBillingSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*BillingSettings, error) {
	b := new(BillingSettings)
	if err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return b, nil
}
