package layout

import (
	"fmt"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/geo"
)

func pt(x, y geo.Cm) geo.Point { return geo.Point{X: x, Y: y} }

func rectPoly(x0, y0, x1, y1 geo.Cm) geo.Polygon {
	return geo.Polygon{pt(x0, y0), pt(x1, y0), pt(x1, y1), pt(x0, y1)}
}

// baseMap is a small valid map without any issue: a quay, a pier with two berths and a pedestal, a linear and a
// surface area, a land resource, a buoy and a kit symbol.
func baseMap(t testing.TB) Map {
	t.Helper()
	pier := Structure{
		Ref: "pier", Kind: StructurePier, Code: "P", Points: []geo.Point{pt(5000, 1000), pt(5000, 6000)}, Width: 300,
		Parent: "quay", Sides: []Side{SideLeft, SideRight}, Depth: []DepthSpan{{From: 0, To: 5000, Depth: 400}},
	}
	r1, err := PlaceAlong(&pier, SideLeft, 0, 300, 1000)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := PlaceAlong(&pier, SideLeft, 300, 300, 1000)
	if err != nil {
		t.Fatal(err)
	}
	berth := func(ref Ref, code string, r geo.Rect) Berth {
		return Berth{
			Ref: ref, Code: code, Structure: "pier", Side: SideLeft, Rect: r, Mooring: MooringLine, MaxLOA: 1000, MaxBeam: 250,
			Pedestal: "ped", PedestalSocket: "S1", PriceClass: "L8", Active: true,
		}
	}
	return Map{
		Width: 10000, Height: 10000, Grid: 50,
		Water: []geo.Polygon{rectPoly(0, 1000, 10000, 10000)},
		Structures: []Structure{
			{Ref: "quay", Kind: StructureQuay, Code: "Q", Points: []geo.Point{pt(0, 1000), pt(10000, 1000)}, Sides: []Side{SideRight}},
			pier,
		},
		Berths: []Berth{
			berth("b1", "P-1", r1), berth("b2", "P-2", r2),
			{
				Ref: "buoy", Code: "BOA-1", Rect: geo.Rect{Anchor: pt(2000, 4000), Width: 300, Length: 900}, Mooring: MooringBuoy,
				SwingRadius: 500, MaxLOA: 900, MaxBeam: 300, Depth: 300, PriceClass: "L8", Active: true, Pedestal: "ped",
			},
		},
		Areas: []DynamicArea{
			{Ref: "lin", Code: "T-1", Kind: AreaLinear, Structure: "pier", Side: SideRight, From: 0, To: 2000, Mode: ModeSternTo, Cell: 50, Margin: 50, MaxBeam: 800},
			{Ref: "surf", Code: "S-1", Kind: AreaSurface, Outline: rectPoly(100, 7000, 2100, 9000), Cell: 100, Margin: 60},
		},
		LandResources: []LandResource{{Ref: "land", Code: "PK-1", Kind: LandParking, Rect: geo.Rect{Anchor: pt(8000, 8000), Width: 250, Length: 500, Rotation: 900}}},
		Pedestals:     []Pedestal{{Ref: "ped", Code: "C1", At: pt(5100, 1300), Sockets: []Socket{{Code: "S1", Kind: "power", Amps: 16}}}},
		Decorations:   []Decoration{{Ref: "deco", Kind: DecorationSymbol, Asset: "harbour_office", At: pt(500, 500), Size: 300}},
	}
}

func codes(list []Issue) []string {
	var out []string
	for _, is := range list {
		out = append(out, is.Code)
	}
	return out
}

func hasIssue(list []Issue, code string) bool {
	return slices.ContainsFunc(list, func(i Issue) bool { return i.Code == code })
}

func TestValidateMap_BaseIsClean(t *testing.T) {
	rep := ValidateMap(baseMap(t), DefaultSettings())
	if len(rep.Errors)+len(rep.Warnings) != 0 {
		t.Fatalf("errors %v warnings %v (%+v)", codes(rep.Errors), codes(rep.Warnings), rep)
	}
}

// One row per §2.3 code: the mutation triggers it, and the base map (the other direction) does not have it.
func TestValidateMap_Scenario(t *testing.T) {
	cases := []struct {
		code   string
		sev    Severity
		mutate func(m *Map, s *Settings)
	}{
		{CodeTooManyElements, SeverityError, func(_ *Map, s *Settings) { s.MaxElements = 3 }},
		{CodeInvalidGeometry, SeverityError, func(m *Map, _ *Settings) { m.Berths[0].Rect.Width = 0 }},
		{CodeInvalidGeometry, SeverityError, func(m *Map, _ *Settings) { m.Areas[1].GridRotation = 450 }},
		{CodeInvalidGeometry, SeverityError, func(m *Map, _ *Settings) { m.Berths[2].SwingRadius = 0 }},
		{CodeElementOutsideMap, SeverityError, func(m *Map, _ *Settings) { m.Berths[0].Rect.Anchor.X = 9500 }},
		{CodeElementOutsideMap, SeverityError, func(m *Map, _ *Settings) { m.Berths[2].Rect.Anchor = pt(200, 4000) }},
		{CodeDuplicateCode, SeverityError, func(m *Map, _ *Settings) { m.Berths[1].Code = "P-1" }},
		{CodeDuplicateCode, SeverityError, func(m *Map, _ *Settings) { m.LandResources[0].Code = "T-1" }},
		{CodeBerthOverlap, SeverityError, func(m *Map, _ *Settings) { m.Berths[1].Rect.Anchor.Y = m.Berths[0].Rect.Anchor.Y + 100 }},
		{CodeOccupiableOverlap, SeverityError, func(m *Map, _ *Settings) { m.LandResources[0].Rect.Anchor = pt(5300, 1100) }},
		{CodeOccupiableOverlap, SeverityError, func(m *Map, _ *Settings) { m.Areas[1].Outline = rectPoly(4000, 1100, 4500, 1500) }},
		{CodeAreaCellsOutside, SeverityError, func(m *Map, _ *Settings) { m.Areas[0].To = 6000 }},
		{CodeAreaCellsOutside, SeverityError, func(m *Map, _ *Settings) { m.Areas[0].Side = SideHead }},
		{CodeAreaCellsOutside, SeverityError, func(m *Map, _ *Settings) { m.Areas[1].Blocked = []int{400} }},
		{CodeLOAExceedsLength, SeverityError, func(m *Map, _ *Settings) { m.Berths[0].MaxLOA = 1051 }},
		{CodeDanglingReference, SeverityError, func(m *Map, _ *Settings) { m.Berths[0].Structure = "nope" }},
		{CodeDanglingReference, SeverityError, func(m *Map, _ *Settings) { m.Structures[1].Parent = "pier" }},
		{CodePedestalSocketUnknown, SeverityError, func(m *Map, _ *Settings) { m.Berths[0].PedestalSocket = "S9" }},
		{CodeAssetUnknown, SeverityError, func(m *Map, _ *Settings) { m.Decorations[0].Asset = "unicorn" }},
		{CodeBerthOutsideWater, SeverityWarning, func(m *Map, _ *Settings) { m.Water = []geo.Polygon{rectPoly(0, 5000, 10000, 10000)} }},
		{CodeBerthUnattached, SeverityWarning, func(m *Map, _ *Settings) { m.Berths[0].Structure = "" }},
		{CodeBerthUnattached, SeverityWarning, func(m *Map, _ *Settings) { m.Berths[0].Rect.Anchor.X += 51 }},
		{CodeDepthMissing, SeverityWarning, func(m *Map, _ *Settings) { m.Structures[1].Depth = nil }},
		{CodePriceClassMissing, SeverityWarning, func(m *Map, _ *Settings) { m.Berths[0].PriceClass = "" }},
		{CodeBuoySwingOverlap, SeverityWarning, func(m *Map, _ *Settings) {
			b := m.Berths[2]
			b.Ref, b.Code, b.Rect.Anchor.X = "buoy2", "BOA-2", 2900
			m.Berths = append(m.Berths, b)
		}},
		{CodeBuoyBerthOverlap, SeverityWarning, func(m *Map, _ *Settings) { m.Berths[2].Rect.Anchor = pt(5800, 1700) }},
		{CodePedestalUnlinked, SeverityWarning, func(m *Map, _ *Settings) {
			for i := range m.Berths {
				m.Berths[i].Pedestal, m.Berths[i].PedestalSocket = "", ""
			}
		}},
	}
	seen := map[string]bool{}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%02d_%s", i, c.code), func(t *testing.T) {
			m, s := baseMap(t), DefaultSettings()
			c.mutate(&m, &s)
			rep := ValidateMap(m, s)
			list := rep.Errors
			if c.sev == SeverityWarning {
				list = rep.Warnings
			}
			if !hasIssue(list, c.code) {
				t.Fatalf("%s not reported: errors %v warnings %v", c.code, codes(rep.Errors), codes(rep.Warnings))
			}
			if c.sev == SeverityWarning && !rep.OK() {
				t.Fatalf("warning case has errors %v", codes(rep.Errors))
			}
		})
		seen[c.code] = true
	}
	all := []string{
		CodeTooManyElements, CodeInvalidGeometry, CodeElementOutsideMap, CodeDuplicateCode, CodeBerthOverlap,
		CodeOccupiableOverlap, CodeAreaCellsOutside, CodeLOAExceedsLength, CodeDanglingReference, CodePedestalSocketUnknown,
		CodeAssetUnknown, CodeBerthOutsideWater, CodeBerthUnattached, CodeDepthMissing, CodePriceClassMissing,
		CodeBuoySwingOverlap, CodeBuoyBerthOverlap, CodePedestalUnlinked,
	}
	for _, c := range all {
		if !seen[c] {
			t.Errorf("no scenario row for %s", c)
		}
	}
	// Near misses: at the limits, nothing is reported.
	m := baseMap(t)
	m.Berths[0].MaxLOA = 1050
	m.Berths[0].Rect.Anchor.X += 50
	m.Areas[0].To = 5000
	if rep := ValidateMap(m, DefaultSettings()); !rep.OK() || len(rep.Warnings) != 0 {
		t.Fatalf("near misses: errors %v warnings %v", codes(rep.Errors), codes(rep.Warnings))
	}
}

func TestValidateMap_Deterministic(t *testing.T) {
	m := baseMap(t)
	m.Berths[1].Code = "P-1"
	m.Berths[0].PriceClass = ""
	m.Berths[1].Rect.Anchor.Y = m.Berths[0].Rect.Anchor.Y + 10
	a, b := ValidateMap(m, DefaultSettings()), ValidateMap(m, DefaultSettings())
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("not deterministic")
	}
	if !slices.IsSortedFunc(a.Errors, func(x, y Issue) int {
		if x.Code < y.Code {
			return -1
		}
		if x.Code > y.Code {
			return 1
		}
		return 0
	}) {
		t.Fatalf("errors not sorted: %v", codes(a.Errors))
	}
	for _, is := range a.Errors {
		if is.Code == CodeBerthOverlap && (len(is.Refs) != 2 || is.Refs[0] != "b1" || is.Refs[1] != "b2" || is.Kind != KindBerth) {
			t.Fatalf("overlap issue %+v", is)
		}
	}
}

func axisBerth(ref Ref, x, y, w, l geo.Cm, rot geo.Angle) Berth {
	return Berth{
		Ref: ref, Code: string(ref), Rect: geo.Rect{Anchor: pt(x, y), Width: w, Length: l, Rotation: rot}, Mooring: MooringLine,
		MaxLOA: l, MaxBeam: w, PriceClass: "L8", Depth: 300,
	}
}

func TestValidateMap_AdjacentBerthsDoNotOverlap(t *testing.T) {
	for _, rot := range []geo.Angle{0, 900, 1800, 2700} {
		m := Map{Width: 100000, Height: 100000}
		// Two berths side by side: widths 300 along the anchor line, sharing a side.
		dir := geo.Rect{Rotation: rot}
		_ = dir
		a := axisBerth("a", 50000, 50000, 300, 1000, rot)
		b := a
		b.Ref, b.Code = "b", "b"
		b.Rect.Anchor = offset(a.Rect.Anchor, rot, 0, 600) // 300 cm to the left of a
		m.Berths = []Berth{a, b}
		if rep := ValidateMap(m, DefaultSettings()); hasIssue(rep.Errors, CodeBerthOverlap) {
			t.Fatalf("rotation %d: adjacent berths reported as overlapping", rot)
		}
	}
}

func TestValidateMap_OneCmTolerance(t *testing.T) {
	m := Map{Width: 10000, Height: 10000}
	a := axisBerth("a", 1000, 1150, 300, 1000, 0) // y from 1000 to 1300
	for _, c := range []struct {
		overlap geo.Cm
		want    bool
	}{{0, false}, {1, false}, {2, true}, {10, true}} {
		b := axisBerth("b", 1000, 1450-c.overlap, 300, 1000, 0) // y from 1300-overlap
		m.Berths = []Berth{a, b}
		if got := hasIssue(ValidateMap(m, DefaultSettings()).Errors, CodeBerthOverlap); got != c.want {
			t.Errorf("overlap %d cm: reported %v, want %v", c.overlap, got, c.want)
		}
	}
}

// genBerths draws a random set of berths (any rotation) in a 20 × 20 m square.
func genBerths(t *rapid.T, n int) []Berth {
	out := make([]Berth, n)
	for i := range out {
		out[i] = axisBerth(Ref(fmt.Sprintf("b%03d", i)),
			geo.Cm(rapid.Int64Range(2000, 4000).Draw(t, "x")), geo.Cm(rapid.Int64Range(2000, 4000).Draw(t, "y")),
			geo.Cm(rapid.Int64Range(50, 600).Draw(t, "w")), geo.Cm(rapid.Int64Range(50, 1500).Draw(t, "l")),
			geo.Angle(rapid.SampledFrom([]int64{0, 900, 1800, 2700, rapid.Int64Range(0, 3599).Draw(t, "rot")}).Draw(t, "r")))
	}
	return out
}

func brutePairs(bs []Berth) map[[2]Ref]bool {
	out := map[[2]Ref]bool{}
	for i := range bs {
		for j := i + 1; j < len(bs); j++ {
			if BerthsOverlap(bs[i].Rect, bs[j].Rect) {
				a, b := bs[i].Ref, bs[j].Ref
				if b < a {
					a, b = b, a
				}
				out[[2]Ref{a, b}] = true
			}
		}
	}
	return out
}

func reportedPairs(rep Report) map[[2]Ref]bool {
	out := map[[2]Ref]bool{}
	for _, is := range rep.Errors {
		if is.Code == CodeBerthOverlap {
			out[[2]Ref{is.Refs[0], is.Refs[1]}] = true
		}
	}
	return out
}

// INV-04: ValidateMap reports berth_overlap if and only if a brute-force check finds an overlapping pair.
func TestValidateMap_RejectsOverlap(t *testing.T) {
	// Table: identical, crossing at 45°, nested, far apart.
	tbl := []struct {
		name string
		bs   []Berth
		want bool
	}{
		{"identical", []Berth{axisBerth("a", 1000, 1000, 300, 1000, 0), axisBerth("b", 1000, 1000, 300, 1000, 0)}, true},
		{"crossing", []Berth{axisBerth("a", 1000, 1000, 300, 1000, 0), axisBerth("b", 1500, 600, 300, 1000, 450)}, true},
		{"nested", []Berth{axisBerth("a", 1000, 1000, 600, 1000, 0), axisBerth("b", 1200, 1000, 100, 100, 0)}, true},
		{"apart", []Berth{axisBerth("a", 1000, 1000, 300, 1000, 0), axisBerth("b", 1000, 3000, 300, 1000, 0)}, false},
	}
	for _, c := range tbl {
		rep := ValidateMap(Map{Width: 10000, Height: 10000, Berths: c.bs}, DefaultSettings())
		if hasIssue(rep.Errors, CodeBerthOverlap) != c.want {
			t.Errorf("%s: got %v", c.name, codes(rep.Errors))
		}
	}
	// A berth thinner than MinRectSide cannot be checked for overlap, so it is refused as invalid geometry,
	// even when placed inside another berth.
	for _, thin := range []Berth{axisBerth("t", 1100, 1200, 1, 300, 0), axisBerth("t", 1100, 1200, 200, 1, 0)} {
		rep := ValidateMap(Map{Width: 10000, Height: 10000, Berths: []Berth{axisBerth("a", 1000, 1000, 600, 1000, 0), thin}}, DefaultSettings())
		if !hasIssue(rep.Errors, CodeInvalidGeometry) {
			t.Errorf("thin berth %v accepted: %v", thin.Rect, codes(rep.Errors))
		}
	}
	rapid.Check(t, func(t *rapid.T) {
		bs := genBerths(t, rapid.IntRange(2, 12).Draw(t, "n"))
		rep := ValidateMap(Map{Width: 10000, Height: 10000, Berths: bs}, DefaultSettings())
		if hasIssue(rep.Errors, CodeBerthOverlap) != (len(brutePairs(bs)) > 0) {
			t.Fatalf("report %v, brute %v", codes(rep.Errors), brutePairs(bs))
		}
	})
}

func TestRapid_ValidateMapMatchesBruteForce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bs := genBerths(t, rapid.IntRange(2, 30).Draw(t, "n"))
		got, want := reportedPairs(ValidateMap(Map{Width: 10000, Height: 10000, Berths: bs}, DefaultSettings())), brutePairs(bs)
		if len(got) != len(want) {
			t.Fatalf("reported %v, brute force %v", got, want)
		}
		for k := range want {
			if !got[k] {
				t.Fatalf("pair %v not reported", k)
			}
		}
	})
}

func TestBerthsOverlap_ImpliesShrunkOverlap(t *testing.T) {
	// When two berths do not overlap beyond the tolerance, their rectangles shrunk by 1 cm do not overlap
	// either (the weaker check of the plan holds).
	rapid.Check(t, func(t *rapid.T) {
		bs := genBerths(t, 2)
		if !BerthsOverlap(bs[0].Rect, bs[1].Rect) {
			a, b := ShrinkRect(bs[0].Rect, 1), ShrinkRect(bs[1].Rect, 1)
			if a.Width > 0 && a.Length > 0 && b.Width > 0 && b.Length > 0 && geo.RectsOverlap(a, b) {
				t.Fatalf("shrunk rects overlap: %+v %+v", a, b)
			}
		}
	})
}

func FuzzValidateMap(f *testing.F) {
	f.Add(int64(1000), int64(1000), int64(300), int64(1000), int64(0), int64(500), int64(4), uint8(0))
	f.Add(int64(-5), int64(1e9), int64(0), int64(-3), int64(5000), int64(0), int64(3), uint8(7))
	f.Fuzz(func(t *testing.T, x, y, w, l, rot, r, n int64, kind uint8) {
		m := Map{Width: 10000, Height: 10000}
		for i := range int(n%8 + 1) {
			d := geo.Cm(int64(i) * 137)
			switch (int(kind) + i) % 5 {
			case 0:
				m.Berths = append(m.Berths, axisBerth(Ref(fmt.Sprint(i)), geo.Cm(x)+d, geo.Cm(y), geo.Cm(w), geo.Cm(l), geo.Angle(rot)))
			case 1:
				b := axisBerth(Ref(fmt.Sprint(i)), geo.Cm(x), geo.Cm(y)+d, geo.Cm(w), geo.Cm(l), 0)
				b.Mooring, b.SwingRadius = MooringBuoy, geo.Cm(r)
				m.Berths = append(m.Berths, b)
			case 2:
				m.Areas = append(m.Areas, DynamicArea{
					Ref: Ref(fmt.Sprint(i)), Kind: AreaSurface, Code: fmt.Sprint(i), Cell: geo.Cm(r),
					GridRotation: geo.Angle(rot), Outline: geo.Polygon{pt(geo.Cm(x), geo.Cm(y)), pt(geo.Cm(x+w), geo.Cm(y)), pt(geo.Cm(x), geo.Cm(y+l))},
				})
			case 3:
				m.Structures = append(m.Structures, Structure{
					Ref: Ref(fmt.Sprint(i)), Kind: StructurePier, Code: fmt.Sprint(i), Width: geo.Cm(w),
					Points: []geo.Point{pt(geo.Cm(x), geo.Cm(y)), pt(geo.Cm(x+l), geo.Cm(y+r))},
				})
				m.Areas = append(m.Areas, DynamicArea{
					Ref: Ref(fmt.Sprint("a", i)), Kind: AreaLinear, Code: fmt.Sprint("a", i), Structure: Ref(fmt.Sprint(i)),
					Side: SideLeft, From: geo.Cm(r), To: geo.Cm(l), Cell: 50, Mode: ModeAlongside, MaxBeam: geo.Cm(w),
				})
			default:
				m.LandResources = append(m.LandResources, LandResource{
					Ref: Ref(fmt.Sprint(i)), Code: fmt.Sprint(i),
					Rect: geo.Rect{Anchor: pt(geo.Cm(x), geo.Cm(y)+d), Width: geo.Cm(w), Length: geo.Cm(l), Rotation: geo.Angle(rot)},
				})
			}
		}
		_ = ValidateMap(m, DefaultSettings())
	})
}

// bigMap returns a map with n berths on piers, plus pedestals: the editor benchmark scale.
func bigMap(tb testing.TB, n int) Map {
	tb.Helper()
	m := Map{Width: 500000, Height: 500000}
	perPier := 100
	for p := 0; len(m.Berths) < n; p++ {
		g, err := GeneratePier(PierParams{
			KeyPrefix: fmt.Sprintf("p%d-", p), Origin: pt(geo.Cm(5000+(p%20)*4000), geo.Cm(5000+(p/20)*20000)), Rotation: 900,
			Length: 15000, Width: 300, Code: fmt.Sprintf("P%d", p), Sides: []Side{SideLeft, SideRight}, BerthsPerSide: perPier / 2,
			BerthWidth: 300, BerthLength: 1000, Numbering: Numbering{Prefix: fmt.Sprintf("P%d-", p)}, PriceRule: DefaultPriceClassRule(),
		})
		if err != nil {
			tb.Fatal(err)
		}
		m.Structures = append(m.Structures, g.Structures...)
		m.Berths = append(m.Berths, g.Berths...)
	}
	m.Berths = m.Berths[:n]
	return m
}

func BenchmarkValidateMap_3000(b *testing.B) {
	m := bigMap(b, 3000)
	s := DefaultSettings()
	if rep := ValidateMap(m, s); !rep.OK() {
		b.Fatalf("benchmark map invalid: %v", codes(rep.Errors))
	}
	b.ResetTimer()
	for b.Loop() {
		ValidateMap(m, s)
	}
}
