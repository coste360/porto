// Package money implements integer money arithmetic in euro cents.
//
// Amounts are int64 cents. No float is ever used: multiplications by ratios go through a 128-bit intermediate
// and are rounded half-even (banker's rounding).
package money

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

// Cents is an amount of money in hundredths of the currency unit.
type Cents int64

var (
	// ErrInvalidAmount reports a string that is not a valid amount.
	ErrInvalidAmount = errors.New("money: invalid amount")
	// ErrOverflow reports a result that does not fit int64 cents.
	ErrOverflow = errors.New("money: overflow")
	// ErrInvalidWeights reports an allocation with no weights, a negative weight or an all-zero weight vector.
	ErrInvalidWeights = errors.New("money: invalid allocation weights")
)

// String returns the canonical representation: optional '-', integer part, '.', two decimals ("-12.34", "0.05").
func (c Cents) String() string {
	neg := c < 0
	u := uint64(c)
	if neg {
		u = -u // two's complement magnitude, correct for math.MinInt64 too
	}
	s := fmt.Sprintf("%d.%02d", u/100, u%100)
	if neg {
		return "-" + s
	}
	return s
}

// ParseCents parses a decimal amount with '.' or ',' as decimal separator and at most two decimals.
// Accepted: "12", "12.3", "12,30", "-0.05", "+4". Thousands separators and exponents are rejected.
func ParseCents(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrInvalidAmount
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
			return 0, ErrInvalidAmount
		}
	}
	if intPart == "" || !allDigits(intPart) || !allDigits(frac) {
		return 0, ErrInvalidAmount
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("%w: more than two decimals", ErrInvalidAmount)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	units, err := strconv.ParseUint(intPart, 10, 64)
	if err != nil {
		return 0, ErrOverflow
	}
	f, _ := strconv.ParseUint(frac, 10, 64) // two validated digits
	hi, mag := bits.Mul64(units, 100)
	mag, carry := bits.Add64(mag, f, 0)
	if hi != 0 || carry != 0 {
		return 0, ErrOverflow
	}
	return fromMagnitude(mag, neg)
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// fromMagnitude turns a sign and magnitude into Cents, reporting ErrOverflow when it does not fit.
func fromMagnitude(mag uint64, neg bool) (Cents, error) {
	if neg {
		if mag > 1<<63 {
			return 0, ErrOverflow
		}
		return Cents(-mag), nil //nolint:gosec // range checked above
	}
	if mag > math.MaxInt64 {
		return 0, ErrOverflow
	}
	return Cents(mag), nil
}

func abs64(v int64) uint64 {
	if v < 0 {
		return -uint64(v)
	}
	return uint64(v)
}

// mulDivHalfEven returns a*b/d rounded half-even using a 128-bit intermediate. d must not be zero.
func mulDivHalfEven(a, b, d int64) (int64, error) {
	if d == 0 {
		panic("money: division by zero")
	}
	neg := (a < 0) != (b < 0) != (d < 0)
	ua, ub, ud := abs64(a), abs64(b), abs64(d)
	hi, lo := bits.Mul64(ua, ub)
	if hi >= ud {
		return 0, ErrOverflow
	}
	q, r := bits.Div64(hi, lo, ud)
	if r > ud-r || (r == ud-r && q%2 == 1) {
		if q == math.MaxUint64 {
			return 0, ErrOverflow
		}
		q++
	}
	if q == 0 {
		return 0, nil
	}
	c, err := fromMagnitude(q, neg)
	return int64(c), err
}

// DivRoundHalfEven returns num/den rounded half-even. It panics when den is zero or the result overflows
// (only MinInt64 / -1): both are programming errors.
func DivRoundHalfEven(num, den int64) int64 {
	q, err := mulDivHalfEven(num, 1, den)
	if err != nil {
		panic(err)
	}
	return q
}

// MulRatChecked returns c*num/den rounded half-even, or ErrOverflow.
func (c Cents) MulRatChecked(num, den int64) (Cents, error) {
	q, err := mulDivHalfEven(int64(c), num, den)
	return Cents(q), err
}

// MulRat returns c*num/den rounded half-even with a 128-bit intermediate. It panics when den is zero or the
// result does not fit int64 (use MulRatChecked for untrusted factors).
func (c Cents) MulRat(num, den int64) Cents {
	q, err := c.MulRatChecked(num, den)
	if err != nil {
		panic(err)
	}
	return q
}

// PercentBP returns c × bp / 10000 (bp in basis points: 2200 = 22%), rounded half-even.
func (c Cents) PercentBP(bp int64) Cents { return c.MulRat(bp, 10_000) }

// VATSplit splits a VAT-inclusive amount into net and VAT at rateBP basis points (2200 = 22%).
// net is rounded half-even and vat = gross - net, so net+vat == gross always. rateBP must be >= 0.
func VATSplit(gross Cents, rateBP int64) (net, vat Cents) {
	if rateBP < 0 {
		panic("money: negative VAT rate")
	}
	net = gross.MulRat(10_000, 10_000+rateBP)
	return net, gross - net
}

// Allocate splits total proportionally to weights with the largest remainder method: the result has one share
// per weight and sums to total exactly. Ties on the remainder go to the lower index. A negative total is
// allocated as its magnitude and negated, so shares keep the sign of the total.
func Allocate(total Cents, weights []int64) ([]Cents, error) {
	if len(weights) == 0 {
		return nil, ErrInvalidWeights
	}
	var sum uint64
	for _, w := range weights {
		if w < 0 {
			return nil, ErrInvalidWeights
		}
		var carry uint64
		sum, carry = bits.Add64(sum, uint64(w), 0)
		if carry != 0 || sum > math.MaxInt64 {
			return nil, ErrOverflow
		}
	}
	if sum == 0 {
		return nil, ErrInvalidWeights
	}
	neg := total < 0
	mag := abs64(int64(total))
	shares := make([]uint64, len(weights))
	rems := make([]uint64, len(weights))
	var assigned uint64
	for i, w := range weights {
		hi, lo := bits.Mul64(mag, uint64(w))
		shares[i], rems[i] = bits.Div64(hi, lo, sum) // hi < sum because w <= sum
		assigned += shares[i]
	}
	left := mag - assigned // < len(weights)
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	// Stable selection of the `left` largest remainders (n is small: weights are lines, holders, units).
	for k := uint64(0); k < left; k++ {
		best := -1
		for _, i := range order {
			if i < 0 {
				continue
			}
			if best < 0 || rems[i] > rems[best] {
				best = i
			}
		}
		shares[best]++
		order[best] = -1
	}
	out := make([]Cents, len(weights))
	for i, s := range shares {
		v, err := fromMagnitude(s, neg)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
