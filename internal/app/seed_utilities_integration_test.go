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

// TestSeed_DemoUtilitiesCoherent (m9-plan §15.2): the demo has meters with readings, consumptions, plans and the
// tasks the schedule job generated; the utility invariants (INV-90, INV-91, INV-92) hold; a second run adds nothing.
func TestSeed_DemoUtilitiesCoherent(t *testing.T) {
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
		count := func(table string) int {
			n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", res.MarinaID).Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		meters, readings, plans, tasks, consumptions := count("utility_meters"), count("meter_readings"), count("maintenance_plans"),
			count("maintenance_tasks"), count("utility_consumptions")
		t.Logf("meters %d readings %d plans %d tasks %d consumptions %d", meters, readings, plans, tasks, consumptions)
		if meters < 2 || readings < 4 || plans < 2 || tasks < 2 || consumptions == 0 || count("equipment") != 3 {
			t.Fatal("utilities seed incomplete")
		}
		vs, err := store.CheckUtilityInvariants(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 0 {
			t.Fatalf("utility invariant violations: %+v", vs)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("utility_meters") != meters || count("maintenance_tasks") != tasks {
			t.Fatal("the second run added utilities data")
		}
	})
}
