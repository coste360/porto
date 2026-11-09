package collect

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Rental collection seams (M11, m11-plan §9.2-§9.3, D15, D19): the payment link of the rental quick cash (a pay page
// request of the booking charges, shown as a QR, no email) and the manual-capture pre-authorisation of a rental
// deposit (intent purpose rental_deposit). The provider events of the deposit go to the RentalDepositHooks
// (rental_hooks.go); the link is paid through the existing pay page and succeeded path (fiscal document there).

// ChargeLinkTTL is the validity of a rental quick-cash payment link.
const ChargeLinkTTL = 72 * time.Hour

// ChargeLink is a payment link of rental charges (no email; shown as a QR on the phone).
type ChargeLink struct {
	IntentID  string // the payment request of the link (the provider intent is opened on the pay page)
	URL       string
	ExpiresAt time.Time
	Reused    bool // an open link of the same charges was returned
}

// ChargeLinkTx returns a payment link for charges (quick cash method link, D19). A second call for the same
// charges returns the open link. The provider webhook confirms through the existing path (fiscal document there).
// A pay page belongs to a customer: a walk-in renter is ErrCustomerRequired; no enabled checkout provider is
// ErrProviderDisabled; no open charge is ErrChargesNotOpen.
func (s *Service) ChargeLinkTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID string, chargeIDs []string) (*ChargeLink, error) {
	if customerID == "" {
		return nil, ErrCustomerRequired
	}
	if _, err := s.enabledProvider(ctx, tx, scope, needCheckout); err != nil {
		return nil, err
	}
	charges, err := store.LockChargesSorted(ctx, tx, scope, chargeIDs)
	if err != nil {
		return nil, err
	}
	open := make([]store.Charge, 0, len(charges))
	for _, c := range charges {
		if c.ResidualCents() > 0 && c.CustomerID == customerID {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return nil, ErrChargesNotOpen
	}
	slug, _, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(ChargeLinkTTL)
	if r, tok, err := s.reusableRequestTx(ctx, tx, scope, actor, customerID, open, expires); err != nil {
		return nil, err
	} else if r != nil {
		return &ChargeLink{IntentID: r.ID, URL: s.PayURL(slug, tok), ExpiresAt: r.ExpiresAt, Reused: true}, nil
	}
	r, tok, err := s.createRequestTx(ctx, tx, scope, actor, customerID, RequestManual, open, 0, expires)
	if err != nil {
		return nil, err
	}
	return &ChargeLink{IntentID: r.ID, URL: s.PayURL(slug, tok), ExpiresAt: r.ExpiresAt}, nil
}

// RentalDepositAuthorization is a pre-authorisation page of a rental deposit.
type RentalDepositAuthorization struct {
	IntentID    string
	CheckoutURL string
	ExpiresAt   time.Time
}

// enabledAuthorizer selects an enabled checkout provider whose driver implements ext.PaymentAuthorizer.
func (s *Service) enabledAuthorizer(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*enabled, error) {
	rows, err := store.ListProviderSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := &rows[i]
		if !r.Enabled || r.CredentialsEnc == "" {
			continue
		}
		drv, err := s.onlineDriver(r.ProviderCode)
		if err != nil || !drv.Capabilities().Checkout {
			continue
		}
		if _, ok := drv.(ext.PaymentAuthorizer); ok {
			return &enabled{drv: drv, row: r}, nil
		}
	}
	return nil, ErrProviderDisabled
}

// StartRentalDepositTx creates the manual-capture intent (purpose rental_deposit) of a booking deposit; the
// provider is called after commit by OpenRentalDeposit. No enabled provider implementing ext.PaymentAuthorizer
// is ErrProviderDisabled (the rental service answers 422 preauth_unavailable). A live intent of the booking with
// the same amount and provider is returned again (a second tap shows the same QR); another one is cancelled first.
func (s *Service) StartRentalDepositTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, bookingID, customerID string, amount money.Cents) (*store.PaymentIntent, error) {
	if customerID == "" {
		return nil, ErrCustomerRequired
	}
	if amount <= 0 {
		return nil, invalid("amount_cents", "positive")
	}
	e, err := s.enabledAuthorizer(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	prefix := PurposeRentalDeposit + ":" + bookingID + ":"
	live, err := store.LiveIntentsOf(ctx, tx, scope, "", "", PurposeRentalDeposit)
	if err != nil {
		return nil, err
	}
	for i := range live {
		in := &live[i]
		if len(in.IdempotencyKey) < len(prefix) || in.IdempotencyKey[:len(prefix)] != prefix {
			continue
		}
		if !in.AuthorizedAt.IsZero() || in.Status == store.IntentProcessing {
			return nil, ErrPaymentInFlight
		}
		if in.ProviderCode == e.row.ProviderCode && in.AmountCents == int64(amount) && in.CustomerID == customerID {
			return in, nil
		}
		if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentCancelled, "", s.now()); err != nil {
			return nil, err
		}
	}
	pi := &store.PaymentIntent{
		Purpose: PurposeRentalDeposit, ProviderCode: e.row.ProviderCode, CustomerID: customerID, AmountCents: int64(amount),
		Method: "card", ExpiresAt: s.now().Add(CheckoutTTL), CaptureMethod: store.CaptureManual,
	}
	return s.newIntentTx(ctx, tx, scope, actor, pi, bookingID, nil)
}

// OpenRentalDeposit opens the provider session of a created rental deposit intent (after commit).
func (s *Service) OpenRentalDeposit(ctx context.Context, scope store.MarinaScope, intentID string) (*RentalDepositAuthorization, error) {
	in, err := store.GetPaymentIntent(ctx, s.d.DB.DB, scope, intentID)
	if err != nil {
		return nil, err
	}
	if in.Purpose != PurposeRentalDeposit {
		return nil, ErrNotFound
	}
	e, err := s.providerByCode(ctx, s.d.DB.DB, scope, in.ProviderCode)
	if err != nil {
		return nil, ErrProviderDisabled
	}
	plan := &checkoutPlan{intent: in, e: e, desc: "Cauzione noleggio", captureManual: true}
	if in.CheckoutURL != "" && (in.ExpiresAt.IsZero() || in.ExpiresAt.After(s.now())) {
		plan.reuse = in.CheckoutURL
	}
	url, err := s.startCheckout(ctx, scope, plan)
	if err != nil {
		return nil, err
	}
	cur, err := store.GetPaymentIntent(ctx, s.d.DB.DB, scope, intentID)
	if err != nil {
		return nil, err
	}
	return &RentalDepositAuthorization{IntentID: cur.ID, CheckoutURL: url, ExpiresAt: cur.ExpiresAt}, nil
}

// CaptureIntentAmount captures part of an authorised intent (idempotency key capture:<intent>:<amount>); the
// succeeded webhook runs OnRentalDepositCapturedTx. A zero amount is VoidIntent.
func (s *Service) CaptureIntentAmount(ctx context.Context, scope store.MarinaScope, intentID string, amount money.Cents) error {
	if amount <= 0 {
		return s.VoidIntent(ctx, scope, intentID)
	}
	db := s.d.DB.DB
	in, err := store.GetPaymentIntent(ctx, db, scope, intentID)
	if err != nil {
		return err
	}
	if int64(amount) > in.AmountCents {
		return invalid("amount_cents", "above_authorised")
	}
	if in.AuthorizedAt.IsZero() || !in.CapturedAt.IsZero() || !in.VoidedAt.IsZero() || in.Status == store.IntentSucceeded {
		return nil
	}
	e, err := s.providerByCode(ctx, db, scope, in.ProviderCode)
	if err != nil {
		return err
	}
	az, ok := e.drv.(ext.PaymentAuthorizer)
	if !ok {
		return ErrProviderDisabled
	}
	cctx, err := s.withCreds(ctx, scope, e.row)
	if err != nil {
		return err
	}
	if _, err := az.Capture(cctx, ext.CaptureRequest{
		MarinaID: scope.ID(), IntentID: in.ID, IdempotencyKey: "capture:" + in.ID + ":" + strconv.FormatInt(int64(amount), 10),
		PaymentRef: in.ProviderPaymentRef, AmountCents: int64(amount), Currency: in.Currency,
	}); err != nil {
		var se interface{ ProviderCode() string }
		if errors.As(err, &se) {
			return providerError(err)
		}
		return err // transport error: the rental.deposits job tries again with the same idempotency key
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return store.MarkIntentCaptured(ctx, tx, scope, in.ID, s.now())
	})
}

// PreauthAvailable reports whether an enabled provider of the marina implements ext.PaymentAuthorizer (the manual
// driver never does).
func (s *Service) PreauthAvailable(ctx context.Context, db bun.IDB, scope store.MarinaScope) (bool, error) {
	_, err := s.enabledAuthorizer(ctx, db, scope)
	if errors.Is(err, ErrProviderDisabled) {
		return false, nil
	}
	return err == nil, err
}
