//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestPaymentsStore_Limits covers the conditional updates the ledger relies on (INV-52): allocated + refunded
// never above the amount, a charge's paid amount never above its total nor below 0, the status recomputed from
// paid + credited, the insert-only matches, the ledger numbering and the work item upsert.
func TestPaymentsStore_Limits(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		s, _, _, _ := occupancySetup(t, db)
		now := t0
		cust := &store.Customer{
			ID: store.NewID(), MarinaID: s.ID(), Kind: "person", LastName: "Rossi", FirstName: "Mario",
			BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
			SearchText: "rossi", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertCustomer(ctx, db.DB, cust); err != nil {
			t.Fatal(err)
		}
		day := calendar.MustParse("2027-06-01")
		ch := &store.Charge{
			ID: store.NewID(), MarinaID: s.ID(), Number: "A2027-000001", CustomerID: cust.ID, OriginType: store.ChargeOriginManual,
			OriginID: store.NewID(), IssuedOn: day, DueOn: day, TotalCents: 100_00, NetCents: 81_97, VATCents: 18_03,
			PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := db.NewInsert().Model(ch).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		n1, err := store.NextLedgerNumber(ctx, db.DB, s, "payment", 2027)
		if err != nil {
			t.Fatal(err)
		}
		n2, _ := store.NextLedgerNumber(ctx, db.DB, s, "payment", 2027)
		if n1 != 1 || n2 != 2 {
			t.Fatalf("numbers %d %d", n1, n2)
		}
		p := &store.Payment{
			ID: store.NewID(), MarinaID: s.ID(), Number: "P2027-000001", CustomerID: cust.ID, Kind: "payment", Method: "bank_transfer",
			Source: "manual", AmountCents: 60_00, Currency: "EUR", ReceivedOn: day, RecordedByActor: "test", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertPayment(ctx, db.DB, p); err != nil {
			t.Fatal(err)
		}
		if ok, err := store.AddPaymentAllocated(ctx, db.DB, s, p.ID, 50_00, now); err != nil || !ok {
			t.Fatalf("allocate %v %v", ok, err)
		}
		if ok, _ := store.AddPaymentRefunded(ctx, db.DB, s, p.ID, 11_00, now); ok {
			t.Fatal("refund above the unallocated money")
		}
		if ok, _ := store.AddPaymentRefunded(ctx, db.DB, s, p.ID, 10_00, now); !ok {
			t.Fatal("refund of the unallocated money")
		}
		if ok, _ := store.AddPaymentAllocated(ctx, db.DB, s, p.ID, 1, now); ok {
			t.Fatal("allocation above the amount")
		}
		if ok, _ := store.AddPaymentAllocated(ctx, db.DB, s, p.ID, -50_01, now); ok {
			t.Fatal("allocated below 0")
		}
		status, paid, ok, err := store.AddChargePaid(ctx, db.DB, s, ch.ID, 40_00, now)
		if err != nil || !ok || status != store.ChargePartiallyPaid || paid != 40_00 {
			t.Fatalf("partial %s %d %v %v", status, paid, ok, err)
		}
		if _, _, ok, _ := store.AddChargePaid(ctx, db.DB, s, ch.ID, 60_01, now); ok {
			t.Fatal("paid above the total")
		}
		if status, _, ok, _ = store.AddChargePaid(ctx, db.DB, s, ch.ID, 60_00, now); !ok || status != store.ChargePaid {
			t.Fatalf("paid %s %v", status, ok)
		}
		if _, _, ok, _ := store.AddChargePaid(ctx, db.DB, s, ch.ID, 1, now); ok {
			t.Fatal("positive delta on a paid charge")
		}
		if status, _, ok, _ = store.AddChargePaid(ctx, db.DB, s, ch.ID, -100_00, now); !ok || status != store.ChargeOpen {
			t.Fatalf("reversal %s %v", status, ok)
		}
		m := &store.PaymentMatch{ID: store.NewID(), MarinaID: s.ID(), PaymentID: p.ID, ChargeID: ch.ID, AmountCents: 10_00, CreatedByActor: "test", CreatedAt: now}
		if err := store.InsertPaymentMatch(ctx, db.DB, m); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().TableExpr("payment_matches").Set("amount_cents = 1").Where("id = ?", m.ID).Exec(ctx); !store.IsTriggerViolation(err, store.InsertOnlyMessage) {
			t.Fatalf("matches are insert-only: %v", err)
		}
		// Partial reversals add up to the match amount, never beyond it (00017).
		for _, part := range []int64{4_00, 6_00} {
			if _, ok, err := store.AddMatchReversed(ctx, db.DB, s, m.ID, part); err != nil || !ok {
				t.Fatalf("partial reversal %d: %v %v", part, ok, err)
			}
			rev := &store.PaymentMatch{ID: store.NewID(), MarinaID: s.ID(), PaymentID: p.ID, ChargeID: ch.ID, AmountCents: -part, ReversesMatchID: m.ID, CreatedByActor: "test", CreatedAt: now}
			if err := store.InsertPaymentMatch(ctx, db.DB, rev); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := store.AddMatchReversed(ctx, db.DB, s, rev.ID, 1); ok {
				t.Fatal("a reversal row is not reversible")
			}
		}
		if _, ok, err := store.AddMatchReversed(ctx, db.DB, s, m.ID, 1); err != nil || ok {
			t.Fatalf("reversal beyond the match: %v %v", ok, err)
		}
		if _, err := db.NewUpdate().TableExpr("payment_matches").Set("reversed_cents = 1001").Where("id = ?", m.ID).Exec(ctx); err == nil {
			t.Fatal("payment_matches_reversed_check: reversed above the amount")
		}
		if _, err := db.NewUpdate().TableExpr("payment_matches").Set("reversed_cents = 1").Where("id = ?", m.ID).Exec(ctx); !store.IsTriggerViolation(err, store.InsertOnlyMessage) {
			t.Fatalf("reversed_cents never decreases: %v", err)
		}
		if _, err := db.NewUpdate().TableExpr("payment_matches").Set("reversed_cents = reversed_cents, created_by_actor = 'x'").Where("id = ?", m.ID).Exec(ctx); !store.IsTriggerViolation(err, store.InsertOnlyMessage) {
			t.Fatalf("only reversed_cents may change: %v", err)
		}
		// Work item states: create, update, idempotent raise.
		w := &store.WorkItemState{SourceType: "late_payment", SourceID: p.ID, Status: store.WorkItemInProgress}
		if err := store.SaveWorkItemState(ctx, db.DB, s, w, now); err != nil {
			t.Fatal(err)
		}
		w2 := &store.WorkItemState{SourceType: "late_payment", SourceID: p.ID, Status: store.WorkItemDone, Note: "fatto"}
		if err := store.SaveWorkItemState(ctx, db.DB, s, w2, now); err != nil {
			t.Fatal(err)
		}
		if err := store.RaiseWorkItem(ctx, db.DB, s, "late_payment", p.ID, "", now); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetWorkItemState(ctx, db.DB, s, "late_payment", p.ID)
		if err != nil || got.Status != store.WorkItemDone || got.Version != 2 || got.Note != "fatto" {
			t.Fatalf("work item %+v %v", got, err)
		}
		// A new occurrence of the signal reopens the closed item; raising it again leaves it as it is.
		for range 2 {
			if err := store.RaiseWorkItem(ctx, db.DB, s, "late_payment", p.ID, "second", now); err != nil {
				t.Fatal(err)
			}
		}
		got, err = store.GetWorkItemState(ctx, db.DB, s, "late_payment", p.ID)
		if err != nil || got.Status != store.WorkItemOpen || got.Occurrence != "second" || got.Version != 3 || got.Note != "fatto" {
			t.Fatalf("reopened work item %+v %v", got, err)
		}
	})
}
