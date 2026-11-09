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

// TestSeed_DemoSensorAlerts (m12-plan §17.2): one reconcile run on the seeded sensors leaves an alert of every kind
// and at least one automatic presence (source sensor, never on a transit); Q40-Q43 hold and a second run adds no
// alert.
func TestSeed_DemoSensorAlerts(t *testing.T) {
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
		var kinds []string
		if err := db.NewSelect().Table("sensor_alerts").ColumnExpr("DISTINCT kind").Where("marina_id = ?", res.MarinaID).
			Scan(ctx, &kinds); err != nil {
			t.Fatal(err)
		}
		t.Logf("seeded alert kinds: %v", kinds)
		got := map[string]bool{}
		for _, k := range kinds {
			got[k] = true
		}
		for _, k := range []string{
			store.SensorAlertUnexpectedOccupied, store.SensorAlertTransitArrival, store.SensorAlertReturnDetected,
			store.SensorAlertReleaseProposal, store.SensorAlertEarlyReturn, store.SensorAlertDeviceSilent, store.SensorAlertBatteryLow,
		} {
			if !got[k] {
				t.Errorf("no %s alert in the demo", k)
			}
		}
		n, err := db.NewSelect().Table("presences").Where("marina_id = ?", res.MarinaID).Where("source = 'sensor'").Count(ctx)
		if err != nil || n < 1 {
			t.Fatalf("sensor presences %d %v", n, err)
		}
		vs, err := store.CheckSensorInvariants(ctx, db.DB, res.MarinaID)
		if err != nil || len(vs) != 0 {
			t.Fatalf("Q40/Q41 %+v %v", vs, err)
		}
		vs, err = store.CheckSensorAlertInvariants(ctx, db.DB, res.MarinaID)
		if err != nil || len(vs) != 0 {
			t.Fatalf("Q42/Q43 %+v %v", vs, err)
		}
		before, _ := db.NewSelect().Table("sensor_alerts").Where("marina_id = ?", res.MarinaID).Count(ctx)
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		after, _ := db.NewSelect().Table("sensor_alerts").Where("marina_id = ?", res.MarinaID).Count(ctx)
		if after != before {
			t.Fatalf("second seed: alerts %d → %d", before, after)
		}
	})
}
