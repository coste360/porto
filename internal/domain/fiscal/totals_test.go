package fiscal

import (
	"bytes"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/xlsx"
)

var rates = []struct {
	bp     int64
	nature string
}{{2200, ""}, {1000, ""}, {400, ""}, {0, "N1"}, {0, "N4"}}

// TestCorrispettivi_TotalsEqualPayments: the grand total and Σ rows equal Σ documented receipt payments − Σ their
// refunds; every row has net + VAT = gross; each document is counted once per VAT bucket it has.
func TestCorrispettivi_TotalsEqualPayments(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := calendar.MustParse("2027-07-01")
		methods := []string{"cash", "card_pos"}
		n := rapid.IntRange(0, 40).Draw(t, "docs")
		var docs []TotalsDoc
		var refunds []TotalsRefund
		var paid, refunded money.Cents
		for i := range n {
			pid := "p" + string(rune('a'+i%26)) + string(rune('a'+i/26))
			var buckets []Bucket
			var gross money.Cents
			for _, r := range rates {
				if !rapid.Bool().Draw(t, "has") {
					continue
				}
				g := money.Cents(rapid.Int64Range(1, 500_000).Draw(t, "g"))
				net, vat := money.VATSplit(g, r.bp)
				buckets = append(buckets, Bucket{RateBP: r.bp, Nature: r.nature, Gross: g, Net: net, VAT: vat})
				gross += g
			}
			if len(buckets) == 0 {
				continue
			}
			d := TotalsDoc{PaymentID: pid, Date: start.AddDays(rapid.IntRange(0, 40).Draw(t, "day")), Method: rapid.SampledFrom(methods).Draw(t, "m"), Buckets: buckets}
			docs = append(docs, d)
			paid += gross
			if rapid.Bool().Draw(t, "refund") {
				a := money.Cents(rapid.Int64Range(1, int64(gross)).Draw(t, "ra"))
				refunds = append(refunds, TotalsRefund{PaymentID: pid, Date: d.Date.AddDays(rapid.IntRange(0, 5).Draw(t, "rd")), Method: d.Method, Amount: a})
				refunded += a
			}
		}
		group := rapid.SampledFrom([]string{GroupDay, GroupMonth}).Draw(t, "group")
		rows, total := Aggregate(docs, refunds, group)
		if total != paid-refunded {
			t.Fatalf("total %d, want %d", total, paid-refunded)
		}
		var sum money.Cents
		count := 0
		for i, r := range rows {
			if r.Net+r.VAT != r.Gross {
				t.Fatalf("row %+v", r)
			}
			if i > 0 && rows[i-1].Period > r.Period {
				t.Fatal("rows not sorted by period")
			}
			sum += r.Gross
			count += r.Documents
		}
		want := 0
		for _, d := range docs {
			want += len(d.Buckets)
		}
		if sum != total || count != want {
			t.Fatalf("Σ rows %d total %d, documents %d/%d", sum, total, count, want)
		}
	})
}

func TestCorrispettivi_Files(t *testing.T) {
	day := calendar.MustParse("2027-07-03")
	docs := []TotalsDoc{
		{PaymentID: "p1", Date: day, Method: "cash", Buckets: []Bucket{{RateBP: 2200, Gross: 12200, Net: 10000, VAT: 2200}, {Nature: "N4", Gross: 500, Net: 500}}},
		{PaymentID: "p2", Date: day, Method: "card_pos", Buckets: []Bucket{{RateBP: 1000, Gross: 1100, Net: 1000, VAT: 100}}},
	}
	refunds := []TotalsRefund{{PaymentID: "p1", Date: day, Method: "cash", Amount: 1270}}
	rows, total := Aggregate(docs, refunds, GroupDay)
	tot := &Totals{From: day, To: day, Group: GroupDay, Rows: rows, Total: total}
	csv := TotalsCSV(tot)
	if !bytes.HasPrefix(csv, []byte(bom)) || !strings.Contains(string(csv), "2027-07-03;22;;cash;1;90,00;19,80;109,80\r\n") ||
		!strings.Contains(string(csv), "Totale;;;;;;;125,30\r\n") {
		t.Fatalf("csv:\n%s", csv)
	}
	b, err := TotalsXLSX(tot)
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := xlsx.ReadFirstSheet(bytes.NewReader(b), int64(len(b)), xlsx.DefaultLimits)
	if err != nil || len(sheet) != len(rows)+2 {
		t.Fatalf("xlsx rows %d err %v", len(sheet), err)
	}
	if rate(450) != "4,5" || rate(2200) != "22" || rate(0) != "0" {
		t.Fatal("rate format")
	}
}

func TestDocumentLines_PartialSpread(t *testing.T) {
	lines := []store.ChargeLine{
		{Description: "Gasolio", QtyMilli: 20000, GrossCents: 3660, NetCents: 3000, VATRateBP: 2200},
		{Description: "Acqua", QtyMilli: 1000, GrossCents: 1100, NetCents: 1000, VATRateBP: 1000},
	}
	l, b := documentLines(lines, 4760, 4760)
	if len(l) != 2 || b[0].Gross != 3660 || b[1].Gross != 1100 {
		t.Fatalf("full %+v %+v", l, b)
	}
	_, b = documentLines(lines, 4760, 1000)
	var g money.Cents
	for _, x := range b {
		g += x.Gross
		if x.Net+x.VAT != x.Gross {
			t.Fatalf("bucket %+v", x)
		}
	}
	if g != 1000 {
		t.Fatalf("partial %d", g)
	}
}
