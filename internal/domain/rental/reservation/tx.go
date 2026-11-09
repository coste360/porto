package reservation

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/domain/rental/states"
	"github.com/coste360/porto/internal/store"
)

// Transitions called by internal/domain/rental/handover inside its own transaction (m11-plan §6, §16.3). The
// signatures are frozen (handover.Reservations). Each locks the booking, checks the status (and expected version
// when > 0) under the lock, changes status and rows, and writes audit and outbox in tx. Errors: auth.ErrNotFound,
// ErrVersionMismatch, ErrInvalidTransition.

// CheckOutTx moves a confirmed booking to checked_out at the instant at (rental.checked_out).
func (s *Service) CheckOutTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, at time.Time) (*store.RentalBooking, error) {
	b, to, err := lockFor(ctx, tx, scope, bookingID, expected, states.CheckOut)
	if err != nil {
		return nil, err
	}
	version := b.Version
	b.Status, b.CheckedOutAt = to, store.UTC(at)
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, s.now()); err != nil {
		return nil, versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.check_out", b, map[string]any{"checked_out_at": b.CheckedOutAt}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.RentalCheckedOut, "", b); err != nil {
		return nil, err
	}
	return b, nil
}

// ReturnTx moves a checked-out booking to returned at returnedAt; on an early return it releases the unit rows
// after slot_up(returnedAt) + buffer and the skipper rows after slot_up(returnedAt) (D11) (rental.returned). A late
// return keeps the rows unchanged (the late fee is charged by the handover side).
func (s *Service) ReturnTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string, expected int, returnedAt time.Time) (*store.RentalBooking, error) {
	b, to, err := lockFor(ctx, tx, scope, bookingID, expected, states.Return)
	if err != nil {
		return nil, err
	}
	loc := location(ctx, tx, scope)
	r := RangeOf(b)
	cut := slots.Up(returnedAt, loc)
	end := r.End
	early := false
	if slots.Index(r.Start.Date, cut) < slots.Index(r.Start.Date, end) {
		early = true
		// Keep at least the first slot of the range (a return before the start keeps the first slot).
		if slots.Index(r.Start.Date, cut) <= r.Start.Slot {
			cut = slots.After(r.Start, 1)
		}
		settings, err := settingsTx(ctx, tx, scope)
		if err != nil {
			return nil, err
		}
		cutKey := slots.AsStart(cut)
		bufEnd := slots.AsStart(slots.After(cut, settings.Row.BufferSlots))
		if err := store.TruncateRentalBookingSlots(ctx, tx, scope, b.ID,
			store.RentalSlotKey{Date: cutKey.Date, Slot: cutKey.Slot}, store.RentalSlotKey{Date: bufEnd.Date, Slot: bufEnd.Slot}); err != nil {
			return nil, err
		}
	}
	version := b.Version
	b.Status, b.ReturnedAt = to, store.UTC(returnedAt)
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, s.now()); err != nil {
		return nil, versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.return", b, map[string]any{
		"returned_at": b.ReturnedAt, "early": early,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.RentalReturned, "", b); err != nil {
		return nil, err
	}
	return b, nil
}

// CloseTx moves a returned booking to closed (rental.changed closed). The caller checks that the deposit is final
// and the return charge decided.
func (s *Service) CloseTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string) (*store.RentalBooking, error) {
	b, to, err := lockFor(ctx, tx, scope, bookingID, 0, states.Close)
	if err != nil {
		return nil, err
	}
	now := s.now()
	version := b.Version
	b.Status, b.ClosedAt = to, now
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, version, now); err != nil {
		return nil, versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.close", b, nil); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.RentalChanged, "closed", b); err != nil {
		return nil, err
	}
	return b, nil
}

// RepriceTx recomputes the price snapshot of a booking (after a reschedule or an extension) and returns the delta
// against the previous snapshot; the handover side adjusts the main charge (§9.1). Terminal bookings are
// ErrInvalidTransition.
func (s *Service) RepriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID string) (*RepriceResult, error) {
	b, err := store.LockRentalBooking(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	if states.Terminal(states.Status(b.Status)) {
		return nil, ErrInvalidTransition
	}
	prev := *b
	extras, err := bookingExtras(ctx, tx, scope, b.ID)
	if err != nil {
		return nil, err
	}
	if err := s.priceTx(ctx, tx, scope, b, extras); err != nil {
		return nil, err
	}
	res := &RepriceResult{Booking: b, Delta: moneyOf(b.TotalCents - prev.TotalCents)}
	_ = decodeJSON(b.PriceSnapshot, &res.Snapshot)
	_ = decodeJSON(prev.PriceSnapshot, &res.Previous)
	if string(b.PriceSnapshot) == string(prev.PriceSnapshot) {
		return res, nil
	}
	if err := store.WriteRentalBookingState(ctx, tx, scope, b, prev.Version, s.now()); err != nil {
		return nil, versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.reprice", b, map[string]any{"previous_total_cents": prev.TotalCents}); err != nil {
		return nil, err
	}
	return res, nil
}

// CancelTx cancels a held or confirmed booking and deletes its rows (rental.cancelled); the caller cancels the main
// charge when cancellable and voids the pre-authorisation after commit.
func (s *Service) CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID, reason string) (*store.RentalBooking, error) {
	return s.cancelTx(ctx, tx, scope, actor, bookingID, 0, reason, states.Cancel)
}

// LockBookingTx locks a booking row of the marina (lock order: booking, unit, skipper) and returns it; the
// handover side re-checks status and version under this lock.
func (s *Service) LockBookingTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, bookingID string) (*store.RentalBooking, error) {
	b, err := store.LockRentalBooking(ctx, tx, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	return b, nil
}
