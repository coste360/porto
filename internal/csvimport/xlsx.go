package csvimport

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/coste360/porto/internal/xlsx"
)

// Excel input of the imports (m5-plan §3.8, §16 D18): the first sheet of an .xlsx workbook is read into the same
// Table as a CSV, so every M4 entity handler accepts Excel unchanged. A workbook travels in the csv field of the
// import input as a data URL (XLSXDataURLPrefix + base64), since the import API carries text.

// XLSXDataURLPrefix marks an Excel workbook in the csv field of an import.
const XLSXDataURLPrefix = "data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,"

// MaxXLSXBytes is the largest workbook accepted (compressed).
const MaxXLSXBytes = 6 << 20

// xlsxPayload returns the workbook of a data URL text.
func xlsxPayload(text string) ([]byte, bool, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), XLSXDataURLPrefix)
	if !ok {
		return nil, false, nil
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return nil, true, fmt.Errorf("%w: invalid base64", ErrCSVInvalid)
	}
	return b, true, nil
}

// ParseXLSX reads the first sheet of a workbook into a Table (at most MaxRows data rows, bounded reader).
func ParseXLSX(b []byte, hasHeader bool) (*Table, error) {
	if len(b) > MaxXLSXBytes {
		return nil, fmt.Errorf("%w: workbook too large", ErrCSVInvalid)
	}
	rows, err := xlsx.ReadFirstSheet(bytes.NewReader(b), int64(len(b)), xlsx.DefaultLimits)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCSVInvalid, err)
	}
	t := &Table{HasHeader: hasHeader}
	for _, rec := range rows {
		if blank(rec) {
			continue
		}
		if hasHeader && t.Header == nil {
			t.Header = rec
			continue
		}
		t.Rows = append(t.Rows, rec)
		if len(t.Rows) > MaxRows {
			return nil, fmt.Errorf("%w: more than %d rows", ErrCSVInvalid, MaxRows)
		}
	}
	return t, nil
}
