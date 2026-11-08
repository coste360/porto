package export

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/xlsx"
)

func env(key calc.ReportKey, section string) calc.Envelope {
	return calc.Envelope{
		Report: key, From: calendar.MustParse("2026-01-01"), To: calendar.MustParse("2026-03-31"),
		Today: calendar.MustParse("2026-04-02"), Section: section,
	}
}

func chargesResult() *calc.ChargesResult {
	r := &calc.ChargesResult{Basis: "issued"}
	r.Envelope = env(calc.KeyCharges, "")
	r.Records = []calc.ChargesRecord{
		{
			ChargeID: "c1", Number: "A-1", Seq: 1, IssuedOn: calendar.MustParse("2026-01-05"),
			OriginType: "contract", CustomerName: "=HYPERLINK(\"x\")", Description: "Ormeggio; annuale \"A\"",
			VATRateBP: 2200, Net: 100000, VAT: 22000, Gross: 122000, PaymentStatus: "open", InvoicingStatus: "to_invoice",
		},
		{
			ChargeID: "c2", Number: "A-2", Seq: 1, IssuedOn: calendar.MustParse("2026-02-10"),
			OriginType: "adjustment", CustomerName: "-Rossi", Description: "Rettifica",
			VATRateBP: 2200, Net: -5, VAT: -1, Gross: -6, PaymentStatus: "open", InvoicingStatus: "to_invoice",
		},
	}
	r.Totals = calc.ChargesTotals{Charges: 2, Lines: 2, Net: 99995, VAT: 21999, Gross: 121994, Charged: 121994}
	r.Groups = []calc.Group[calc.ChargesTotals]{{Key: "contract", Label: "Contratto", Totals: calc.ChargesTotals{Charges: 1, Lines: 1, Net: 100000}}}
	return r
}

func TestExport_CSVItalian(t *testing.T) {
	var b bytes.Buffer
	if err := WriteCSV(&b, Charges(chargesResult(), "it")); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, "\xef\xbb\xbf") {
		t.Fatal("missing UTF-8 BOM")
	}
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(out, "\xef\xbb\xbf"), "\r\n"), "\r\n")
	if len(lines) != 4 { // header, 2 rows, Totale
		t.Fatalf("lines = %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "Data emissione;Numero;Riga;Scadenza;Origine;Cliente;") {
		t.Errorf("header = %q", lines[0])
	}
	for _, want := range []string{
		"05/01/2026;A-1;1;;contract;\"'=HYPERLINK(\"\"x\"\")\";", // formula guarded, quotes doubled
		"\"Ormeggio; annuale \"\"A\"\"\"",                        // separator inside text is quoted
		";22,00;;;;1000,00;220,00;1220,00;open;to_invoice;",      // bp as percent, cents with comma
	} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row 1 %q does not contain %q", lines[1], want)
		}
	}
	for _, want := range []string{"10/02/2026;A-2;", ";'-Rossi;", ";-0,05;-0,01;-0,06;"} {
		if !strings.Contains(lines[2], want) {
			t.Errorf("row 2 %q does not contain %q", lines[2], want)
		}
	}
	if !strings.HasPrefix(lines[3], "Totale;") || !strings.Contains(lines[3], ";999,95;219,99;1219,94;") {
		t.Errorf("totale = %q", lines[3])
	}
	// English labels, Italian formats.
	b.Reset()
	if err := WriteCSV(&b, Charges(chargesResult(), "en")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Issued on;Number;") || !strings.Contains(b.String(), "Total;") ||
		!strings.Contains(b.String(), "1000,00") {
		t.Errorf("english csv = %q", b.String())
	}
}

func TestExport_Formats(t *testing.T) {
	tests := []struct {
		n    int64
		typ  Type
		want string
	}{
		{123456, TypeCents, "1234,56"},
		{-5, TypeCents, "-0,05"},
		{0, TypeCents, "0,00"},
		{1234, TypeBP, "12,34"},
		{10000, TypeBP, "100,00"},
		{1250, TypeCm, "12,50"},
		{1_234_550, TypeCm2, "123,46"}, // 123,4550 m² → half-even on the hundredth: 123,46 (…55 → 6)
		{1_234_450, TypeCm2, "123,44"}, // 123,4450 → 123,44 (…45 → 4, even)
		{500, TypeMilli, "0,500"},
		{42, TypeInt, "42"},
		{math.MinInt64, TypeCents, "-92233720368547758,08"},
	}
	for _, tt := range tests {
		if got := decimal(tt.n, tt.typ, ','); got != tt.want {
			t.Errorf("decimal(%d, %d) = %q, want %q", tt.n, tt.typ, got, tt.want)
		}
	}
	if got := csvCell(Day(calendar.MustParse("2026-02-03")), TypeDate); got != "03/02/2026" {
		t.Errorf("date = %q", got)
	}
	if got := csvCell(Text("2026-03"), TypeMonth); got != "03/2026" {
		t.Errorf("month = %q", got)
	}
	for in, want := range map[string]string{"=1+1": "'=1+1", "+39": "'+39", "@x": "'@x", "\tx": "'\tx", "\rx": "'\rx", "ok": "ok", "": ""} {
		if got := guard(in); got != want {
			t.Errorf("guard(%q) = %q, want %q", in, got, want)
		}
	}
	if !Opt(calc.Opt[money.Cents]{}).IsNull() || !BP(calc.BP{}).IsNull() || !Day(calendar.Date{}).IsNull() || !Text("").IsNull() {
		t.Error("missing values must be empty cells")
	}
}

func TestExport_XLSXRoundTrip(t *testing.T) {
	tab := Charges(chargesResult(), "it")
	var b bytes.Buffer
	if err := WriteXLSX(&b, tab); err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.ReadFirstSheet(bytes.NewReader(b.Bytes()), int64(b.Len()), xlsx.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1+len(tab.Rows)+1 {
		t.Fatalf("rows = %d", len(rows))
	}
	for i, c := range tab.Columns {
		if rows[0][i] != c.Label {
			t.Errorf("header %d = %q, want %q", i, rows[0][i], c.Label)
		}
	}
	all := append(append([][]Value{}, tab.Rows...), tab.Total)
	for r, row := range all {
		got := rows[r+1]
		for i, c := range tab.Columns {
			want := xlsxText(row[i], c.Type)
			have := ""
			if i < len(got) {
				have = got[i]
			}
			if have != want {
				t.Errorf("row %d col %s = %q, want %q", r, c.Key, have, want)
			}
		}
	}
}

// xlsxText is what the reader returns for a cell: decimals with '.', ISO dates, text as is.
func xlsxText(v Value, typ Type) string {
	switch v.kind {
	case kindInt:
		return decimal(v.n, typ, '.')
	case kindDate:
		return v.d.String()
	case kindText:
		if typ == TypeMonth {
			return italianMonth(v.s)
		}
		return v.s
	}
	return ""
}

// allResults has one non-empty result of every report (and the comparison), every section of the multi-section
// ones, so the label and Totale checks cover every column.
func allResults() []any {
	d := calendar.MustParse("2026-02-01")
	occ := &calc.OccupancyResult{Variant: "physical", Family: "berth"}
	occ.Envelope = env(calc.KeyOccupancy, "")
	occ.Totals = calc.OccupancyTotals{Unit: calc.UnitBerthNight, OccupiedUnits: 3, AvailableUnits: 10, OccupancyBP: calc.BP{Value: 3000, OK: true}}
	occ.Records = []calc.OccupancyRecord{{Role: "numerator", ResourceCode: "A1", OccupiedUnits: 3}, {Role: "denominator", ResourceCode: "A1", AvailableUnits: 10}}
	eff := &calc.EfficiencyResult{Family: "linear_area"}
	eff.Envelope = env(calc.KeyEfficiency, "")
	eff.Totals = calc.EfficiencyTotals{Unit: calc.UnitCmNight, Nights: 2, VesselUnits: 1000, ResourceUnits: 2000}
	eff.Records = []calc.EfficiencyRecord{{ResourceCode: "Z", Nights: 2, VesselUnits: 1000, ResourceUnits: 2000, LOA: geo.Cm(1200)}}
	waste := &calc.WasteResult{ThresholdBP: 4000, Suggestions: []calc.WasteSuggestion{{Kind: "swap", AllocationIDs: []string{"a", "b"}, FromBerths: []calc.ResourceBrief{{ID: "1", Code: "A1"}}, ToBerths: []calc.ResourceBrief{{ID: "2", Code: "B2"}}, GainCm2Nights: 12345}}}
	waste.Envelope = env(calc.KeyAllocationWaste, "")
	waste.Records = []calc.WasteRecord{{BerthCode: "A1", WasteBP: 5000, NightsInPeriod: 3, Lost: calc.Opt[money.Cents]{Value: 100, OK: true}}}
	waste.Totals = calc.WasteTotals{Count: 1, Nights: 3, Lost: calc.Opt[money.Cents]{Value: 100, OK: true}}
	ld := &calc.LostDemandResult{}
	ld.Envelope = env(calc.KeyLostDemand, "")
	ld.Records = []calc.LostDemandRecord{{Arrival: d, Departure: d.AddDays(2), NightsInPeriod: 2}}
	ld.Totals = calc.LostDemandTotals{Count: 1, Nights: 2}
	prod := &calc.ProductionResult{}
	prod.Envelope = env(calc.KeyProduction, "")
	prod.Records = []calc.ProductionRecord{{Number: "C1", StartsOn: d, EndsOn: d.AddDays(5), Competence: 500}}
	prod.Totals = calc.ProductionTotals{Contracts: 1, Competence: 500}
	ch := chargesResult()
	rc := &calc.RevenueClassesResult{Basis: "competence", Classes: []calc.RevenueClassGroup{{Key: "MOOR", Label: "Ormeggi", Net: 100, Months: []calc.RevenueClassMonth{{Month: "2026-02", Net: 100}}}}}
	rc.Envelope = env(calc.KeyRevenueClasses, "")
	rc.Records = []calc.ChargesRecord{{Number: "A-1", Month: "2026-02", Net: 100}}
	rc.Totals = calc.RevenueClassesTotals{Lines: 1, Net: 100}
	cash := &calc.CashResult{}
	cash.Totals = calc.CashTotals{VAT: []calc.CashVATTotal{{RateBP: 2200, Net: 100, VAT: 22, Gross: 122}}, Methods: []calc.CashMethodTotal{{Method: "cash", Total: 122}}}
	vs := &calc.VesselsResult{}
	vs.Envelope = env(calc.KeyVessels, "")
	vs.Records = []calc.VesselsRecord{{Name: "Aurora", Nights: 4, LOA: 1200}}
	vs.Totals = calc.VesselsTotals{Vessels: 1, Nights: 4, LOASum: 1200, AvgLOA: calc.Opt[geo.Cm]{Value: 1200, OK: true}}
	br := &calc.BerthRevenueResult{}
	br.Envelope = env(calc.KeyBerthRevenue, "")
	br.Records = []calc.BerthRevenueRecord{{BerthCode: "A1", Length: 1200, Revenue: 1000}}
	br.Totals = calc.BerthRevenueTotals{Berths: 1, Length: 1200, Revenue: 1000}
	cmp := &calc.Comparison{
		Indicator: "occupancy", Unit: calc.UnitBP,
		Current:  calc.ComparisonPeriod{From: d, To: d.AddDays(1), Value: calc.Opt[int64]{Value: 5000, OK: true}, Numerator: 1, Denominator: 2, HasFraction: true},
		Previous: calc.ComparisonPeriod{Value: calc.Opt[int64]{}, HasFraction: true},
		Series:   []calc.ComparisonPoint{{Bucket: d, Current: calc.Opt[int64]{Value: 5000, OK: true}, CurrentNumerator: 1, CurrentDenominator: 2, HasFraction: true}},
	}
	out := []any{occ, eff, waste, ld, prod, ch, rc, vs, br, cmp}
	for _, s := range []string{"customers", "renewals"} {
		x := &calc.CustomersResult{}
		x.Envelope = env(calc.KeyCustomers, s)
		x.Records = []calc.CustomersRecord{{Section: s, CustomerName: "Bianchi", Value: 10}}
		out = append(out, x)
	}
	for _, s := range []string{"competence", "deferral", "accrual"} {
		x := &calc.AccrualsResult{}
		x.Envelope = env(calc.KeyAccruals, s)
		x.Records = []calc.AccrualsRecord{{Section: s, Date: d}}
		out = append(out, x)
	}
	for _, s := range []string{"main", "payouts"} {
		x := &calc.PaymentsResult{}
		x.Envelope = env(calc.KeyPayments, s)
		x.Records = []calc.PaymentsRecord{{Section: s, Date: d, Amount: 10}}
		out = append(out, x)
	}
	for _, s := range []string{"open", "future", "reminders"} {
		x := &calc.ReceivablesResult{}
		x.Envelope = env(calc.KeyReceivables, s)
		x.Records = []calc.ReceivablesRecord{{Section: s, Number: "A-1", Overdue: true}}
		out = append(out, x)
	}
	for _, s := range []string{"movements", "absences", "releases"} {
		x := &calc.PresencesResult{}
		x.Envelope = env(calc.KeyPresences, s)
		x.Records = []calc.PresencesRecord{{Section: s, Night: d}}
		out = append(out, x)
	}
	for _, s := range []string{"holders", "charges", "allotments", "mandates"} {
		x := &calc.RightsResult{}
		x.Envelope = env(calc.KeyRights, s)
		x.Records = []calc.RightsRecord{{Section: s, ShareMillis: 500}}
		out = append(out, x)
	}
	for _, s := range []string{"sessions", "fiscal"} {
		x := &calc.CashResult{Totals: cash.Totals}
		x.Envelope = env(calc.KeyCash, s)
		x.Records = []calc.CashRecord{{Section: s, BusinessDate: d}}
		out = append(out, x)
	}
	return out
}

func TestExport_EveryReport(t *testing.T) {
	if _, err := Of(42, "it"); err == nil {
		t.Error("Of(int) must fail")
	}
	for _, res := range allResults() {
		for _, lang := range []string{"it", "en"} {
			tab, err := Of(res, lang)
			if err != nil {
				t.Fatal(err)
			}
			name := string(tab.Report) + "/" + tab.Section + "/" + lang
			if tab.Report == "" || tab.From.IsZero() || len(tab.Columns) == 0 || len(tab.Rows) == 0 {
				t.Fatalf("%s: incomplete table %+v", name, tab)
			}
			for _, c := range tab.Columns {
				if _, ok := labels[c.Key]; !ok {
					t.Errorf("%s: column %s has no label", name, c.Key)
				}
			}
			for _, s := range tab.Summaries {
				for _, c := range s.Columns {
					if _, ok := labels[c.Key]; !ok {
						t.Errorf("%s/%s: column %s has no label", name, s.Name, c.Key)
					}
				}
			}
			for _, row := range tab.Rows {
				if len(row) != len(tab.Columns) {
					t.Errorf("%s: row has %d cells for %d columns", name, len(row), len(tab.Columns))
				}
			}
			if len(tab.Total) != len(tab.Columns) || tab.Total[0].String() == "" {
				t.Errorf("%s: Totale row %v", name, tab.Total)
			}
			var csvb, xb bytes.Buffer
			if err := WriteCSV(&csvb, tab); err != nil {
				t.Errorf("%s: csv: %v", name, err)
			}
			if err := WriteXLSX(&xb, tab); err != nil {
				t.Errorf("%s: xlsx: %v", name, err)
			}
			if got := strings.Count(csvb.String(), "\r\n"); got != len(tab.Rows)+2 {
				t.Errorf("%s: csv lines = %d", name, got)
			}
		}
	}
	for k, l := range labels {
		if l[0] == "" || l[1] == "" {
			t.Errorf("label %s incomplete", k)
		}
	}
}

func TestExport_TotalRowFromTotals(t *testing.T) {
	tab := Charges(chargesResult(), "it")
	want := map[string]int64{"net_cents": 99995, "vat_cents": 21999, "gross_cents": 121994}
	for i, c := range tab.Columns {
		w, ok := want[c.Key]
		if !ok {
			continue
		}
		if n, _ := tab.Total[i].Int64(); n != w {
			t.Errorf("Totale %s = %d, want %d", c.Key, n, w)
		}
		var sum int64
		for _, row := range tab.Rows {
			n, _ := row[i].Int64()
			sum += n
		}
		if sum != w {
			t.Errorf("Σ %s = %d, want %d", c.Key, sum, w)
		}
	}
	if got := tab.Filename("csv"); got != "porto-charges-2026-01-01-2026-03-31.csv" {
		t.Errorf("filename = %q", got)
	}
}

func TestExport_HiddenMoneyIsEmpty(t *testing.T) {
	r := &calc.WasteResult{}
	r.Envelope = env(calc.KeyAllocationWaste, "")
	r.Records = []calc.WasteRecord{{BerthCode: "A1", NightsInPeriod: 2}}
	r.Totals = calc.WasteTotals{Count: 1, Nights: 2}
	var b bytes.Buffer
	if err := WriteCSV(&b, Waste(r, "it")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(b.String(), "\r\n")
	if !strings.HasSuffix(lines[1], ";2;;;") {
		t.Errorf("hidden money must be empty cells: %q", lines[1])
	}
}

// TestExport_ComparisonCSV checks the comparison table written with the shared Italian rules: value columns by
// unit (cm² half-even), the bucket as GG/MM/AAAA, the Totale row, and the fraction columns also when only the
// previous period is a ratio.
func TestExport_ComparisonCSV(t *testing.T) {
	d := calendar.MustParse("2026-01-01")
	c := &calc.Comparison{
		Unit:     calc.UnitCm2Night,
		Current:  calc.ComparisonPeriod{From: d, To: d.AddDays(30), Value: calc.Opt[int64]{Value: 125150, OK: true}},
		Previous: calc.ComparisonPeriod{Value: calc.Opt[int64]{Value: 125050, OK: true}, Numerator: 1, Denominator: 4, HasFraction: true},
		Series:   []calc.ComparisonPoint{{Bucket: d, Current: calc.Opt[int64]{Value: 125150, OK: true}}},
	}
	var b bytes.Buffer
	if err := WriteCSV(&b, Comparison(c, "it")); err != nil {
		t.Fatal(err)
	}
	want := bom + "Periodo;Valore;Valore anno precedente;Numeratore;Denominatore;Numeratore anno precedente;Denominatore anno precedente\r\n" +
		"01/01/2026;12,52;;0;0;0;0\r\n" +
		"Totale;12,52;12,50;0;0;1;4\r\n"
	if b.String() != want {
		t.Fatalf("csv\n%q\nwant\n%q", b.String(), want)
	}
}
