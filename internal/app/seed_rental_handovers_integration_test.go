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

// TestSeed_DemoRentalHandoversCoherent (m11-plan §16.2): the demo has a rental out at sea and two returned and
// closed ones (one with a damage and a partially retained deposit, one released); the billing invariants hold; a
// second run adds nothing.
func TestSeed_DemoRentalHandoversCoherent(t *testing.T) {
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
		if n := count("rental_bookings", "status = ?", store.RentalStatusCheckedOut); n < 1 {
			t.Fatalf("%d rentals out at sea", n)
		}
		if n := count("rental_bookings", "status = ?", store.RentalStatusClosed); n < 2 {
			t.Fatalf("%d closed rentals", n)
		}
		if count("rental_damages", "status = ?", store.RentalDamageCharged) != 1 ||
			count("rental_bookings", "deposit_status = ?", store.RentalDepositStatusPartiallyRetained) != 1 {
			t.Fatal("damage or retained deposit missing")
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
