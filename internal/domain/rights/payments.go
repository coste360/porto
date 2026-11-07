package rights

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// M5 entry point called by internal/domain/payment in its transaction (m5-plan §8.2).

// SettlementPayment is the payment that moved a holder instalment's charge (settlement_source = payment).
type SettlementPayment struct {
	PaymentID  string
	Method     string
	ReceivedOn calendar.Date
	Reference  string // the staff reference of the payment (stored encrypted, never logged)
}

// SettleCharge is a manual settlement of a holder instalment's charge: a payment of the method for the residual.
type SettleCharge struct {
	Method     string
	Reference  string
	ReceivedOn calendar.Date
}

// ChargeSettler records the manual payment that settles a holder instalment's charge (implemented by
// internal/domain/payment, which registers itself: rights cannot import payment).
type ChargeSettler interface {
	SettleHolderChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, chargeID string, in SettleCharge) (paymentID string, err error)
}

// SetChargeSettler registers the payment service as the settler of holder instalment charges.
func (s *Service) SetChargeSettler(cs ChargeSettler) {
	s.settler = cs
}

func (s *Service) chargeSettler() ChargeSettler { return s.settler }

// settlementMethodOf maps a payment method to the M4 settlement methods.
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

// settleViaPaymentTx settles a locked holder instalment with a charge through a manual payment of the charge;
// handled is false without a charge or a settler (historical manual path).
func (s *Service) settleViaPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, h *store.HolderInstallment, in SettleInput) (bool, error) {
	cs := s.chargeSettler()
	if cs == nil || h.ChargeID == "" {
		return false, nil
	}
	if _, err := cs.SettleHolderChargeTx(ctx, tx, scope, p, h.ChargeID, SettleCharge{Method: in.Method, Reference: in.Reference, ReceivedOn: in.SettledOn}); err != nil {
		return false, err
	}
	cur, err := store.GetHolderInstallment(ctx, tx, scope, h.ID)
	if err != nil {
		return false, err
	}
	if cur.Status != store.InstallmentPaid {
		return false, store.ErrConflict
	}
	*h = *cur
	return true, nil
}

// ApplyChargeSettlementTx makes a holder instalment follow the payment status of its charge (partially_paid,
// paid, or back after a reversal). p is nil for jobs and provider webhooks.
func (s *Service) ApplyChargeSettlementTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal,
	holderInstallmentID, chargeStatus string, pay SettlementPayment,
) error {
	h, err := store.GetHolderInstallment(ctx, tx, scope, holderInstallmentID)
	if err != nil {
		return err
	}
	if err := lockHolderRights(ctx, tx, scope, []store.HolderInstallment{*h}); err != nil {
		return err
	}
	if h, err = store.GetHolderInstallment(ctx, tx, scope, holderInstallmentID); err != nil {
		return err
	}
	if h.Status == store.InstallmentCancelled {
		return nil
	}
	switch chargeStatus {
	case store.ChargePaid:
		if h.Status == store.InstallmentPaid {
			return nil
		}
		on := pay.ReceivedOn
		if on.IsZero() {
			on = s.today(ctx, tx, scope)
		}
		return s.settleTx(ctx, tx, scope, p, h, h.Version, store.SettlementPayment, SettleInput{Method: settlementMethodOf(pay.Method), Reference: pay.Reference, SettledOn: on})
	case store.ChargePartiallyPaid:
		return s.unsettleTx(ctx, tx, scope, p, h, store.InstallmentPartiallyPaid, pay)
	case store.ChargeOpen:
		return s.unsettleTx(ctx, tx, scope, p, h, store.InstallmentIssued, pay)
	}
	return nil
}

// unsettleTx moves a holder instalment to partially_paid or back to issued, clearing the settlement columns.
func (s *Service) unsettleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, h *store.HolderInstallment, to string, pay SettlementPayment) error {
	if h.Status == to {
		return nil
	}
	from := []string{store.InstallmentIssued, store.InstallmentScheduled, store.InstallmentPartiallyPaid, store.InstallmentPaid}
	ok, err := store.FollowHolderInstallment(ctx, tx, scope, h.ID, from, to, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrConflict
	}
	action := "holder_installment.partially_paid"
	if h.Status == store.InstallmentPaid || to == store.InstallmentIssued {
		action = "holder_installment.unsettle"
	}
	return s.write(ctx, tx, actorOf(p), scope, action, "holder_installment", h.ID, map[string]any{
		"source_type": h.SourceType, "from_status": h.Status, "status": to, "payment_id": pay.PaymentID,
	}, changed("holder_installment", h.ID, event.ChangeUpdated))
}
