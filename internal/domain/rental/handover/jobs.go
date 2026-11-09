package handover

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Jobs of the handover (m11-plan §13.2): rental.deposits retries the capture or void of decided pre-authorisations
// with the same idempotency keys; rental.identity_retention wipes identity data and identity/licence photos after
// closed_at (or cancelled_at, no_show_at) + identity_retention_days, audited.

func jobActor(name string) audit.Actor { return audit.Actor{Type: audit.ActorSystem, ID: name} }

func (s *Service) eachMarina(ctx context.Context, fn func(scope store.MarinaScope) error) error {
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if err := fn(scope); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RunDeposits retries the capture or void of decided pre-authorisations (job rental.deposits).
func (s *Service) RunDeposits(ctx context.Context) error {
	return s.eachMarina(ctx, func(scope store.MarinaScope) error {
		rows, err := store.RentalDepositsToSettle(ctx, s.d.DB.DB, scope, 100)
		if err != nil {
			return err
		}
		for i := range rows {
			b := &rows[i]
			if b.DepositDecisionCents == nil {
				continue
			}
			if err := s.d.Collect.CaptureIntentAmount(ctx, scope, b.DepositIntentID, money.Cents(*b.DepositDecisionCents)); err != nil {
				s.d.Log.WarnContext(ctx, "rental deposit not settled", "marina_id", scope.ID(), "booking_id", b.ID, "intent_id", b.DepositIntentID)
			}
		}
		return nil
	})
}

// RunIdentityRetention wipes identity data and document photos past the retention (job rental.identity_retention).
func (s *Service) RunIdentityRetention(ctx context.Context) error {
	actor := jobActor(JobIdentityRetention)
	return s.eachMarina(ctx, func(scope store.MarinaScope) error {
		set, err := store.LoadRentalSettings(ctx, s.d.DB.DB, scope)
		if err != nil {
			return err
		}
		days := set.IdentityRetentionDays
		if days <= 0 {
			days = 365
		}
		before := s.now().Add(-time.Duration(days) * 24 * time.Hour)
		rows, err := store.RentalIdentityToPurge(ctx, s.d.DB.DB, scope, before, 200)
		if err != nil {
			return err
		}
		for i := range rows {
			if err := s.purgeIdentity(ctx, scope, actor, rows[i].ID, rows[i].BookingID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) purgeIdentity(ctx context.Context, scope store.MarinaScope, actor audit.Actor, handoverID, bookingID string) error {
	var keys []string
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		keys = keys[:0]
		h, err := store.LockRentalHandover(ctx, tx, scope, bookingID, store.RentalPhaseCheckout)
		if err != nil || h.ID != handoverID || !h.IdentityPurgedAt.IsZero() {
			return nil //nolint:nilerr // gone or already purged
		}
		files, err := store.RentalHandoverFilesOf(ctx, tx, scope, []string{h.ID})
		if err != nil {
			return err
		}
		now := s.now()
		var ids []string
		for _, f := range files {
			if f.Kind != store.RentalFileIDDocument && f.Kind != store.RentalFileLicence {
				continue
			}
			key, err := store.SoftDeleteStoredFile(ctx, tx, scope, f.FileID, now)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if key != "" {
				keys = append(keys, key)
			}
			ids = append(ids, f.ID)
		}
		if err := store.DeleteRentalHandoverFiles(ctx, tx, scope, ids); err != nil {
			return err
		}
		h.IdentityEnc, h.IdentityPurgedAt = "", now
		if err := store.UpdateRentalHandover(ctx, tx, scope, h, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, scope, actor, "rental_handover.identity_purge", "rental_handover", h.ID, map[string]any{
			"booking_id": bookingID, "files": len(ids),
		})
	})
	if err != nil {
		return err
	}
	for _, k := range keys {
		if s.d.Files != nil {
			_ = s.d.Files.Remove(k)
		}
	}
	return nil
}
