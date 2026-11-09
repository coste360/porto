//go:build integration

package collect

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestRentalLink_ChargeLinkAndPreauth (m11-plan §9.2-§9.3): the quick-cash link of rental charges needs a customer and
// an enabled provider, and a second call returns the open link; the deposit pre-authorisation needs a provider
// implementing ext.PaymentAuthorizer, reuses its live intent, opens the provider page after commit, and a partial
// capture goes out with the capture:<intent>:<amount> key.
func TestRentalLink_ChargeLinkAndPreauth(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Noleggio")
		ch := e.charge(t, cust, 16500, e.today())
		run := func(fn func(tx bun.Tx) error) error {
			return db.RunInTx(ctx(), func(_ context.Context, tx bun.Tx) error { return fn(tx) })
		}
		if ok, err := e.svc.PreauthAvailable(ctx(), db.DB, e.scope); err != nil || ok {
			t.Fatalf("preauth available without provider: %v %v", ok, err)
		}
		if err := run(func(tx bun.Tx) error {
			_, err := e.svc.ChargeLinkTx(ctx(), tx, e.scope, e.Owner.Actor(), cust, []string{ch.ID})
			return err
		}); !errors.Is(err, ErrProviderDisabled) {
			t.Fatalf("link without provider: %v", err)
		}
		e.enableFake(t)
		if ok, _ := e.svc.PreauthAvailable(ctx(), db.DB, e.scope); !ok {
			t.Fatal("fake provider implements PaymentAuthorizer")
		}
		if err := run(func(tx bun.Tx) error {
			_, err := e.svc.ChargeLinkTx(ctx(), tx, e.scope, e.Owner.Actor(), "", []string{ch.ID})
			return err
		}); !errors.Is(err, ErrCustomerRequired) {
			t.Fatalf("walk-in link: %v", err)
		}
		var first, second *ChargeLink
		for _, out := range []**ChargeLink{&first, &second} {
			if err := run(func(tx bun.Tx) error {
				var err error
				*out, err = e.svc.ChargeLinkTx(ctx(), tx, e.scope, e.Owner.Actor(), cust, []string{ch.ID})
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		if first.URL == "" || first.Reused || !second.Reused || second.URL != first.URL {
			t.Fatalf("links %+v %+v", first, second)
		}

		booking := store.NewID()
		var in, again *store.PaymentIntent
		for _, out := range []**store.PaymentIntent{&in, &again} {
			if err := run(func(tx bun.Tx) error {
				var err error
				*out, err = e.svc.StartRentalDepositTx(ctx(), tx, e.scope, e.Owner.Actor(), booking, cust, 50000)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		if in.ID != again.ID || in.Purpose != PurposeRentalDeposit || in.CaptureMethod != store.CaptureManual {
			t.Fatalf("intents %+v %+v", in, again)
		}
		auth, err := e.svc.OpenRentalDeposit(ctx(), e.scope, in.ID)
		if err != nil || auth.CheckoutURL == "" || !e.fake.LastCheckout().CaptureManual {
			t.Fatalf("open %+v %v", auth, err)
		}
		if err := run(func(tx bun.Tx) error {
			_, err := store.MarkIntentAuthorized(ctx(), tx, e.scope, in.ID, "pi_dep", e.Clock.Now())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.CaptureIntentAmount(ctx(), e.scope, in.ID, 18000); err != nil {
			t.Fatal(err)
		}
		if n := len(e.fake.Captures); n != 1 || e.fake.Captures[0].AmountCents != 18000 || e.fake.Captures[0].IdempotencyKey != "capture:"+in.ID+":18000" {
			t.Fatalf("captures %+v", e.fake.Captures)
		}
	})
}
