package layout

import "github.com/coste360/porto/internal/geo"

// Element kinds as they appear in Issue.Kind and in the API (MapIssue.element_type).
const (
	KindMap          = "map"
	KindStructure    = "structure"
	KindBerth        = "berth"
	KindDynamicArea  = "dynamic_area"
	KindLandResource = "land_resource"
	KindPedestal     = "pedestal"
	KindDecoration   = "decoration"
)

// StructureKind is the kind of a harbour structure (spec 7.2).
type StructureKind string

// Structure kinds.
const (
	StructureQuay       StructureKind = "quay"
	StructurePier       StructureKind = "pier"
	StructureFinger     StructureKind = "finger"
	StructureBreakwater StructureKind = "breakwater"
	StructureChannel    StructureKind = "channel"
	StructureWalkway    StructureKind = "walkway"
)

// StructureKinds lists the valid structure kinds.
var StructureKinds = []StructureKind{StructureQuay, StructurePier, StructureFinger, StructureBreakwater, StructureChannel, StructureWalkway}

// Side is the side of a structure, relative to its direction (first point towards the last).
type Side string

// Sides.
const (
	SideLeft  Side = "left"
	SideRight Side = "right"
	SideHead  Side = "head"
	SideNone  Side = "none"
)

// MooringType is how a vessel is moored at a berth (spec 7.3).
type MooringType string

// Mooring types.
const (
	MooringLine      MooringType = "mooring_line"
	MooringFinger    MooringType = "finger"
	MooringAlongside MooringType = "alongside"
	MooringPiles     MooringType = "piles"
	MooringBuoy      MooringType = "buoy"
)

// MooringTypes lists the valid mooring types.
var MooringTypes = []MooringType{MooringLine, MooringFinger, MooringAlongside, MooringPiles, MooringBuoy}

// AreaKind is the kind of a dynamic area (spec 7.4).
type AreaKind string

// Area kinds.
const (
	AreaLinear  AreaKind = "linear"
	AreaSurface AreaKind = "surface"
)

// MooringMode is how vessels lie in a linear area.
type MooringMode string

// Mooring modes.
const (
	ModeSternTo   MooringMode = "stern_to"
	ModeAlongside MooringMode = "alongside"
)

// LandKind is the kind of a land resource (spec 7.6).
type LandKind string

// Land resource kinds.
const (
	LandParking     LandKind = "parking"
	LandBox         LandKind = "box"
	LandStoreroom   LandKind = "storeroom"
	LandLocker      LandKind = "locker"
	LandCradle      LandKind = "cradle"
	LandTrailerSpot LandKind = "trailer_spot"
	LandCustom      LandKind = "custom"
)

// LandKinds lists the valid land resource kinds.
var LandKinds = []LandKind{LandParking, LandBox, LandStoreroom, LandLocker, LandCradle, LandTrailerSpot, LandCustom}

// DecorationKind is the kind of a decoration (spec 7.7).
type DecorationKind string

// Decoration kinds.
const (
	DecorationSymbol     DecorationKind = "symbol"
	DecorationText       DecorationKind = "text"
	DecorationDepthLabel DecorationKind = "depth_label"
	DecorationNorthArrow DecorationKind = "north_arrow"
	DecorationScaleBar   DecorationKind = "scale_bar"
)

// DecorationKinds lists the valid decoration kinds.
var DecorationKinds = []DecorationKind{DecorationSymbol, DecorationText, DecorationDepthLabel, DecorationNorthArrow, DecorationScaleBar}

// DepthSpan is a depth along the structure axis, between From and To (cm from the first point).
type DepthSpan struct{ From, To, Depth geo.Cm }

// Structure is a non-occupiable harbour structure.
type Structure struct {
	Ref      Ref
	Kind     StructureKind
	Code     string
	Points   []geo.Point // polyline, or polygon when Closed
	Closed   bool
	Width    geo.Cm    // pier, walkway, breakwater line; 0 otherwise
	Rect     *geo.Rect // finger only
	Parent   Ref       // finger -> pier; pier -> quay (optional)
	Sides    []Side    // pier: usable sides; quay: mooring side
	Floating bool
	Depth    []DepthSpan
}

// Berth is a berth: a rotated rectangle, or a circle for buoys (Rect.Anchor is the centre).
type Berth struct {
	Ref             Ref
	Code            string
	Structure       Ref
	Side            Side
	Position        int64
	Rect            geo.Rect
	Mooring         MooringType
	SwingRadius     geo.Cm
	MaxLOA, MaxBeam geo.Cm
	Depth           geo.Cm
	MaxAirDraft     geo.Cm
	OverhangAllowed bool
	Pedestal        Ref
	PedestalSocket  string
	PriceClass      string
	Active          bool
}

// IsBuoy reports whether the berth is a buoy (circle geometry).
func (b *Berth) IsBuoy() bool { return b.Mooring == MooringBuoy }

// Circle returns the swing circle of a buoy.
func (b *Berth) Circle() geo.Circle { return geo.Circle{Center: b.Rect.Anchor, Radius: b.SwingRadius} }

// DynamicArea is an area without predefined berths (spec 7.4).
type DynamicArea struct {
	Ref          Ref
	Code         string
	Kind         AreaKind
	Structure    Ref
	Side         Side
	From, To     geo.Cm
	Mode         MooringMode
	Outline      geo.Polygon
	GridRotation geo.Angle
	Cell, Margin geo.Cm
	MaxLOA       geo.Cm
	MaxBeam      geo.Cm
	MaxHeight    geo.Cm
	Blocked      []int
	Exits        []int
}

// LandResource is a discrete land resource; Rect.Anchor is the midpoint of the access side.
type LandResource struct {
	Ref  Ref
	Code string
	Kind LandKind
	Rect geo.Rect
}

// Socket is one socket of a pedestal.
type Socket struct {
	Code  string
	Kind  string // power | water
	Amps  int64
	Phase string
}

// Pedestal is a power and water column.
type Pedestal struct {
	Ref     Ref
	Code    string
	At      geo.Point
	Sockets []Socket
}

// Decoration is a kit symbol, a text, a depth label, the north arrow or the scale bar.
type Decoration struct {
	Ref      Ref
	Kind     DecorationKind
	Asset    string
	At       geo.Point
	Rotation geo.Angle
	Size     geo.Cm
	Points   []geo.Point
}

// Map is a complete map document: the whole state of one map.
type Map struct {
	Width, Height, Grid geo.Cm
	Water, Land         []geo.Polygon
	Concession          geo.Polygon // optional
	Structures          []Structure
	Berths              []Berth
	Areas               []DynamicArea
	LandResources       []LandResource
	Pedestals           []Pedestal
	Decorations         []Decoration
}

// ElementCount returns the number of elements of the document.
func (m *Map) ElementCount() int {
	return len(m.Structures) + len(m.Berths) + len(m.Areas) + len(m.LandResources) + len(m.Pedestals) + len(m.Decorations)
}

// Settings are the validation settings of a marina.
type Settings struct {
	LOATolerance     geo.Cm // harbor_settings.loa_tolerance_cm (default 50)
	OverlapTolerance geo.Cm // fixed 1 cm (spec 8.4)
	MaxElements      int    // fixed 20 000 per map
}

// Limits (m1-plan §11 D25).
const (
	MaxElementsPerMap        = 20_000
	MaxGenerated             = 2_000
	MaxMapSide        geo.Cm = 500_000
	MinMapSide        geo.Cm = 100
	DefaultLOATol            = 50
	MaxSurfaceCells          = 250_000
	AttachDistance           = 50 // berth_unattached threshold (cm)
	FenderMargin             = 25 // default max_beam = width - 2 × FenderMargin
)

// MinRectSide is the smallest width and length of a berth or land resource rectangle: below it the tolerant
// overlap test of spec 8.4 is undefined, so a thinner rectangle could never be checked (INV-04).
const MinRectSide geo.Cm = 2

// DefaultSettings returns the settings with the default LOA tolerance.
func DefaultSettings() Settings {
	return Settings{LOATolerance: DefaultLOATol, OverlapTolerance: 1, MaxElements: MaxElementsPerMap}
}
