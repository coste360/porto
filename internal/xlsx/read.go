package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Limits bound what the reader accepts (m5-plan §11.2: ≤ 50 MiB uncompressed, ≤ 100 000 cells, zip-bomb ratio).
type Limits struct {
	MaxUncompressed int64 // total bytes inflated over the parts the reader opens
	MaxCells        int
	MaxRatio        int64 // inflated / compressed per part
	MaxParts        int
}

// DefaultLimits are the limits of the imports.
var DefaultLimits = Limits{MaxUncompressed: 50 << 20, MaxCells: 100_000, MaxRatio: 200, MaxParts: 2000}

// Errors of the reader.
var (
	ErrInvalidFile = errors.New("xlsx: invalid file")
	ErrTooLarge    = errors.New("xlsx: file too large")
)

// ReadFirstSheet returns the cells of the first worksheet as text rows (trailing empty cells trimmed, empty rows
// kept as empty slices up to the last non-empty row). Numbers are returned as stored (decimal text); cells with a
// date format are returned as YYYY-MM-DD.
func ReadFirstSheet(r io.ReaderAt, size int64, lim Limits) ([][]string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	if lim.MaxParts > 0 && len(zr.File) > lim.MaxParts {
		return nil, fmt.Errorf("%w: %d parts", ErrTooLarge, len(zr.File))
	}
	rd := &reader{zr: zr, lim: lim}
	sheetPath, err := rd.firstSheetPath()
	if err != nil {
		return nil, err
	}
	shared, err := rd.sharedStrings()
	if err != nil {
		return nil, err
	}
	dateStyles, err := rd.dateStyles()
	if err != nil {
		return nil, err
	}
	data, err := rd.part(sheetPath)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("%w: missing worksheet", ErrInvalidFile)
	}
	return rd.sheet(data, shared, dateStyles)
}

type reader struct {
	zr       *zip.Reader
	lim      Limits
	inflated int64
}

// part returns the inflated content of a part (nil when absent), enforcing the limits on the actual bytes.
func (rd *reader) part(name string) ([]byte, error) {
	name = strings.TrimPrefix(name, "/")
	for _, f := range rd.zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		budget := rd.lim.MaxUncompressed - rd.inflated
		if rd.lim.MaxRatio > 0 {
			comp := max(int64(f.CompressedSize64), 1)
			budget = min(budget, max(comp*rd.lim.MaxRatio, 64<<10))
		}
		b, err := io.ReadAll(io.LimitReader(rc, budget+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		if int64(len(b)) > budget {
			return nil, fmt.Errorf("%w: part %s", ErrTooLarge, name)
		}
		rd.inflated += int64(len(b))
		return b, nil
	}
	return nil, nil
}

type xRel struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

func (rd *reader) rels(name string) ([]xRel, error) {
	b, err := rd.part(name)
	if err != nil || b == nil {
		return nil, err
	}
	var v struct {
		Rels []xRel `xml:"Relationship"`
	}
	if err := xml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	return v.Rels, nil
}

// workbookPath follows _rels/.rels to the workbook part (xl/workbook.xml by default).
func (rd *reader) workbookPath() (string, error) {
	rels, err := rd.rels("_rels/.rels")
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if strings.HasSuffix(r.Type, "/officeDocument") {
			return strings.TrimPrefix(r.Target, "/"), nil
		}
	}
	return "xl/workbook.xml", nil
}

func (rd *reader) firstSheetPath() (string, error) {
	wb, err := rd.workbookPath()
	if err != nil {
		return "", err
	}
	b, err := rd.part(wb)
	if err != nil {
		return "", err
	}
	if b == nil {
		return "", fmt.Errorf("%w: missing workbook", ErrInvalidFile)
	}
	var w struct {
		Sheets []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(b, &w); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	if len(w.Sheets) == 0 {
		return "", fmt.Errorf("%w: no sheet", ErrInvalidFile)
	}
	dir := path.Dir(wb)
	rels, err := rd.rels(path.Join(dir, "_rels", path.Base(wb)+".rels"))
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if r.ID == w.Sheets[0].RID {
			if strings.HasPrefix(r.Target, "/") {
				return strings.TrimPrefix(r.Target, "/"), nil
			}
			return path.Join(dir, r.Target), nil
		}
	}
	return path.Join(dir, "worksheets/sheet1.xml"), nil
}

// richText is the text of a shared string or inline string (plain or rich runs).
type richText struct {
	T string `xml:"t"`
	R []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (t richText) String() string {
	if len(t.R) == 0 {
		return t.T
	}
	var b strings.Builder
	b.WriteString(t.T)
	for _, r := range t.R {
		b.WriteString(r.T)
	}
	return b.String()
}

func (rd *reader) sharedStrings() ([]string, error) {
	b, err := rd.part("xl/sharedStrings.xml")
	if err != nil || b == nil {
		return nil, err
	}
	var v struct {
		SI []richText `xml:"si"`
	}
	if err := xml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	if rd.lim.MaxCells > 0 && len(v.SI) > rd.lim.MaxCells {
		return nil, fmt.Errorf("%w: %d shared strings", ErrTooLarge, len(v.SI))
	}
	out := make([]string, len(v.SI))
	for i, s := range v.SI {
		out[i] = s.String()
	}
	return out, nil
}

// dateStyles returns which cellXfs indexes carry a date format (built-in 14..22, 45..47 or a custom format with
// day/month/year tokens).
func (rd *reader) dateStyles() (map[int]bool, error) {
	b, err := rd.part("xl/styles.xml")
	if err != nil || b == nil {
		return map[int]bool{}, err
	}
	var v struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			NumFmtID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := xml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	custom := map[int]bool{}
	for _, f := range v.NumFmts {
		custom[f.ID] = isDateFormat(f.Code)
	}
	out := map[int]bool{}
	for i, x := range v.Xfs {
		id := x.NumFmtID
		if (id >= 14 && id <= 22) || (id >= 45 && id <= 47) || custom[id] {
			out[i] = true
		}
	}
	return out, nil
}

func isDateFormat(code string) bool {
	var b strings.Builder
	inQuote := false
	for _, r := range strings.ToLower(code) {
		switch {
		case r == '"':
			inQuote = !inQuote
		case inQuote:
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if i := strings.IndexByte(s, '['); i >= 0 && strings.Contains(s[i:], "]") && !strings.Contains(s, "d") {
		return false
	}
	return strings.Contains(s, "d") && strings.Contains(s, "m") || strings.Contains(s, "yy")
}

// sheet streams the worksheet XML into rows.
func (rd *reader) sheet(data []byte, shared []string, dateStyles map[int]bool) ([][]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var rows [][]string
	cells := 0
	nextRow := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "row" {
			continue
		}
		var row struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				R  string   `xml:"r,attr"`
				T  string   `xml:"t,attr"`
				S  int      `xml:"s,attr"`
				V  *string  `xml:"v"`
				IS richText `xml:"is"`
			} `xml:"c"`
		}
		if err := dec.DecodeElement(&row, &se); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		idx := nextRow
		if row.R > 0 {
			idx = row.R - 1
		}
		if idx < nextRow || idx > rd.limitRows() {
			return nil, fmt.Errorf("%w: row %d", ErrInvalidFile, row.R)
		}
		for len(rows) < idx {
			rows = append(rows, nil)
		}
		var out []string
		col := 0
		for _, c := range row.Cells {
			cells++
			if rd.lim.MaxCells > 0 && cells > rd.lim.MaxCells {
				return nil, fmt.Errorf("%w: more than %d cells", ErrTooLarge, rd.lim.MaxCells)
			}
			if c.R != "" {
				ci, ok := colIndex(c.R)
				if !ok || ci < col || ci > 16383 {
					return nil, fmt.Errorf("%w: cell %q", ErrInvalidFile, c.R)
				}
				col = ci
			}
			val := ""
			switch c.T {
			case "inlineStr":
				val = c.IS.String()
			case "s":
				if c.V != nil {
					i, err := strconv.Atoi(strings.TrimSpace(*c.V))
					if err != nil || i < 0 || i >= len(shared) {
						return nil, fmt.Errorf("%w: shared string %q", ErrInvalidFile, *c.V)
					}
					val = shared[i]
				}
			case "b":
				if c.V != nil && strings.TrimSpace(*c.V) == "1" {
					val = "TRUE"
				} else if c.V != nil {
					val = "FALSE"
				}
			default: // n, str, e, or absent
				if c.V != nil {
					val = strings.TrimSpace(*c.V)
					if (c.T == "" || c.T == "n") && dateStyles[c.S] {
						if d, ok := serialToISO(val); ok {
							val = d
						}
					}
				}
			}
			if pad := col - len(out); pad > 0 {
				cells += pad // padding counts against the cell budget (bounded memory)
				if rd.lim.MaxCells > 0 && cells > rd.lim.MaxCells {
					return nil, fmt.Errorf("%w: more than %d cells", ErrTooLarge, rd.lim.MaxCells)
				}
				for len(out) < col {
					out = append(out, "")
				}
			}
			out = append(out, val)
			col++
		}
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		rows = append(rows, out)
		nextRow = idx + 1
	}
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	return rows, nil
}

func (rd *reader) limitRows() int {
	if rd.lim.MaxCells > 0 {
		return min(rd.lim.MaxCells, 1_048_576)
	}
	return 1_048_576
}

// colIndex parses the column of a cell reference ("AB12" → 27).
func colIndex(ref string) (int, bool) {
	n := 0
	i := 0
	for ; i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z'; i++ {
		n = n*26 + int(ref[i]-'A'+1)
		if n > 16384 {
			return 0, false
		}
	}
	if i == 0 {
		return 0, false
	}
	for j := i; j < len(ref); j++ {
		if ref[j] < '0' || ref[j] > '9' {
			return 0, false
		}
	}
	return n - 1, true
}

// serialToISO converts a date serial (integer part only) to YYYY-MM-DD.
func serialToISO(v string) (string, bool) {
	intPart, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(intPart)
	if err != nil || n < 1 || n > 2958465 {
		return "", false
	}
	return epoch.AddDays(n).String(), true
}
