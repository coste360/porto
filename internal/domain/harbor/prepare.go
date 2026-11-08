package harbor

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Field validation and reference resolution of a map document (m1-plan §4.3 "Map save field validation").
// Shape errors (types, ranges, unknown ids, duplicate keys) are validation_failed; geometric and semantic rules
// are left to layout.ValidateMap (map_invalid).

var (
	keyPattern = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,64}$`)

	mapKinds        = []string{"water", "yard", "shed", "parking", "boxes", "other"}
	berthSides      = []string{"left", "right", "head", "none"}
	areaSides       = []string{"left", "right", "head"}
	powerAmps       = []int64{16, 32, 63, 125}
	powerPhases     = []string{"single", "three"}
	supportKinds    = []string{"cradle", "trailer", "rack"}
	utilityKinds    = []string{"power", "water"}
	gridRotations   = []int64{0, 900, 1800, 2700}
	imageMediaTypes = []string{"image/png", "image/jpeg", "image/svg+xml"}
)

// Element kinds of berth_group_members.resource_type.
const (
	resourceBerth = "berth"
	resourceArea  = "dynamic_area"
	resourceLand  = "land_resource"
)

func enumOf[T ~string](vals []T) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = string(v)
	}
	return out
}

// checker collects field errors with JSON paths.
type checker struct{ v *auth.ValidationError }

func (c checker) add(field, code string) { c.v.Add(field, code) }

func (c checker) enum(field, val string, vals []string) {
	if !slices.Contains(vals, val) {
		c.add(field, "invalid")
	}
}

func (c checker) rangeI(field string, val, lo, hi int64) {
	if val < lo || val > hi {
		c.add(field, "out_of_range")
	}
}

func (c checker) point(field string, p geo.Point) {
	if p.X < -geo.MaxCoord || p.X > geo.MaxCoord || p.Y < -geo.MaxCoord || p.Y > geo.MaxCoord {
		c.add(field, "out_of_range")
	}
}

func (c checker) points(field string, pts []geo.Point, maxN int) {
	if len(pts) > maxN {
		c.add(field, "too_many")
		return
	}
	for i, p := range pts {
		c.point(fmt.Sprintf("%s[%d]", field, i), p)
	}
}

func (c checker) text(field, val string, maxLen int, required bool) {
	if required && strings.TrimSpace(val) == "" {
		c.add(field, "required")
		return
	}
	if utf8.RuneCountInString(val) > maxLen || !utf8.ValidString(val) || strings.ContainsFunc(val, func(r rune) bool { return r < 0x20 && r != '\n' }) {
		c.add(field, "invalid")
	}
}

func (c checker) i18n(field string, t I18nText, required bool) {
	c.text(field+".it", t.It, 200, required)
	if t.En != nil {
		c.text(field+".en", *t.En, 200, false)
	}
}

func (c checker) code(field, val string) {
	if !layout.ValidCode(val) {
		c.add(field, "invalid")
	}
}

func (c checker) tags(field string, tags []string) {
	if len(tags) > 20 {
		c.add(field, "too_many")
		return
	}
	for i, t := range tags {
		c.text(fmt.Sprintf("%s[%d]", field, i), t, 32, true)
	}
}

func (c checker) length(field string, v int64, required bool) {
	if v == 0 && !required {
		return
	}
	c.rangeI(field, v, 1, int64(layout.MaxMapSide)*2)
}

func (c checker) depthProfile(field string, spans []DepthSpan) {
	if len(spans) > 100 {
		c.add(field, "too_many")
	}
	for i, d := range spans {
		if d.FromCm < 0 || d.ToCm < d.FromCm || d.DepthCm <= 0 {
			c.add(fmt.Sprintf("%s[%d]", field, i), "invalid")
		}
	}
}

// checkMeta validates the map metadata.
func checkMeta(c checker, m *Meta) {
	c.enum("kind", m.Kind, mapKinds)
	c.i18n("name", m.Name, true)
	c.text("icon", m.Icon, 32, true)
	c.rangeI("width_cm", m.WidthCm, int64(layout.MinMapSide), int64(layout.MaxMapSide))
	c.rangeI("height_cm", m.HeightCm, int64(layout.MinMapSide), int64(layout.MaxMapSide))
	c.rangeI("grid_cm", m.GridCm, 1, 10000)
	if len(m.WaterPolygons) > 100 || len(m.LandPolygons) > 100 {
		c.add("water_polygons", "too_many")
	}
	for i, p := range m.WaterPolygons {
		c.points(fmt.Sprintf("water_polygons[%d]", i), p, geo.MaxPolygonPoints)
	}
	for i, p := range m.LandPolygons {
		c.points(fmt.Sprintf("land_polygons[%d]", i), p, geo.MaxPolygonPoints)
	}
	c.points("concession_outline", m.ConcessionOutline, geo.MaxPolygonPoints)
	if b := m.Background; b != nil {
		c.rangeI("background.scale_mcm_per_px", b.ScaleMcmPerPx, 1, 1_000_000_000)
		c.rangeI("background.rotation_dd", b.RotationDd, 0, 3599)
		c.rangeI("background.opacity_pct", int64(b.OpacityPct), 0, 100)
		c.point("background.origin", b.Origin)
	}
	if g := m.Georef; g != nil {
		if len(g.Points) != 3 {
			c.add("georef.points", "invalid")
		} else {
			for i, p := range g.Points {
				c.point(fmt.Sprintf("georef.points[%d]", i), geo.Point{X: geo.Cm(p.X), Y: geo.Cm(p.Y)})
				c.rangeI(fmt.Sprintf("georef.points[%d].lat_e7", i), p.LatE7, -900_000_000, 900_000_000)
				c.rangeI(fmt.Sprintf("georef.points[%d].lon_e7", i), p.LonE7, -1_800_000_000, 1_800_000_000)
			}
			if _, err := georefOf(g); err != nil {
				c.add("georef.points", "collinear")
			}
		}
	}
}

func georefOf(g *Georef) (layout.Georef, error) {
	var cps [3]layout.ControlPoint
	for i := range 3 {
		p := g.Points[i]
		cps[i] = layout.ControlPoint{At: geo.Point{X: geo.Cm(p.X), Y: geo.Cm(p.Y)}, LatE7: p.LatE7, LonE7: p.LonE7}
	}
	return layout.NewGeoref(cps)
}

// existing is the set of saved element ids of a map, with their kinds.
type existing map[string]string

func existingOf(e *store.MapElements) existing {
	out := existing{}
	for _, r := range e.Structures {
		out[r.ID] = layout.KindStructure
	}
	for _, r := range e.Pedestals {
		out[r.ID] = layout.KindPedestal
	}
	for _, r := range e.Berths {
		out[r.ID] = layout.KindBerth
	}
	for _, r := range e.Areas {
		out[r.ID] = layout.KindDynamicArea
	}
	for _, r := range e.LandResources {
		out[r.ID] = layout.KindLandResource
	}
	for _, r := range e.Decorations {
		out[r.ID] = layout.KindDecoration
	}
	return out
}

// prepared is a checked document: the layout model for validation and, after Resolve, the rows to store.
type prepared struct {
	req    *SaveRequest
	layout layout.Map
	ids    map[string]string // token (id or key) → final id
	idMap  map[string]string // key → new id
}

// prepare checks the shape of a document against the saved state of the map and builds its layout model.
// groupIDs are the berth groups of the marina; files says whether a background file id is a map background of
// the marina.
func prepare(req *SaveRequest, saved existing, groupIDs map[string]bool, background func(id string) (*store.StoredFile, bool)) (*prepared, error) {
	c := checker{&auth.ValidationError{}}
	checkMeta(c, &req.Meta)
	if n := req.Count(); n > layout.MaxElementsPerMap {
		c.add("elements", "too_many")
		return nil, c.v
	}
	if b := req.Background; b != nil {
		f, ok := background(b.FileID)
		if !ok {
			c.add("background.file_id", "unknown_element")
		} else {
			b.ContentType, b.WidthPx, b.HeightPx = f.ContentType, f.WidthPx, f.HeightPx
		}
	}
	p := &prepared{req: req, ids: map[string]string{}, idMap: map[string]string{}}
	seenID := map[string]bool{}
	token := func(field, kind, id, key string) string {
		switch {
		case id != "" && key != "":
			c.add(field+".id", "conflict")
		case id != "":
			if saved[id] != kind {
				c.add(field+".id", "unknown_element")
			} else if seenID[id] {
				c.add(field+".id", "duplicate")
			}
			seenID[id] = true
			return id
		case key != "":
			if !keyPattern.MatchString(key) {
				c.add(field+".key", "invalid")
			} else if _, isID := saved[key]; isID {
				c.add(field+".key", "collides_with_id")
			} else if _, dup := p.idMap[key]; dup {
				c.add(field+".key", "duplicate")
			} else {
				p.idMap[key] = store.NewID()
				return key
			}
		default:
			c.add(field+".key", "required")
		}
		return ""
	}
	groups := func(field string, ids []string) {
		if len(ids) > 50 {
			c.add(field, "too_many")
		}
		for j, g := range ids {
			if !groupIDs[g] {
				c.add(fmt.Sprintf("%s[%d]", field, j), "unknown_element")
			}
		}
	}
	lm := &p.layout
	lm.Width, lm.Height, lm.Grid = geo.Cm(req.WidthCm), geo.Cm(req.HeightCm), geo.Cm(req.GridCm)
	for _, w := range req.WaterPolygons {
		lm.Water = append(lm.Water, geo.Polygon(w))
	}
	for _, l := range req.LandPolygons {
		lm.Land = append(lm.Land, geo.Polygon(l))
	}
	lm.Concession = req.ConcessionOutline

	for i := range req.Structures {
		s := &req.Structures[i]
		f := fmt.Sprintf("structures[%d]", i)
		ref := token(f, layout.KindStructure, s.ID, s.Key)
		c.enum(f+".kind", s.Kind, enumOf(layout.StructureKinds))
		c.code(f+".code", s.Code)
		c.text(f+".name", s.Name, 100, false)
		c.points(f+".points", s.Points, geo.MaxPolygonPoints)
		c.length(f+".width_cm", s.WidthCm, false)
		c.depthProfile(f+".depth_profile", s.DepthProfile)
		if s.BollardSpacingCm < 0 {
			c.add(f+".bollard_spacing_cm", "out_of_range")
		}
		ls := layout.Structure{
			Ref: layout.Ref(ref), Kind: layout.StructureKind(s.Kind), Code: s.Code, Points: s.Points, Closed: s.Closed,
			Width: geo.Cm(s.WidthCm), Parent: layout.Ref(s.ParentID), Floating: s.Floating,
		}
		for j, side := range s.Sides {
			c.enum(fmt.Sprintf("%s.sides[%d]", f, j), side, areaSides)
			ls.Sides = append(ls.Sides, layout.Side(side))
		}
		if s.Rect != nil {
			c.point(f+".rect.anchor", s.Rect.Anchor)
			c.length(f+".rect.width_cm", s.Rect.WidthCm, true)
			c.length(f+".rect.length_cm", s.Rect.LengthCm, true)
			c.rangeI(f+".rect.rotation_dd", s.Rect.RotationDd, 0, 3599)
			ls.Rect = &geo.Rect{Anchor: s.Rect.Anchor, Width: geo.Cm(s.Rect.WidthCm), Length: geo.Cm(s.Rect.LengthCm), Rotation: geo.Angle(s.Rect.RotationDd)}
		}
		for _, d := range s.DepthProfile {
			ls.Depth = append(ls.Depth, layout.DepthSpan{From: geo.Cm(d.FromCm), To: geo.Cm(d.ToCm), Depth: geo.Cm(d.DepthCm)})
		}
		lm.Structures = append(lm.Structures, ls)
	}
	for i := range req.Pedestals {
		pd := &req.Pedestals[i]
		f := fmt.Sprintf("pedestals[%d]", i)
		ref := token(f, layout.KindPedestal, pd.ID, pd.Key)
		c.code(f+".code", pd.Code)
		c.point(f+".position", pd.Position)
		if len(pd.Sockets) > 32 || len(pd.Meters) > 32 {
			c.add(f+".sockets", "too_many")
		}
		lp := layout.Pedestal{Ref: layout.Ref(ref), Code: pd.Code, At: pd.Position}
		for j, so := range pd.Sockets {
			sf := fmt.Sprintf("%s.sockets[%d]", f, j)
			c.text(sf+".code", so.Code, 16, true)
			c.enum(sf+".kind", so.Kind, utilityKinds)
			if so.Amps != 0 && !slices.Contains(powerAmps, so.Amps) {
				c.add(sf+".amps", "invalid")
			}
			if so.Phase != "" {
				c.enum(sf+".phase", so.Phase, powerPhases)
			}
			lp.Sockets = append(lp.Sockets, layout.Socket{Code: so.Code, Kind: so.Kind, Amps: so.Amps, Phase: so.Phase})
		}
		for j, me := range pd.Meters {
			c.enum(fmt.Sprintf("%s.meters[%d].kind", f, j), me.Kind, utilityKinds)
			c.text(fmt.Sprintf("%s.meters[%d].serial", f, j), me.Serial, 64, true)
		}
		lm.Pedestals = append(lm.Pedestals, lp)
	}
	for i := range req.Berths {
		b := &req.Berths[i]
		f := fmt.Sprintf("berths[%d]", i)
		ref := token(f, layout.KindBerth, b.ID, b.Key)
		c.code(f+".code", b.Code)
		c.enum(f+".side", b.Side, berthSides)
		c.enum(f+".mooring_type", b.MooringType, enumOf(layout.MooringTypes))
		c.point(f+".anchor", b.Anchor)
		c.rangeI(f+".rotation_dd", b.RotationDd, 0, 3599)
		c.length(f+".width_cm", b.WidthCm, true)
		c.length(f+".length_cm", b.LengthCm, true)
		c.length(f+".max_loa_cm", b.MaxLOACm, false)
		c.length(f+".max_beam_cm", b.MaxBeamCm, false)
		c.length(f+".depth_cm", b.DepthCm, false)
		c.length(f+".max_air_draft_cm", b.MaxAirDraftCm, false)
		c.length(f+".swing_radius_cm", b.SwingRadiusCm, false)
		if b.MaxDisplacementKg < 0 {
			c.add(f+".max_displacement_kg", "out_of_range")
		}
		if b.MooringType == string(layout.MooringBuoy) && b.SwingRadiusCm == 0 {
			c.add(f+".swing_radius_cm", "required")
		}
		if b.PowerA != 0 && !slices.Contains(powerAmps, b.PowerA) {
			c.add(f+".power_a", "invalid")
		}
		if b.PowerPhase != "" {
			c.enum(f+".power_phase", b.PowerPhase, powerPhases)
		}
		c.text(f+".pedestal_socket", b.PedestalSocket, 16, false)
		c.text(f+".price_class", b.PriceClass, 32, false)
		c.tags(f+".attributes", b.Attributes)
		groups(f+".group_ids", b.GroupIDs)
		if b.MaxLOACm == 0 {
			b.MaxLOACm = b.LengthCm
		}
		if b.MaxBeamCm == 0 {
			b.MaxBeamCm = int64(layout.DefaultMaxBeam(geo.Cm(b.WidthCm)))
		}
		lm.Berths = append(lm.Berths, layout.Berth{
			Ref: layout.Ref(ref), Code: b.Code, Structure: layout.Ref(b.StructureID), Side: layout.Side(b.Side), Position: b.Position,
			Rect:    geo.Rect{Anchor: b.Anchor, Width: geo.Cm(b.WidthCm), Length: geo.Cm(b.LengthCm), Rotation: geo.Angle(b.RotationDd)},
			Mooring: layout.MooringType(b.MooringType), SwingRadius: geo.Cm(b.SwingRadiusCm), MaxLOA: geo.Cm(b.MaxLOACm),
			MaxBeam: geo.Cm(b.MaxBeamCm), Depth: geo.Cm(b.DepthCm), MaxAirDraft: geo.Cm(b.MaxAirDraftCm), OverhangAllowed: b.OverhangAllowed,
			Pedestal: layout.Ref(b.PedestalID), PedestalSocket: b.PedestalSocket, PriceClass: b.PriceClass, Active: b.Active,
		})
	}
	for i := range req.DynamicAreas {
		a := &req.DynamicAreas[i]
		f := fmt.Sprintf("dynamic_areas[%d]", i)
		ref := token(f, layout.KindDynamicArea, a.ID, a.Key)
		c.enum(f+".kind", a.Kind, enumOf([]layout.AreaKind{layout.AreaLinear, layout.AreaSurface}))
		c.code(f+".code", a.Code)
		defaultsOfArea(a)
		c.rangeI(f+".cell_cm", *a.CellCm, 10, 1000)
		c.rangeI(f+".margin_cm", *a.MarginCm, 0, 1000)
		c.length(f+".max_loa_cm", a.MaxLOACm, false)
		c.length(f+".max_beam_cm", a.MaxBeamCm, false)
		c.length(f+".max_height_cm", a.MaxHeightCm, false)
		if a.MaxWeightKg < 0 {
			c.add(f+".max_weight_kg", "out_of_range")
		}
		if !slices.Contains(gridRotations, a.GridRotationDd) {
			c.add(f+".grid_rotation_dd", "invalid")
		}
		for j, s := range a.Supports {
			c.enum(fmt.Sprintf("%s.supports[%d]", f, j), s, supportKinds)
		}
		if len(a.BlockedCells) > layout.MaxSurfaceCells || len(a.Exits) > layout.MaxSurfaceCells {
			c.add(f+".blocked_cells", "too_many")
		}
		c.depthProfile(f+".depth_profile", a.DepthProfile)
		c.text(f+".price_class", a.PriceClass, 32, false)
		groups(f+".group_ids", a.GroupIDs)
		la := layout.DynamicArea{
			Ref: layout.Ref(ref), Code: a.Code, Kind: layout.AreaKind(a.Kind), Structure: layout.Ref(a.StructureID), Side: layout.Side(a.Side),
			Mode: layout.MooringMode(a.MooringMode), Outline: a.Outline, GridRotation: geo.Angle(a.GridRotationDd), Cell: geo.Cm(*a.CellCm),
			Margin: geo.Cm(*a.MarginCm), MaxLOA: geo.Cm(a.MaxLOACm), MaxBeam: geo.Cm(a.MaxBeamCm), MaxHeight: geo.Cm(a.MaxHeightCm),
			Blocked: a.BlockedCells, Exits: a.Exits,
		}
		switch a.Kind {
		case string(layout.AreaLinear):
			if a.FromCm == nil || a.ToCm == nil {
				c.add(f+".from_cm", "required")
			} else {
				la.From, la.To = geo.Cm(*a.FromCm), geo.Cm(*a.ToCm)
			}
			c.enum(f+".mooring_mode", a.MooringMode, []string{string(layout.ModeSternTo), string(layout.ModeAlongside)})
			c.enum(f+".side", a.Side, areaSides)
			if len(a.Outline) > 0 {
				c.add(f+".outline", "not_allowed")
			}
		case string(layout.AreaSurface):
			if len(a.Outline) == 0 {
				c.add(f+".outline", "required")
			}
			c.points(f+".outline", a.Outline, geo.MaxPolygonPoints)
			if a.StructureID != "" || a.FromCm != nil || a.ToCm != nil || a.MooringMode != "" || a.Side != "" {
				c.add(f+".structure_id", "not_allowed")
			}
		}
		lm.Areas = append(lm.Areas, la)
	}
	for i := range req.LandResources {
		l := &req.LandResources[i]
		f := fmt.Sprintf("land_resources[%d]", i)
		ref := token(f, layout.KindLandResource, l.ID, l.Key)
		c.enum(f+".kind", l.Kind, enumOf(layout.LandKinds))
		c.code(f+".code", l.Code)
		c.point(f+".anchor", l.Anchor)
		c.rangeI(f+".rotation_dd", l.RotationDd, 0, 3599)
		c.length(f+".width_cm", l.WidthCm, true)
		c.length(f+".length_cm", l.LengthCm, true)
		c.text(f+".price_class", l.PriceClass, 32, false)
		c.tags(f+".attributes", l.Attributes)
		groups(f+".group_ids", l.GroupIDs)
		lm.LandResources = append(lm.LandResources, layout.LandResource{
			Ref: layout.Ref(ref), Code: l.Code, Kind: layout.LandKind(l.Kind),
			Rect: geo.Rect{Anchor: l.Anchor, Width: geo.Cm(l.WidthCm), Length: geo.Cm(l.LengthCm), Rotation: geo.Angle(l.RotationDd)},
		})
	}
	for i := range req.Decorations {
		d := &req.Decorations[i]
		f := fmt.Sprintf("decorations[%d]", i)
		ref := token(f, layout.KindDecoration, d.ID, d.Key)
		c.enum(f+".kind", d.Kind, enumOf(layout.DecorationKinds))
		if d.Kind == string(layout.DecorationSymbol) && d.Asset == "" {
			c.add(f+".asset", "required")
		}
		c.text(f+".asset", d.Asset, 64, false)
		c.text(f+".variant", d.Variant, 32, false)
		if d.Label != nil {
			c.i18n(f+".label", *d.Label, false)
		}
		if d.Position == nil && len(d.Points) == 0 {
			c.add(f+".position", "required")
		}
		c.points(f+".points", d.Points, 1000)
		if d.RotationDd != nil {
			c.rangeI(f+".rotation_dd", *d.RotationDd, 0, 3599)
		}
		c.length(f+".size_cm", d.SizeCm, false)
		ld := layout.Decoration{Ref: layout.Ref(ref), Kind: layout.DecorationKind(d.Kind), Asset: d.Asset, Size: geo.Cm(d.SizeCm), Points: d.Points}
		if d.Position != nil {
			c.point(f+".position", *d.Position)
			ld.At = *d.Position
		}
		if d.RotationDd != nil {
			ld.Rotation = geo.Angle(*d.RotationDd)
		}
		lm.Decorations = append(lm.Decorations, ld)
	}
	if err := c.v.Err(); err != nil {
		return nil, err
	}
	for id := range seenID {
		p.ids[id] = id
	}
	for key, id := range p.idMap {
		p.ids[key] = id
	}
	return p, nil
}

// defaultsOfArea fills the default cell and margin of an area (spec 7.4): 50/100 cm cells for linear/surface,
// margins 50 (stern_to), 100 (alongside), 60 (surface).
func defaultsOfArea(a *DynamicArea) {
	if a.CellCm == nil {
		v := int64(50)
		if a.Kind == string(layout.AreaSurface) {
			v = 100
		}
		a.CellCm = &v
	}
	if a.MarginCm == nil {
		v := int64(50)
		switch {
		case a.Kind == string(layout.AreaSurface):
			v = 60
		case a.MooringMode == string(layout.ModeAlongside):
			v = 100
		}
		a.MarginCm = &v
	}
}

// resolve maps a reference token to the final id ("" stays ""). Unknown tokens cannot reach this point: the
// document passed ValidateMap (dangling_reference).
func (p *prepared) resolve(token string) string {
	if token == "" {
		return ""
	}
	return p.ids[token]
}

// finalID returns the id of an element (its id, or the new id of its key).
func (p *prepared) finalID(id, key string) string {
	if id != "" {
		return id
	}
	return p.idMap[key]
}

// rows builds the stored rows and group memberships of a validated document.
func (p *prepared) rows(now time.Time) (*store.MapElements, []store.BerthGroupMember, error) {
	req := p.req
	out := &store.MapElements{}
	var members []store.BerthGroupMember
	member := func(rt, id string, groups []string) {
		for _, g := range slices.Compact(slices.Sorted(slices.Values(groups))) {
			members = append(members, store.BerthGroupMember{ID: store.NewID(), GroupID: g, ResourceType: rt, ResourceID: id, CreatedAt: now})
		}
	}
	for i := range req.Structures {
		s := &req.Structures[i]
		geom := structureGeometry{Points: s.Points, Closed: s.Closed, Rect: s.Rect}
		attrs := structureAttributes{Sides: s.Sides, Floating: s.Floating, DepthProfile: s.DepthProfile, ElevationCm: s.ElevationCm, BollardSpacingCm: s.BollardSpacingCm}
		out.Structures = append(out.Structures, store.HarborStructure{
			ID: p.finalID(s.ID, s.Key), Kind: s.Kind, Code: s.Code, Name: s.Name, Geometry: store.MustJSON(geom), WidthCm: s.WidthCm,
			ParentID: p.resolve(s.ParentID), Attributes: store.MustJSON(attrs), CreatedAt: now, UpdatedAt: now,
		})
	}
	for i := range req.Pedestals {
		pd := &req.Pedestals[i]
		sockets, meters := pd.Sockets, pd.Meters
		if sockets == nil {
			sockets = []Socket{}
		}
		if meters == nil {
			meters = []Meter{}
		}
		out.Pedestals = append(out.Pedestals, store.Pedestal{
			ID: p.finalID(pd.ID, pd.Key), Code: pd.Code, XCm: int64(pd.Position.X), YCm: int64(pd.Position.Y),
			Sockets: store.MustJSON(sockets), Meters: store.MustJSON(meters), CreatedAt: now, UpdatedAt: now,
		})
	}
	for i := range req.Berths {
		b := &req.Berths[i]
		id := p.finalID(b.ID, b.Key)
		out.Berths = append(out.Berths, store.Berth{
			ID: id, StructureID: p.resolve(b.StructureID), Code: b.Code, Side: b.Side, Position: b.Position,
			AnchorXCm: int64(b.Anchor.X), AnchorYCm: int64(b.Anchor.Y), RotationDd: b.RotationDd, WidthCm: b.WidthCm, LengthCm: b.LengthCm,
			MaxLOACm: b.MaxLOACm, MaxBeamCm: b.MaxBeamCm, DepthCm: b.DepthCm, MaxAirDraftCm: b.MaxAirDraftCm,
			MaxDisplacementKg: b.MaxDisplacementKg, MooringType: b.MooringType, SwingRadiusCm: b.SwingRadiusCm,
			OverhangAllowed: b.OverhangAllowed, PedestalID: p.resolve(b.PedestalID), PedestalSocket: b.PedestalSocket, PowerA: b.PowerA,
			PowerPhase: b.PowerPhase, Water: b.Water, WiFi: b.Wifi, PriceClass: b.PriceClass, Attributes: store.Strings(nonNilStrings(b.Attributes)),
			OnlineSellable: b.OnlineSellable, Active: b.Active, CreatedAt: now, UpdatedAt: now,
		})
		member(resourceBerth, id, b.GroupIDs)
	}
	for i := range req.DynamicAreas {
		a := &req.DynamicAreas[i]
		id := p.finalID(a.ID, a.Key)
		la, err := layout.AreaOf(p.layout.Areas[i], p.layout)
		if err != nil {
			return nil, nil, fmt.Errorf("harbor: area %d: %w", i, err)
		}
		row := store.DynamicArea{
			ID: id, Kind: a.Kind, Code: a.Code, StructureID: p.resolve(a.StructureID), Side: a.Side, FromCm: a.FromCm, ToCm: a.ToCm,
			MooringMode: a.MooringMode, CellCm: *a.CellCm, MarginCm: *a.MarginCm, MaxLOACm: a.MaxLOACm, MaxBeamCm: a.MaxBeamCm,
			MaxHeightCm: a.MaxHeightCm, MaxWeightKg: a.MaxWeightKg, Supports: store.Strings(nonNilStrings(a.Supports)),
			DepthProfile: store.MustJSON(nonNilSpans(a.DepthProfile)), BlockedCells: store.MustJSON(nonNilInts(a.BlockedCells)),
			Exits: store.MustJSON(nonNilInts(a.Exits)), GridRotationDd: a.GridRotationDd, CellCount: int64(layout.CellCount(la)),
			PriceClass: a.PriceClass, OnlineSellable: a.OnlineSellable && a.Kind == string(layout.AreaLinear), Active: a.Active,
			CreatedAt: now, UpdatedAt: now,
		}
		if a.Kind == string(layout.AreaSurface) {
			row.Outline = store.MustJSON(a.Outline)
		}
		out.Areas = append(out.Areas, row)
		member(resourceArea, id, a.GroupIDs)
	}
	for i := range req.LandResources {
		l := &req.LandResources[i]
		id := p.finalID(l.ID, l.Key)
		out.LandResources = append(out.LandResources, store.LandResource{
			ID: id, Kind: l.Kind, Code: l.Code, XCm: int64(l.Anchor.X), YCm: int64(l.Anchor.Y), RotationDd: l.RotationDd,
			WidthCm: l.WidthCm, LengthCm: l.LengthCm, PriceClass: l.PriceClass, Attributes: store.Strings(nonNilStrings(l.Attributes)),
			Active: l.Active, CreatedAt: now, UpdatedAt: now,
		})
		member(resourceLand, id, l.GroupIDs)
	}
	for i := range req.Decorations {
		d := &req.Decorations[i]
		g := decorationGeometry{RotationDd: d.RotationDd, SizeCm: d.SizeCm, Points: d.Points}
		if d.Position != nil {
			g.XCm, g.YCm = i64(int64(d.Position.X)), i64(int64(d.Position.Y))
		}
		label := I18nText{}
		if d.Label != nil {
			label = *d.Label
		}
		out.Decorations = append(out.Decorations, store.MapDecoration{
			ID: p.finalID(d.ID, d.Key), Kind: d.Kind, Asset: d.Asset, Variant: d.Variant, Geometry: store.MustJSON(g),
			Label: store.MustJSON(label), DepthCm: d.DepthCm, CreatedAt: now, UpdatedAt: now,
		})
	}
	return out, members, nil
}

func nonNilSpans(s []DepthSpan) []DepthSpan {
	if s == nil {
		return []DepthSpan{}
	}
	return s
}

// docContext loads what prepare needs from the database: saved ids of the map, groups and background files.
func docContext(ctx context.Context, db bun.IDB, scope store.MarinaScope, mapID string) (*store.MapElements, map[string]bool, func(string) (*store.StoredFile, bool), error) {
	var elems *store.MapElements
	if mapID != "" {
		var err error
		if elems, err = store.LoadMapElements(ctx, db, scope, mapID); err != nil {
			return nil, nil, nil, err
		}
	} else {
		elems = &store.MapElements{}
	}
	gs, err := store.ListBerthGroups(ctx, db, scope)
	if err != nil {
		return nil, nil, nil, err
	}
	groups := make(map[string]bool, len(gs))
	for _, g := range gs {
		groups[g.ID] = true
	}
	files := func(id string) (*store.StoredFile, bool) {
		f, err := store.GetStoredFile(ctx, db, scope, id)
		if err != nil || f.Purpose != purposeBackground {
			return nil, false
		}
		return f, true
	}
	return elems, groups, files, nil
}

const purposeBackground = "map_background"
