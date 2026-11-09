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

// TestSeed_DemoSensorsCoherent (m12-plan §17.2): the demo has a gateway, the fake source, 40 berth devices, 2
// segment devices and 1 pedestal device with a back-dated history and 6 h of readings; Q40/Q41 hold; a second run
// adds nothing.
func TestSeed_DemoSensorsCoherent(t *testing.T) {
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
		if n := count("sensor_devices", "resource_type = ?", "berth"); n != 40 {
			t.Fatalf("berth devices %d", n)
		}
		if count("sensor_devices", "resource_type = ?", "dynamic_area") != 2 || count("sensor_devices", "resource_type = ?", "pedestal") != 1 {
			t.Fatal("segment or pedestal devices")
		}
		if count("sensor_gateways", "") != 1 || count("sensor_sources", "") != 1 || count("sensor_devices", "gateway_id IS NOT NULL") != 8 ||
			count("sensor_devices", "source_id IS NOT NULL") != 2 {
			t.Fatal("bindings")
		}
		if n := count("sensor_readings", ""); n < 43*30 {
			t.Fatalf("readings %d", n)
		}
		if count("sensor_device_states", "physical = ?", "unknown") > 1 || count("sensor_device_states", "battery_pct = 12") < 1 {
			t.Fatal("states")
		}
		if count("sensor_state_changes", "at < ?", a.Clock.Now().AddDate(0, 0, -4)) < 40 {
			t.Fatal("back-dated history")
		}
		vs, err := store.CheckSensorInvariants(ctx, db.DB, res.MarinaID)
		if err != nil || len(vs) != 0 {
			t.Fatalf("sensor invariants %+v %v", vs, err)
		}
		devices := count("sensor_devices", "")
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("sensor_devices", "") != devices {
			t.Fatal("second seed added devices")
		}
	})
}
