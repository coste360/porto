//go:build integration

package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestSeed_DemoRentalHandoversCoherent (m11-plan §16.2): the demo has two rentals out at sea and the closed ones of
// today and the last three days (two with a damage and a partially retained deposit, the others released); the
// billing invariants hold; a second run adds nothing. Today's rentals follow fixed bands of the day, not the
// moment the seed ran: seeded at 07:00 and viewed at 11:00, 15:00 and 16:30 the rental phone screen shows exactly
// one late card (the showcase out at sea, due back at 09:30), never every card in "Ritardo".
func TestSeed_DemoRentalHandoversCoherent(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	day := func(h, m int) time.Time { return time.Date(2026, time.July, 10, h, m, 0, 0, rome) }
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		vars := map[string]string{
			"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_DEV": "true", "PORTO_DATA_DIR": t.TempDir(),
		}
		cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
		if err != nil {
			t.Fatal(err)
		}
		a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(day(7, 0))})
		if err != nil {
			t.Fatal(err)
		}
		ctx := fixture.Ctx()
		res, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		count := func(table, where string, args ...any) int {
			q := db.NewSelect().TableExpr(table).Where("marina_id = ?", res.MarinaID)
			if where != "" {
				q = q.Where(where, args...)
			}
			n, err := q.Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		if n := count("rental_bookings", "status = ?", store.RentalStatusCheckedOut); n != 2 {
			t.Fatalf("%d rentals out at sea", n)
		}
		if n := count("rental_bookings", "status = ?", store.RentalStatusClosed); n < 10 {
			t.Fatalf("%d closed rentals", n)
		}
		if count("rental_damages", "status = ?", store.RentalDamageCharged) != 2 ||
			count("rental_bookings", "deposit_status = ?", store.RentalDepositStatusPartiallyRetained) != 2 {
			t.Fatal("damage or retained deposit missing")
		}
		p := demoOwnerPrincipal(ctx, t, a, res)
		for _, viewAt := range []time.Time{day(11, 0), day(15, 0), day(16, 30)} {
			d, err := a.RentalBookings.WithClock(clock.NewFake(viewAt)).Day(ctx, p, res.MarinaID, reservation.DayQuery{})
			if err != nil {
				t.Fatal(err)
			}
			late := 0
			for _, e := range append(d.Pickups, d.Out...) {
				if e.Late {
					late++
				}
			}
			if len(d.Out) != 2 || late != 1 || len(d.Late) != 1 || len(d.Pickups) == 0 {
				t.Fatalf("viewed at %s: %d out, %d pickups, %d late cards (%d in Ritardo)", viewAt.Format("15:04"),
					len(d.Out), len(d.Pickups), late, len(d.Late))
			}
		}
		handovers := count("rental_handovers", "")
		scope, err := store.SystemMarinaScope(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if vs, err := store.CheckBillingInvariants(ctx, db.DB, scope); err != nil || len(vs) != 0 {
			t.Fatalf("billing invariants %v %v", vs, err)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("rental_handovers", "") != handovers {
			t.Fatal("second run added handovers")
		}
	})
}
