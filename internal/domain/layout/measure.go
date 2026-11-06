package layout

import (
	"errors"
	"math/bits"

	"github.com/coste360/porto/internal/geo"
)

// ErrOutOfRange is returned by PointAt for a distance outside the polyline.
var ErrOutOfRange = errors.New("layout: distance outside the polyline")

// ceilDiv returns ⌈a/b⌉ for b > 0.
func ceilDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a > 0) == (b > 0) {
		q++
	}
	return q
}

// floorDiv returns ⌊a/b⌋ for b > 0.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// ISqrt returns ⌊√n⌋.
func ISqrt(n uint64) uint64 {
	if n < 2 {
		return n
	}
	// Newton iteration from an upper bound.
	x := uint64(1) << ((bits.Len64(n) + 1) / 2)
	for {
		y := (x + n/x) / 2
		if y >= x {
			return x
		}
		x = y
	}
}

// segLen returns the length of the segment a→b rounded half-even to the centimetre.
func segLen(a, b geo.Point) geo.Cm {
	d2 := uint64(geo.Dist2(a, b)) //nolint:gosec // squares are non-negative
	r := ISqrt(4 * d2)            // ⌊2d⌋
	k := r / 2
	if r%2 == 1 {
		if r*r == 4*d2 { // exact half: d = k + ½
			if k%2 == 1 {
				k++
			}
		} else {
			k++
		}
	}
	return geo.Cm(k) //nolint:gosec // bounded by MaxCoord
}

// PolylineLength returns the length of a polyline: the sum of its segment lengths, each rounded half-even.
func PolylineLength(pts []geo.Point) geo.Cm {
	var total geo.Cm
	for i := 1; i < len(pts); i++ {
		total += segLen(pts[i-1], pts[i])
	}
	return total
}

// PointAt returns the point at distance s along the polyline and the direction of the segment it lies on. The
// end of a segment belongs to the next segment, except for the last point.
func PointAt(pts []geo.Point, s geo.Cm) (geo.Point, geo.Angle, error) {
	if len(pts) < 2 || s < 0 {
		return geo.Point{}, 0, ErrOutOfRange
	}
	var acc geo.Cm
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		l := segLen(a, b)
		if l == 0 {
			continue
		}
		if s < acc+l || (i == len(pts)-1 && s == acc+l) {
			t := int64(s - acc)
			p := geo.Point{
				X: a.X + geo.Cm(geo.DivRoundHalfEven(int64(b.X-a.X)*t, int64(l))),
				Y: a.Y + geo.Cm(geo.DivRoundHalfEven(int64(b.Y-a.Y)*t, int64(l))),
			}
			return p, AngleOf(int64(b.X-a.X), int64(b.Y-a.Y)), nil
		}
		acc += l
	}
	return geo.Point{}, 0, ErrOutOfRange
}

// crossDir returns cross(direction(a), v) × TrigScale scale: positive when v lies clockwise on screen (greater
// angle) from direction a.
func crossDir(a geo.Angle, dx, dy int64) int64 {
	return geo.Cos(a)*dy - geo.Sin(a)*dx
}

// AngleOf returns the direction of the vector (dx, dy) to the nearest tenth of a degree (ties to even). It uses
// only the trigonometric table and cross products. |dx|, |dy| must not exceed 4·10⁹. The zero vector is 0.
func AngleOf(dx, dy int64) geo.Angle {
	if dx == 0 && dy == 0 {
		return 0
	}
	var q int64
	switch {
	case dx > 0 && dy >= 0:
		q = 0
	case dx <= 0 && dy > 0:
		q = 1
	case dx < 0 && dy <= 0:
		q = 2
	default:
		q = 3
	}
	lo, hi := q*900, q*900+900 // the angle is in [lo, hi)
	// Largest a in [lo, hi) with crossDir(a, v) >= 0 (v not before a).
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if crossDir(geo.Angle(mid), dx, dy) >= 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	a := lo
	before := crossDir(geo.Angle(a), dx, dy)      // ∝ sin(θ − a) ≥ 0
	after := -crossDir(geo.NewAngle(a+1), dx, dy) // ∝ sin(a+1 − θ) > 0
	if after < before || (after == before && a%2 == 1) {
		a++
	}
	return geo.NewAngle(a)
}

// offset returns origin + (along·u + across·left)/2, rounded once half-even, where u is the direction of rot and
// left is u rotated by −900. along and across are in half centimetres, so that odd widths stay exact.
func offset(origin geo.Point, rot geo.Angle, along2, across2 int64) geo.Point {
	ux, uy := geo.Cos(rot), geo.Sin(rot)
	lx, ly := uy, -ux
	return geo.Point{
		X: origin.X + geo.Cm(geo.DivRoundHalfEven(along2*ux+across2*lx, 2*geo.TrigScale)),
		Y: origin.Y + geo.Cm(geo.DivRoundHalfEven(along2*uy+across2*ly, 2*geo.TrigScale)),
	}
}

// ShrinkRect returns r shrunk by d on every side, keeping its centre (the anchor moves inwards by d).
func ShrinkRect(r geo.Rect, d geo.Cm) geo.Rect {
	return geo.Rect{
		Anchor:   offset(r.Anchor, r.Rotation, 2*int64(d), 0),
		Width:    r.Width - 2*d,
		Length:   r.Length - 2*d,
		Rotation: r.Rotation,
	}
}

// tolerantPolygon returns the corners of r in doubled coordinates (half centimetres), shrunk by half a
// centimetre on every side, each corner rounded once. Two rectangles whose tolerant polygons do not overlap
// may overlap by at most 1 cm (spec 8.4).
func tolerantPolygon(r geo.Rect) geo.Polygon {
	ux, uy := geo.Cos(r.Rotation), geo.Sin(r.Rotation)
	lx, ly := uy, -ux
	w, l := int64(r.Width), int64(r.Length)
	ax, ay := 2*int64(r.Anchor.X), 2*int64(r.Anchor.Y)
	corner := func(across, along int64) geo.Point {
		return geo.Point{
			X: geo.Cm(ax + geo.DivRoundHalfEven(across*lx+along*ux, geo.TrigScale)),
			Y: geo.Cm(ay + geo.DivRoundHalfEven(across*ly+along*uy, geo.TrigScale)),
		}
	}
	// Half width in doubled units is w; shrink by 1 doubled unit (½ cm) on every side.
	hw, a0, a1 := w-1, int64(1), 2*l-1
	if hw <= 0 || a1 <= a0 {
		return nil
	}
	return geo.Polygon{corner(hw, a0), corner(-hw, a0), corner(-hw, a1), corner(hw, a1)}
}

// BerthsOverlap reports whether two berth (or land resource, or band) rectangles overlap by more than the 1 cm
// tolerance of spec 8.4. Sharing a side, or overlapping by at most 1 cm, is not an overlap.
func BerthsOverlap(a, b geo.Rect) bool {
	pa, pb := tolerantPolygon(a), tolerantPolygon(b)
	if pa == nil || pb == nil {
		return false
	}
	return geo.ConvexOverlap(pa, pb)
}

// cmpProducts compares a·b with c·d for non-negative operands without overflow: -1, 0 or 1.
func cmpProducts(a, b, c, d uint64) int {
	h1, l1 := bits.Mul64(a, b)
	h2, l2 := bits.Mul64(c, d)
	switch {
	case h1 != h2:
		if h1 < h2 {
			return -1
		}
		return 1
	case l1 != l2:
		if l1 < l2 {
			return -1
		}
		return 1
	}
	return 0
}

func abs64(v int64) uint64 {
	if v < 0 {
		return uint64(-v) //nolint:gosec // geometry is bounded by MaxCoord
	}
	return uint64(v) //nolint:gosec // non-negative
}

// cmpSegDist compares the distance from p to the closed segment a→b with r (r >= 0): -1 when closer than r,
// 0 when exactly r, 1 when farther.
func cmpSegDist(p, a, b geo.Point, r geo.Cm) int {
	rr := uint64(r) * uint64(r) //nolint:gosec // r >= 0
	ab := b.Sub(a)
	ap := p.Sub(a)
	l2 := int64(ab.X)*int64(ab.X) + int64(ab.Y)*int64(ab.Y)
	t := int64(ab.X)*int64(ap.X) + int64(ab.Y)*int64(ap.Y)
	cmp := func(d2 uint64) int {
		switch {
		case d2 < rr:
			return -1
		case d2 > rr:
			return 1
		}
		return 0
	}
	if l2 == 0 || t <= 0 {
		return cmp(uint64(geo.Dist2(p, a))) //nolint:gosec // squares
	}
	if t >= l2 {
		return cmp(uint64(geo.Dist2(p, b))) //nolint:gosec // squares
	}
	c := abs64(geo.Cross(a, b, p))           // distance = c / √l2  ⇒ compare c² with r²·l2
	return cmpProducts(c, c, rr, uint64(l2)) //nolint:gosec // l2 > 0
}

// segDist returns the distance from p to the closed segment a→b, rounded to the nearest centimetre (approximate,
// for warnings and projections).
func segDist(p, a, b geo.Point) geo.Cm {
	ab := b.Sub(a)
	ap := p.Sub(a)
	l2 := int64(ab.X)*int64(ab.X) + int64(ab.Y)*int64(ab.Y)
	t := int64(ab.X)*int64(ap.X) + int64(ab.Y)*int64(ap.Y)
	if l2 == 0 || t <= 0 {
		return segLen(p, a)
	}
	if t >= l2 {
		return segLen(p, b)
	}
	c := abs64(geo.Cross(a, b, p))
	l := ISqrt(uint64(l2)) //nolint:gosec // l2 > 0
	if l == 0 {
		return 0
	}
	return geo.Cm(c / l) //nolint:gosec // bounded
}

// projectOnPolyline returns the arc length along pts of the point of pts nearest to p, and the distance to it.
func projectOnPolyline(pts []geo.Point, p geo.Point) (s, dist geo.Cm) {
	var acc geo.Cm
	best := geo.Cm(-1)
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		l := segLen(a, b)
		d := segDist(p, a, b)
		if best < 0 || d < best {
			best = d
			ab, ap := b.Sub(a), p.Sub(a)
			l2 := int64(ab.X)*int64(ab.X) + int64(ab.Y)*int64(ab.Y)
			t := int64(ab.X)*int64(ap.X) + int64(ab.Y)*int64(ap.Y)
			var along geo.Cm
			switch {
			case l2 == 0 || t <= 0:
				along = 0
			case t >= l2:
				along = l
			default:
				along = geo.Cm(geo.DivRoundHalfEven(t*int64(l), l2)) // t/l2 × l
			}
			s = acc + along
		}
		acc += l
	}
	return s, best
}

// boxOf returns the bounding box of points.
func boxOf(pts []geo.Point) geo.Box {
	if len(pts) == 0 {
		return geo.Box{}
	}
	return geo.Polygon(pts).Bounds()
}

// expand grows a box by d on every side.
func expand(b geo.Box, d geo.Cm) geo.Box {
	return geo.Box{Min: geo.Point{X: b.Min.X - d, Y: b.Min.Y - d}, Max: geo.Point{X: b.Max.X + d, Y: b.Max.Y + d}}
}

// circleBox returns the bounding box of a circle.
func circleBox(c geo.Circle) geo.Box {
	return expand(geo.Box{Min: c.Center, Max: c.Center}, c.Radius)
}
