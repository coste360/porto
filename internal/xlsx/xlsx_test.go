package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"io"
	"os" //nolint:depguard // the test reads and rewrites its golden file (not the writer or the reader)
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

var update = flag.Bool("update", false, "rewrite the golden files of the xlsx tests")

func read(t interface{ Fatal(...any) }, b []byte) [][]string {
	rows, err := ReadFirstSheet(bytes.NewReader(b), int64(len(b)), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func genText(t *rapid.T, label string) string {
	return rapid.StringOfN(rapid.RuneFrom(nil, unicode.Latin, unicode.Greek, unicode.Han, unicode.Punct, unicode.Symbol,
		unicode.Space), 1, 40, -1).Filter(func(s string) bool {
		return strings.TrimSpace(s) != "" && s == strings.ToValidUTF8(s, "") && !strings.ContainsRune(s, 0xFFFD) &&
			!strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' && r != '\r' })
	}).Draw(t, label)
}

func TestXLSX_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ncols := rapid.IntRange(1, 30).Draw(t, "cols")
		nrows := rapid.IntRange(0, 40).Draw(t, "rows")
		header := make([]string, ncols)
		for i := range header {
			header[i] = genText(t, "header")
		}
		want := [][]string{header}
		s := Sheet{Name: "Righe", Header: header}
		for range nrows {
			var cells []Cell
			var texts []string
			for range rapid.IntRange(1, ncols).Draw(t, "n") {
				switch rapid.IntRange(0, 3).Draw(t, "kind") {
				case 0:
					v := genText(t, "s")
					cells, texts = append(cells, Str(v)), append(texts, v)
				case 1:
					c := money.Cents(rapid.Int64Range(-1e12, 1e12).Draw(t, "c"))
					cells, texts = append(cells, Cents(c)), append(texts, c.String())
				case 2:
					d := calendar.MustParse("1901-01-01").AddDays(rapid.IntRange(0, 70000).Draw(t, "d"))
					cells, texts = append(cells, Date(d)), append(texts, d.String())
				default:
					cells, texts = append(cells, Empty()), append(texts, "")
				}
			}
			for len(texts) > 0 && texts[len(texts)-1] == "" {
				texts = texts[:len(texts)-1]
			}
			s.Rows = append(s.Rows, cells)
			want = append(want, texts)
		}
		for len(want) > 0 && len(want[len(want)-1]) == 0 {
			want = want[:len(want)-1]
		}
		var buf bytes.Buffer
		if err := Write(&buf, []Sheet{s, {Name: "Altro", Rows: [][]Cell{{Str("x")}}}}); err != nil {
			t.Fatal(err)
		}
		got := read(t, buf.Bytes())
		for i := range want {
			if want[i] == nil {
				want[i] = []string{}
			}
		}
		for i := range got {
			if got[i] == nil {
				got[i] = []string{}
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip:\n got %q\nwant %q", got, want)
		}
		// Determinism: the same sheets give the same bytes.
		var again bytes.Buffer
		_ = Write(&again, []Sheet{s, {Name: "Altro", Rows: [][]Cell{{Str("x")}}}})
		if !bytes.Equal(again.Bytes(), buf.Bytes()) {
			t.Fatal("not deterministic")
		}
	})
}

func TestXLSX_OpensInSpreadsheetSchema(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []Sheet{
		{Name: "Righe", Header: []string{"Numero", "Data", "Importo"}, Widths: []int{14, 12, 12}, Rows: [][]Cell{
			{Str("A2027-000001"), Date(calendar.MustParse("2027-03-31")), Cents(122000)},
			{Str("a & <b>"), Empty(), Decimal("-0.5")},
		}},
		{Name: "Riepilogo IVA", Header: []string{"Aliquota"}, Rows: [][]Cell{{Int(22)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var out strings.Builder
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, f := range zr.File {
			if f.Name != n {
				continue
			}
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			out.WriteString("== " + n + "\n" + string(b) + "\n")
		}
	}
	golden := filepath.Join("testdata", "workbook.golden")
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(golden, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if out.String() != string(want) {
		t.Fatalf("workbook parts differ from %s (run with -update to accept)", golden)
	}
	for _, need := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if !strings.Contains(out.String(), "== "+need+"\n") {
			t.Fatalf("missing part %s", need)
		}
	}
	rows := read(t, buf.Bytes())
	if rows[1][1] != "2027-03-31" || rows[1][2] != "1220.00" || rows[2][0] != "a & <b>" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestXLSX_WriteRejects(t *testing.T) {
	for _, sheets := range [][]Sheet{
		nil,
		{{Name: ""}},
		{{Name: "a/b"}},
		{{Name: "x"}, {Name: "X"}},
		{{Name: "n", Rows: [][]Cell{{Decimal("1e5")}}}},
	} {
		if err := Write(io.Discard, sheets); err == nil {
			t.Fatalf("accepted %+v", sheets)
		}
	}
}

func TestXLSX_ReadSharedStringsAndGaps(t *testing.T) {
	parts := map[string]string{
		"[Content_Types].xml":        `<Types/>`,
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="S" sheetId="1" r:id="rId7"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId7" Type="x/worksheet" Target="worksheets/data.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Rossi</t></si><si><r><t>Ma</t></r><r><t>rio</t></r></si></sst>`,
		"xl/styles.xml":              `<styleSheet><numFmts><numFmt numFmtId="170" formatCode="d/m/yyyy;@"/></numFmts><cellXfs><xf numFmtId="0"/><xf numFmtId="170"/><xf numFmtId="14"/></cellXfs></styleSheet>`,
		"xl/worksheets/data.xml": `<worksheet><sheetData>` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c></row>` +
			`<row r="3"><c r="B3" s="1"><v>46477</v></c><c r="C3" s="2"><v>46477.5</v></c><c r="D3"><v>12.5</v></c><c r="E3" t="b"><v>1</v></c></row>` +
			`</sheetData></worksheet>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, b := range parts {
		f, _ := zw.Create(n)
		_, _ = io.WriteString(f, b)
	}
	_ = zw.Close()
	got := read(t, buf.Bytes())
	want := [][]string{{"Rossi", "", "Mario"}, nil, {"", "2027-03-31", "2027-03-31", "12.5", "TRUE"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestXLSX_ZipBombRejected(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("xl/worksheets/sheet1.xml")
	_, _ = f.Write([]byte(`<worksheet><sheetData><row><c t="inlineStr"><is><t>`))
	_, _ = f.Write(bytes.Repeat([]byte("a"), 20<<20))
	_, _ = f.Write([]byte(`</t></is></c></row></sheetData></worksheet>`))
	w2, _ := zw.Create("xl/workbook.xml")
	_, _ = io.WriteString(w2, `<workbook><sheets><sheet name="a" sheetId="1"/></sheets></workbook>`)
	_ = zw.Close()
	_, err := ReadFirstSheet(bytes.NewReader(buf.Bytes()), int64(buf.Len()), DefaultLimits)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestXLSX_TooManyCells(t *testing.T) {
	s := Sheet{Name: "a"}
	for range 300 {
		s.Rows = append(s.Rows, []Cell{Int(1), Int(2), Int(3), Int(4)})
	}
	var buf bytes.Buffer
	if err := Write(&buf, []Sheet{s}); err != nil {
		t.Fatal(err)
	}
	lim := DefaultLimits
	lim.MaxCells = 1000
	if _, err := ReadFirstSheet(bytes.NewReader(buf.Bytes()), int64(buf.Len()), lim); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestXLSX_ColName(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := ColName(i); got != want {
			t.Fatalf("ColName(%d) = %s, want %s", i, got, want)
		}
		if ci, ok := colIndex(want + "1"); !ok || ci != i {
			t.Fatalf("colIndex(%s) = %d", want, ci)
		}
	}
}

func FuzzXLSX_Read(f *testing.F) {
	var buf bytes.Buffer
	_ = Write(&buf, []Sheet{{Name: "a", Header: []string{"x"}, Rows: [][]Cell{{Str("y"), Cents(5), Date(calendar.MustParse("2027-01-01"))}}}})
	f.Add(buf.Bytes())
	f.Add([]byte("PK\x03\x04"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		lim := Limits{MaxUncompressed: 50 << 20, MaxCells: 100_000, MaxRatio: 200, MaxParts: 2000}
		rows, err := ReadFirstSheet(bytes.NewReader(b), int64(len(b)), lim)
		if err != nil {
			return
		}
		n := 0
		for _, r := range rows {
			n += len(r)
		}
		if n > lim.MaxCells {
			t.Fatalf("%d cells above the limit", n)
		}
	})
}
