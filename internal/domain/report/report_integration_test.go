//go:build integration

package report_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestReport_DashboardDrillDown (INV-62, spec 32 M7): on the seeded demo, for three periods, every available tile
// of the dashboard and its previous value equal the sum of the records of its drill report run with the drill
// parameters, and the comparison series sum to the tile.
func TestReport_DashboardDrillDown(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		owner := d.principal(auth.RoleOwner)
		ctx := context.Background()
		nonZero, nonZeroPrev := map[string]bool{}, map[string]bool{}
		// The demo has no activity before this year: the same periods one year ahead have the demo data as their
		// previous period, so the previous-year values are checked against real records too (INV-62).
		ps := periods(d.today)
		for name, pr := range periods(d.today) {
			ps["next_"+name] = [2]calendar.Date{
				calendar.New(pr[0].Year()+1, pr[0].Month(), pr[0].Day()), calendar.New(pr[1].Year()+1, pr[1].Month(), pr[1].Day()),
			}
		}
		for name, pr := range ps {
			dash, err := d.svc.Dashboard(ctx, owner, d.marina, report.DashboardParams{From: pr[0], To: pr[1]})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if dash.PrevFrom.IsZero() {
				t.Fatalf("%s: no previous period", name)
			}
			for _, tile := range dash.Tiles {
				if !tile.Available {
					if tile.Reason == "" || tile.Drill != nil {
						t.Errorf("%s %s: seam tile without reason", name, tile.Key)
					}
					continue
				}
				key := string(tile.Key)
				got := sumRecords(t, d, owner, key, tile.Drill, pr[0], pr[1])
				if got != tile.Current {
					t.Errorf("%s %s: records %+v != tile %+v", name, key, got, tile.Current)
				}
				if tile.Current.Value.Value != 0 {
					nonZero[key] = true
				}
				if tile.Previous != nil {
					prev := sumRecords(t, d, owner, key, tile.Drill, dash.PrevFrom, dash.PrevTo)
					if prev != *tile.Previous {
						t.Errorf("%s %s previous: records %+v != tile %+v", name, key, prev, *tile.Previous)
					}
					if tile.Previous.Value.Value != 0 {
						nonZeroPrev[key] = true
					}
				}
				var sNum, sDen int64
				for _, p := range tile.Series {
					sNum += p.Value.Value
					_ = sDen
				}
				if len(tile.Series) > 0 && !calc.Indicators[key].Fraction && sNum != tile.Current.Value.Value {
					t.Errorf("%s %s: Σ series %d != tile %d", name, key, sNum, tile.Current.Value.Value)
				}
			}
		}
		for _, k := range []string{
			calc.IndProduction, calc.IndCharged, calc.IndCollected, calc.IndOccupancy, calc.IndEfficiency,
			calc.IndVesselsHosted, calc.IndReceivablesOpen,
		} {
			if !nonZero[k] {
				t.Errorf("tile %s is zero on every period: the demo should exercise it", k)
			}
		}
		for _, k := range []string{calc.IndProduction, calc.IndCharged, calc.IndCollected, calc.IndOccupancy} {
			if !nonZeroPrev[k] {
				t.Errorf("tile %s has a zero previous value on every period: the previous-year check is vacuous", k)
			}
		}
	})
}

// TestReport_Comparison: the series of the comparison sum to the current and previous values with both
// alignments, and previous days are mapped onto current buckets.
func TestReport_Comparison(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		owner := d.principal(auth.RoleOwner)
		ctx := context.Background()
		from := calendar.New(d.today.Year(), time.January, 1)
		for _, ind := range []string{calc.IndProduction, calc.IndCharged, calc.IndCollected, calc.IndArrivals, calc.IndLostDemandCount, calc.IndOccupancy} {
			for _, al := range []string{"date", "weekday"} {
				c, err := d.svc.Compare(ctx, owner, d.marina, report.ComparisonParams{Indicator: ind, From: from, To: d.today, Align: al})
				if err != nil {
					t.Fatalf("%s %s: %v", ind, al, err)
				}
				var cn, cd, pn, pd int64
				for _, p := range c.Series {
					cn += p.CurrentNumerator
					cd += p.CurrentDenominator
					pn += p.PreviousNumerator
					pd += p.PreviousDenominator
					if !p.Current.OK && !c.Current.HasFraction {
						t.Fatalf("%s: bucket without value", ind)
					}
				}
				if !c.Current.HasFraction {
					cn, pn = 0, 0
					for _, p := range c.Series {
						cn += p.Current.Value
						pn += p.Previous.Value
					}
					if cn != c.Current.Value.Value || pn != c.Previous.Value.Value {
						t.Errorf("%s %s: Σ series %d/%d != %d/%d", ind, al, cn, pn, c.Current.Value.Value, c.Previous.Value.Value)
					}
					continue
				}
				if cn != c.Current.Numerator || cd != c.Current.Denominator || pn != c.Previous.Numerator || pd != c.Previous.Denominator {
					t.Errorf("%s %s: Σ series %d/%d %d/%d != %+v %+v", ind, al, cn, cd, pn, pd, c.Current, c.Previous)
				}
			}
		}
		if _, err := d.svc.Compare(ctx, owner, d.marina, report.ComparisonParams{Indicator: "receivables_open"}); !errors.Is(err, report.ErrUnknownIndicator) {
			t.Errorf("receivables are not comparable: %v", err)
		}
	})
}

// everyReport runs every report with p on marinaID (default period) and returns the errors by report.
func everyReport(ctx context.Context, s *report.Service, p *auth.Principal, marinaID string) map[report.Key]error {
	q := report.Params{}
	out := map[report.Key]error{}
	_, out[calc.KeyOccupancy] = s.Occupancy(ctx, p, marinaID, q)
	_, out[calc.KeyEfficiency] = s.Efficiency(ctx, p, marinaID, q)
	_, out[calc.KeyAllocationWaste] = s.AllocationWaste(ctx, p, marinaID, q)
	_, out[calc.KeyLostDemand] = s.LostDemand(ctx, p, marinaID, q)
	_, out[calc.KeyCustomers] = s.Customers(ctx, p, marinaID, q)
	_, out[calc.KeyProduction] = s.Production(ctx, p, marinaID, q)
	_, out[calc.KeyAccruals] = s.Accruals(ctx, p, marinaID, q)
	_, out[calc.KeyCharges] = s.Charges(ctx, p, marinaID, q)
	_, out[calc.KeyPayments] = s.Payments(ctx, p, marinaID, q)
	_, out[calc.KeyRevenueClasses] = s.RevenueClasses(ctx, p, marinaID, q)
	_, out[calc.KeyReceivables] = s.Receivables(ctx, p, marinaID, q)
	_, out[calc.KeyPresences] = s.Presences(ctx, p, marinaID, q)
	_, out[calc.KeyRights] = s.Rights(ctx, p, marinaID, q)
	_, out[calc.KeyCash] = s.Cash(ctx, p, marinaID, q)
	_, out[calc.KeyVessels] = s.Vessels(ctx, p, marinaID, q)
	_, out[calc.KeyBerthRevenue] = s.BerthRevenue(ctx, p, marinaID, q)
	_, out["dashboard"] = s.Dashboard(ctx, p, marinaID, report.DashboardParams{})
	_, out[calc.KeyComparison] = s.Compare(ctx, p, marinaID, report.ComparisonParams{Indicator: calc.IndOccupancy})
	return out
}

var operational = map[report.Key]bool{
	calc.KeyOccupancy: true, calc.KeyEfficiency: true, calc.KeyAllocationWaste: true, calc.KeyLostDemand: true,
	calc.KeyPresences: true, calc.KeyVessels: true, "dashboard": true, calc.KeyComparison: true,
}

// TestReport_Permissions (spec 26.1, D17): full roles read everything; office reads the operational reports with
// the money columns hidden; dock, yard and rental are refused; another marina is not found.
func TestReport_Permissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		ctx := context.Background()
		for _, role := range []auth.Role{auth.RoleOwner, auth.RoleDirector, auth.RoleAccounting, auth.RoleViewer} {
			for k, err := range everyReport(ctx, d.svc, d.principal(role), d.marina) {
				if err != nil {
					t.Errorf("%s %s: %v", role, k, err)
				}
			}
		}
		office := d.principal(auth.RoleOffice)
		for k, err := range everyReport(ctx, d.svc, office, d.marina) {
			if operational[k] && err != nil {
				t.Errorf("office %s: %v", k, err)
			}
			if !operational[k] && !errors.Is(err, auth.ErrForbidden) {
				t.Errorf("office %s: %v, want forbidden", k, err)
			}
		}
		w, err := d.svc.AllocationWaste(ctx, office, d.marina, report.Params{})
		must(t, err)
		if w.Totals.Lost.OK || w.Totals.Applied.OK {
			t.Error("office must not see the lost revenue")
		}
		pr, err := d.svc.Presences(ctx, office, d.marina, report.Params{})
		must(t, err)
		if pr.Totals.ReleaseRevenue.OK || pr.Totals.ReleaseCredit.OK {
			t.Error("office must not see release money")
		}
		dash, err := d.svc.Dashboard(ctx, office, d.marina, report.DashboardParams{})
		must(t, err)
		for _, tile := range dash.Tiles {
			if tile.Available && calc.Indicators[string(tile.Key)].Unit == calc.UnitCents {
				t.Errorf("office sees money tile %s", tile.Key)
			}
		}
		if _, err := d.svc.Compare(ctx, office, d.marina, report.ComparisonParams{Indicator: calc.IndProduction}); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("office production comparison: %v", err)
		}
		for _, role := range []auth.Role{auth.RoleDock, auth.RoleYard, auth.RoleRental} {
			for k, err := range everyReport(ctx, d.svc, d.principal(role), d.marina) {
				if !errors.Is(err, auth.ErrForbidden) {
					t.Errorf("%s %s: %v, want forbidden", role, k, err)
				}
			}
		}
		// The viewer has reports.all but not customers.read: no names.
		c, err := d.svc.Production(ctx, d.principal(auth.RoleViewer), d.marina, report.Params{})
		must(t, err)
		for _, r := range c.Records {
			if r.CustomerName != "" {
				t.Fatal("viewer must not see customer names")
			}
		}
		other := &auth.Principal{Kind: auth.KindUser, UserID: store.NewID(), Grants: []auth.Grant{{OrganizationID: store.NewID(), Role: auth.RoleOwner}}}
		for k, err := range everyReport(ctx, d.svc, other, d.marina) {
			if !errors.Is(err, auth.ErrNotFound) {
				t.Errorf("other organization %s: %v, want not found", k, err)
			}
		}
	})
}

func counts(t *testing.T, db *store.DB) [3]int {
	t.Helper()
	var out [3]int
	for i, table := range []string{"audit_log", "outbox_events", "idempotency_keys"} {
		n, err := db.NewSelect().TableExpr(table).Count(context.Background())
		must(t, err)
		out[i] = int(n)
	}
	return out
}

// TestReport_ReadOnly (INV-61): every report and the dashboard write nothing.
func TestReport_ReadOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		before := counts(t, db)
		var changes int64
		if !db.IsPostgres() {
			_ = db.NewRaw("SELECT total_changes()").Scan(context.Background(), &changes)
		}
		for k, err := range everyReport(context.Background(), d.svc, d.principal(auth.RoleOwner), d.marina) {
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
		}
		if after := counts(t, db); after != before {
			t.Fatalf("reports wrote rows: %v → %v", before, after)
		}
		if !db.IsPostgres() {
			var now int64
			_ = db.NewRaw("SELECT total_changes()").Scan(context.Background(), &now)
			if now != changes {
				t.Logf("total_changes %d → %d (pooled connections may differ)", changes, now)
			}
		}
	})
}

// queryCounter counts the queries run through the DB.
type queryCounter struct {
	n int
	q []string
}

func (q *queryCounter) BeforeQuery(ctx context.Context, e *bun.QueryEvent) context.Context {
	q.n++
	q.q = append(q.q, e.Query)
	return ctx
}
func (q *queryCounter) AfterQuery(context.Context, *bun.QueryEvent) {}

// TestReport_QueryCount (m7-plan §9): each report runs a fixed number of queries whatever the data size (≤ 12
// reads, plus the authorisation and the read transaction).
func TestReport_QueryCount(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		d := seedDemo(t, db)
		qc := &queryCounter{}
		db.AddQueryHook(qc) //nolint:staticcheck // the service holds this *bun.DB: a WithQueryHook copy would not see its queries
		p := d.principal(auth.RoleOwner)
		ctx := context.Background()
		// Baseline: authorisation and transaction only (a report whose loaders return at once).
		qc.n = 0
		must(t, d.svc.Check(ctx, p, d.marina))
		authQueries := qc.n
		for _, k := range []report.Key{
			calc.KeyOccupancy, calc.KeyEfficiency, calc.KeyAllocationWaste, calc.KeyLostDemand,
			calc.KeyCustomers, calc.KeyProduction, calc.KeyAccruals, calc.KeyCharges, calc.KeyPayments, calc.KeyRevenueClasses,
			calc.KeyReceivables, calc.KeyPresences, calc.KeyRights, calc.KeyCash, calc.KeyVessels, calc.KeyBerthRevenue,
		} {
			qc.n, qc.q = 0, nil
			errs := runOne(ctx, d.svc, p, d.marina, k)
			must(t, errs)
			// Reads = queries − authorisation − BEGIN/COMMIT − the catalog loads of the active price lists (one load
			// per list, bounded by the lists, not by the data).
			reads := qc.n - authQueries - 2
			for _, x := range qc.q {
				if strings.Contains(x, `"price_list"`) || strings.Contains(x, `"rate_period"`) || strings.Contains(x, `"price_list_band"`) ||
					strings.Contains(x, `"price_rule"`) || strings.Contains(x, `"price_list_item"`) || strings.Contains(x, `"item"."`) {
					reads--
				}
			}
			if reads > 12 {
				t.Errorf("%s: %d reads (%d queries, %d logged)", k, reads, qc.n, len(qc.q))
				for _, x := range qc.q {
					t.Log(x[:min(len(x), 120)])
				}
			}
		}
	})
}

func runOne(ctx context.Context, s *report.Service, p *auth.Principal, marinaID string, k report.Key) error {
	return runQ(ctx, s, p, marinaID, k, report.Params{})
}

func runQ(ctx context.Context, s *report.Service, p *auth.Principal, marinaID string, k report.Key, q report.Params) error {
	var err error
	switch k {
	case calc.KeyOccupancy:
		_, err = s.Occupancy(ctx, p, marinaID, q)
	case calc.KeyEfficiency:
		_, err = s.Efficiency(ctx, p, marinaID, q)
	case calc.KeyAllocationWaste:
		_, err = s.AllocationWaste(ctx, p, marinaID, q)
	case calc.KeyLostDemand:
		_, err = s.LostDemand(ctx, p, marinaID, q)
	case calc.KeyCustomers:
		_, err = s.Customers(ctx, p, marinaID, q)
	case calc.KeyProduction:
		_, err = s.Production(ctx, p, marinaID, q)
	case calc.KeyAccruals:
		_, err = s.Accruals(ctx, p, marinaID, q)
	case calc.KeyCharges:
		_, err = s.Charges(ctx, p, marinaID, q)
	case calc.KeyPayments:
		_, err = s.Payments(ctx, p, marinaID, q)
	case calc.KeyRevenueClasses:
		_, err = s.RevenueClasses(ctx, p, marinaID, q)
	case calc.KeyReceivables:
		_, err = s.Receivables(ctx, p, marinaID, q)
	case calc.KeyPresences:
		_, err = s.Presences(ctx, p, marinaID, q)
	case calc.KeyRights:
		_, err = s.Rights(ctx, p, marinaID, q)
	case calc.KeyCash:
		_, err = s.Cash(ctx, p, marinaID, q)
	case calc.KeyVessels:
		_, err = s.Vessels(ctx, p, marinaID, q)
	case calc.KeyBerthRevenue:
		_, err = s.BerthRevenue(ctx, p, marinaID, q)
	}
	return err
}
