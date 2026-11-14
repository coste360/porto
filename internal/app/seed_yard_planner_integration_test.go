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

// TestSeed_DemoYardPlannerCoherent (m10-plan §16.2): the demo has four employees with calendars, two of them linked
// to the yard demo users, lifting bookings of tomorrow and a request; Q31 holds; a second run adds nothing.
func TestSeed_DemoYardPlannerCoherent(t *testing.T) {
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
			app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: seedTestClock()})
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
		employees, calendars, bookings := count("employees", ""), count("employee_calendars", ""), count("equipment_bookings", "")
		t.Logf("employees %d calendars %d bookings %d", employees, calendars, bookings)
		if employees != 4 || calendars != 28 || count("employees", "user_id IS NOT NULL") != 2 {
			t.Fatal("employees seed incomplete")
		}
		if count("equipment_bookings", "status = ?", store.LiftBookingBooked) < 2 || count("equipment_bookings", "status = ?", store.LiftBookingRequested) != 1 {
			t.Fatal("lifting seed incomplete")
		}
		vs, err := store.CheckLiftingInvariants(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 0 {
			t.Fatalf("lifting invariant violations: %+v", vs)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("employees", "") != employees || count("equipment_bookings", "") != bookings {
			t.Fatal("the second run added planner data")
		}
	})
}
