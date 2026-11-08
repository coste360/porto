package admin

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/xlsx"
)

func mustDate(t *testing.T, s string) calendar.Date {
	t.Helper()
	d, err := calendar.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func some(v int64) calc.Opt[int64] { return calc.Opt[int64]{Value: v, OK: true} }

func TestDashboardOf_Mapping(t *testing.T) {
	d := &calc.Dashboard{
		From: mustDate(t, "2026-01-01"), To: mustDate(t, "2026-11-08"), Today: mustDate(t, "2026-11-08"),
		PrevFrom: mustDate(t, "2025-01-01"), PrevTo: mustDate(t, "2025-11-08"),
		Compare: "previous_year", Align: "date", Granularity: "month",
		Tiles: []calc.Tile{
			{
				Key: calc.TileOccupancy, Unit: calc.UnitBP, Available: true, DimsApplied: true,
				Current:  calc.TileValue{Value: some(6250), Numerator: 5, Denominator: 8, HasFraction: true},
				Previous: &calc.TileValue{Value: some(5000), Numerator: 4, Denominator: 8, HasFraction: true},
				Delta:    some(1250), DeltaBP: calc.BP{Value: 2500, OK: true}, Trend: "up",
				Series: []calc.SeriesPoint{{Bucket: mustDate(t, "2026-01-01"), Value: some(1), Previous: calc.Opt[int64]{}}},
				Drill:  &calc.Drill{Report: calc.KeyOccupancy, Params: map[string]string{"variant": "physical"}},
			},
			{Key: calc.TileYardHours, Unit: calc.UnitCount, Reason: calc.ReasonMilestoneM10},
			{
				Key: calc.TileProduction, Unit: calc.UnitCents, Available: true,
				Current:   calc.TileValue{Value: some(123456)},
				Breakdown: []calc.Breakdown{{Key: "IT", Label: "Italia", Value: 3}, {Key: "x", Value: 1}},
				Drill:     &calc.Drill{Report: calc.KeyProduction},
			},
		},
	}
	out := dashboardOf(d)
	if out.PreviousPeriod == nil || out.PreviousPeriod.From != "2025-01-01" || out.Period.To != "2026-11-08" {
		t.Fatalf("periods %+v %+v", out.Period, out.PreviousPeriod)
	}
	occ := out.Tiles[0]
	if *occ.Value != 6250 || *occ.Numerator != 5 || *occ.Denominator != 8 || *occ.Previous.Numerator != 4 ||
		*occ.Delta != 1250 || *occ.DeltaBp != 2500 || occ.Trend != "up" || occ.Drill.Params["variant"] != "physical" {
		t.Fatalf("occupancy tile %+v", occ)
	}
	if occ.Series[0].Value == nil || occ.Series[0].Previous != nil {
		t.Fatalf("series %+v", occ.Series)
	}
	yard := out.Tiles[1]
	if yard.Available || yard.Reason == nil || *yard.Reason != "milestone_m10" || yard.Value != nil || yard.Trend != "none" ||
		yard.Drill != nil || yard.Series == nil {
		t.Fatalf("yard tile %+v", yard)
	}
	prod := out.Tiles[2]
	if prod.Numerator != nil || prod.Previous != nil || prod.Breakdown == nil || len(*prod.Breakdown) != 2 ||
		(*prod.Breakdown)[1].Label != nil || prod.Drill.Params == nil {
		t.Fatalf("production tile %+v", prod)
	}
	// JSON shape: null value for the unavailable tile, filters always an object.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"filters":{}`) || !strings.Contains(string(b), `"value":null`) {
		t.Fatalf("json %s", b)
	}
	none := dashboardOf(&calc.Dashboard{Compare: "none"})
	if none.PreviousPeriod != nil || none.Tiles == nil {
		t.Fatalf("no comparison %+v", none)
	}
}

func comparisonFixture(t *testing.T, unit calc.Unit, fraction bool) *calc.Comparison {
	t.Helper()
	return &calc.Comparison{
		Indicator: "x", Unit: unit, Align: "weekday", Granularity: "month", YearsBack: 1,
		Current:  calc.ComparisonPeriod{From: mustDate(t, "2026-01-01"), To: mustDate(t, "2026-02-28"), Value: some(-123456), Numerator: 3, Denominator: 4, HasFraction: fraction},
		Previous: calc.ComparisonPeriod{From: mustDate(t, "2025-01-02"), To: mustDate(t, "2025-03-01"), Value: calc.Opt[int64]{}, HasFraction: fraction},
		Delta:    calc.Opt[int64]{}, DeltaBP: calc.BP{},
		Series: []calc.ComparisonPoint{
			{Bucket: mustDate(t, "2026-01-01"), Current: some(100005), Previous: some(5), CurrentNumerator: 1, CurrentDenominator: 2, HasFraction: fraction},
			{Bucket: mustDate(t, "2026-02-01"), Current: some(-223461), Previous: calc.Opt[int64]{}, CurrentNumerator: 2, CurrentDenominator: 2, HasFraction: fraction},
		},
	}
}

func TestComparisonOf_Mapping(t *testing.T) {
	c := comparisonFixture(t, calc.UnitBP, true)
	out := comparisonOf(c)
	if out.Current.Numerator == nil || *out.Current.Value != -123456 || out.Previous.Value != nil || out.Delta != nil ||
		out.DeltaBp != nil || len(out.Series) != 2 || *out.Series[0].CurrentDenominator != 2 || out.Series[1].Previous != nil ||
		out.Filters == nil || out.Align != "weekday" {
		t.Fatalf("comparison %+v", out)
	}
	plain := comparisonOf(comparisonFixture(t, calc.UnitCents, false))
	if plain.Current.Numerator != nil || plain.Series[0].CurrentNumerator != nil {
		t.Fatalf("plain comparison %+v", plain)
	}
}

// comparisonCSV and comparisonXLSX run the handler's export path (export.Comparison then WriteCSV/WriteXLSX).
func comparisonCSV(c *calc.Comparison, lang string) ([]byte, error) {
	b, _, _, err := comparisonExport(c, lang, report.FormatCSV)
	return b, err
}

func comparisonXLSX(c *calc.Comparison, lang string) ([]byte, error) {
	b, _, _, err := comparisonExport(c, lang, report.FormatXLSX)
	return b, err
}

func TestComparisonCSV_Italian(t *testing.T) {
	b, err := comparisonCSV(comparisonFixture(t, calc.UnitCents, false), "it")
	if err != nil {
		t.Fatal(err)
	}
	want := "\ufeffPeriodo;Valore;Valore anno precedente\r\n" +
		"01/01/2026;1000,05;0,05\r\n" +
		"01/02/2026;-2234,61;\r\n" +
		"Totale;-1234,56;\r\n"
	if string(b) != want {
		t.Fatalf("csv\n%q\nwant\n%q", b, want)
	}
	b, err = comparisonCSV(comparisonFixture(t, calc.UnitBP, true), "en")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\r\n")
	if lines[0] != "\ufeffBucket;Value;Previous value;Numerator;Denominator;Previous numerator;Previous denominator" ||
		lines[1] != "01/01/2026;1000,05;0,05;1;2;0;0" || lines[3] != "Total;-1234,56;;3;4;0;0" {
		t.Fatalf("csv en %q", lines)
	}
	// Unknown language falls back to Italian labels.
	if b, _ := comparisonCSV(comparisonFixture(t, calc.UnitCount, false), "fr"); !strings.HasPrefix(string(b), "\ufeffPeriodo;") ||
		!strings.Contains(string(b), "01/01/2026;100005;5\r\n") {
		t.Fatalf("csv fr %q", b)
	}
	// A fraction only on the previous period still exports the fraction columns.
	c := comparisonFixture(t, calc.UnitBP, false)
	c.Previous.HasFraction = true
	if b, _ := comparisonCSV(c, "it"); !strings.Contains(string(b), "Denominatore anno precedente") {
		t.Fatalf("csv previous fraction %q", b)
	}
}

// TestComparisonCSV_Units checks the value cell of every unit (cents → euro, bp → percent, cm → m, cm² → m²
// half-even, counts plain) through the shared export formatter.
func TestComparisonCSV_Units(t *testing.T) {
	tests := []struct {
		v    int64
		u    calc.Unit
		want string
	}{
		{0, calc.UnitCents, "0,00"},
		{-5, calc.UnitCents, "-0,05"},
		{123456, calc.UnitCentsPerM, "1234,56"},
		{123456, calc.UnitCentsPerNight, "1234,56"},
		{6250, calc.UnitBP, "62,50"},
		{1250, calc.UnitCm, "12,50"},
		{1250, calc.UnitCmNight, "12,50"},
		{125050, calc.UnitCm2, "12,50"},      // 12.505 m² → half-even 12.50
		{125150, calc.UnitCm2Night, "12,52"}, // 12.515 m² → half-even 12.52
		{42, calc.UnitCount, "42"},
		{7, calc.UnitBerthNight, "7"},
	}
	for _, tt := range tests {
		c := comparisonFixture(t, tt.u, false)
		c.Series = c.Series[:1]
		c.Series[0].Current = some(tt.v)
		b, err := comparisonCSV(c, "it")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Split(string(b), "\r\n")[1]; !strings.HasPrefix(got, "01/01/2026;"+tt.want+";") {
			t.Errorf("unit %s value %d: row %q, want %q", tt.u, tt.v, got, tt.want)
		}
	}
}

// TestComparisonExport_SameRowsAsJSON is INV-63 for the comparison: the export rows are the JSON series in the
// same order and every exported column equals the JSON field of the same name, for both alignments and with
// and without the fraction columns; the Totale row carries the period values.
func TestComparisonExport_SameRowsAsJSON(t *testing.T) {
	for _, align := range []string{"date", "weekday"} {
		for _, unit := range []calc.Unit{calc.UnitCents, calc.UnitBP, calc.UnitCm2Night, calc.UnitCount} {
			for _, frac := range []bool{false, true} {
				c := comparisonFixture(t, unit, frac)
				c.Align = align
				b, err := json.Marshal(comparisonOf(c))
				if err != nil {
					t.Fatal(err)
				}
				var doc struct {
					Series  []map[string]any `json:"series"`
					Current map[string]any   `json:"current"`
				}
				dec := json.NewDecoder(bytes.NewReader(b))
				dec.UseNumber()
				if err := dec.Decode(&doc); err != nil {
					t.Fatal(err)
				}
				tab := export.Comparison(c, "it")
				if len(tab.Rows) != len(doc.Series) {
					t.Fatalf("%s/%s: %d rows, %d points", align, unit, len(tab.Rows), len(doc.Series))
				}
				for i, row := range tab.Rows {
					for k, col := range tab.Columns {
						jv, ok := doc.Series[i][col.Key]
						if !ok {
							if !row[k].IsNull() {
								t.Errorf("%s/%s: point %d has no %s, export %q", align, unit, i, col.Key, row[k].String())
							}
							continue
						}
						if got, want := row[k].String(), jsonText(jv); got != want {
							t.Errorf("%s/%s frac=%v: point %d %s: export %q, json %q", align, unit, frac, i, col.Key, got, want)
						}
					}
				}
				if got, want := tab.Total[1].String(), jsonText(doc.Current["value"]); got != want {
					t.Errorf("%s/%s: Totale %q, json %q", align, unit, got, want)
				}
			}
		}
	}
}

func TestComparisonXLSX_RoundTrip(t *testing.T) {
	b, err := comparisonXLSX(comparisonFixture(t, calc.UnitCents, false), "it")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.ReadFirstSheet(bytes.NewReader(b), int64(len(b)), xlsx.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Periodo", "Valore", "Valore anno precedente"},
		{"2026-01-01", "1000.05", "0.05"},
		{"2026-02-01", "-2234.61"},
		{"Totale", "-1234.56"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows %q", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Fatalf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}
