package layout

import (
	"errors"
	"math/big"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/geo"
)

// Georeferencing errors.
var (
	ErrCollinear  = errors.New("layout: georeference control points are collinear")
	ErrBadDecimal = errors.New("layout: invalid decimal coordinate")
)

// E7 is the scale of WGS84 coordinates (degrees × 10⁷, as marinas.geo).
const E7 = 10_000_000

// ControlPoint ties a map point (cm) to a WGS84 position (degrees × 10⁷).
type ControlPoint struct {
	At           geo.Point
	LatE7, LonE7 int64
}

// Georef is the affine transform defined by three control points (spec 7.1): lat = a·x + b·y + c and
// lon = d·x + e·y + f, computed exactly with rationals.
type Georef struct {
	a, b, c, d, e, f *big.Rat
	det              *big.Rat // a·e − b·d
}

func ratInt(v int64) *big.Rat { return new(big.Rat).SetInt64(v) }

// NewGeoref solves the affine transform. ErrCollinear when the map points are collinear or the geographic
// points are degenerate.
func NewGeoref(cps [3]ControlPoint) (Georef, error) {
	p1, p2, p3 := cps[0].At, cps[1].At, cps[2].At
	det := geo.Cross(p1, p2, p3) // det of [[x,y,1]...] = (x2−x1)(y3−y1) − (y2−y1)(x3−x1)
	if det == 0 {
		return Georef{}, ErrCollinear
	}
	solve := func(v1, v2, v3 int64) (a, b, c *big.Rat) {
		// Unknowns of v = a·x + b·y + c, by Cramer's rule on differences from point 1.
		dx2, dy2 := ratInt(int64(p2.X-p1.X)), ratInt(int64(p2.Y-p1.Y))
		dx3, dy3 := ratInt(int64(p3.X-p1.X)), ratInt(int64(p3.Y-p1.Y))
		dv2, dv3 := ratInt(v2-v1), ratInt(v3-v1)
		dd := ratInt(det)
		// a = (dv2·dy3 − dv3·dy2)/det, b = (dx2·dv3 − dx3·dv2)/det
		a = new(big.Rat).Sub(new(big.Rat).Mul(dv2, dy3), new(big.Rat).Mul(dv3, dy2))
		a.Quo(a, dd)
		b = new(big.Rat).Sub(new(big.Rat).Mul(dx2, dv3), new(big.Rat).Mul(dx3, dv2))
		b.Quo(b, dd)
		c = ratInt(v1)
		c.Sub(c, new(big.Rat).Mul(a, ratInt(int64(p1.X))))
		c.Sub(c, new(big.Rat).Mul(b, ratInt(int64(p1.Y))))
		return a, b, c
	}
	g := Georef{}
	g.a, g.b, g.c = solve(cps[0].LatE7, cps[1].LatE7, cps[2].LatE7)
	g.d, g.e, g.f = solve(cps[0].LonE7, cps[1].LonE7, cps[2].LonE7)
	g.det = new(big.Rat).Sub(new(big.Rat).Mul(g.a, g.e), new(big.Rat).Mul(g.b, g.d))
	if g.det.Sign() == 0 {
		return Georef{}, ErrCollinear
	}
	return g, nil
}

// roundRat rounds a rational half-even to an int64. It is the big.Rat counterpart of geo.DivRoundHalfEven,
// kept separate because the georeference affine transform works on exact rationals whose numerator and
// denominator do not fit in int64; TestRoundRat_MatchesDivRoundHalfEven keeps the two rules identical.
func roundRat(r *big.Rat) int64 {
	num, den := new(big.Int).Set(r.Num()), r.Denom()
	neg := num.Sign() < 0
	num.Abs(num)
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	twice := new(big.Int).Lsh(rem, 1)
	switch twice.Cmp(den) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}

func affine(a, b, c *big.Rat, x, y int64) *big.Rat {
	v := new(big.Rat).Mul(a, ratInt(x))
	v.Add(v, new(big.Rat).Mul(b, ratInt(y)))
	return v.Add(v, c)
}

// ToWGS84 converts a map point to WGS84 × 10⁷, rounded half-even.
func (g Georef) ToWGS84(p geo.Point) (latE7, lonE7 int64) {
	return roundRat(affine(g.a, g.b, g.c, int64(p.X), int64(p.Y))), roundRat(affine(g.d, g.e, g.f, int64(p.X), int64(p.Y)))
}

// FromWGS84 converts a WGS84 position to map centimetres, rounded half-even.
func (g Georef) FromWGS84(latE7, lonE7 int64) geo.Point {
	dl := new(big.Rat).Sub(ratInt(latE7), g.c)
	dn := new(big.Rat).Sub(ratInt(lonE7), g.f)
	x := new(big.Rat).Sub(new(big.Rat).Mul(g.e, dl), new(big.Rat).Mul(g.b, dn))
	x.Quo(x, g.det)
	y := new(big.Rat).Sub(new(big.Rat).Mul(g.a, dn), new(big.Rat).Mul(g.d, dl))
	y.Quo(y, g.det)
	return geo.Point{X: geo.Cm(roundRat(x)), Y: geo.Cm(roundRat(y))}
}

// FormatE7 writes degrees × 10⁷ as an exact decimal with 7 digits, e.g. 123456789 → "12.3456789".
func FormatE7(v int64) string {
	sign := ""
	u := abs64(v)
	if v < 0 {
		sign = "-"
	}
	frac := strconv.FormatUint(u%E7, 10)
	return sign + strconv.FormatUint(u/E7, 10) + "." + strings.Repeat("0", 7-len(frac)) + frac
}

// ParseE7 parses a decimal number of degrees (plain or with an exponent, as JSON numbers) into degrees × 10⁷,
// rounding half-even beyond the seventh decimal. No floating point is involved.
func ParseE7(s string) (int64, error) {
	if s == "" || len(s) > 64 || strings.ContainsAny(s, "/ ") {
		return 0, ErrBadDecimal
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, ErrBadDecimal
	}
	r.Mul(r, ratInt(E7))
	if r.Cmp(ratInt(180*E7)) > 0 || r.Cmp(ratInt(-180*E7)) < 0 {
		return 0, ErrBadDecimal
	}
	return roundRat(r), nil
}
