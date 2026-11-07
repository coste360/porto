package charge

import (
	"archive/zip"
	"bytes"
	"flag"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

var update = flag.Bool("update", false, "rewrite the golden files of the export tests")

func d(s string) calendar.Date { return calendar.MustParse(s) }

// sampleCharges is a fixed export: an instalment with two rates, a walk-in counter sale with a commercial document
// and a negative adjustment of the instalment.
func sampleCharges() []ExportCharge {
	t0 := time.Date(2027, 7, 1, 9, 0, 0, 0, time.UTC)
	inst := store.Charge{
		ID: "01900000-0000-7000-8000-00000000000a", Number: "A2027-000001", OriginType: store.ChargeOriginInstallment,
		IssuedOn: d("2027-06-10"), DueOn: d("2027-07-10"), TotalCents: 13300, NetCents: 10900, VATCents: 2400,
		PaidAmountCents: 5000, PaymentStatus: store.ChargePartiallyPaid, CreatedAt: t0,
	}
	sale := store.Charge{
		ID: "01900000-0000-7000-8000-00000000000b", Number: "A2027-000002", OriginType: store.ChargeOriginCounterSale,
		IssuedOn: d("2027-06-11"), DueOn: d("2027-06-11"), TotalCents: 550, NetCents: 500, VATCents: 50,
		PaidAmountCents: 550, PaymentStatus: store.ChargePaid, FiscalMode: FiscalReceipt, CreatedAt: t0,
	}
	adj := store.Charge{
		ID: "01900000-0000-7000-8000-00000000000c", Number: "A2027-000003", OriginType: store.ChargeOriginAdjustment,
		IssuedOn: d("2027-06-12"), DueOn: d("2027-07-12"), TotalCents: -1220, NetCents: -1000, VATCents: -220,
		CreditedCents: -1220, PaymentStatus: store.ChargePaid, CorrectsChargeID: inst.ID, CreatedAt: t0,
	}
	fiscal := FiscalSnapshot{
		Kind: "person", Name: "Mario Rossi", FiscalCode: "RSSMRA80A01H501U", Address: FiscalAddress{
			Line1: "Via del Porto 1", PostalCode: "57100", City: "Livorno", Province: "LI", Country: "IT",
		},
	}
	return []ExportCharge{
		{Charge: adj, CorrectsNumber: inst.Number, ContractCode: "ORM-2027-0001", Fiscal: fiscal, Lines: []store.ChargeLine{
			{Seq: 1, ItemCode: "ORM-ANN", Description: "Ormeggio annuale", QtyMilli: -1000, UnitCents: 1220, NetCents: -1000, VATCents: -220, GrossCents: -1220, VATRateBP: 2200, PriceIncludesVAT: true, RevenueClassCode: "ormeggi"},
		}},
		{Charge: sale, FiscalDocument: "2027-06-11/0001", Lines: []store.ChargeLine{
			{Seq: 1, ItemCode: "ACQUA", Description: "Acqua; gettone", QtyMilli: 2500, UnitCents: 220, NetCents: 500, VATCents: 50, GrossCents: 550, VATRateBP: 1000, PriceIncludesVAT: true, RevenueClassCode: "servizi"},
		}},
		{Charge: inst, ContractCode: "ORM-2027-0001", InstallmentSeq: 2, VesselName: "Libeccio", VesselReg: "LI1234", Fiscal: fiscal, Lines: []store.ChargeLine{
			{Seq: 2, ItemCode: "SVC", Description: "=Servizio", QtyMilli: 1000, UnitCents: 1100, NetCents: 1000, VATCents: 100, GrossCents: 1100, VATRateBP: 1000, PriceIncludesVAT: true, RevenueClassCode: "servizi"},
			{Seq: 1, ItemCode: "ORM-ANN", Description: "Ormeggio annuale", QtyMilli: 333, UnitCents: 36636, NetCents: 9900, VATCents: 2300, GrossCents: 12200, VATRateBP: 2200, PriceIncludesVAT: true, RevenueClassCode: "ormeggi", ServiceFrom: d("2027-06-10"), ServiceTo: d("2028-06-09")},
		}},
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file (run with -update to accept)", name)
	}
}

func TestExportCSV_Golden(t *testing.T) {
	b, err := ExportCSV(ExportRows(sampleCharges()))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "export.csv.golden", b)
	text := string(b)
	if !strings.HasPrefix(text, "\ufeffNumero addebito;Data addebito;") || !strings.Contains(text, "\r\n") ||
		!strings.Contains(text, ";'=Servizio;") || !strings.Contains(text, ";99,00;22;;23,00;122,00;") {
		t.Fatalf("CSV format:\n%s", text)
	}
}

func TestExportXLSX_Golden(t *testing.T) {
	b, err := ExportXLSX(ExportRows(sampleCharges()))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		names = append(names, f.Name)
		parts[f.Name] = string(body)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, n := range names {
		if strings.HasPrefix(n, "xl/worksheets/") || n == "xl/workbook.xml" {
			out.WriteString("== " + n + "\n" + parts[n] + "\n")
		}
	}
	golden(t, "export.xlsx.golden", []byte(out.String()))
	if !strings.Contains(parts["xl/workbook.xml"], `name="Righe"`) || !strings.Contains(parts["xl/workbook.xml"], `name="Riepilogo IVA"`) ||
		!strings.Contains(parts["xl/workbook.xml"], `name="Addebiti"`) {
		t.Fatal("missing sheets")
	}
}

// TestExportRows_Deterministic: the rows depend only on the charges, never on their order.
func TestExportRows_Deterministic(t *testing.T) {
	base, err := ExportCSV(ExportRows(sampleCharges()))
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(51, 5))
	for range 50 {
		cs := sampleCharges()
		r.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
		for k := range cs {
			r.Shuffle(len(cs[k].Lines), func(i, j int) { cs[k].Lines[i], cs[k].Lines[j] = cs[k].Lines[j], cs[k].Lines[i] })
		}
		got, _ := ExportCSV(ExportRows(cs))
		if !bytes.Equal(got, base) {
			t.Fatal("rows depend on the input order")
		}
	}
	rows := ExportRows(sampleCharges())
	var keys []string
	for _, row := range rows {
		keys = append(keys, row.Charge.Charge.Number+"/"+string(rune('0'+row.Line.Seq)))
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("order %v", keys)
	}
}

// TestExportTotals_PerRate: totals per (rate, nature) of the lines, per origin of the charges.
func TestExportTotals_PerRate(t *testing.T) {
	var cs []store.Charge
	var ls []store.ChargeLine
	for _, c := range sampleCharges() {
		cs = append(cs, c.Charge)
		ls = append(ls, c.Lines...)
	}
	tt := TotalsOf(cs, ls)
	if tt.Gross != tt.ChargeTotals || tt.Gross != money.Cents(13300+550-1220) || tt.Net+tt.VAT != tt.Gross {
		t.Fatalf("totals %+v", tt)
	}
	if len(tt.ByVAT) != 2 || tt.ByVAT[0].RateBP != 2200 || tt.ByVAT[0].Gross != 10980 {
		t.Fatalf("by vat %+v", tt.ByVAT)
	}
}

func TestReconcileCSV_Parse(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []ReconcileRow
		err  bool
	}{
		{
			name: "semicolon with BOM and numbers",
			text: "\ufeffNumero addebito;Numero fattura;Data\r\nA2027-000001;FT/1;15/06/2027\r\n\r\na2027-000002;FT/2;2027-06-16\r\n",
			want: []ReconcileRow{{Row: 1, ChargeNumber: "A2027-000001", InvoiceNumber: "FT/1", InvoiceDate: "15/06/2027"}, {Row: 2, ChargeNumber: "A2027-000002", InvoiceNumber: "FT/2", InvoiceDate: "2027-06-16"}},
		},
		{
			name: "comma with ids",
			text: "id_addebito,numero fattura,data fattura\n01900000-0000-7000-8000-00000000000a,FT-9,1/7/2027\n",
			want: []ReconcileRow{{Row: 1, ChargeID: "01900000-0000-7000-8000-00000000000a", InvoiceNumber: "FT-9", InvoiceDate: "1/7/2027"}},
		},
		{name: "tab", text: "Id addebito\tNumero fattura\tData\nx\tF1\t01.07.2027\n", want: []ReconcileRow{{Row: 1, ChargeID: "x", InvoiceNumber: "F1", InvoiceDate: "01.07.2027"}}},
		{name: "missing column", text: "numero addebito;data\nA2027-000001;15/06/2027\n", err: true},
		{name: "empty", text: "", err: true},
		{name: "bad quotes", text: "numero addebito;numero fattura;data\n\"A2027;F;1/1/2027\n", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseReconcileCSV(tc.text)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if !tc.err && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	for in, want := range map[string]string{"15/06/2027": "2027-06-15", "1/7/2027": "2027-07-01", "2027-06-16": "2027-06-16", "01.07.2027": "2027-07-01"} {
		got, ok := ParseInvoiceDate(in)
		if !ok || got.String() != want {
			t.Fatalf("ParseInvoiceDate(%q) = %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "31/02/2027", "2027/06/01", "15-06-27"} {
		if _, ok := ParseInvoiceDate(bad); ok {
			t.Fatalf("ParseInvoiceDate(%q) accepted", bad)
		}
	}
}

func TestParseEuros(t *testing.T) {
	for in, want := range map[string]money.Cents{
		"1.234,56": 123456, "1234.56": 123456, "1,234.56": 123456, "-12,5": -1250, "€ 10": 1000, "0,005": 0, "0,015": 2,
		"-0,015": -2, "1.5E2": 15000, "12": 1200, "0.30000000000000004": 30,
	} {
		got, ok := ParseEuros(in)
		if !ok || got != want {
			t.Fatalf("ParseEuros(%q) = %d %v, want %d", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "1,2,3", "€"} {
		if _, ok := ParseEuros(bad); ok {
			t.Fatalf("ParseEuros(%q) accepted", bad)
		}
	}
}
