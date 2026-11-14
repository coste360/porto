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

// TestSeed_DemoProfilePublished (m12-plan §17.2 PUBLIC seed): the demo marina has a published profile whose public
// page shows the transit price matrix of the demo list (publish_matrix), the simplified map and the aggregate
// occupancy.
func TestSeed_DemoProfilePublished(t *testing.T) {
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
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Profiles.PublicProfile(ctx, app.DemoMarinaSlug, "it"); err != nil {
			t.Fatalf("public profile: %v", err)
		}
		prices, _, err := a.Profiles.PublicPrices(ctx, app.DemoMarinaSlug, "en")
		if err != nil {
			t.Fatalf("public prices: %v", err)
		}
		if len(prices.Rows) == 0 || len(prices.Columns) == 0 || len(prices.Cells) == 0 {
			t.Fatalf("empty demo price matrix: %d rows, %d columns, %d cells", len(prices.Rows), len(prices.Columns), len(prices.Cells))
		}
		if _, _, err := a.Profiles.PublicMap(ctx, app.DemoMarinaSlug); err != nil {
			t.Fatalf("public map: %v", err)
		}
		occ, _, err := a.Profiles.PublicOccupancy(ctx, app.DemoMarinaSlug)
		if err != nil {
			t.Fatalf("public occupancy: %v", err)
		}
		if occ == nil {
			t.Fatal("no aggregate occupancy")
		}
	})
}
