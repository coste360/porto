package report

import (
	"errors"
	"net/url"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
)

func TestParams_ParseAndValues(t *testing.T) {
	cases := []struct {
		key   Key
		query string
	}{
		{calc.KeyOccupancy, "from=2026-01-01&to=2026-03-31&variant=contractual&family=linear_area&group_by=week&map_kind=water&group_id=g1&page=2&page_size=50"},
		// Efficiency has the same filters as occupancy, resource_type included (m7-plan §3.2).
		{calc.KeyEfficiency, "family=surface_area&resource_type=dynamic_area&group_by=structure&structure_id=s1"},
		{calc.KeyAllocationWaste, "threshold_bp=3500&suggestions=false&group_by=kind&category=B"},
		{calc.KeyLostDemand, "group_by=length_band&channel=online&purpose=transit&format=csv&lang=en"},
		{calc.KeyCustomers, "new=true&section=renewals&group_by=value_band"},
		{calc.KeyCharges, "basis=competence&origin=installment&origin=transit&payment_status=open&fiscal_mode=receipt&item_id=i1"},
		{calc.KeyReceivables, "as_of=2026-05-31&overdue=true&origin=manual&section=future&group_by=bucket"},
		{calc.KeyPayments, "method=cash&customer_id=c1&section=payouts"},
		{calc.KeyProduction, "kind=transit"},
		// M9 (m9-plan §11): kind and status are enumerated per report.
		{calc.KeyStorage, "group_by=area&map_kind=shed&area_id=a1&exits_blocked_only=true&from=2026-03-01&to=2026-03-30"},
		{calc.KeyMaintenance, "group_by=intervention&status=in_progress&element_type=pedestal&overdue_only=true"},
		{calc.KeyUtilities, "group_by=meter&kind=water&status=unassigned&meter_id=m1&contract_id=c1"},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		p, err := ParseParams(c.key, q)
		if err != nil {
			t.Fatalf("%s %s: %v", c.key, c.query, err)
		}
		if p.Values().Encode() != q.Encode() {
			t.Errorf("%s: Values %s != %s", c.key, p.Values().Encode(), q.Encode())
		}
		back, err := ParseParams(c.key, p.Values())
		if err != nil || !reflect.DeepEqual(back, p) {
			t.Errorf("%s: round trip %+v != %+v (%v)", c.key, back, p, err)
		}
	}
}

func TestParams_Errors(t *testing.T) {
	cases := []struct {
		key   Key
		query string
		want  error
		param string
	}{
		{calc.KeyOccupancy, "from=2026-13-01", ErrInvalidPeriod, ""},
		{calc.KeyOccupancy, "from=2026-03-01&to=2026-02-01", ErrInvalidPeriod, ""},
		{calc.KeyOccupancy, "from=2025-01-01&to=2026-02-05", ErrInvalidPeriod, ""},
		{calc.KeyOccupancy, "group_by=customer", ErrInvalidFilter, "group_by"},
		{calc.KeyOccupancy, "variant=x", ErrInvalidFilter, "variant"},
		{calc.KeyOccupancy, "page_size=1001", ErrInvalidFilter, "page_size"},
		{calc.KeyCustomers, "section=open", ErrInvalidFilter, "section"},
		{calc.KeyCharges, "origin=bogus", ErrInvalidFilter, "origin"},
		{calc.KeyReceivables, "origin=manual&origin=transit", ErrInvalidFilter, "origin"},
		{calc.KeyAllocationWaste, "threshold_bp=10001", ErrInvalidFilter, "threshold_bp"},
		{calc.KeyPayments, "method=gold", ErrInvalidFilter, "method"},
		{calc.KeyPayments, "customer_id=0123456789012345678901234567890123456789", ErrInvalidFilter, "customer_id"},
		{calc.KeyStorage, "group_by=resource", ErrInvalidFilter, "group_by"},
		{calc.KeyStorage, "exits_blocked_only=maybe", ErrInvalidFilter, "exits_blocked_only"},
		{calc.KeyMaintenance, "status=pending", ErrInvalidFilter, "status"},
		{calc.KeyMaintenance, "element_type=boat", ErrInvalidFilter, "element_type"},
		{calc.KeyUtilities, "kind=mooring", ErrInvalidFilter, "kind"},
		{calc.KeyUtilities, "status=open", ErrInvalidFilter, "status"},
		{calc.KeyProduction, "kind=power", ErrInvalidFilter, "kind"},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		_, err := ParseParams(c.key, q)
		if !errors.Is(err, c.want) {
			t.Errorf("%s %s: %v, want %v", c.key, c.query, err, c.want)
			continue
		}
		var fe *FilterError
		if c.param != "" && (!errors.As(err, &fe) || fe.Param != c.param) {
			t.Errorf("%s %s: param %v", c.key, c.query, err)
		}
	}
	// 400 days are accepted.
	q, _ := url.ParseQuery("from=2026-01-01&to=2027-02-04")
	if _, err := ParseParams(calc.KeyOccupancy, q); err != nil {
		t.Errorf("400 days: %v", err)
	}
	// A parameter the report does not use is kept and listed as ignored.
	q, _ = url.ParseQuery("basis=issued&map_id=m1")
	p, err := ParseParams(calc.KeyPayments, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, ignored := p.filters(); !reflect.DeepEqual(ignored, []string{"basis", "map_id"}) {
		t.Errorf("ignored %v", ignored)
	}
}

// TestDashboard_DrillParamsRoundTrip: the drill parameters of every tile parse back to the very Params the
// dashboard runs, so following a drill link reproduces the tile's report request (INV-62).
func TestDashboard_DrillParamsRoundTrip(t *testing.T) {
	dims := Dims{StructureID: "s1"}
	for _, ts := range append(tileSpecs, comparisonSpecs...) {
		if ts.seam != "" {
			continue
		}
		q := ParamsOf(ts, dims)
		dr := drillOf(q)
		if dr.Report != ts.report {
			t.Fatalf("%s: report %s", ts.key, dr.Report)
		}
		v := url.Values{}
		for k, x := range dr.Params {
			if k == "origin" {
				continue
			}
			v.Set(k, x)
		}
		back, err := ParseParams(ts.report, v)
		if err != nil {
			t.Fatalf("%s: %v", ts.key, err)
		}
		if !reflect.DeepEqual(back, q) {
			t.Errorf("%s: %+v != %+v", ts.key, back, q)
		}
		if _, ignored := back.filters(); len(ignored) != 0 {
			t.Errorf("%s: drill carries ignored filters %v", ts.key, ignored)
		}
	}
	want := map[string]map[string]string{
		calc.IndOccupancy:       {"variant": "physical", "family": "berth", "map_kind": "water", "structure_id": "s1"},
		calc.IndCharged:         {"basis": "issued"},
		calc.IndReceivablesOver: {"overdue": "true"},
		calc.IndTransitAvgNight: {"kind": "transit"},
		calc.IndNewCustomers:    {"new": "true"},
		calc.IndRenewalRate:     {"section": "renewals"},
	}
	for key, params := range want {
		ts, _ := specOf(key)
		if got := drillOf(ParamsOf(ts, dims)).Params; !reflect.DeepEqual(got, params) {
			t.Errorf("%s drill %v, want %v", key, got, params)
		}
	}
}

func TestComparison_Params(t *testing.T) {
	q, _ := url.ParseQuery("indicator=occupancy&years_back=3&align=weekday&granularity=month")
	p, err := ParseComparisonParams(q)
	if err != nil || p.YearsBack != 3 || p.Align != "weekday" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, bad := range []string{"", "indicator=receivables_open", "indicator=vessels_hosted", "indicator=nope"} {
		q, _ := url.ParseQuery(bad)
		if _, err := ParseComparisonParams(q); !errors.Is(err, ErrUnknownIndicator) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	q, _ = url.ParseQuery("indicator=occupancy&years_back=6")
	if _, err := ParseComparisonParams(q); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("years_back: %v", err)
	}
	q, _ = url.ParseQuery("compare=none&align=weekday&from=2026-01-01")
	if d, err := ParseDashboardParams(q); err != nil || d.Compare != "none" {
		t.Errorf("dashboard %+v %v", d, err)
	}
}

// TestParams_RoundTripRapid (property): any valid set of parameters survives Values → ParseParams unchanged.
func TestParams_RoundTripRapid(t *testing.T) {
	keys := []Key{calc.KeyOccupancy, calc.KeyCharges, calc.KeyReceivables, calc.KeyCustomers, calc.KeyAllocationWaste, calc.KeyPayments}
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.SampledFrom(keys).Draw(t, "key")
		sp := specs[key]
		p := Params{Report: key}
		if rapid.Bool().Draw(t, "period") {
			p.From = calendar.MustParse("2026-01-01").AddDays(rapid.IntRange(0, 300).Draw(t, "from"))
			p.To = p.From.AddDays(rapid.IntRange(0, MaxPeriodDays-1).Draw(t, "len"))
		}
		if len(sp.groupBy) > 0 && rapid.Bool().Draw(t, "g") {
			p.GroupBy = rapid.SampledFrom(sp.groupBy).Draw(t, "group_by")
		}
		if len(sp.sections) > 0 && rapid.Bool().Draw(t, "s") {
			p.Section = rapid.SampledFrom(sp.sections).Draw(t, "section")
		}
		p.Dims.MapKind = rapid.SampledFrom(append([]string{""}, mapKinds...)).Draw(t, "map_kind")
		p.Page = rapid.IntRange(0, 50).Draw(t, "page")
		p.PageSize = rapid.IntRange(0, MaxPageSize).Draw(t, "page_size")
		p.NewOnly = rapid.Bool().Draw(t, "new")
		p.OverdueOnly = rapid.Bool().Draw(t, "overdue")
		if key == calc.KeyCharges {
			p.Origins = rapid.SliceOfNDistinct(rapid.SampledFrom(chargeOrigins), 0, 3, rapid.ID[string]).Draw(t, "origins")
			if len(p.Origins) == 0 {
				p.Origins = nil
			}
		} else {
			p.Origin = rapid.SampledFrom(append([]string{""}, chargeOrigins...)).Draw(t, "origin")
		}
		if rapid.Bool().Draw(t, "th") {
			v := rapid.Int64Range(0, 10_000).Draw(t, "threshold")
			p.ThresholdBP = &v
		}
		back, err := ParseParams(key, p.Values())
		if err != nil {
			t.Fatalf("%+v: %v", p, err)
		}
		if !reflect.DeepEqual(back, p) {
			t.Fatalf("round trip %+v != %+v", back, p)
		}
	})
}
