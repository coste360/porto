package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M3 occupancy tables (internal/migrations/*/00006_occupancy.sql). Owned by the
// OCCUPANCY-ENGINE builder after the contract phase (fields may be added only if they exist in the migration).
// No double occupancy is the primary key of the three row tables (INV-01, ADR 0003): rows are inserted with
// ON CONFLICT DO NOTHING and a shortfall is a conflict, never a "check then insert".

// Resource types of an allocation (and of the occupancy row families).
const (
	ResourceBerth = "berth"         // berth_occupancies (berths and buoys)
	ResourceArea  = "dynamic_area"  // cell_occupancies (linear and surface areas)
	ResourceLand  = "land_resource" // land_occupancies
)

// Allocation kinds.
const (
	AllocKindHold        = "hold"
	AllocKindContract    = "contract"
	AllocKindTransit     = "transit"
	AllocKindOwner       = "owner"
	AllocKindBlock       = "block"
	AllocKindMaintenance = "maintenance"
)

// Allocation statuses: only active allocations have occupancy rows (m3-plan §3.2).
const (
	AllocActive     = "active"
	AllocCancelled  = "cancelled"
	AllocExpired    = "expired"
	AllocSuperseded = "superseded"
)

// Allocation is a row of allocations: a vessel (or a block) positioned on a resource for a range of nights.
type Allocation struct {
	bun.BaseModel `bun:"table:allocations"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	ContractLineID  string        `bun:"contract_line_id,nullzero"`
	VesselID        string        `bun:"vessel_id,nullzero"` // empty only for kind block
	ResourceType    string        `bun:"resource_type"`
	ResourceID      string        `bun:"resource_id"`
	MapID           string        `bun:"map_id"`
	XCm             *int64        `bun:"x_cm"` // dynamic areas only
	YCm             *int64        `bun:"y_cm"`
	RotationDd      *int64        `bun:"rotation_dd"`
	FirstNight      calendar.Date `bun:"first_night"`
	LastNight       calendar.Date `bun:"last_night"` // inclusive
	Kind            string        `bun:"kind"`
	Status          string        `bun:"status"`
	ExpiresAt       time.Time     `bun:"expires_at,nullzero"`
	FitOverride     JSON          `bun:"fit_override"` // {user_id, reason, codes[]}, nil = none
	BlockReason     string        `bun:"block_reason,nullzero"`
	MovedFromID     string        `bun:"moved_from_id,nullzero"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// BerthOccupancy is a row of berth_occupancies: primary key (berth_id, night).
type BerthOccupancy struct {
	bun.BaseModel `bun:"table:berth_occupancies"`

	MarinaID     string        `bun:"marina_id"`
	BerthID      string        `bun:"berth_id,pk"`
	Night        calendar.Date `bun:"night,pk"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
	ExpiresAt    time.Time     `bun:"expires_at,nullzero"`
}

// CellOccupancy is a row of cell_occupancies: primary key (area_id, cell, night).
type CellOccupancy struct {
	bun.BaseModel `bun:"table:cell_occupancies"`

	MarinaID     string        `bun:"marina_id"`
	AreaID       string        `bun:"area_id,pk"`
	Cell         int64         `bun:"cell,pk"`
	Night        calendar.Date `bun:"night,pk"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
	ExpiresAt    time.Time     `bun:"expires_at,nullzero"`
}

// LandOccupancy is a row of land_occupancies: primary key (resource_id, night).
type LandOccupancy struct {
	bun.BaseModel `bun:"table:land_occupancies"`

	MarinaID     string        `bun:"marina_id"`
	ResourceID   string        `bun:"resource_id,pk"`
	Night        calendar.Date `bun:"night,pk"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
	ExpiresAt    time.Time     `bun:"expires_at,nullzero"`
}

// Release is a row of releases: nights of a contract allocation given back for resale (spec 10.6).
type Release struct {
	bun.BaseModel `bun:"table:releases"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	AllocationID       string        `bun:"allocation_id"`
	ContractID         string        `bun:"contract_id"`
	ResourceType       string        `bun:"resource_type"`
	ResourceID         string        `bun:"resource_id"`
	FromNight          calendar.Date `bun:"from_night"`
	ToNight            calendar.Date `bun:"to_night"` // inclusive
	Status             string        `bun:"status"`   // active, revoked
	Source             string        `bun:"source"`
	ResoldAllocationID string        `bun:"resold_allocation_id,nullzero"`
	ResoldNights       int64         `bun:"resold_nights"`
	CreditCents        int64         `bun:"credit_cents"`
	Notes              string        `bun:"notes"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	RevokedAt          time.Time     `bun:"revoked_at,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// Presence is a row of presences: an arrival, departure, haul-out or launch of a vessel.
type Presence struct {
	bun.BaseModel `bun:"table:presences"`

	ID           string        `bun:"id,pk"`
	MarinaID     string        `bun:"marina_id"`
	VesselID     string        `bun:"vessel_id"`
	AllocationID string        `bun:"allocation_id,nullzero"`
	ContractID   string        `bun:"contract_id,nullzero"`
	Event        string        `bun:"event"` // arrived, departed, hauled_out, launched
	At           time.Time     `bun:"at"`
	Night        calendar.Date `bun:"night"`  // local date of At
	Source       string        `bun:"source"` // staff, sensor, customer
	UserID       string        `bun:"user_id,nullzero"`
	Notes        string        `bun:"notes"` // never logged
	PhotoFileID  string        `bun:"photo_file_id,nullzero"`
	CreatedAt    time.Time     `bun:"created_at"`
	UpdatedAt    time.Time     `bun:"updated_at"`
}

// VesselPresence is the row of vessel_presence of a vessel: its current state (m3-plan §13 D21).
type VesselPresence struct {
	bun.BaseModel `bun:"table:vessel_presence"`

	VesselID         string        `bun:"vessel_id,pk"`
	MarinaID         string        `bun:"marina_id"`
	State            string        `bun:"state"` // in_port, out, ashore, yard
	Since            time.Time     `bun:"since"`
	AllocationID     string        `bun:"allocation_id,nullzero"`
	LastPresenceID   string        `bun:"last_presence_id"`
	ExpectedReturnOn calendar.Date `bun:"expected_return_on,nullzero"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// MoveUndo is a row of move_undo_log: what a move, swap or layout save changed, undoable for 24 h.
type MoveUndo struct {
	bun.BaseModel `bun:"table:move_undo_log"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	UserID    string    `bun:"user_id,nullzero"`
	Operation string    `bun:"operation"` // move, swap, layout
	Payload   JSON      `bun:"payload"`   // {entries:[{allocation_id, status, last_night, version}], created:[ids]}
	ExpiresAt time.Time `bun:"expires_at"`
	UndoneAt  time.Time `bun:"undone_at,nullzero"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// Closure is a row of closures: new arrivals refused on a scope for a range of dates (spec 10.8).
type Closure struct {
	bun.BaseModel `bun:"table:closures"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	ScopeType       string        `bun:"scope_type"` // marina, map, structure, group, resource
	ScopeID         string        `bun:"scope_id,nullzero"`
	ResourceType    string        `bun:"resource_type,nullzero"` // scope resource only
	FromDate        calendar.Date `bun:"from_date"`
	ToDate          calendar.Date `bun:"to_date"` // inclusive
	Reason          string        `bun:"reason"`  // no PII by policy
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// QuoteOptionPlacement is a row of quote_option_placements: one segment of a quote option.
type QuoteOptionPlacement struct {
	bun.BaseModel `bun:"table:quote_option_placements"`

	ID           string        `bun:"id,pk"`
	MarinaID     string        `bun:"marina_id"`
	OptionID     string        `bun:"option_id"`
	Seq          int           `bun:"seq"` // 1..2
	ResourceType string        `bun:"resource_type"`
	ResourceID   string        `bun:"resource_id"`
	XCm          *int64        `bun:"x_cm"`
	YCm          *int64        `bun:"y_cm"`
	RotationDd   *int64        `bun:"rotation_dd"`
	FirstNight   calendar.Date `bun:"first_night"`
	LastNight    calendar.Date `bun:"last_night"`
	AllocationID string        `bun:"allocation_id,nullzero"` // the hold, when the option holds
	CreatedAt    time.Time     `bun:"created_at"`
	UpdatedAt    time.Time     `bun:"updated_at"`
}
