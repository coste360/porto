package handover

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Deposits (m11-plan §7 step 3, §9.2, D15): cash and card POS are payments of kind deposit (cash needs an open
// session); a pre-authorisation is a manual-capture intent of purpose rental_deposit through a provider implementing
// ext.PaymentAuthorizer (pending until the authorized webhook); waived needs a reason. A deposit is never revenue
// until retained (INV-98).

func depositView(b *store.RentalBooking) *DepositView {
	v := &DepositView{
		Mode: b.DepositMode, Status: b.DepositStatus, Amount: money.Cents(b.DepositCents),
		Retained: money.Cents(b.DepositRetainedCents), PaymentID: b.DepositPaymentID, IntentID: b.DepositIntentID,
		WaiveReason: b.DepositWaiveReason,
	}
	if v.Status == "" {
		v.Status = store.RentalDepositStatusNone
	}
	if b.DepositDecisionCents != nil {
		d := money.Cents(*b.DepositDecisionCents)
		v.Decision = &d
	}
	return v
}

// GetDeposit returns the deposit state (with the pre-authorisation page while pending).
func (s *Service) GetDeposit(ctx context.Context, p *auth.Principal, marinaID, bookingID string) (*DepositView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	b, err := store.GetRentalBooking(ctx, s.d.DB.DB, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	v := depositView(b)
	if b.DepositStatus == store.RentalDepositStatusPending && b.DepositIntentID != "" {
		if in, err := store.GetPaymentIntent(ctx, s.d.DB.DB, scope, b.DepositIntentID); err == nil {
			if in.CheckoutURL != "" && (in.ExpiresAt.IsZero() || in.ExpiresAt.After(s.now())) {
				v.CheckoutURL, v.ExpiresAt = in.CheckoutURL, in.ExpiresAt
			}
		}
	}
	return v, nil
}

func depositModes(set *store.RentalSettings) []string {
	var modes []string
	_ = json.Unmarshal(set.DepositModes, &modes)
	return modes
}

// StartDeposit takes the deposit (rental.manage plus payments.collect, or payments.collect_cash for cash).
func (s *Service) StartDeposit(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in DepositInput) (*DepositView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	switch in.Mode {
	case store.RentalDepositCash:
		if !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollect) && !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollectCash) {
			return nil, auth.ErrForbidden
		}
	case store.RentalDepositCardPOS, store.RentalDepositPreauth:
		if !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollect) {
			return nil, auth.ErrForbidden
		}
	case store.RentalDepositWaived:
		in.Reason = strings.TrimSpace(in.Reason)
		if n := utf8.RuneCountInString(in.Reason); n == 0 {
			return nil, invalid("reason", "required")
		} else if n > 200 {
			return nil, invalid("reason", "too_long")
		}
	default:
		return nil, invalid("mode", "invalid")
	}
	if in.Amount < 0 {
		return nil, invalid("amount_cents", "positive")
	}
	if in.RegisterID != "" && !store.ValidID(in.RegisterID) {
		return nil, invalid("register_id", "invalid")
	}
	set, err := store.LoadRentalSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	if in.Mode != store.RentalDepositWaived && !slices.Contains(depositModes(set), in.Mode) {
		if in.Mode == store.RentalDepositPreauth {
			return nil, ErrPreauthUnavailable
		}
		return nil, invalid("mode", "not_enabled")
	}
	if in.Mode == store.RentalDepositPreauth {
		if ok, err := s.d.Collect.PreauthAvailable(ctx, s.d.DB.DB, scope); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrPreauthUnavailable
		}
	}
	register := in.RegisterID
	if register == "" {
		register = set.DefaultRegisterID
	}
	actor := p.Actor()
	var intentID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		intentID = ""
		b, err := s.lock(ctx, tx, scope, bookingID, 0, store.RentalStatusConfirmed)
		if err != nil {
			return err
		}
		switch b.DepositStatus {
		case store.RentalDepositStatusHeld, store.RentalDepositStatusAuthorized, store.RentalDepositStatusWaived:
			if b.DepositMode == in.Mode {
				return nil // already taken: one effect
			}
			return ErrInvalidTransition
		case store.RentalDepositStatusPending:
			if b.DepositMode == in.Mode && in.Mode == store.RentalDepositPreauth {
				intentID = b.DepositIntentID // a second tap shows the same page
				return nil
			}
		case "", store.RentalDepositStatusNone:
		default:
			return ErrInvalidTransition
		}
		amount := in.Amount
		if amount == 0 {
			amount = money.Cents(b.DepositCents) // the booking's snapshot of the unit deposit
		}
		set := map[string]any{"deposit_mode": in.Mode, "deposit_cents": int64(amount)}
		switch in.Mode {
		case store.RentalDepositCash, store.RentalDepositCardPOS:
			if amount <= 0 {
				return invalid("amount_cents", "positive")
			}
			method := store.PaymentMethodCash
			if in.Mode == store.RentalDepositCardPOS {
				method = store.PaymentMethodCardPOS
			}
			pay, err := s.d.Payments.RecordRentalDepositTx(ctx, tx, scope, actor, payment.RentalDepositInput{
				CustomerID: b.CustomerID, Method: method, Amount: amount, CashRegisterID: register,
				IdempotencyKey: "rental_deposit:" + b.ID, Notes: "Cauzione noleggio",
			})
			if err != nil {
				return err
			}
			set["deposit_status"], set["deposit_payment_id"], set["deposit_intent_id"] = store.RentalDepositStatusHeld, pay.ID, nil
		case store.RentalDepositPreauth:
			if b.CustomerID == "" {
				return ErrCustomerRequired
			}
			if amount <= 0 {
				return invalid("amount_cents", "positive")
			}
			pi, err := s.d.Collect.StartRentalDepositTx(ctx, tx, scope, actor, b.ID, b.CustomerID, amount)
			if err != nil {
				return mapCollect(err)
			}
			intentID = pi.ID
			set["deposit_status"], set["deposit_intent_id"] = store.RentalDepositStatusPending, pi.ID
		case store.RentalDepositWaived:
			set["deposit_status"], set["deposit_cents"], set["deposit_waive_reason"] = store.RentalDepositStatusWaived, int64(0), in.Reason
		}
		if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, set, s.now()); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, actor, "rental_booking.deposit", "rental_booking", b.ID, map[string]any{
			"mode": in.Mode, "amount_cents": set["deposit_cents"], "status": set["deposit_status"], "intent_id": intentID,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "deposit")
	})
	if err != nil {
		return nil, err
	}
	v, err := s.GetDeposit(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if intentID != "" && v.CheckoutURL == "" {
		auth, err := s.d.Collect.OpenRentalDeposit(ctx, scope, intentID)
		if err != nil {
			return nil, mapCollect(err)
		}
		v.CheckoutURL, v.ExpiresAt = auth.CheckoutURL, auth.ExpiresAt
	}
	return v, nil
}

func mapCollect(err error) error {
	switch {
	case errors.Is(err, collect.ErrProviderDisabled):
		return ErrPreauthUnavailable
	case errors.Is(err, collect.ErrCustomerRequired):
		return ErrCustomerRequired
	}
	return err
}

// depositFinal reports whether the deposit of a booking needs nothing more (released, retained, waived or none).
func depositFinal(b *store.RentalBooking) bool {
	switch b.DepositStatus {
	case "", store.RentalDepositStatusNone, store.RentalDepositStatusReleased, store.RentalDepositStatusRetained,
		store.RentalDepositStatusPartiallyRetained, store.RentalDepositStatusWaived:
		return true
	}
	return false
}

// retainedStatus is the final status of a deposit of amount d with r retained.
func retainedStatus(d, r money.Cents) string {
	switch {
	case r <= 0:
		return store.RentalDepositStatusReleased
	case r >= d:
		return store.RentalDepositStatusRetained
	}
	return store.RentalDepositStatusPartiallyRetained
}
