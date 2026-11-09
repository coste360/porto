package charge

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Charges of contracts, transits and holder instalments (m5-plan §3.3): the hook implementations called by the
// contract and rights services in their transactions, the true-up and the sync of an origin.

var (
	_ contract.BillingHooks = (*Service)(nil)
	_ rights.BillingHooks   = (*Service)(nil)
)

// billingCustomer is the customer a contract charge is addressed to.
func billingCustomer(c *store.Contract) string {
	if c.BillingCustomerID != "" {
		return c.BillingCustomerID
	}
	return c.CustomerID
}

// ContractTargetOf returns the billable target of a contract or transit (chargeplan.ContractTarget over its
// snapshot breakdown and adjustments). A contract without a snapshot has one line of the contract_trueup_fallback
// origin item for its whole total (ErrBillingItemMissing when not configured).
func (s *Service) ContractTargetOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Contract) (chargeplan.Target, error) {
	snap := contract.DecodeSnapshot(c.PriceSnapshot)
	if snap == nil {
		if c.TotalCents == 0 {
			return chargeplan.Target{}, nil
		}
		l, err := s.originLine(ctx, db, scope, store.OriginItemTrueUpFallback, money.Cents(c.TotalCents), c.StartsOn, c.EndsOn.AddDays(-1))
		if err != nil {
			return chargeplan.Target{}, err
		}
		l.Key = "contract"
		return chargeplan.Target{Lines: []chargeplan.Line{l}}, nil
	}
	return chargeplan.ContractTarget(snap.Breakdown, planAdjustments(snap), money.Cents(c.TotalCents))
}

// planAdjustments maps the snapshot adjustments (INV-27) to the planner's: moves and vessel changes concern the
// berth lines, early payments and addenda every line.
func planAdjustments(snap *contract.PriceSnapshot) []chargeplan.Adjustment {
	var berth []string
	for _, l := range snap.Breakdown.Lines {
		if l.Kind == pricing.LineBerth && l.Gross != 0 {
			berth = append(berth, chargeplan.SnapKey(l.Seq))
		}
	}
	out := make([]chargeplan.Adjustment, 0, len(snap.Adjustments))
	for _, a := range snap.Adjustments {
		pa := chargeplan.Adjustment{Kind: a.Kind, AddendumID: a.AddendumID, DeltaCents: money.Cents(a.NewCents - a.OldCents)}
		switch a.Kind {
		case contract.AdjustmentMove, contract.AdjustmentMoveUndo, contract.AdjustmentVesselChange:
			pa.LineKeys = berth
		}
		out = append(out, pa)
	}
	return out
}

// plannedOrigins are the origins whose lines count against a contract's target (system lines carry a plan key).
var plannedOrigins = []string{store.ChargeOriginInstallment, store.ChargeOriginContractTrueUp, store.ChargeOriginTransit, store.ChargeOriginAdjustment}

// chargedOfContract returns the planned lines already charged for a contract (non-cancelled charges of the
// contract with a plan key) and the charges themselves.
func chargedOfContract(ctx context.Context, db bun.IDB, scope store.MarinaScope, contractID string) ([]chargeplan.ChargedLine, []store.Charge, error) {
	all, err := store.ChargesOfContract(ctx, db, scope, contractID)
	if err != nil {
		return nil, nil, err
	}
	var live []store.Charge
	var ids []string
	for _, c := range all {
		if c.PaymentStatus == store.ChargeCancelled || !contains(plannedOrigins, c.OriginType) {
			continue
		}
		live = append(live, c)
		ids = append(ids, c.ID)
	}
	lines, err := store.ChargeLinesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	return chargedOf(lines), live, nil
}

// OnInstallmentIssuedTx creates the charge of an issued instalment (m5-plan §3.3): the instalment amount split
// over the contract's remaining target, installments.charge_id set once, installment.charged, the payment request
// (dedupe key installment:<id>:issued) and, after the last live instalment, the contract true-up. A target that
// does not match the contract total creates nothing (logged with ids; the instalment stays without charge and the
// backfill retries once the contract is fixed).
func (s *Service) OnInstallmentIssuedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, inst *store.Installment) error {
	if inst.AmountCents <= 0 || inst.ChargeID != "" {
		return nil
	}
	ch, created, err := s.installmentChargeTx(ctx, tx, scope, actor, c, inst, calendar.Date{})
	if err != nil || ch == nil {
		return err
	}
	if s.requester != nil && created {
		bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		if bs.AutoPaymentRequest {
			if err := s.requester.RequestForChargeTx(ctx, tx, scope, actor, ch, "installment:"+inst.ID+":issued"); err != nil {
				return err
			}
		}
	}
	return s.trueUpIfLastTx(ctx, tx, scope, actor, c)
}

// installmentChargeTx creates (or links) the charge of an instalment; issuedOn zero = today. It returns nil when
// no charge was created (zero amount, target mismatch); created is false when an existing charge was linked.
func (s *Service) installmentChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, inst *store.Installment, issuedOn calendar.Date) (*store.Charge, bool, error) {
	if existing, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginInstallment, inst.ID); err == nil {
		if _, err := store.SetInstallmentCharge(ctx, tx, scope, inst.ID, existing.ID, s.now()); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	target, err := s.ContractTargetOf(ctx, tx, scope, c)
	if errors.Is(err, chargeplan.ErrTargetMismatch) || errors.Is(err, ErrBillingItemMissing) {
		return nil, false, s.notBilledTx(ctx, tx, scope, err, inst.ID, contractOccurrence(c), "charge: instalment not charged",
			"contract_id", c.ID, "installment_id", inst.ID)
	}
	if err != nil {
		return nil, false, err
	}
	charged, _, err := chargedOfContract(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, false, err
	}
	parts, err := chargeplan.Split(chargeplan.Remaining(target, charged), money.Cents(inst.AmountCents))
	if err != nil {
		return nil, false, err
	}
	desc := s.describe(ctx, tx, scope, "installment", contract.Code(c), strconv.Itoa(inst.Seq))
	ch, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: store.ChargeOriginInstallment, OriginID: inst.ID, CustomerID: billingCustomer(c), ContractID: c.ID,
		VesselID: c.VesselID, IssuedOn: issuedOn, DueOn: inst.DueOn, Description: desc, Lines: s.describeLines(ctx, tx, scope, linesFromParts(parts), desc),
	})
	if err != nil || ch == nil {
		return nil, false, err
	}
	if ok, err := store.SetInstallmentCharge(ctx, tx, scope, inst.ID, ch.ID, s.now()); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, fmt.Errorf("charge: instalment %s already charged", inst.ID)
	}
	inst.ChargeID = ch.ID
	if err := s.emit(ctx, tx, scope, event.InstallmentCharged, event.InstallmentChargedData{
		InstallmentID: inst.ID, ContractID: c.ID, ChargeID: ch.ID,
	}); err != nil {
		return nil, false, err
	}
	return ch, true, nil
}

// notBilledTx records, in the caller's transaction, that money of a source (instalment, holder instalment,
// contract or transit) could not be charged: a worklist item of kind billing_target_mismatch or
// billing_item_missing (m5-plan §3.3, §16 D26) whose source id is the source, plus an ids-only log line. occurrence
// keys the occurrence (contractOccurrence for a contract's money): the same signal again leaves a closed item
// closed, a new one reopens it.
func (s *Service) notBilledTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, cause error, sourceID, occurrence, msg string, attrs ...any) error {
	reason := reasonOf(cause)
	s.d.Log.Warn(msg, append([]any{"marina_id", scope.ID(), "reason", reason}, attrs...)...)
	return store.RaiseWorkItem(ctx, tx, scope, reason, sourceID, occurrence, s.now())
}

// contractOccurrence keys a not-billed signal of a contract by its version: a mismatch raised again after the
// contract has changed is a new occurrence.
func contractOccurrence(c *store.Contract) string {
	return "v" + strconv.Itoa(c.Version)
}

func reasonOf(err error) string {
	switch {
	case errors.Is(err, chargeplan.ErrTargetMismatch):
		return "billing_target_mismatch"
	case errors.Is(err, ErrBillingItemMissing):
		return "billing_item_missing"
	}
	return "error"
}

// trueUpIfLastTx charges the remaining target of a contract when no scheduled instalment is left and the contract
// has at least one live instalment charge.
func (s *Service) trueUpIfLastTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract) error {
	rows, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return err
	}
	charged := false
	for _, r := range rows {
		if r.Status == store.InstallmentScheduled {
			return nil
		}
		if r.ChargeID != "" {
			charged = true
		}
	}
	if !charged {
		return nil
	}
	return s.trueUpTx(ctx, tx, scope, actor, c)
}

// trueUpTx charges what remains of a contract's target (positive, or negative as an adjustment applied at
// creation), linked to the latest charge of the contract (m5-plan §3.3 contract_trueup).
func (s *Service) trueUpTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract) error {
	target, err := s.ContractTargetOf(ctx, tx, scope, c)
	if errors.Is(err, chargeplan.ErrTargetMismatch) || errors.Is(err, ErrBillingItemMissing) {
		return s.notBilledTx(ctx, tx, scope, err, c.ID, contractOccurrence(c), "charge: true-up skipped", "contract_id", c.ID)
	}
	if err != nil {
		return err
	}
	charged, live, err := chargedOfContract(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	parts := chargeplan.All(chargeplan.Remaining(target, charged))
	if sumParts(parts) == 0 {
		return nil
	}
	latest := ""
	if len(live) > 0 {
		latest = live[len(live)-1].ID
	}
	bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
	if err != nil {
		return err
	}
	today := s.today(ctx, tx, scope)
	desc := s.describe(ctx, tx, scope, "trueup", contract.Code(c), "")
	_, _, err = s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: store.ChargeOriginContractTrueUp, OriginID: c.ID, CustomerID: billingCustomer(c), ContractID: c.ID,
		VesselID: c.VesselID, DueOn: today.AddDays(bs.DefaultDueDays), CorrectsChargeID: latest, Description: desc,
		Lines: s.describeLines(ctx, tx, scope, linesFromParts(parts), desc),
	})
	return err
}

func sumParts(parts []chargeplan.Part) money.Cents {
	var t money.Cents
	for _, p := range parts {
		t += p.Gross
	}
	return t
}

// OnTransitConfirmedTx creates the charge of a confirmed transit (one charge for the whole stay), due on arrival
// or, with pay at departure, on departure (never before today). An already charged transit is synced instead.
func (s *Service) OnTransitConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract) error {
	if _, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTransit, c.ID); err == nil {
		return s.syncTransitTx(ctx, tx, scope, actor, c)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	target, err := s.ContractTargetOf(ctx, tx, scope, c)
	if errors.Is(err, chargeplan.ErrTargetMismatch) || errors.Is(err, ErrBillingItemMissing) {
		return s.notBilledTx(ctx, tx, scope, err, c.ID, contractOccurrence(c), "charge: transit not charged", "contract_id", c.ID)
	}
	if err != nil {
		return err
	}
	parts := chargeplan.All(chargeplan.Remaining(target, nil))
	if sumParts(parts) <= 0 {
		return nil // a free stay has no charge
	}
	due := c.StartsOn
	if c.PayAtDeparture {
		due = c.EndsOn
	}
	if today := s.today(ctx, tx, scope); due.Before(today) {
		due = today
	}
	desc := s.describe(ctx, tx, scope, "transit", contract.Code(c), "")
	_, _, err = s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: store.ChargeOriginTransit, OriginID: c.ID, CustomerID: billingCustomer(c), ContractID: c.ID,
		VesselID: c.VesselID, DueOn: due, Description: desc, Lines: s.describeLines(ctx, tx, scope, linesFromParts(parts), desc),
	})
	return err
}

// OnContractRepricedTx keeps the charges of a repriced contract in line: a charged transit is synced; a contract
// whose instalments are all issued gets a true-up; otherwise the next instalments absorb the new total.
func (s *Service) OnContractRepricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, _ contract.RepriceReason) error {
	if c.Kind == contract.KindTransit {
		if _, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTransit, c.ID); errors.Is(err, store.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		return s.syncTransitTx(ctx, tx, scope, actor, c)
	}
	return s.trueUpIfLastTx(ctx, tx, scope, actor, c)
}

// OnContractClosedTx handles a cancelled or no-show transit after confirmation: its charge is cancelled while
// still cancellable, else its charged amount is reversed by a negative adjustment (offset, then credit). A
// terminated contract is charged by staff (createTerminationCharge).
func (s *Service) OnContractClosedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, change contract.CloseChange) error {
	if c.Kind != contract.KindTransit || change == contract.CloseTerminated {
		return nil
	}
	primary, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTransit, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	all, err := store.ChargesOfOrigin(ctx, tx, scope, store.ChargeOriginTransit, c.ID, true)
	if err != nil {
		return err
	}
	if len(all) == 1 {
		ok, err := s.cancelIfPossibleTx(ctx, tx, scope, actor, primary, string(change))
		if err != nil || ok {
			return err
		}
	}
	return s.syncToTargetTx(ctx, tx, scope, actor, c, chargeplan.Target{}, all, primary, string(change))
}

// syncTransitTx brings the charges of a transit in line with its current target.
func (s *Service) syncTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract) error {
	target, err := s.ContractTargetOf(ctx, tx, scope, c)
	if errors.Is(err, chargeplan.ErrTargetMismatch) || errors.Is(err, ErrBillingItemMissing) {
		return s.notBilledTx(ctx, tx, scope, err, c.ID, contractOccurrence(c), "charge: transit sync skipped", "contract_id", c.ID)
	}
	if err != nil {
		return err
	}
	all, err := store.ChargesOfOrigin(ctx, tx, scope, store.ChargeOriginTransit, c.ID, true)
	if err != nil {
		return err
	}
	primary, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTransit, c.ID)
	if err != nil {
		return err
	}
	return s.syncToTargetTx(ctx, tx, scope, actor, c, target, all, primary, "reprice")
}

// SyncOriginTx brings the charges of an origin (transit, contract true-up) in line with its current target:
// rewrite the latest unexported unpaid charge in place, or add a linked adjustment (m5-plan §3.5).
func (s *Service) SyncOriginTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, originType, originID string) error {
	c, err := store.GetContract(ctx, tx, scope, originID)
	if err != nil {
		return notFound(err)
	}
	switch originType {
	case store.ChargeOriginTransit:
		return s.syncTransitTx(ctx, tx, scope, actor, c)
	case store.ChargeOriginContractTrueUp, store.ChargeOriginInstallment:
		return s.trueUpIfLastTx(ctx, tx, scope, actor, c)
	}
	return fmt.Errorf("charge: no sync for origin %s", originType)
}

// syncToTargetTx makes the planned lines of the given charges equal the target: when the primary is still
// editable (to_invoice, unpaid, not offset, not corrected) its lines are rewritten in place; otherwise a delta
// adjustment linked to the primary charges the difference.
func (s *Service) syncToTargetTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract,
	target chargeplan.Target, all []store.Charge, primary *store.Charge, reason string,
) error {
	ids := make([]string, 0, len(all))
	for _, ch := range all {
		ids = append(ids, ch.ID)
	}
	lines, err := store.ChargeLinesOf(ctx, tx, scope, ids)
	if err != nil {
		return err
	}
	rem := chargeplan.Remaining(target, chargedOf(lines))
	parts := chargeplan.All(rem)
	if len(parts) == 0 {
		return nil
	}
	if len(all) == 1 {
		editable, err := s.editable(ctx, tx, scope, primary)
		if err != nil {
			return err
		}
		if editable {
			var others []store.ChargeLine
			for _, l := range lines {
				if l.ChargeID != primary.ID {
					others = append(others, l)
				}
			}
			own := chargeplan.All(chargeplan.Remaining(target, chargedOf(others)))
			if sumParts(own) > 0 {
				return s.rewriteTx(ctx, tx, scope, actor, primary, s.describeLines(ctx, tx, scope, linesFromParts(own), primary.Description))
			}
			if sumParts(own) == 0 && len(own) == 0 {
				_, err := s.cancelIfPossibleTx(ctx, tx, scope, actor, primary, reason)
				return err
			}
		}
	}
	if sumParts(parts) == 0 {
		// Per-line differences that cancel out cannot be a charge (total 0): the office has to fix it by hand.
		return s.notBilledTx(ctx, tx, scope, chargeplan.ErrTargetMismatch, c.ID, contractOccurrence(c), "charge: per-line differences cancel out",
			"contract_id", c.ID, "charge_id", primary.ID)
	}
	desc := s.describe(ctx, tx, scope, "adjustment", primary.Number, "")
	_, _, err = s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: store.ChargeOriginAdjustment, OriginID: primary.ID, CustomerID: primary.CustomerID,
		ContractID: primary.ContractID, VesselID: primary.VesselID, CorrectsChargeID: primary.ID, CorrectionKind: "delta",
		FiscalMode: primary.FiscalMode, Description: desc, Lines: s.describeLines(ctx, tx, scope, linesFromParts(parts), desc),
		DueOn: s.dueDefault(ctx, tx, scope),
	})
	return err
}

// dueDefault is today + the default due days.
func (s *Service) dueDefault(ctx context.Context, tx bun.IDB, scope store.MarinaScope) calendar.Date {
	today := s.today(ctx, tx, scope)
	bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
	if err != nil {
		return today.AddDays(30)
	}
	return today.AddDays(bs.DefaultDueDays)
}

// editable reports whether a charge can be edited in place: to_invoice, open, unpaid, not offset and not corrected.
func (s *Service) editable(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ch *store.Charge) (bool, error) {
	if ch.InvoicingStatus != store.ChargeToInvoice || ch.PaymentStatus != store.ChargeOpen || ch.PaidAmountCents != 0 || ch.CreditedCents != 0 || ch.TotalCents <= 0 {
		return false, nil
	}
	matched, err := store.HasMatches(ctx, tx, scope, ch.ID)
	if err != nil || matched {
		return false, err
	}
	corr, err := store.CorrectionsOf(ctx, tx, scope, ch.ID)
	if err != nil {
		return false, err
	}
	for _, c := range corr {
		if c.PaymentStatus != store.ChargeCancelled {
			return false, nil
		}
	}
	return true, nil
}

// rewriteTx replaces the lines of an editable charge (version + 1, audit charge.update, charge.changed).
func (s *Service) rewriteTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, in []LineInput) error {
	lines, t, err := buildLines(scope, ch.ID, in, s.now())
	if err != nil {
		return err
	}
	if t.gross <= 0 {
		return fmt.Errorf("charge: rewrite to a non-positive total")
	}
	err = store.EditCharge(ctx, tx, scope, ch.ID, ch.Version, store.ChargeEdit{
		Lines: lines, Total: int64(t.gross), Net: int64(t.net), VAT: int64(t.vat), ServiceFrom: t.from, ServiceTo: t.to,
	}, s.now())
	if err != nil {
		return mapStore(err)
	}
	if err := s.write(ctx, tx, scope, actor, "charge.update", "charge", ch.ID, map[string]any{
		"total_cents": int64(t.gross), "previous_total_cents": ch.TotalCents, "lines": len(lines), "reason": "sync",
	}); err != nil {
		return err
	}
	ch.TotalCents, ch.NetCents, ch.VATCents, ch.Version = int64(t.gross), int64(t.net), int64(t.vat), ch.Version+1
	return s.emit(ctx, tx, scope, event.ChargeChanged, event.ChargeChangedData{ChargeID: ch.ID, Change: event.ChangeUpdated})
}

// CancelTx cancels a charge inside the caller's transaction when it is still cancellable (open, to_invoice, unpaid,
// never matched, not offset); false when it was not, and nothing changes. The origin's cancel hook runs in the same
// transaction (rental replaces its main charge this way, m11-plan §9.1). The caller holds the lock on the origin.
func (s *Service) CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, reason string) (bool, error) {
	if ch.PaymentStatus == store.ChargeCancelled {
		return false, nil
	}
	matched, err := store.HasMatches(ctx, tx, scope, ch.ID)
	if err != nil || matched {
		return false, err
	}
	return s.cancelIfPossibleTx(ctx, tx, scope, actor, ch, reason)
}

// cancelIfPossibleTx cancels a charge while it is cancellable (open, to_invoice, unpaid, not offset); false when
// it was not.
func (s *Service) cancelIfPossibleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, reason string) (bool, error) {
	ok, err := store.CancelChargeRow(ctx, tx, scope, ch.ID, 0, reason, s.now())
	if err != nil || !ok {
		return false, mapStore(err)
	}
	if err := s.write(ctx, tx, scope, actor, "charge.cancel", "charge", ch.ID, map[string]any{"reason": reason, "total_cents": ch.TotalCents}); err != nil {
		return false, err
	}
	if h := s.onCancel[ch.OriginType]; h != nil {
		if err := h(ctx, tx, scope, actor, ch); err != nil {
			return false, err
		}
	}
	return true, s.emit(ctx, tx, scope, event.ChargeCancelled, event.ChargeCancelledData{ChargeID: ch.ID, OriginType: ch.OriginType})
}

// OnHolderInstallmentIssuedTx creates the charge of an issued holder instalment (m5-plan §3.3): one line of the
// origin item of its source (transfer fees: the takeover item of the transfer, else the origin item), negative for
// a negative common-charge balance (whole amount as credit). Without an origin item nothing is created (logged; the
// office worklist shows issued holder instalments without charge and the backfill retries once it is set).
func (s *Service) OnHolderInstallmentIssuedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, h *store.HolderInstallment) error {
	cur, err := store.GetHolderInstallment(ctx, tx, scope, h.ID)
	if err != nil {
		return err
	}
	ch, err := s.holderChargeTx(ctx, tx, scope, actor, cur, calendar.Date{})
	if err != nil || ch == nil || ch.TotalCents <= 0 || s.requester == nil {
		return err
	}
	bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
	if err != nil || !bs.AutoPaymentRequest {
		return err
	}
	return s.requester.RequestForChargeTx(ctx, tx, scope, actor, ch, "holder_installment:"+cur.ID+":issued")
}

// holderOriginKeys maps holder instalment sources to origin item keys.
var holderOriginKeys = map[string]string{
	"management_fee": store.OriginItemManagementFee, "common_advance": store.OriginItemCommonAdvance,
	"common_balance": store.OriginItemCommonBalance, "transfer_fee": store.OriginItemTransferFee,
}

func (s *Service) holderChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, h *store.HolderInstallment, issuedOn calendar.Date) (*store.Charge, error) {
	if h.AmountCents == 0 || h.ChargeID != "" {
		return nil, nil
	}
	if existing, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginHolderInstallment, h.ID); err == nil {
		_, err := store.SetHolderInstallmentCharge(ctx, tx, scope, h.ID, existing.ID, s.now())
		return existing, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	key := holderOriginKeys[h.SourceType]
	itemID := ""
	from, to := calendar.New(h.IssueOn.Year(), 1, 1), calendar.New(h.IssueOn.Year(), 12, 31)
	if h.SourceType == "transfer_fee" {
		from, to = h.IssueOn, h.IssueOn
		if tr, err := store.GetRightTransfer(ctx, tx, scope, h.SourceID); err == nil && tr.TakeoverItemID != "" {
			itemID = tr.TakeoverItemID
		}
	}
	var line chargeplan.Line
	var err error
	if itemID != "" {
		line, err = s.itemLine(ctx, tx, scope, itemID, money.Cents(h.AmountCents), from, to)
	} else {
		line, err = s.originLine(ctx, tx, scope, key, money.Cents(h.AmountCents), from, to)
	}
	if errors.Is(err, ErrBillingItemMissing) {
		return nil, s.notBilledTx(ctx, tx, scope, err, h.ID, "", "charge: holder instalment not charged", "holder_installment_id", h.ID)
	}
	if err != nil {
		return nil, err
	}
	desc := s.describe(ctx, tx, scope, "holder_"+h.SourceType, "", strconv.Itoa(h.Seq))
	in := linesFromParts(chargeplan.All([]chargeplan.Remainder{{Line: line, Gross: line.Gross, VAT: line.VAT}}))
	ch, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: store.ChargeOriginHolderInstallment, OriginID: h.ID, CustomerID: h.CustomerID, RightID: h.RightID,
		IssuedOn: issuedOn, DueOn: h.DueOn, Description: desc, Lines: s.describeLines(ctx, tx, scope, in, desc),
	})
	if err != nil || ch == nil {
		return nil, err
	}
	if ok, err := store.SetHolderInstallmentCharge(ctx, tx, scope, h.ID, ch.ID, s.now()); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("charge: holder instalment %s already charged", h.ID)
	}
	return ch, s.emit(ctx, tx, scope, event.InstallmentCharged, event.InstallmentChargedData{
		InstallmentID: h.ID, Holder: true, RightID: h.RightID, ChargeID: ch.ID,
	})
}

// originLine builds one line of gross amount with the item configured for an origin key (ErrBillingItemMissing
// when none).
func (s *Service) originLine(ctx context.Context, db bun.IDB, scope store.MarinaScope, key string, gross money.Cents, from, to calendar.Date) (chargeplan.Line, error) {
	itemID, err := store.OriginItem(ctx, db, scope, key)
	if err != nil {
		return chargeplan.Line{}, err
	}
	if itemID == "" {
		return chargeplan.Line{}, ErrBillingItemMissing
	}
	return s.itemLine(ctx, db, scope, itemID, gross, from, to)
}

// itemLine builds one line of gross amount with a catalogue item (VAT split on the gross, qty 1).
func (s *Service) itemLine(ctx context.Context, db bun.IDB, scope store.MarinaScope, itemID string, gross money.Cents, from, to calendar.Date) (chargeplan.Line, error) {
	it, err := store.GetItem(ctx, db, scope, itemID)
	if errors.Is(err, store.ErrNotFound) {
		return chargeplan.Line{}, ErrBillingItemMissing
	}
	if err != nil {
		return chargeplan.Line{}, err
	}
	class, err := s.revenueClassCode(ctx, db, scope, it.RevenueClassID)
	if err != nil {
		return chargeplan.Line{}, err
	}
	net, vat := money.VATSplit(gross, it.VATRateBP)
	return chargeplan.Line{
		ItemID: it.ID, ItemCode: it.Code, Description: itemName(it), RevenueClass: class, ExternalCode: it.ExternalCode,
		Qty: 1000, VATRateBP: it.VATRateBP, VATNature: it.VATNature, PriceIncludesVAT: it.PriceIncludesVAT,
		Net: net, VAT: vat, Gross: gross, ServiceFrom: from, ServiceTo: to,
	}, nil
}

func (s *Service) revenueClassCode(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	rc, err := store.GetRevenueClass(ctx, db, scope, id)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return rc.Code, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
