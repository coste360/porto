package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Fiscal bridge claims (M6b, m6b-plan §8.4): documents of the core driver "bridge" are claimed by the desktop of
// the marina with a lease. Every claim is a conditional UPDATE on the row (still pending, never claimed or lease
// expired), so a document is held by at most one claim at a time on both dialects (INV-86). Owned by REMOTE.

// BridgeCandidates returns pending bridge documents of the marina that are not leased at now, oldest first.
func BridgeCandidates(ctx context.Context, db bun.IDB, s MarinaScope, driver string, now time.Time, limit int) ([]string, error) {
	var ids []string
	err := db.NewSelect().Model((*FiscalDocument)(nil)).Column("id").
		Where("marina_id = ?", s.ID()).Where("status = ?", FiscalPending).Where("driver = ?", driver).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("bridge_claim_until IS NULL").WhereOr("bridge_claim_until < ?", UTC(now))
		}).
		OrderExpr("id").Limit(int64(limit)).Scan(ctx, &ids)
	return ids, err
}

// ClaimBridgeDocument leases one document to deviceID with claimID until until. It reports whether this call won
// the row (false: issued meanwhile, or claimed by a concurrent caller).
func ClaimBridgeDocument(ctx context.Context, db bun.IDB, s MarinaScope, id, deviceID, claimID string, now, until time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*FiscalDocument)(nil)).
		Set("bridge_device_id = ?", deviceID).Set("bridge_claim_id = ?", claimID).
		Set("bridge_claim_until = ?", UTC(until)).Set("bridge_attempts = bridge_attempts + 1").
		Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", FiscalPending).
		WhereGroup(" AND ", func(q *bun.UpdateQuery) *bun.UpdateQuery {
			return q.Where("bridge_claim_until IS NULL").WhereOr("bridge_claim_until < ?", UTC(now))
		}).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// BridgeOutcome is the result of a claimed document reported by the desktop.
type BridgeOutcome struct {
	Status      string // FiscalIssued, FiscalFailed or FiscalPending (deferred: the claim is released)
	ExternalRef string
	ErrorCode   string
	IssuedAt    time.Time
}

// SetBridgeOutcome applies the outcome of claimID on a pending document whose lease is still valid at now. It
// reports whether the row changed (false: stale or foreign claim).
func SetBridgeOutcome(ctx context.Context, tx bun.IDB, s MarinaScope, id, claimID string, o BridgeOutcome, now time.Time) (bool, error) {
	q := tx.NewUpdate().Model((*FiscalDocument)(nil)).Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", FiscalPending).
		Where("bridge_claim_id = ?", claimID).Where("bridge_claim_until >= ?", UTC(now))
	switch o.Status {
	case FiscalIssued:
		q = q.Set("status = ?", FiscalIssued).Set("external_ref = ?", nullString(o.ExternalRef)).
			Set("error_code = NULL").Set("issued_at = ?", UTC(o.IssuedAt)).Set("attempts = attempts + 1")
	case FiscalFailed:
		q = q.Set("status = ?", FiscalFailed).Set("error_code = ?", nullString(o.ErrorCode)).
			Set("attempts = attempts + 1").Set("bridge_claim_until = NULL")
	default: // deferred: release the lease, keep the claim id for the idempotent replay
		q = q.Set("bridge_claim_until = NULL")
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// PendingFiscalDocumentsExcept is PendingFiscalDocuments without the documents of driver (the job fiscal.issue
// leaves the bridge documents to their desktop, and must not be starved by them).
func PendingFiscalDocumentsExcept(ctx context.Context, db bun.IDB, driver string, limit int) ([]FiscalDocument, error) {
	out := []FiscalDocument{}
	err := db.NewSelect().Model(&out).Where("status = ?", FiscalPending).Where("driver <> ?", driver).
		OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// SetFiscalBridgeDevice stores the bridge device of the billing settings ("" = none) after SaveBillingSettings,
// in the same transaction (the M5 update lists its own columns).
func SetFiscalBridgeDevice(ctx context.Context, tx bun.IDB, s MarinaScope, deviceID string) error {
	_, err := tx.NewUpdate().Model((*BillingSettings)(nil)).Set("fiscal_bridge_device_id = ?", nullString(deviceID)).
		Where("marina_id = ?", s.ID()).Exec(ctx)
	return err
}
