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
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestSeed_DemoHistoryAndOnlineBooking: the demo has a previous year (contracts, transits with presences, charges
// issued and payments received in Y−1, written on the clock of Y−1) and a working public booking page (enabled,
// payable, terms and privacy published, resources sold online); the billing, payment, contract and occupancy
// invariants hold; a second run adds nothing.
func TestSeed_DemoHistoryAndOnlineBooking(t *testing.T) {
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
		now := time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)
		a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(now)})
		if err != nil {
			t.Fatal(err)
		}
		ctx := fixture.Ctx()
		res, err := a.SeedDemo(ctx, "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("history contracts %d transits %d payments %d, sold online %d", res.HistoryContracts, res.HistoryTransits,
			res.HistoryPayments, res.OnlineSellable)
		if res.HistoryContracts < 50 || res.HistoryTransits < 50 || res.HistoryPayments < 100 || res.OnlineSellable == 0 {
			t.Fatal("history or online booking seed incomplete")
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
		y0, y1 := calendar.New(2025, time.January, 1), calendar.New(2026, time.January, 1)
		if count("charges", "issued_on >= ? AND issued_on < ?", y0, y1) == 0 || count("payments", "received_on >= ? AND received_on < ?", y0, y1) == 0 {
			t.Fatal("no charges issued or payments received in the previous year")
		}
		if count("presences", "night < ?", y1) == 0 {
			t.Fatal("no presences in the previous year")
		}
		scope, err := store.ResolveMarinaScope(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		for name, check := range map[string]func() ([]store.Violation, error){
			"billing":   func() ([]store.Violation, error) { return store.CheckBillingInvariants(ctx, db.DB, scope) },
			"payments":  func() ([]store.Violation, error) { return store.CheckPaymentsInvariants(ctx, db.DB, scope) },
			"contracts": func() ([]store.Violation, error) { return store.CheckContractInvariants(ctx, db.DB, res.MarinaID) },
			"occupancy": func() ([]store.Violation, error) {
				return store.CheckOccupancyInvariants(ctx, db.DB, res.MarinaID, calendar.New(2026, time.June, 15))
			},
		} {
			vs, err := check()
			if err != nil {
				t.Fatal(err)
			}
			if len(vs) != 0 {
				t.Fatalf("%s invariant violations: %+v", name, vs)
			}
		}
		cfgB, err := a.Booking.Config(ctx, app.DemoMarinaSlug)
		if err != nil {
			t.Fatal(err)
		}
		if !cfgB.Enabled || !cfgB.Payable || len(cfgB.Texts) != 2 || len(cfgB.Categories) == 0 {
			t.Fatalf("booking config enabled=%v payable=%v texts=%d categories=%d", cfgB.Enabled, cfgB.Payable, len(cfgB.Texts), len(cfgB.Categories))
		}
		contracts, payments := count("contracts", ""), count("payments", "")
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("contracts", "") != contracts || count("payments", "") != payments || count("booking_texts", "") != 2 {
			t.Fatal("the second run added history or booking data")
		}
	})
}
