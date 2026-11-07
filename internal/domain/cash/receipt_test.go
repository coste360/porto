package cash

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

func sampleReceipt() views.ReceiptView {
	return views.ReceiptView{
		Marina:   views.Marina{Name: "Porto <Demo>", LegalName: "Porto Demo Srl", VATNumber: "01234567890", Website: "https://example.test"},
		Customer: &views.Party{DisplayName: "Mario Rossi"},
		Number:   "P2027-000012", Date: calendar.MustParse("2027-07-03"), Method: "cash", AmountCents: 4760,
		Lines: []views.ReceiptLine{
			{Description: "Gasolio <b>", Qty: 20000, Gross: 3660, VATRate: 2200},
			{Description: "Acqua", Qty: 1000, Gross: 1100, VATRate: 1000},
		},
		VAT:          []views.VATRow{{RateBP: 2200, Net: 3000, VAT: 660, Gross: 3660}, {RateBP: 1000, Net: 1000, VAT: 100, Gross: 1100}},
		FiscalNumber: "20270703-0001", Footer: "Grazie\nArrivederci",
	}
}

func TestReceiptHTML_NoScriptAndNotFiscal(t *testing.T) {
	for _, format := range []string{Receipt80, Receipt58} {
		for _, locale := range []string{"it", "en"} {
			b, err := ReceiptHTML(sampleReceipt(), format, locale)
			if err != nil {
				t.Fatal(err)
			}
			html := string(b)
			lower := strings.ToLower(html)
			for _, bad := range []string{"<script", "http://", "https://", "src=", "<link", "@import", "<b>"} {
				if strings.Contains(lower, bad) {
					t.Fatalf("%s/%s contains %q", format, locale, bad)
				}
			}
			for _, want := range []string{"Non valido ai fini fiscali", "P2027-000012", "47,60", "20270703-0001", "03/07/2027", "Porto &lt;Demo&gt;", "Grazie<br>"} {
				if !strings.Contains(html, want) {
					t.Fatalf("%s/%s lacks %q:\n%s", format, locale, want, html)
				}
			}
			if !strings.Contains(html, format+"mm") {
				t.Fatalf("page width %s missing", format)
			}
		}
	}
	if rateLabel(2200) != "22" || rateLabel(450) != "4,5" || qtyLabel(20000) != "20" || qtyLabel(1500) != "1,5" {
		t.Fatal("labels")
	}
}

func TestZReport_HTMLAndPDF(t *testing.T) {
	open := time.Date(2027, 7, 3, 7, 0, 0, 0, time.UTC)
	z := &ZReport{
		MarinaName: "Porto Demo", RegisterCode: "main", Locale: "it",
		Session:     store.CashSession{FloatCents: 10000, BusinessDate: calendar.MustParse("2027-07-03"), OpenedAt: open, ClosedAt: open.Add(10 * time.Hour)},
		Summary:     summary.Compute(summary.Input{Float: 10000, Payments: map[string]money.Cents{"cash": 5000}, Counted: map[string]money.Cents{"cash": 14800}}),
		VAT:         []summary.VATRow{{RateBP: 2200, Net: 4098, VAT: 902, Gross: 5000}},
		Movements:   []store.CashMovement{{Kind: "out", AmountCents: 200, Reason: "Spese"}},
		FiscalCount: 2, FiscalFirst: "20270703-0001", FiscalLast: "20270703-0002",
	}
	b, err := z.HTML()
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"Chiusura di cassa (Z)", "150,00", "148,00", "-2,00", "20270703-0001 – 20270703-0002", "Spese"} {
		if !strings.Contains(html, want) {
			t.Fatalf("z html lacks %q:\n%s", want, html)
		}
	}
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Fatal("script in z report")
	}
	pdf, err := z.PDF()
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("pdf %v", err)
	}
}
