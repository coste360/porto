package export

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/coste360/porto/internal/xlsx"
)

// bom is the UTF-8 byte order mark: spreadsheets opened with an Italian locale need it to read UTF-8.
const bom = "\xef\xbb\xbf"

// WriteCSV writes t as Italian CSV: UTF-8 with BOM, ';' separator, CRLF, decimal comma, dates GG/MM/AAAA, text
// cells guarded against formula injection; the header is the column labels and the last row the Totale.
func WriteCSV(w io.Writer, t Table) error {
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString(bom); err != nil {
		return err
	}
	cw := csv.NewWriter(bw)
	cw.Comma = ';'
	cw.UseCRLF = true
	header := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		header[i] = guard(c.Label)
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	rec := make([]string, len(t.Columns))
	write := func(row []Value) error {
		for i, c := range t.Columns {
			rec[i] = ""
			if i < len(row) {
				rec[i] = csvCell(row[i], c.Type)
			}
		}
		return cw.Write(rec)
	}
	for _, row := range t.Rows {
		if err := write(row); err != nil {
			return err
		}
	}
	if len(t.Total) > 0 {
		if err := write(t.Total); err != nil {
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	return bw.Flush()
}

// csvCell formats one cell for the CSV.
func csvCell(v Value, t Type) string {
	switch v.kind {
	case kindInt:
		return decimal(v.n, t, ',')
	case kindDate:
		return italianDate(v.d)
	case kindText:
		if t == TypeMonth {
			return italianMonth(v.s)
		}
		return guard(v.s)
	}
	return ""
}

// guard prefixes with an apostrophe the text a spreadsheet would read as a formula (= + - @, tab, CR).
func guard(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// Sheet names of the workbook (always Italian, like the number formats).
const (
	SheetData    = "Dati"
	SheetSummary = "Riepilogo"
)

// WriteXLSX writes t as a workbook: sheet Dati (header, rows, Totale) then the summaries. Numbers are decimal
// cells built from the integers (never float), dates are date cells.
func WriteXLSX(w io.Writer, t Table) error {
	sheets := make([]xlsx.Sheet, 0, 1+len(t.Summaries))
	rows := t.Rows
	if len(t.Total) > 0 {
		rows = append(rows[:len(rows):len(rows)], t.Total)
	}
	data, err := xlsxSheet(SheetData, t.Columns, rows)
	if err != nil {
		return err
	}
	sheets = append(sheets, data)
	for _, s := range t.Summaries {
		sh, err := xlsxSheet(s.Name, s.Columns, s.Rows)
		if err != nil {
			return err
		}
		sheets = append(sheets, sh)
	}
	if err := xlsx.Write(w, sheets); err != nil {
		return fmt.Errorf("writing workbook: %w", err)
	}
	return nil
}

func xlsxSheet(name string, cols []Column, rows [][]Value) (xlsx.Sheet, error) {
	sh := xlsx.Sheet{Name: name, Header: make([]string, len(cols)), Widths: make([]int, len(cols))}
	for i, c := range cols {
		sh.Header[i] = c.Label
		sh.Widths[i] = max(utf8.RuneCountInString(c.Label)+2, minWidth(c.Type))
	}
	sh.Rows = make([][]xlsx.Cell, len(rows))
	for r, row := range rows {
		cells := make([]xlsx.Cell, len(cols))
		for i, c := range cols {
			cells[i] = xlsx.Empty()
			if i >= len(row) {
				continue
			}
			v := row[i]
			switch v.kind {
			case kindInt:
				cells[i] = xlsxNumber(v.n, c.Type)
			case kindDate:
				cells[i] = xlsx.Date(v.d)
			case kindText:
				s := v.s
				if c.Type == TypeMonth {
					s = italianMonth(s)
				}
				cells[i] = xlsx.Str(s)
				if n := utf8.RuneCountInString(s) + 2; n > sh.Widths[i] {
					sh.Widths[i] = min(n, 60)
				}
			}
		}
		sh.Rows[r] = cells
	}
	return sh, nil
}

func xlsxNumber(n int64, t Type) xlsx.Cell {
	s := decimal(n, t, '.')
	if !strings.Contains(s, ".") {
		return xlsx.Int(n)
	}
	return xlsx.Decimal(s)
}

func minWidth(t Type) int {
	switch t {
	case TypeDate:
		return 12
	case TypeCents:
		return 14
	case TypeText:
		return 12
	default:
		return 10
	}
}
