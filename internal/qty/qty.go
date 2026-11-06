// Package qty implements fixed-point fractional quantities with three decimals (hours, metres, m², kWh,
// units). A Milli is the value × 1000; no float is ever used and every rounding is half-even.
//
// The package is pure.
package qty

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/money"
)

// Milli is a quantity in thousandths.
type Milli int64

// Scale is the number of Milli units in one.
const Scale = 1000

// One is the quantity 1.
const One Milli = Scale

var (
	// ErrInvalid reports a malformed quantity string.
	ErrInvalid = errors.New("qty: invalid quantity")
	// ErrTooPrecise reports a quantity with more than three decimals (never rounded silently).
	ErrTooPrecise = errors.New("qty: more than three decimals")
	// ErrOverflow reports a result that does not fit int64 milli.
	ErrOverflow = errors.New("qty: overflow")
	// ErrDivByZero reports a division by zero.
	ErrDivByZero = errors.New("qty: division by zero")
)

func abs64(v int64) uint64 {
	if v < 0 {
		return -uint64(v)
	}
	return uint64(v)
}

// fromMagnitude converts sign and magnitude to int64, or ErrOverflow.
func fromMagnitude(mag uint64, neg bool) (int64, error) {
	if neg {
		if mag > 1<<63 {
			return 0, ErrOverflow
		}
		return -int64(mag), nil //nolint:gosec // -2^63 wraps to itself, which is the intended value
	}
	if mag > math.MaxInt64 {
		return 0, ErrOverflow
	}
	return int64(mag), nil
}

// mulDivHalfEven returns a*b/d rounded half-even with a 128-bit intermediate.
func mulDivHalfEven(a, b, d int64) (int64, error) {
	if d == 0 {
		return 0, ErrDivByZero
	}
	neg := (a < 0) != (b < 0) != (d < 0)
	ua, ub, ud := abs64(a), abs64(b), abs64(d)
	hi, lo := bits.Mul64(ua, ub)
	if hi >= ud {
		return 0, ErrOverflow
	}
	q, r := bits.Div64(hi, lo, ud)
	if half := ud - r; r > half || (r == half && q%2 == 1) {
		if q == math.MaxUint64 {
			return 0, ErrOverflow
		}
		q++
	}
	if q == 0 {
		return 0, nil
	}
	return fromMagnitude(q, neg)
}

// FromInt returns n as a quantity.
func FromInt(n int64) (Milli, error) {
	v, err := mulDivHalfEven(n, Scale, 1)
	return Milli(v), err
}

// FromFraction returns num/den rounded half-even to the milli: FromFraction(90, 60) = 1.500 (minutes to hours).
func FromFraction(num, den int64) (Milli, error) {
	v, err := mulDivHalfEven(num, Scale, den)
	return Milli(v), err
}

// Parse parses a decimal quantity: surrounding spaces, an optional sign, digits, and optionally one '.' or ','
// followed by 1 to 3 digits. More than three decimals is ErrTooPrecise; thousands separators and exponents are
// ErrInvalid; values outside int64 milli are ErrOverflow.
func Parse(s string) (Milli, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%w: empty", ErrInvalid)
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexAny(s, ".,"); i >= 0 {
		intPart, frac = s[:i], s[i+1:]
		if frac == "" {
			return 0, fmt.Errorf("%w: separator without decimals", ErrInvalid)
		}
	}
	if intPart == "" || !digits(intPart) || !digits(frac) {
		return 0, ErrInvalid
	}
	if len(frac) > 3 {
		return 0, ErrTooPrecise
	}
	frac += strings.Repeat("0", 3-len(frac))
	units, err := strconv.ParseUint(intPart, 10, 64)
	if err != nil {
		return 0, ErrOverflow
	}
	f, _ := strconv.ParseUint(frac, 10, 64) // three validated digits
	hi, mag := bits.Mul64(units, Scale)
	mag, carry := bits.Add64(mag, f, 0)
	if hi != 0 || carry != 0 {
		return 0, ErrOverflow
	}
	v, err := fromMagnitude(mag, neg)
	return Milli(v), err
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// MustParse is Parse for constants and tests; it panics on error.
func MustParse(s string) Milli {
	m, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return m
}

// format renders m with exactly three decimals, then trims trailing digits to `decimals` (m must already be
// rounded to that precision).
func format(m Milli, decimals int, sep byte, trim bool) string {
	neg := m < 0
	u := abs64(int64(m))
	ip, fp := u/Scale, u%Scale
	var b strings.Builder
	if neg && u != 0 {
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatUint(ip, 10))
	f := fmt.Sprintf("%03d", fp)[:decimals]
	if trim {
		f = strings.TrimRight(f, "0")
	}
	if f != "" {
		b.WriteByte(sep)
		b.WriteString(f)
	}
	return b.String()
}

// String returns the canonical form: '.' separator, trailing zeros trimmed ("12", "12.5", "-0.125").
func (m Milli) String() string { return format(m, 3, '.', true) }

// Format returns m with exactly `decimals` decimals (clamped to 0..3), rounded half-even, using sep as the
// decimal separator: MustParse("12.5").Format(2, ',') == "12,50".
func (m Milli) Format(decimals int, sep byte) string {
	decimals = min(max(decimals, 0), 3)
	return format(m.Round(decimals), decimals, sep, false)
}

var pow10 = [4]int64{1000, 100, 10, 1} // Milli units per step at 0..3 decimals

// Round rounds m half-even to `decimals` decimals (clamped to 0..3). At the extremes of the int64 range,
// where rounding away from zero would overflow, it rounds towards zero instead.
func (m Milli) Round(decimals int) Milli {
	decimals = min(max(decimals, 0), 3)
	step := pow10[decimals]
	if step == 1 {
		return m
	}
	q, err := mulDivHalfEven(int64(m), 1, step)
	if err == nil {
		if v, err := mulDivHalfEven(q, step, 1); err == nil {
			return Milli(v)
		}
	}
	return m - m%Milli(step)
}

// Add returns m+o, or ErrOverflow.
func (m Milli) Add(o Milli) (Milli, error) {
	s := m + o
	if (o > 0 && s < m) || (o < 0 && s > m) {
		return 0, ErrOverflow
	}
	return s, nil
}

// Sub returns m-o, or ErrOverflow.
func (m Milli) Sub(o Milli) (Milli, error) {
	d := m - o
	if (o > 0 && d > m) || (o < 0 && d < m) {
		return 0, ErrOverflow
	}
	return d, nil
}

// Mul returns m×o rounded half-even to the milli: 1.5 × 1.5 = 2.25.
func (m Milli) Mul(o Milli) (Milli, error) {
	v, err := mulDivHalfEven(int64(m), int64(o), Scale)
	return Milli(v), err
}

// Div returns m÷o rounded half-even to the milli: 1 ÷ 3 = 0.333.
func (m Milli) Div(o Milli) (Milli, error) {
	v, err := mulDivHalfEven(int64(m), Scale, int64(o))
	return Milli(v), err
}

// MulInt returns m×n, or ErrOverflow.
func (m Milli) MulInt(n int64) (Milli, error) {
	v, err := mulDivHalfEven(int64(m), n, 1)
	return Milli(v), err
}

// DivInt returns m÷n rounded half-even to the milli.
func (m Milli) DivInt(n int64) (Milli, error) {
	v, err := mulDivHalfEven(int64(m), 1, n)
	return Milli(v), err
}

// MulCents returns the amount for quantity m at unit price c, rounded half-even to the cent:
// 3.5 h × 40.00 € = 140.00 €.
func (m Milli) MulCents(c money.Cents) (money.Cents, error) {
	v, err := mulDivHalfEven(int64(m), int64(c), Scale)
	if errors.Is(err, ErrOverflow) {
		return 0, fmt.Errorf("%w: %w", money.ErrOverflow, err)
	}
	return money.Cents(v), err
}

// Sign returns -1, 0 or +1.
func (m Milli) Sign() int {
	switch {
	case m < 0:
		return -1
	case m > 0:
		return 1
	default:
		return 0
	}
}

// IsZero reports whether m is zero.
func (m Milli) IsZero() bool { return m == 0 }

// Cmp returns -1, 0 or +1 as m is less than, equal to or greater than o.
func (m Milli) Cmp(o Milli) int {
	switch {
	case m < o:
		return -1
	case m > o:
		return 1
	default:
		return 0
	}
}

// MarshalJSON encodes m as an integer number of milli units (the API representation).
func (m Milli) MarshalJSON() ([]byte, error) { return strconv.AppendInt(nil, int64(m), 10), nil }

// UnmarshalJSON accepts an integer number of milli units.
func (m *Milli) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: JSON quantity must be an integer of milli units", ErrInvalid)
	}
	*m = Milli(v)
	return nil
}

// MarshalText returns the canonical String form (CSV exports, query parameters, map keys).
func (m Milli) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

// UnmarshalText parses the decimal form with Parse.
func (m *Milli) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// Value implements driver.Valuer: an INTEGER column of milli units.
func (m Milli) Value() (driver.Value, error) { return int64(m), nil }

// Scan implements sql.Scanner for INTEGER columns.
func (m *Milli) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*m = Milli(v)
	case []byte:
		return m.scanString(string(v))
	case string:
		return m.scanString(v)
	default:
		return fmt.Errorf("%w: cannot scan %T", ErrInvalid, src)
	}
	return nil
}

func (m *Milli) scanString(s string) error {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: cannot scan %q", ErrInvalid, s)
	}
	*m = Milli(v)
	return nil
}
