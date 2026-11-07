package signature

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
)

const syncBatch = 200

// RunSync sends requests whose document became ready, completes requests whose signers have all signed (a failed
// completion is retried here), expires the requests past their expiry, sends the reminders every
// signature_reminder_days and polls the provider envelopes (job signatures.sync, idempotent).
func (s *Service) RunSync(ctx context.Context) error {
	after := ""
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	for {
		rows, err := store.OpenSignatureRequests(ctx, s.d.DB.DB, after, syncBatch)
		if err != nil {
			return err
		}
		for i := range rows {
			keep(s.syncOne(ctx, &rows[i]))
		}
		if len(rows) < syncBatch {
			break
		}
		after = rows[len(rows)-1].ID
	}
	return first
}

func (s *Service) syncOne(ctx context.Context, r *store.SignatureRequest) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, r.MarinaID)
	if err != nil {
		return err
	}
	now := s.now()
	if !now.Before(r.ExpiresAt) {
		return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			err := s.failTx(ctx, tx, scope, r, StatusExpired, audit.System)
			if errors.Is(err, ErrNotSigner) {
				return nil
			}
			return err
		})
	}
	switch r.Status {
	case StatusPendingDocument:
		return s.dispatch(ctx, r.MarinaID, r.ID)
	case StatusSent, StatusPartiallySigned:
		if r.Driver == DriverProvider {
			return s.poll(ctx, scope, r)
		}
		signers, err := store.SignersOf(ctx, s.d.DB.DB, scope, r.ID)
		if err != nil {
			return err
		}
		all := len(signers) > 0
		var invitedAt time.Time
		for _, sg := range signers {
			if sg.Status != SignerSigned {
				all = false
			}
			if sg.Status == SignerInvited && (invitedAt.IsZero() || sg.InvitedAt.Before(invitedAt)) {
				invitedAt = sg.InvitedAt
			}
		}
		if all {
			return s.complete(ctx, scope, r.ID, nil)
		}
		if r.Driver != DriverOTP || invitedAt.IsZero() {
			return nil
		}
		cs, err := store.GetContractSettings(ctx, s.d.DB.DB, scope, now)
		if err != nil {
			return err
		}
		every := time.Duration(max(cs.SignatureReminderDays, 1)) * 24 * time.Hour
		last := r.LastReminderAt
		if last.IsZero() {
			last = invitedAt
		}
		if now.Sub(last) < every {
			return nil
		}
		return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			fresh, err := store.GetSignatureRequest(ctx, tx, scope, r.ID)
			if err != nil {
				return err
			}
			if !fresh.LastReminderAt.Equal(r.LastReminderAt) || fresh.Version != r.Version {
				return nil // another run sent it
			}
			err = s.remindTx(ctx, tx, scope, fresh, audit.System)
			if errors.Is(err, store.ErrVersionMismatch) {
				return nil
			}
			return err
		})
	}
	return nil
}
