//go:build integration

package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"testing"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestSeed_DemoM4Coherent (m4-plan §10): the M4 demo data satisfies the occupancy invariants and the contract
// invariant queries (INV-31 plan sums, INV-32 settlements, INV-33 open proposals, INV-34, INV-35, INV-40 holder
// shares); the second run adds no M4 data.
func TestSeed_DemoM4Coherent(t *testing.T) {
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
		t.Logf("installments %d campaigns %d proposals %d addenda %d waitlist %d rights %d holder installments %d",
			res.Installments, res.RenewalCampaigns, res.RenewalProposals, res.Addenda, res.WaitlistEntries, res.Rights, res.HolderInstallments)
		if res.Installments == 0 || res.Addenda != 3 || res.WaitlistEntries != 6 {
			t.Fatalf("M4 seed counts %+v", res)
		}
		vs, err := store.CheckContractInvariants(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 0 {
			t.Fatalf("contract invariant violations: %+v", vs)
		}
		loc, _ := calendar.LoadLocation("Europe/Rome")
		if vs, err = store.CheckOccupancyInvariants(ctx, db.DB, res.MarinaID, calendar.In(seedTestNow, loc)); err != nil {
			t.Fatal(err)
		} else if len(vs) != 0 {
			t.Fatalf("occupancy invariant violations: %+v", vs)
		}
		again, err := a.SeedDemo(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if again.Installments != 0 || again.RenewalCampaigns != 0 || again.Addenda != 0 || again.WaitlistEntries != 0 ||
			again.Rights != 0 || again.Templates != 0 {
			t.Fatalf("second run seeded M4 data %+v", again)
		}
	})
}
