package export

import (
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/money"
)

// Type is the type of a column; it decides how the integer of a cell is written (always Italian formats).
type Type int

// Column types (m7-plan §7).
const (
	TypeText  Type = iota // free text or an enumeration code
	TypeInt               // plain integer (count, nights, years, berth-nights)
	TypeCents             // money: 1234,56
	TypeBP                // basis points as a percent: 12,34
	TypeCm                // centimetres (or cm-nights) as metres: 12,50
	TypeCm2               // cm² (or cm²-nights) as m² with two decimals, half-even
	TypeDate              // GG/MM/AAAA
	TypeMonth             // YYYY-MM text as MM/AAAA
	TypeMilli             // thousandths: 0,500
)

// Column is one column of a table: Key is the JSON field name of the record (or of the totals), Label the
// header in the user's locale.
type Column struct {
	Key   string
	Label string
	Type  Type
}

type valueKind uint8

const (
	kindNull valueKind = iota
	kindText
	kindInt
	kindDate
)

// Value is one cell: null (empty cell: a hidden amount, an unpriced value, a field of another section), text,
// an integer interpreted by the column type, or a date.
type Value struct {
	kind valueKind
	s    string
	n    int64
	d    calendar.Date
}

// Null is an empty cell.
func Null() Value { return Value{} }

// Text is a text cell ("" is an empty cell).
func Text(s string) Value {
	if s == "" {
		return Value{}
	}
	return Value{kind: kindText, s: s}
}

// Int is an integer cell (cents, bp, cm, cm², milli or a plain count, according to the column).
func Int(n int64) Value { return Value{kind: kindInt, n: n} }

// Day is a date cell (a zero date is an empty cell).
func Day(d calendar.Date) Value {
	if d.IsZero() {
		return Value{}
	}
	return Value{kind: kindDate, d: d}
}

// Opt is an optional integer cell (empty when !OK).
func Opt[T ~int64](o calc.Opt[T]) Value {
	if !o.OK {
		return Value{}
	}
	return Int(int64(o.Value))
}

// BP is a ratio cell (empty when the denominator was zero).
func BP(b calc.BP) Value {
	if !b.OK {
		return Value{}
	}
	return Int(b.Value)
}

// IsNull reports an empty cell.
func (v Value) IsNull() bool { return v.kind == kindNull }

// Int64 returns the integer of an integer cell (0 otherwise) and whether the cell is an integer.
func (v Value) Int64() (int64, bool) { return v.n, v.kind == kindInt }

// String returns the text of a text cell, the ISO date of a date cell, the integer of an integer cell.
func (v Value) String() string {
	switch v.kind {
	case kindText:
		return v.s
	case kindInt:
		return strconv.FormatInt(v.n, 10)
	case kindDate:
		return v.d.String()
	}
	return ""
}

// Sheet is a table with a name (a summary sheet of the workbook).
type Sheet struct {
	Name    string
	Columns []Column
	Rows    [][]Value
}

// Table is a report as rows: Columns and Rows come from Result.Records in the JSON order, Total is the Totale
// row from Result.Totals (same length as Columns), Summaries are the extra workbook sheets (Riepilogo with the
// groups, then report-specific ones). CSV writes Columns, Rows and Total; XLSX writes them as sheet Dati followed
// by the Summaries.
type Table struct {
	Report    calc.ReportKey
	From, To  calendar.Date
	Section   string
	Columns   []Column
	Rows      [][]Value
	Total     []Value
	Summaries []Sheet
}

// Filename is the attachment name: porto-<key>-<from>-<to>.<ext>.
func (t Table) Filename(ext string) string {
	return "porto-" + string(t.Report) + "-" + t.From.String() + "-" + t.To.String() + "." + ext
}

// fixed writes n with scale implied decimals and the given separator ("-1234,56" for n=-123456, scale 2).
func fixed(n int64, scale int, sep byte) string {
	neg := n < 0
	u := uint64(n)
	if neg {
		u = -u
	}
	digits := strconv.FormatUint(u, 10)
	for len(digits) <= scale {
		digits = "0" + digits
	}
	cut := len(digits) - scale
	out := digits[:cut]
	if scale > 0 {
		out += string(sep) + digits[cut:]
	}
	if neg {
		return "-" + out
	}
	return out
}

// decimal returns the integer of a cell of type t as a decimal with the separator and its scale applied
// (cents and bp: 2 decimals; cm: metres; cm²: m² rounded half-even to 2 decimals; milli: 3 decimals).
func decimal(n int64, t Type, sep byte) string {
	switch t {
	case TypeCents, TypeBP, TypeCm:
		return fixed(n, 2, sep)
	case TypeCm2:
		return fixed(money.DivRoundHalfEven(n, 100), 2, sep)
	case TypeMilli:
		return fixed(n, 3, sep)
	default:
		return strconv.FormatInt(n, 10)
	}
}

// italianDate is GG/MM/AAAA.
func italianDate(d calendar.Date) string {
	return pad2(d.Day()) + "/" + pad2(int(d.Month())) + "/" + strconv.Itoa(d.Year())
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// italianMonth turns YYYY-MM into MM/AAAA (other text unchanged).
func italianMonth(s string) string {
	if len(s) == 7 && s[4] == '-' {
		return s[5:] + "/" + s[:4]
	}
	return s
}
