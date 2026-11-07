package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Kind is the type of a cell.
type Kind int

// Cell kinds.
const (
	KindString Kind = iota
	KindNumber
	KindDate
	KindEmpty
)

// Cell is one cell of a sheet. Numbers are decimal strings ("-12.34"), never float64.
type Cell struct {
	Kind Kind
	S    string        // KindString
	N    string        // KindNumber: decimal text
	D    calendar.Date // KindDate
}

// Str is a string cell.
func Str(s string) Cell { return Cell{Kind: KindString, S: s} }

// Int is an integer number cell.
func Int(n int64) Cell { return Cell{Kind: KindNumber, N: strconv.FormatInt(n, 10)} }

// Cents is a number cell with two decimals built from integer cents.
func Cents(c money.Cents) Cell { return Cell{Kind: KindNumber, N: c.String()} }

// Decimal is a number cell from decimal text; it must match -?digits(.digits)?.
func Decimal(s string) Cell { return Cell{Kind: KindNumber, N: s} }

// Date is a date cell (shown as GG/MM/AAAA); a zero date is an empty cell.
func Date(d calendar.Date) Cell {
	if d.IsZero() {
		return Cell{Kind: KindEmpty}
	}
	return Cell{Kind: KindDate, D: d}
}

// Empty is an empty cell.
func Empty() Cell { return Cell{Kind: KindEmpty} }

// Sheet is one worksheet: an optional bold header row, data rows and column widths (characters).
type Sheet struct {
	Name   string
	Header []string
	Widths []int
	Rows   [][]Cell
}

// Errors of the writer.
var (
	ErrInvalidSheet  = errors.New("xlsx: invalid sheet")
	ErrInvalidNumber = errors.New("xlsx: invalid number")
)

// epoch is day 0 of the 1900 date system as used by spreadsheets (1899-12-30, which absorbs the 1900 leap bug
// for every date after 1900-03-01).
var epoch = calendar.New(1899, time.December, 30)

// zipTime is the fixed modification time of every part, so the same sheets give the same bytes.
var zipTime = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// Write writes a workbook with the sheets (at least one) to w. The output is deterministic.
func Write(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		return fmt.Errorf("%w: no sheet", ErrInvalidSheet)
	}
	seen := map[string]bool{}
	for _, s := range sheets {
		if s.Name == "" || utf8.RuneCountInString(s.Name) > 31 || strings.ContainsAny(s.Name, `[]:*?/\`) || seen[strings.ToLower(s.Name)] {
			return fmt.Errorf("%w: name %q", ErrInvalidSheet, s.Name)
		}
		seen[strings.ToLower(s.Name)] = true
	}
	zw := zip.NewWriter(w)
	add := func(name, body string) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipTime})
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes(len(sheets))},
		{"_rels/.rels", rootRels},
		{"xl/workbook.xml", workbook(sheets)},
		{"xl/_rels/workbook.xml.rels", workbookRels(len(sheets))},
		{"xl/styles.xml", styles},
	}
	for _, p := range parts {
		if err := add(p.name, p.body); err != nil {
			return err
		}
	}
	for i, s := range sheets {
		body, err := sheetXML(s)
		if err != nil {
			return err
		}
		if err := add(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), body); err != nil {
			return err
		}
	}
	return zw.Close()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

// styles: 0 default, 1 bold header, 2 date dd/mm/yyyy, 3 number with two decimals.
const styles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="dd/mm/yyyy"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="4">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

func contentTypes(n int) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func workbook(sheets []Sheet) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, s := range sheets {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(s.Name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func workbookRels(n int) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1)
	b.WriteString(`</Relationships>`)
	return b.String()
}

func sheetXML(s Sheet) (string, error) {
	var b bytes.Buffer
	b.WriteString(xmlHeader)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if len(s.Header) > 0 {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	}
	if len(s.Widths) > 0 {
		b.WriteString(`<cols>`)
		for i, wd := range s.Widths {
			if wd <= 0 {
				wd = 10
			}
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, min(wd, 255))
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	row := 0
	if len(s.Header) > 0 {
		row++
		fmt.Fprintf(&b, `<row r="%d">`, row)
		for c, h := range s.Header {
			fmt.Fprintf(&b, `<c r="%s%d" t="inlineStr" s="1"><is><t xml:space="preserve">%s</t></is></c>`, ColName(c), row, escape(h))
		}
		b.WriteString(`</row>`)
	}
	for _, cells := range s.Rows {
		row++
		fmt.Fprintf(&b, `<row r="%d">`, row)
		for c, cell := range cells {
			ref := ColName(c) + strconv.Itoa(row)
			switch cell.Kind {
			case KindString:
				fmt.Fprintf(&b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escape(cell.S))
			case KindNumber:
				if !validDecimal(cell.N) {
					return "", fmt.Errorf("%w: %q", ErrInvalidNumber, cell.N)
				}
				fmt.Fprintf(&b, `<c r="%s" s="3"><v>%s</v></c>`, ref, cell.N)
			case KindDate:
				fmt.Fprintf(&b, `<c r="%s" s="2"><v>%d</v></c>`, ref, epoch.DaysUntil(cell.D))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String(), nil
}

// ColName returns the column letters of a 0-based column index (0 = A, 26 = AA).
func ColName(i int) string {
	var out []byte
	for i++; i > 0; i = (i - 1) / 26 {
		out = append([]byte{byte('A' + (i-1)%26)}, out...)
	}
	return string(out)
}

func validDecimal(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	intPart, frac, hasFrac := strings.Cut(s, ".")
	if intPart == "" || (hasFrac && frac == "") {
		return false
	}
	for _, r := range intPart + frac {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// escape escapes XML text and drops characters XML 1.0 cannot carry.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\r':
			b.WriteString("&#13;") // a raw CR would be normalised away by XML parsers
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || r == utf8.RuneError:
			// not representable in XML 1.0
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
