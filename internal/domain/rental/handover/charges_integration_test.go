//go:build integration

package handover

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// The main charge of a booking is cancelled and re-created while untouched (unexported, unpaid), and corrected by
// linked adjustments only once it is exported or paid (m11-plan §6, §9.1, D18, D22, INV-99).

func (e *env) actor() audit.Actor { return audit.Actor{Type: audit.ActorUser, ID: e.OwnerID} }

// inLockedTx runs fn with the booking locked, as the handover and reservation transactions do.
func (e *env) inLockedTx(t testing.TB, id string, fn func(ctx context.Context, tx bun.Tx, b *store.RentalBooking) error) error {
	t.Helper()
	return e.DB.RunInTx(ctx(), func(c context.Context, tx bun.Tx) error {
		b, err := store.LockRentalBooking(c, tx, e.scope, id)
		if err != nil {
			return err
		}
		return fn(c, tx, b)
	})
}

func (e *env) ensureMain(t testing.TB, id string) *store.Charge {
	t.Helper()
	var ch *store.Charge
	if err := e.inLockedTx(t, id, func(c context.Context, tx bun.Tx, b *store.RentalBooking) error {
		var err error
		ch, err = e.svc.EnsureMainChargeTx(c, tx, e.scope, e.actor(), b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ch == nil {
		t.Fatal("no main charge")
	}
	return ch
}

// links returns the link kinds of a booking's charges by charge id.
func (e *env) links(t testing.TB, id string) []store.RentalLinkedCharge {
	t.Helper()
	rows, err := store.RentalChargesOf(ctx(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// rentalInvariants returns the Q33 and B60 violations (this env inserts bookings without slot rows, so Q32 is out
// of scope here).
func (e *env) rentalInvariants(t testing.TB) []store.Violation {
	t.Helper()
	vs, err := store.CheckRentalInvariants(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	out := []store.Violation{}
	for _, v := range vs {
		if v.Query != "Q32" {
			out = append(out, v)
		}
	}
	return out
}

// reprice rewrites the snapshot total of a booking (as a reschedule would) and runs AdjustMainChargeTx.
func (e *env) reprice(t testing.TB, id string, unitCents int64, invoice *bool) (*store.Charge, error) {
	t.Helper()
	var out *store.Charge
	err := e.inLockedTx(t, id, func(c context.Context, tx bun.Tx, b *store.RentalBooking) error {
		var snap price.Snapshot
		if err := json.Unmarshal(b.PriceSnapshot, &snap); err != nil {
			return err
		}
		if unitCents > 0 {
			old := snap.Breakdown.Lines[0].Amount
			snap.Breakdown.Lines[0].UnitCents, snap.Breakdown.Lines[0].Amount = money.Cents(unitCents), money.Cents(unitCents)
			snap.Breakdown.Total += money.Cents(unitCents) - old
		}
		raw, _ := json.Marshal(snap)
		b.PriceSnapshot, b.TotalCents = store.JSON(raw), int64(snap.Breakdown.Total)
		if invoice != nil {
			b.InvoiceRequested = *invoice
		}
		if _, err := tx.NewUpdate().Model(b).Column("price_snapshot", "total_cents", "invoice_requested").
			Where("id = ?", b.ID).Exec(c); err != nil {
			return err
		}
		var err error
		out, err = e.svc.AdjustMainChargeTx(c, tx, e.scope, e.actor(), b)
		return err
	})
	return out, err
}

// TestRental_CancelledMainChargeRecreated (INV-99, INV-51): the office cancels an unpaid main charge (charge cancel
// hook of origin rental): its link becomes replaced, the next quick cash creates a new main charge instead of 409;
// a closed booking left without a live main charge is reported by Q33b.
func TestRental_CancelledMainChargeRecreated(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)
		b := e.booking(t, "", false)
		first := e.ensureMain(t, b.ID)
		if _, err := e.charges.Cancel(ctx(), e.Owner, e.MarinaID, first.ID, first.Version, "errore"); err != nil {
			t.Fatal(err)
		}
		if rows := e.links(t, b.ID); len(rows) != 1 || rows[0].LinkKind != store.RentalChargeReplaced {
			t.Fatalf("links after office cancel %+v", rows)
		}
		qc, err := e.svc.QuickCash(ctx(), e.Owner, e.MarinaID, b.ID, QuickCashInput{Target: TargetMain, Method: store.PaymentMethodCash})
		if err != nil {
			t.Fatalf("quick cash after a cancelled main charge: %v", err)
		}
		paid := 0
		for _, c := range qc.Charges {
			if c.Kind == store.RentalChargeMain && c.ChargeID != first.ID && c.Status == store.ChargePaid {
				paid++
			}
		}
		if paid != 1 || len(qc.FiscalDocumentIDs) != 1 {
			t.Fatalf("quick cash %+v", qc)
		}
		if got := e.sum(t, "SELECT COUNT(*) FROM rental_booking_charges WHERE booking_id = ? AND kind = 'main'", b.ID); got != 1 {
			t.Fatalf("main links %d", got)
		}
		if vs := e.rentalInvariants(t); len(vs) != 0 {
			t.Fatalf("rental invariants %+v", vs)
		}
		e.invariants(t)

		// Q33b: a closed booking whose only main charge was cancelled is reported.
		c := e.booking(t, "", false)
		ch := e.ensureMain(t, c.ID)
		if _, err := e.charges.Cancel(ctx(), e.Owner, e.MarinaID, ch.ID, ch.Version, "errore"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().Table("rental_bookings").Set("status = ?", store.RentalStatusClosed).Where("id = ?", c.ID).Exec(ctx()); err != nil {
			t.Fatal(err)
		}
		vs := e.rentalInvariants(t)
		if len(vs) != 1 || vs[0].Query != "Q33" || vs[0].IDs[0] != c.ID {
			t.Fatalf("Q33b %+v", vs)
		}
	})
}

// TestRental_MainChargeReplacedOrAdjusted (§6, §9.1, D18, D22): a reprice or a cancellation of an untouched main
// charge cancels it (link replaced) and re-creates it, nothing reaches the export as +X/-X; once the main charge is
// paid a reprice is a linked delta adjustment and switching invoice_requested is refused.
func TestRental_MainChargeReplacedOrAdjusted(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)

		// Reprice of an unpaid main charge: replaced, no adjustment.
		b := e.booking(t, e.Customer, false)
		first := e.ensureMain(t, b.ID)
		got, err := e.reprice(t, b.ID, 18000, nil)
		if err != nil || got == nil || got.ID == first.ID || got.TotalCents != 19500 {
			t.Fatalf("replace on reprice %+v %v", got, err)
		}
		kinds := map[string]int{}
		for _, r := range e.links(t, b.ID) {
			kinds[r.LinkKind]++
		}
		if kinds[store.RentalChargeReplaced] != 1 || kinds[store.RentalChargeMain] != 1 || kinds[store.RentalChargeAdjustment] != 0 {
			t.Fatalf("links %v", kinds)
		}

		// Switching to invoice while the main charge is unpaid: re-created in invoice mode.
		inv := true
		got, err = e.reprice(t, b.ID, 0, &inv)
		if err != nil || got == nil || got.FiscalMode != "invoice" || got.CustomerID != e.Customer {
			t.Fatalf("fiscal switch %+v %v", got, err)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM charges WHERE origin_type = 'rental' AND origin_id = ? AND payment_status <> 'cancelled'", b.ID); n != 1 {
			t.Fatalf("live rental charges %d", n)
		}

		// Booking cancelled with an unpaid main charge: cancelled, no negative adjustment.
		c := e.booking(t, "", false)
		main := e.ensureMain(t, c.ID)
		if err := e.inLockedTx(t, c.ID, func(cx context.Context, tx bun.Tx, bk *store.RentalBooking) error {
			bk.Status = store.RentalStatusCancelled
			return e.svc.RentalEndedTx(cx, tx, e.scope, e.actor(), bk)
		}); err != nil {
			t.Fatal(err)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM charges WHERE origin_type = 'rental' AND origin_id = ?", c.ID); n != 1 {
			t.Fatalf("charges of the cancelled booking %d (a +X/-X pair?)", n)
		}
		if st := e.sum(t, "SELECT CASE WHEN payment_status = 'cancelled' THEN 1 ELSE 0 END FROM charges WHERE id = ?", main.ID); st != 1 {
			t.Fatal("main charge not cancelled")
		}

		// Paid main charge: a reprice is a delta adjustment; the fiscal switch is refused.
		d := e.booking(t, e.Customer, false)
		if _, err := e.svc.QuickCash(ctx(), e.Owner, e.MarinaID, d.ID, QuickCashInput{Target: TargetMain, Method: store.PaymentMethodCash}); err != nil {
			t.Fatal(err)
		}
		adj, err := e.reprice(t, d.ID, 16000, nil)
		if err != nil || adj == nil || adj.TotalCents != 1000 || adj.CorrectsChargeID == "" {
			t.Fatalf("delta adjustment %+v %v", adj, err)
		}
		var ve *auth.ValidationError
		if _, err := e.reprice(t, d.ID, 0, &inv); !errors.As(err, &ve) {
			t.Fatalf("fiscal switch of a paid main charge: %v", err)
		}
		e.invariants(t)
		if vs := e.rentalInvariants(t); len(vs) != 0 {
			t.Fatalf("rental invariants %+v", vs)
		}
	})
}

// TestHandover_DepositFromBookingSnapshot: the deposit due at check-out and its default amount come from the
// booking's snapshot, not from the unit's current deposit.
func TestHandover_DepositFromBookingSnapshot(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)
		b := e.booking(t, "", false) // deposit 500.00
		if _, err := db.NewUpdate().Table("rental_units").Set("deposit_cents = 0").Where("id = ?", e.unit.ID).Exec(ctx()); err != nil {
			t.Fatal(err)
		}
		e.prepareCheckOutNoDeposit(t, b)
		v, err := e.svc.GetHandover(ctx(), e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range v.Missing {
			found = found || m == MissingDeposit
		}
		if !found {
			t.Fatalf("missing %v lacks deposit (booking snapshot 500.00)", v.Missing)
		}
		if _, err := e.svc.StartDeposit(ctx(), e.Owner, e.MarinaID, b.ID, DepositInput{Mode: store.RentalDepositCash}); err != nil {
			t.Fatal(err)
		}
		if got := e.reload(t, b.ID); got.DepositCents != 50000 {
			t.Fatalf("deposit %d, want the booking's 500.00", got.DepositCents)
		}
	})
}
