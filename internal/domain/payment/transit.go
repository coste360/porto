package payment

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Desk confirmation of a transit with a payment (m5-plan §8.2, §16 D8): one transaction confirms the transit
// through the manual driver (contract.ConfirmTransitPaidTx → confirmTx, INV-09), the billing hook creates the
// transit charge, and the payment is recorded and matched to it.

// TransitPayment is the payment collected at the desk with the confirmation.
type TransitPayment struct {
	Method         string
	Amount         money.Cents // 0 = the open residual of the transit's charges
	CashRegisterID string
	Reference      string
	IdempotencyKey string
}

// ConfirmTransitWithPayment confirms a held, pending_payment or awaiting_approval transit (M8: an online booking
// approved without an online provider, m8-plan §5.6, §6) with the manual driver and records its payment
// (transits.desk plus payments.collect, or payments.collect_cash for cash). It returns the payment.
func (s *Service) ConfirmTransitWithPayment(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int, in TransitPayment) (*store.Payment, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if _, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermPaymentsCollectCash); err != nil {
		return nil, err
	} else if perm == auth.PermPaymentsCollectCash && in.Method != store.PaymentMethodCash {
		return nil, auth.ErrForbidden
	}
	if s.d.Contracts == nil {
		return nil, errors.New("payment: contract service not wired")
	}
	if in.Amount < 0 {
		return nil, invalid("payment.amount_cents", "positive")
	}
	var out *store.Payment
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.LockContract(ctx, tx, scope, contractID)
		if err != nil {
			return err
		}
		if c.Kind != store.ContractKindTransit {
			return ErrNotFound
		}
		if ifMatch != 0 && c.Version != ifMatch {
			return contract.ErrVersionMismatch
		}
		if _, err := s.d.Contracts.ConfirmTransitPaidTx(ctx, tx, scope, p, contractID, contract.SourceManual); err != nil {
			return err
		}
		open, err := store.LedgerOpenChargesOfContract(ctx, tx, scope, contractID)
		if err != nil {
			return err
		}
		if len(open) == 0 {
			return charge.ErrBillingItemMissing // no transit charge: billing is not wired or its item is missing
		}
		residuals := make([]Open, 0, len(open))
		var due money.Cents
		for _, ch := range open {
			residuals = append(residuals, Open{ChargeID: ch.ID, Residual: money.Cents(ch.ResidualCents())})
			due += money.Cents(ch.ResidualCents())
		}
		amount := in.Amount
		if amount == 0 {
			amount = due
		}
		matches, _ := PlanOldestFirst(residuals, amount)
		out, err = s.RecordTx(ctx, tx, scope, p.Actor(), RecordInput{
			CustomerID: open[0].CustomerID, Kind: store.PaymentKindPayment, Method: in.Method, Source: store.PaymentSourceManual,
			Amount: amount, Reference: in.Reference, Matches: matches, CashRegisterID: in.CashRegisterID,
			IdempotencyKey: in.IdempotencyKey,
		})
		return err
	})
	return out, err
}
