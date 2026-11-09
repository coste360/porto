package handover

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Quick cash (m11-plan §9.3, D19): one step from the booking. The target charges are ensured (the main charge from
// the snapshot; the return charge exists only after check-in) and their residuals are collected by cash or card POS
// through payment.RecordTx (the commercial document of receipt-mode charges is issued by the receipt hook through
// fiscal.RecordTx), or by a payment link shown as a QR (collect.ChargeLinkTx; the fiscal document is issued on the
// provider confirmation). The booking lock serialises concurrent calls: the losers find nothing to collect.

// QuickCash collects the open rental charges in one step (rental.manage plus payments.collect, or
// payments.collect_cash for cash).
func (s *Service) QuickCash(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in QuickCashInput) (*QuickCashResult, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if in.Target == "" {
		in.Target = TargetOpen
	}
	switch in.Target {
	case TargetMain, TargetReturn, TargetOpen:
	default:
		return nil, invalid("target", "invalid")
	}
	switch in.Method {
	case MethodCash:
		if !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollect) && !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollectCash) {
			return nil, auth.ErrForbidden
		}
	case MethodCardPOS, MethodLink:
		if !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollect) {
			return nil, auth.ErrForbidden
		}
	default:
		return nil, invalid("method", "invalid")
	}
	if in.RegisterID != "" && !store.ValidID(in.RegisterID) {
		return nil, invalid("register_id", "invalid")
	}
	set, err := store.LoadRentalSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	register := in.RegisterID
	if register == "" {
		register = set.DefaultRegisterID
	}
	actor := p.Actor()
	out := &QuickCashResult{FiscalDocumentIDs: []string{}}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		*out = QuickCashResult{FiscalDocumentIDs: []string{}}
		b, err := s.lock(ctx, tx, scope, bookingID, 0, store.RentalStatusConfirmed, store.RentalStatusCheckedOut,
			store.RentalStatusReturned, store.RentalStatusClosed, store.RentalStatusNoShow)
		if err != nil {
			return err
		}
		if in.Target != TargetReturn && b.Status != store.RentalStatusNoShow {
			if _, err := s.mainChargeTx(ctx, tx, scope, actor, b); err != nil {
				return err
			}
		}
		rows, err := store.RentalChargesOf(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		var ids []string
		for _, r := range rows {
			if r.PaymentStatus == store.ChargeCancelled || r.TotalCents <= 0 {
				continue
			}
			switch {
			case in.Target == TargetMain && (r.LinkKind == store.RentalChargeMain || r.LinkKind == store.RentalChargeAdjustment):
			case in.Target == TargetReturn && r.LinkKind == store.RentalChargeReturn:
			case in.Target == TargetOpen && r.LinkKind != store.RentalChargeReplaced:
			default:
				continue
			}
			ids = append(ids, r.ID)
		}
		charges, err := store.LockChargesSorted(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		var matches []payment.MatchInput
		var total money.Cents
		var open []string
		for _, c := range charges {
			res := money.Cents(c.ResidualCents())
			if res <= 0 {
				continue
			}
			matches = append(matches, payment.MatchInput{ChargeID: c.ID, Amount: res})
			open = append(open, c.ID)
			total += res
		}
		if total <= 0 {
			return ErrNothingToCollect
		}
		if in.Method == MethodLink {
			link, err := s.d.Collect.ChargeLinkTx(ctx, tx, scope, actor, b.CustomerID, open)
			if err != nil {
				if errors.Is(err, collect.ErrCustomerRequired) {
					return ErrCustomerRequired
				}
				return err
			}
			out.LinkURL, out.LinkExpiresAt = link.URL, link.ExpiresAt
		} else {
			method := store.PaymentMethodCash
			if in.Method == MethodCardPOS {
				method = store.PaymentMethodCardPOS
			}
			pay, err := s.d.Payments.RecordTx(ctx, tx, scope, actor, payment.RecordInput{
				CustomerID: b.CustomerID, Kind: store.PaymentKindPayment, Method: method, Source: store.PaymentSourceManual,
				Amount: total, Matches: matches, CashRegisterID: register, Notes: "Noleggio",
			})
			if err != nil {
				return err
			}
			out.PaymentID = pay.ID
			out.ReceiptURL = "/api/admin/v1/marinas/" + scope.ID() + "/payments/" + pay.ID + "/receipt"
			docs, err := store.FiscalDocumentsOfPayment(ctx, tx, scope, pay.ID)
			if err != nil {
				return err
			}
			for _, d := range docs {
				out.FiscalDocumentIDs = append(out.FiscalDocumentIDs, d.ID)
			}
		}
		if err := s.audit(ctx, tx, scope, actor, "rental_booking.quick_cash", "rental_booking", b.ID, map[string]any{
			"target": in.Target, "method": in.Method, "amount_cents": int64(total), "payment_id": out.PaymentID,
			"charge_ids": open,
		}); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, b, "charge"); err != nil {
			return err
		}
		after, err := store.RentalChargesOf(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		out.Charges = chargeRefs(after)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chargeRefs converts linked charges for the API.
func chargeRefs(rows []store.RentalLinkedCharge) []reservation.ChargeRef {
	out := make([]reservation.ChargeRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, reservation.ChargeRef{
			ChargeID: r.ID, Kind: r.LinkKind, Status: r.PaymentStatus, Total: money.Cents(r.TotalCents),
			Paid: money.Cents(r.PaidAmountCents), FiscalMode: r.FiscalMode,
		})
	}
	return out
}
