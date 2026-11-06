package store

import (
	"time"

	"github.com/uptrace/bun"
)

// Row types of the M1 harbour tables (internal/migrations/*/00002_harbor.sql). Conventions as in models.go:
// nullable text and uuid columns are strings with nullzero, nullable positive integers are int64 with nullzero
// (0 = NULL), nullable integers where 0 is a valid value are *int64, JSON columns are JSON (nil = NULL) or
// Strings. Lengths are integer centimetres, angles tenths of a degree.

// StoredFile is a row of stored_files: an encrypted blob under PORTO_DATA_DIR/files (internal/filestore).
type StoredFile struct {
	bun.BaseModel `bun:"table:stored_files"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Purpose         string    `bun:"purpose"`         // map_background, document
	MapID           string    `bun:"map_id,nullzero"` // map_background: the map it was uploaded for
	ContentType     string    `bun:"content_type"`
	SizeBytes       int64     `bun:"size_bytes"`
	SHA256          string    `bun:"sha256"`
	StorageKey      string    `bun:"storage_key"` // "<marina_id>/<id>.bin"
	WidthPx         int64     `bun:"width_px,nullzero"`
	HeightPx        int64     `bun:"height_px,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	DeletedAt       time.Time `bun:"deleted_at,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// HarborSettings is a row of harbor_settings (one per marina, created lazily with defaults).
type HarborSettings struct {
	bun.BaseModel `bun:"table:harbor_settings"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	PriceClassRule JSON      `bun:"price_class_rule"` // layout.PriceClassRule
	LOAToleranceCm int64     `bun:"loa_tolerance_cm"`
	DefaultGridCm  int64     `bun:"default_grid_cm"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// Map is a row of maps.
type Map struct {
	bun.BaseModel `bun:"table:maps"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	Kind              string    `bun:"kind"` // water, yard, shed, parking, boxes, other
	Name              JSON      `bun:"name"` // I18nText
	Icon              string    `bun:"icon"`
	WidthCm           int64     `bun:"width_cm"`
	HeightCm          int64     `bun:"height_cm"`
	GridCm            int64     `bun:"grid_cm"`
	BackgroundFileID  string    `bun:"background_file_id,nullzero"`
	Background        JSON      `bun:"background"`         // MapBackground calibration, nil = none
	Georef            JSON      `bun:"georef"`             // three control points, nil = none
	ConcessionOutline JSON      `bun:"concession_outline"` // polygon, nil = none
	WaterPolygons     JSON      `bun:"water_polygons"`
	LandPolygons      JSON      `bun:"land_polygons"`
	SortOrder         int       `bun:"sort_order"`
	Version           int       `bun:"version"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// HarborStructure is a row of harbor_structures (quay, pier, finger, breakwater, channel, walkway).
type HarborStructure struct {
	bun.BaseModel `bun:"table:harbor_structures"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	MapID      string    `bun:"map_id"`
	Kind       string    `bun:"kind"`
	Code       string    `bun:"code"`
	Name       string    `bun:"name"`
	Geometry   JSON      `bun:"geometry"`
	WidthCm    int64     `bun:"width_cm,nullzero"`
	ParentID   string    `bun:"parent_id,nullzero"`
	Attributes JSON      `bun:"attributes"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// Pedestal is a row of pedestals (power and water columns).
type Pedestal struct {
	bun.BaseModel `bun:"table:pedestals"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	MapID     string    `bun:"map_id"`
	Code      string    `bun:"code"`
	XCm       int64     `bun:"x_cm"`
	YCm       int64     `bun:"y_cm"`
	Sockets   JSON      `bun:"sockets"`
	Meters    JSON      `bun:"meters"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// Berth is a row of berths.
type Berth struct {
	bun.BaseModel `bun:"table:berths"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	MapID             string    `bun:"map_id"`
	StructureID       string    `bun:"structure_id,nullzero"`
	Code              string    `bun:"code"`
	Side              string    `bun:"side"`
	Position          int64     `bun:"position"`
	AnchorXCm         int64     `bun:"anchor_x_cm"`
	AnchorYCm         int64     `bun:"anchor_y_cm"`
	RotationDd        int64     `bun:"rotation_dd"`
	WidthCm           int64     `bun:"width_cm"`
	LengthCm          int64     `bun:"length_cm"`
	MaxLOACm          int64     `bun:"max_loa_cm"`
	MaxBeamCm         int64     `bun:"max_beam_cm"`
	DepthCm           int64     `bun:"depth_cm,nullzero"`
	MaxAirDraftCm     int64     `bun:"max_air_draft_cm,nullzero"`
	MaxDisplacementKg int64     `bun:"max_displacement_kg,nullzero"`
	MooringType       string    `bun:"mooring_type"`
	SwingRadiusCm     int64     `bun:"swing_radius_cm,nullzero"`
	OverhangAllowed   bool      `bun:"overhang_allowed"`
	PedestalID        string    `bun:"pedestal_id,nullzero"`
	PedestalSocket    string    `bun:"pedestal_socket,nullzero"`
	PowerA            int64     `bun:"power_a,nullzero"`
	PowerPhase        string    `bun:"power_phase,nullzero"`
	Water             bool      `bun:"water"`
	WiFi              bool      `bun:"wifi"`
	PriceClass        string    `bun:"price_class,nullzero"`
	Attributes        Strings   `bun:"attributes"`
	OnlineSellable    bool      `bun:"online_sellable"`
	Active            bool      `bun:"active"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// DynamicArea is a row of dynamic_areas (linear along a structure side, or surface with a cell grid).
type DynamicArea struct {
	bun.BaseModel `bun:"table:dynamic_areas"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	MapID          string    `bun:"map_id"`
	Kind           string    `bun:"kind"` // linear, surface
	Code           string    `bun:"code"`
	StructureID    string    `bun:"structure_id,nullzero"`
	Side           string    `bun:"side,nullzero"`
	FromCm         *int64    `bun:"from_cm"` // linear only; 0 is a valid offset
	ToCm           *int64    `bun:"to_cm"`
	Outline        JSON      `bun:"outline"` // surface only
	MooringMode    string    `bun:"mooring_mode,nullzero"`
	CellCm         int64     `bun:"cell_cm"`
	MarginCm       int64     `bun:"margin_cm"`
	MaxLOACm       int64     `bun:"max_loa_cm,nullzero"`
	MaxBeamCm      int64     `bun:"max_beam_cm,nullzero"`
	MaxHeightCm    int64     `bun:"max_height_cm,nullzero"`
	MaxWeightKg    int64     `bun:"max_weight_kg,nullzero"`
	Supports       Strings   `bun:"supports"`
	DepthProfile   JSON      `bun:"depth_profile"`
	BlockedCells   JSON      `bun:"blocked_cells"` // []int
	Exits          JSON      `bun:"exits"`         // []int
	GridRotationDd int64     `bun:"grid_rotation_dd"`
	CellCount      int64     `bun:"cell_count"`
	PriceClass     string    `bun:"price_class,nullzero"`
	Active         bool      `bun:"active"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// LandResource is a row of land_resources (parking spots, boxes, lockers, ...).
type LandResource struct {
	bun.BaseModel `bun:"table:land_resources"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	MapID      string    `bun:"map_id"`
	Kind       string    `bun:"kind"`
	Code       string    `bun:"code"`
	XCm        int64     `bun:"x_cm"`
	YCm        int64     `bun:"y_cm"`
	RotationDd int64     `bun:"rotation_dd"`
	WidthCm    int64     `bun:"width_cm"`
	LengthCm   int64     `bun:"length_cm"`
	PriceClass string    `bun:"price_class,nullzero"`
	Attributes Strings   `bun:"attributes"`
	Active     bool      `bun:"active"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// MapDecoration is a row of map_decorations (kit symbols, text, depth labels, north arrow, scale bar).
type MapDecoration struct {
	bun.BaseModel `bun:"table:map_decorations"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	MapID     string    `bun:"map_id"`
	Kind      string    `bun:"kind"`
	Asset     string    `bun:"asset,nullzero"`
	Variant   string    `bun:"variant,nullzero"`
	Geometry  JSON      `bun:"geometry"`
	Label     JSON      `bun:"label"` // I18nText
	DepthCm   *int64    `bun:"depth_cm"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// BerthGroup is a row of berth_groups (marina-level, overlapping memberships).
type BerthGroup struct {
	bun.BaseModel `bun:"table:berth_groups"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Code      string    `bun:"code"`
	Name      JSON      `bun:"name"` // I18nText
	Kind      string    `bun:"kind"` // structure, transit, area, custom
	SortOrder int       `bun:"sort_order"`
	Version   int       `bun:"version"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// BerthGroupMember is a row of berth_group_members.
type BerthGroupMember struct {
	bun.BaseModel `bun:"table:berth_group_members"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	GroupID      string    `bun:"group_id"`
	MapID        string    `bun:"map_id"`
	ResourceType string    `bun:"resource_type"` // berth, dynamic_area, land_resource
	ResourceID   string    `bun:"resource_id"`
	CreatedAt    time.Time `bun:"created_at"`
}
