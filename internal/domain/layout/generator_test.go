package layout

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/geo"
)

func berthCodes(g Generated) []string {
	out := make([]string, 0, len(g.Berths))
	for _, b := range g.Berths {
		out = append(out, b.Code)
	}
	return out
}

func pierParams() PierParams {
	return PierParams{
		KeyPrefix: "g1-", Origin: pt(10000, 2000), Rotation: 900, Length: 3000, Width: 300, Code: "B", Sides: []Side{SideLeft, SideRight},
		BerthsPerSide: 5, BerthWidth: 400, BerthLength: 1200, Numbering: Numbering{Scheme: NumberContinuous, Prefix: "B-"},
		PriceRule: DefaultPriceClassRule(), Depth: 350,
	}
}

// noOverlap checks the generated berths and land resources pairwise (brute force) and the codes for uniqueness.
// fataler is the subset of testing.TB that *rapid.T also implements.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func noOverlap(t fataler, g Generated) {
	t.Helper()
	var rects []geo.Rect
	for _, b := range g.Berths {
		if !b.IsBuoy() {
			rects = append(rects, b.Rect)
		}
	}
	for _, l := range g.LandResources {
		rects = append(rects, l.Rect)
	}
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			if BerthsOverlap(rects[i], rects[j]) {
				t.Fatalf("generated rectangles %d and %d overlap: %+v %+v", i, j, rects[i], rects[j])
			}
		}
	}
	for i := range g.Berths {
		for j := i + 1; j < len(g.Berths); j++ {
			if g.Berths[i].IsBuoy() && g.Berths[j].IsBuoy() && geo.CirclesOverlap(g.Berths[i].Circle(), g.Berths[j].Circle()) {
				t.Fatalf("buoys %d and %d overlap", i, j)
			}
		}
	}
	seen := map[string]bool{}
	for _, c := range berthCodes(g) {
		if seen[c] {
			t.Fatalf("duplicate code %s", c)
		}
		seen[c] = true
	}
	for _, l := range g.LandResources {
		if seen[l.Code] {
			t.Fatalf("duplicate code %s", l.Code)
		}
		seen[l.Code] = true
	}
	keys := map[Ref]bool{}
	for _, r := range refsOf(g) {
		if keys[r] {
			t.Fatalf("duplicate key %s", r)
		}
		keys[r] = true
	}
}

func refsOf(g Generated) []Ref {
	var out []Ref
	for _, s := range g.Structures {
		out = append(out, s.Ref)
	}
	for _, b := range g.Berths {
		out = append(out, b.Ref)
	}
	for _, a := range g.Areas {
		out = append(out, a.Ref)
	}
	for _, l := range g.LandResources {
		out = append(out, l.Ref)
	}
	return out
}

func TestGeneratePier_Scenario(t *testing.T) {
	t.Run("continuous", func(t *testing.T) {
		g, err := GeneratePier(pierParams())
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"B-1", "B-2", "B-3", "B-4", "B-5", "B-6", "B-7", "B-8", "B-9", "B-10"}
		if !slices.Equal(berthCodes(g), want) || len(g.Structures) != 1 || g.Structures[0].Code != "B" {
			t.Fatalf("codes %v structures %d", berthCodes(g), len(g.Structures))
		}
		b := g.Berths[0]
		if b.PriceClass != "L12" || b.MaxLOA != 1200 || b.MaxBeam != 350 || b.Position != 0 || g.Berths[1].Position != 10 ||
			b.Structure != "g1-s1" || b.Side != SideLeft || b.Mooring != MooringLine || b.Depth != 350 {
			t.Fatalf("berth %+v", b)
		}
		// Left of a pier pointing down (900) is +x: anchor on the pier edge x = 10150, first slot centre y = 2200.
		if b.Rect.Anchor != pt(10150, 2200) || b.Rect.Rotation != 0 {
			t.Fatalf("anchor %+v rotation %d", b.Rect.Anchor, b.Rect.Rotation)
		}
		if r := g.Berths[5]; r.Side != SideRight || r.Rect.Anchor != pt(9850, 2200) || r.Rect.Rotation != 1800 {
			t.Fatalf("right berth %+v", r)
		}
		rep := ValidateMap(Map{Width: 50000, Height: 50000, Structures: g.Structures, Berths: g.Berths}, DefaultSettings())
		if !rep.OK() || hasIssue(rep.Warnings, CodeBerthUnattached) || hasIssue(rep.Warnings, CodeDepthMissing) {
			t.Fatalf("generated pier: errors %v warnings %v", codes(rep.Errors), codes(rep.Warnings))
		}
	})
	t.Run("odd left even right", func(t *testing.T) {
		p := pierParams()
		p.Numbering = Numbering{Scheme: NumberOddLeftEven, Prefix: "C", Pad: 2}
		p.BerthsPerSide = 3
		g, err := GeneratePier(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"C01", "C03", "C05", "C02", "C04", "C06"}; !slices.Equal(berthCodes(g), want) {
			t.Fatalf("codes %v", berthCodes(g))
		}
	})
	t.Run("per side with head", func(t *testing.T) {
		p := pierParams()
		p.Numbering = Numbering{Scheme: NumberPerSide, Prefix: "D-", Start: 1}
		p.BerthsPerSide, p.HeadBerths = 2, 2
		g, err := GeneratePier(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"D-L1", "D-L2", "D-R1", "D-R2", "D-H1", "D-H2"}; !slices.Equal(berthCodes(g), want) {
			t.Fatalf("codes %v", berthCodes(g))
		}
		h := g.Berths[4]
		if h.Side != SideHead || h.Rect.Rotation != 900 || h.Rect.Anchor.Y != 5000 || h.Rect.Anchor.X != 10200 {
			t.Fatalf("head berth %+v", h)
		}
		if !slices.Contains(g.Structures[0].Sides, SideHead) {
			t.Fatal("head side missing")
		}
		noOverlap(t, g)
	})
	t.Run("fingers", func(t *testing.T) {
		p := pierParams()
		p.FingerEvery, p.BerthsPerSide = 2, 6
		g, err := GeneratePier(p)
		if err != nil {
			t.Fatal(err)
		}
		fingers := g.Structures[1:]
		if len(fingers) != 4 || fingers[0].Kind != StructureFinger || fingers[0].Code != "B-F1" || fingers[0].Parent != "g1-s1" ||
			fingers[0].Rect.Width != 60 || fingers[0].Rect.Length != 900 || g.Berths[0].Mooring != MooringFinger {
			t.Fatalf("fingers %+v", fingers)
		}
		// Third berth starts after two berths and a finger: 800 + 60 → centre 860 + 200.
		if g.Berths[2].Rect.Anchor.Y != 2000+860+200 {
			t.Fatalf("third berth %+v", g.Berths[2].Rect)
		}
		noOverlap(t, g)
	})
	t.Run("taper and fill", func(t *testing.T) {
		p := pierParams()
		p.BerthsPerSide, p.BerthWidth, p.BerthWidthEnd, p.BerthLengthEnd = 0, 300, 500, 1600
		p.Sides = []Side{SideLeft}
		g, err := GeneratePier(p)
		if err != nil {
			t.Fatal(err)
		}
		n := len(g.Berths)
		first, last := g.Berths[0].Rect, g.Berths[n-1].Rect
		if first.Width != 300 || last.Width != 500 || last.Length != 1600 || g.Berths[0].PriceClass != "L8" || g.Berths[n-1].PriceClass != "L16" {
			t.Fatalf("n %d first %+v last %+v", n, first, last)
		}
		var used geo.Cm
		for _, b := range g.Berths {
			used += b.Rect.Width
		}
		if used > 3000 || used+500 <= 3000 {
			t.Fatalf("fill used %d of 3000 with %d berths", used, n)
		}
	})
	t.Run("bad params", func(t *testing.T) {
		p := pierParams()
		p.BerthsPerSide = 20
		if _, err := GeneratePier(p); !errors.Is(err, ErrBadParams) {
			t.Fatalf("does not fit: %v", err)
		}
		p = pierParams()
		p.Width, p.Code = 0, ""
		_, err := GeneratePier(p)
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) != 2 {
			t.Fatalf("validation: %v", err)
		}
		p = pierParams()
		p.Length, p.BerthsPerSide, p.BerthWidth = 400000, 0, 100
		if _, err := GeneratePier(p); !errors.Is(err, ErrBadParams) {
			t.Fatalf("too many: %v", err)
		}
	})
}

func TestGenerateQuayBerths_Corner(t *testing.T) {
	quay := Structure{Ref: "q", Kind: StructureQuay, Code: "Q", Points: []geo.Point{pt(1000, 1000), pt(6000, 1000), pt(6000, 6000)}, Sides: []Side{SideRight}}
	// Right of an east-going then south-going quay is inside the corner: berths of both legs compete there.
	g, err := GenerateQuayBerths(QuayBerthsParams{
		KeyPrefix: "q-", Quay: quay, From: 0, To: 10000, Side: SideRight, BerthWidth: 400,
		BerthLength: 1200, Numbering: Numbering{Prefix: "Q"}, PriceRule: DefaultPriceClassRule(),
	})
	if err != nil {
		t.Fatal(err)
	}
	noOverlap(t, g)
	if len(g.Berths) < 20 || g.Berths[0].Rect.Rotation != 900 {
		t.Fatalf("%d berths, first %+v", len(g.Berths), g.Berths[0].Rect)
	}
	if _, err := GenerateQuayBerths(QuayBerthsParams{Quay: quay, From: 0, To: 20000, Side: SideRight, BerthWidth: 400, BerthLength: 1000}); !errors.Is(err, ErrBadParams) {
		t.Fatalf("beyond the quay: %v", err)
	}
}

func TestGenerateLinearArea_Scenario(t *testing.T) {
	pier := Structure{Ref: "p", Kind: StructurePier, Code: "T", Points: []geo.Point{pt(1000, 1000), pt(1000, 13000)}, Width: 300}
	g, err := GenerateLinearArea(LinearAreaParams{
		KeyPrefix: "t-", Code: "T-L", Structure: pier, Side: SideLeft, From: 0, To: 12000,
		Mode: ModeSternTo, Margin: 50, MaxBeam: 1200,
	})
	if err != nil || len(g.Areas) != 1 || g.Areas[0].Cell != 50 {
		t.Fatalf("%+v %v", g, err)
	}
	rep := ValidateMap(Map{Width: 20000, Height: 20000, Structures: []Structure{pier}, Areas: g.Areas}, DefaultSettings())
	if !rep.OK() {
		t.Fatalf("errors %v", codes(rep.Errors))
	}
	if _, err := GenerateLinearArea(LinearAreaParams{Code: "X", Structure: pier, Side: SideLeft, From: 0, To: 13000, Mode: ModeSternTo}); !errors.Is(err, ErrBadParams) {
		t.Fatalf("beyond the pier: %v", err)
	}
}

func TestGenerateBuoyField_Scenario(t *testing.T) {
	outline := rectPoly(0, 0, 20000, 10000)
	grid, err := GenerateBuoyField(BuoyFieldParams{
		KeyPrefix: "f-", Outline: outline, SwingRadius: 1000, Layout: "grid", MaxLOA: 1200,
		Numbering: Numbering{Prefix: "BOA-"}, PriceRule: DefaultPriceClassRule(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2000 cm spacing, radius 1000: 10 per row, 5 rows.
	if len(grid.Berths) != 50 || grid.Berths[0].Rect.Anchor != pt(1000, 1000) || grid.Berths[0].SwingRadius != 1000 ||
		grid.Berths[0].Mooring != MooringBuoy || grid.Berths[0].Code != "BOA-1" || grid.Berths[0].PriceClass != "L12" {
		t.Fatalf("grid: %d, first %+v", len(grid.Berths), grid.Berths[0])
	}
	noOverlap(t, grid)
	stag, err := GenerateBuoyField(BuoyFieldParams{Outline: outline, SwingRadius: 1000, Layout: "staggered", Numbering: Numbering{Prefix: "S"}})
	if err != nil {
		t.Fatal(err)
	}
	noOverlap(t, stag)
	if len(stag.Berths) != 48 || stag.Berths[10].Rect.Anchor != pt(2000, 2733) {
		t.Fatalf("staggered: %d, second row %+v", len(stag.Berths), stag.Berths[10].Rect.Anchor)
	}
	if _, err := GenerateBuoyField(BuoyFieldParams{Outline: outline, SwingRadius: 1000, Spacing: 1500, Layout: "grid"}); !errors.Is(err, ErrBadParams) {
		t.Fatalf("spacing below the diameter: %v", err)
	}
}

func TestGenerateLandGrid_Scenario(t *testing.T) {
	g, err := GenerateLandGrid(LandGridParams{
		KeyPrefix: "l-", Origin: pt(1000, 1000), Rows: 6, Cols: 10, ItemWidth: 250, ItemLength: 500,
		Aisle: 600, AisleEvery: 2, Kind: LandParking, Numbering: Numbering{Prefix: "P", Pad: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.LandResources) != 60 || g.LandResources[0].Code != "P01" || g.LandResources[59].Code != "P60" {
		t.Fatalf("%d resources", len(g.LandResources))
	}
	first, row2 := g.LandResources[0].Rect, g.LandResources[20].Rect
	if first.Anchor != pt(1125, 1000) || first.Rotation != 900 || row2.Anchor.Y != 1000+2*500+600 {
		t.Fatalf("first %+v row 2 %+v", first, row2)
	}
	noOverlap(t, g)
}

func TestGenerateSurfaceArea_LanesBlockCells(t *testing.T) {
	g, err := GenerateSurfaceArea(SurfaceParams{
		KeyPrefix: "s-", Code: "YARD", Outline: rectPoly(1000, 1000, 2000, 1500), Cell: 100, Margin: 60,
		Exits: []geo.Point{pt(1050, 1050), pt(1950, 1450)},
		Lanes: []Lane{{From: pt(1000, 1250), To: pt(2000, 1250), Width: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := g.Areas[0]
	// 10 × 5 cells; the lane covers y 1200..1300 = row 2 exactly.
	want := []int{20, 21, 22, 23, 24, 25, 26, 27, 28, 29}
	if !slices.Equal(a.Blocked, want) || !slices.Equal(a.Exits, []int{0, 49}) || a.Kind != AreaSurface {
		t.Fatalf("blocked %v exits %v", a.Blocked, a.Exits)
	}
	area, err := AreaOf(a, Map{})
	if err != nil || CellCount(area) != 50 || area.Usable[20] || !area.Usable[19] {
		t.Fatalf("area %+v %v", area, err)
	}
	if _, err := GenerateSurfaceArea(SurfaceParams{Code: "X", Outline: rectPoly(0, 0, 100, 100), Cell: 50, Exits: []geo.Point{pt(500, 500)}}); !errors.Is(err, ErrBadParams) {
		t.Fatalf("exit outside: %v", err)
	}
}

func drawPier(t *rapid.T) PierParams {
	sides := rapid.SampledFrom([][]Side{{SideLeft}, {SideRight}, {SideLeft, SideRight}}).Draw(t, "sides")
	return PierParams{
		KeyPrefix: "r-", Origin: pt(250000, 250000), Rotation: geo.Angle(rapid.Int64Range(0, 3599).Draw(t, "rot")),
		Length: geo.Cm(rapid.Int64Range(1000, 30000).Draw(t, "len")), Width: geo.Cm(rapid.Int64Range(100, 600).Draw(t, "width")),
		Code: "R", Sides: sides, BerthsPerSide: 0, StartOffset: geo.Cm(rapid.Int64Range(0, 500).Draw(t, "start")),
		BerthWidth: geo.Cm(rapid.Int64Range(150, 800).Draw(t, "bw")), BerthLength: geo.Cm(rapid.Int64Range(400, 2500).Draw(t, "bl")),
		BerthWidthEnd: geo.Cm(rapid.SampledFrom([]int64{0, 0, 900}).Draw(t, "bwe")), BerthLengthEnd: geo.Cm(rapid.SampledFrom([]int64{0, 3000}).Draw(t, "ble")),
		FingerEvery: rapid.IntRange(0, 3).Draw(t, "fingers"), HeadBerths: rapid.IntRange(0, 4).Draw(t, "head"),
		Numbering: Numbering{Scheme: rapid.SampledFrom([]string{NumberContinuous, NumberOddLeftEven, NumberPerSide}).Draw(t, "scheme"), Prefix: "R-"},
		PriceRule: DefaultPriceClassRule(),
	}
}

// INV-04: generator output never overlaps, at any rotation.
func TestGenerator_NoOverlap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		switch rapid.IntRange(0, 3).Draw(t, "gen") {
		case 0:
			g, err := GeneratePier(drawPier(t))
			if errors.Is(err, ErrBadParams) {
				return // the pier is too short for one berth
			}
			if err != nil {
				t.Fatal(err)
			}
			noOverlap(t, g)
			rep := ValidateMap(Map{Width: MaxMapSide, Height: MaxMapSide, Structures: g.Structures, Berths: g.Berths}, DefaultSettings())
			if hasIssue(rep.Errors, CodeBerthOverlap) || hasIssue(rep.Errors, CodeDuplicateCode) {
				t.Fatalf("errors %v", codes(rep.Errors))
			}
		case 1:
			rot := geo.Angle(rapid.Int64Range(0, 3599).Draw(t, "rot"))
			g, err := GenerateLandGrid(LandGridParams{
				Origin: pt(100000, 100000), Rotation: rot, Rows: rapid.IntRange(1, 8).Draw(t, "rows"),
				Cols: rapid.IntRange(1, 8).Draw(t, "cols"), ItemWidth: geo.Cm(rapid.Int64Range(100, 600).Draw(t, "w")),
				ItemLength: geo.Cm(rapid.Int64Range(100, 900).Draw(t, "l")), Aisle: 300, AisleEvery: rapid.IntRange(0, 3).Draw(t, "ae"), Kind: LandBox,
			})
			if err != nil {
				t.Fatal(err)
			}
			noOverlap(t, g)
		case 2:
			n := rapid.IntRange(2, 5).Draw(t, "n")
			pts := []geo.Point{pt(100000, 100000)}
			for i := 1; i < n; i++ {
				last := pts[len(pts)-1]
				pts = append(pts, last.Add(pt(geo.Cm(rapid.Int64Range(-8000, 8000).Draw(t, "dx")), geo.Cm(rapid.Int64Range(-8000, 8000).Draw(t, "dy")))))
			}
			quay := Structure{Ref: "q", Kind: StructureQuay, Points: pts}
			if structureGeometryError(&Structure{Kind: StructureWalkway, Points: pts}) != "" {
				return
			}
			total := PolylineLength(pts)
			g, err := GenerateQuayBerths(QuayBerthsParams{
				Quay: quay, From: 0, To: total, Side: rapid.SampledFrom([]Side{SideLeft, SideRight}).Draw(t, "side"),
				BerthWidth: geo.Cm(rapid.Int64Range(200, 600).Draw(t, "bw")), BerthLength: geo.Cm(rapid.Int64Range(500, 2000).Draw(t, "bl")),
			})
			if errors.Is(err, ErrBadParams) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			noOverlap(t, g)
		default:
			r := geo.Cm(rapid.Int64Range(300, 2000).Draw(t, "r"))
			g, err := GenerateBuoyField(BuoyFieldParams{
				Outline: rectPoly(0, 0, 30000, 20000), SwingRadius: r,
				Layout: rapid.SampledFrom([]string{"grid", "staggered"}).Draw(t, "layout"), Spacing: 2*r + geo.Cm(rapid.Int64Range(0, 500).Draw(t, "extra")),
			})
			if err != nil {
				t.Fatal(err)
			}
			noOverlap(t, g)
		}
	})
}

func TestRapid_GeneratorCountsAndUniqueCodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := drawPier(t)
		p.BerthsPerSide = rapid.IntRange(1, 20).Draw(t, "n")
		p.Length = 100000
		g, err := GeneratePier(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := p.BerthsPerSide*len(p.Sides) + p.HeadBerths; len(g.Berths) != want {
			t.Fatalf("%d berths, want %d", len(g.Berths), want)
		}
		fingers := 0
		if p.FingerEvery > 0 {
			fingers = (p.BerthsPerSide - 1) / p.FingerEvery * len(p.Sides)
		}
		if len(g.Structures) != 1+fingers {
			t.Fatalf("%d structures, want %d", len(g.Structures), 1+fingers)
		}
		noOverlap(t, g)
		rows, cols := rapid.IntRange(1, 20).Draw(t, "rows"), rapid.IntRange(1, 20).Draw(t, "cols")
		lg, err := GenerateLandGrid(LandGridParams{Rows: rows, Cols: cols, ItemWidth: 250, ItemLength: 500, Kind: LandParking, Origin: pt(1000, 1000)})
		if err != nil || len(lg.LandResources) != rows*cols {
			t.Fatalf("land grid %d %v", len(lg.LandResources), err)
		}
		noOverlap(t, lg)
	})
}

func TestNumbering_Code(t *testing.T) {
	for _, c := range []struct {
		n    Numbering
		k    int64
		side Side
		want string
	}{
		{Numbering{Prefix: "B-"}, 7, SideLeft, "B-7"},
		{Numbering{Prefix: "B-", Pad: 3}, 7, SideLeft, "B-007"},
		{Numbering{Scheme: NumberPerSide, Prefix: "B-"}, 2, SideRight, "B-R2"},
		{Numbering{Scheme: NumberPerSide}, 1, SideHead, "H1"},
	} {
		if got := c.n.code(c.k, c.side); got != c.want {
			t.Errorf("%+v %d: %q, want %q", c.n, c.k, got, c.want)
		}
	}
	if ValidCode("") || ValidCode(" A") || ValidCode("A\x01") || !ValidCode("B-12") || ValidCode(fmt.Sprintf("%033d", 1)) {
		t.Fatal("ValidCode")
	}
}

// Generated berths and land resources obey the same minimum side as drawn ones (MinRectSide).
func TestGenerators_RejectSidesBelowMinRectSide(t *testing.T) {
	hasField := func(err error, field string) bool {
		var v *ValidationError
		if !errors.As(err, &v) {
			return false
		}
		for _, f := range v.Fields {
			if f.Field == field {
				return true
			}
		}
		return false
	}
	p := pierParams()
	p.BerthWidth = MinRectSide - 1
	if _, err := GeneratePier(p); !hasField(err, "berth_width_cm") {
		t.Fatalf("pier berth width %d: %v", p.BerthWidth, err)
	}
	p = pierParams()
	p.BerthWidthEnd = MinRectSide - 1
	if _, err := GeneratePier(p); !hasField(err, "berth_width_end_cm") {
		t.Fatalf("pier berth end width %d: %v", p.BerthWidthEnd, err)
	}
	p = pierParams()
	p.BerthWidth, p.BerthLength = MinRectSide, MinRectSide
	if _, err := GeneratePier(p); hasField(err, "berth_width_cm") || hasField(err, "berth_length_cm") {
		t.Fatalf("pier berth side %d refused: %v", MinRectSide, err)
	}
	_, err := GenerateLandGrid(LandGridParams{
		KeyPrefix: "l-", Origin: pt(1000, 1000), Rows: 1, Cols: 1, ItemWidth: 1, ItemLength: 500,
		Kind: LandParking, Numbering: Numbering{Prefix: "P"},
	})
	if !hasField(err, "item_width_cm") {
		t.Fatalf("land grid item width 1: %v", err)
	}
}
