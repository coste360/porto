package contract

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// M5 entry points called by internal/domain/payment and internal/domain/collect in their transactions (m5-plan
// §8.2). The principal is nil for jobs and provider webhooks (audit actor system).

// SettlementPayment is the payment that moved an instalment's charge (INV-32: settlement_source = payment).
type SettlementPayment struct {
	PaymentID  string
	Method     string // the payment method, mapped to the M4 settlement methods
	ReceivedOn calendar.Date
	Reference  string // the staff reference of the payment (stored encrypted on the instalment, never logged)
}

// SettleCharge is a manual settlement of an instalment's charge: a payment of the method for the charge residual.
type SettleCharge struct {
	Method     string
	Reference  string
	ReceivedOn calendar.Date
}

// ChargeSettler records the manual payment that settles an instalment's charge (implemented by
// internal/domain/payment, which registers itself with SetChargeSettler: contract cannot import payment). The
// payment matching makes the instalment paid through ApplyChargeSettlementTx.
type ChargeSettler interface {
	SettleChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, chargeID string, in SettleCharge) (paymentID string, err error)
}

// SetChargeSettler registers the payment service as the settler of instalment charges (nil removes it).
func (s *Service) SetChargeSettler(cs ChargeSettler) {
	s.settler = cs
}

func (s *Service) chargeSettler() ChargeSettler { return s.settler }

// settlementMethodOf maps a payment method to the M4 settlement methods (installments.settlement_method CHECK).
func settlementMethodOf(method string) string {
	switch method {
	case "cash", "card_pos", "bank_transfer", "cheque", "other":
		return method
	case "card_online", "wallet":
		return "card_pos"
	case "sepa_debit":
		return "bank_transfer"
	}
	return "other"
}

// settleViaPaymentTx settles a locked instalment through a payment of its charge (issuing a scheduled instalment
// first so that it has one). handled is false when the instalment has no charge and no settler is registered:
// the caller keeps the historical manual path (M4 data before the backfill, billing hooks inactive).
func (s *Service) settleViaPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, r *store.Installment, in SettleInput) (bool, error) {
	cs := s.chargeSettler()
	if cs == nil {
		return false, nil
	}
	if r.Status == store.InstallmentScheduled && s.billingActive() {
		if ok, err := s.issueOneTx(ctx, tx, scope, a, r); err != nil {
			return false, err
		} else if !ok {
			return false, ErrVersionMismatch
		}
		cur, err := store.GetInstallment(ctx, tx, scope, r.ID)
		if err != nil {
			return false, err
		}
		*r = *cur
	}
	if r.ChargeID == "" {
		return false, nil
	}
	if _, err := cs.SettleChargeTx(ctx, tx, scope, a.p, r.ChargeID, SettleCharge{Method: in.Method, Reference: in.Reference, ReceivedOn: *in.SettledOn}); err != nil {
		return false, err
	}
	cur, err := store.GetInstallment(ctx, tx, scope, r.ID)
	if err != nil {
		return false, err
	}
	if cur.Status != store.InstallmentPaid {
		return false, ErrSettleConflict
	}
	return true, nil
}

// ApplyChargeSettlementTx makes an instalment follow the payment status of its charge: partially_paid, paid
// (settleTx with source payment) or back to issued/partially_paid after a reversal (audit installment.unsettle).
func (s *Service) ApplyChargeSettlementTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal,
	installmentID, chargeStatus string, pay SettlementPayment,
) error {
	r, err := store.GetInstallment(ctx, tx, scope, installmentID)
	if err != nil {
		return notFound(err)
	}
	if _, err := store.LockContract(ctx, tx, scope, r.ContractID); err != nil {
		return notFound(err)
	}
	if r, err = store.GetInstallment(ctx, tx, scope, installmentID); err != nil {
		return notFound(err)
	}
	if r.Status == store.InstallmentCancelled {
		return nil // a cancelled row keeps its status; its charge is corrected by the billing hooks
	}
	a := actor{p}
	switch chargeStatus {
	case store.ChargePaid:
		if r.Status == store.InstallmentPaid {
			return nil
		}
		on := pay.ReceivedOn
		if on.IsZero() {
			on = s.today(ctx, tx, scope)
		}
		ok, err := s.settleTx(ctx, tx, scope, a, r, 0, store.SettlementPayment, SettleInput{Method: settlementMethodOf(pay.Method), Reference: pay.Reference, SettledOn: &on})
		if err != nil {
			return err
		}
		if !ok {
			return ErrSettleConflict
		}
		return nil
	case store.ChargePartiallyPaid:
		return s.unsettleTx(ctx, tx, scope, a, r, store.InstallmentPartiallyPaid, pay)
	case store.ChargeOpen:
		return s.unsettleTx(ctx, tx, scope, a, r, store.InstallmentIssued, pay)
	}
	return nil
}

// unsettleTx moves an instalment to partially_paid or back to issued (the settlement columns are cleared), with
// audit installment.unsettle when it was paid and installment.partially_paid otherwise.
func (s *Service) unsettleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, r *store.Installment, to string, pay SettlementPayment) error {
	if r.Status == to {
		return nil
	}
	from := []string{store.InstallmentIssued, store.InstallmentScheduled, store.InstallmentPartiallyPaid, store.InstallmentPaid}
	ok, err := store.FollowInstallment(ctx, tx, scope, r.ID, from, to, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrSettleConflict
	}
	action := "installment.partially_paid"
	if r.Status == store.InstallmentPaid || to == store.InstallmentIssued {
		action = "installment.unsettle"
	}
	if err := s.writeAudit(ctx, tx, scope, a, action, entityInstallment, r.ID, map[string]any{
		"contract_id": r.ContractID, "seq": r.Seq, "from_status": r.Status, "status": to, "payment_id": pay.PaymentID,
	}); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		InstallmentID: r.ID, ContractID: r.ContractID, Change: event.ChangeUpdated, PlanVersion: r.PlanVersion,
	})
}

// ConfirmTransitPaidTx confirms a held or pending_payment transit paid at the desk (source manual) or through the
// provider webhook (source provider), through confirmTx (INV-09); the billing hook creates its charge. A transit
// already confirmed (or further) is returned as it is, so a duplicate webhook changes nothing.
func (s *Service) ConfirmTransitPaidTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal,
	contractID, source string,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if source != SourceManual && source != SourceProvider {
		return nil, errors.New("contract: unknown confirmation source")
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return nil, err
	}
	switch Status(c.Status) {
	case StatusConfirmed, StatusArrived, StatusDeparted:
		return c, nil
	}
	if err := s.confirmTx(ctx, tx, scope, actor{p}, c, source, ManualConfirmation{}); err != nil {
		return nil, err
	}
	return store.GetContract(ctx, tx, scope, contractID)
}

// RequestTransitPaymentTx moves a held transit to pending_payment (payment link) and extends its holds to
// holdUntil (ifMatch 0 = no version check).
func (s *Service) RequestTransitPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal,
	contractID string, ifMatch int, holdUntil time.Time,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, err
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionRequestPayment)
	if err != nil {
		return nil, err
	}
	now := s.now()
	holds, err := s.liveHolds(ctx, tx, scope, c.ID, now)
	if err != nil {
		return nil, err
	}
	if len(holds) == 0 {
		return nil, occupancy.ErrHoldExpired
	}
	until := store.UTC(holdUntil)
	if until.After(now) {
		if err := s.alloc.ConvertTx(ctx, tx, scope, now, holds, occupancy.KindHold, &until); err != nil {
			return nil, err
		}
	}
	from := c.Status
	c.Status = string(t.To)
	a := actor{p}
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "transit.request_payment", entityTransit, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "hold_until": until, "holds": holds,
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: string(StatusPendingPayment)}); err != nil {
		return nil, err
	}
	return c, nil
}

// liveHolds returns the ids of the active, unexpired holds of a contract.
func (s *Service) liveHolds(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string, now time.Time) ([]string, error) {
	allocs, err := contractAllocs(ctx, tx, scope, contractID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, al := range allocs {
		if al.Status == store.AllocActive && al.Kind == store.AllocKindHold && al.ExpiresAt.After(now) {
			out = append(out, al.ID)
		}
	}
	return out, nil
}

// RevertTransitPaymentTx moves a pending_payment transit back to held when its payment intent expired and the
// hold is still valid (otherwise occupancy.ErrHoldExpired: the expiry job handles it).
func (s *Service) RevertTransitPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal,
	contractID string,
) (*store.Contract, error) {
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return nil, err
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionPaymentExpired)
	if err != nil {
		return nil, err
	}
	now := s.now()
	holds, err := s.liveHolds(ctx, tx, scope, c.ID, now)
	if err != nil {
		return nil, err
	}
	if len(holds) == 0 {
		return nil, occupancy.ErrHoldExpired
	}
	from := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, actor{p}, "transit.payment_expired", entityTransit, c.ID, map[string]any{
		"from_status": from, "status": c.Status,
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: string(StatusHeld)}); err != nil {
		return nil, err
	}
	return c, nil
}

// ArrearsReason is the suspend reason written by SuspendForArrearsTx.
const ArrearsReason = "arrears"

// ArrearsCheck is the arrears policy re-checked by SuspendForArrearsTx under the contract lock: charges due
// before Cutoff and still open (without an active exclusion on Today) must sum to at least MinAmountCents.
type ArrearsCheck struct {
	Cutoff, Today  calendar.Date
	MinAmountCents int64
}

// SuspendForArrearsTx suspends an active mooring or charter contract for arrears (reason arrears, actor
// system:payments.arrears); never transits. The overdue amount and the customer exclusion are read again after
// the contract and its overdue charges are locked, so that a payment committed after the caller's unlocked scan
// never leads to a suspension (ErrNotInArrears).
func (s *Service) SuspendForArrearsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string, chk ArrearsCheck) (*store.Contract, error) {
	c, err := lockContract(ctx, tx, scope, contractID, KindMooring, 0)
	if err != nil {
		return nil, err
	}
	sum, err := store.LockOverdueOfContract(ctx, tx, scope, c.ID, chk.Cutoff, chk.Today)
	if err != nil {
		return nil, err
	}
	if sum <= 0 || sum < chk.MinAmountCents {
		return nil, ErrNotInArrears
	}
	cust := c.BillingCustomerID
	if cust == "" {
		cust = c.CustomerID
	}
	if excluded, err := store.CustomerDunningExcluded(ctx, tx, scope, cust, chk.Today); err != nil {
		return nil, err
	} else if excluded {
		return nil, ErrNotInArrears
	}
	t, err := Next(MooringTransitions, Status(c.Status), ActionSuspend)
	if err != nil {
		return nil, err
	}
	now := s.now()
	from := c.Status
	c.Status = string(t.To)
	c.SuspendedAt, c.SuspendReason = now, ArrearsReason
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	entry := auditEntry(scope, system, "contract.suspend", entityContract, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "reason": ArrearsReason,
	})
	entry.Actor = audit.Actor{Type: audit.ActorSystem, ID: "system:payments.arrears"}
	if err := s.d.Audit.Write(ctx, tx, entry); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeSuspended}); err != nil {
		return nil, err
	}
	return c, nil
}
