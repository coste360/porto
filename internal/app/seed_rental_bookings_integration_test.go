//go:build integration

package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"testing"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestSeed_DemoRentalBookingsCoherent (m11-plan §16.2): the demo has held, confirmed, cancelled and no-show
// rentals, one expired hold and two blocks; Q32 holds; the holds job cancels the expired hold; a second run adds
// nothing.
func TestSeed_DemoRentalBookingsCoherent(t *testing.T) {
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
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}})
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
		for st, min := range map[string]int{
			store.RentalStatusHeld: 3, store.RentalStatusCancelled: 1, store.RentalStatusNoShow: 1,
		} {
			if n := count("rental_bookings", "status = ?", st); n < min {
				t.Fatalf("%d %s bookings, want >= %d", n, st, min)
			}
		}
		if count("rental_bookings", "status IN (?, ?)", store.RentalStatusConfirmed, store.RentalStatusCheckedOut) < 4 ||
			count("rental_blocks", "") != 2 {
			t.Fatal("rental seed incomplete")
		}
		bookings := count("rental_bookings", "")
		vs, err := store.CheckRentalInvariants(ctx, db.DB, res.MarinaID)
		if err != nil || len(vs) != 0 {
			t.Fatalf("rental invariants %+v %v", vs, err)
		}
		if err := a.RentalBookings.RunHoldsExpire(ctx); err != nil {
			t.Fatal(err)
		}
		if n := count("rental_bookings", "cancel_reason = ?", "hold_expired"); n != 1 {
			t.Fatalf("expired holds cancelled %d", n)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("rental_bookings", "") != bookings {
			t.Fatal("the second run added rental bookings")
		}
	})
}
