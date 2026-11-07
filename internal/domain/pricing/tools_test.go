package pricing

import (
	"bytes"
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

var d = calendar.MustParse

// checkMatrixEqualsQuote verifies INV-23 on one matrix: every cell is Quote(cell.Probe).
func checkMatrixEqualsQuote(t interface {
	Fatalf(string, ...any)
}, cat Catalog, m MatrixResult,
) {
	if len(m.Cells) != len(m.Rows)*len(m.Columns) {
		t.Fatalf("cells %d != rows %d x columns %d", len(m.Cells), len(m.Rows), len(m.Columns))
	}
	for _, c := range m.Cells {
		bd, err := Quote(c.Probe, cat)
		if got := ErrorCode(err); got != c.Error {
			t.Fatalf("cell %d,%d: error %q, quote %q", c.Row, c.Col, c.Error, got)
		}
		if err != nil {
			if c.Breakdown != nil {
				t.Fatalf("cell %d,%d has a breakdown and an error", c.Row, c.Col)
			}
			continue
		}
		if a, b := mustJSON(t, bd), mustJSON(t, c.Breakdown); !bytes.Equal(a, b) {
			t.Fatalf("cell %d,%d differs from Quote(probe):\n%s\n%s", c.Row, c.Col, a, b)
		}
	}
}

// INV-23: the matrix is computed by the engine on its probes, for random catalogs and for every corpus fixture.
func TestMatrix_EqualsQuote(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		for _, ct := range cat.PriceList.ContractTypes {
			spec := MatrixSpec{
				ContractType: ct, QuotedOn: d("2026-12-01"),
				Channel:          rapid.SampledFrom(append([]Channel{""}, Channels()...)).Draw(t, "ch"),
				CustomerCategory: rapid.SampledFrom(append([]string{""}, genCustCats...)).Draw(t, "cc"),
			}
			if rapid.Bool().Draw(t, "rows_categories") {
				spec.Rows = RowsCategories
				spec.Categories = []CoverageCategory{{Code: "A", LOA: 1200, Beam: 400, BerthLength: 1250, BerthWidth: 420}, {Code: "B", LOA: 900}}
			}
			m, err := Matrix(cat, spec)
			if err != nil {
				t.Fatalf("matrix: %v", err)
			}
			checkMatrixEqualsQuote(t, cat, m)
		}
	})
	for _, name := range fixtureNames(t) {
		cat := buildCatalog(t, corpusCase{Catalog: name})
		for _, ct := range cat.PriceList.ContractTypes {
			m, err := Matrix(cat, MatrixSpec{ContractType: ct, QuotedOn: d("2026-12-01")})
			if err != nil {
				t.Fatalf("%s %s: %v", name, ct, err)
			}
			checkMatrixEqualsQuote(t, cat, m)
		}
	}
}

func TestMatrix_Scenario(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "bands-basic"})
	cat.PriceList.Periods = []RatePeriod{
		{Code: "LOW", Kind: PeriodSeason, Ranges: []DateRange{{From: d("2027-09-01"), To: d("2027-12-31")}, {From: d("2026-01-01"), To: d("2027-06-30")}}},
		{Code: "HIGH", Kind: PeriodSeason, Ranges: []DateRange{{From: d("2027-07-01"), To: d("2027-08-31")}}},
		{Code: "EV", Kind: PeriodEvent, Ranges: []DateRange{{From: d("2027-07-20"), To: d("2027-07-22")}}},
		{Code: "OLD", Kind: PeriodEvent, Ranges: []DateRange{{From: d("2026-07-20"), To: d("2026-07-22")}}},
	}
	m, err := Matrix(cat, MatrixSpec{ContractType: ContractTransit, QuotedOn: d("2026-12-01")})
	if err != nil {
		t.Fatal(err)
	}
	// Columns: seasons then events by code; probes at the first night of the first range inside validity;
	// OLD has no range inside validity.
	wantCols := []MatrixColumn{
		{Key: "HIGH", Arrival: d("2027-07-01"), Departure: d("2027-07-02")},
		{Key: "LOW", Arrival: d("2027-01-01"), Departure: d("2027-01-02")},
		{Key: "EV", Arrival: d("2027-07-20"), Departure: d("2027-07-21")},
	}
	if len(m.Columns) != len(wantCols) {
		t.Fatalf("columns %+v", m.Columns)
	}
	for i, c := range wantCols {
		if m.Columns[i] != c {
			t.Errorf("column %d = %+v, want %+v", i, m.Columns[i], c)
		}
	}
	// Rows: band probes at max_cm, open band at previous + 1, beam = half_even(loa / 3).
	wantRows := []MatrixRow{
		{Key: "L08", Kind: RowKindBand, ProbeLOA: 800, ProbeBeam: 267},
		{Key: "L10", Kind: RowKindBand, ProbeLOA: 1000, ProbeBeam: 333},
		{Key: "L12", Kind: RowKindBand, ProbeLOA: 1200, ProbeBeam: 400},
		{Key: "L15", Kind: RowKindBand, ProbeLOA: 1500, ProbeBeam: 500},
		{Key: "LXX", Kind: RowKindBand, ProbeLOA: 1501, ProbeBeam: 500},
	}
	for i, r := range wantRows {
		if m.Rows[i] != r {
			t.Errorf("row %d = %+v, want %+v", i, m.Rows[i], r)
		}
	}
	// Totals by hand: Amounts of r-lb per band, one night, VAT included.
	want := []money.Cents{4000, 5000, 6500, 9000, 15000}
	for _, c := range m.Cells {
		if c.Breakdown == nil || c.Breakdown.Total != want[c.Row] {
			t.Errorf("cell %d,%d = %+v %s, want %d", c.Row, c.Col, c.Breakdown, c.Error, want[c.Row])
		}
		if c.Probe.Channel != ChannelDesk || c.Probe.Vessel.HullType != DefaultHullType {
			t.Errorf("probe %+v", c.Probe)
		}
	}
	if _, err := Matrix(cat, MatrixSpec{ContractType: ContractAnnual}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("annual not served: %v", err)
	}

	units := buildCatalog(t, corpusCase{Catalog: "units"})
	for ct, col := range map[ContractType]MatrixColumn{
		ContractAnnual:   {Key: ColumnYear, Arrival: d("2027-01-01"), Departure: d("2028-01-01")},
		ContractSeasonal: {Key: ColumnSeason, Arrival: d("2027-05-01"), Departure: d("2027-10-01")},
		ContractMonthly:  {Key: ColumnAll, Arrival: d("2027-01-01"), Departure: d("2027-02-01")},
		ContractTransit:  {Key: ColumnAll, Arrival: d("2027-01-01"), Departure: d("2027-01-02")},
	} {
		m, err := Matrix(units, MatrixSpec{ContractType: ct, Rows: RowsCategories, Categories: []CoverageCategory{{Code: "A", LOA: 1000, Beam: 350}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Columns) != 1 || m.Columns[0] != col {
			t.Errorf("%s columns %+v, want %+v", ct, m.Columns, col)
		}
	}
}

func TestCoverage_Scenario(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "per-berth"})
	rep := Coverage(cat, CoverageSpec{Categories: []CoverageCategory{{Code: "A", LOA: 1000, Beam: 300}, {Code: "Z", LOA: 1000, Beam: 300}}, QuotedOn: d("2026-12-01")})
	// transit: A priced, Z has no amount; annual: both priced (default amount). No bands, no periods.
	if rep.Checked != 4 {
		t.Errorf("checked %d", rep.Checked)
	}
	if len(rep.Gaps) != 1 || rep.Gaps[0] != (Gap{ContractType: ContractTransit, Category: "Z", ItemID: "trn", Reason: ReasonNoAmount}) {
		t.Errorf("gaps %+v", rep.Gaps)
	}

	per := buildCatalog(t, corpusCase{Catalog: "periods"})
	per.Rules = per.Rules[:3] // LOW, MID, HIGH only: SHOW and REGATTA have no price
	rep = Coverage(per, CoverageSpec{Categories: []CoverageCategory{{Code: "A"}}})
	var periods []string
	for _, g := range rep.Gaps {
		periods = append(periods, g.Period+":"+g.Reason)
	}
	if len(periods) != 2 || periods[0] != "REGATTA:no_rule" || periods[1] != "SHOW:no_rule" {
		t.Errorf("gaps %v", periods)
	}
}

func TestRenew_ZeroAdjustmentShiftsOnly(t *testing.T) {
	for _, name := range fixtureNames(t) {
		cat := buildCatalog(t, corpusCase{Catalog: name})
		out, err := Renew(cat, RenewalSpec{ShiftYears: 0, Code: "COPY"})
		if err != nil {
			t.Fatal(err)
		}
		if out.PriceList.ID != "" || out.PriceList.Code != "COPY" {
			t.Fatalf("%s: list id/code %q %q", name, out.PriceList.ID, out.PriceList.Code)
		}
		for i := range out.Rules {
			if out.Rules[i].ID != "" || out.Rules[i].Seq != cat.Rules[i].Seq {
				t.Fatalf("%s: rule %d id %q seq %d", name, i, out.Rules[i].ID, out.Rules[i].Seq)
			}
			out.Rules[i].ID = cat.Rules[i].ID
		}
		out.PriceList.ID, out.PriceList.Code = cat.PriceList.ID, cat.PriceList.Code
		if a, b := mustJSON(t, out), mustJSON(t, cat); !bytes.Equal(a, b) {
			t.Fatalf("%s: a plain copy changed the content:\n%s\n%s", name, a, b)
		}
	}
	// A shift moves every date by whole years and nothing else.
	cat := buildCatalog(t, corpusCase{Catalog: "periods"})
	out, err := Renew(cat, RenewalSpec{ShiftYears: 1, Code: "PER-2028"})
	if err != nil {
		t.Fatal(err)
	}
	if out.PriceList.ValidFrom != d("2028-01-01") || out.PriceList.ValidTo != d("2028-12-31") ||
		out.PriceList.Periods[3].Ranges[0] != (DateRange{From: d("2028-09-15"), To: d("2028-09-20")}) {
		t.Fatalf("shifted list %+v", out.PriceList)
	}
	if *out.Rules[0].AmountCents != *cat.Rules[0].AmountCents {
		t.Fatalf("amount changed without adjustments")
	}
	if cat.PriceList.ValidFrom != d("2027-01-01") {
		t.Fatalf("Renew modified its input")
	}
}

func TestRenew_Scenario(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "per-berth"})
	cat.PriceList.Bands = []Band{{Code: "L10", MaxCm: ptr(geo.Cm(1000)), Category: "B"}, {Code: "LXX"}}
	cat.Rules = append(cat.Rules,
		Rule{ID: "r-lb", Seq: 3, Stage: StageBase, Active: true, ItemID: "trn", Basis: BasisLengthBand, Unit: UnitNight, Amounts: map[string]money.Cents{"L10": 5000, "LXX": 9000}},
		Rule{ID: "r-pm", Seq: 4, Stage: StageBase, Active: true, ItemID: "orm", Basis: BasisPerMeter, Unit: UnitYear, UnitCents: ptr(money.Cents(12345))},
		Rule{ID: "m-x", Seq: 5, Stage: StageModifier, Active: true, Effect: Effect{AmountCents: ptr(money.Cents(1000))}},
	)
	spec := RenewalSpec{
		ShiftYears: 1, ISTATBP: 150, Code: "PB-2028", AmountRounding: AmountRoundTenCents,
		Adjustments: []Adjustment{
			{Scope: AdjustAll, PercentBP: ptr(int64(200))},
			{Scope: AdjustCategory, Category: "B", AmountCents: ptr(money.Cents(-1000))},
			{Scope: AdjustItem, ItemID: "orm", PercentBP: ptr(int64(500))},
		},
	}
	out, err := Renew(cat, spec)
	if err != nil {
		t.Fatal(err)
	}
	// Hand derivation (ISTAT 1.5 %, then the most specific adjustment, then rounding to 10 cents):
	// trn per_berth A 5000: ISTAT 5075, all +2 % = half_even(5176.5) = 5176 (even), round 10 -> 5180.
	// trn per_berth B 8000: ISTAT 8120, category B -1000 = 7120 -> 7120.
	// trn per_berth C 12000: ISTAT 12180, all +2 % = 12423.6 -> 12424 -> 12420.
	// orm per_berth A 300000: ISTAT 304500, item +5 % = 319725 -> 319720 (ties to even: 31972.5 -> 31972).
	// orm per_berth B 450000: ISTAT 456750 -> item +5 % = 479587.5 -> 479588 -> 479590.
	// orm default 200000: 203000 -> 213150 -> 213150.
	// trn length_band L10 (linked to B) 5000: 5075 - 1000 = 4075 -> 407.5 -> 408 -> 4080; LXX 9000: 9135 -> 9317.7 -> 9318 -> 9320.
	// orm per_meter 12345: ISTAT half_even(12530.175) = 12530 -> +5 % = 13156.5 -> 13156 -> 13160.
	// modifier amounts are copied unchanged: 1000.
	want := map[string]map[string]money.Cents{
		"r-pb-n": {"A": 5180, "B": 7120, "C": 12420},
		"r-pb-y": {"A": 319720, "B": 479590},
		"r-lb":   {"L10": 4080, "LXX": 9320},
	}
	for i, r := range out.Rules {
		id := cat.Rules[i].ID
		if w, ok := want[id]; ok {
			for k, v := range w {
				if r.Amounts[k] != v {
					t.Errorf("%s[%s] = %d, want %d", id, k, r.Amounts[k], v)
				}
			}
		}
		switch id {
		case "r-pb-y":
			if *r.AmountCents != 213150 {
				t.Errorf("r-pb-y default = %d, want 213150", *r.AmountCents)
			}
		case "r-pm":
			if *r.UnitCents != 13160 {
				t.Errorf("r-pm unit = %d, want 13160", *r.UnitCents)
			}
		case "m-x":
			if *r.Effect.AmountCents != 1000 {
				t.Errorf("modifier changed: %d", *r.Effect.AmountCents)
			}
		}
	}
	if out.PriceList.ValidFrom != d("2028-01-01") || out.PriceList.Code != "PB-2028" {
		t.Errorf("list %+v", out.PriceList)
	}
	// Fixed adjustments never go below zero; validity overrides apply after the shift.
	out, err = Renew(cat, RenewalSpec{
		ShiftYears: 1, Adjustments: []Adjustment{{Scope: AdjustAll, AmountCents: ptr(money.Cents(-1_000_000))}},
		ValidTo: ptr(d("2028-06-30")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Rules[0].Amounts["A"] != 0 || out.PriceList.ValidTo != d("2028-06-30") {
		t.Errorf("clamp/override: %d %s", out.Rules[0].Amounts["A"], out.PriceList.ValidTo)
	}
	for _, bad := range []RenewalSpec{
		{ShiftYears: 6},
		{ShiftYears: -1},
		{ISTATBP: 10_001},
		{AmountRounding: "cents"},
		{Adjustments: []Adjustment{{Scope: AdjustAll}}},
		{Adjustments: []Adjustment{{Scope: AdjustItem, PercentBP: ptr(int64(1))}}},
		{Adjustments: []Adjustment{{Scope: "list", PercentBP: ptr(int64(1))}}},
	} {
		if _, err := Renew(cat, bad); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	// Diff: the matrix before/after, with delta and delta bp (on the plain fixture).
	cat = buildCatalog(t, corpusCase{Catalog: "per-berth"})
	diff, err := Diff(cat, mustRenew(t, cat, RenewalSpec{ShiftYears: 1, Adjustments: []Adjustment{{Scope: AdjustAll, PercentBP: ptr(int64(1000))}}}),
		MatrixSpec{ContractType: ContractTransit, Rows: RowsCategories, Categories: []CoverageCategory{{Code: "A", LOA: 1000}, {Code: "Z", LOA: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Cells) != 2 {
		t.Fatalf("diff cells %+v", diff.Cells)
	}
	a := diff.Cells[0]
	if *a.OldCents != 5000 || *a.NewCents != 5500 || *a.DeltaCents != 500 || *a.DeltaBP != 1000 {
		t.Errorf("diff A %+v", a)
	}
	if z := diff.Cells[1]; z.OldCents != nil || z.NewCents != nil || z.DeltaBP != nil {
		t.Errorf("diff Z %+v", z)
	}
	if diff.After.Columns[0].Arrival != d("2028-01-01") {
		t.Errorf("after probes use the shifted dates: %+v", diff.After.Columns)
	}
}

func mustRenew(t *testing.T, cat Catalog, spec RenewalSpec) Catalog {
	t.Helper()
	out, err := Renew(cat, spec)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenew_LeapDay(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "units"})
	cat.PriceList.ValidFrom, cat.PriceList.ValidTo = d("2028-02-29"), d("2029-02-28")
	cat.PriceList.Periods = []RatePeriod{{Code: "W", Kind: PeriodSeason, Ranges: []DateRange{{From: d("2028-02-29"), To: d("2028-03-31")}}}}
	out := mustRenew(t, cat, RenewalSpec{ShiftYears: 1})
	if out.PriceList.ValidFrom != d("2029-02-28") || out.PriceList.Periods[0].Ranges[0].From != d("2029-02-28") {
		t.Errorf("Feb 29 + 1 year = %s / %s", out.PriceList.ValidFrom, out.PriceList.Periods[0].Ranges[0].From)
	}
	out = mustRenew(t, cat, RenewalSpec{ShiftYears: 4})
	if out.PriceList.ValidFrom != d("2032-02-29") {
		t.Errorf("Feb 29 + 4 years = %s", out.PriceList.ValidFrom)
	}
	if w := out.PriceList.Settings.SeasonWindow; w.From != d("2031-05-01") || w.To != d("2031-09-30") {
		t.Errorf("season window %+v", w)
	}
}
