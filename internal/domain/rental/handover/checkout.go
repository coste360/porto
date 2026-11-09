package handover

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/store"
)

// CheckOut hands the boat over (If-Match): preconditions, main charge, reservation.CheckOutTx, handover document
// (m11-plan §7 step 5). One transaction locks the booking and checks identity (+ licence), signed contract, deposit,
// checklist with the minimum photos and engine hours at least the unit's; a missing step is 422 handover_incomplete.
func (s *Service) CheckOut(ctx context.Context, p *auth.Principal, marinaID, bookingID string, ifMatch int, in CheckOutInput) (*reservation.BookingView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 500 {
		return nil, invalid("note", "too_long")
	}
	now := s.now()
	at := store.UTC(in.ActualAt)
	if at.IsZero() {
		at = now
	}
	if at.After(now.Add(OfflineAfter)) {
		return nil, invalid("actual_at", "in_future")
	}
	actor := p.Actor()
	var out *store.RentalBooking
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := s.lock(ctx, tx, scope, bookingID, ifMatch, store.RentalStatusConfirmed)
		if err != nil {
			return err
		}
		d, err := s.loadDraft(ctx, tx, scope, b, store.RentalPhaseCheckout)
		if err != nil {
			return err
		}
		if missing := d.missing(store.RentalPhaseCheckout); len(missing) > 0 {
			return &IncompleteError{Missing: missing}
		}
		if hasEngine(d.unit) && *d.handover.EngineHoursMilli < d.unit.EngineHoursMilli {
			return ErrEngineHoursDecreasing
		}
		if d.identity != nil && !d.identity.LicenceExpiresOn.IsZero() && d.identity.LicenceExpiresOn.Before(b.EndsOn) {
			return ErrLicenceExpired
		}
		if _, err := s.mainChargeTx(ctx, tx, scope, actor, b); err != nil {
			return err
		}
		if out, err = s.d.Reservations.CheckOutTx(ctx, tx, scope, actor, b.ID, ifMatch, at); err != nil {
			return err
		}
		h := d.handover
		h.ActualAt, h.CompletedAt, h.CompletedByUserID = at, now, userOf(actor)
		if in.Note != "" {
			h.Notes = strings.TrimSpace(strings.TrimSpace(h.Notes + "\n" + in.Note))
		}
		if err := store.UpdateRentalHandover(ctx, tx, scope, h, now); err != nil {
			return err
		}
		if _, err := s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
			Kind: docs.KindRentalHandover, SubjectType: docs.SubjectRentalBooking, SubjectID: h.ID, CreatedBy: userOf(actor), At: at,
		}); err != nil {
			return err
		}
		return s.audit(ctx, tx, scope, actor, "rental_handover.complete", "rental_handover", h.ID, map[string]any{
			"booking_id": b.ID, "phase": store.RentalPhaseCheckout, "actual_at": at,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, p, marinaID, out)
}
