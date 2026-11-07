//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestCollectStore_Guarantees covers the database guarantees the collection, cash and fiscal services rely on:
// provider events recorded once, consecutive fiscal numbers per day, one reminder run per day, the default register
// created once, one open session per register and the no-op conditional update of an open session.
func TestCollectStore_Guarantees(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		s, _, _, _ := occupancySetup(t, db)
		now := t0
		ev := &store.PaymentProviderEvent{ID: store.NewID(), MarinaID: s.ID(), ProviderCode: "fakepay", EventID: "evt_1", Status: "applied", ReceivedAt: now, CreatedAt: now}
		for i, want := range []bool{true, false} {
			ev.ID = store.NewID()
			ok, err := store.InsertProviderEvent(ctx, db.DB, ev)
			if err != nil || ok != want {
				t.Fatalf("event %d: %v %v", i, ok, err)
			}
		}
		day := calendar.MustParse("2027-07-03")
		for want := int64(1); want <= 3; want++ {
			n, err := store.NextFiscalNumber(ctx, db.DB, s, day)
			if err != nil || n != want {
				t.Fatalf("fiscal number %d: %d %v", want, n, err)
			}
		}
		if store.FiscalNumber(day, 7) != "20270703-0007" {
			t.Fatal("format")
		}
		run := &store.ReminderRun{ID: store.NewID(), MarinaID: s.ID(), RunOn: day, Status: "running", Counts: store.JSON(`{}`), StartedAt: now, CreatedAt: now, UpdatedAt: now}
		r1, created, err := store.InsertReminderRun(ctx, db.DB, s, run)
		if err != nil || !created {
			t.Fatalf("run: %v", err)
		}
		dup := *run
		dup.ID = store.NewID()
		r2, created, err := store.InsertReminderRun(ctx, db.DB, s, &dup)
		if err != nil || created || r2.ID != r1.ID {
			t.Fatalf("second run: %v %v", created, err)
		}
		reg, err := store.EnsureDefaultCashRegister(ctx, db.DB, s, "Cassa", now)
		if err != nil {
			t.Fatal(err)
		}
		if again, err := store.EnsureDefaultCashRegister(ctx, db.DB, s, "Cassa", now); err != nil || again.ID != reg.ID {
			t.Fatalf("default twice: %v", err)
		}
		sess := &store.CashSession{
			ID: store.NewID(), MarinaID: s.ID(), RegisterID: reg.ID, Status: store.CashSessionOpen, OpenedAt: now, BusinessDate: day,
			Expected: store.JSON(`{}`), Counted: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertCashSession(ctx, db.DB, sess); err != nil {
			t.Fatal(err)
		}
		other := *sess
		other.ID = store.NewID()
		if err := store.InsertCashSession(ctx, db.DB, &other); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second open session: %v", err)
		}
		if ok, err := store.TouchOpenCashSession(ctx, db.DB, s, sess.ID); err != nil || !ok {
			t.Fatalf("touch open: %v %v", ok, err)
		}
		if err := store.CloseCashSession(ctx, db.DB, s, sess, 1, now); err != nil {
			t.Fatal(err)
		}
		if ok, err := store.TouchOpenCashSession(ctx, db.DB, s, sess.ID); err != nil || ok {
			t.Fatalf("touch closed: %v %v", ok, err)
		}
		if vs, err := store.CheckBillingInvariants(ctx, db.DB, s); err != nil || len(vs) != 0 {
			t.Fatalf("invariants %+v %v", vs, err)
		}
	})
}
