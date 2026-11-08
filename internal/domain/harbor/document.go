package harbor

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// The map document (m1-plan §4.3). Field names and JSON tags are those of the contract (MapDocument,
// MapSaveRequest and the element schemas), so internal/http/view converts with a strict JSON round trip.
// Elements carry the id of a saved element or the key of a new one; references take either.

// I18nText is a text in Italian (required) and English.
type I18nText struct {
	It string  `json:"it"`
	En *string `json:"en,omitempty"`
}

// Rect is a rotated rectangle (finger structures).
type Rect struct {
	Anchor     geo.Point `json:"anchor"`
	WidthCm    int64     `json:"width_cm"`
	LengthCm   int64     `json:"length_cm"`
	RotationDd int64     `json:"rotation_dd"`
}

// DepthSpan is a depth along a structure axis.
type DepthSpan struct {
	FromCm  int64 `json:"from_cm"`
	ToCm    int64 `json:"to_cm"`
	DepthCm int64 `json:"depth_cm"`
}

// Structure is a harbour structure.
type Structure struct {
	ID               string      `json:"id,omitempty"`
	Key              string      `json:"key,omitempty"`
	Kind             string      `json:"kind"`
	Code             string      `json:"code"`
	Name             string      `json:"name,omitempty"`
	Points           []geo.Point `json:"points,omitempty"`
	Closed           bool        `json:"closed,omitempty"`
	Rect             *Rect       `json:"rect,omitempty"`
	WidthCm          int64       `json:"width_cm,omitempty"`
	ParentID         string      `json:"parent_id,omitempty"`
	Sides            []string    `json:"sides,omitempty"`
	Floating         bool        `json:"floating,omitempty"`
	DepthProfile     []DepthSpan `json:"depth_profile,omitempty"`
	ElevationCm      *int64      `json:"elevation_cm,omitempty"`
	BollardSpacingCm int64       `json:"bollard_spacing_cm,omitempty"`
}

// Berth is a berth (rotated rectangle, or a buoy circle centred on the anchor).
type Berth struct {
	ID                string    `json:"id,omitempty"`
	Key               string    `json:"key,omitempty"`
	Code              string    `json:"code"`
	StructureID       string    `json:"structure_id,omitempty"`
	Side              string    `json:"side"`
	Position          int64     `json:"position"`
	Anchor            geo.Point `json:"anchor"`
	RotationDd        int64     `json:"rotation_dd"`
	WidthCm           int64     `json:"width_cm"`
	LengthCm          int64     `json:"length_cm"`
	MaxLOACm          int64     `json:"max_loa_cm,omitempty"`
	MaxBeamCm         int64     `json:"max_beam_cm,omitempty"`
	DepthCm           int64     `json:"depth_cm,omitempty"`
	MaxAirDraftCm     int64     `json:"max_air_draft_cm,omitempty"`
	MaxDisplacementKg int64     `json:"max_displacement_kg,omitempty"`
	MooringType       string    `json:"mooring_type"`
	SwingRadiusCm     int64     `json:"swing_radius_cm,omitempty"`
	OverhangAllowed   bool      `json:"overhang_allowed"`
	PedestalID        string    `json:"pedestal_id,omitempty"`
	PedestalSocket    string    `json:"pedestal_socket,omitempty"`
	PowerA            int64     `json:"power_a,omitempty"`
	PowerPhase        string    `json:"power_phase,omitempty"`
	Water             bool      `json:"water"`
	Wifi              bool      `json:"wifi"`
	PriceClass        string    `json:"price_class,omitempty"`
	Attributes        []string  `json:"attributes"`
	OnlineSellable    bool      `json:"online_sellable"`
	Active            bool      `json:"active"`
	GroupIDs          []string  `json:"group_ids"`
}

// DynamicArea is a linear or surface dynamic area.
type DynamicArea struct {
	ID             string      `json:"id,omitempty"`
	Key            string      `json:"key,omitempty"`
	Kind           string      `json:"kind"`
	Code           string      `json:"code"`
	StructureID    string      `json:"structure_id,omitempty"`
	Side           string      `json:"side,omitempty"`
	FromCm         *int64      `json:"from_cm,omitempty"`
	ToCm           *int64      `json:"to_cm,omitempty"`
	Outline        []geo.Point `json:"outline,omitempty"`
	MooringMode    string      `json:"mooring_mode,omitempty"`
	CellCm         *int64      `json:"cell_cm,omitempty"`
	MarginCm       *int64      `json:"margin_cm,omitempty"`
	MaxLOACm       int64       `json:"max_loa_cm,omitempty"`
	MaxBeamCm      int64       `json:"max_beam_cm,omitempty"`
	MaxHeightCm    int64       `json:"max_height_cm,omitempty"`
	MaxWeightKg    int64       `json:"max_weight_kg,omitempty"`
	Supports       []string    `json:"supports,omitempty"`
	DepthProfile   []DepthSpan `json:"depth_profile,omitempty"`
	BlockedCells   []int       `json:"blocked_cells"`
	Exits          []int       `json:"exits"`
	GridRotationDd int64       `json:"grid_rotation_dd"`
	CellCount      *int64      `json:"cell_count,omitempty"`
	PriceClass     string      `json:"price_class,omitempty"`
	// OnlineSellable offers a linear area on the public booking page (M8, m8-plan §3.2, §12.3).
	OnlineSellable bool     `json:"online_sellable,omitempty"`
	Active         bool     `json:"active"`
	GroupIDs       []string `json:"group_ids"`
}

// LandResource is a land resource.
type LandResource struct {
	ID         string    `json:"id,omitempty"`
	Key        string    `json:"key,omitempty"`
	Kind       string    `json:"kind"`
	Code       string    `json:"code"`
	Anchor     geo.Point `json:"anchor"`
	RotationDd int64     `json:"rotation_dd"`
	WidthCm    int64     `json:"width_cm"`
	LengthCm   int64     `json:"length_cm"`
	PriceClass string    `json:"price_class,omitempty"`
	Attributes []string  `json:"attributes"`
	Active     bool      `json:"active"`
	GroupIDs   []string  `json:"group_ids"`
}

// Socket is a pedestal socket.
type Socket struct {
	Code  string `json:"code"`
	Kind  string `json:"kind"`
	Amps  int64  `json:"amps,omitempty"`
	Phase string `json:"phase,omitempty"`
}

// Meter is a pedestal meter.
type Meter struct {
	Kind   string `json:"kind"`
	Serial string `json:"serial"`
}

// Pedestal is a power and water column.
type Pedestal struct {
	ID       string    `json:"id,omitempty"`
	Key      string    `json:"key,omitempty"`
	Code     string    `json:"code"`
	Position geo.Point `json:"position"`
	Sockets  []Socket  `json:"sockets"`
	Meters   []Meter   `json:"meters"`
}

// Decoration is a decoration.
type Decoration struct {
	ID         string      `json:"id,omitempty"`
	Key        string      `json:"key,omitempty"`
	Kind       string      `json:"kind"`
	Asset      string      `json:"asset,omitempty"`
	Variant    string      `json:"variant,omitempty"`
	Position   *geo.Point  `json:"position,omitempty"`
	RotationDd *int64      `json:"rotation_dd,omitempty"`
	SizeCm     int64       `json:"size_cm,omitempty"`
	Points     []geo.Point `json:"points,omitempty"`
	Label      *I18nText   `json:"label,omitempty"`
	DepthCm    *int64      `json:"depth_cm,omitempty"`
}

// Background is the calibration of the background image.
type Background struct {
	FileID        string    `json:"file_id"`
	ContentType   string    `json:"content_type"`
	WidthPx       int64     `json:"width_px"`
	HeightPx      int64     `json:"height_px"`
	Origin        geo.Point `json:"origin"`
	ScaleMcmPerPx int64     `json:"scale_mcm_per_px"`
	RotationDd    int64     `json:"rotation_dd"`
	OpacityPct    int       `json:"opacity_pct"`
	Visible       bool      `json:"visible"`
}

// ControlPoint ties a map point to WGS84 × 10⁷.
type ControlPoint struct {
	X     int64 `json:"x"`
	Y     int64 `json:"y"`
	LatE7 int64 `json:"lat_e7"`
	LonE7 int64 `json:"lon_e7"`
}

// Georef is the georeference of a map.
type Georef struct {
	Points []ControlPoint `json:"points"`
}

// Meta is the metadata of a map.
type Meta struct {
	Kind              string        `json:"kind"`
	Name              I18nText      `json:"name"`
	Icon              string        `json:"icon"`
	WidthCm           int64         `json:"width_cm"`
	HeightCm          int64         `json:"height_cm"`
	GridCm            int64         `json:"grid_cm"`
	SortOrder         int           `json:"sort_order"`
	Background        *Background   `json:"background"`
	Georef            *Georef       `json:"georef"`
	ConcessionOutline []geo.Point   `json:"concession_outline"`
	WaterPolygons     [][]geo.Point `json:"water_polygons"`
	LandPolygons      [][]geo.Point `json:"land_polygons"`
}

// Elements are the elements of a map.
type Elements struct {
	Structures    []Structure    `json:"structures"`
	Berths        []Berth        `json:"berths"`
	DynamicAreas  []DynamicArea  `json:"dynamic_areas"`
	LandResources []LandResource `json:"land_resources"`
	Pedestals     []Pedestal     `json:"pedestals"`
	Decorations   []Decoration   `json:"decorations"`
}

// Count returns the number of elements.
func (e *Elements) Count() int {
	return len(e.Structures) + len(e.Berths) + len(e.DynamicAreas) + len(e.LandResources) + len(e.Pedestals) + len(e.Decorations)
}

// SaveRequest is the complete document of a map, as saved or validated (MapSaveRequest).
type SaveRequest struct {
	Meta
	Elements
}

// Document is a saved map document (MapDocument).
type Document struct {
	ID        string    `json:"id"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Meta
	Elements
}

// Request returns the document as a save request (ids kept).
func (d *Document) Request() SaveRequest {
	return cloneRequest(SaveRequest{Meta: d.Meta, Elements: d.Elements})
}

// SaveResult is the result of a save (MapSaveResult).
type SaveResult struct {
	Document Document          `json:"document"`
	IDMap    map[string]string `json:"id_map"`
	Warnings []layout.Issue    `json:"-"`
}

// Counts are the element counts of a map (MapCounts).
type Counts struct {
	Structures    int `json:"structures"`
	Berths        int `json:"berths"`
	DynamicAreas  int `json:"dynamic_areas"`
	LandResources int `json:"land_resources"`
	Pedestals     int `json:"pedestals"`
	Decorations   int `json:"decorations"`
}

// Summary is a map in the list (MapSummary).
type Summary struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Name          I18nText  `json:"name"`
	Icon          string    `json:"icon"`
	WidthCm       int64     `json:"width_cm"`
	HeightCm      int64     `json:"height_cm"`
	SortOrder     int       `json:"sort_order"`
	Version       int       `json:"version"`
	Counts        Counts    `json:"counts"`
	Georeferenced bool      `json:"georeferenced"`
	HasBackground bool      `json:"has_background"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ---- stored rows → document ----

type structureGeometry struct {
	Points []geo.Point `json:"points,omitempty"`
	Closed bool        `json:"closed,omitempty"`
	Rect   *Rect       `json:"rect,omitempty"`
}

type structureAttributes struct {
	Sides            []string    `json:"sides,omitempty"`
	Floating         bool        `json:"floating,omitempty"`
	DepthProfile     []DepthSpan `json:"depth_profile,omitempty"`
	ElevationCm      *int64      `json:"elevation_cm,omitempty"`
	BollardSpacingCm int64       `json:"bollard_spacing_cm,omitempty"`
}

type decorationGeometry struct {
	XCm        *int64      `json:"x_cm,omitempty"`
	YCm        *int64      `json:"y_cm,omitempty"`
	RotationDd *int64      `json:"rotation_dd,omitempty"`
	SizeCm     int64       `json:"size_cm,omitempty"`
	Points     []geo.Point `json:"points,omitempty"`
}

func unmarshal[T any](j store.JSON) T {
	var v T
	if len(j) > 0 {
		_ = json.Unmarshal(j, &v)
	}
	return v
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilInts(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}

func i64(v int64) *int64 { return &v }

// documentOf builds the document of a map from its rows and its group memberships.
func documentOf(m *store.Map, e *store.MapElements, members []store.BerthGroupMember) Document {
	groups := map[string][]string{}
	for _, mb := range members {
		groups[mb.ResourceID] = append(groups[mb.ResourceID], mb.GroupID)
	}
	for k := range groups {
		slices.Sort(groups[k])
	}
	gid := func(id string) []string { return nonNilStrings(groups[id]) }
	d := Document{ID: m.ID, Version: m.Version, UpdatedAt: m.UpdatedAt, Meta: metaOf(m), Elements: Elements{
		Structures: make([]Structure, 0, len(e.Structures)), Berths: make([]Berth, 0, len(e.Berths)),
		DynamicAreas: make([]DynamicArea, 0, len(e.Areas)), LandResources: make([]LandResource, 0, len(e.LandResources)),
		Pedestals: make([]Pedestal, 0, len(e.Pedestals)), Decorations: make([]Decoration, 0, len(e.Decorations)),
	}}
	for i := range e.Structures {
		r := &e.Structures[i]
		g := unmarshal[structureGeometry](r.Geometry)
		a := unmarshal[structureAttributes](r.Attributes)
		d.Structures = append(d.Structures, Structure{
			ID: r.ID, Kind: r.Kind, Code: r.Code, Name: r.Name, Points: g.Points, Closed: g.Closed, Rect: g.Rect, WidthCm: r.WidthCm,
			ParentID: r.ParentID, Sides: a.Sides, Floating: a.Floating, DepthProfile: a.DepthProfile, ElevationCm: a.ElevationCm,
			BollardSpacingCm: a.BollardSpacingCm,
		})
	}
	for i := range e.Berths {
		r := &e.Berths[i]
		d.Berths = append(d.Berths, Berth{
			ID: r.ID, Code: r.Code, StructureID: r.StructureID, Side: r.Side, Position: r.Position,
			Anchor: geo.Point{X: geo.Cm(r.AnchorXCm), Y: geo.Cm(r.AnchorYCm)}, RotationDd: r.RotationDd, WidthCm: r.WidthCm,
			LengthCm: r.LengthCm, MaxLOACm: r.MaxLOACm, MaxBeamCm: r.MaxBeamCm, DepthCm: r.DepthCm, MaxAirDraftCm: r.MaxAirDraftCm,
			MaxDisplacementKg: r.MaxDisplacementKg, MooringType: r.MooringType, SwingRadiusCm: r.SwingRadiusCm,
			OverhangAllowed: r.OverhangAllowed, PedestalID: r.PedestalID, PedestalSocket: r.PedestalSocket, PowerA: r.PowerA,
			PowerPhase: r.PowerPhase, Water: r.Water, Wifi: r.WiFi, PriceClass: r.PriceClass, Attributes: nonNilStrings(r.Attributes),
			OnlineSellable: r.OnlineSellable, Active: r.Active, GroupIDs: gid(r.ID),
		})
	}
	for i := range e.Areas {
		r := &e.Areas[i]
		a := DynamicArea{
			ID: r.ID, Kind: r.Kind, Code: r.Code, StructureID: r.StructureID, Side: r.Side, FromCm: r.FromCm, ToCm: r.ToCm,
			Outline: unmarshal[[]geo.Point](r.Outline), MooringMode: r.MooringMode, CellCm: i64(r.CellCm), MarginCm: i64(r.MarginCm),
			MaxLOACm: r.MaxLOACm, MaxBeamCm: r.MaxBeamCm, MaxHeightCm: r.MaxHeightCm, MaxWeightKg: r.MaxWeightKg,
			Supports: []string(r.Supports), DepthProfile: unmarshal[[]DepthSpan](r.DepthProfile),
			BlockedCells: nonNilInts(unmarshal[[]int](r.BlockedCells)), Exits: nonNilInts(unmarshal[[]int](r.Exits)),
			GridRotationDd: r.GridRotationDd, CellCount: i64(r.CellCount), PriceClass: r.PriceClass, OnlineSellable: r.OnlineSellable,
			Active: r.Active, GroupIDs: gid(r.ID),
		}
		if len(a.Supports) == 0 {
			a.Supports = nil
		}
		d.DynamicAreas = append(d.DynamicAreas, a)
	}
	for i := range e.LandResources {
		r := &e.LandResources[i]
		d.LandResources = append(d.LandResources, LandResource{
			ID: r.ID, Kind: r.Kind, Code: r.Code, Anchor: geo.Point{X: geo.Cm(r.XCm), Y: geo.Cm(r.YCm)}, RotationDd: r.RotationDd,
			WidthCm: r.WidthCm, LengthCm: r.LengthCm, PriceClass: r.PriceClass, Attributes: nonNilStrings(r.Attributes), Active: r.Active,
			GroupIDs: gid(r.ID),
		})
	}
	for i := range e.Pedestals {
		r := &e.Pedestals[i]
		p := Pedestal{
			ID: r.ID, Code: r.Code, Position: geo.Point{X: geo.Cm(r.XCm), Y: geo.Cm(r.YCm)},
			Sockets: unmarshal[[]Socket](r.Sockets), Meters: unmarshal[[]Meter](r.Meters),
		}
		if p.Sockets == nil {
			p.Sockets = []Socket{}
		}
		if p.Meters == nil {
			p.Meters = []Meter{}
		}
		d.Pedestals = append(d.Pedestals, p)
	}
	for i := range e.Decorations {
		r := &e.Decorations[i]
		g := unmarshal[decorationGeometry](r.Geometry)
		dec := Decoration{
			ID: r.ID, Kind: r.Kind, Asset: r.Asset, Variant: r.Variant, RotationDd: g.RotationDd, SizeCm: g.SizeCm,
			Points: g.Points, DepthCm: r.DepthCm,
		}
		if g.XCm != nil && g.YCm != nil {
			dec.Position = &geo.Point{X: geo.Cm(*g.XCm), Y: geo.Cm(*g.YCm)}
		}
		if l := unmarshal[I18nText](r.Label); l.It != "" || l.En != nil {
			dec.Label = &l
		}
		d.Decorations = append(d.Decorations, dec)
	}
	return d
}

func metaOf(m *store.Map) Meta {
	meta := Meta{
		Kind: m.Kind, Name: unmarshal[I18nText](m.Name), Icon: m.Icon, WidthCm: m.WidthCm, HeightCm: m.HeightCm, GridCm: m.GridCm,
		SortOrder: m.SortOrder, WaterPolygons: unmarshal[[][]geo.Point](m.WaterPolygons), LandPolygons: unmarshal[[][]geo.Point](m.LandPolygons),
	}
	if meta.WaterPolygons == nil {
		meta.WaterPolygons = [][]geo.Point{}
	}
	if meta.LandPolygons == nil {
		meta.LandPolygons = [][]geo.Point{}
	}
	if len(m.Background) > 0 && string(m.Background) != "null" && m.BackgroundFileID != "" {
		b := unmarshal[Background](m.Background)
		b.FileID = m.BackgroundFileID
		meta.Background = &b
	}
	if len(m.Georef) > 0 && string(m.Georef) != "null" {
		g := unmarshal[Georef](m.Georef)
		meta.Georef = &g
	}
	if len(m.ConcessionOutline) > 0 && string(m.ConcessionOutline) != "null" {
		meta.ConcessionOutline = unmarshal[[]geo.Point](m.ConcessionOutline)
	}
	return meta
}

func summaryOf(m *store.Map, c store.MapCounts) Summary {
	meta := metaOf(m)
	return Summary{
		ID: m.ID, Kind: m.Kind, Name: meta.Name, Icon: m.Icon, WidthCm: m.WidthCm, HeightCm: m.HeightCm, SortOrder: m.SortOrder,
		Version: m.Version, UpdatedAt: m.UpdatedAt, Georeferenced: meta.Georef != nil, HasBackground: meta.Background != nil,
		Counts: Counts{
			Structures: c.Structures, Berths: c.Berths, DynamicAreas: c.DynamicAreas, LandResources: c.LandResources,
			Pedestals: c.Pedestals, Decorations: c.Decorations,
		},
	}
}
