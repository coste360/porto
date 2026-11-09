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

// TestSeed_DemoYardOrdersCoherent (m10-plan §16.2): the demo has the orders of the five accepted quotes with approved
// and pending hours, materials and external costs, three orders without a quote (one invoiced); Q30, Q32, Q33 hold; a second run adds nothing.
func TestSeed_DemoYardOrdersCoherent(t *testing.T) {
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
		orders := count("work_orders", "")
		t.Logf("orders %d timesheets %d movements %d", orders, count("timesheets", ""), count("stock_movements", ""))
		if orders == 0 {
			t.Skip("no yard catalogue in the demo (catalogue seed not active)")
		}
		if orders != 8 || count("work_orders", "status = ?", store.WorkOrderInvoiced) != 1 || count("work_orders", "quote_id IS NOT NULL") != 5 {
			t.Fatal("orders seed incomplete")
		}
		if count("timesheets", "status = ?", store.TimesheetApproved) == 0 || count("timesheets", "status = ?", store.TimesheetSubmitted) == 0 {
			t.Fatal("timesheets seed incomplete")
		}
		if count("work_order_billings", "kind = ?", store.WOBillingFinal) != 1 || count("work_order_external_costs", "") != 2 {
			t.Fatal("billings or external costs missing")
		}
		vs, err := store.CheckYardInvariants(ctx, db.DB, res.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
		}
		if _, err := a.SeedDemo(ctx, "demo-password-123"); err != nil {
			t.Fatal(err)
		}
		if count("work_orders", "") != orders {
			t.Fatal("second run added orders")
		}
	})
}
