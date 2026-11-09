//go:build integration

package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestRentalDeposit_WalkInRetainRelease (m11-plan §8.1, D16, INV-98): a walk-in rental deposit taken in cash (no
// customer), idempotent by key; a partial retention creates the booking's deposit_retention charge matched to the
// deposit; a second retention, a retention above the unallocated amount and an invoice-mode retention without a
// customer are refused.
func TestRentalDeposit_WalkInRetainRelease(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		incl := true
		classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
		if err != nil || len(classes) == 0 {
			t.Fatal(err)
		}
		it, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
			Code: "CAU-TRAT", Name: catalog.I18nText{It: "Trattenuta"}, ItemType: "service", Unit: "lump", PriceIncludesVAT: &incl,
			RevenueClassID: classes[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		booking := store.NewID()
		run := func(fn func(tx bun.Tx) error) error {
			return db.RunInTx(ctx, func(_ context.Context, tx bun.Tx) error { return fn(tx) })
		}
		var dep *store.Payment
		in := RentalDepositInput{Method: store.PaymentMethodCash, Amount: 50000, IdempotencyKey: "rental_deposit:" + booking}
		if err := run(func(tx bun.Tx) error {
			var err error
			dep, err = e.svc.RecordRentalDepositTx(ctx, tx, e.scope, e.Owner.Actor(), in)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if dep.Kind != store.PaymentKindDeposit || dep.CustomerID != "" || dep.CashSessionID == "" || dep.AmountCents != 50000 {
			t.Fatalf("deposit %+v", dep)
		}
		if err := run(func(tx bun.Tx) error {
			again, err := e.svc.RecordRentalDepositTx(ctx, tx, e.scope, e.Owner.Actor(), in)
			if err == nil && again.ID != dep.ID {
				t.Errorf("replay created %s", again.ID)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		retain := func(amount int64, mode string) (*store.Charge, error) {
			var ch *store.Charge
			err := run(func(tx bun.Tx) error {
				var err error
				ch, err = e.svc.RetainDepositTx(ctx, tx, e.scope, e.Owner.Actor(), RetainDepositInput{
					DepositPaymentID: dep.ID, Amount: moneyOf(amount), FiscalMode: mode, ItemID: it.ID, BookingID: booking,
				})
				return err
			})
			return ch, err
		}
		if _, err := retain(18000, charge.FiscalInvoice); !errors.Is(err, charge.ErrCustomerRequired) {
			t.Fatalf("invoice without customer: %v", err)
		}
		if _, err := retain(60000, charge.FiscalReceipt); !errors.Is(err, ErrRefundExceeds) {
			t.Fatalf("above the deposit: %v", err)
		}
		ch, err := retain(18000, charge.FiscalReceipt)
		if err != nil {
			t.Fatal(err)
		}
		if ch.OriginType != store.ChargeOriginDepositRetention || ch.OriginID != booking || ch.FiscalMode != charge.FiscalReceipt ||
			ch.TotalCents != 18000 || ch.PaidAmountCents != 18000 {
			t.Fatalf("retention %+v", ch)
		}
		if _, err := retain(1000, charge.FiscalReceipt); err == nil {
			t.Fatal("second retention of the same booking accepted")
		}
		if got := e.payment(t, dep.ID); got.AllocatedCents != 18000 {
			t.Fatalf("allocated %d", got.AllocatedCents)
		}
		e.invariants(t)
	})
}

func moneyOf(v int64) money.Cents { return money.Cents(v) }
