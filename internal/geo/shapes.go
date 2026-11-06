package geo

import "fmt"

// Polygon is a closed polygon; the closing edge from the last point to the first is implicit.
type Polygon []Point

// MaxPolygonPoints bounds the number of vertices accepted by Validate.
const MaxPolygonPoints = 10_000

func (p Polygon) edge(i int) Segment { return Segment{p[i], p[(i+1)%len(p)]} }

// Validate checks that p has at least 3 points, no consecutive duplicates, a non-zero area, no
// self-intersection and coordinates within MaxCoord.
func (p Polygon) Validate() error {
	n := len(p)
	if n < 3 {
		return fmt.Errorf("%w: polygon needs at least 3 points, has %d", ErrInvalidGeometry, n)
	}
	if n > MaxPolygonPoints {
		return fmt.Errorf("%w: polygon has more than %d points", ErrInvalidGeometry, MaxPolygonPoints)
	}
	for i, pt := range p {
		if !pt.inBounds() {
			return fmt.Errorf("%w: point %d out of bounds", ErrInvalidGeometry, i)
		}
		if pt == p[(i+1)%n] {
			return fmt.Errorf("%w: point %d repeats the next point", ErrInvalidGeometry, i)
		}
	}
	if p.Area2() == 0 {
		return fmt.Errorf("%w: polygon has zero area", ErrInvalidGeometry)
	}
	for i := range n {
		ei := p.edge(i)
		for j := i + 1; j < n; j++ {
			ej := p.edge(j)
			adjacent := j == i+1 || (i == 0 && j == n-1)
			if !adjacent {
				if SegmentsIntersect(ei, ej) {
					return fmt.Errorf("%w: edges %d and %d intersect", ErrInvalidGeometry, i, j)
				}
				continue
			}
			// Adjacent edges share one vertex; they must not fold back onto each other.
			a, shared, b := ei.A, ei.B, ej.B
			if j == n-1 && i == 0 {
				a, shared, b = ej.A, ej.B, ei.B
			}
			if Cross(shared, a, b) == 0 && dot(a.Sub(shared), b.Sub(shared)) > 0 {
				return fmt.Errorf("%w: edges %d and %d overlap", ErrInvalidGeometry, i, j)
			}
		}
	}
	return nil
}

func dot(a, b Point) int64 { return int64(a.X)*int64(b.X) + int64(a.Y)*int64(b.Y) }

// Area2 returns twice the signed area (shoelace formula, exact). It is positive when the points turn
// counter-clockwise in mathematical orientation (clockwise on the y-down screen).
func (p Polygon) Area2() Cm2 {
	if len(p) < 3 {
		return 0
	}
	// Coordinates relative to p[0] keep terms small; int64 wrap-around is harmless because the exact
	// result of a polygon within MaxCoord fits int64.
	var sum int64
	o := p[0]
	for i := 1; i+1 < len(p); i++ {
		sum += Cross(o, p[i], p[i+1])
	}
	return Cm2(sum)
}

// Area returns the unsigned area, rounded half-even to the square centimetre.
func (p Polygon) Area() Cm2 {
	a := int64(p.Area2())
	if a < 0 {
		a = -a
	}
	return Cm2(DivRoundHalfEven(a, 2))
}

// Bounds returns the bounding box.
func (p Polygon) Bounds() Box { return boundsOf(p) }

func (p Polygon) onBoundary(pt Point) bool {
	for i := range p {
		if pointOnSegment(p.edge(i), pt) {
			return true
		}
	}
	return false
}

// Contains reports whether pt is inside p or on its boundary.
func (p Polygon) Contains(pt Point) bool {
	if len(p) < 3 {
		return false
	}
	if p.onBoundary(pt) {
		return true
	}
	return p.crossingNumberOdd(pt)
}

// ContainsStrict reports whether pt is inside p and not on its boundary.
func (p Polygon) ContainsStrict(pt Point) bool {
	if len(p) < 3 || p.onBoundary(pt) {
		return false
	}
	return p.crossingNumberOdd(pt)
}

// crossingNumberOdd casts a ray towards +x. Edges are half-open in y, so a ray through a vertex is counted
// once. pt must not be on the boundary.
func (p Polygon) crossingNumberOdd(pt Point) bool {
	inside := false
	for i := range p {
		a, b := p[i], p[(i+1)%len(p)]
		if (a.Y > pt.Y) == (b.Y > pt.Y) {
			continue
		}
		// pt is left of the crossing ⇔ sign of Cross(a, b, pt) agrees with the edge going down (y increasing).
		if c := Cross(a, b, pt); (c > 0) == (b.Y > a.Y) {
			inside = !inside
		}
	}
	return inside
}

// IsConvex reports whether p is a valid convex polygon (collinear consecutive vertices are allowed).
func (p Polygon) IsConvex() bool {
	if p.Validate() != nil {
		return false
	}
	sign := 0
	for i := range p {
		c := Cross(p[i], p[(i+1)%len(p)], p[(i+2)%len(p)])
		switch {
		case c == 0:
			continue
		case sign == 0:
			sign = signOf(c)
		case signOf(c) != sign:
			return false
		}
	}
	return true
}

func signOf(v int64) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

// PolygonsIntersect reports whether a and b share at least one point: an edge crossing or touching, or one
// containing the other.
func PolygonsIntersect(a, b Polygon) bool {
	if len(a) == 0 || len(b) == 0 || !a.Bounds().Intersects(b.Bounds()) {
		return false
	}
	for i := range a {
		for j := range b {
			if SegmentsIntersect(a.edge(i), b.edge(j)) {
				return true
			}
		}
	}
	return a.Contains(b[0]) || b.Contains(a[0])
}

// ConvexOverlap reports whether the interiors of two convex polygons overlap. Polygons that only touch along
// an edge or at a corner do not overlap, and a degenerate (zero-area) polygon overlaps nothing. It uses the
// separating axis theorem on the integer edge normals of both polygons.
func ConvexOverlap(a, b Polygon) bool {
	if len(a) < 3 || len(b) < 3 || a.Area2() == 0 || b.Area2() == 0 {
		return false
	}
	return !separatedByEdgeOf(a, b) && !separatedByEdgeOf(b, a)
}

// separatedByEdgeOf reports whether some edge normal of p separates p and q (projections touching counts as
// separated, since interiors are then disjoint).
func separatedByEdgeOf(p, q Polygon) bool {
	for i := range p {
		e := p[(i+1)%len(p)].Sub(p[i])
		if e.X == 0 && e.Y == 0 {
			continue
		}
		axis := Point{-e.Y, e.X}
		pMin, pMax := project(p, axis)
		qMin, qMax := project(q, axis)
		if pMax <= qMin || qMax <= pMin {
			return true
		}
	}
	return false
}

func project(p Polygon, axis Point) (lo, hi int64) {
	lo, hi = dot(p[0], axis), dot(p[0], axis)
	for _, pt := range p[1:] {
		d := dot(pt, axis)
		lo, hi = min(lo, d), max(hi, d)
	}
	return lo, hi
}

// Rect is a rotated rectangle (berth, finger, land resource; spec 7.3). The anchor is the midpoint of the
// mooring side; the rectangle extends Length from the anchor in the direction Rotation, and Width across it.
type Rect struct {
	Anchor   Point `json:"anchor"`
	Width    Cm    `json:"width"`
	Length   Cm    `json:"length"`
	Rotation Angle `json:"rotation"`
}

// Validate checks positive sizes and that every corner is within MaxCoord.
func (r Rect) Validate() error {
	if r.Width <= 0 || r.Length <= 0 {
		return fmt.Errorf("%w: width and length must be positive", ErrInvalidGeometry)
	}
	if r.Width > 2*MaxCoord || r.Length > 2*MaxCoord || !r.Anchor.inBounds() {
		return fmt.Errorf("%w: rectangle out of bounds", ErrInvalidGeometry)
	}
	if r.Rotation < 0 || r.Rotation > 3599 {
		return fmt.Errorf("%w: rotation %d not in 0..3599", ErrInvalidGeometry, r.Rotation)
	}
	for _, c := range r.Corners() {
		if !c.inBounds() {
			return fmt.Errorf("%w: rectangle out of bounds", ErrInvalidGeometry)
		}
	}
	return nil
}

// Corners returns anchor-left, anchor-right, far-right, far-left, where left and right are seen looking from
// the anchor along Rotation. The winding is the same for every rotation (clockwise in mathematical
// orientation, counter-clockwise on screen). Each corner is rounded half-even once from its exact value.
func (r Rect) Corners() [4]Point {
	ux, uy := Cos(r.Rotation), Sin(r.Rotation) // direction, × TrigScale
	w, l := int64(r.Width), int64(r.Length)
	// left = (uy, -ux); offsets are computed over 2·TrigScale so that odd widths stay exact until rounding.
	corner := func(side, far int64) Point {
		return Point{
			X: r.Anchor.X + Cm(DivRoundHalfEven(side*uy*w+far*2*ux*l, 2*TrigScale)),
			Y: r.Anchor.Y + Cm(DivRoundHalfEven(-side*ux*w+far*2*uy*l, 2*TrigScale)),
		}
	}
	return [4]Point{corner(1, 0), corner(-1, 0), corner(-1, 1), corner(1, 1)}
}

// Polygon returns the corners as a polygon.
func (r Rect) Polygon() Polygon {
	c := r.Corners()
	return Polygon{c[0], c[1], c[2], c[3]}
}

// Bounds returns the bounding box of the corners.
func (r Rect) Bounds() Box {
	c := r.Corners()
	return boundsOf(c[:])
}

// Area returns Width × Length (exact, not computed from the rounded corners).
func (r Rect) Area() Cm2 { return Cm2(int64(r.Width) * int64(r.Length)) }

// Contains reports whether p is inside r or on its border.
func (r Rect) Contains(p Point) bool { return r.Polygon().Contains(p) }

// RectsOverlap reports whether the interiors of a and b overlap. Berths sharing a side or a corner do not.
func RectsOverlap(a, b Rect) bool { return ConvexOverlap(a.Polygon(), b.Polygon()) }

// SegmentIntersectsRect reports whether the closed segment s touches or enters r.
func SegmentIntersectsRect(s Segment, r Rect) bool {
	p := r.Polygon()
	if p.Contains(s.A) || p.Contains(s.B) {
		return true
	}
	for i := range p {
		if SegmentsIntersect(s, p.edge(i)) {
			return true
		}
	}
	return false
}
