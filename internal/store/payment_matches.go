package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Matches of payments to charges (insert-only) and the only writer of charges.paid_amount_cents and of the
// payment_status transitions open ⇄ partially_paid ⇄ paid (m5-plan §4.2, §14.3). Called by
// internal/domain/payment only, with the payment and the charges locked.

// InsertPaymentMatch inserts a match (a reversal carries reverses_match_id and the opposite amount).
func InsertPaymentMatch(ctx context.Context, db bun.IDB, m *PaymentMatch) error {
	_, err := db.NewInsert().Model(m).Exec(ctx)
	return err
}

// GetPaymentMatch returns a match of the marina.
func GetPaymentMatch(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PaymentMatch, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(PaymentMatch)
	if err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// MatchesOfPayments returns the matches of some payments, oldest first.
func MatchesOfPayments(ctx context.Context, db bun.IDB, s MarinaScope, paymentIDs []string) ([]PaymentMatch, error) {
	out := []PaymentMatch{}
	if len(paymentIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("payment_id IN (?)", bun.List(paymentIDs)).
		OrderExpr("created_at, id").Scan(ctx)
	return out, err
}

// MatchesOfCharges returns the matches of some charges, oldest first.
func MatchesOfCharges(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) ([]PaymentMatch, error) {
	out := []PaymentMatch{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("charge_id IN (?)", bun.List(chargeIDs)).
		OrderExpr("created_at, id").Scan(ctx)
	return out, err
}

// ReversalOf returns the reversal of a match (ErrNotFound when not reversed).
func ReversalOf(ctx context.Context, db bun.IDB, s MarinaScope, matchID string) (*PaymentMatch, error) {
	m := new(PaymentMatch)
	if err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("reverses_match_id = ?", matchID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// AddChargePaid moves paid_amount_cents of a positive charge by delta and recomputes its payment status from
// paid + credited (open at 0, paid at the total, partially_paid between), conditional on the charge limits:
// never above the total, never below 0, never on a cancelled charge, and a positive delta only on open or
// partially paid charges. It returns the new status and paid amount; ok false means the limit would be exceeded.
func AddChargePaid(ctx context.Context, db bun.IDB, s MarinaScope, id string, delta int64, now time.Time) (status string, paid int64, ok bool, err error) {
	statuses := []string{ChargeOpen, ChargePartiallyPaid, ChargePaid}
	if delta > 0 {
		statuses = statuses[:2]
	}
	var row struct {
		Status string `bun:"payment_status"`
		Paid   int64  `bun:"paid_amount_cents"`
	}
	err = db.NewRaw(`UPDATE charges SET paid_amount_cents = paid_amount_cents + ?,
		payment_status = CASE
			WHEN paid_amount_cents + ? + credited_cents = total_cents THEN 'paid'
			WHEN paid_amount_cents + ? + credited_cents = 0 THEN 'open'
			ELSE 'partially_paid' END,
		version = version + 1, updated_at = ?
		WHERE marina_id = ? AND id = ? AND total_cents > 0 AND payment_status IN (?)
		AND paid_amount_cents + credited_cents + ? <= total_cents AND paid_amount_cents + ? >= 0
		RETURNING payment_status, paid_amount_cents`,
		delta, delta, delta, UTC(now), s.ID(), id, bun.List(statuses), delta, delta).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return row.Status, row.Paid, true, nil
}

// ChargeVATBucket is the gross, net and VAT of a charge per (rate, nature), from its lines.
type ChargeVATBucket struct {
	ChargeID  string `bun:"charge_id"`
	VATRateBP int64  `bun:"vat_rate_bp"`
	VATNature string `bun:"vat_nature"`
	Net       int64  `bun:"net"`
	VAT       int64  `bun:"vat"`
	Gross     int64  `bun:"gross"`
}

// ChargeVATBuckets returns the VAT buckets of some charges, ordered by charge, rate and nature.
func ChargeVATBuckets(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string][]ChargeVATBucket, error) {
	out := map[string][]ChargeVATBucket{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	var rows []ChargeVATBucket
	err := db.NewSelect().TableExpr("charge_lines").
		ColumnExpr("charge_id, vat_rate_bp, COALESCE(vat_nature, '') AS vat_nature").
		ColumnExpr("SUM(net_cents) AS net, SUM(vat_cents) AS vat, SUM(gross_cents) AS gross").
		Where("marina_id = ?", s.ID()).Where("charge_id IN (?)", bun.List(chargeIDs)).
		GroupExpr("charge_id, vat_rate_bp, COALESCE(vat_nature, '')").
		OrderExpr("charge_id, vat_rate_bp, COALESCE(vat_nature, '')").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ChargeID] = append(out[r.ChargeID], r)
	}
	return out, nil
}

// LedgerChargesByIDs returns charges of the marina without locking them (any order).
func LedgerChargesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Charge, error) {
	out := []Charge{}
	if len(ids) == 0 {
		return out, nil
	}
	for _, id := range ids {
		if !ValidID(id) {
			return nil, ErrNotFound
		}
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// LedgerChargeNumbers returns the numbers of some charges by id.
func LedgerChargeNumbers(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID     string `bun:"id"`
		Number string `bun:"number"`
	}
	if err := db.NewSelect().TableExpr("charges").Column("id", "number").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Number
	}
	return out, nil
}

// LedgerChargeByOrigin returns the live primary charge of an origin (not a correction, not cancelled).
func LedgerChargeByOrigin(ctx context.Context, db bun.IDB, s MarinaScope, originType, originID string) (*Charge, error) {
	c := new(Charge)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("origin_type = ?", originType).
		Where("origin_id = ?", originID).Where("corrects_charge_id IS NULL").Where("payment_status <> ?", ChargeCancelled).
		OrderExpr("created_at DESC, id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// LedgerOpenChargesOfContract returns the open positive charges of a contract or transit (its charge and adjustments),
// oldest due first.
func LedgerOpenChargesOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("total_cents > 0").Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		OrderExpr("due_on, number").Scan(ctx)
	return out, err
}

// LedgerCharge returns one charge of the marina (read without a lock).
func LedgerCharge(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Charge, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Charge)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// LedgerOriginItemID returns the catalogue item configured for an origin key of charge_origin_items.
func LedgerOriginItemID(ctx context.Context, db bun.IDB, s MarinaScope, key string) (string, error) {
	var id string
	err := db.NewSelect().TableExpr("charge_origin_items").Column("item_id").Where("marina_id = ?", s.ID()).
		Where("origin_key = ?", key).Scan(ctx, &id)
	if err != nil {
		return "", notFound(err)
	}
	return id, nil
}

// LedgerFiscalDocuments returns the fiscal documents of some payments, oldest first (payment detail).
func LedgerFiscalDocuments(ctx context.Context, db bun.IDB, s MarinaScope, paymentIDs []string) ([]FiscalDocument, error) {
	out := []FiscalDocument{}
	if len(paymentIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("payment_id IN (?)", bun.List(paymentIDs)).
		OrderExpr("created_at, id").Scan(ctx)
	return out, err
}
