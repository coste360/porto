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
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestSeed_DemoOccupancyCoherent seeds the demo twice: the first run creates contracts, transits, quotes and
// presences that satisfy every invariant query; the second run is a no-op (m3-plan §10).
func TestSeed_DemoOccupancyCoherent(t *testing.T) {
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
		t.Logf("contracts %d transits %d quotes %d inquiries %d allocations %d rows %d/%d/%d releases %d presences %d",
			res.Contracts, res.Transits, res.Quotes, res.Inquiries, res.Allocations, res.BerthRows, res.CellRows, res.LandRows,
			res.Releases, res.Presences)
		if res.Contracts < 120 || res.Transits < 20 || res.Quotes < 6 || res.Inquiries != 12 || res.BerthRows < 120*300 || res.LandRows == 0 {
			t.Fatalf("seed counts %+v", res)
		}
		loc, _ := calendar.LoadLocation("Europe/Rome")
		today := calendar.In(time.Now(), loc)
		vs, err := store.CheckOccupancyInvariants(ctx, db.DB, res.MarinaID, today)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 0 {
			t.Fatalf("invariant violations: %+v", vs)
		}
		again, err := a.SeedDemo(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if again.Contracts != 0 || again.Transits != 0 || again.Quotes != 0 {
			t.Fatalf("second run seeded %+v", again)
		}
	})
}
