package app

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/geo"
)

// Demo harbour (m1-plan §6): four maps built with the generators through the harbour service (so audit, outbox
// and the save validation apply as in production). The water map has a quay, a breakwater and a channel, piers
// A–E with 320 berths in 8 price classes, a dynamic pier T with two linear areas, a staggered field of 30 buoys,
// 80 pedestals, kit decorations and a georeference. Then a yard, a shed and a parking map.

// demoPier is one pier of the water map: the left side comes from the pier generator, the right side (when its
// width differs) from the berths-along-a-structure generator on the same pier.
type demoPier struct {
	code              string
	x                 geo.Cm
	perSide           int
	left, right       int64 // berth widths
	length, lengthEnd int64 // berth lengths (lengthEnd > 0: tapered)
	widthEnd          int64
	fingers, head     int
	pierLength        int64
}

var demoPiers = []demoPier{
	{code: "A", x: 2000, perSide: 40, left: 300, right: 350, length: 800, pierLength: 14000},
	{code: "B", x: 5600, perSide: 36, left: 400, right: 400, length: 1000, fingers: 2, pierLength: 15500},
	{code: "C", x: 9600, perSide: 32, left: 450, right: 400, length: 1200, pierLength: 14400},
	{code: "D", x: 14000, perSide: 28, left: 500, right: 450, length: 1400, pierLength: 14000},
	{code: "E", x: 19200, perSide: 22, left: 550, right: 550, widthEnd: 700, length: 1600, lengthEnd: 2000, head: 4, pierLength: 14000},
}

func i64p(v int64) *int64 { return &v }
func intp(v int) *int     { return &v }
func strp(v string) *string {
	return &v
}

func pt(x, y geo.Cm) geo.Point { return geo.Point{X: x, Y: y} }

func rectOutline(x0, y0, x1, y1 geo.Cm) []geo.Point {
	return []geo.Point{pt(x0, y0), pt(x1, y0), pt(x1, y1), pt(x0, y1)}
}

// seeder accumulates a map document through generator previews.
type seeder struct {
	a     *App
	ctx   context.Context
	p     *auth.Principal
	m     string
	mapID string
	doc   harbor.SaveRequest
}

func (s *seeder) generate(spec harbor.GeneratorSpec) (harbor.Elements, error) {
	doc := s.doc
	res, err := s.a.Harbor.Generate(s.ctx, s.p, s.m, s.mapID, harbor.GenerateInput{Generator: spec, Document: &doc})
	if err != nil {
		return harbor.Elements{}, fmt.Errorf("generate %s %s: %w", spec.Type, spec.KeyPrefix, err)
	}
	if res.Report != nil && !res.Report.OK() {
		return harbor.Elements{}, fmt.Errorf("generate %s %s: %d errors", spec.Type, spec.KeyPrefix, len(res.Report.Errors))
	}
	e := res.Elements
	s.doc.Structures = append(s.doc.Structures, e.Structures...)
	s.doc.Berths = append(s.doc.Berths, e.Berths...)
	s.doc.DynamicAreas = append(s.doc.DynamicAreas, e.DynamicAreas...)
	s.doc.LandResources = append(s.doc.LandResources, e.LandResources...)
	return e, nil
}

func (s *seeder) save() (*harbor.SaveResult, error) {
	return s.a.Harbor.SaveMap(s.ctx, s.p, s.m, s.mapID, 1, s.doc)
}

func (a *App) newSeedMap(ctx context.Context, p *auth.Principal, marinaID, kind, it, en string, w, h int64) (*seeder, error) {
	d, err := a.Harbor.CreateMap(ctx, p, marinaID, harbor.MapCreate{Kind: kind, Name: harbor.I18nText{It: it, En: strp(en)}, WidthCm: w, HeightCm: h})
	if err != nil {
		return nil, err
	}
	return &seeder{a: a, ctx: ctx, p: p, m: marinaID, mapID: d.ID, doc: d.Request()}, nil
}

// seedHarbor seeds the demo harbour when the marina has no maps, and adds the counts to res.
func (a *App) seedHarbor(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	maps, err := a.Harbor.ListMaps(ctx, p, res.MarinaID)
	if err != nil {
		return err
	}
	if len(maps) > 0 {
		return nil
	}
	groups := map[string]string{}
	for _, g := range []struct{ code, it, kind string }{
		{"PONT-A", "Pontile A", "structure"},
		{"PONT-B", "Pontile B", "structure"},
		{"PONT-C", "Pontile C", "structure"},
		{"PONT-D", "Pontile D", "structure"},
		{"PONT-E", "Pontile E", "structure"},
		{"PONT-T", "Pontile dinamico T", "area"},
		{"TRANSITI", "Transiti", "transit"},
		{"BOE", "Campo boe", "custom"},
	} {
		gr, err := a.Harbor.CreateGroup(ctx, p, res.MarinaID, harbor.GroupInput{Code: g.code, Name: harbor.I18nText{It: g.it}, Kind: g.kind})
		if err != nil {
			return err
		}
		groups[g.code] = gr.ID
	}
	water, err := a.seedWaterMap(ctx, p, res.MarinaID, groups)
	if err != nil {
		return fmt.Errorf("water map: %w", err)
	}
	res.Maps++
	res.Berths += water
	for _, f := range []func(context.Context, *auth.Principal, string) (int, error){a.seedYardMap, a.seedShedMap, a.seedParkingMap} {
		n, err := f(ctx, p, res.MarinaID)
		if err != nil {
			return err
		}
		res.Maps++
		res.Berths += n
	}
	return nil
}

func (a *App) seedWaterMap(ctx context.Context, p *auth.Principal, marinaID string, groups map[string]string) (int, error) {
	s, err := a.newSeedMap(ctx, p, marinaID, "water", "Specchio acqueo", "Water area", 60000, 40000)
	if err != nil {
		return 0, err
	}
	rng := rand.New(rand.NewPCG(2026, 11)) //nolint:gosec // deterministic demo data
	s.doc.WaterPolygons = [][]geo.Point{rectOutline(0, 3000, 60000, 40000)}
	s.doc.LandPolygons = [][]geo.Point{rectOutline(0, 0, 60000, 3000)}
	s.doc.Georef = &harbor.Georef{Points: []harbor.ControlPoint{
		{X: 0, Y: 0, LatE7: 409_101_000, LonE7: 95_402_000},
		{X: 60000, Y: 0, LatE7: 409_101_000, LonE7: 95_472_260},
		{X: 0, Y: 40000, LatE7: 409_065_068, LonE7: 95_402_000},
	}}
	s.doc.Structures = []harbor.Structure{
		{
			Key: "quay", Kind: "quay", Code: "BANCHINA", Name: "Banchina di riva", Points: []geo.Point{pt(0, 3000), pt(60000, 3000)}, Sides: []string{"right"},
			DepthProfile: []harbor.DepthSpan{{FromCm: 0, ToCm: 60000, DepthCm: 400}},
		},
		{Key: "diga", Kind: "breakwater", Code: "DIGA", Name: "Diga foranea", Closed: true, Points: rectOutline(57000, 3000, 58500, 30000)},
		{Key: "canale", Kind: "channel", Code: "IMBOCCATURA", Name: "Imboccatura", Closed: true, Points: rectOutline(50000, 33000, 60000, 40000)},
	}
	// Piers A–E.
	for i, dp := range demoPiers {
		sides := []string{"left"}
		if dp.right == dp.left {
			sides = append(sides, "right")
		}
		spec := harbor.GeneratorSpec{
			Type: "pier", KeyPrefix: fmt.Sprintf("p%d-", i), Origin: &geo.Point{X: dp.x, Y: 3000}, RotationDd: i64p(900),
			LengthCm: i64p(dp.pierLength), WidthCm: i64p(300), Code: strp(dp.code), RootID: strp("quay"), Sides: &sides,
			BerthsPerSide: intp(dp.perSide), BerthWidthCm: i64p(dp.left), BerthLengthCm: i64p(dp.length), DepthCm: i64p(300 + int64(i)*50),
			Numbering: &harbor.Numbering{Scheme: "continuous", Prefix: strp(dp.code + "-")}, GroupID: strp(groups["PONT-"+dp.code]),
		}
		if dp.widthEnd > 0 {
			spec.BerthWidthEndCm, spec.BerthLengthEndCm = i64p(dp.widthEnd), i64p(dp.lengthEnd)
		}
		if dp.fingers > 0 {
			spec.FingerEvery = intp(dp.fingers)
		}
		if dp.head > 0 {
			spec.HeadBerths = intp(dp.head)
		}
		if _, err := s.generate(spec); err != nil {
			return 0, err
		}
		if dp.right != dp.left {
			if _, err := s.generate(harbor.GeneratorSpec{
				Type: "quay_berths", KeyPrefix: fmt.Sprintf("p%dr-", i), QuayID: strp(fmt.Sprintf("p%d-s1", i)), FromCm: i64p(0),
				ToCm: i64p(dp.pierLength), Side: strp("right"), BerthWidthCm: i64p(dp.right), Count: intp(dp.perSide),
				BerthLengthCm: i64p(dp.length), Numbering: &harbor.Numbering{Scheme: "continuous", Prefix: strp(dp.code + "-"), Start: i64p(int64(dp.perSide + 1))},
				GroupID: strp(groups["PONT-"+dp.code]),
			}); err != nil {
				return 0, err
			}
		}
	}
	// Dynamic pier T: stern-to on the left, alongside on the right.
	s.doc.Structures = append(s.doc.Structures, harbor.Structure{
		Key: "pier-t", Kind: "pier", Code: "T", Name: "Pontile dinamico",
		Points: []geo.Point{pt(24200, 3000), pt(24200, 15000)}, WidthCm: 300, ParentID: "quay", Sides: []string{"left", "right"},
		DepthProfile: []harbor.DepthSpan{{FromCm: 0, ToCm: 12000, DepthCm: 450}},
	})
	for _, g := range []harbor.GeneratorSpec{
		{
			Type: "linear_area", KeyPrefix: "tl-", Code: strp("T-POPPA"), StructureID: strp("pier-t"), Side: strp("left"), FromCm: i64p(0), ToCm: i64p(12000),
			Mode: strp("stern_to"), CellCm: i64p(50), MarginCm: i64p(50), MaxBeamCm: i64p(1200), GroupID: strp(groups["PONT-T"]),
		},
		{
			Type: "linear_area", KeyPrefix: "tr-", Code: strp("T-FIANCO"), StructureID: strp("pier-t"), Side: strp("right"), FromCm: i64p(0), ToCm: i64p(12000),
			Mode: strp("alongside"), CellCm: i64p(50), MarginCm: i64p(100), MaxBeamCm: i64p(600), GroupID: strp(groups["PONT-T"]),
		},
		{
			Type: "buoy_field", KeyPrefix: "boe-", Outline: &[]geo.Point{pt(30000, 22000), pt(45400, 22000), pt(45400, 37000), pt(30000, 37000)},
			SwingRadiusCm: i64p(1400), Layout: strp("staggered"), MaxLOACm: i64p(1200), GroupID: strp(groups["BOE"]),
			Numbering: &harbor.Numbering{Scheme: "continuous", Prefix: strp("BOA-"), Pad: intp(2)},
		},
	} {
		if _, err := s.generate(g); err != nil {
			return 0, err
		}
	}
	// Transit berths: the head of pier E and ten berths of pier A.
	transit := 0
	for i := range s.doc.Berths {
		b := &s.doc.Berths[i]
		switch {
		case b.Side == "head", b.Code >= "A-31" && b.Code <= "A-40" && len(b.Code) == 4:
			b.GroupIDs = append(b.GroupIDs, groups["TRANSITI"])
			b.Attributes = append(b.Attributes, "transit_only")
			transit++
		}
	}
	_ = transit
	// 80 pedestals, one every four berths of a pier side, with four sockets (16 and 32 A).
	type sideKey struct{ structure, side string }
	runs := map[sideKey][]int{}
	var order []sideKey
	for i, b := range s.doc.Berths {
		if b.MooringType == "buoy" || b.Side == "head" {
			continue
		}
		k := sideKey{b.StructureID, b.Side}
		if _, ok := runs[k]; !ok {
			order = append(order, k)
		}
		runs[k] = append(runs[k], i)
	}
	sockets := []harbor.Socket{
		{Code: "S1", Kind: "power", Amps: 16, Phase: "single"},
		{Code: "S2", Kind: "power", Amps: 16, Phase: "single"},
		{Code: "S3", Kind: "power", Amps: 32, Phase: "single"},
		{Code: "S4", Kind: "power", Amps: 32, Phase: "three"},
	}
	n := 0
	for _, k := range order {
		idx := runs[k]
		for g := 0; g+4 <= len(idx) && n < 80; g += 4 {
			first := s.doc.Berths[idx[g+1]]
			rot := geo.Angle(first.RotationDd)
			at := pt(first.Anchor.X-geo.Cm(geo.Cos(rot)*60/geo.TrigScale), first.Anchor.Y-geo.Cm(geo.Sin(rot)*60/geo.TrigScale))
			n++
			key := fmt.Sprintf("col-%d", n)
			s.doc.Pedestals = append(s.doc.Pedestals, harbor.Pedestal{
				Key: key, Code: fmt.Sprintf("C%02d", n), Position: at, Sockets: sockets,
				Meters: []harbor.Meter{{Kind: "power", Serial: fmt.Sprintf("EN-%04d", n)}, {Kind: "water", Serial: fmt.Sprintf("AQ-%04d", n)}},
			})
			for j := range 4 {
				b := &s.doc.Berths[idx[g+j]]
				b.PedestalID, b.PedestalSocket = key, sockets[j].Code
				b.PowerA, b.PowerPhase, b.Water = sockets[j].Amps, sockets[j].Phase, true
			}
		}
	}
	// Decorations from the kit, north arrow, scale bar and depth labels.
	for i, d := range []struct {
		asset string
		x, y  geo.Cm
	}{
		{"harbour_office", 30000, 1500},
		{"fuel_station", 27500, 2600},
		{"toilets", 8000, 1500},
		{"showers", 9500, 1500},
		{"travel_lift", 40000, 2400},
		{"crane", 43000, 2400},
		{"slipway", 46000, 2400},
		{"light_red", 50500, 32500},
		{"light_green", 56500, 32500},
		{"lighthouse", 57750, 29000},
		{"waste_collection", 12000, 1500},
		{"pump_out", 28500, 2600},
	} {
		s.doc.Decorations = append(s.doc.Decorations, harbor.Decoration{
			Key: fmt.Sprintf("deco-%d", i), Kind: "symbol", Asset: d.asset,
			Position: &geo.Point{X: d.x, Y: d.y}, SizeCm: 600,
		})
	}
	s.doc.Decorations = append(s.doc.Decorations,
		harbor.Decoration{Key: "north", Kind: "north_arrow", Position: &geo.Point{X: 58500, Y: 1500}, SizeCm: 1200, RotationDd: i64p(0)},
		harbor.Decoration{Key: "scale", Kind: "scale_bar", Points: []geo.Point{pt(1000, 39000), pt(11000, 39000)}, Label: &harbor.I18nText{It: "100 m"}})
	for i := range 8 {
		depth := int64(300 + rng.IntN(500))
		x, y := geo.Cm(3000+i*6000), geo.Cm(20000+rng.IntN(15000))
		s.doc.Decorations = append(s.doc.Decorations, harbor.Decoration{
			Key: fmt.Sprintf("depth-%d", i), Kind: "depth_label",
			Position: &geo.Point{X: x, Y: y}, DepthCm: &depth,
		})
	}
	res, err := s.save()
	if err != nil {
		return 0, err
	}
	return len(res.Document.Berths), nil
}

func (a *App) seedYardMap(ctx context.Context, p *auth.Principal, marinaID string) (int, error) {
	s, err := a.newSeedMap(ctx, p, marinaID, "yard", "Piazzale", "Yard", 12000, 8000)
	if err != nil {
		return 0, err
	}
	s.doc.LandPolygons = [][]geo.Point{rectOutline(0, 0, 12000, 8000)}
	if _, err := s.generate(harbor.GeneratorSpec{
		Type: "surface_area", KeyPrefix: "yard-", Code: strp("PIAZZALE"), Outline: &[]geo.Point{pt(2000, 1500), pt(10000, 1500), pt(10000, 6500), pt(2000, 6500)},
		CellCm: i64p(100), MarginCm: i64p(60), Exits: doorCells(2050, 9950, 3750, 6),
		Lanes: &[]harbor.LaneSpec{{From: pt(2000, 4000), To: pt(10000, 4000), WidthCm: 400}}, MaxLOACm: i64p(2400),
	}); err != nil {
		return 0, err
	}
	_, err = s.save()
	return 0, err
}

func (a *App) seedShedMap(ctx context.Context, p *auth.Principal, marinaID string) (int, error) {
	s, err := a.newSeedMap(ctx, p, marinaID, "shed", "Capannone", "Shed", 5000, 3000)
	if err != nil {
		return 0, err
	}
	if _, err := s.generate(harbor.GeneratorSpec{
		Type: "surface_area", KeyPrefix: "shed-", Code: strp("CAPANNONE"), Outline: &[]geo.Point{pt(500, 250), pt(4500, 250), pt(4500, 2750), pt(500, 2750)},
		CellCm: i64p(100), MarginCm: i64p(60), MaxLOACm: i64p(1500), MaxHeightCm: i64p(1400), Exits: doorCells(550, -1, 1250, 6),
	}); err != nil {
		return 0, err
	}
	// Pillars: blocked cells of the 40 × 25 grid.
	area := &s.doc.DynamicAreas[0]
	for _, row := range []int{8, 16} {
		for _, col := range []int{10, 20, 30} {
			area.BlockedCells = append(area.BlockedCells, row*40+col)
		}
	}
	_, err = s.save()
	return 0, err
}

func (a *App) seedParkingMap(ctx context.Context, p *auth.Principal, marinaID string) (int, error) {
	s, err := a.newSeedMap(ctx, p, marinaID, "parking", "Parcheggi e box", "Parking and boxes", 10000, 7000)
	if err != nil {
		return 0, err
	}
	for _, g := range []harbor.GeneratorSpec{
		{
			Type: "land_grid", KeyPrefix: "park-", Origin: &geo.Point{X: 500, Y: 500}, RotationDd: i64p(0), Rows: intp(6), Cols: intp(10),
			ItemWidthCm: i64p(250), ItemLengthCm: i64p(500), AisleCm: i64p(600), AisleEvery: intp(2), Kind: strp("parking"),
			Numbering: &harbor.Numbering{Scheme: "continuous", Prefix: strp("P"), Pad: intp(2)},
		},
		{
			Type: "land_grid", KeyPrefix: "box-", Origin: &geo.Point{X: 500, Y: 5000}, RotationDd: i64p(0), Rows: intp(2), Cols: intp(12),
			ItemWidthCm: i64p(300), ItemLengthCm: i64p(600), Kind: strp("box"),
			Numbering: &harbor.Numbering{Scheme: "continuous", Prefix: strp("BOX-"), Pad: intp(2)},
		},
	} {
		if _, err := s.generate(g); err != nil {
			return 0, err
		}
	}
	_, err = s.save()
	return 0, err
}

// doorCells returns the exit points of a demo door n cells (1 m each) tall on the left grid edge (x at the centre
// of column 0) and, when right ≥ 0, on the right edge too, from the cell centre y0 downwards. A storage boat leaves
// through a door at least beam + margin wide (m9-plan D7), so a one-cell door would lock every boat in.
func doorCells(left, right, y0 geo.Cm, n int) *[]geo.Point {
	var out []geo.Point
	for i := range n {
		y := y0 + geo.Cm(i)*100
		out = append(out, pt(left, y))
		if right >= 0 {
			out = append(out, pt(right, y))
		}
	}
	return &out
}
