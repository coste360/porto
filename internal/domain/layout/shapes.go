package layout

import (
	"github.com/coste360/porto/internal/geo"
)

// Geometry helpers of the harbour elements used by validation and generators.

// structureHalfWidth returns half the width of a structure in cm (0 for quays and polygons).
func structureHalfWidth(s *Structure) geo.Cm {
	if s.Closed || s.Rect != nil {
		return 0
	}
	return s.Width / 2
}

// usableSides returns the sides of a structure where berths and linear areas may attach.
func usableSides(s *Structure) []Side {
	if len(s.Sides) > 0 {
		return s.Sides
	}
	if s.Kind == StructurePier {
		return []Side{SideLeft, SideRight}
	}
	return nil
}

// supportOf returns the support polyline of a structure side and the offset of the side edge from it, in half
// centimetres (positive = left of the support direction). The head of a pier is the segment across its last
// point from its left to its right edge.
func supportOf(s *Structure, side Side) (pts []geo.Point, across2 int64, ok bool) {
	if s.Closed || s.Rect != nil || len(s.Points) < 2 {
		return nil, 0, false
	}
	switch side {
	case SideLeft:
		return s.Points, int64(s.Width), true
	case SideRight:
		return s.Points, -int64(s.Width), true
	case SideHead:
		n := len(s.Points)
		end := s.Points[n-1]
		rot := AngleOf(int64(end.X-s.Points[n-2].X), int64(end.Y-s.Points[n-2].Y))
		l := offset(end, rot, 0, int64(s.Width))
		r := offset(end, rot, 0, -int64(s.Width))
		if l == r {
			return nil, 0, false
		}
		return []geo.Point{l, r}, 0, true
	}
	return nil, 0, false
}

// sideRotation is the outward direction of the left (+) or right (−) side of a support direction.
func sideRotation(dir geo.Angle, across2 int64, side Side) geo.Angle {
	if side == SideRight || (side != SideLeft && across2 < 0) {
		return dir.Add(900)
	}
	return geo.NewAngle(int64(dir) - 900)
}

// PlaceAlong returns the rectangle of a slot [start, start+width] along the side of a structure, extending
// length outwards (the berth placement maths of the generators, for one berth). The slot must lie on a single
// segment of the support polyline.
func PlaceAlong(s *Structure, side Side, start, width, length geo.Cm) (geo.Rect, error) {
	pts, across2, ok := supportOf(s, side)
	if !ok || width <= 0 || length <= 0 || start < 0 {
		return geo.Rect{}, ErrOutOfRange
	}
	var acc geo.Cm
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		l := segLen(a, b)
		if start >= acc && start+width <= acc+l && l > 0 {
			dir := AngleOf(int64(b.X-a.X), int64(b.Y-a.Y))
			along2 := 2*int64(start-acc) + int64(width) // slot middle, in half cm
			anchor := offset(a, dir, along2, across2)
			return geo.Rect{Anchor: anchor, Width: width, Length: length, Rotation: sideRotation(dir, across2, side)}, nil
		}
		acc += l
	}
	return geo.Rect{}, ErrOutOfRange
}

// SupportLength returns the usable length of a structure side (0 when the side has no support).
func SupportLength(s *Structure, side Side) geo.Cm {
	pts, _, ok := supportOf(s, side)
	if !ok {
		return 0
	}
	return PolylineLength(pts)
}

// linearBand returns the occupiable band of a linear area: the rectangle on the chord of [From, To] along the
// structure side, with depth MaxBeam (stern_to) or MaxBeam + Margin (alongside).
func linearBand(d *DynamicArea, s *Structure) (geo.Rect, bool) {
	pts, across2, ok := supportOf(s, d.Side)
	if !ok {
		return geo.Rect{}, false
	}
	p0, _, err0 := PointAt(pts, d.From)
	p1, _, err1 := PointAt(pts, d.To)
	if err0 != nil || err1 != nil || p0 == p1 {
		return geo.Rect{}, false
	}
	dir := AngleOf(int64(p1.X-p0.X), int64(p1.Y-p0.Y))
	chord := segLen(p0, p1)
	depth := bandDepth(d)
	anchor := offset(p0, dir, int64(chord), across2)
	return geo.Rect{Anchor: anchor, Width: chord, Length: depth, Rotation: sideRotation(dir, across2, d.Side)}, true
}

// DefaultBandDepth is the depth of a linear area band without max_beam_cm and max_loa_cm.
const DefaultBandDepth geo.Cm = 1000

func bandDepth(d *DynamicArea) geo.Cm {
	depth := d.MaxBeam
	if depth <= 0 {
		depth = d.MaxLOA
	}
	if depth <= 0 {
		depth = DefaultBandDepth
	}
	if d.Mode == ModeAlongside {
		depth += d.Margin
	}
	return depth
}

// edgesOf returns the boundary segments of a structure: the outline of each segment band for structures with a
// width, the polyline for quays, the polygon edges, the finger rectangle.
func edgesOf(s *Structure) []geo.Segment {
	var out []geo.Segment
	poly := func(p geo.Polygon) {
		for i := range p {
			out = append(out, geo.Segment{A: p[i], B: p[(i+1)%len(p)]})
		}
	}
	switch {
	case s.Rect != nil:
		poly(s.Rect.Polygon())
	case s.Closed:
		poly(s.Points)
	case s.Width > 0:
		for i := 1; i < len(s.Points); i++ {
			a, b := s.Points[i-1], s.Points[i]
			l := segLen(a, b)
			if l == 0 {
				continue
			}
			dir := AngleOf(int64(b.X-a.X), int64(b.Y-a.Y))
			// Anchor on the left edge at mid-segment, extending the pier width towards the right edge.
			r := geo.Rect{Anchor: offset(a, dir, int64(l), int64(s.Width)), Width: l, Length: s.Width, Rotation: dir.Add(900)}
			poly(r.Polygon())
		}
	default:
		for i := 1; i < len(s.Points); i++ {
			out = append(out, geo.Segment{A: s.Points[i-1], B: s.Points[i]})
		}
	}
	return out
}

// structureBox returns the bounding box of a structure (bands included).
func structureBox(s *Structure) geo.Box {
	if s.Rect != nil {
		return s.Rect.Bounds()
	}
	return expand(boxOf(s.Points), structureHalfWidth(s))
}

// doubled returns p with coordinates × 2 (half centimetres), the space of tolerantPolygon.
func doubled(p geo.Polygon) geo.Polygon {
	out := make(geo.Polygon, len(p))
	for i, q := range p {
		out[i] = geo.Point{X: 2 * q.X, Y: 2 * q.Y}
	}
	return out
}

// maxTriangulated bounds the outlines that are triangulated exactly; larger ones use the inclusive test.
const maxTriangulated = 512

// triangulate splits a simple polygon into triangles by ear clipping (exact integer predicates).
func triangulate(p geo.Polygon) []geo.Polygon {
	n := len(p)
	if n < 3 {
		return nil
	}
	if n == 3 || p.IsConvex() {
		out := make([]geo.Polygon, 0, n-2)
		for i := 1; i+1 < n; i++ {
			out = append(out, geo.Polygon{p[0], p[i], p[i+1]})
		}
		return out
	}
	sign := int64(1)
	if p.Area2() < 0 {
		sign = -1
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	var out []geo.Polygon
	for guard := 0; len(idx) > 3 && guard < n*n; guard++ {
		clipped := false
		for k := range idx {
			i0, i1, i2 := idx[(k+len(idx)-1)%len(idx)], idx[k], idx[(k+1)%len(idx)]
			a, b, c := p[i0], p[i1], p[i2]
			cr := geo.Cross(a, b, c) * sign
			if cr < 0 {
				continue // reflex
			}
			if cr == 0 { // collinear vertex: drop it
				idx = append(idx[:k], idx[k+1:]...)
				clipped = true
				break
			}
			tri := geo.Polygon{a, b, c}
			ear := true
			for _, j := range idx {
				if j == i0 || j == i1 || j == i2 {
					continue
				}
				if tri.Contains(p[j]) {
					ear = false
					break
				}
			}
			if ear {
				out = append(out, tri)
				idx = append(idx[:k], idx[k+1:]...)
				clipped = true
				break
			}
		}
		if !clipped {
			break
		}
	}
	if len(idx) == 3 {
		out = append(out, geo.Polygon{p[idx[0]], p[idx[1]], p[idx[2]]})
	}
	return out
}

// convexPieces returns convex pieces of a polygon (itself when convex), or nil when it is too large.
func convexPieces(p geo.Polygon) []geo.Polygon {
	if p.IsConvex() {
		return []geo.Polygon{p}
	}
	if len(p) > maxTriangulated {
		return nil
	}
	return triangulate(p)
}

// piecesOverlap reports whether two sets of convex pieces have overlapping interiors. A nil set (outline too
// large to triangulate) falls back on the inclusive polygon intersection of the originals.
func piecesOverlap(a, b []geo.Polygon, fa, fb geo.Polygon) bool {
	if a == nil || b == nil {
		return geo.PolygonsIntersect(fa, fb)
	}
	for _, x := range a {
		for _, y := range b {
			if x.Bounds().Intersects(y.Bounds()) && geo.ConvexOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

// LinearBand returns the occupiable band of a linear area on its structure (false when the support or the
// range is invalid). It is the rectangle ValidateMap checks for overlaps.
func LinearBand(d DynamicArea, s Structure) (geo.Rect, bool) { return linearBand(&d, &s) }

// ProjectOnStructure returns the distance along the axis of a polyline structure of the point of the axis
// nearest to p (0 for polygons and fingers).
func ProjectOnStructure(s Structure, p geo.Point) geo.Cm {
	if s.Closed || s.Rect != nil || len(s.Points) < 2 {
		return 0
	}
	at, _ := projectOnPolyline(s.Points, p)
	return at
}
