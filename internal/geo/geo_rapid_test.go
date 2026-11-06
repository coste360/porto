package geo

import (
	"testing"

	"pgregory.net/rapid"
)

// coordRange keeps generated shapes small enough to translate without leaving MaxCoord.
const coordRange = 10_000_000

func genPoint(t *rapid.T, label string) Point {
	return pt(
		Cm(rapid.Int64Range(-coordRange, coordRange).Draw(t, label+"_x")),
		Cm(rapid.Int64Range(-coordRange, coordRange).Draw(t, label+"_y")),
	)
}

func genRect(t *rapid.T, label string) Rect {
	return Rect{
		Anchor:   genPoint(t, label+"_anchor"),
		Width:    Cm(rapid.Int64Range(1, 100_000).Draw(t, label+"_w")),
		Length:   Cm(rapid.Int64Range(1, 100_000).Draw(t, label+"_l")),
		Rotation: Angle(rapid.Int32Range(0, 3599).Draw(t, label+"_rot")),
	}
}

// genNearbyRects draws two rectangles close enough to overlap often.
func genNearbyRects(t *rapid.T) (Rect, Rect) {
	a := genRect(t, "a")
	b := genRect(t, "b")
	b.Anchor = a.Anchor.Add(pt(
		Cm(rapid.Int64Range(-200_000, 200_000).Draw(t, "dx")),
		Cm(rapid.Int64Range(-200_000, 200_000).Draw(t, "dy")),
	))
	return a, b
}

func TestRotate_FullTurnIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p, c := genPoint(t, "p"), genPoint(t, "c")
		q := p
		for range 36 {
			q = Rotate(q, c, 100)
		}
		// Each step rounds by at most half a centimetre per axis, plus the table error (≤ 1e-9 × distance).
		dist := max(abs(int64(p.X-c.X)), abs(int64(p.Y-c.Y)))
		bound := 36 * (1 + 2*dist/TrigScale + 1)
		if abs(int64(q.X-p.X)) > bound || abs(int64(q.Y-p.Y)) > bound {
			t.Fatalf("after a full turn %v → %v (bound %d)", p, q, bound)
		}
	})
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRapid_RotatePreservesDistance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genPoint(t, "c")
		p := c.Add(pt(Cm(rapid.Int64Range(-100_000, 100_000).Draw(t, "dx")), Cm(rapid.Int64Range(-100_000, 100_000).Draw(t, "dy"))))
		a := Angle(rapid.Int32Range(0, 3599).Draw(t, "a"))
		q := Rotate(p, c, a)
		// |q-c| differs from |p-c| by at most the rounding (√2/2 cm) and the table error.
		d0, d1 := Dist2(p, c), Dist2(q, c)
		r := isqrt(max(d0, d1))
		if abs(d1-d0) > 2*r+2 {
			t.Fatalf("distance changed: %d → %d", d0, d1)
		}
	})
}

func isqrt(v int64) int64 {
	r := int64(0)
	for b := int64(1) << 31; b > 0; b >>= 1 {
		if (r+b)*(r+b) <= v {
			r += b
		}
	}
	return r
}

func TestRapid_RectsOverlapSymmetric(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genNearbyRects(t)
		if RectsOverlap(a, b) != RectsOverlap(b, a) {
			t.Fatalf("asymmetric: %+v %+v", a, b)
		}
	})
}

func TestRapid_RectOverlapsItself(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := genRect(t, "r")
		if r.Polygon().Area2() == 0 {
			t.Skip("degenerate after rounding (sub-centimetre width)")
		}
		if !RectsOverlap(r, r) {
			t.Fatalf("%+v does not overlap itself", r)
		}
	})
}

func TestRapid_TranslationInvariance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genNearbyRects(t)
		d := genPoint(t, "d")
		a2, b2 := a, b
		a2.Anchor, b2.Anchor = a.Anchor.Add(d), b.Anchor.Add(d)
		if RectsOverlap(a, b) != RectsOverlap(a2, b2) {
			t.Fatalf("translation by %v changed overlap of %+v %+v", d, a, b)
		}
	})
}

func TestRapid_BoundsContainCorners(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := genRect(t, "r")
		b := r.Bounds()
		for _, c := range r.Corners() {
			if !b.Contains(c) {
				t.Fatalf("corner %v outside bounds %v", c, b)
			}
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("generated rect invalid: %v", err)
		}
	})
}

func TestRapid_ContainsImpliesBoundsContains(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := genRect(t, "r")
		p := r.Anchor.Add(pt(Cm(rapid.Int64Range(-150_000, 150_000).Draw(t, "dx")), Cm(rapid.Int64Range(-150_000, 150_000).Draw(t, "dy"))))
		if r.Contains(p) && !r.Bounds().Contains(p) {
			t.Fatalf("%v inside %+v but outside its bounds", p, r)
		}
		if r.Polygon().ContainsStrict(p) && !r.Contains(p) {
			t.Fatal("strict without inclusive")
		}
	})
}

func TestRapid_SeparatedBoxesNeverOverlap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genNearbyRects(t)
		if !a.Bounds().Intersects(b.Bounds()) && RectsOverlap(a, b) {
			t.Fatalf("disjoint bounds but overlap: %+v %+v", a, b)
		}
	})
}

func TestRapid_AxisAlignedOverlapMatchesBoxMath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genNearbyRects(t)
		a.Rotation = Angle(rapid.SampledFrom([]int32{0, 900, 1800, 2700}).Draw(t, "rot_a"))
		b.Rotation = Angle(rapid.SampledFrom([]int32{0, 900, 1800, 2700}).Draw(t, "rot_b"))
		ba, bb := a.Bounds(), b.Bounds()
		// Open-interval overlap of the boxes (touching is not overlap); a zero-thickness box has no interior.
		want := ba.Min.X < bb.Max.X && bb.Min.X < ba.Max.X && ba.Min.Y < bb.Max.Y && bb.Min.Y < ba.Max.Y &&
			ba.Min.X < ba.Max.X && ba.Min.Y < ba.Max.Y && bb.Min.X < bb.Max.X && bb.Min.Y < bb.Max.Y
		if got := RectsOverlap(a, b); got != want {
			t.Fatalf("RectsOverlap = %v, box math = %v: %+v %+v", got, want, a, b)
		}
	})
}

func TestRapid_PolygonAreaSignFlipsOnReverse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(3, 12).Draw(t, "n")
		p := make(Polygon, n)
		for i := range p {
			p[i] = genPoint(t, "p")
		}
		rev := make(Polygon, n)
		for i := range p {
			rev[i] = p[n-1-i]
		}
		if p.Area2() != -rev.Area2() || p.Area() != rev.Area() {
			t.Fatalf("Area2 %d vs reversed %d", p.Area2(), rev.Area2())
		}
		// Rotating the starting vertex does not change the area.
		shift := append(append(Polygon{}, p[1:]...), p[0])
		if shift.Area2() != p.Area2() {
			t.Fatalf("start vertex changed Area2: %d vs %d", shift.Area2(), p.Area2())
		}
	})
}

func TestRapid_RectAreaMatchesPolygon(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := genRect(t, "r")
		r.Width = Cm(rapid.Int64Range(100, 100_000).Draw(t, "w"))
		r.Length = Cm(rapid.Int64Range(100, 100_000).Draw(t, "l"))
		// Rounding each corner by ≤ √2/2 cm changes the area by at most about the perimeter.
		diff := abs(int64(r.Polygon().Area()) - int64(r.Area()))
		if diff > 2*int64(r.Width+r.Length)+4 {
			t.Fatalf("polygon area %d vs exact %d", r.Polygon().Area(), r.Area())
		}
		if !r.Polygon().IsConvex() {
			t.Fatalf("rect polygon not convex: %v", r.Polygon())
		}
	})
}

func TestRapid_SquaresOverlapMatchesContainment(t *testing.T) {
	// Two convex polygons whose interiors overlap must intersect inclusively too.
	rapid.Check(t, func(t *rapid.T) {
		a, b := genNearbyRects(t)
		if RectsOverlap(a, b) && !PolygonsIntersect(a.Polygon(), b.Polygon()) {
			t.Fatalf("overlap without intersection: %+v %+v", a, b)
		}
	})
}
