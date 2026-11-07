//go:build integration

package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// chargeRow inserts a customer (once per call) and a one-line N1 charge of total cents.
func chargeRow(t *testing.T, db *store.DB, marinaID string, total int64, number string) *store.Charge {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	cu := &store.Customer{
		ID: store.NewID(), MarinaID: marinaID, Kind: "person", LastName: "Rossi", BillingAddress: store.JSON(`{}`), Locale: "it",
		Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(cu).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	c := &store.Charge{
		ID: store.NewID(), MarinaID: marinaID, Number: number, CustomerID: cu.ID, OriginType: store.ChargeOriginManual,
		IssuedOn: calendar.MustParse("2027-06-01"), DueOn: calendar.MustParse("2027-07-01"), TotalCents: total, NetCents: total,
		PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	c.OriginID = c.ID
	if total < 0 {
		c.PaymentStatus, c.CreditedCents = store.ChargePaid, total
	}
	line := store.ChargeLine{
		ID: store.NewID(), MarinaID: marinaID, ChargeID: c.ID, Seq: 1, QtyMilli: 1000, NetCents: total, GrossCents: total,
		VATNature: "N1", PriceIncludesVAT: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertCharge(ctx, db.DB, c, []store.ChargeLine{line}); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestConcurrency_BillingSequence: 100 parallel transactions get 100 distinct, gap-free numbers.
func TestConcurrency_BillingSequence(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		_, marinaID := seedOrgMarina(t, db, "seq")
		scope, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		got := map[int64]bool{}
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := db.RunInTx(context.Background(), func(ctx context.Context, tx bun.Tx) error {
					n, err := store.NextBillingNumber(ctx, tx, scope, store.SeriesCharge, 2027)
					if err != nil {
						return err
					}
					mu.Lock()
					defer mu.Unlock()
					if got[n] {
						t.Errorf("number %d handed out twice", n)
					}
					got[n] = true
					return nil
				})
				if err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		for i := int64(1); i <= 100; i++ {
			if !got[i] {
				t.Fatalf("number %d missing (%d numbers)", i, len(got))
			}
		}
	})
}

// TestCharges_ConditionalWrites: MarkExported skips charges already exported, ApplyOffset never exceeds the
// residual, EditCharge refuses lines on an exported charge.
func TestCharges_ConditionalWrites(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		_, marinaID := seedOrgMarina(t, db, "cond")
		ctx := context.Background()
		scope, err := store.ResolveMarinaScope(ctx, db.DB, marinaID)
		if err != nil {
			t.Fatal(err)
		}
		a := chargeRow(t, db, marinaID, 10000, "A2027-000001")
		b := chargeRow(t, db, marinaID, 5000, "A2027-000002")
		neg := chargeRow(t, db, marinaID, -3000, "A2027-000003")
		now := time.Now()
		exportID := store.NewID()
		done, err := store.MarkExported(ctx, db.DB, scope, []string{a.ID}, exportID, now)
		if err != nil || len(done) != 1 {
			t.Fatalf("first mark %v %v", done, err)
		}
		done, err = store.MarkExported(ctx, db.DB, scope, []string{a.ID, b.ID}, store.NewID(), now)
		if err != nil || len(done) != 1 || done[0] != b.ID {
			t.Fatalf("second mark %v %v", done, err)
		}
		ok, err := store.ApplyOffset(ctx, db.DB, scope, neg.ID, b.ID, 3000, now)
		if err != nil || !ok {
			t.Fatalf("offset %v %v", ok, err)
		}
		ok, err = store.ApplyOffset(ctx, db.DB, scope, neg.ID, b.ID, 2001, now)
		if err != nil || ok {
			t.Fatalf("offset above the residual accepted: %v %v", ok, err)
		}
		got, _ := store.GetCharge(ctx, db.DB, scope, b.ID)
		if got.CreditedCents != 3000 || got.PaymentStatus != store.ChargePartiallyPaid {
			t.Fatalf("after offset %+v", got)
		}
		err = store.EditCharge(ctx, db.DB, scope, a.ID, 0, store.ChargeEdit{Lines: []store.ChargeLine{}, Total: 1, Net: 1}, now)
		if err == nil {
			t.Fatal("lines of an exported charge edited")
		}
		due := calendar.MustParse("2027-09-01")
		if err := store.EditCharge(ctx, db.DB, scope, a.ID, 0, store.ChargeEdit{DueOn: &due}, now); err != nil {
			t.Fatalf("due date of an exported charge: %v", err)
		}
	})
}
