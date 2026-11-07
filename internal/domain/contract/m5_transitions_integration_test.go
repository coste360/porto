//go:build integration

package contract

import (
	"context"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestContract_M5StateChangesWriteAuditAndOutbox is INV-30 for the transitions added in M5 (m5-plan §8.1):
// held → pending_payment (request_payment), pending_payment → held (payment_expired), pending_payment → confirmed,
// cancelled and expired, and active → suspended for arrears: each writes one audit row of the contract and the
// outbox events of its transition row in the same transaction.
func TestContract_M5StateChangesWriteAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		o := e.Owner
		scope := e.scope(t)
		inTx := func(fn func(ctx context.Context, tx bun.Tx) error) {
			t.Helper()
			if err := db.RunInTx(ctx, fn); err != nil {
				t.Fatal(err)
			}
		}
		request := func(id string) {
			t.Helper()
			b := e.tally(t, id)
			inTx(func(ctx context.Context, tx bun.Tx) error {
				_, err := e.svc.RequestTransitPaymentTx(ctx, tx, scope, o, id, 0, e.Clock.Now().Add(30*time.Minute))
				return err
			})
			expectTransition(t, "request_payment", b, e.tally(t, id), next(t, TransitTransitions, StatusHeld, ActionRequestPayment))
		}

		// request_payment, payment_expired, request again, confirm (manual driver).
		a := e.transit(t, o, e.berths[0], "2027-06-02", "2027-06-05", false).Row.ID
		request(a)
		b := e.tally(t, a)
		inTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.svc.RevertTransitPaymentTx(ctx, tx, scope, o, a)
			return err
		})
		expectTransition(t, "payment_expired", b, e.tally(t, a), next(t, TransitTransitions, StatusPendingPayment, ActionPaymentExpired))
		request(a)
		b = e.tally(t, a)
		inTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.svc.ConfirmTransitPaidTx(ctx, tx, scope, o, a, SourceManual)
			return err
		})
		expectTransition(t, "pending_payment confirm", b, e.tally(t, a), next(t, TransitTransitions, StatusPendingPayment, ActionConfirm))

		// pending_payment cancel.
		c := e.transit(t, o, e.berths[1], "2027-06-02", "2027-06-05", false).Row.ID
		request(c)
		cur, _ := store.GetContract(ctx, db.DB, scope, c)
		b = e.tally(t, c)
		if _, err := e.svc.CancelTransit(ctx, o, e.MarinaID, c, cur.Version, "test"); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "pending_payment cancel", b, e.tally(t, c), next(t, TransitTransitions, StatusPendingPayment, ActionCancel))

		// pending_payment expire: the extended hold lapses and the expiry job runs.
		x := e.transit(t, o, e.berths[2], "2027-06-02", "2027-06-05", false).Row.ID
		request(x)
		e.Clock.Advance(2 * time.Hour)
		b = e.tally(t, x)
		if _, err := e.svc.ExpireMarina(ctx, e.MarinaID); err != nil {
			t.Fatal(err)
		}
		if got, _ := store.GetContract(ctx, db.DB, scope, x); got.Status != string(StatusExpired) {
			t.Fatalf("expired transit: %s", got.Status)
		}
		expectTransition(t, "pending_payment expire", b, e.tally(t, x), next(t, TransitTransitions, StatusPendingPayment, ActionExpire))

		// Mooring suspended for arrears.
		m := e.mooring(t, o, e.berths[3], "2027-06-10", true).Row.ID
		b = e.tally(t, m)
		inTx(func(ctx context.Context, tx bun.Tx) error {
			// An open charge of the contract overdue for the check (the re-check under lock needs one).
			now := store.UTC(e.Clock.Now())
			ch := &store.Charge{
				ID: store.NewID(), MarinaID: e.MarinaID, Number: "ARR-1", CustomerID: e.cust, ContractID: m, OriginType: store.ChargeOriginManual,
				IssuedOn: d("2027-06-10"), DueOn: d("2027-06-10"), TotalCents: 100_00, NetCents: 100_00, PaymentStatus: store.ChargeOpen,
				InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			ch.OriginID = ch.ID
			line := store.ChargeLine{
				ID: store.NewID(), MarinaID: e.MarinaID, ChargeID: ch.ID, Seq: 1, QtyMilli: 1000, NetCents: 100_00, GrossCents: 100_00,
				VATNature: "N1", PriceIncludesVAT: true, CreatedAt: now, UpdatedAt: now,
			}
			if err := store.InsertCharge(ctx, tx, ch, []store.ChargeLine{line}); err != nil {
				return err
			}
			_, err := e.svc.SuspendForArrearsTx(ctx, tx, scope, m, ArrearsCheck{Cutoff: d("2027-09-01"), Today: d("2027-09-01")})
			return err
		})
		expectTransition(t, "suspend arrears", b, e.tally(t, m), next(t, MooringTransitions, StatusActive, ActionSuspend))
		if got, _ := store.GetContract(ctx, db.DB, scope, m); got.SuspendReason != ArrearsReason {
			t.Fatalf("suspend reason %q", got.SuspendReason)
		}
	})
}
