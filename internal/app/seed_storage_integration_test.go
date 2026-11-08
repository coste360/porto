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

// TestSeed_DemoStorageCoherent (m9-plan §15.2): the demo has storage contracts in the shed and the yard and one
// draft scenario; the storage and occupancy invariants hold; a second run adds nothing.
func TestSeed_DemoStorageCoherent(t *testing.T) {
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
		count := func(table, where string) int {
			n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", res.MarinaID).Where(where).Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		contracts, scenarios := count("contracts", "kind = 'storage'"), count("storage_scenarios", "status = 'draft'")
		t.Logf("storage contracts %d scenarios %d", contracts, scenarios)
		if contracts < 8 || scenarios != 1 || count("storage_contract_details", "1 = 1") != contracts {
			t.Fatal("storage seed incomplete")
		}
		vs, err := store.CheckStorageInvariants(ctx, db.DB, res.MarinaID)
		if err != nil || len(vs) > 0 {
			t.Fatalf("storage invariants: %v %+v", err, vs)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("contracts", "kind = 'storage'") != contracts {
			t.Fatal("second run added storage contracts")
		}
	})
}
