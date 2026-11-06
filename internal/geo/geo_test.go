package geo

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func pt(x, y Cm) Point { return Point{x, y} }

func TestNewAngle_Normalizes(t *testing.T) {
	tests := []struct {
		in   int64
		want Angle
	}{
		{0, 0},
		{1, 1},
		{3599, 3599},
		{3600, 0},
		{-1, 3599},
		{-3600, 0},
		{-3601, 3599},
		{7205, 5},
		{math.MaxInt64, Angle(math.MaxInt64 % 3600)},
		{math.MinInt64, Angle(3600 + math.MinInt64%3600)},
	}
	for _, tt := range tests {
		if got := NewAngle(tt.in); got != tt.want {
			t.Errorf("NewAngle(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
	if got := Angle(3500).Add(200); got != 100 {
		t.Errorf("3500+200 = %d", got)
	}
}

func TestSinCos_ExactAtRightAngles(t *testing.T) {
	tests := []struct {
		a        Angle
		sin, cos int64
	}{
		{0, 0, TrigScale},
		{900, TrigScale, 0},
		{1800, 0, -TrigScale},
		{2700, -TrigScale, 0},
	}
	for _, tt := range tests {
		if Sin(tt.a) != tt.sin || Cos(tt.a) != tt.cos {
			t.Errorf("angle %d: sin %d cos %d, want %d %d", tt.a, Sin(tt.a), Cos(tt.a), tt.sin, tt.cos)
		}
	}
	if Sin(300) != 500_000_000 || Cos(600) != 500_000_000 {
		t.Errorf("30°: sin %d, cos(60°) %d", Sin(300), Cos(600))
	}
}

// TestSinTable_MatchesMath is the only place where floats are used: it checks the committed table.
func TestSinTable_MatchesMath(t *testing.T) {
	for i := range Angle(3600) {
		rad := float64(i) * math.Pi / 1800
		if d := math.Abs(float64(Sin(i)) - math.Sin(rad)*1e9); d > 1 {
			t.Fatalf("Sin(%d) = %d, math %f", i, Sin(i), math.Sin(rad)*1e9)
		}
		if d := math.Abs(float64(Cos(i)) - math.Cos(rad)*1e9); d > 1 {
			t.Fatalf("Cos(%d) = %d, math %f", i, Cos(i), math.Cos(rad)*1e9)
		}
	}
	for i := 1; i <= 900; i++ {
		if sinTable[i] <= sinTable[i-1] {
			t.Fatalf("table not strictly increasing at %d", i)
		}
	}
}

func TestRotate_QuarterTurnsExact(t *testing.T) {
	c := pt(100, 200)
	p := pt(130, 210) // offset (30, 10)
	tests := []struct {
		a    Angle
		want Point
	}{
		{0, pt(130, 210)},
		{900, pt(90, 230)},   // (30,10) → (-10,30): +x turns to +y (down), clockwise on screen
		{1800, pt(70, 190)},  // (-30,-10)
		{2700, pt(110, 170)}, // (10,-30)
	}
	for _, tt := range tests {
		if got := Rotate(p, c, tt.a); got != tt.want {
			t.Errorf("Rotate(%v, %d) = %v, want %v", p, tt.a, got, tt.want)
		}
	}
	if got := Rotate(pt(1000, 0), pt(0, 0), 300); got != pt(866, 500) {
		t.Errorf("Rotate 30° = %v", got)
	}
}

func TestRotate_HalfEven(t *testing.T) {
	// cos 60° = 0.5 exactly: 1 × 0.5 = 0.5 → 0, 3 × 0.5 = 1.5 → 2.
	if got := Rotate(pt(1, 0), pt(0, 0), 600); got.X != 0 {
		t.Errorf("0.5 rounds to %d", got.X)
	}
	if got := Rotate(pt(3, 0), pt(0, 0), 600); got.X != 2 {
		t.Errorf("1.5 rounds to %d", got.X)
	}
	if got := Rotate(pt(-3, 0), pt(0, 0), 600); got.X != -2 {
		t.Errorf("-1.5 rounds to %d", got.X)
	}
}

func TestOrientationAndCross(t *testing.T) {
	o := pt(0, 0)
	if Orientation(o, pt(10, 0), pt(10, 10)) != 1 || Orientation(o, pt(10, 0), pt(10, -10)) != -1 ||
		Orientation(o, pt(10, 0), pt(20, 0)) != 0 {
		t.Fatal("orientation signs")
	}
	if Cross(o, pt(MaxCoord, -MaxCoord), pt(-MaxCoord, -MaxCoord)) != -2*int64(MaxCoord)*int64(MaxCoord) {
		t.Fatal("cross at bounds")
	}
	if Dist2(pt(0, 0), pt(3, 4)) != 25 {
		t.Fatal("Dist2")
	}
	if pt(1, 2).Add(pt(3, 4)) != pt(4, 6) || pt(1, 2).Sub(pt(3, 4)) != pt(-2, -2) {
		t.Fatal("Add/Sub")
	}
}

func TestBox(t *testing.T) {
	a := Box{pt(0, 0), pt(10, 10)}
	tests := []struct {
		name string
		b    Box
		want bool
	}{
		{"same", a, true},
		{"touching edge", Box{pt(10, 0), pt(20, 10)}, true},
		{"touching corner", Box{pt(10, 10), pt(20, 20)}, true},
		{"apart x", Box{pt(11, 0), pt(20, 10)}, false},
		{"apart y", Box{pt(0, 11), pt(10, 20)}, false},
		{"inside", Box{pt(2, 2), pt(3, 3)}, true},
	}
	for _, tt := range tests {
		if a.Intersects(tt.b) != tt.want || tt.b.Intersects(a) != tt.want {
			t.Errorf("%s: Intersects = %v", tt.name, a.Intersects(tt.b))
		}
	}
	if !a.Contains(pt(10, 0)) || a.Contains(pt(11, 0)) {
		t.Error("Box.Contains")
	}
	if u := a.Union(Box{pt(-5, 3), pt(4, 20)}); u != (Box{pt(-5, 0), pt(10, 20)}) {
		t.Errorf("Union = %v", u)
	}
}

func TestPoint_JSON(t *testing.T) {
	b, _ := json.Marshal(pt(12, -3))
	if string(b) != `{"x":12,"y":-3}` {
		t.Fatalf("json = %s", b)
	}
}

func TestRect_CornersAxisAligned(t *testing.T) {
	a := pt(1000, 500)
	tests := []struct {
		rot  Angle
		want [4]Point
	}{
		// Facing +x: left is up (-y).
		{0, [4]Point{pt(1000, 400), pt(1000, 600), pt(1800, 600), pt(1800, 400)}},
		// Facing +y (down): left is +x.
		{900, [4]Point{pt(1100, 500), pt(900, 500), pt(900, 1300), pt(1100, 1300)}},
		// Facing -x: left is down.
		{1800, [4]Point{pt(1000, 600), pt(1000, 400), pt(200, 400), pt(200, 600)}},
		// Facing -y (up): left is -x.
		{2700, [4]Point{pt(900, 500), pt(1100, 500), pt(1100, -300), pt(900, -300)}},
	}
	for _, tt := range tests {
		r := Rect{Anchor: a, Width: 200, Length: 800, Rotation: tt.rot}
		if got := r.Corners(); got != tt.want {
			t.Errorf("rotation %d: corners %v, want %v", tt.rot, got, tt.want)
		}
		// Same winding for every rotation.
		if r.Polygon().Area2() >= 0 {
			t.Errorf("rotation %d: Area2 = %d, want negative (consistent winding)", tt.rot, r.Polygon().Area2())
		}
		if r.Polygon().Area() != 160_000 {
			t.Errorf("rotation %d: polygon area %d", tt.rot, r.Polygon().Area())
		}
	}
	r := Rect{Anchor: pt(0, 0), Width: 200, Length: 800, Rotation: 0}
	if b := r.Bounds(); b != (Box{pt(0, -100), pt(800, 100)}) {
		t.Errorf("Bounds = %v", b)
	}
}

func TestRect_AreaExact(t *testing.T) {
	tests := []Rect{
		{Width: 3, Length: 7, Rotation: 0},
		{Width: 450, Length: 1200, Rotation: 123},
		{Width: 1, Length: 1, Rotation: 450},
	}
	for _, r := range tests {
		if r.Area() != Cm2(int64(r.Width)*int64(r.Length)) {
			t.Errorf("Area(%v) = %d", r, r.Area())
		}
	}
}

func TestRect_ContainsAnchor(t *testing.T) {
	for rot := Angle(0); rot < 3600; rot += 75 {
		r := Rect{Anchor: pt(5000, -3000), Width: 450, Length: 1500, Rotation: rot}
		if !r.Contains(r.Anchor) {
			t.Errorf("rotation %d: anchor not contained", rot)
		}
		// The point half way along the axis is strictly inside.
		mid := Rotate(r.Anchor.Add(pt(750, 0)), r.Anchor, rot)
		if !r.Polygon().ContainsStrict(mid) {
			t.Errorf("rotation %d: midpoint %v not strictly inside", rot, mid)
		}
		// A point behind the anchor (towards the pier) is outside.
		behind := Rotate(r.Anchor.Add(pt(-10, 0)), r.Anchor, rot)
		if r.Contains(behind) {
			t.Errorf("rotation %d: point behind the anchor contained", rot)
		}
	}
}

func TestRect_Validate(t *testing.T) {
	tests := []struct {
		name string
		r    Rect
		ok   bool
	}{
		{"ok", Rect{Anchor: pt(0, 0), Width: 400, Length: 1200, Rotation: 900}, true},
		{"zero width", Rect{Width: 0, Length: 1}, false},
		{"negative length", Rect{Width: 1, Length: -1}, false},
		{"anchor out of bounds", Rect{Anchor: pt(MaxCoord+1, 0), Width: 1, Length: 1}, false},
		{"far side out of bounds", Rect{Anchor: pt(MaxCoord-10, 0), Width: 2, Length: 100}, false},
		{"huge width", Rect{Width: 3 * MaxCoord, Length: 1}, false},
		{"bad rotation", Rect{Width: 1, Length: 1, Rotation: 3600}, false},
		{"negative rotation", Rect{Width: 1, Length: 1, Rotation: -1}, false},
	}
	for _, tt := range tests {
		err := tt.r.Validate()
		if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidGeometry)) {
			t.Errorf("%s: Validate = %v", tt.name, err)
		}
	}
}

func seg(ax, ay, bx, by Cm) Segment { return Segment{pt(ax, ay), pt(bx, by)} }

func TestSegmentsIntersect_Cases(t *testing.T) {
	tests := []struct {
		name          string
		s, u          Segment
		intersect     bool
		crossProperly bool
	}{
		{"crossing", seg(0, 0, 10, 10), seg(0, 10, 10, 0), true, true},
		{"T-touch", seg(0, 0, 10, 0), seg(5, 0, 5, 10), true, false},
		{"T-touch near miss", seg(0, 0, 10, 0), seg(5, 1, 5, 10), false, false},
		{"collinear overlap", seg(0, 0, 10, 0), seg(5, 0, 15, 0), true, false},
		{"collinear contained", seg(0, 0, 10, 0), seg(2, 0, 3, 0), true, false},
		{"collinear touching", seg(0, 0, 10, 0), seg(10, 0, 20, 0), true, false},
		{"collinear disjoint", seg(0, 0, 10, 0), seg(11, 0, 20, 0), false, false},
		{"parallel", seg(0, 0, 10, 0), seg(0, 1, 10, 1), false, false},
		{"shared endpoint", seg(0, 0, 10, 0), seg(10, 0, 10, 10), true, false},
		{"disjoint", seg(0, 0, 1, 1), seg(5, 5, 6, 7), false, false},
		{"lines cross outside", seg(0, 0, 1, 1), seg(10, 0, 9, 1), false, false},
		{"degenerate point on segment", seg(0, 0, 10, 0), seg(4, 0, 4, 0), true, false},
		{"degenerate point off segment", seg(0, 0, 10, 0), seg(4, 1, 4, 1), false, false},
	}
	for _, tt := range tests {
		if got := SegmentsIntersect(tt.s, tt.u); got != tt.intersect {
			t.Errorf("%s: SegmentsIntersect = %v", tt.name, got)
		}
		if got := SegmentsIntersect(tt.u, tt.s); got != tt.intersect {
			t.Errorf("%s: SegmentsIntersect not symmetric", tt.name)
		}
		if got := SegmentsCrossProperly(tt.s, tt.u); got != tt.crossProperly {
			t.Errorf("%s: SegmentsCrossProperly = %v", tt.name, got)
		}
	}
}

func TestSegmentsCrossProperly_Cases(t *testing.T) {
	if !SegmentsCrossProperly(seg(-5, 0, 5, 0), seg(0, -5, 0, 5)) {
		t.Error("plus sign")
	}
	if SegmentsCrossProperly(seg(0, 0, 5, 0), seg(0, -5, 0, 5)) {
		t.Error("endpoint on the other segment is not proper")
	}
}

func square(x, y, side Cm) Polygon {
	return Polygon{pt(x, y), pt(x+side, y), pt(x+side, y+side), pt(x, y+side)}
}

func TestPolygon_Validate(t *testing.T) {
	tests := []struct {
		name string
		p    Polygon
		ok   bool
	}{
		{"square", square(0, 0, 10), true},
		{"triangle", Polygon{pt(0, 0), pt(10, 0), pt(0, 10)}, true},
		{"L-shape", Polygon{pt(0, 0), pt(20, 0), pt(20, 10), pt(10, 10), pt(10, 20), pt(0, 20)}, true},
		{"collinear vertex", Polygon{pt(0, 0), pt(5, 0), pt(10, 0), pt(10, 10), pt(0, 10)}, true},
		{"too few", Polygon{pt(0, 0), pt(1, 1)}, false},
		{"bow-tie", Polygon{pt(0, 0), pt(10, 10), pt(10, 0), pt(0, 10)}, false},
		{"duplicate consecutive", Polygon{pt(0, 0), pt(10, 0), pt(10, 0), pt(10, 10)}, false},
		{"duplicate closing", Polygon{pt(0, 0), pt(10, 0), pt(10, 10), pt(0, 0)}, false},
		{"collinear only", Polygon{pt(0, 0), pt(5, 0), pt(10, 0)}, false},
		{"spike back-track", Polygon{pt(0, 0), pt(10, 0), pt(10, 10), pt(10, 5), pt(0, 10)}, false},
		{"touching itself at a vertex", Polygon{pt(0, 0), pt(10, 0), pt(5, 5), pt(10, 10), pt(0, 10), pt(5, 5)}, false},
		{"out of bounds", Polygon{pt(0, 0), pt(MaxCoord+1, 0), pt(0, 10)}, false},
	}
	for _, tt := range tests {
		err := tt.p.Validate()
		if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidGeometry)) {
			t.Errorf("%s: Validate = %v", tt.name, err)
		}
	}
	big := make(Polygon, MaxPolygonPoints+1)
	if err := big.Validate(); err == nil {
		t.Error("too many points accepted")
	}
}

func TestPolygon_Area2(t *testing.T) {
	sq := square(0, 0, 10)
	if sq.Area2() != 200 || sq.Area() != 100 {
		t.Errorf("square Area2 %d Area %d", sq.Area2(), sq.Area())
	}
	rev := Polygon{sq[3], sq[2], sq[1], sq[0]}
	if rev.Area2() != -200 || rev.Area() != 100 {
		t.Errorf("reversed square Area2 %d", rev.Area2())
	}
	l := Polygon{pt(0, 0), pt(20, 0), pt(20, 10), pt(10, 10), pt(10, 20), pt(0, 20)}
	if l.Area() != 300 {
		t.Errorf("L area %d", l.Area())
	}
	tri := Polygon{pt(0, 0), pt(3, 0), pt(0, 1)} // area 1.5 → 2 (half-even)
	if tri.Area2() != 3 || tri.Area() != 2 {
		t.Errorf("triangle Area2 %d Area %d", tri.Area2(), tri.Area())
	}
	tri2 := Polygon{pt(0, 0), pt(5, 0), pt(0, 1)} // 2.5 → 2
	if tri2.Area() != 2 {
		t.Errorf("triangle area %d", tri2.Area())
	}
	far := square(MaxCoord-10, MaxCoord-10, 10)
	if far.Area() != 100 {
		t.Errorf("square at the bounds area %d", far.Area())
	}
	whole := Polygon{pt(-MaxCoord, -MaxCoord), pt(MaxCoord, -MaxCoord), pt(MaxCoord, MaxCoord), pt(-MaxCoord, MaxCoord)}
	if whole.Area() != Cm2(4*int64(MaxCoord)*int64(MaxCoord)) {
		t.Errorf("whole map area %d", whole.Area())
	}
	if (Polygon{pt(0, 0)}).Area2() != 0 {
		t.Error("degenerate area")
	}
}

func TestPolygon_Contains(t *testing.T) {
	// Concave "C" with a notch on the right.
	c := Polygon{pt(0, 0), pt(30, 0), pt(30, 10), pt(10, 10), pt(10, 20), pt(30, 20), pt(30, 30), pt(0, 30)}
	diamond := Polygon{pt(10, 0), pt(20, 10), pt(10, 20), pt(0, 10)}
	tests := []struct {
		name             string
		p                Polygon
		q                Point
		contains, strict bool
	}{
		{"inside", c, pt(5, 5), true, true},
		{"outside", c, pt(40, 5), false, false},
		{"on edge", c, pt(15, 0), true, false},
		{"on vertex", c, pt(30, 0), true, false},
		{"notch", c, pt(20, 15), false, false},
		{"notch edge", c, pt(20, 10), true, false},
		{"ray through vertex inside", diamond, pt(5, 10), true, true},
		{"ray through vertex outside", diamond, pt(-5, 10), false, false},
		{"ray through two vertices", c, pt(-1, 10), false, false},
		{"ray along horizontal edge", c, pt(5, 10), true, true},
		{"above apex", diamond, pt(10, -1), false, false},
		{"apex", diamond, pt(10, 0), true, false},
	}
	for _, tt := range tests {
		if got := tt.p.Contains(tt.q); got != tt.contains {
			t.Errorf("%s: Contains = %v", tt.name, got)
		}
		if got := tt.p.ContainsStrict(tt.q); got != tt.strict {
			t.Errorf("%s: ContainsStrict = %v", tt.name, got)
		}
	}
	if (Polygon{}).Contains(pt(0, 0)) || (Polygon{}).ContainsStrict(pt(0, 0)) {
		t.Error("empty polygon contains")
	}
}

func TestPolygon_ContainsStrict(t *testing.T) {
	sq := square(0, 0, 10)
	for _, p := range []Point{pt(0, 0), pt(10, 10), pt(5, 0), pt(0, 5)} {
		if sq.ContainsStrict(p) {
			t.Errorf("boundary point %v strictly contained", p)
		}
	}
	if !sq.ContainsStrict(pt(1, 1)) {
		t.Error("interior point not strictly contained")
	}
}

func TestPolygon_IsConvex(t *testing.T) {
	tests := []struct {
		name string
		p    Polygon
		want bool
	}{
		{"square", square(0, 0, 10), true},
		{"collinear vertex", Polygon{pt(0, 0), pt(5, 0), pt(10, 0), pt(10, 10), pt(0, 10)}, true},
		{"L", Polygon{pt(0, 0), pt(20, 0), pt(20, 10), pt(10, 10), pt(10, 20), pt(0, 20)}, false},
		{"bow-tie", Polygon{pt(0, 0), pt(10, 10), pt(10, 0), pt(0, 10)}, false},
		{"pentagram", Polygon{pt(0, -100), pt(59, 81), pt(-95, -31), pt(95, -31), pt(-59, 81)}, false},
	}
	for _, tt := range tests {
		if got := tt.p.IsConvex(); got != tt.want {
			t.Errorf("%s: IsConvex = %v", tt.name, got)
		}
	}
}

func TestPolygonsIntersect(t *testing.T) {
	a := square(0, 0, 10)
	tests := []struct {
		name string
		b    Polygon
		want bool
	}{
		{"overlap", square(5, 5, 10), true},
		{"touch edge", square(10, 0, 10), true},
		{"touch corner", square(10, 10, 10), true},
		{"nested", square(2, 2, 2), true},
		{"apart", square(20, 20, 5), false},
		{"near miss", square(11, 0, 10), false},
	}
	for _, tt := range tests {
		if PolygonsIntersect(a, tt.b) != tt.want || PolygonsIntersect(tt.b, a) != tt.want {
			t.Errorf("%s: PolygonsIntersect = %v", tt.name, PolygonsIntersect(a, tt.b))
		}
	}
	if PolygonsIntersect(a, nil) {
		t.Error("nil polygon")
	}
}

func TestRectsOverlap_Cases(t *testing.T) {
	base := Rect{Anchor: pt(0, 0), Width: 400, Length: 1200, Rotation: 900}
	tests := []struct {
		name string
		b    Rect
		want bool
	}{
		{"identical", base, true},
		{"nested", Rect{Anchor: pt(0, 100), Width: 100, Length: 100, Rotation: 900}, true},
		{"adjacent berth sharing a side", Rect{Anchor: pt(400, 0), Width: 400, Length: 1200, Rotation: 900}, false},
		{"adjacent with 1 cm overlap", Rect{Anchor: pt(399, 0), Width: 400, Length: 1200, Rotation: 900}, true},
		{"corner touch", Rect{Anchor: pt(400, 1200), Width: 400, Length: 1200, Rotation: 900}, false},
		{"facing berths sharing the far side", Rect{Anchor: pt(0, 2400), Width: 400, Length: 1200, Rotation: 2700}, false},
		{"rotated 45° crossing", Rect{Anchor: pt(-500, 100), Width: 100, Length: 2000, Rotation: 450}, true},
		{"rotated 45° near corner but outside", Rect{Anchor: pt(300, -300), Width: 100, Length: 300, Rotation: 3150}, false},
		{"far apart", Rect{Anchor: pt(100_000, 100_000), Width: 400, Length: 1200, Rotation: 0}, false},
		{"opposite direction same footprint", Rect{Anchor: pt(0, 1200), Width: 400, Length: 1200, Rotation: 2700}, true},
	}
	for _, tt := range tests {
		if got := RectsOverlap(base, tt.b); got != tt.want {
			t.Errorf("%s: RectsOverlap = %v", tt.name, got)
		}
		if got := RectsOverlap(tt.b, base); got != tt.want {
			t.Errorf("%s: RectsOverlap not symmetric", tt.name)
		}
	}
}

func TestConvexOverlap_Degenerate(t *testing.T) {
	line := Polygon{pt(0, 0), pt(5, 0), pt(10, 0)}
	if ConvexOverlap(square(0, -5, 10), line) || ConvexOverlap(line, line) || ConvexOverlap(nil, square(0, 0, 1)) {
		t.Error("degenerate polygons have no interior")
	}
}

func TestSegmentIntersectsRect_Cases(t *testing.T) {
	r := Rect{Anchor: pt(0, 0), Width: 200, Length: 1000, Rotation: 0} // box x 0..1000, y -100..100
	tests := []struct {
		name string
		s    Segment
		want bool
	}{
		{"crossing", seg(500, -500, 500, 500), true},
		{"inside", seg(100, 0, 200, 0), true},
		{"touching side", seg(0, 100, 1000, 100), true},
		{"touching corner", seg(1000, 100, 2000, 2000), true},
		{"endpoint on edge", seg(500, 100, 500, 500), true},
		{"outside", seg(0, 101, 1000, 101), false},
		{"passes near corner", seg(1001, -500, 1001, 500), false},
	}
	for _, tt := range tests {
		if got := SegmentIntersectsRect(tt.s, r); got != tt.want {
			t.Errorf("%s: SegmentIntersectsRect = %v", tt.name, got)
		}
	}
}

func TestCirclesOverlap_Cases(t *testing.T) {
	a := Circle{Center: pt(0, 0), Radius: 500}
	tests := []struct {
		name string
		b    Circle
		want bool
	}{
		{"same", a, true},
		{"tangent", Circle{Center: pt(1000, 0), Radius: 500}, false},
		{"overlap by 1 cm", Circle{Center: pt(999, 0), Radius: 500}, true},
		{"diagonal tangent", Circle{Center: pt(600, 800), Radius: 500}, false},
		{"nested", Circle{Center: pt(10, 10), Radius: 10}, true},
		{"apart", Circle{Center: pt(5000, 0), Radius: 500}, false},
	}
	for _, tt := range tests {
		if CirclesOverlap(a, tt.b) != tt.want || CirclesOverlap(tt.b, a) != tt.want {
			t.Errorf("%s: CirclesOverlap = %v", tt.name, CirclesOverlap(a, tt.b))
		}
	}
}

func FuzzPolygonContains(f *testing.F) {
	f.Add(int64(0), int64(0), int64(10), int64(0), int64(10), int64(10), int64(0), int64(10), int64(5), int64(5))
	f.Add(int64(0), int64(0), int64(10), int64(10), int64(10), int64(0), int64(0), int64(10), int64(5), int64(5))
	f.Add(int64(-1), int64(-1), int64(-1), int64(-1), int64(3), int64(3), int64(3), int64(3), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, x0, y0, x1, y1, x2, y2, x3, y3, px, py int64) {
		c := func(v int64) Cm { return Cm(v % int64(MaxCoord)) }
		p := Polygon{pt(c(x0), c(y0)), pt(c(x1), c(y1)), pt(c(x2), c(y2)), pt(c(x3), c(y3))}
		q := pt(c(px), c(py))
		in := p.Contains(q)
		if in && !p.Bounds().Contains(q) {
			t.Fatalf("%v contains %v outside its bounds", p, q)
		}
		if p.ContainsStrict(q) && !in {
			t.Fatal("strict without inclusive")
		}
		_ = p.Validate()
		_ = p.IsConvex()
		_ = p.Area()
	})
}
