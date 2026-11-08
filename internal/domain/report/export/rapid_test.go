package export

import (
	"bytes"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/money"
)

// sameRows asserts INV-63 on one table built from n records: one row per record, one CSV line per row (plus
// header and Totale), and under every column that carries a total the Σ of the row cells equals the Totale cell.
func sameRows(t *rapid.T, tab Table, n int) {
	if len(tab.Rows) != n {
		t.Fatalf("%s: %d rows for %d records", tab.Report, len(tab.Rows), n)
	}
	var b bytes.Buffer
	if err := WriteCSV(&b, tab); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(b.String(), "\r\n"); got != n+2 {
		t.Fatalf("%s: csv lines = %d, want %d", tab.Report, got, n+2)
	}
	for i, c := range tab.Columns {
		total, ok := tab.Total[i].Int64()
		if !ok {
			continue
		}
		var sum int64
		for _, row := range tab.Rows {
			v, _ := row[i].Int64()
			sum += v
		}
		if sum != total {
			t.Fatalf("%s: Σ %s = %d, Totale = %d", tab.Report, c.Key, sum, total)
		}
	}
}

func cents(t *rapid.T, label string) money.Cents {
	return money.Cents(rapid.Int64Range(-5_000_000, 5_000_000).Draw(t, label))
}

// TestExport_SameRowsAsJSON (INV-63): the CSV/XLSX table of a report has exactly the JSON records as rows and its
// Totale row equals the totals of the same fold, for random records of the money reports.
func TestExport_SameRowsAsJSON(t *testing.T) {
	day := calendar.MustParse("2026-03-01")
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		for _, lang := range []string{"it", "en"} {
			ch := &calc.ChargesResult{}
			ch.Envelope = env(calc.KeyCharges, "")
			pr := &calc.ProductionResult{}
			pr.Envelope = env(calc.KeyProduction, "")
			pa := &calc.PaymentsResult{}
			pa.Envelope = env(calc.KeyPayments, "")
			for i := range n {
				net, vat := cents(t, "net"), cents(t, "vat")
				ch.Records = append(ch.Records, calc.ChargesRecord{ChargeID: "c", Seq: i + 1, IssuedOn: day, Net: net, VAT: vat, Gross: net + vat})
				kind := rapid.SampledFrom([]string{calc.ContractTransit, "annual", "seasonal"}).Draw(t, "kind")
				pr.Records = append(pr.Records, calc.ProductionRecord{
					Kind: kind, StartsOn: day, EndsOn: day, NightsInPeriod: rapid.Int64Range(0, 366).Draw(t, "nights"),
					Competence: cents(t, "competence"),
				})
				pa.Records = append(pa.Records, calc.PaymentsRecord{Date: day, Amount: cents(t, "amount"), Fee: cents(t, "fee")})
			}
			ch.Totals = calc.FoldCharges(ch.Records)
			pr.Totals = calc.FoldProduction(pr.Records)
			pa.Totals = calc.FoldPayments(pa.Records)
			sameRows(t, Charges(ch, lang), n)
			sameRows(t, Production(pr, lang), n)
			sameRows(t, Payments(pa, lang), n)
		}
	})
}
