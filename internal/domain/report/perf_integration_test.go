//go:build integration

package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestReport_PerformanceBudget (m7-plan §11.5): on the demo data, a 366-day period, first page: every report
// ≤ 500 ms (occupancy, efficiency and waste with suggestions ≤ 1 s), the dashboard with the previous year ≤ 2 s,
// the occupancy export of the year ≤ 2 s. Budgets × 3 under -race; PostgreSQL is measured and logged only.
func TestReport_PerformanceBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("performance budget")
	}
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		p := d.principal(auth.RoleOwner)
		ctx := context.Background()
		to := d.today
		from := to.AddDays(-365)
		factor := time.Duration(1)
		if raceEnabled {
			factor = 3
		}
		budget := func(name string, limit time.Duration, fn func() error) {
			t.Helper()
			start := time.Now()
			if err := fn(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			took := time.Since(start)
			t.Logf("%s: %v", name, took)
			if !db.IsPostgres() && took > limit*factor {
				t.Errorf("%s took %v, budget %v", name, took, limit*factor)
			}
		}
		q := report.Params{From: from, To: to}
		slow := map[report.Key]bool{calc.KeyOccupancy: true, calc.KeyEfficiency: true, calc.KeyAllocationWaste: true}
		for _, k := range []report.Key{
			calc.KeyOccupancy, calc.KeyEfficiency, calc.KeyAllocationWaste, calc.KeyLostDemand,
			calc.KeyCustomers, calc.KeyProduction, calc.KeyAccruals, calc.KeyCharges, calc.KeyPayments, calc.KeyRevenueClasses,
			calc.KeyReceivables, calc.KeyPresences, calc.KeyRights, calc.KeyCash, calc.KeyVessels, calc.KeyBerthRevenue,
		} {
			limit := 500 * time.Millisecond
			if slow[k] {
				limit = time.Second
			}
			budget(string(k), limit, func() error { return runQ(ctx, d.svc, p, d.marina, k, q) })
		}
		budget("dashboard", 2*time.Second, func() error {
			_, err := d.svc.Dashboard(ctx, p, d.marina, report.DashboardParams{From: from, To: to})
			return err
		})
		budget("occupancy csv", 2*time.Second, func() error {
			_, err := d.svc.Occupancy(ctx, p, d.marina, report.Params{From: from, To: to, Format: report.FormatCSV})
			return err
		})
	})
}
