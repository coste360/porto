package collect

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Rental deposit pre-authorisations (M11, m11-plan §9.2, D15). An intent of purpose rental_deposit is a
// manual-capture authorisation of a rental deposit: its provider events run the RentalDepositHooks in the webhook
// transaction (INV-56 dedupe unchanged), never the charge matching of succeededTx. internal/domain/rental/handover
// implements the hooks and app.go wires them; collect never imports rental.

// RentalDepositHooks receive the provider events of rental deposit intents, inside the event transaction.
type RentalDepositHooks interface {
	// OnRentalDepositAuthorizedTx moves the booking deposit pending -> authorized.
	OnRentalDepositAuthorizedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error
	// OnRentalDepositCapturedTx records the captured amount as a deposit payment (card_online, providerRef),
	// retains it in full and closes the booking when nothing else is pending. It is idempotent on providerRef.
	OnRentalDepositCapturedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, amount money.Cents, providerRef string) error
	// OnRentalDepositVoidedTx releases the deposit (status released) of a voided or expired authorisation.
	OnRentalDepositVoidedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error
}

// SetRentalDepositHooks wires the rental hooks (app.go, before serving).
func (s *Service) SetRentalDepositHooks(h RentalDepositHooks) { s.rental = h }

func (s *Service) rentalDepositAuthorizedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error {
	if s.rental == nil {
		return nil
	}
	return s.rental.OnRentalDepositAuthorizedTx(ctx, tx, scope, actor, in)
}

func (s *Service) rentalDepositCapturedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, ev *ext.PaymentEvent) error {
	if s.rental == nil {
		return nil
	}
	amount := money.Cents(ev.AmountCents)
	if amount <= 0 {
		amount = money.Cents(in.AmountCents)
	}
	ref := ev.PaymentRef
	if ref == "" {
		ref = in.ProviderPaymentRef
	}
	if ref == "" {
		ref = "intent:" + in.ID
	}
	return s.rental.OnRentalDepositCapturedTx(ctx, tx, scope, actor, in, amount, ref)
}

func (s *Service) rentalDepositVoidedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error {
	if s.rental == nil {
		return nil
	}
	return s.rental.OnRentalDepositVoidedTx(ctx, tx, scope, actor, in)
}
