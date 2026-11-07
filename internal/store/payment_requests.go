package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Payment requests and the charge reads of the collection service (m5-plan §5.5). The public token is stored as
// its SHA-256 only (INV-75 rules). Owned by CASH-DRIVERS.

// Payment request statuses.
const (
	RequestOpen      = "open"
	RequestPaid      = "paid"
	RequestCancelled = "cancelled"
	RequestExpired   = "expired"
)

// InsertPaymentRequest inserts a request with its charges.
func InsertPaymentRequest(ctx context.Context, tx bun.IDB, s MarinaScope, r *PaymentRequest, charges []PaymentRequestCharge) error {
	if _, err := tx.NewInsert().Model(r).Exec(ctx); err != nil {
		return err
	}
	for i := range charges {
		c := &charges[i]
		c.ID, c.MarinaID, c.RequestID, c.CreatedAt = NewID(), s.ID(), r.ID, r.CreatedAt
		if _, err := tx.NewInsert().Model(c).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// GetPaymentRequest returns a request of the marina.
func GetPaymentRequest(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PaymentRequest, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(PaymentRequest)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return r, notFound(err)
}

// LockPaymentRequest locks a request (FOR UPDATE on PostgreSQL).
func LockPaymentRequest(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PaymentRequest, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(PaymentRequest)
	q := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	return r, notFound(q.Scan(ctx))
}

// PaymentRequestByTokenHash finds a request by token hash with the slug of its marina (public page).
func PaymentRequestByTokenHash(ctx context.Context, db bun.IDB, hash string) (*PaymentRequest, string, error) {
	r := new(PaymentRequest)
	if err := db.NewSelect().Model(r).Where("token_hash = ?", hash).Scan(ctx); err != nil {
		return nil, "", notFound(err)
	}
	var slug []string
	if err := db.NewSelect().TableExpr("marinas").Column("slug").Where("id = ?", r.MarinaID).
		Where("archived_at IS NULL").Scan(ctx, &slug); err != nil {
		return nil, "", err
	}
	if len(slug) == 0 {
		return nil, "", ErrNotFound
	}
	return r, slug[0], nil
}

// PaymentRequestFilter narrows ListPaymentRequests.
type PaymentRequestFilter struct {
	Status     string
	CustomerID string
}

// ListPaymentRequests returns requests newest first (cursor = last id).
func ListPaymentRequests(ctx context.Context, db bun.IDB, s MarinaScope, f PaymentRequestFilter, cursor string, limit int) (Page[PaymentRequest], error) {
	limit = ClampLimit(limit)
	rows := []PaymentRequest{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[PaymentRequest]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[PaymentRequest]{}, err
	}
	return pageOf(rows, limit, func(r PaymentRequest) string { return r.ID }), nil
}

// RequestChargeIDs returns the charge ids of requests (request id → charge ids, in insertion order).
func RequestChargeIDs(ctx context.Context, db bun.IDB, s MarinaScope, requestIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(requestIDs) == 0 {
		return out, nil
	}
	var rows []PaymentRequestCharge
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("request_id IN (?)", bun.List(requestIDs)).
		OrderExpr("id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RequestID] = append(out[r.RequestID], r.ChargeID)
	}
	return out, nil
}

// UpdateRequestSent records a send (and an optional token rotation) at version expected (0 = no version check).
func UpdateRequestSent(ctx context.Context, tx bun.IDB, s MarinaScope, r *PaymentRequest, expected int, now time.Time) error {
	now = UTC(now)
	q := tx.NewUpdate().Model((*PaymentRequest)(nil)).Set("sent_at = ?", now).Set("sent_count = sent_count + 1").
		Set("amount_cents = ?", r.AmountCents).Set("token_hash = ?", r.TokenHash).
		Set("last_message_id = ?", nullString(r.LastMessageID)).Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Where("status = ?", RequestOpen)
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	r.SentAt, r.SentCount, r.Version, r.UpdatedAt = now, r.SentCount+1, r.Version+1, now
	return nil
}

// SetRequestAmount refreshes the amount of an open request (Σ residuals).
func SetRequestAmount(ctx context.Context, tx bun.IDB, s MarinaScope, id string, amount int64, expiresAt, now time.Time) error {
	q := tx.NewUpdate().Model((*PaymentRequest)(nil)).Set("amount_cents = ?", amount).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", RequestOpen)
	if !expiresAt.IsZero() {
		q = q.Set("expires_at = ?", UTC(expiresAt))
	}
	_, err := q.Exec(ctx)
	return err
}

// SetRequestStatus moves a request from open to status (paid, cancelled, expired) at version expected (0 = any).
func SetRequestStatus(ctx context.Context, tx bun.IDB, s MarinaScope, id, status string, expected int, now time.Time) (bool, error) {
	now = UTC(now)
	q := tx.NewUpdate().Model((*PaymentRequest)(nil)).Set("status = ?", status).Set("version = version + 1").
		Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", RequestOpen)
	switch status {
	case RequestPaid:
		q = q.Set("paid_at = ?", now)
	case RequestCancelled:
		q = q.Set("cancelled_at = ?", now)
	}
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// ExpiredOpenRequests returns open requests whose expiry passed.
func ExpiredOpenRequests(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]PaymentRequest, error) {
	out := []PaymentRequest{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", RequestOpen).
		Where("expires_at IS NOT NULL").Where("expires_at < ?", UTC(now)).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// OpenRequestsOfCharges returns the open requests that include any of the charges.
func OpenRequestsOfCharges(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) ([]PaymentRequest, error) {
	out := []PaymentRequest{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", RequestOpen).
		Where("id IN (SELECT request_id FROM payment_request_charges WHERE marina_id = ? AND charge_id IN (?))", s.ID(), bun.List(chargeIDs)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

// CollectChargesByIDs returns charges of the marina without locking, in the order of ids (unknown ids are skipped).
func CollectChargesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Charge, error) {
	var want []string
	for _, id := range ids {
		if ValidID(id) {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return []Charge{}, nil
	}
	rows := []Charge{}
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(want)).Scan(ctx); err != nil {
		return nil, err
	}
	byID := map[string]Charge{}
	for _, c := range rows {
		byID[c.ID] = c
	}
	out := make([]Charge, 0, len(rows))
	seen := map[string]bool{}
	for _, id := range want {
		if c, ok := byID[id]; ok && !seen[id] {
			out = append(out, c)
			seen[id] = true
		}
	}
	return out, nil
}

// CollectChargeLines returns the lines of a charge in sequence order.
func CollectChargeLines(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) ([]ChargeLine, error) {
	out := []ChargeLine{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("charge_id = ?", chargeID).OrderExpr("seq").Scan(ctx)
	return out, err
}

// OpenChargesForReminders returns the open positive charges with a customer due on or before upTo.
func OpenChargesForReminders(ctx context.Context, db bun.IDB, s MarinaScope, upTo calendar.Date) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("total_cents > 0").Where("customer_id IS NOT NULL").Where("due_on <= ?", upTo).
		OrderExpr("customer_id, due_on, number").Scan(ctx)
	return out, err
}

// OverdueByContract sums, per mooring or charter contract, the residual of its open charges due before cutoff
// (arrears policy). Charges with a charge-level dunning exclusion active on today (disputed, payment plan, other)
// do not count. Contracts without such charges are absent.
func OverdueByContract(ctx context.Context, db bun.IDB, s MarinaScope, cutoff, today calendar.Date) (map[string]int64, error) {
	var rows []struct {
		ContractID string `bun:"contract_id"`
		Sum        int64  `bun:"sum"`
	}
	err := db.NewSelect().TableExpr("charges AS ch").Join("JOIN contracts AS c ON c.id = ch.contract_id AND c.marina_id = ch.marina_id").
		ColumnExpr("ch.contract_id, SUM(ch.total_cents - ch.paid_amount_cents - ch.credited_cents) AS sum").
		Where("ch.marina_id = ?", s.ID()).Where("ch.payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("ch.total_cents > 0").Where("ch.due_on < ?", cutoff).
		Where("c.kind IN (?)", bun.List([]string{ContractKindMooring, ContractKindCharter})).
		Where("NOT EXISTS (SELECT 1 FROM dunning_exclusions AS x WHERE x.marina_id = ch.marina_id AND x.charge_id = ch.id "+
			"AND x.removed_at IS NULL AND (x.until_on IS NULL OR x.until_on >= ?))", today).
		GroupExpr("ch.contract_id").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.ContractID] = r.Sum
	}
	return out, nil
}

// LockOverdueOfContract locks (ascending id, FOR UPDATE on PostgreSQL) the open charges of one contract that
// OverdueByContract counts with the same cutoff and charge-level exclusions, and returns their outstanding sum.
// It is the re-check under lock of the arrears policy: a charge being paid concurrently is waited for and, once
// paid, no longer matches the WHERE re-evaluated by PostgreSQL READ COMMITTED. Lock order: the caller holds the
// contract lock (contract → charges, as the payment paths).
func LockOverdueOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string, cutoff, today calendar.Date) (int64, error) {
	var rows []struct {
		ID          string `bun:"id"`
		Outstanding int64  `bun:"outstanding"`
	}
	q := db.NewSelect().TableExpr("charges AS ch").
		ColumnExpr("ch.id, ch.total_cents - ch.paid_amount_cents - ch.credited_cents AS outstanding").
		Where("ch.marina_id = ?", s.ID()).Where("ch.contract_id = ?", contractID).
		Where("ch.payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("ch.total_cents > 0").Where("ch.due_on < ?", cutoff).
		Where("NOT EXISTS (SELECT 1 FROM dunning_exclusions AS x WHERE x.marina_id = ch.marina_id AND x.charge_id = ch.id "+
			"AND x.removed_at IS NULL AND (x.until_on IS NULL OR x.until_on >= ?))", today).
		OrderExpr("ch.id")
	if IsPostgres(db) {
		q = q.For("UPDATE OF ch")
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return 0, err
	}
	var sum int64
	for _, r := range rows {
		sum += r.Outstanding
	}
	return sum, nil
}

// CustomerDunningExcluded reports whether the customer has an active customer-level dunning exclusion on today.
func CustomerDunningExcluded(ctx context.Context, db bun.IDB, s MarinaScope, customerID string, today calendar.Date) (bool, error) {
	if customerID == "" {
		return false, nil
	}
	return db.NewSelect().TableExpr("dunning_exclusions").Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Where("removed_at IS NULL").Where("(until_on IS NULL OR until_on >= ?)", today).Exists(ctx)
}

// CollectContractsByStatus returns the ids of the marina's contracts of the given kinds and status.
func CollectContractsByStatus(ctx context.Context, db bun.IDB, s MarinaScope, kinds []string, status string) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Column("id", "kind", "status", "customer_id", "billing_customer_id", "suspended_at", "suspend_reason").
		Where("marina_id = ?", s.ID()).Where("kind IN (?)", bun.List(kinds)).Where("status = ?", status).OrderExpr("id").Scan(ctx)
	return out, err
}
