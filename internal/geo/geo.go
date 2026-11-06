// Package geo is Porto's integer plane geometry in centimetres (spec 7.1).
//
// The origin is the top-left corner of the map and the y axis points down, as in SVG. Angles are tenths of
// degree; angle 0 points along +x and angles grow clockwise on screen (the SVG rotate() convention), so the
// direction of angle a is (Cos(a), Sin(a)) / TrigScale. No float is used at run time: trigonometry reads a
// committed fixed-point table and every rounding is half-even to the nearest centimetre.
//
// The package is pure: no I/O, no clock, no randomness.
package geo

//go:generate go run ./internal/gentrig

import (
	"errors"
	"math/bits"
)

// Cm is a length in centimetres.
type Cm int64

// Cm2 is an area in square centimetres.
type Cm2 int64

// MaxCoord bounds |x| and |y| of every validated coordinate (1000 km), so that differences, dot and cross
// products of coordinates always fit int64.
const MaxCoord Cm = 100_000_000

// ErrInvalidGeometry reports a shape that fails validation.
var ErrInvalidGeometry = errors.New("geo: invalid geometry")

// Angle is an angle in tenths of degree, normalized to 0..3599 (the rotation_dd columns).
type Angle int32

// NewAngle normalizes any number of tenths of degree (negative included) to 0..3599.
func NewAngle(dd int64) Angle {
	m := dd % 3600
	if m < 0 {
		m += 3600
	}
	return Angle(m)
}

// Add returns a+b normalized.
func (a Angle) Add(b Angle) Angle { return NewAngle(int64(a) + int64(b)) }

// TrigScale is the fixed-point scale of Sin and Cos.
const TrigScale int64 = 1_000_000_000

// Sin returns sin(a) × TrigScale, exact at multiples of 90°.
func Sin(a Angle) int64 {
	a = NewAngle(int64(a))
	switch {
	case a <= 900:
		return sinTable[a]
	case a <= 1800:
		return sinTable[1800-a]
	case a <= 2700:
		return -sinTable[a-1800]
	default:
		return -sinTable[3600-a]
	}
}

// Cos returns cos(a) × TrigScale, exact at multiples of 90°.
func Cos(a Angle) int64 { return Sin(a.Add(900)) }

// Point is a position on the map.
type Point struct {
	X Cm `json:"x"`
	Y Cm `json:"y"`
}

// Add returns p+q.
func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }

// Sub returns p-q.
func (p Point) Sub(q Point) Point { return Point{p.X - q.X, p.Y - q.Y} }

func (p Point) inBounds() bool {
	return p.X >= -MaxCoord && p.X <= MaxCoord && p.Y >= -MaxCoord && p.Y <= MaxCoord
}

// Dist2 returns the squared distance between a and b.
func Dist2(a, b Point) int64 {
	dx, dy := int64(a.X-b.X), int64(a.Y-b.Y)
	return dx*dx + dy*dy
}

// Cross returns the z component of (a-o) × (b-o).
func Cross(o, a, b Point) int64 {
	return int64(a.X-o.X)*int64(b.Y-o.Y) - int64(a.Y-o.Y)*int64(b.X-o.X)
}

// Orientation returns the sign of Cross(a, b, c): 1 when a→b→c turns counter-clockwise in mathematical
// (y-up) orientation, -1 when it turns clockwise, 0 when collinear. Because the map's y axis points down, 1
// looks clockwise on screen and -1 counter-clockwise.
func Orientation(a, b, c Point) int {
	switch v := Cross(a, b, c); {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

// divRound returns num/den rounded half-even. den > 0.
func divRound(num, den int64) int64 {
	neg := num < 0
	u := uint64(num)
	if neg {
		u = -u
	}
	q, r := bits.Div64(0, u, uint64(den))
	if half := uint64(den) - r; r > half || (r == half && q%2 == 1) {
		q++
	}
	if neg {
		return -int64(q) //nolint:gosec // |num/den| <= |num|
	}
	return int64(q) //nolint:gosec // |num/den| <= |num|
}

// Rotate rotates p around center by a (clockwise on screen) and rounds half-even to the centimetre.
// Points must be within MaxCoord.
func Rotate(p, center Point, a Angle) Point {
	s, c := Sin(a), Cos(a)
	dx, dy := int64(p.X-center.X), int64(p.Y-center.Y)
	return Point{
		X: center.X + Cm(divRound(dx*c-dy*s, TrigScale)),
		Y: center.Y + Cm(divRound(dx*s+dy*c, TrigScale)),
	}
}

// Box is an axis-aligned box; both corners are inclusive.
type Box struct {
	Min, Max Point
}

// Intersects reports whether b and o share at least one point (touching boxes intersect).
func (b Box) Intersects(o Box) bool {
	return b.Min.X <= o.Max.X && o.Min.X <= b.Max.X && b.Min.Y <= o.Max.Y && o.Min.Y <= b.Max.Y
}

// Contains reports whether p is inside b or on its border.
func (b Box) Contains(p Point) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y
}

// Union returns the smallest box containing b and o.
func (b Box) Union(o Box) Box {
	return Box{
		Min: Point{min(b.Min.X, o.Min.X), min(b.Min.Y, o.Min.Y)},
		Max: Point{max(b.Max.X, o.Max.X), max(b.Max.Y, o.Max.Y)},
	}
}

func boundsOf(pts []Point) Box {
	if len(pts) == 0 {
		return Box{}
	}
	b := Box{Min: pts[0], Max: pts[0]}
	for _, p := range pts[1:] {
		b.Min.X, b.Min.Y = min(b.Min.X, p.X), min(b.Min.Y, p.Y)
		b.Max.X, b.Max.Y = max(b.Max.X, p.X), max(b.Max.Y, p.Y)
	}
	return b
}

// Segment is the closed segment from A to B.
type Segment struct {
	A, B Point
}

// onSegment reports whether p, known to be collinear with s, lies within s's bounding box.
func onSegment(s Segment, p Point) bool {
	return min(s.A.X, s.B.X) <= p.X && p.X <= max(s.A.X, s.B.X) &&
		min(s.A.Y, s.B.Y) <= p.Y && p.Y <= max(s.A.Y, s.B.Y)
}

// pointOnSegment reports whether p lies on the closed segment s.
func pointOnSegment(s Segment, p Point) bool {
	return Cross(s.A, s.B, p) == 0 && onSegment(s, p)
}

// SegmentsIntersect reports whether the closed segments share at least one point (touching at an endpoint
// and collinear overlap count).
func SegmentsIntersect(s, t Segment) bool {
	o1, o2 := Orientation(s.A, s.B, t.A), Orientation(s.A, s.B, t.B)
	o3, o4 := Orientation(t.A, t.B, s.A), Orientation(t.A, t.B, s.B)
	if o1 != o2 && o3 != o4 {
		return true // general position, including an endpoint touching the other segment
	}
	// Remaining cases need an endpoint collinear with the other segment and inside its extent.
	return (o1 == 0 && onSegment(s, t.A)) || (o2 == 0 && onSegment(s, t.B)) ||
		(o3 == 0 && onSegment(t, s.A)) || (o4 == 0 && onSegment(t, s.B))
}

// SegmentsCrossProperly reports whether the interiors of s and t cross at exactly one point that is not an
// endpoint of either.
func SegmentsCrossProperly(s, t Segment) bool {
	o1, o2 := Orientation(s.A, s.B, t.A), Orientation(s.A, s.B, t.B)
	o3, o4 := Orientation(t.A, t.B, s.A), Orientation(t.A, t.B, s.B)
	return o1*o2 < 0 && o3*o4 < 0
}

// Circle is a disc (buoy swing area, spec 7.3).
type Circle struct {
	Center Point `json:"center"`
	Radius Cm    `json:"radius"`
}

// CirclesOverlap reports whether the interiors of a and b overlap (tangent circles do not).
func CirclesOverlap(a, b Circle) bool {
	r := int64(a.Radius + b.Radius)
	return Dist2(a.Center, b.Center) < r*r
}
