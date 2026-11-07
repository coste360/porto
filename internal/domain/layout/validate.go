package layout

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/geo"
)

// Issue codes of ValidateMap (m1-plan §2.3, spec 8.4).
const (
	CodeTooManyElements       = "too_many_elements"
	CodeInvalidGeometry       = "invalid_geometry"
	CodeElementOutsideMap     = "element_outside_map"
	CodeDuplicateCode         = "duplicate_code"
	CodeBerthOverlap          = "berth_overlap"
	CodeOccupiableOverlap     = "occupiable_overlap"
	CodeAreaCellsOutside      = "area_cells_outside_support"
	CodeLOAExceedsLength      = "loa_exceeds_length"
	CodeDanglingReference     = "dangling_reference"
	CodePedestalSocketUnknown = "pedestal_socket_unknown"
	CodeAssetUnknown          = "asset_unknown"
	CodeBerthOutsideWater     = "berth_outside_water"
	CodeBerthUnattached       = "berth_unattached"
	CodeDepthMissing          = "depth_missing"
	CodePriceClassMissing     = "price_class_missing"
	CodeBuoySwingOverlap      = "buoy_swing_overlap"
	CodeBuoyBerthOverlap      = "buoy_berth_overlap"
	CodePedestalUnlinked      = "pedestal_unlinked"
	CodeBerthBelowMinSize     = "berth_below_min_size"
)

// validator accumulates issues.
type validator struct {
	m    *Map
	s    Settings
	rep  Report
	mapB geo.Box

	structures map[Ref]*Structure
	pedestals  map[Ref]*Pedestal
	badStruct  map[Ref]bool // structures with invalid geometry
}

func (v *validator) add(sev Severity, code, kind, field string, params map[string]int64, refs ...Ref) {
	is := Issue{Code: code, Severity: sev, Kind: kind, Refs: refs, Field: field, Params: params}
	if sev == SeverityError {
		v.rep.Errors = append(v.rep.Errors, is)
	} else {
		v.rep.Warnings = append(v.rep.Warnings, is)
	}
}

func (v *validator) err(code, kind, field string, refs ...Ref) {
	v.add(SeverityError, code, kind, field, nil, refs...)
}

func (v *validator) warn(code, kind string, refs ...Ref) {
	v.add(SeverityWarning, code, kind, "", nil, refs...)
}

// ValidateMap checks a complete map document (spec 8.4). Errors block the save; warnings are reported. The
// report is deterministic: issues are sorted by code, then by refs.
func ValidateMap(m Map, s Settings) Report {
	if s.MaxElements <= 0 {
		s.MaxElements = MaxElementsPerMap
	}
	v := &validator{
		m: &m, s: s,
		mapB:       geo.Box{Max: geo.Point{X: m.Width, Y: m.Height}},
		structures: make(map[Ref]*Structure, len(m.Structures)),
		pedestals:  make(map[Ref]*Pedestal, len(m.Pedestals)),
		badStruct:  map[Ref]bool{},
	}
	if n := m.ElementCount(); n > s.MaxElements {
		v.add(SeverityError, CodeTooManyElements, KindMap, "", map[string]int64{"count": int64(n), "max": int64(s.MaxElements)})
		return v.rep
	}
	if m.Width < MinMapSide || m.Height < MinMapSide || m.Width > MaxMapSide || m.Height > MaxMapSide {
		v.err(CodeInvalidGeometry, KindMap, "width_cm")
	}
	v.mapPolygons()
	v.checkStructures()
	v.checkPedestals()
	berthOK := v.checkBerths()
	bands, surfaces := v.checkAreas()
	landOK := v.checkLand()
	v.checkDecorations()
	v.checkCodes()
	v.checkOverlaps(berthOK, landOK, bands, surfaces)
	v.checkLinks()
	sortIssues(v.rep.Errors)
	sortIssues(v.rep.Warnings)
	return v.rep
}

func sortIssues(list []Issue) {
	slices.SortStableFunc(list, func(a, b Issue) int {
		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		for i := 0; i < len(a.Refs) && i < len(b.Refs); i++ {
			if c := cmp.Compare(a.Refs[i], b.Refs[i]); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(len(a.Refs), len(b.Refs)); c != 0 {
			return c
		}
		return cmp.Compare(a.Field, b.Field)
	})
}

func (v *validator) inMap(b geo.Box) bool {
	return b.Min.X >= 0 && b.Min.Y >= 0 && b.Max.X <= v.m.Width && b.Max.Y <= v.m.Height
}

func pointsInBounds(pts []geo.Point) bool {
	for _, p := range pts {
		if p.X < -geo.MaxCoord || p.X > geo.MaxCoord || p.Y < -geo.MaxCoord || p.Y > geo.MaxCoord {
			return false
		}
	}
	return true
}

func (v *validator) mapPolygons() {
	check := func(field string, p geo.Polygon) {
		if p.Validate() != nil {
			v.err(CodeInvalidGeometry, KindMap, field)
		}
	}
	for i, p := range v.m.Water {
		check(fmt.Sprintf("water_polygons[%d]", i), p)
	}
	for i, p := range v.m.Land {
		check(fmt.Sprintf("land_polygons[%d]", i), p)
	}
	if len(v.m.Concession) > 0 {
		check("concession_outline", v.m.Concession)
	}
}

// ---- structures ----

func (v *validator) checkStructures() {
	for i := range v.m.Structures {
		s := &v.m.Structures[i]
		v.structures[s.Ref] = s
		if field := structureGeometryError(s); field != "" {
			v.err(CodeInvalidGeometry, KindStructure, field, s.Ref)
			v.badStruct[s.Ref] = true
			continue
		}
		if !v.inMap(structureBox(s)) {
			v.err(CodeElementOutsideMap, KindStructure, "", s.Ref)
		}
	}
	for i := range v.m.Structures {
		s := &v.m.Structures[i]
		if s.Parent == "" {
			if s.Kind == StructureFinger {
				v.err(CodeDanglingReference, KindStructure, "parent_id", s.Ref)
			}
			continue
		}
		p, ok := v.structures[s.Parent]
		want := map[StructureKind]StructureKind{StructureFinger: StructurePier, StructurePier: StructureQuay}[s.Kind]
		if !ok || p == s || want == "" || p.Kind != want {
			v.err(CodeDanglingReference, KindStructure, "parent_id", s.Ref)
		}
	}
}

// structureGeometryError returns the invalid field of a structure, or "".
func structureGeometryError(s *Structure) string {
	if s.Kind == StructureFinger {
		if s.Rect == nil || s.Rect.Validate() != nil {
			return "rect"
		}
		return ""
	}
	if s.Rect != nil {
		return "rect"
	}
	if s.Closed {
		if geo.Polygon(s.Points).Validate() != nil {
			return "points"
		}
		return ""
	}
	if s.Kind == StructureChannel {
		return "closed"
	}
	if len(s.Points) < 2 || !pointsInBounds(s.Points) {
		return "points"
	}
	for i := 1; i < len(s.Points); i++ {
		if s.Points[i] == s.Points[i-1] {
			return "points"
		}
	}
	if s.Width < 0 || s.Width > geo.MaxCoord || (s.Kind == StructurePier && s.Width == 0) {
		return "width_cm"
	}
	for _, side := range s.Sides {
		switch side {
		case SideLeft, SideRight:
		case SideHead:
			if s.Kind != StructurePier {
				return "sides"
			}
		default:
			return "sides"
		}
	}
	if s.Kind == StructureQuay && len(s.Sides) != 1 {
		return "sides"
	}
	for _, d := range s.Depth {
		if d.From < 0 || d.To < d.From || d.Depth <= 0 {
			return "depth_profile"
		}
	}
	return ""
}

// ---- pedestals ----

func (v *validator) checkPedestals() {
	for i := range v.m.Pedestals {
		p := &v.m.Pedestals[i]
		v.pedestals[p.Ref] = p
		if !pointsInBounds([]geo.Point{p.At}) {
			v.err(CodeInvalidGeometry, KindPedestal, "position", p.Ref)
			continue
		}
		if !v.inMap(geo.Box{Min: p.At, Max: p.At}) {
			v.err(CodeElementOutsideMap, KindPedestal, "", p.Ref)
		}
		seen := map[string]bool{}
		for _, so := range p.Sockets {
			if so.Code == "" || seen[so.Code] {
				v.err(CodeInvalidGeometry, KindPedestal, "sockets", p.Ref)
				break
			}
			seen[so.Code] = true
		}
	}
}

// ---- berths ----

func (v *validator) checkBerths() []bool {
	ok := make([]bool, len(v.m.Berths))
	for i := range v.m.Berths {
		b := &v.m.Berths[i]
		var bounds geo.Box
		if b.IsBuoy() {
			if b.SwingRadius <= 0 || b.SwingRadius > geo.MaxCoord || !pointsInBounds([]geo.Point{b.Rect.Anchor}) {
				v.err(CodeInvalidGeometry, KindBerth, "swing_radius_cm", b.Ref)
				continue
			}
			bounds = circleBox(b.Circle())
		} else {
			if b.Rect.Validate() != nil || degenerate(b.Rect) {
				v.err(CodeInvalidGeometry, KindBerth, "rect", b.Ref)
				continue
			}
			bounds = b.Rect.Bounds()
		}
		ok[i] = true
		if !v.inMap(bounds) {
			v.err(CodeElementOutsideMap, KindBerth, "", b.Ref)
		}
		if !b.IsBuoy() && !b.OverhangAllowed && b.MaxLOA > b.Rect.Length+v.s.LOATolerance {
			v.add(SeverityError, CodeLOAExceedsLength, KindBerth, "max_loa_cm",
				map[string]int64{"max_loa_cm": int64(b.MaxLOA), "length_cm": int64(b.Rect.Length), "tolerance_cm": int64(v.s.LOATolerance)}, b.Ref)
		}
		if b.Structure != "" {
			s, found := v.structures[b.Structure]
			if !found || s.Kind == StructureChannel {
				v.err(CodeDanglingReference, KindBerth, "structure_id", b.Ref)
			}
		}
		if b.Pedestal != "" {
			p, found := v.pedestals[b.Pedestal]
			if !found {
				v.err(CodeDanglingReference, KindBerth, "pedestal_id", b.Ref)
			} else if b.PedestalSocket != "" && !slices.ContainsFunc(p.Sockets, func(s Socket) bool { return s.Code == b.PedestalSocket }) {
				v.err(CodePedestalSocketUnknown, KindBerth, "pedestal_socket", b.Ref)
			}
		} else if b.PedestalSocket != "" {
			v.err(CodePedestalSocketUnknown, KindBerth, "pedestal_socket", b.Ref)
		}
		v.berthWarnings(b)
	}
	return ok
}

func (v *validator) berthWarnings(b *Berth) {
	if b.PriceClass == "" {
		v.warn(CodePriceClassMissing, KindBerth, b.Ref)
	}
	if !b.IsBuoy() && (b.Rect.Width < MinBerthWidth || b.Rect.Length < MinBerthLength) {
		v.add(SeverityWarning, CodeBerthBelowMinSize, KindBerth, "rect", map[string]int64{
			"width_cm": int64(b.Rect.Width), "length_cm": int64(b.Rect.Length),
			"min_width_cm": int64(MinBerthWidth), "min_length_cm": int64(MinBerthLength),
		}, b.Ref)
	}
	if len(v.m.Water) > 0 {
		pts := []geo.Point{b.Rect.Anchor}
		if !b.IsBuoy() {
			c := b.Rect.Corners()
			pts = c[:]
		}
		inside := slices.ContainsFunc(v.m.Water, func(w geo.Polygon) bool {
			if w.Validate() != nil {
				return false
			}
			for _, p := range pts {
				if !w.Contains(p) {
					return false
				}
			}
			return true
		})
		if !inside {
			v.warn(CodeBerthOutsideWater, KindBerth, b.Ref)
		}
	}
	s := v.structures[b.Structure]
	if s != nil && v.badStruct[s.Ref] {
		s = nil
	}
	if !b.IsBuoy() {
		attached := false
		if s != nil {
			for _, e := range edgesOf(s) {
				if cmpSegDist(b.Rect.Anchor, e.A, e.B, AttachDistance) <= 0 {
					attached = true
					break
				}
			}
		}
		if !attached {
			v.warn(CodeBerthUnattached, KindBerth, b.Ref)
		}
	}
	if b.Depth == 0 && !v.depthCovered(b, s) {
		v.warn(CodeDepthMissing, KindBerth, b.Ref)
	}
}

// depthCovered reports whether the depth profile of the berth's structure (or of the finger's pier) covers the
// projection of its anchor on the structure axis.
func (v *validator) depthCovered(b *Berth, s *Structure) bool {
	if s == nil {
		return false
	}
	if s.Kind == StructureFinger {
		p := v.structures[s.Parent]
		if p == nil || v.badStruct[p.Ref] {
			return false
		}
		s = p
	}
	if len(s.Depth) == 0 || s.Closed || len(s.Points) < 2 {
		return false
	}
	at, _ := projectOnPolyline(s.Points, b.Rect.Anchor)
	return slices.ContainsFunc(s.Depth, func(d DepthSpan) bool { return d.From <= at && at <= d.To })
}

// ---- dynamic areas ----

type surfaceShape struct {
	idx    int
	poly   geo.Polygon // doubled
	pieces []geo.Polygon
}

func (v *validator) checkAreas() (bands map[int]geo.Rect, surfaces []surfaceShape) {
	bands = map[int]geo.Rect{}
	for i := range v.m.Areas {
		d := &v.m.Areas[i]
		if d.Cell < Snap || d.Cell > 1000 || d.Margin < 0 || d.Margin > 1000 {
			v.err(CodeInvalidGeometry, KindDynamicArea, "cell_cm", d.Ref)
			continue
		}
		switch d.Kind {
		case AreaLinear:
			if r, ok := v.checkLinear(d); ok {
				bands[i] = r
			}
		case AreaSurface:
			if p, ok := v.checkSurface(d); ok {
				dp := doubled(p)
				surfaces = append(surfaces, surfaceShape{idx: i, poly: dp, pieces: convexPieces(dp)})
			}
		default:
			v.err(CodeInvalidGeometry, KindDynamicArea, "kind", d.Ref)
		}
	}
	return bands, surfaces
}

func (v *validator) checkLinear(d *DynamicArea) (geo.Rect, bool) {
	if d.Structure == "" {
		v.err(CodeAreaCellsOutside, KindDynamicArea, "structure_id", d.Ref)
		return geo.Rect{}, false
	}
	s, ok := v.structures[d.Structure]
	if !ok || (s.Kind != StructurePier && s.Kind != StructureQuay) {
		v.err(CodeDanglingReference, KindDynamicArea, "structure_id", d.Ref)
		return geo.Rect{}, false
	}
	if v.badStruct[s.Ref] {
		return geo.Rect{}, false
	}
	if !slices.Contains(usableSides(s), d.Side) {
		v.err(CodeAreaCellsOutside, KindDynamicArea, "side", d.Ref)
		return geo.Rect{}, false
	}
	l := SupportLength(s, d.Side)
	if d.From < 0 || d.To > l || d.From >= d.To {
		v.add(SeverityError, CodeAreaCellsOutside, KindDynamicArea, "to_cm",
			map[string]int64{"from_cm": int64(d.From), "to_cm": int64(d.To), "support_cm": int64(l)}, d.Ref)
		return geo.Rect{}, false
	}
	r, ok := linearBand(d, s)
	if !ok || r.Validate() != nil {
		v.err(CodeInvalidGeometry, KindDynamicArea, "from_cm", d.Ref)
		return geo.Rect{}, false
	}
	if !v.inMap(r.Bounds()) {
		v.err(CodeElementOutsideMap, KindDynamicArea, "", d.Ref)
	}
	return r, true
}

func (v *validator) checkSurface(d *DynamicArea) (geo.Polygon, bool) {
	if d.GridRotation%900 != 0 || d.GridRotation < 0 || d.GridRotation > 3599 {
		v.err(CodeInvalidGeometry, KindDynamicArea, "grid_rotation_dd", d.Ref)
		return nil, false
	}
	if d.Outline.Validate() != nil {
		v.err(CodeInvalidGeometry, KindDynamicArea, "outline", d.Ref)
		return nil, false
	}
	if !v.inMap(d.Outline.Bounds()) {
		v.err(CodeAreaCellsOutside, KindDynamicArea, "outline", d.Ref)
		return d.Outline, true
	}
	a, err := AreaOf(*d, *v.m)
	if err != nil {
		v.err(CodeInvalidGeometry, KindDynamicArea, "cell_cm", d.Ref)
		return d.Outline, true
	}
	n := CellCount(a)
	for _, list := range [][]int{d.Blocked, d.Exits} {
		if slices.ContainsFunc(list, func(i int) bool { return i < 0 || i >= n }) {
			v.add(SeverityError, CodeAreaCellsOutside, KindDynamicArea, "blocked_cells", map[string]int64{"cell_count": int64(n)}, d.Ref)
			return d.Outline, true
		}
	}
	if !slices.Contains(a.Usable, true) {
		v.err(CodeAreaCellsOutside, KindDynamicArea, "cell_cm", d.Ref)
	}
	return d.Outline, true
}

// ---- land resources and decorations ----

// degenerate reports a rectangle too thin for the tolerant overlap test (MinRectSide).
func degenerate(r geo.Rect) bool {
	return r.Width < MinRectSide || r.Length < MinRectSide
}

func (v *validator) checkLand() []bool {
	ok := make([]bool, len(v.m.LandResources))
	for i := range v.m.LandResources {
		l := &v.m.LandResources[i]
		if l.Rect.Validate() != nil || degenerate(l.Rect) {
			v.err(CodeInvalidGeometry, KindLandResource, "rect", l.Ref)
			continue
		}
		ok[i] = true
		if !v.inMap(l.Rect.Bounds()) {
			v.err(CodeElementOutsideMap, KindLandResource, "", l.Ref)
		}
	}
	return ok
}

func (v *validator) checkDecorations() {
	for i := range v.m.Decorations {
		d := &v.m.Decorations[i]
		pts := append([]geo.Point{d.At}, d.Points...)
		if !pointsInBounds(pts) || d.Rotation < 0 || d.Rotation > 3599 || d.Size < 0 {
			v.err(CodeInvalidGeometry, KindDecoration, "position", d.Ref)
			continue
		}
		if d.Kind == DecorationSymbol && !ValidAsset(d.Asset) {
			v.err(CodeAssetUnknown, KindDecoration, "asset", d.Ref)
		}
	}
}

// ---- codes ----

func (v *validator) checkCodes() {
	type owner struct {
		ref  Ref
		kind string
	}
	families := []map[string]owner{{}, {}, {}}
	add := func(fam int, code string, ref Ref, kind string) {
		if first, ok := families[fam][code]; ok {
			v.add(SeverityError, CodeDuplicateCode, first.kind, "code", nil, first.ref, ref)
			return
		}
		families[fam][code] = owner{ref, kind}
	}
	for i := range v.m.Structures {
		add(0, v.m.Structures[i].Code, v.m.Structures[i].Ref, KindStructure)
	}
	for i := range v.m.Berths {
		add(1, v.m.Berths[i].Code, v.m.Berths[i].Ref, KindBerth)
	}
	for i := range v.m.Areas {
		add(1, v.m.Areas[i].Code, v.m.Areas[i].Ref, KindDynamicArea)
	}
	for i := range v.m.LandResources {
		add(1, v.m.LandResources[i].Code, v.m.LandResources[i].Ref, KindLandResource)
	}
	for i := range v.m.Pedestals {
		add(2, v.m.Pedestals[i].Code, v.m.Pedestals[i].Ref, KindPedestal)
	}
}

// ---- links ----

func (v *validator) checkLinks() {
	linked := map[Ref]bool{}
	for i := range v.m.Berths {
		if p := v.m.Berths[i].Pedestal; p != "" {
			linked[p] = true
		}
	}
	for i := range v.m.Pedestals {
		if !linked[v.m.Pedestals[i].Ref] {
			v.warn(CodePedestalUnlinked, KindPedestal, v.m.Pedestals[i].Ref)
		}
	}
}

// ---- overlaps ----

type shapeKind uint8

const (
	shapeBerth shapeKind = iota
	shapeBuoy
	shapeLand
	shapeBand
	shapeSurface
)

type shape struct {
	kind  shapeKind
	ref   Ref
	ekind string
	box   geo.Box
	rect  geo.Rect
	circ  geo.Circle
	surf  *surfaceShape
}

func (v *validator) checkOverlaps(berthOK, landOK []bool, bands map[int]geo.Rect, surfaces []surfaceShape) {
	var shapes []shape
	for i := range v.m.Berths {
		if !berthOK[i] {
			continue
		}
		b := &v.m.Berths[i]
		if b.IsBuoy() {
			shapes = append(shapes, shape{kind: shapeBuoy, ref: b.Ref, ekind: KindBerth, box: circleBox(b.Circle()), circ: b.Circle()})
		} else {
			shapes = append(shapes, shape{kind: shapeBerth, ref: b.Ref, ekind: KindBerth, box: b.Rect.Bounds(), rect: b.Rect})
		}
	}
	for i := range v.m.LandResources {
		if landOK[i] {
			l := &v.m.LandResources[i]
			shapes = append(shapes, shape{kind: shapeLand, ref: l.Ref, ekind: KindLandResource, box: l.Rect.Bounds(), rect: l.Rect})
		}
	}
	for i := range v.m.Areas {
		if r, ok := bands[i]; ok {
			shapes = append(shapes, shape{kind: shapeBand, ref: v.m.Areas[i].Ref, ekind: KindDynamicArea, box: r.Bounds(), rect: r})
		}
	}
	for i := range surfaces {
		s := &surfaces[i]
		shapes = append(shapes, shape{kind: shapeSurface, ref: v.m.Areas[s.idx].Ref, ekind: KindDynamicArea, box: v.m.Areas[s.idx].Outline.Bounds(), surf: s})
	}
	forEachCandidatePair(shapes, v.m.Width, v.m.Height, func(a, b *shape) {
		if a.kind > b.kind {
			a, b = b, a
		}
		code, sev := pairRule(a.kind, b.kind)
		if code == "" || !shapesOverlap(a, b) {
			return
		}
		r1, r2 := a, b
		if r2.ref < r1.ref {
			r1, r2 = r2, r1
		}
		v.add(sev, code, r1.ekind, "", nil, r1.ref, r2.ref)
	})
}

// pairRule returns the issue of an overlapping pair (kinds ordered), or "" when the pair is not checked.
func pairRule(a, b shapeKind) (string, Severity) {
	switch {
	case a == shapeBerth && b == shapeBerth:
		return CodeBerthOverlap, SeverityError
	case a == shapeBuoy && b == shapeBuoy:
		return CodeBuoySwingOverlap, SeverityWarning
	case a == shapeBerth && b == shapeBuoy:
		return CodeBuoyBerthOverlap, SeverityWarning
	case a == shapeBuoy:
		return "", ""
	case a == shapeBerth && b == shapeLand:
		return CodeOccupiableOverlap, SeverityError
	case a >= shapeLand || b >= shapeBand:
		return CodeOccupiableOverlap, SeverityError
	}
	return "", ""
}

func shapesOverlap(a, b *shape) bool {
	switch {
	case a.kind == shapeBuoy && b.kind == shapeBuoy:
		return geo.CirclesOverlap(a.circ, b.circ)
	case a.kind == shapeBerth && b.kind == shapeBuoy:
		return circleHitsRect(b.circ, a.rect)
	case a.kind != shapeSurface && b.kind != shapeSurface:
		return BerthsOverlap(a.rect, b.rect)
	case a.kind == shapeSurface && b.kind == shapeSurface:
		return piecesOverlap(a.surf.pieces, b.surf.pieces, a.surf.poly, b.surf.poly)
	default:
		r, s := a, b
		if r.kind == shapeSurface {
			r, s = s, r
		}
		tp := tolerantPolygon(r.rect)
		if tp == nil {
			return false
		}
		return piecesOverlap([]geo.Polygon{tp}, s.surf.pieces, tp, s.surf.poly)
	}
}

// circleHitsRect reports whether a circle and a rectangle have overlapping interiors.
func circleHitsRect(c geo.Circle, r geo.Rect) bool {
	p := r.Polygon()
	if p.ContainsStrict(c.Center) {
		return true
	}
	for i := range p {
		if cmpSegDist(c.Center, p[i], p[(i+1)%len(p)], c.Radius) < 0 {
			return true
		}
	}
	return false
}

// indexCell is the side of the uniform bucketing grid (m1-plan §2.3).
const indexCell geo.Cm = 1000

// forEachCandidatePair calls fn once for every pair of shapes whose bounding boxes intersect, bucketing them in
// a uniform grid. Each pair is reported in the cell that holds the minimum corner of the intersection of their
// boxes, so no deduplication map is needed. Cells are clamped to the map (plus one cell), which keeps out-of-map
// elements cheap without losing pairs.
func forEachCandidatePair(shapes []shape, w, h geo.Cm, fn func(a, b *shape)) {
	maxC := int(int64(w)/int64(indexCell)) + 1
	maxR := int(int64(h)/int64(indexCell)) + 1
	clamp := func(v geo.Cm, hi int) int {
		c := int(floorDiv(int64(v), int64(indexCell)))
		return min(max(c, -1), hi)
	}
	cols := maxC + 2
	buckets := map[int][]int{}
	for i := range shapes {
		b := shapes[i].box
		for r := clamp(b.Min.Y, maxR); r <= clamp(b.Max.Y, maxR); r++ {
			for c := clamp(b.Min.X, maxC); c <= clamp(b.Max.X, maxC); c++ {
				k := (r+1)*cols + (c + 1)
				buckets[k] = append(buckets[k], i)
			}
		}
	}
	keys := make([]int, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		list := buckets[k]
		r, c := k/cols-1, k%cols-1
		for x := 0; x < len(list); x++ {
			a := &shapes[list[x]]
			for y := x + 1; y < len(list); y++ {
				b := &shapes[list[y]]
				if !a.box.Intersects(b.box) {
					continue
				}
				if clamp(max(a.box.Min.X, b.box.Min.X), maxC) != c || clamp(max(a.box.Min.Y, b.box.Min.Y), maxR) != r {
					continue
				}
				fn(a, b)
			}
		}
	}
}
