//go:build integration

package admin_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/xlsx"
)

// addRole creates a member with role and returns its logged-in client.
func addRole(t *testing.T, ps *pricingSetup, role string) *harborClient {
	t.Helper()
	var mem struct {
		UserID string `json:"user_id"`
	}
	ps.owner.expect(t, 201, "", "POST", ps.M+"/members", map[string]any{"email": role + "@porto.test", "name": role, "role": role}, &mem)
	if err := ps.s.app.Auth.SetPassword(context.Background(), mem.UserID, harborPW); err != nil {
		t.Fatal(err)
	}
	return ps.s.login(t, role+"@porto.test")
}

func TestDashboardHandlers_Authorise(t *testing.T) {
	ps := newPricingSetup(t)
	for _, p := range []string{"/dashboard", "/dashboard?from=2026-05-01&to=2026-01-01", "/reports/comparison?indicator=production", "/reports/comparison"} {
		ps.other.expect(t, 404, "not_found", "GET", ps.M+p, nil, nil)
		ps.dock.expect(t, 403, "forbidden", "GET", ps.M+p, nil, nil)
		ps.yard.expect(t, 403, "forbidden", "GET", ps.M+p, nil, nil)
	}
}

func TestDashboardHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	M := ps.M

	var d api.Dashboard
	ps.owner.expect(t, 200, "", "GET", M+"/dashboard", nil, &d)
	if d.Compare != "previous_year" || d.PreviousPeriod == nil || d.Align != "date" || d.Period.From > d.Period.To {
		t.Fatalf("dashboard %+v", d)
	}
	keys := map[api.TileKey]api.Tile{}
	for _, tl := range d.Tiles {
		keys[tl.Key] = tl
	}
	for _, k := range []api.TileKey{"production", "charged", "collected", "occupancy", "vessels_hosted", "renewal_rate"} {
		tl, ok := keys[k]
		if !ok || !tl.Available || tl.Drill == nil {
			t.Fatalf("tile %s missing or without drill: %+v", k, tl)
		}
	}
	for _, k := range []api.TileKey{"yard_open_orders", "rental_revenue"} {
		if tl := keys[k]; tl.Available || tl.Reason == nil || tl.Drill != nil {
			t.Fatalf("seam tile %s %+v", k, tl)
		}
	}
	if tl := keys["receivables_open"]; tl.Previous != nil {
		t.Fatalf("receivables have no previous value: %+v", tl)
	}

	// No comparison, explicit period, weekday alignment (decode into a fresh value: omitted fields stay zero).
	d = api.Dashboard{}
	ps.owner.expect(t, 200, "", "GET", M+"/dashboard?compare=none&from=2026-01-01&to=2026-01-31", nil, &d)
	if d.PreviousPeriod != nil || d.Period.From != "2026-01-01" || d.Granularity != "day" {
		t.Fatalf("dashboard none %+v", d)
	}
	for _, tl := range d.Tiles {
		if tl.Previous != nil {
			t.Fatalf("compare=none: tile %s has a previous value", tl.Key)
		}
	}
	d = api.Dashboard{}
	ps.owner.expect(t, 200, "", "GET", M+"/dashboard?align=weekday&from=2026-03-02&to=2026-03-08", nil, &d)
	if d.PreviousPeriod == nil || d.PreviousPeriod.From != "2025-03-03" {
		t.Fatalf("weekday previous %+v", d.PreviousPeriod)
	}

	// Invalid input: 422 after authorisation.
	ps.owner.expect(t, 422, "invalid_period", "GET", M+"/dashboard?from=2026-05-01&to=2026-01-01", nil, nil)
	ps.owner.expect(t, 422, "invalid_period", "GET", M+"/dashboard?from=2024-01-01&to=2026-01-01", nil, nil)
	ps.owner.expect(t, 422, "invalid_report_filter", "GET", M+"/dashboard?align=sideways", nil, nil)

	// Office (reports.operational): operational tiles only, no money.
	office := addRole(t, ps, "office")
	d = api.Dashboard{}
	office.expect(t, 200, "", "GET", M+"/dashboard", nil, &d)
	for _, tl := range d.Tiles {
		switch tl.Key {
		case "production", "charged", "collected", "receivables_open", "receivables_overdue", "berth_revenue_per_m", "transit_avg_night", "new_customers", "renewal_rate":
			t.Fatalf("office sees money tile %s", tl.Key)
		}
	}
	if len(d.Tiles) == 0 {
		t.Fatal("office sees no tiles")
	}
}

func TestReportComparisonHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	M := ps.M

	ps.owner.expect(t, 422, "unknown_indicator", "GET", M+"/reports/comparison", nil, nil)
	ps.owner.expect(t, 422, "unknown_indicator", "GET", M+"/reports/comparison?indicator=receivables_open", nil, nil)
	ps.owner.expect(t, 422, "invalid_period", "GET", M+"/reports/comparison?indicator=production&from=2026-02-01&to=2026-01-01", nil, nil)

	var c api.ComparisonResult
	q := "/reports/comparison?indicator=production&from=2026-01-01&to=2026-03-31&years_back=2&align=weekday&granularity=month"
	ps.owner.expect(t, 200, "", "GET", M+q, nil, &c)
	if c.Indicator != "production" || c.Unit != "cents" || c.YearsBack != 2 || c.Align != "weekday" || len(c.Series) != 3 ||
		c.Series[0].Bucket != "2026-01-01" {
		t.Fatalf("comparison %+v", c)
	}
	var sum int64
	for _, s := range c.Series {
		if s.Current != nil {
			sum += *s.Current
		}
	}
	if c.Current.Value == nil || sum != *c.Current.Value {
		t.Fatalf("series sum %d != current %v", sum, c.Current.Value)
	}

	r := ps.owner.expect(t, 200, "", "GET", M+q+"&format=csv", nil, nil)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") ||
		!strings.Contains(r.header.Get("Content-Disposition"), `filename="porto-comparison-2026-01-01-2026-03-31.csv"`) ||
		!bytes.HasPrefix(r.body, []byte("\ufeffPeriodo;Valore;")) || bytes.Count(r.body, []byte("\r\n")) != 5 {
		t.Fatalf("csv %v %q", r.header, r.body)
	}
	r = ps.owner.expect(t, 200, "", "GET", M+q+"&format=xlsx&lang=en", nil, nil)
	rows, err := xlsx.ReadFirstSheet(bytes.NewReader(r.body), int64(len(r.body)), xlsx.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[0][0] != "Bucket" || rows[4][0] != "Total" {
		t.Fatalf("xlsx %q", rows)
	}

	// Office may compare operational indicators only.
	office := addRole(t, ps, "office")
	office.expect(t, 200, "", "GET", M+"/reports/comparison?indicator=occupancy", nil, &c)
	office.expect(t, 403, "forbidden", "GET", M+"/reports/comparison?indicator=production", nil, nil)
}
