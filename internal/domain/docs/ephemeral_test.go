package docs

import (
	"bytes"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs/views"
)

// TestEphemeral_ExportSummaryRenders: the built-in templates of charge_export_summary (it, en) validate against the
// fixed view and render deterministic PDFs.
func TestEphemeral_ExportSummaryRenders(t *testing.T) {
	view := views.ChargeExportSummaryView{
		Marina: views.Marina{Name: "Porto Test"}, Number: "E2027-0001", Kind: "export",
		PeriodFrom: calendar.MustParse("2027-06-01"), PeriodTo: calendar.MustParse("2027-06-30"), Filters: "Rata",
		CreatedAt: time.Date(2027, 7, 1, 9, 0, 0, 0, time.UTC), ChargeCount: 1, LineCount: 1,
		VAT:      []views.VATRow{{RateBP: 2200, Net: 10000, VAT: 2200, Gross: 12200}},
		ByOrigin: []views.TotalRow{{Key: "Rata", Count: 1, Gross: 12200}}, ByRevenueClass: []views.TotalRow{{Key: "ormeggi", Count: 1, Gross: 12200}},
		Charges: []views.ExportChargeRow{{Number: "A2027-000001", IssuedOn: calendar.MustParse("2027-06-10"), Customer: "Mario Rossi", OriginType: "Rata", Net: 10000, VAT: 2200, Total: 12200}},
		Net:     10000, VATTotal: 2200, Total: 12200,
	}
	schema, err := ephemeralSchema(KindChargeExportSummary)
	if err != nil {
		t.Fatal(err)
	}
	for _, loc := range []string{"it", "en"} {
		src, _, ok := Builtin(KindChargeExportSummary, loc)
		if !ok {
			t.Fatalf("no built-in %s", loc)
		}
		in := RenderInput{Kind: KindChargeExportSummary, Source: src, View: view, Locale: loc, Today: "2027-07-01", At: view.CreatedAt, Title: "x"}
		a, err := renderWithSchema(in, schema)
		if err != nil {
			t.Fatalf("%s: %v %+v", loc, err, err)
		}
		b, _ := renderWithSchema(in, schema)
		if !bytes.HasPrefix(a, []byte("%PDF-")) || !bytes.Equal(a, b) {
			t.Fatalf("%s: not a deterministic PDF", loc)
		}
	}
	if _, err := ephemeralSchema(KindQuote); err != nil {
		t.Fatalf("published kind: %v", err)
	}
	if _, err := ephemeralSchema(Kind("not_a_kind")); err == nil {
		t.Fatal("non-ephemeral kind accepted")
	}
}
