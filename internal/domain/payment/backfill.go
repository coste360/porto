package payment

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

var _ charge.BackfillPayer = (*Service)(nil)

// RecordBackfillPaymentTx implements charge.BackfillPayer (m5-plan §3.7, D7): the historical manual settlement of
// an M4 instalment or holder instalment becomes a payment with source backfill, matched in full to its new charge.
// It never lands in a cash session (the money was counted in M4) and never queues a receipt. The staff reference
// is decrypted with the instalment's AAD and re-encrypted under the payment's.
func (s *Service) RecordBackfillPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, in charge.BackfillPayment) error {
	if ch == nil || ch.TotalCents <= 0 {
		return nil
	}
	amount := in.Amount
	if amount <= 0 || amount > money.Cents(ch.TotalCents) {
		amount = money.Cents(ch.TotalCents)
	}
	method := in.Method
	if !slices.Contains(manualMethods, method) {
		method = store.PaymentMethodOther
	}
	ref := ""
	if in.RefEnc != "" && s.d.Keys != nil {
		aad := ""
		switch ch.OriginType {
		case store.ChargeOriginInstallment:
			aad = "installment-ref:" + ch.OriginID
		case store.ChargeOriginHolderInstallment:
			aad = "holder-installment-ref:" + ch.OriginID
		}
		if aad != "" {
			if v, err := s.d.Keys.DecryptString(in.RefEnc, aad); err == nil {
				ref = v
			}
		}
	}
	today := s.today(ctx, tx, scope)
	on := in.ReceivedOn
	if on.IsZero() || on.After(today) {
		on = today
	}
	_, err := s.RecordTx(ctx, tx, scope, actor, RecordInput{
		CustomerID: ch.CustomerID, Kind: store.PaymentKindPayment, Method: method, Source: store.PaymentSourceBackfill,
		Amount: amount, ReceivedOn: on, Reference: ref,
		Matches: []MatchInput{{ChargeID: ch.ID, Amount: amount}},
	})
	return err
}

var _ charge.OriginFollower = (*Service)(nil)

// FollowChargeOriginTx implements charge.OriginFollower: after an offset changed a charge's payment status, its
// instalment or holder instalment follows (no payment is involved, so no payment id or reference is carried).
func (s *Service) FollowChargeOriginTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, chargeID string) error {
	ch, err := store.LedgerCharge(ctx, tx, scope, chargeID)
	if err != nil {
		return err
	}
	if ch.OriginType != store.ChargeOriginInstallment && ch.OriginType != store.ChargeOriginHolderInstallment {
		return nil
	}
	return s.followOrigin(ctx, tx, scope, actor, ch, &store.Payment{
		Method: store.PaymentMethodOther, ReceivedOn: s.today(ctx, tx, scope),
	})
}
