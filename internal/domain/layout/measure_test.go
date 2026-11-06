package layout

import (
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/geo"
)

func TestISqrt(t *testing.T) {
	for _, c := range []struct{ n, want uint64 }{{0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 2}, {15, 3}, {16, 4}, {1 << 62, 1 << 31}, {^uint64(0), 4294967295}} {
		if got := ISqrt(c.n); got != c.want {
			t.Errorf("ISqrt(%d) = %d, want %d", c.n, got, c.want)
		}
	}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.Uint64().Draw(t, "n")
		r := ISqrt(n)
		if hi, lo := mul128(r, r); hi != 0 || lo > n {
			t.Fatalf("ISqrt(%d) = %d too large", n, r)
		}
		if hi, lo := mul128(r+1, r+1); hi == 0 && lo <= n {
			t.Fatalf("ISqrt(%d) = %d too small", n, r)
		}
	})
}

func mul128(a, b uint64) (uint64, uint64) {
	if a == 1<<32 && b == 1<<32 {
		return 1, 0
	}
	hi := uint64(0)
	lo := a * b
	if a != 0 && lo/a != b {
		hi = 1
	}
	return hi, lo
}

func TestAngleOf_MatchesTable(t *testing.T) {
	for a := range geo.Angle(3600) {
		if got := AngleOf(geo.Cos(a), geo.Sin(a)); got != a {
			t.Fatalf("AngleOf(dir %d) = %d", a, got)
		}
		// Small vectors round to the nearest tenth.
		x, y := geo.DivRoundHalfEven(geo.Cos(a)*1000, geo.TrigScale), geo.DivRoundHalfEven(geo.Sin(a)*1000, geo.TrigScale)
		if got := AngleOf(x, y); absAngle(int64(got)-int64(a)) > 1 {
			t.Fatalf("AngleOf(%d,%d) = %d, want about %d", x, y, got, a)
		}
	}
	for _, c := range []struct {
		dx, dy int64
		want   geo.Angle
	}{{1, 0, 0}, {0, 1, 900}, {-1, 0, 1800}, {0, -1, 2700}, {1, 1, 450}, {-5, -5, 2250}, {0, 0, 0}} {
		if got := AngleOf(c.dx, c.dy); got != c.want {
			t.Errorf("AngleOf(%d,%d) = %d, want %d", c.dx, c.dy, got, c.want)
		}
	}
}

func absAngle(d int64) int64 {
	d = (d%3600 + 3600) % 3600
	return min(d, 3600-d)
}

func TestPolylineLength_And_PointAt(t *testing.T) {
	pts := []geo.Point{pt(0, 0), pt(300, 400), pt(300, 1400)}
	if l := PolylineLength(pts); l != 1500 {
		t.Fatalf("length %d", l)
	}
	if l := PolylineLength([]geo.Point{pt(0, 0), pt(1, 1)}); l != 1 { // √2 rounds to 1
		t.Fatalf("diagonal %d", l)
	}
	p, a, err := PointAt(pts, 250)
	if err != nil || p != pt(150, 200) || a != AngleOf(3, 4) {
		t.Fatalf("PointAt 250 = %v %d %v", p, a, err)
	}
	p, a, err = PointAt(pts, 500)
	if err != nil || p != pt(300, 400) || a != 900 {
		t.Fatalf("PointAt vertex = %v %d %v", p, a, err)
	}
	if p, _, err := PointAt(pts, 1500); err != nil || p != pt(300, 1400) {
		t.Fatalf("PointAt end = %v %v", p, err)
	}
	if _, _, err := PointAt(pts, 1501); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("beyond: %v", err)
	}
	r := ShrinkRect(geo.Rect{Anchor: pt(100, 100), Width: 300, Length: 1000, Rotation: 900}, 1)
	if r.Anchor != pt(100, 101) || r.Width != 298 || r.Length != 998 {
		t.Fatalf("shrink %+v", r)
	}
}

func TestPriceClassFor_Bands(t *testing.T) {
	r := DefaultPriceClassRule()
	for _, c := range []struct {
		w    geo.Cm
		want string
	}{{100, "L8"}, {300, "L8"}, {301, "L10"}, {400, "L12"}, {450, "L14"}, {500, "L16"}, {550, "L18"}, {650, "L20"}, {651, "L25"}, {2000, "L25"}} {
		if got := PriceClassFor(c.w, r); got != c.want {
			t.Errorf("width %d: %s, want %s", c.w, got, c.want)
		}
	}
	if got := PriceClassFor(500, PriceClassRule{Fallback: "X"}); got != "X" {
		t.Fatalf("empty rule: %s", got)
	}
}

func TestFormatE7(t *testing.T) {
	for _, c := range []struct {
		v    int64
		want string
	}{{0, "0.0000000"}, {123456789, "12.3456789"}, {-1, "-0.0000001"}, {-1800000000, "-180.0000000"}, {407000005, "40.7000005"}} {
		if got := FormatE7(c.v); got != c.want {
			t.Errorf("FormatE7(%d) = %s, want %s", c.v, got, c.want)
		}
		if back, err := ParseE7(c.want); err != nil || back != c.v {
			t.Errorf("ParseE7(%s) = %d %v", c.want, back, err)
		}
	}
	for _, c := range []struct {
		s    string
		want int64
	}{{"12.34567895", 123456790}, {"12.34567885", 123456788}, {"1e-7", 1}, {"40", 400000000}, {"-0.00000005", 0}} {
		if got, err := ParseE7(c.s); err != nil || got != c.want {
			t.Errorf("ParseE7(%s) = %d %v, want %d", c.s, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "1/3", "181", "1e400"} {
		if _, err := ParseE7(bad); err == nil {
			t.Errorf("ParseE7(%q) accepted", bad)
		}
	}
}

// realisticGeoref places a 6 × 4 km map around lat 40.9, lon 9.6, with the map rotated by a random angle.
func realisticGeoref(t *rapid.T) (Georef, [3]ControlPoint) {
	lat0 := rapid.Int64Range(-600_000_000, 600_000_000).Draw(t, "lat0")
	lon0 := rapid.Int64Range(-1_700_000_000, 1_700_000_000).Draw(t, "lon0")
	rot := geo.Angle(rapid.Int64Range(0, 3599).Draw(t, "rot"))
	// About 0.9 e7-units of latitude per cm; longitude scaled by 1.3 (mid latitudes).
	toGeo := func(p geo.Point) (int64, int64) {
		q := geo.Rotate(p, geo.Point{}, rot)
		return lat0 - geo.DivRoundHalfEven(int64(q.Y)*9, 10), lon0 + geo.DivRoundHalfEven(int64(q.X)*117, 100)
	}
	var cps [3]ControlPoint
	for i, p := range []geo.Point{pt(0, 0), pt(600000, 0), pt(0, 400000)} {
		la, lo := toGeo(p)
		cps[i] = ControlPoint{At: p, LatE7: la, LonE7: lo}
	}
	g, err := NewGeoref(cps)
	if err != nil {
		t.Fatal(err)
	}
	return g, cps
}

func TestGeoref_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g, cps := realisticGeoref(t)
		for _, cp := range cps {
			if la, lo := g.ToWGS84(cp.At); la != cp.LatE7 || lo != cp.LonE7 {
				t.Fatalf("control point %+v maps to %d %d", cp, la, lo)
			}
		}
		p := pt(geo.Cm(rapid.Int64Range(0, 600000).Draw(t, "x")), geo.Cm(rapid.Int64Range(0, 400000).Draw(t, "y")))
		back := g.FromWGS84(g.ToWGS84(p))
		if d := back.Sub(p); d.X < -1 || d.X > 1 || d.Y < -1 || d.Y > 1 {
			t.Fatalf("round trip %v -> %v", p, back)
		}
	})
}

func TestGeoref_Collinear(t *testing.T) {
	_, err := NewGeoref([3]ControlPoint{{At: pt(0, 0)}, {At: pt(100, 100), LatE7: 1}, {At: pt(200, 200), LonE7: 1}})
	if !errors.Is(err, ErrCollinear) {
		t.Fatalf("collinear map points: %v", err)
	}
	_, err = NewGeoref([3]ControlPoint{{At: pt(0, 0)}, {At: pt(100, 0)}, {At: pt(0, 100)}})
	if !errors.Is(err, ErrCollinear) {
		t.Fatalf("degenerate geographic points: %v", err)
	}
}

func TestFootprint_DisjointCellsImplyNoOverlap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mode := rapid.SampledFrom([]MooringMode{ModeSternTo, ModeAlongside}).Draw(t, "mode")
		a := Area{
			Kind: AreaLinear, Length: geo.Cm(rapid.Int64Range(500, 10000).Draw(t, "len")), Mode: mode,
			Cell: geo.Cm(rapid.SampledFrom([]int64{10, 25, 50, 100}).Draw(t, "cell")), Margin: geo.Cm(rapid.Int64Range(0, 120).Draw(t, "margin")),
		}
		type placed struct {
			lo, hi geo.Cm
			c      Cells
		}
		var ps []placed
		for i := range 2 {
			v := Vessel{LOA: geo.Cm(rapid.Int64Range(100, 2000).Draw(t, "loa")), Beam: geo.Cm(rapid.Int64Range(50, 600).Draw(t, "beam"))}
			x := geo.Cm(rapid.Int64Range(0, int64(a.Length)/10).Draw(t, "x")) * 10
			c, err := Footprint(v, Placement{X: x}, a)
			if err != nil {
				return
			}
			w := v.Beam
			if mode == ModeAlongside {
				w = v.LOA
			}
			ps = append(ps, placed{x, x + w, c})
			_ = i
		}
		disjoint := ps[0].c.Last < ps[1].c.First || ps[1].c.Last < ps[0].c.First
		physical := ps[0].lo < ps[1].hi && ps[1].lo < ps[0].hi
		if disjoint && physical {
			t.Fatalf("disjoint cells %+v but hulls overlap", ps)
		}
	})
	rapid.Check(t, func(t *rapid.T) {
		cols, rows := rapid.IntRange(1, 8).Draw(t, "cols"), rapid.IntRange(1, 8).Draw(t, "rows")
		u := make([]bool, cols*rows)
		for i := range u {
			u[i] = true
		}
		a := Area{Kind: AreaSurface, Cell: 100, Margin: geo.Cm(rapid.Int64Range(0, 80).Draw(t, "m")), Cols: cols, Rows: rows, Usable: u}
		type placed struct {
			box geo.Box
			c   Cells
		}
		var ps []placed
		for range 2 {
			v := Vessel{LOA: geo.Cm(rapid.Int64Range(50, 400).Draw(t, "loa")), Beam: geo.Cm(rapid.Int64Range(30, 200).Draw(t, "beam"))}
			p := Placement{
				X: geo.Cm(rapid.Int64Range(0, int64(cols)*100).Draw(t, "x")), Y: geo.Cm(rapid.Int64Range(0, int64(rows)*100).Draw(t, "y")),
				Rotation: geo.Angle(rapid.SampledFrom([]int64{0, 900, 1800, 2700}).Draw(t, "rot")),
			}
			c, err := Footprint(v, p, a)
			if err != nil {
				return
			}
			w, h := v.LOA, v.Beam
			if p.Rotation == 900 || p.Rotation == 2700 {
				w, h = h, w
			}
			ps = append(ps, placed{geo.Box{Min: pt(p.X, p.Y), Max: pt(p.X+w, p.Y+h)}, c})
		}
		shared := false
		for _, i := range ps[0].c.Indexes {
			for _, j := range ps[1].c.Indexes {
				shared = shared || i == j
			}
		}
		b0, b1 := ps[0].box, ps[1].box
		overlap := b0.Min.X < b1.Max.X && b1.Min.X < b0.Max.X && b0.Min.Y < b1.Max.Y && b1.Min.Y < b0.Max.Y
		if !shared && overlap {
			t.Fatalf("disjoint cells but hulls overlap: %+v", ps)
		}
	})
}

// Every point of a footprint lies in an occupied cell.
func TestRapid_FootprintCoversRect(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cols, rows := rapid.IntRange(1, 10).Draw(t, "cols"), rapid.IntRange(1, 10).Draw(t, "rows")
		cell := geo.Cm(rapid.SampledFrom([]int64{50, 100, 120}).Draw(t, "cell"))
		u := make([]bool, cols*rows)
		for i := range u {
			u[i] = true
		}
		a := Area{Kind: AreaSurface, Cell: cell, Margin: geo.Cm(rapid.Int64Range(0, 80).Draw(t, "m")), Cols: cols, Rows: rows, Usable: u}
		v := Vessel{LOA: geo.Cm(rapid.Int64Range(10, 500).Draw(t, "loa")), Beam: geo.Cm(rapid.Int64Range(10, 300).Draw(t, "beam"))}
		p := Placement{
			X: geo.Cm(rapid.Int64Range(0, int64(cols)*int64(cell)).Draw(t, "x")), Y: geo.Cm(rapid.Int64Range(0, int64(rows)*int64(cell)).Draw(t, "y")),
			Rotation: geo.Angle(rapid.SampledFrom([]int64{0, 900, 1800, 2700}).Draw(t, "rot")),
		}
		c, err := Footprint(v, p, a)
		if err != nil {
			return
		}
		w, h := v.LOA+a.Margin, v.Beam+a.Margin
		if p.Rotation == 900 || p.Rotation == 2700 {
			w, h = h, w
		}
		occupied := map[int]bool{}
		for _, i := range c.Indexes {
			occupied[i] = true
		}
		// Sample the interior points (half-cm grid via doubled coordinates) and check their cells.
		for _, fx := range []int64{0, 1, 2} {
			for _, fy := range []int64{0, 1, 2} {
				x2 := 2*int64(p.X) + fx*int64(w) // doubled: x, middle, end
				y2 := 2*int64(p.Y) + fy*int64(h)
				if fx == 2 {
					x2--
				}
				if fy == 2 {
					y2--
				}
				col, row := floorDiv(x2, 2*int64(cell)), floorDiv(y2, 2*int64(cell))
				if !occupied[int(row)*cols+int(col)] {
					t.Fatalf("point (%d,%d)/2 of %+v not covered by %v", x2, y2, p, c.Indexes)
				}
			}
		}
	})
	rapid.Check(t, func(t *rapid.T) {
		a := Area{Kind: AreaLinear, Length: 5000, Mode: ModeSternTo, Cell: geo.Cm(rapid.SampledFrom([]int64{10, 50, 70}).Draw(t, "c")), Margin: 50}
		v := Vessel{LOA: 1000, Beam: geo.Cm(rapid.Int64Range(1, 900).Draw(t, "b"))}
		x := geo.Cm(rapid.Int64Range(3, 400).Draw(t, "x")) * 10
		c, err := Footprint(v, Placement{X: x}, a)
		if err != nil {
			return
		}
		w := v.Beam + a.Margin
		if int64(x) < int64(c.First)*int64(a.Cell) || int64(x+w) > int64(c.Last+1)*int64(a.Cell) {
			t.Fatalf("interval [%d,%d) not within cells %d..%d", x, x+w, c.First, c.Last)
		}
	})
}
