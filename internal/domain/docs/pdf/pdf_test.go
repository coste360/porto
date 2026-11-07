package pdf

import (
	"bytes"
	"encoding/binary"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/docs/layout"
	"github.com/coste360/porto/internal/domain/docs/markdown"
)

var at = time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC)

func sample(t *testing.T, src string) []byte {
	t.Helper()
	l := layout.Lay(layout.Input{Body: markdown.Parse(src), Footer: markdown.Parse("{{page}}/{{pages}}")})
	b, err := Draw(l, Meta{Title: "Contratto", Author: "Marina", Lang: "it", Instant: at})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const body = "# Contratto di ormeggio\n\nIl cliente **Mario Rossi** è titolare. Importo 1.234,56 € e quota 333 ‰.\n\n| A | B |\n|---|---|\n| x | y |\n\n{{pagebreak}}\n\nSeconda pagina."

func TestPDF_Deterministic(t *testing.T) {
	a, b := sample(t, body), sample(t, body)
	if !bytes.Equal(a, b) {
		t.Fatal("same layout and instant gave different bytes")
	}
	other := sample(t, body+" x")
	if bytes.Equal(a, other) {
		t.Fatal("different content gave the same bytes")
	}
	n, err := PageCount(a)
	if err != nil || n != 2 {
		t.Fatalf("pages %d, %v", n, err)
	}
	if err := Verify(a); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(a, []byte("/Producer")) {
		t.Fatal("no producer")
	}
}

func TestPDF_PDFAMarkers(t *testing.T) {
	raw := sample(t, body)
	fin, err := FinalizePDFA(raw, ArchiveMeta{Title: "Contratto <x>", Author: "Marina", Lang: "it", Instant: at})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(fin, raw) {
		t.Fatal("finalisation rewrote the original bytes")
	}
	if err := Verify(fin); err != nil {
		t.Fatal(err)
	}
	s := string(fin)
	for _, want := range []string{
		"<pdfaid:part>2</pdfaid:part>", "<pdfaid:conformance>B</pdfaid:conformance>", "/Type /Metadata /Subtype /XML",
		"/OutputIntents [", "/S /GTS_PDFA1", "/DestOutputProfile", "Contratto &lt;x&gt;", "/FontFile2",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("finalised PDF lacks %q", want)
		}
	}
	id := regexp.MustCompile(`/ID \[<([0-9A-F]{32})><([0-9A-F]{32})>\]`).FindStringSubmatch(s)
	if len(id) != 3 || id[1] != id[2] || id[1] != FileID(raw) {
		t.Fatalf("file id: %v", id)
	}
	// Never the 14 core fonts: every font is an embedded TrueType (Type0/CIDFontType2 of the Go fonts).
	for _, core := range []string{"/Helvetica", "/Times-Roman", "/Courier", "/BaseFont /Symbol", "/ZapfDingbats"} {
		if strings.Contains(s, core) {
			t.Fatalf("core font %s referenced", core)
		}
	}
	d, err := parse(fin)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := d.object(d.root)
	if _, ok := dictRef(cat.dict, "/Metadata"); !ok {
		t.Fatal("catalog revision without /Metadata")
	}
	if _, ok := dictRef(cat.dict, "/Pages"); !ok {
		t.Fatal("catalog revision lost /Pages")
	}
	if n, _ := PageCount(fin); n != 2 {
		t.Fatalf("pages after finalisation %d", n)
	}
	again, _ := FinalizePDFA(raw, ArchiveMeta{Title: "Contratto <x>", Author: "Marina", Lang: "it", Instant: at})
	if !bytes.Equal(fin, again) {
		t.Fatal("finalisation not deterministic")
	}
}

func TestPDF_AppendKeepsPrefix(t *testing.T) {
	raw := sample(t, body)
	fin, err := FinalizePDFA(raw, ArchiveMeta{Title: "Contratto", Instant: at})
	if err != nil {
		t.Fatal(err)
	}
	extra := sample(t, "# Pagina delle evidenze\n\nFirma elettronica semplice.")
	out, err := AppendPages(fin, extra)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, fin) {
		t.Fatal("original bytes are not a prefix")
	}
	if err := Verify(out); err != nil {
		t.Fatal(err)
	}
	if n, _ := PageCount(out); n != 3 {
		t.Fatalf("pages %d, want 3", n)
	}
	// Appending twice keeps the chain readable.
	out2, err := AppendPages(out, extra)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := PageCount(out2); n != 4 || !bytes.HasPrefix(out2, out) {
		t.Fatalf("second append: %d pages", n)
	}
	if err := Verify(out2); err != nil {
		t.Fatal(err)
	}
	d, _ := parse(out)
	if len(d.id) != 2 || d.id[0] != FileID(raw) {
		t.Fatalf("id after append: %v", d.id)
	}
}

func TestPDF_ICCProfileValid(t *testing.T) {
	p := SRGBProfile()
	if len(p) < 128 || len(p) > 1024 {
		t.Fatalf("size %d", len(p))
	}
	if int(binary.BigEndian.Uint32(p[0:4])) != len(p) {
		t.Fatal("declared size differs")
	}
	if string(p[12:16]) != "mntr" || string(p[16:20]) != "RGB " || string(p[20:24]) != "XYZ " || string(p[36:40]) != "acsp" {
		t.Fatal("header signatures")
	}
	if binary.BigEndian.Uint32(p[8:12]) != 0x04200000 {
		t.Fatal("version")
	}
	n := int(binary.BigEndian.Uint32(p[128:132]))
	want := map[string]bool{
		"desc": true, "cprt": true, "wtpt": true, "chad": true, "rXYZ": true, "gXYZ": true,
		"bXYZ": true, "rTRC": true, "gTRC": true, "bTRC": true,
	}
	for i := range n {
		e := p[132+12*i : 144+12*i]
		sig, off, size := string(e[0:4]), int(binary.BigEndian.Uint32(e[4:8])), int(binary.BigEndian.Uint32(e[8:12]))
		if off%4 != 0 || off+size > len(p) || off < 132+12*n {
			t.Fatalf("tag %s at %d+%d", sig, off, size)
		}
		delete(want, sig)
	}
	if len(want) != 0 {
		t.Fatalf("missing tags %v", want)
	}
	// The adapted primaries sum to the D50 white point (within s15.16 rounding).
	rgb, _ := srgbToD50()
	for i, w := range []float64{0.9642, 1.0, 0.8249} {
		s := rgb[i][0] + rgb[i][1] + rgb[i][2]
		if s-w > 1e-4 || w-s > 1e-4 {
			t.Fatalf("row %d sums %f, want %f", i, s, w)
		}
	}
	if !bytes.Equal(p, SRGBProfile()) {
		t.Fatal("profile not constant")
	}
}

func TestPDF_RejectsForeign(t *testing.T) {
	if _, err := FinalizePDFA([]byte("not a pdf"), ArchiveMeta{}); err == nil {
		t.Fatal("accepted garbage")
	}
	if _, err := AppendPages([]byte("%PDF-1.4\n%%EOF"), sample(t, "x")); err == nil {
		t.Fatal("accepted a pdf without xref")
	}
}
