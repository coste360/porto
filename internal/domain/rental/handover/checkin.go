package handover

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Check-in (m11-plan §8, §8.1, §5.4-§5.5): the return charge (late fee + metered fuel + extras used + damage excess)
// through charge.CreateTx linked as kind return (INV-99), the deposit decision (cash and card POS: retention through
// payment.RetainDepositTx and release through payment.RefundTx; pre-authorisation: capture or void after commit; a
// deposit is never revenue until retained, INV-98), damages charged or waived, the unit's engine hours,
// reservation.ReturnTx and, when nothing is pending, reservation.CloseTx, all in one transaction.

// returnPlan is the computed return of a booking.
type returnPlan struct {
	preview  CheckInPreview
	lines    []itemLine
	retain   money.Cents
	deposit  money.Cents
	damages  []store.RentalDamage // open damages, in order
	waived   map[string]bool
	returned time.Time
}

func checkCheckIn(in *CheckInInput) error {
	v := &auth.ValidationError{}
	switch in.DepositDecision {
	case DecisionRelease, DecisionRetainAll, DecisionRetain:
	default:
		v.Add("deposit_decision", "invalid")
	}
	if in.Retain < 0 {
		v.Add("retain_cents", "positive")
	}
	if len(in.Extras) > 20 {
		v.Add("extras", "count")
	}
	for _, e := range in.Extras {
		if e.Qty <= 0 {
			v.Add("extras.qty_milli", "positive")
		}
	}
	if len(in.WaiveDamageIDs) > 20 {
		v.Add("waive_damage_ids", "count")
	}
	in.WaiveReason = strings.TrimSpace(in.WaiveReason)
	if utf8.RuneCountInString(in.WaiveReason) > 200 {
		v.Add("waive_reason", "too_long")
	}
	if len(in.WaiveDamageIDs) > 0 && in.WaiveReason == "" {
		v.Add("waive_reason", "required")
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 500 {
		v.Add("note", "too_long")
	}
	return v.Err()
}

// heldDeposit is the deposit a decision applies to (0 when waived, pending or none).
func heldDeposit(b *store.RentalBooking) money.Cents {
	switch b.DepositStatus {
	case store.RentalDepositStatusHeld, store.RentalDepositStatusAuthorized:
		return money.Cents(b.DepositCents)
	}
	return 0
}

// computeReturn prices the return of a checked-out booking (no writes).
func (s *Service) computeReturn(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, in CheckInInput) (*returnPlan, error) {
	loc := s.location(ctx, db, scope)
	rp := &returnPlan{returned: store.UTC(in.ReturnedAt), waived: map[string]bool{}}
	if rp.returned.IsZero() {
		rp.returned = s.now()
	}
	rp.returned = rp.returned.Truncate(time.Second)
	formula, err := store.GetRentalFormula(ctx, db, scope, b.FormulaID)
	if err != nil {
		return nil, err
	}
	unit, err := store.GetRentalUnit(ctx, db, scope, b.UnitID)
	if err != nil {
		return nil, err
	}
	set, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	pv := &rp.preview
	// Late fee: real elapsed minutes after the planned end (§5.4).
	if formula.LateItemID != "" {
		hour, err := s.d.Charges.ItemPrice(ctx, db, scope, formula.LateItemID, b.EndsOn)
		if err != nil {
			return nil, err
		}
		lf := price.LateFee(slots.Instant(b.EndsOn, b.EndSlot, loc), rp.returned, formula.GraceMinutes, formula.LateStepMinutes, hour)
		pv.LateMinutes, pv.LateFee = max(lf.LateMinutes, 0), lf.Fee
		if lf.Fee > 0 {
			q := qty.Milli(int64(lf.Blocks) * int64(formula.LateStepMinutes) * 1000 / 60)
			rp.lines = append(rp.lines, itemLine{ItemID: formula.LateItemID, Qty: q, Unit: hour, Amount: lf.Fee})
			pv.Lines = append(pv.Lines, PreviewLine{Kind: "late_fee", ItemID: formula.LateItemID, Qty: q, UnitCents: hour, Amount: lf.Fee})
		}
	}
	// Metered fuel (§5.5).
	if formula.Fuel == store.RentalFuelMetered && formula.FuelItemID != "" {
		out, err := store.GetRentalHandover(ctx, db, scope, b.ID, store.RentalPhaseCheckout)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		back, err := store.GetRentalHandover(ctx, db, scope, b.ID, store.RentalPhaseCheckin)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if out != nil && back != nil && out.FuelEighths != nil && (back.FuelEighths != nil || back.RefuelMilli != nil) {
			unitPrice, err := s.d.Charges.ItemPrice(ctx, db, scope, formula.FuelItemID, calendar.In(rp.returned, loc))
			if err != nil {
				return nil, err
			}
			fin := price.FuelInput{OutEighths: *out.FuelEighths, Tank: qty.Milli(unit.TankMilli), Refuel: qtyPtr(back.RefuelMilli), UnitCents: unitPrice}
			fin.InEighths = fin.OutEighths
			if back.FuelEighths != nil {
				fin.InEighths = *back.FuelEighths
			}
			fr := price.Fuel(fin)
			pv.FuelLitres, pv.Fuel = fr.Litres, fr.Amount
			if fr.Amount > 0 {
				rp.lines = append(rp.lines, itemLine{ItemID: formula.FuelItemID, Qty: fr.Litres, Unit: unitPrice, Amount: fr.Amount})
				pv.Lines = append(pv.Lines, PreviewLine{Kind: "fuel", ItemID: formula.FuelItemID, Qty: fr.Litres, UnitCents: unitPrice, Amount: fr.Amount})
			}
		}
	}
	// Extras used beyond the booked ones, priced on the start date (§5.5).
	days := price.Days(pricing.RentalKind(formula.Kind), slots.Days(bookingRange(b)))
	for _, e := range in.Extras {
		ex, err := store.GetRentalExtra(ctx, db, scope, e.ExtraID)
		if err != nil {
			return nil, notFound(err)
		}
		unitPrice, err := s.d.Charges.ItemPrice(ctx, db, scope, ex.ItemID, b.StartsOn)
		if err != nil {
			return nil, err
		}
		q := e.Qty
		if ex.ChargeMode == store.RentalExtraPerDay && days > 1 {
			if q, err = q.MulInt(int64(days)); err != nil {
				return nil, invalid("extras.qty_milli", "overflow")
			}
		}
		amount, err := q.MulCents(unitPrice)
		if err != nil {
			return nil, invalid("extras.qty_milli", "overflow")
		}
		pv.Extras += amount
		rp.lines = append(rp.lines, itemLine{ItemID: ex.ItemID, Qty: q, Unit: unitPrice, Amount: amount})
		pv.Lines = append(pv.Lines, PreviewLine{Kind: "extra", ItemID: ex.ItemID, Qty: q, UnitCents: unitPrice, Amount: amount})
	}
	// Damages and the deposit decision (§5.5, D17).
	all, err := store.RentalDamagesOf(ctx, db, scope, b.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range in.WaiveDamageIDs {
		rp.waived[id] = true
	}
	for _, d := range all {
		if d.Status != store.RentalDamageOpen {
			continue
		}
		rp.damages = append(rp.damages, d)
		if !rp.waived[d.ID] {
			pv.DamageEstimate += money.Cents(d.EstimateCents)
		}
	}
	for id := range rp.waived {
		if !slices.ContainsFunc(rp.damages, func(d store.RentalDamage) bool { return d.ID == id }) {
			return nil, invalid("waive_damage_ids", "unknown")
		}
	}
	rp.deposit = heldDeposit(b)
	switch in.DepositDecision {
	case DecisionRetainAll:
		rp.retain = rp.deposit
	case DecisionRetain:
		rp.retain = in.Retain
	}
	tot, err := price.Return(price.ReturnInput{
		LateFee: pv.LateFee, Fuel: pv.Fuel, Extras: pv.Extras, Deposit: rp.deposit, Estimates: pv.DamageEstimate, Retained: rp.retain,
	})
	if err != nil {
		if errors.Is(err, price.ErrDepositExceeds) {
			return nil, ErrDepositExceeds
		}
		return nil, err
	}
	pv.DamageExcess, pv.ReturnTotal, pv.Deposit, pv.DepositProposal = tot.DamageExcess, tot.Total, rp.deposit, tot.Proposal
	if tot.DamageExcess > 0 {
		if set.DamageItemID == "" {
			return nil, charge.ErrBillingItemMissing
		}
		rp.lines = append(rp.lines, itemLine{ItemID: set.DamageItemID, Qty: 1000, Unit: tot.DamageExcess, Amount: tot.DamageExcess})
		pv.Lines = append(pv.Lines, PreviewLine{Kind: "damage", ItemID: set.DamageItemID, Qty: 1000, UnitCents: tot.DamageExcess, Amount: tot.DamageExcess})
	}
	if err := s.itemCodes(ctx, db, scope, pv.Lines); err != nil {
		return nil, err
	}
	return rp, nil
}

func (s *Service) itemCodes(ctx context.Context, db bun.IDB, scope store.MarinaScope, lines []PreviewLine) error {
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ItemID)
	}
	items, err := store.ItemsByIDs(ctx, db, scope, ids)
	if err != nil {
		return err
	}
	codes := map[string]string{}
	for _, it := range items {
		codes[it.ID] = it.Code
	}
	for i := range lines {
		lines[i].ItemCode = codes[lines[i].ItemID]
	}
	return nil
}

// PreviewCheckIn computes the return totals without writing.
func (s *Service) PreviewCheckIn(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in CheckInInput) (*CheckInPreview, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if in.DepositDecision == "" {
		in.DepositDecision = DecisionRelease
	}
	if err := checkCheckIn(&in); err != nil {
		return nil, err
	}
	b, err := store.GetRentalBooking(ctx, s.d.DB.DB, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	if b.Status != store.RentalStatusCheckedOut {
		return nil, ErrInvalidTransition
	}
	rp, err := s.computeReturn(ctx, s.d.DB.DB, scope, b, in)
	if err != nil {
		return nil, err
	}
	if rp.preview.Lines == nil {
		rp.preview.Lines = []PreviewLine{}
	}
	return &rp.preview, nil
}

// CheckIn takes the boat back (If-Match, one transaction, m11-plan §8.1).
func (s *Service) CheckIn(ctx context.Context, p *auth.Principal, marinaID, bookingID string, ifMatch int, in CheckInInput) (*reservation.BookingView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if err := checkCheckIn(&in); err != nil {
		return nil, err
	}
	now := s.now()
	if in.ReturnedAt.IsZero() {
		in.ReturnedAt = now
	}
	if in.ReturnedAt.After(now.Add(OfflineAfter)) {
		return nil, invalid("returned_at", "in_future")
	}
	set, err := store.LoadRentalSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	actor := p.Actor()
	var out *store.RentalBooking
	var capture *money.Cents
	var intentID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		capture, intentID = nil, ""
		b, err := s.lock(ctx, tx, scope, bookingID, ifMatch, store.RentalStatusCheckedOut)
		if err != nil {
			return err
		}
		if store.UTC(in.ReturnedAt).Before(b.CheckedOutAt) {
			return invalid("returned_at", "before_check_out")
		}
		d, err := s.loadDraft(ctx, tx, scope, b, store.RentalPhaseCheckin)
		if err != nil {
			return err
		}
		if missing := d.missing(store.RentalPhaseCheckin); len(missing) > 0 {
			return &IncompleteError{Missing: missing}
		}
		out0, err := store.GetRentalHandover(ctx, tx, scope, b.ID, store.RentalPhaseCheckout)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if hasEngine(d.unit) && out0 != nil && out0.EngineHoursMilli != nil && *d.handover.EngineHoursMilli < *out0.EngineHoursMilli {
			return ErrEngineHoursDecreasing
		}
		rp, err := s.computeReturn(ctx, tx, scope, b, in)
		if err != nil {
			return err
		}
		// 2. Return charge.
		lines, err := s.chargeLines(ctx, tx, scope, b, rp.lines)
		if err != nil {
			return err
		}
		if _, err := s.createLinkedTx(ctx, tx, scope, actor, b, store.RentalChargeReturn, "", lines); err != nil {
			return err
		}
		if len(in.Extras) > 0 {
			rows := make([]store.RentalBookingExtra, 0, len(in.Extras))
			for _, e := range in.Extras {
				rows = append(rows, store.RentalBookingExtra{
					ID: store.NewID(), MarinaID: scope.ID(), BookingID: b.ID, ExtraID: e.ExtraID, QtyMilli: int64(e.Qty),
					Source: store.RentalExtraSourceReturn, CreatedAt: now,
				})
			}
			if err := store.ReplaceRentalBookingExtras(ctx, tx, scope, b.ID, store.RentalExtraSourceReturn, rows, now); err != nil {
				return err
			}
		}
		// 3. Deposit decision (INV-98).
		if err := s.settleDepositTx(ctx, tx, scope, actor, b, rp, set); err != nil {
			return err
		}
		if b.DepositMode == store.RentalDepositPreauth && b.DepositStatus == store.RentalDepositStatusCapturePending {
			r := money.Cents(*b.DepositDecisionCents)
			capture, intentID = &r, b.DepositIntentID
		}
		// 4. Damages: charged (retained share first, in order) or waived.
		left := rp.retain
		for i := range rp.damages {
			dm := &rp.damages[i]
			if rp.waived[dm.ID] {
				dm.Status, dm.WaiveReason = store.RentalDamageWaived, in.WaiveReason
			} else {
				share := min(money.Cents(dm.EstimateCents), left)
				left -= share
				dm.Status, dm.RetainedCents, dm.ChargedCents = store.RentalDamageCharged, int64(share), dm.EstimateCents-int64(share)
			}
			if err := store.UpdateRentalDamage(ctx, tx, scope, dm, now); err != nil {
				return err
			}
		}
		// 5. Unit engine hours, return, handover document.
		if hasEngine(d.unit) && d.handover.EngineHoursMilli != nil {
			if err := store.RaiseRentalUnitEngineHours(ctx, tx, scope, d.unit.ID, *d.handover.EngineHoursMilli, now); err != nil {
				return err
			}
		}
		if out, err = s.d.Reservations.ReturnTx(ctx, tx, scope, actor, b.ID, ifMatch, rp.returned); err != nil {
			return err
		}
		h := d.handover
		h.ActualAt, h.CompletedAt, h.CompletedByUserID = rp.returned, now, userOf(actor)
		if in.Note != "" {
			h.Notes = strings.TrimSpace(h.Notes + "\n" + in.Note)
		}
		if err := store.UpdateRentalHandover(ctx, tx, scope, h, now); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, actor, "rental_handover.complete", "rental_handover", h.ID, map[string]any{
			"booking_id": b.ID, "phase": store.RentalPhaseCheckin, "actual_at": rp.returned, "late_minutes": rp.preview.LateMinutes,
			"return_total_cents": int64(rp.preview.ReturnTotal), "deposit_decision": in.DepositDecision,
			"retained_cents": int64(rp.retain),
		}); err != nil {
			return err
		}
		if _, err := s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
			Kind: docs.KindRentalHandover, SubjectType: docs.SubjectRentalBooking, SubjectID: h.ID, CreatedBy: userOf(actor), At: rp.returned,
		}); err != nil {
			return err
		}
		// 6. Close when the deposit is final.
		cur, err := store.GetRentalBooking(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		if err := s.closeIfDoneTx(ctx, tx, scope, actor, cur); err != nil {
			return err
		}
		out, err = store.GetRentalBooking(ctx, tx, scope, b.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if capture != nil {
		if err := s.d.Collect.CaptureIntentAmount(ctx, scope, intentID, *capture); err != nil {
			s.d.Log.WarnContext(ctx, "rental deposit settlement deferred to the job", "booking_id", bookingID, "intent_id", intentID)
		}
	}
	return s.view(ctx, p, marinaID, out)
}

// settleDepositTx applies the deposit decision of a check-in to the locked booking b (and updates b in memory).
func (s *Service) settleDepositTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking, rp *returnPlan, set *store.RentalSettings) error {
	r, dep := rp.retain, rp.deposit
	fields := map[string]any{}
	switch {
	case b.DepositStatus == store.RentalDepositStatusHeld && (b.DepositMode == store.RentalDepositCash || b.DepositMode == store.RentalDepositCardPOS):
		pay, err := store.GetPayment(ctx, tx, scope, b.DepositPaymentID)
		if err != nil {
			return err
		}
		if r > 0 {
			if _, err := s.d.Payments.RetainDepositTx(ctx, tx, scope, actor, payment.RetainDepositInput{
				DepositPaymentID: pay.ID, Amount: r, FiscalMode: fiscalMode(b), CustomerID: b.CustomerID, BookingID: b.ID,
				ItemID: s.retentionItem(ctx, tx, scope, set), Reason: "Trattenuta cauzione noleggio",
			}); err != nil {
				return err
			}
		}
		if rel := dep - r; rel > 0 {
			if _, err := s.d.Payments.RefundTx(ctx, tx, scope, actor, pay.ID, payment.RefundInput{
				Amount: rel, Method: pay.Method, Reason: "Restituzione cauzione noleggio", CashRegisterID: set.DefaultRegisterID,
			}); err != nil {
				return err
			}
		}
		fields["deposit_status"], fields["deposit_retained_cents"] = retainedStatus(dep, r), int64(r)
	case b.DepositMode == store.RentalDepositPreauth && b.DepositStatus == store.RentalDepositStatusAuthorized:
		fields["deposit_status"], fields["deposit_decision_cents"] = store.RentalDepositStatusCapturePending, int64(r)
	case b.DepositMode == store.RentalDepositPreauth && b.DepositStatus == store.RentalDepositStatusPending:
		fields["deposit_status"] = store.RentalDepositStatusReleased // never authorised: nothing to release
	default:
		if r > 0 {
			return ErrDepositExceeds
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, fields, s.now()); err != nil {
		return err
	}
	if st, ok := fields["deposit_status"].(string); ok {
		b.DepositStatus = st
	}
	if v, ok := fields["deposit_decision_cents"].(int64); ok {
		b.DepositDecisionCents = &v
	}
	if v, ok := fields["deposit_retained_cents"].(int64); ok {
		b.DepositRetainedCents = v
	}
	return s.audit(ctx, tx, scope, actor, "rental_booking.deposit", "rental_booking", b.ID, map[string]any{
		"status": b.DepositStatus, "retained_cents": int64(r), "released_cents": int64(dep - r),
	})
}

// Close closes a returned booking whose deposit is final (If-Match).
func (s *Service) Close(ctx context.Context, p *auth.Principal, marinaID, bookingID string, ifMatch int) (*reservation.BookingView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	var out *store.RentalBooking
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := s.lock(ctx, tx, scope, bookingID, ifMatch, store.RentalStatusReturned)
		if err != nil {
			return err
		}
		if !depositFinal(b) {
			return &IncompleteError{Missing: []string{MissingDeposit}}
		}
		out, err = s.d.Reservations.CloseTx(ctx, tx, scope, p.Actor(), b.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, p, marinaID, out)
}

// retentionItem is the item of a deposit retention: the deposit_retention origin item of the billing settings, else
// the damage item of the rental settings ("" lets payment answer billing_item_missing).
func (s *Service) retentionItem(ctx context.Context, db bun.IDB, scope store.MarinaScope, set *store.RentalSettings) string {
	if id, err := store.LedgerOriginItemID(ctx, db, scope, "deposit_retention"); err == nil && id != "" {
		return id
	}
	return set.DamageItemID
}
