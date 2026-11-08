package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
)

var dateType = reflect.TypeFor[calendar.Date]()

// fill sets every exported field of v to a deterministic non-zero value derived from n (strings "v<n>", integers
// n, dates 2026-01-<n>, optionals and ratios present), so every mapped field is exercised.
func fill(v reflect.Value, n int) {
	switch {
	case v.Type() == dateType:
		v.Set(reflect.ValueOf(calendar.New(2026, 1, 1+n%28)))
		return
	case v.Kind() == reflect.Struct && v.NumField() == 2 && v.Type().Field(1).Name == "OK":
		v.Field(0).SetInt(int64(100 + n))
		v.Field(1).SetBool(true)
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("v%d", n))
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(n))
	case reflect.Bool:
		v.SetBool(n%2 == 1)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), n*7+i)
			}
		}
	}
}

func records[R any](n int, section string) []R {
	out := make([]R, n)
	for i := range out {
		fill(reflect.ValueOf(&out[i]).Elem(), i+1)
		if f := reflect.ValueOf(&out[i]).Elem().FieldByName("Section"); f.IsValid() {
			f.SetString(section)
		}
	}
	return out
}

func envelope(key calc.ReportKey, section string) calc.Envelope {
	return calc.Envelope{
		Report: key, From: calendar.New(2026, 1, 1), To: calendar.New(2026, 3, 31), Today: calendar.New(2026, 4, 1),
		Section: section, Page: calc.Page{Page: 1, PageSize: 100, Total: 3},
	}
}

// reportCase is one result with its JSON encoding.
type reportCase struct {
	name string
	res  any
	json any
}

func reportCases() []reportCase {
	var cs []reportCase
	add := func(name string, res, j any) { cs = append(cs, reportCase{name, res, j}) }
	totals := func(p any) { fill(reflect.ValueOf(p).Elem(), 3) }

	occ := &calc.OccupancyResult{Variant: "physical", Family: "berth"}
	occ.Envelope, occ.Records = envelope(calc.KeyOccupancy, ""), records[calc.OccupancyRecord](3, "")
	totals(&occ.Totals)
	occ.Totals.Unit = calc.UnitBerthNight
	add("occupancy", occ, occupancyJSON(occ))
	eff := &calc.EfficiencyResult{Family: "berth"}
	eff.Envelope, eff.Records = envelope(calc.KeyEfficiency, ""), records[calc.EfficiencyRecord](3, "")
	totals(&eff.Totals)
	eff.Totals.Unit = calc.UnitCm2Night
	add("efficiency", eff, efficiencyJSON(eff))
	ws := &calc.WasteResult{ThresholdBP: 4000}
	ws.Envelope, ws.Records = envelope(calc.KeyAllocationWaste, ""), records[calc.WasteRecord](3, "")
	totals(&ws.Totals)
	add("allocation-waste", ws, wasteJSON(ws))
	ld := &calc.LostDemandResult{}
	ld.Envelope, ld.Records = envelope(calc.KeyLostDemand, ""), records[calc.LostDemandRecord](3, "")
	totals(&ld.Totals)
	add("lost-demand", ld, lostDemandJSON(ld))
	for _, s := range []string{"customers", "renewals"} {
		x := &calc.CustomersResult{}
		x.Envelope, x.Records = envelope(calc.KeyCustomers, s), records[calc.CustomersRecord](3, s)
		totals(&x.Totals)
		add("customers/"+s, x, customersJSON(x))
	}
	pr := &calc.ProductionResult{}
	pr.Envelope, pr.Records = envelope(calc.KeyProduction, ""), records[calc.ProductionRecord](3, "")
	totals(&pr.Totals)
	add("production", pr, productionJSON(pr))
	for _, s := range []string{"competence", "deferral", "accrual"} {
		x := &calc.AccrualsResult{}
		x.Envelope, x.Records = envelope(calc.KeyAccruals, s), records[calc.AccrualsRecord](3, s)
		totals(&x.Totals)
		add("accruals/"+s, x, accrualsJSON(x))
	}
	ch := &calc.ChargesResult{Basis: "issued"}
	ch.Envelope, ch.Records = envelope(calc.KeyCharges, ""), records[calc.ChargesRecord](3, "")
	totals(&ch.Totals)
	add("charges", ch, chargesReportJSON(ch))
	for _, s := range []string{"main", "payouts"} {
		x := &calc.PaymentsResult{}
		x.Envelope, x.Records = envelope(calc.KeyPayments, s), records[calc.PaymentsRecord](3, s)
		totals(&x.Totals)
		add("payments/"+s, x, paymentsReportJSON(x))
	}
	rc := &calc.RevenueClassesResult{Basis: "competence"}
	rc.Envelope, rc.Records = envelope(calc.KeyRevenueClasses, ""), records[calc.ChargesRecord](3, "")
	totals(&rc.Totals)
	add("revenue-classes", rc, revenueClassesJSON(rc))
	for _, s := range []string{"open", "future", "reminders"} {
		x := &calc.ReceivablesResult{}
		x.Envelope, x.Records = envelope(calc.KeyReceivables, s), records[calc.ReceivablesRecord](3, s)
		totals(&x.Totals)
		add("receivables/"+s, x, receivablesReportJSON(x))
	}
	for _, s := range []string{"movements", "absences", "releases"} {
		x := &calc.PresencesResult{}
		x.Envelope, x.Records = envelope(calc.KeyPresences, s), records[calc.PresencesRecord](3, s)
		totals(&x.Totals)
		add("presences/"+s, x, presencesReportJSON(x))
	}
	for _, s := range []string{"holders", "charges", "allotments", "mandates"} {
		x := &calc.RightsResult{}
		x.Envelope, x.Records = envelope(calc.KeyRights, s), records[calc.RightsRecord](3, s)
		totals(&x.Totals)
		add("rights/"+s, x, rightsReportJSON(x))
	}
	for _, s := range []string{"sessions", "fiscal"} {
		x := &calc.CashResult{}
		x.Envelope, x.Records = envelope(calc.KeyCash, s), records[calc.CashRecord](3, s)
		totals(&x.Totals)
		add("cash/"+s, x, cashReportJSON(x))
	}
	vs := &calc.VesselsResult{}
	vs.Envelope, vs.Records = envelope(calc.KeyVessels, ""), records[calc.VesselsRecord](3, "")
	totals(&vs.Totals)
	add("vessels", vs, vesselsReportJSON(vs))
	br := &calc.BerthRevenueResult{}
	br.Envelope, br.Records = envelope(calc.KeyBerthRevenue, ""), records[calc.BerthRevenueRecord](3, "")
	totals(&br.Totals)
	add("berth-revenue", br, berthRevenueJSON(br))
	return cs
}

// TestExport_SameRowsAsJSON (INV-63): for every report and section the export table has one row per JSON record,
// in the same order, and every exported column equals the JSON field of the same name; the Totale cells equal
// the JSON totals of the same name.
func TestExport_SameRowsAsJSON(t *testing.T) {
	for _, tc := range reportCases() {
		b, err := json.Marshal(tc.json)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Records []map[string]any `json:"records"`
			Totals  map[string]any   `json:"totals"`
			Groups  []any            `json:"groups"`
			Notes   []any            `json:"notes"`
			Ignored []any            `json:"ignored_filters"`
			Filters map[string]any   `json:"filters"`
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			t.Fatal(err)
		}
		if doc.Groups == nil || doc.Notes == nil || doc.Ignored == nil || doc.Filters == nil {
			t.Errorf("%s: null array or map in %s", tc.name, b)
		}
		tab, err := export.Of(tc.res, "it")
		if err != nil {
			t.Fatal(err)
		}
		if len(tab.Rows) != len(doc.Records) {
			t.Fatalf("%s: %d rows, %d records", tc.name, len(tab.Rows), len(doc.Records))
		}
		for i, row := range tab.Rows {
			for c, col := range tab.Columns {
				jv, ok := doc.Records[i][col.Key]
				if !ok {
					if !row[c].IsNull() {
						t.Errorf("%s: record %d has no %s but the export has %q", tc.name, i, col.Key, row[c].String())
					}
					continue
				}
				if got, want := row[c].String(), jsonText(jv); got != want {
					t.Errorf("%s: record %d %s: export %q, json %q", tc.name, i, col.Key, got, want)
				}
			}
		}
		for c, col := range tab.Columns {
			if c == 0 || tab.Total[c].IsNull() {
				continue
			}
			if jv, ok := doc.Totals[col.Key]; ok {
				if got, want := tab.Total[c].String(), jsonText(jv); got != want {
					t.Errorf("%s: Totale %s: export %q, json %q", tc.name, col.Key, got, want)
				}
			}
		}
		var csvb bytes.Buffer
		if err := export.WriteCSV(&csvb, tab); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(csvb.String(), "\r\n"); got != len(doc.Records)+2 {
			t.Errorf("%s: csv lines %d for %d records", tc.name, got, len(doc.Records))
		}
	}
}

func jsonText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "1"
		}
		return "0"
	case json.Number:
		return x.String()
	case string:
		return x
	}
	return fmt.Sprint(v)
}

func TestReports_AcceptsCSV(t *testing.T) {
	for in, want := range map[string]bool{
		"text/csv": true, "application/json, text/csv;q=0.5": true, "TEXT/CSV": true,
		"": false, "application/json": false, "*/*": false,
	} {
		if got := acceptsCSV(in); got != want {
			t.Errorf("acceptsCSV(%q) = %v", in, got)
		}
	}
}
