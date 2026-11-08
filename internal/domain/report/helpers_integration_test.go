//go:build integration

package report_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

// demoNow is the fixed instant of the report tests: late summer, with occupancy, transits and a billing history.
var demoNow = time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)

type demo struct {
	app    *app.App
	db     *store.DB
	marina string
	org    string
	svc    *report.Service
	today  calendar.Date
}

// seedDemo builds the app on db with a fake clock at demoNow and seeds the demo marina.
func seedDemo(t *testing.T, db *store.DB) *demo {
	t.Helper()
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
		app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(demoNow)})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.SeedDemo(fixture.Ctx(), "demo-password-123")
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := calendar.LoadLocation("Europe/Rome")
	return &demo{app: a, db: db, marina: res.MarinaID, org: res.OrganizationID, svc: a.Reports, today: calendar.In(demoNow, loc)}
}

// principal is a user with role on the demo marina (marina-level grant).
func (d *demo) principal(role auth.Role) *auth.Principal {
	return &auth.Principal{Kind: auth.KindUser, UserID: store.NewID(), Grants: []auth.Grant{{OrganizationID: d.org, MarinaID: d.marina, Role: role}}}
}

// params parses a drill (report + parameters) with a period, as the report handler does.
func params(t *testing.T, key report.Key, drill map[string]string, from, to calendar.Date, format report.Format) report.Params {
	t.Helper()
	v := url.Values{}
	for k, x := range drill {
		v.Set(k, x)
	}
	v.Set("from", from.String())
	v.Set("to", to.String())
	if format != "" {
		v.Set("format", string(format))
	}
	q, err := report.ParseParams(key, v)
	if err != nil {
		t.Fatalf("parse %s %v: %v", key, v, err)
	}
	return q
}

// sumRecords runs the drill report of a tile on a period (every record, as an export) and returns the tile value
// recomputed from the records: Σ value, or Σ numerator / Σ denominator.
func sumRecords(t *testing.T, d *demo, p *auth.Principal, key string, drill *calc.Drill, from, to calendar.Date) calc.TileValue {
	t.Helper()
	ctx := context.Background()
	q := params(t, drill.Report, drill.Params, from, to, report.FormatCSV)
	ind := calc.Indicators[key]
	frac := func(num, den int64) calc.TileValue { return ind.Value(calc.Fact{Num: num, Den: den}) }
	switch drill.Report {
	case calc.KeyProduction:
		r, err := d.svc.Production(ctx, p, d.marina, q)
		must(t, err)
		var comp, nights int64
		for _, x := range r.Records {
			if key == calc.IndTransitAvgNight {
				if x.Kind == calc.ContractTransit {
					comp += int64(x.Competence)
					nights += x.NightsInPeriod
				}
				continue
			}
			comp += int64(x.Competence)
		}
		if key == calc.IndTransitAvgNight {
			return frac(comp, nights)
		}
		return frac(comp, 0)
	case calc.KeyCharges:
		r, err := d.svc.Charges(ctx, p, d.marina, q)
		must(t, err)
		var s int64
		for _, x := range r.Records {
			s += int64(x.Gross)
		}
		return frac(s, 0)
	case calc.KeyPayments:
		r, err := d.svc.Payments(ctx, p, d.marina, q)
		must(t, err)
		var s int64
		for _, x := range r.Records {
			s += int64(x.Amount)
		}
		return frac(s, 0)
	case calc.KeyReceivables:
		r, err := d.svc.Receivables(ctx, p, d.marina, q)
		must(t, err)
		var s int64
		for _, x := range r.Records {
			if key == calc.IndReceivablesOver && !x.Overdue {
				continue
			}
			s += int64(x.Residual)
		}
		return frac(s, 0)
	case calc.KeyOccupancy:
		r, err := d.svc.Occupancy(ctx, p, d.marina, q)
		must(t, err)
		var n, a int64
		for _, x := range r.Records {
			n += x.OccupiedUnits
			a += x.AvailableUnits
		}
		return frac(n, a)
	case calc.KeyEfficiency:
		r, err := d.svc.Efficiency(ctx, p, d.marina, q)
		must(t, err)
		var v, a int64
		for _, x := range r.Records {
			v += x.VesselUnits
			a += x.ResourceUnits
		}
		return frac(v, a)
	case calc.KeyBerthRevenue:
		r, err := d.svc.BerthRevenue(ctx, p, d.marina, q)
		must(t, err)
		var v, l int64
		for _, x := range r.Records {
			v += int64(x.Revenue)
			l += int64(x.Length)
		}
		return frac(v, l)
	case calc.KeyCustomers:
		r, err := d.svc.Customers(ctx, p, d.marina, q)
		must(t, err)
		var n, den int64
		for _, x := range r.Records {
			if key == calc.IndRenewalRate {
				n += x.Renewed
				den++
				continue
			}
			n += x.New
		}
		return frac(n, den)
	case calc.KeyVessels:
		r, err := d.svc.Vessels(ctx, p, d.marina, q)
		must(t, err)
		var loa int64
		for _, x := range r.Records {
			loa += int64(x.LOA)
		}
		if key == calc.IndVesselsAvgLOA {
			return frac(loa, int64(len(r.Records)))
		}
		return frac(int64(len(r.Records)), 0)
	}
	t.Fatalf("no drill for %s", drill.Report)
	return calc.TileValue{}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// periods of the acceptance (m7-plan §11.4): month to date, year to date, last twelve months.
func periods(today calendar.Date) map[string][2]calendar.Date {
	return map[string][2]calendar.Date{
		"mtd":      {calendar.New(today.Year(), today.Month(), 1), today},
		"ytd":      {calendar.New(today.Year(), time.January, 1), today},
		"last_12m": {today.AddDays(-364), today},
	}
}
