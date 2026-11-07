package payment

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Settlement of instalments through payments (m5-plan §8.2, INV-32): the M4 settle routes record a manual payment
// of the charge residual matched to the charge; the matching makes the instalment paid through
// ApplyChargeSettlementTx with settlement_source payment.

var (
	_ contract.ChargeSettler = (*Service)(nil)
	_ rights.ChargeSettler   = (*Service)(nil)
)

// SettleChargeTx implements contract.ChargeSettler.
func (s *Service) SettleChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, chargeID string, in contract.SettleCharge) (string, error) {
	return s.settleChargeTx(ctx, tx, scope, p, chargeID, in.Method, in.Reference, in.ReceivedOn)
}

// SettleHolderChargeTx implements rights.ChargeSettler.
func (s *Service) SettleHolderChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, chargeID string, in rights.SettleCharge) (string, error) {
	return s.settleChargeTx(ctx, tx, scope, p, chargeID, in.Method, in.Reference, in.ReceivedOn)
}

func (s *Service) settleChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, chargeID, method, reference string, on calendar.Date) (string, error) {
	ch, err := store.LedgerCharge(ctx, tx, scope, chargeID)
	if err != nil {
		return "", err
	}
	residual := ch.ResidualCents()
	if residual <= 0 {
		return "", &LimitError{Limit: LimitCharge, ChargeID: ch.ID}
	}
	pay, err := s.RecordTx(ctx, tx, scope, p.Actor(), RecordInput{
		CustomerID: ch.CustomerID, Kind: store.PaymentKindPayment, Method: method, Source: store.PaymentSourceManual,
		Amount: money.Cents(residual), ReceivedOn: on, Reference: reference,
		Matches: []MatchInput{{ChargeID: ch.ID, Amount: money.Cents(residual)}},
	})
	if err != nil {
		return "", err
	}
	return pay.ID, nil
}
