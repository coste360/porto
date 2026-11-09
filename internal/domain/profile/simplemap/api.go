// Package simplemap builds the simplified public map of a published profile (m12-plan §12.4, D22): bounds,
// structures, berth footprints (layout.Footprint) with the berth category only, dynamic-area and land-resource
// outlines without labels, and the profile's points of interest. Never berth codes, occupancy, vessels, customers,
// sensors or devices (INV-103). At most 32 points per element, coordinates rounded to 10 cm. Pure.
package simplemap

// MaxPoints is the largest number of points of one element.
const MaxPoints = 32

// RoundCm is the rounding step of every coordinate and width.
const RoundCm = 10

// POI kinds accepted on the public map.
var poiKinds = map[string]bool{"entrance": true, "fuel": true, "office": true, "pump_out": true}

// Point is [x_cm, y_cm].
type Point [2]int64

// Shape is one element of the simplified map.
type Shape struct {
	Points   []Point
	Closed   bool
	WidthCm  int64
	Category string // berths only
}

// POI is a point of interest.
type POI struct {
	Kind     string
	XCm, YCm int64
}

// Map is the simplified map.
type Map struct {
	MinXCm, MinYCm, MaxXCm, MaxYCm  int64
	Structures, Berths, Areas, Land []Shape
	POI                             []POI
	Version                         int
}

// Input is the published map as loaded by the profile service: element geometry already converted to points in
// map centimetres (rectangles as their four corners, linear areas as their band), berth categories, profile POIs.
type Input struct {
	MinXCm, MinYCm, MaxXCm, MaxYCm  int64
	Structures, Berths, Areas, Land []Shape
	POI                             []POI
	Version                         int
}

// Round rounds v to the nearest multiple of RoundCm, half to even multiple.
func Round(v int64) int64 {
	q, r := v/RoundCm, v%RoundCm
	if r < 0 {
		q, r = q-1, r+RoundCm
	}
	switch {
	case 2*r > RoundCm:
		q++
	case 2*r == RoundCm && q%2 != 0:
		q++
	}
	return q * RoundCm
}

// Build simplifies the map: every coordinate and width rounded to 10 cm, consecutive duplicate points removed,
// elements decimated to at most 32 points (endpoints of open lines kept), degenerate elements dropped, categories
// kept on berths only, unknown POI kinds dropped. The output is deterministic for a given input.
func Build(in Input) Map {
	out := Map{
		MinXCm: Round(in.MinXCm), MinYCm: Round(in.MinYCm), MaxXCm: Round(in.MaxXCm), MaxYCm: Round(in.MaxYCm),
		Structures: shapes(in.Structures, false), Berths: shapes(in.Berths, true), Areas: shapes(in.Areas, false),
		Land: shapes(in.Land, false), POI: []POI{}, Version: in.Version,
	}
	for _, p := range in.POI {
		if poiKinds[p.Kind] {
			out.POI = append(out.POI, POI{Kind: p.Kind, XCm: Round(p.XCm), YCm: Round(p.YCm)})
		}
	}
	return out
}

func shapes(in []Shape, keepCategory bool) []Shape {
	out := make([]Shape, 0, len(in))
	for _, s := range in {
		pts := make([]Point, 0, len(s.Points))
		for _, p := range s.Points {
			q := Point{Round(p[0]), Round(p[1])}
			if len(pts) > 0 && pts[len(pts)-1] == q {
				continue
			}
			pts = append(pts, q)
		}
		if s.Closed && len(pts) > 1 && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		pts = decimate(pts, s.Closed)
		if len(pts) < 2 || (s.Closed && len(pts) < 3) {
			continue
		}
		sh := Shape{Points: pts, Closed: s.Closed, WidthCm: max(0, Round(s.WidthCm))}
		if keepCategory {
			sh.Category = s.Category
		}
		out = append(out, sh)
	}
	return out
}

// decimate keeps at most MaxPoints points chosen at evenly spaced indexes (integer arithmetic): an open line keeps
// its first and last point, a closed ring starts at its first point.
func decimate(pts []Point, closed bool) []Point {
	n := len(pts)
	if n <= MaxPoints {
		return pts
	}
	out := make([]Point, 0, MaxPoints)
	for i := range MaxPoints {
		var j int
		if closed {
			j = i * n / MaxPoints
		} else {
			j = i * (n - 1) / (MaxPoints - 1)
		}
		out = append(out, pts[j])
	}
	return out
}
