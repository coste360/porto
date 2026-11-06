package harbor

import (
	"context"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Generator previews (spec 8.3, m1-plan §11 D2/D3). The pure algorithms are layout.Generate*; this is the
// service entry point: it authorises, resolves the structures the parameters refer to (in the given unsaved
// document, or in the saved map), passes the marina's price-class rule and returns unsaved elements. They are
// applied by the next saveMap.

// Numbering is the numbering of generated elements (contract Numbering).
type Numbering struct {
	Scheme string  `json:"scheme"`
	Prefix *string `json:"prefix,omitempty"`
	Start  *int64  `json:"start,omitempty"`
	Pad    *int    `json:"pad,omitempty"`
}

// LaneSpec is a corridor of a surface generator.
type LaneSpec struct {
	From    geo.Point `json:"from"`
	To      geo.Point `json:"to"`
	WidthCm int64     `json:"width_cm"`
}

// GeneratorSpec holds the parameters of every generator type (contract GeneratorParams, by "type").
type GeneratorSpec struct {
	Type      string  `json:"type"`
	KeyPrefix string  `json:"key_prefix"`
	GroupID   *string `json:"group_id,omitempty"`

	Origin           *geo.Point   `json:"origin,omitempty"`
	RotationDd       *int64       `json:"rotation_dd,omitempty"`
	LengthCm         *int64       `json:"length_cm,omitempty"`
	WidthCm          *int64       `json:"width_cm,omitempty"`
	Floating         *bool        `json:"floating,omitempty"`
	Code             *string      `json:"code,omitempty"`
	RootID           *string      `json:"root_id,omitempty"`
	Sides            *[]string    `json:"sides,omitempty"`
	BerthsPerSide    *int         `json:"berths_per_side,omitempty"`
	StartOffsetCm    *int64       `json:"start_offset_cm,omitempty"`
	BerthWidthCm     *int64       `json:"berth_width_cm,omitempty"`
	BerthLengthCm    *int64       `json:"berth_length_cm,omitempty"`
	BerthWidthEndCm  *int64       `json:"berth_width_end_cm,omitempty"`
	BerthLengthEndCm *int64       `json:"berth_length_end_cm,omitempty"`
	FingerEvery      *int         `json:"finger_every,omitempty"`
	FingerWidthCm    *int64       `json:"finger_width_cm,omitempty"`
	FingerLengthCm   *int64       `json:"finger_length_cm,omitempty"`
	HeadBerths       *int         `json:"head_berths,omitempty"`
	MooringType      *string      `json:"mooring_type,omitempty"`
	Numbering        *Numbering   `json:"numbering,omitempty"`
	DepthCm          *int64       `json:"depth_cm,omitempty"`
	QuayID           *string      `json:"quay_id,omitempty"`
	FromCm           *int64       `json:"from_cm,omitempty"`
	ToCm             *int64       `json:"to_cm,omitempty"`
	Side             *string      `json:"side,omitempty"`
	Count            *int         `json:"count,omitempty"`
	StructureID      *string      `json:"structure_id,omitempty"`
	Mode             *string      `json:"mode,omitempty"`
	CellCm           *int64       `json:"cell_cm,omitempty"`
	MarginCm         *int64       `json:"margin_cm,omitempty"`
	MaxLOACm         *int64       `json:"max_loa_cm,omitempty"`
	MaxBeamCm        *int64       `json:"max_beam_cm,omitempty"`
	MaxHeightCm      *int64       `json:"max_height_cm,omitempty"`
	Outline          *[]geo.Point `json:"outline,omitempty"`
	SwingRadiusCm    *int64       `json:"swing_radius_cm,omitempty"`
	Layout           *string      `json:"layout,omitempty"`
	SpacingCm        *int64       `json:"spacing_cm,omitempty"`
	Rows             *int         `json:"rows,omitempty"`
	Cols             *int         `json:"cols,omitempty"`
	ItemWidthCm      *int64       `json:"item_width_cm,omitempty"`
	ItemLengthCm     *int64       `json:"item_length_cm,omitempty"`
	AisleCm          *int64       `json:"aisle_cm,omitempty"`
	AisleEvery       *int         `json:"aisle_every,omitempty"`
	Kind             *string      `json:"kind,omitempty"`
	GridRotationDd   *int64       `json:"grid_rotation_dd,omitempty"`
	Exits            *[]geo.Point `json:"exits,omitempty"`
	Lanes            *[]LaneSpec  `json:"lanes,omitempty"`
}

// GenerateInput is a preview request.
type GenerateInput struct {
	Generator GeneratorSpec
	Document  *SaveRequest // current unsaved document (optional)
}

// GenerateResult is a preview (keys only, nothing saved).
type GenerateResult struct {
	Elements Elements
	Report   *layout.Report // of document + elements, when a document was given
	Counts   Counts
}

func val[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func cm(p *int64) geo.Cm { return geo.Cm(val(p)) }

func numberingOf(n *Numbering) layout.Numbering {
	if n == nil {
		return layout.Numbering{}
	}
	return layout.Numbering{Scheme: n.Scheme, Prefix: val(n.Prefix), Start: val(n.Start), Pad: val(n.Pad)}
}

// Generate runs a generator and returns its preview (harbor.configure).
func (s *Service) Generate(ctx context.Context, p *auth.Principal, marinaID, mapID string, in GenerateInput) (*GenerateResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	var out *GenerateResult
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		var base SaveRequest
		if in.Document != nil {
			base = *in.Document
		} else {
			d, err := loadDocument(ctx, tx, scope, m)
			if err != nil {
				return err
			}
			base = d.Request()
		}
		baseCopy := cloneRequest(base)
		prep, settings, err := s.prepareIn(ctx, tx, scope, mapID, &baseCopy)
		if err != nil {
			return err
		}
		g := &in.Generator
		if g.GroupID != nil {
			if _, err := store.GetBerthGroup(ctx, tx, scope, *g.GroupID); err != nil {
				return fieldError("generator.group_id", "unknown_element")
			}
		}
		gen, err := runGenerator(g, prep.layout.Structures, settings.PriceClassRule)
		if err != nil {
			return err
		}
		res := &GenerateResult{Elements: elementsOf(gen, val(g.GroupID))}
		res.Counts = Counts{
			Structures: len(gen.Structures), Berths: len(gen.Berths), DynamicAreas: len(gen.Areas),
			LandResources: len(gen.LandResources), Pedestals: len(gen.Pedestals), Decorations: len(gen.Decorations),
		}
		if in.Document != nil {
			merged := cloneRequest(*in.Document)
			merged.Structures = append(merged.Structures, res.Elements.Structures...)
			merged.Berths = append(merged.Berths, res.Elements.Berths...)
			merged.DynamicAreas = append(merged.DynamicAreas, res.Elements.DynamicAreas...)
			merged.LandResources = append(merged.LandResources, res.Elements.LandResources...)
			mp, _, err := s.prepareIn(ctx, tx, scope, mapID, &merged)
			if err != nil {
				return err
			}
			rep := layout.ValidateMap(mp.layout, settings.validation())
			res.Report = &rep
		}
		out = res
		return nil
	})
	return out, err
}

// cloneRequest copies the element slices so that prepare's defaults never leak into the caller's document.
func cloneRequest(r SaveRequest) SaveRequest {
	r.Structures = slices.Clone(r.Structures)
	r.Berths = slices.Clone(r.Berths)
	r.DynamicAreas = slices.Clone(r.DynamicAreas)
	for i := range r.DynamicAreas {
		a := &r.DynamicAreas[i]
		if a.CellCm != nil {
			a.CellCm = i64(*a.CellCm)
		}
		if a.MarginCm != nil {
			a.MarginCm = i64(*a.MarginCm)
		}
	}
	r.LandResources = slices.Clone(r.LandResources)
	r.Pedestals = slices.Clone(r.Pedestals)
	r.Decorations = slices.Clone(r.Decorations)
	return r
}

func fieldError(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

// badParams maps a layout parameter error to validation_failed with "generator." field paths.
func badParams(err error) error {
	var ve *layout.ValidationError
	if errors.As(err, &ve) {
		v := &auth.ValidationError{}
		for _, f := range ve.Fields {
			v.Add("generator."+f.Field, f.Code)
		}
		return v
	}
	return err
}

func findStructure(list []layout.Structure, ref *string, field string) (layout.Structure, error) {
	if ref == nil || *ref == "" {
		return layout.Structure{}, fieldError(field, "required")
	}
	for _, s := range list {
		if string(s.Ref) == *ref {
			return s, nil
		}
	}
	return layout.Structure{}, fieldError(field, "unknown_element")
}

func sidesOf(p *[]string) []layout.Side {
	var out []layout.Side
	for _, s := range val(p) {
		out = append(out, layout.Side(s))
	}
	return out
}

func runGenerator(g *GeneratorSpec, structures []layout.Structure, rule layout.PriceClassRule) (layout.Generated, error) {
	var gen layout.Generated
	var err error
	switch g.Type {
	case "pier":
		root := layout.Ref(val(g.RootID))
		if root != "" {
			if _, err := findStructure(structures, g.RootID, "generator.root_id"); err != nil {
				return gen, err
			}
		}
		gen, err = layout.GeneratePier(layout.PierParams{
			KeyPrefix: g.KeyPrefix, Origin: val(g.Origin), Rotation: geo.Angle(val(g.RotationDd)), Length: cm(g.LengthCm), Width: cm(g.WidthCm),
			Floating: val(g.Floating), Code: val(g.Code), Root: root, Sides: sidesOf(g.Sides), BerthsPerSide: val(g.BerthsPerSide),
			StartOffset: cm(g.StartOffsetCm), BerthWidth: cm(g.BerthWidthCm), BerthLength: cm(g.BerthLengthCm),
			BerthWidthEnd: cm(g.BerthWidthEndCm), BerthLengthEnd: cm(g.BerthLengthEndCm), FingerEvery: val(g.FingerEvery),
			FingerWidth: cm(g.FingerWidthCm), FingerLength: cm(g.FingerLengthCm), HeadBerths: val(g.HeadBerths),
			Mooring: layout.MooringType(val(g.MooringType)), Numbering: numberingOf(g.Numbering), Depth: cm(g.DepthCm), PriceRule: rule,
		})
	case "quay_berths":
		quay, ferr := findStructure(structures, g.QuayID, "generator.quay_id")
		if ferr != nil {
			return gen, ferr
		}
		gen, err = layout.GenerateQuayBerths(layout.QuayBerthsParams{
			KeyPrefix: g.KeyPrefix, Quay: quay, From: cm(g.FromCm), To: cm(g.ToCm), Side: layout.Side(val(g.Side)),
			BerthWidth: cm(g.BerthWidthCm), Count: val(g.Count), BerthLength: cm(g.BerthLengthCm),
			Mooring: layout.MooringType(val(g.MooringType)), Numbering: numberingOf(g.Numbering), PriceRule: rule,
		})
	case "linear_area":
		st, ferr := findStructure(structures, g.StructureID, "generator.structure_id")
		if ferr != nil {
			return gen, ferr
		}
		mode := layout.MooringMode(val(g.Mode))
		margin := int64(50)
		if mode == layout.ModeAlongside {
			margin = 100
		}
		if g.MarginCm != nil {
			margin = *g.MarginCm
		}
		gen, err = layout.GenerateLinearArea(layout.LinearAreaParams{
			KeyPrefix: g.KeyPrefix, Code: val(g.Code), Structure: st, Side: layout.Side(val(g.Side)), From: cm(g.FromCm), To: cm(g.ToCm),
			Mode: mode, Cell: cm(g.CellCm), Margin: geo.Cm(margin), MaxLOA: cm(g.MaxLOACm), MaxBeam: cm(g.MaxBeamCm),
		})
	case "buoy_field":
		gen, err = layout.GenerateBuoyField(layout.BuoyFieldParams{
			KeyPrefix: g.KeyPrefix, Outline: geo.Polygon(val(g.Outline)), SwingRadius: cm(g.SwingRadiusCm), Layout: val(g.Layout),
			Spacing: cm(g.SpacingCm), MaxLOA: cm(g.MaxLOACm), Numbering: numberingOf(g.Numbering), PriceRule: rule,
		})
	case "land_grid":
		gen, err = layout.GenerateLandGrid(layout.LandGridParams{
			KeyPrefix: g.KeyPrefix, Origin: val(g.Origin), Rotation: geo.Angle(val(g.RotationDd)), Rows: val(g.Rows), Cols: val(g.Cols),
			ItemWidth: cm(g.ItemWidthCm), ItemLength: cm(g.ItemLengthCm), Aisle: cm(g.AisleCm), AisleEvery: val(g.AisleEvery),
			Kind: layout.LandKind(val(g.Kind)), Numbering: numberingOf(g.Numbering),
		})
	case "surface_area":
		margin := int64(60)
		if g.MarginCm != nil {
			margin = *g.MarginCm
		}
		var lanes []layout.Lane
		for _, l := range val(g.Lanes) {
			lanes = append(lanes, layout.Lane{From: l.From, To: l.To, Width: geo.Cm(l.WidthCm)})
		}
		gen, err = layout.GenerateSurfaceArea(layout.SurfaceParams{
			KeyPrefix: g.KeyPrefix, Code: val(g.Code), Outline: geo.Polygon(val(g.Outline)), Cell: cm(g.CellCm),
			GridRotation: geo.Angle(val(g.GridRotationDd)), Exits: val(g.Exits), Lanes: lanes, Margin: geo.Cm(margin),
			MaxLOA: cm(g.MaxLOACm), MaxHeight: cm(g.MaxHeightCm),
		})
	default:
		return gen, fieldError("generator.type", "invalid")
	}
	if err != nil {
		return gen, badParams(err)
	}
	return gen, nil
}

// elementsOf converts generated elements into document elements with keys.
func elementsOf(g layout.Generated, groupID string) Elements {
	groups := []string{}
	if groupID != "" {
		groups = []string{groupID}
	}
	e := Elements{
		Structures: []Structure{}, Berths: []Berth{}, DynamicAreas: []DynamicArea{}, LandResources: []LandResource{},
		Pedestals: []Pedestal{}, Decorations: []Decoration{},
	}
	for _, s := range g.Structures {
		st := Structure{
			Key: string(s.Ref), Kind: string(s.Kind), Code: s.Code, Points: s.Points, Closed: s.Closed, WidthCm: int64(s.Width),
			ParentID: string(s.Parent), Floating: s.Floating,
		}
		for _, side := range s.Sides {
			st.Sides = append(st.Sides, string(side))
		}
		if s.Rect != nil {
			st.Rect = &Rect{Anchor: s.Rect.Anchor, WidthCm: int64(s.Rect.Width), LengthCm: int64(s.Rect.Length), RotationDd: int64(s.Rect.Rotation)}
		}
		for _, d := range s.Depth {
			st.DepthProfile = append(st.DepthProfile, DepthSpan{FromCm: int64(d.From), ToCm: int64(d.To), DepthCm: int64(d.Depth)})
		}
		e.Structures = append(e.Structures, st)
	}
	for _, b := range g.Berths {
		e.Berths = append(e.Berths, Berth{
			Key: string(b.Ref), Code: b.Code, StructureID: string(b.Structure), Side: string(b.Side), Position: b.Position,
			Anchor: b.Rect.Anchor, RotationDd: int64(b.Rect.Rotation), WidthCm: int64(b.Rect.Width), LengthCm: int64(b.Rect.Length),
			MaxLOACm: int64(b.MaxLOA), MaxBeamCm: int64(b.MaxBeam), DepthCm: int64(b.Depth), MooringType: string(b.Mooring),
			SwingRadiusCm: int64(b.SwingRadius), PriceClass: b.PriceClass, Attributes: []string{}, Active: true, GroupIDs: slices.Clone(groups),
		})
	}
	for _, a := range g.Areas {
		da := DynamicArea{
			Key: string(a.Ref), Kind: string(a.Kind), Code: a.Code, StructureID: string(a.Structure), Side: string(a.Side),
			Outline: a.Outline, MooringMode: string(a.Mode), CellCm: i64(int64(a.Cell)), MarginCm: i64(int64(a.Margin)),
			MaxLOACm: int64(a.MaxLOA), MaxBeamCm: int64(a.MaxBeam), MaxHeightCm: int64(a.MaxHeight), BlockedCells: nonNilInts(a.Blocked),
			Exits: nonNilInts(a.Exits), GridRotationDd: int64(a.GridRotation), Active: true, GroupIDs: slices.Clone(groups),
		}
		if a.Kind == layout.AreaLinear {
			da.FromCm, da.ToCm = i64(int64(a.From)), i64(int64(a.To))
		}
		e.DynamicAreas = append(e.DynamicAreas, da)
	}
	for _, l := range g.LandResources {
		e.LandResources = append(e.LandResources, LandResource{
			Key: string(l.Ref), Kind: string(l.Kind), Code: l.Code, Anchor: l.Rect.Anchor, RotationDd: int64(l.Rect.Rotation),
			WidthCm: int64(l.Rect.Width), LengthCm: int64(l.Rect.Length), Attributes: []string{}, Active: true, GroupIDs: slices.Clone(groups),
		})
	}
	return e
}
