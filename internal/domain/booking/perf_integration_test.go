//go:build integration

package booking_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestBooking_AvailabilityBudget (m8-plan §3.2): on the demo seed with every berth sold online, the public
// availability search answers with p95 < 300 ms (SQLite; × 3 under -race; PostgreSQL is measured and logged only).
// Skipped with -short.
func TestBooking_AvailabilityBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("performance budget")
	}
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		now := time.Date(2026, time.May, 4, 8, 0, 0, 0, time.UTC)
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
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(now)})
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.SeedDemo(fixture.Ctx(), "demo-password-123")
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if _, err := db.NewUpdate().Table("marinas").Set("booking_enabled = ?", true).Where("id = ?", res.MarinaID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		n, err := db.NewUpdate().Table("berths").Set("online_sellable = ?", true).Where("marina_id = ?", res.MarinaID).Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if rows, _ := n.RowsAffected(); rows < 50 {
			t.Fatalf("demo harbour too small for a budget: %d berths", rows)
		}
		vessels := []booking.Vessel{
			{LOACm: 800, BeamCm: 290, DraftCm: 150, HullType: "monohull_sail"},
			{LOACm: 1200, BeamCm: 400, DraftCm: 200, HullType: "monohull_sail"},
			{LOACm: 1600, BeamCm: 480, DraftCm: 180, HullType: "motor"},
		}
		start := calendar.New(2026, time.June, 1)
		search := func(i int) (*booking.Availability, time.Duration) {
			in := booking.AvailabilityInput{
				Arrival: start.AddDays(i * 3 % 90), Vessel: vessels[i%len(vessels)], ClientKey: "budget",
			}
			in.Departure = in.Arrival.AddDays(1 + i%5)
			t0 := time.Now()
			av, err := a.Booking.Availability(ctx, app.DemoMarinaSlug, in)
			if err != nil {
				t.Fatalf("availability %d: %v", i, err)
			}
			return av, time.Since(t0)
		}
		search(0) // warm up caches and statements
		const runs = 60
		took := make([]time.Duration, 0, runs)
		available := 0
		for i := range runs {
			av, d := search(i)
			took = append(took, d)
			if len(av.Categories) == 0 || av.Reason != "" {
				t.Fatalf("search %d: %+v", i, av)
			}
			for _, c := range av.Categories {
				if c.Available {
					available++
					break
				}
			}
		}
		if available == 0 {
			t.Fatal("nothing ever available: the budget measured nothing")
		}
		slices.Sort(took)
		p95 := took[len(took)*95/100-1]
		limit := 300 * time.Millisecond
		if raceEnabled {
			limit *= 3
		}
		t.Logf("availability p50 %v p95 %v max %v (%d runs, %d with availability)", took[len(took)/2], p95, took[len(took)-1], runs, available)
		if !db.IsPostgres() && p95 > limit {
			t.Errorf("availability p95 %v, budget %v", p95, limit)
		}
	})
}
