package handover

import (
	"context"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Rental charges (m11-plan §9.1, D18, INV-99): one main charge from the price snapshot (created lazily at the first
// quick cash, at check-out or at no-show, never for a held booking) and one return charge per booking, linked in
// rental_booking_charges whose partial unique index keeps them single even concurrently. A reprice after the main
// charge exists becomes a linked delta adjustment (corrects_charge_id = main): exported charges never change
// (INV-51). Deposits never appear as charge lines (INV-98).

// itemLine is a priced line: an item, a quantity and the amount the pure pricing computed, in the item's price
// terms. When qty x unit does not give the amount exactly (half-even hourly rates) the line is one piece of the
// amount, so the charge total always equals the snapshot.
type itemLine struct {
	ItemID      string
	Qty         qty.Milli
	Unit        money.Cents
	Amount      money.Cents
	Description string
}

func (s *Service) chargeLines(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, in []itemLine) ([]charge.LineInput, error) {
	staff := make([]charge.ItemLine, 0, len(in))
	for _, l := range in {
		if l.Amount == 0 {
			continue
		}
		q, unit := l.Qty, l.Unit
		if got, err := q.MulCents(unit); q <= 0 || err != nil || got != l.Amount {
			q, unit = qty.Milli(1000), l.Amount
		}
		u := unit
		staff = append(staff, charge.ItemLine{
			ItemID: l.ItemID, Qty: q, Unit: &u, Description: l.Description, ServiceFrom: b.StartsOn, ServiceTo: b.EndsOn,
		})
	}
	if len(staff) == 0 {
		return nil, nil
	}
	return s.d.Charges.PriceLines(ctx, db, scope, staff, true)
}

// mainLines are the lines of the main charge: the snapshot lines (unit, skipper, extras).
func mainLines(b *store.RentalBooking) []itemLine {
	snap := snapshotOf(b)
	out := make([]itemLine, 0, len(snap.Breakdown.Lines))
	for _, l := range snap.Breakdown.Lines {
		out = append(out, itemLine{ItemID: l.ItemID, Qty: l.Qty, Unit: l.UnitCents, Amount: l.Amount})
	}
	return out
}

func (s *Service) unitVessel(ctx context.Context, db bun.IDB, scope store.MarinaScope, unitID string) string {
	if u, err := store.GetRentalUnit(ctx, db, scope, unitID); err == nil {
		return u.VesselID
	}
	return ""
}

// liveCharge returns the linked charge of a kind that is not cancelled (nil when none).
func liveCharge(rows []store.RentalLinkedCharge, kind string) *store.RentalLinkedCharge {
	for i := range rows {
		if rows[i].LinkKind == kind && rows[i].PaymentStatus != store.ChargeCancelled {
			return &rows[i]
		}
	}
	return nil
}

// createLinkedTx creates a rental charge of a booking and links it with kind (main, return, adjustment). A second
// main or return link is store.ErrConflict (INV-99) and rolls the caller back.
func (s *Service) createLinkedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking, kind, correctsID string, lines []charge.LineInput) (*store.Charge, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	if fiscalMode(b) == charge.FiscalInvoice && b.CustomerID == "" {
		return nil, ErrCustomerRequired
	}
	today := s.today(ctx, tx, scope)
	in := charge.CreateInput{
		OriginType: store.ChargeOriginRental, OriginID: b.ID, CustomerID: b.CustomerID, VesselID: s.unitVessel(ctx, tx, scope, b.UnitID),
		IssuedOn: today, DueOn: today, FiscalMode: fiscalMode(b), Description: chargeDescription(b, kind), Lines: lines,
	}
	if correctsID != "" {
		in.CorrectsChargeID, in.CorrectionKind = correctsID, "delta"
	}
	ch, _, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, in)
	if err != nil {
		if errors.Is(err, charge.ErrCustomerRequired) {
			return nil, ErrCustomerRequired
		}
		return nil, err
	}
	if ch == nil {
		return nil, nil // a zero total creates nothing
	}
	if err := store.InsertRentalBookingCharge(ctx, tx, store.NewRentalBookingCharge(scope, b.ID, ch.ID, kind, s.now())); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.charge", "rental_booking", b.ID, map[string]any{
		"charge_id": ch.ID, "kind": kind, "total_cents": ch.TotalCents, "fiscal_mode": ch.FiscalMode,
	}); err != nil {
		return nil, err
	}
	return ch, nil
}

func chargeDescription(b *store.RentalBooking, kind string) string {
	n := fmt.Sprintf("NL%04d-%06d", b.Year, b.Number)
	switch kind {
	case store.RentalChargeReturn:
		return "Noleggio " + n + " - rientro"
	case store.RentalChargeAdjustment:
		return "Noleggio " + n + " - rettifica"
	}
	return "Noleggio " + n
}

// mainChargeTx returns the live main charge of a booking, creating it from the snapshot when missing (§9.1). The
// booking is locked by the caller.
func (s *Service) mainChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) (*store.Charge, error) {
	if b.Status == store.RentalStatusHeld || b.Status == store.RentalStatusCancelled {
		return nil, ErrInvalidTransition
	}
	rows, err := store.RentalChargesOf(ctx, tx, scope, b.ID)
	if err != nil {
		return nil, err
	}
	if live := liveCharge(rows, store.RentalChargeMain); live != nil {
		return &live.Charge, nil
	}
	lines, err := s.chargeLines(ctx, tx, scope, b, mainLines(b))
	if err != nil {
		return nil, err
	}
	return s.createLinkedTx(ctx, tx, scope, actor, b, store.RentalChargeMain, "", lines)
}

// EnsureMainChargeTx creates the main charge of a locked booking when missing (no-show keeps it, §6); reservation
// calls it through its hooks.
func (s *Service) EnsureMainChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) (*store.Charge, error) {
	return s.mainChargeTx(ctx, tx, scope, actor, b)
}

// adjustmentsOf returns the live adjustment charges linked to a booking that correct the charge mainID.
func adjustmentsOf(rows []store.RentalLinkedCharge, mainID string) []store.RentalLinkedCharge {
	var out []store.RentalLinkedCharge
	for _, r := range rows {
		if r.LinkKind == store.RentalChargeAdjustment && r.PaymentStatus != store.ChargeCancelled && r.CorrectsChargeID == mainID {
			out = append(out, r)
		}
	}
	return out
}

// untouched reports whether a charge can still be cancelled: open, never exported, nothing paid, matched or offset
// (a charge with a fiscal document is paid, so it is never untouched).
func untouched(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Charge) (bool, error) {
	if c.PaymentStatus != store.ChargeOpen || c.InvoicingStatus != store.ChargeToInvoice || c.PaidAmountCents != 0 || c.CreditedCents != 0 {
		return false, nil
	}
	matched, err := store.HasMatches(ctx, db, scope, c.ID)
	return !matched, err
}

// replaceableTx reports whether the live main charge and every live adjustment of it are untouched, so the main
// charge can be cancelled and re-created instead of corrected (m11-plan §6, §9.1, D18, D22).
func replaceableTx(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.RentalLinkedCharge, main *store.RentalLinkedCharge) (bool, error) {
	if ok, err := untouched(ctx, db, scope, &main.Charge); err != nil || !ok {
		return false, err
	}
	for _, a := range adjustmentsOf(rows, main.ID) {
		if ok, err := untouched(ctx, db, scope, &a.Charge); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// cancelMainTx cancels an untouched main charge; the rental cancel hook (onChargeCancelledTx) cancels its
// adjustments and marks the link replaced, so a new main charge can be linked (INV-99).
func (s *Service) cancelMainTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, main *store.RentalLinkedCharge, reason string) error {
	ok, err := s.d.Charges.CancelTx(ctx, tx, scope, actor, &main.Charge, reason)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrConflict // changed since it was read under the booking lock
	}
	return nil
}

// onChargeCancelledTx is the charge cancel hook of origin rental: it runs in the transaction that cancels a rental
// charge (the office through charge.Service.Cancel, or this service replacing the main charge). A main or return
// link becomes replaced, so the booking can be charged again (INV-99); cancelling a main charge cancels its live
// adjustments too, and is refused (charge.ErrImmutable) while one of them is exported, paid or offset.
func (s *Service) onChargeCancelledTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge) error {
	bookingID := ch.OriginID
	rows, err := store.RentalChargesOf(ctx, tx, scope, bookingID)
	if err != nil {
		return err
	}
	kind := ""
	for _, r := range rows {
		if r.ID == ch.ID {
			kind = r.LinkKind
		}
	}
	if kind != store.RentalChargeMain && kind != store.RentalChargeReturn {
		return nil
	}
	if kind == store.RentalChargeMain {
		for _, a := range adjustmentsOf(rows, ch.ID) {
			ok, err := s.d.Charges.CancelTx(ctx, tx, scope, actor, &a.Charge, "Rettifica del documento annullato")
			if err != nil {
				return err
			}
			if !ok {
				return charge.ErrImmutable
			}
		}
	}
	if err := store.SetRentalBookingChargeKind(ctx, tx, scope, bookingID, ch.ID, store.RentalChargeReplaced); err != nil {
		return err
	}
	return s.audit(ctx, tx, scope, actor, "rental_booking.charge_replaced", "rental_booking", bookingID, map[string]any{
		"charge_id": ch.ID, "kind": kind,
	})
}

// AdjustMainChargeTx follows a reprice of a booking (reschedule, extension) or a change of its customer or of
// invoice_requested. Without a main charge nothing is billed yet. An untouched main charge (unexported, unpaid,
// with untouched adjustments) is cancelled and re-created from the snapshot with the booking's current customer
// and fiscal mode (the link becomes replaced, D18). Otherwise the delta between the snapshot total and the main
// charge plus its adjustments becomes an adjustment linked to the main one (a negative delta is offset to the main
// charge's residual, then credited, m5-plan §3.5): exported charges never change (INV-51). A main charge that can
// no longer be replaced keeps its fiscal mode, so switching invoice_requested then is refused (validation
// invoice_requested charge_paid): one booking never mixes receipt and invoice charges.
func (s *Service) AdjustMainChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) (*store.Charge, error) {
	rows, err := store.RentalChargesOf(ctx, tx, scope, b.ID)
	if err != nil {
		return nil, err
	}
	main := liveCharge(rows, store.RentalChargeMain)
	if main == nil {
		return nil, nil
	}
	adjustments := adjustmentsOf(rows, main.ID)
	billed := money.Cents(main.TotalCents)
	for _, r := range adjustments {
		billed += money.Cents(r.TotalCents)
	}
	delta := snapshotOf(b).Total() - billed
	stale := main.FiscalMode != fiscalMode(b) || main.CustomerID != b.CustomerID
	if delta == 0 && !stale {
		return nil, nil
	}
	ok, err := replaceableTx(ctx, tx, scope, rows, main)
	if err != nil {
		return nil, err
	}
	if ok {
		if err := s.cancelMainTx(ctx, tx, scope, actor, main, "Sostituito dopo modifica della prenotazione"); err != nil {
			return nil, err
		}
		return s.mainChargeTx(ctx, tx, scope, actor, b)
	}
	if main.FiscalMode != fiscalMode(b) {
		return nil, invalid("invoice_requested", "charge_paid")
	}
	if delta == 0 {
		return nil, nil
	}
	unit, err := store.GetRentalUnit(ctx, tx, scope, b.UnitID)
	if err != nil {
		return nil, err
	}
	lines, err := s.chargeLines(ctx, tx, scope, b, []itemLine{{ItemID: unit.ItemID, Qty: 1000, Unit: delta, Amount: delta}})
	if err != nil {
		return nil, err
	}
	return s.createLinkedTx(ctx, tx, scope, actor, b, store.RentalChargeAdjustment, main.ID, lines)
}

// CancelMainChargeTx follows the cancellation of a booking (§6, D22): an untouched main charge is cancelled (its
// link becomes replaced, nothing reaches the export); an unpaid main charge that was already exported is offset in
// full by a negative linked adjustment (no fee in M11); a charge with payments stays for the office (refund or
// credit).
func (s *Service) CancelMainChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error {
	rows, err := store.RentalChargesOf(ctx, tx, scope, b.ID)
	if err != nil {
		return err
	}
	main := liveCharge(rows, store.RentalChargeMain)
	if main == nil || main.PaidAmountCents != 0 {
		return nil
	}
	ok, err := replaceableTx(ctx, tx, scope, rows, main)
	if err != nil {
		return err
	}
	if ok {
		return s.cancelMainTx(ctx, tx, scope, actor, main, "Prenotazione annullata")
	}
	residual := money.Cents(main.TotalCents - main.CreditedCents)
	for _, r := range adjustmentsOf(rows, main.ID) {
		residual += money.Cents(r.TotalCents)
	}
	if residual <= 0 {
		return nil
	}
	unit, err := store.GetRentalUnit(ctx, tx, scope, b.UnitID)
	if err != nil {
		return err
	}
	lines, err := s.chargeLines(ctx, tx, scope, b, []itemLine{{ItemID: unit.ItemID, Qty: 1000, Unit: -residual, Amount: -residual}})
	if err != nil {
		return err
	}
	_, err = s.createLinkedTx(ctx, tx, scope, actor, b, store.RentalChargeAdjustment, main.ID, lines)
	return err
}
