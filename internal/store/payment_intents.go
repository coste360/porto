package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Payment intents: online collection attempts (m5-plan §5.4). Statuses move forward only (created → open →
// processing → succeeded | failed | expired | cancelled), except a succeeded after failed/expired/cancelled (money
// arrived). payment_intent_charges.open_flag is 1 while the intent is created, open or processing; its partial
// unique index keeps one live intent per charge (§16 D16). Owned by CASH-DRIVERS.

// LiveIntentStatuses are the statuses that keep open_flag set.
var LiveIntentStatuses = []string{IntentCreated, IntentOpen, IntentProcessing}

// InsertPaymentIntent inserts an intent with its charges. A charge that already has a live intent is ErrConflict;
// an idempotency key already used returns the existing intent with created = false.
func InsertPaymentIntent(ctx context.Context, tx bun.IDB, s MarinaScope, in *PaymentIntent, charges []PaymentIntentCharge) (*PaymentIntent, bool, error) {
	res, err := tx.NewInsert().Model(in).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if affected(res) == 0 {
		existing := new(PaymentIntent)
		if err := tx.NewSelect().Model(existing).Where("marina_id = ?", s.ID()).Where("idempotency_key = ?", in.IdempotencyKey).Scan(ctx); err != nil {
			return nil, false, notFound(err)
		}
		return existing, false, nil
	}
	for i := range charges {
		c := &charges[i]
		c.ID, c.MarinaID, c.IntentID, c.OpenFlag = NewID(), s.ID(), in.ID, true
		c.CreatedAt, c.UpdatedAt = in.CreatedAt, in.CreatedAt
		if _, err := tx.NewInsert().Model(c).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return nil, false, ErrConflict
			}
			return nil, false, err
		}
	}
	return in, true, nil
}

// GetPaymentIntent returns an intent of the marina.
func GetPaymentIntent(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PaymentIntent, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	in := new(PaymentIntent)
	err := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return in, notFound(err)
}

// LockPaymentIntent locks an intent of the marina (FOR UPDATE on PostgreSQL).
func LockPaymentIntent(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*PaymentIntent, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	in := new(PaymentIntent)
	q := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	return in, notFound(q.Scan(ctx))
}

// IntentIDByRef finds the intent of a provider session or payment reference in the marina ("" when unknown).
func IntentIDByRef(ctx context.Context, db bun.IDB, s MarinaScope, provider, sessionRef, paymentRef string) (string, error) {
	for _, c := range []struct{ col, ref string }{{"provider_session_ref", sessionRef}, {"provider_payment_ref", paymentRef}} {
		if c.ref == "" {
			continue
		}
		var ids []string
		if err := db.NewSelect().Model((*PaymentIntent)(nil)).Column("id").Where("marina_id = ?", s.ID()).
			Where("provider_code = ?", provider).Where("? = ?", bun.Ident(c.col), c.ref).Limit(1).Scan(ctx, &ids); err != nil {
			return "", err
		}
		if len(ids) == 1 {
			return ids[0], nil
		}
	}
	return "", nil
}

// IntentRefExistsElsewhere reports whether a provider reference belongs to an intent of another marina (a foreign
// event is ignored, never applied).
func IntentRefExistsElsewhere(ctx context.Context, db bun.IDB, s MarinaScope, provider, sessionRef, paymentRef string) (bool, error) {
	q := db.NewSelect().Model((*PaymentIntent)(nil)).Where("marina_id <> ?", s.ID()).Where("provider_code = ?", provider)
	switch {
	case sessionRef != "" && paymentRef != "":
		q = q.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("provider_session_ref = ?", sessionRef).WhereOr("provider_payment_ref = ?", paymentRef)
		})
	case sessionRef != "":
		q = q.Where("provider_session_ref = ?", sessionRef)
	case paymentRef != "":
		q = q.Where("provider_payment_ref = ?", paymentRef)
	default:
		return false, nil
	}
	return q.Exists(ctx)
}

// IntentCharges returns the charges of an intent.
func IntentCharges(ctx context.Context, db bun.IDB, s MarinaScope, intentID string) ([]PaymentIntentCharge, error) {
	out := []PaymentIntentCharge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("intent_id = ?", intentID).OrderExpr("id").Scan(ctx)
	return out, err
}

// SetIntentRefs stores the provider session/payment refs, checkout URL and expiry after the provider answered,
// moving created → open. Refs already set are kept.
func SetIntentRefs(ctx context.Context, tx bun.IDB, s MarinaScope, id, sessionRef, paymentRef, url string, expiresAt, now time.Time) error {
	now = UTC(now)
	q := tx.NewUpdate().Model((*PaymentIntent)(nil)).Set("version = version + 1").Set("updated_at = ?", now).
		Set("status = CASE WHEN status = ? THEN ? ELSE status END", IntentCreated, IntentOpen).
		Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if sessionRef != "" {
		q = q.Set("provider_session_ref = COALESCE(provider_session_ref, ?)", sessionRef)
	}
	if paymentRef != "" {
		q = q.Set("provider_payment_ref = COALESCE(provider_payment_ref, ?)", paymentRef)
	}
	if url != "" {
		q = q.Set("checkout_url = ?", url)
	}
	if !expiresAt.IsZero() {
		q = q.Set("expires_at = ?", UTC(expiresAt))
	}
	_, err := q.Exec(ctx)
	if err != nil && IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// SetIntentPaymentRef stores the provider payment reference when not set yet.
func SetIntentPaymentRef(ctx context.Context, tx bun.IDB, s MarinaScope, id, paymentRef string, now time.Time) error {
	if paymentRef == "" {
		return nil
	}
	_, err := tx.NewUpdate().Model((*PaymentIntent)(nil)).Set("provider_payment_ref = ?", paymentRef).
		Set("updated_at = ?", UTC(now)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("provider_payment_ref IS NULL").Exec(ctx)
	if err != nil && IsUniqueViolation(err) {
		return nil // another intent already owns the reference: keep ours unset
	}
	return err
}

// SetIntentStatus moves an intent to status when its current status is one of from, keeping open_flag of its
// charges in step. It reports whether the intent changed.
func SetIntentStatus(ctx context.Context, tx bun.IDB, s MarinaScope, id string, from []string, to, failureCode string, now time.Time) (bool, error) {
	now = UTC(now)
	q := tx.NewUpdate().Model((*PaymentIntent)(nil)).Set("status = ?", to).Set("version = version + 1").
		Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status IN (?)", bun.List(from))
	if failureCode != "" {
		q = q.Set("failure_code = ?", failureCode)
	}
	if to == IntentSucceeded {
		q = q.Set("succeeded_at = ?", now)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	if affected(res) == 0 {
		return false, nil
	}
	live := false
	for _, st := range LiveIntentStatuses {
		if st == to {
			live = true
		}
	}
	if !live {
		if _, err := tx.NewUpdate().Model((*PaymentIntentCharge)(nil)).Set("open_flag = ?", false).Set("updated_at = ?", now).
			Where("marina_id = ?", s.ID()).Where("intent_id = ?", id).Where("open_flag = ?", true).Exec(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

// TouchIntentChecked records a reconciliation check.
func TouchIntentChecked(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) error {
	_, err := db.NewUpdate().Model((*PaymentIntent)(nil)).Set("last_checked_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// IntentsToReconcile returns open or processing intents created before olderThan, least recently checked first.
func IntentsToReconcile(ctx context.Context, db bun.IDB, s MarinaScope, olderThan time.Time, limit int) ([]PaymentIntent, error) {
	out := []PaymentIntent{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("status IN (?)", bun.List([]string{IntentOpen, IntentProcessing})).Where("created_at < ?", UTC(olderThan)).
		OrderExpr("last_checked_at ASC NULLS FIRST, id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// ExpiredOpenIntents returns created/open intents of the marina whose expiry passed.
func ExpiredOpenIntents(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]PaymentIntent, error) {
	out := []PaymentIntent{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("status IN (?)", bun.List([]string{IntentCreated, IntentOpen})).
		Where("expires_at IS NOT NULL").Where("expires_at < ?", UTC(now)).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// LiveIntentsOf returns the live intents of a request or a contract (purpose filter optional).
func LiveIntentsOf(ctx context.Context, db bun.IDB, s MarinaScope, requestID, contractID, purpose string) ([]PaymentIntent, error) {
	out := []PaymentIntent{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN (?)", bun.List(LiveIntentStatuses))
	if requestID != "" {
		q = q.Where("request_id = ?", requestID)
	}
	if contractID != "" {
		q = q.Where("contract_id = ?", contractID)
	}
	if purpose != "" {
		q = q.Where("purpose = ?", purpose)
	}
	err := q.OrderExpr("id").Scan(ctx)
	return out, err
}

// CountIntents returns how many intents share an idempotency key prefix (attempt numbering).
func CountIntents(ctx context.Context, db bun.IDB, s MarinaScope, keyPrefix string) (int, error) {
	n, err := db.NewSelect().Model((*PaymentIntent)(nil)).Where("marina_id = ?", s.ID()).
		Where("idempotency_key LIKE ?", keyPrefix+"%").Count(ctx)
	return int(n), err
}

// ChargesWithLiveIntent returns which of the given charges have a live intent.
func ChargesWithLiveIntent(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	var ids []string
	if err := db.NewSelect().Model((*PaymentIntentCharge)(nil)).Column("charge_id").Where("marina_id = ?", s.ID()).
		Where("open_flag = ?", true).Where("charge_id IN (?)", bun.List(chargeIDs)).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// FailedIntents returns failed intents of the marina, newest first (worklist).
func FailedIntents(ctx context.Context, db bun.IDB, s MarinaScope, limit int) ([]PaymentIntent, error) {
	out := []PaymentIntent{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", IntentFailed).
		OrderExpr("id DESC").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// IntentsOfRequest returns every intent of a request, newest first.
func IntentsOfRequest(ctx context.Context, db bun.IDB, s MarinaScope, requestID string) ([]PaymentIntent, error) {
	out := []PaymentIntent{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("request_id = ?", requestID).OrderExpr("id DESC").Scan(ctx)
	return out, err
}

// SetIntentMethod sets the requested method of a created intent.
func SetIntentMethod(ctx context.Context, tx bun.IDB, s MarinaScope, id, method string, now time.Time) error {
	_, err := tx.NewUpdate().Model((*PaymentIntent)(nil)).Set("method = ?", method).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", IntentCreated).Exec(ctx)
	return err
}

// PaymentIDsOfIntent returns the payments recorded for an intent.
func PaymentIDsOfIntent(ctx context.Context, db bun.IDB, s MarinaScope, intentID string) ([]string, error) {
	var ids []string
	err := db.NewSelect().TableExpr("payments").Column("id").Where("marina_id = ?", s.ID()).Where("intent_id = ?", intentID).
		OrderExpr("id").Scan(ctx, &ids)
	return ids, err
}

// PendingProviderRefunds returns the pending provider refunds of a payment (oldest first) or of the marina when
// paymentID is empty and olderThan is set (reconciliation).
func PendingProviderRefunds(ctx context.Context, db bun.IDB, s MarinaScope, paymentID string, olderThan time.Time, limit int) ([]Refund, error) {
	out := []Refund{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", RefundPending).
		Where("source = ?", PaymentSourceProvider)
	if paymentID != "" {
		q = q.Where("payment_id = ?", paymentID)
	}
	if !olderThan.IsZero() {
		q = q.Where("created_at < ?", UTC(olderThan))
	}
	err := q.OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}
