package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M9 storage tables (internal/migrations/*/00018_storage.sql, m9-plan §13). Owned by the STORAGE
// builder after the contract phase; PLANNER reads them for the storage report (read-only).

// Storage enums (m9-plan §3.1, §7).
const (
	StorageSupportCradle  = "cradle"
	StorageSupportTrailer = "trailer"
	StorageSupportRack    = "rack"
	StorageSupportNone    = "none"

	StorageSupportOwn    = "own"
	StorageSupportMarina = "marina"

	ScenarioDraft     = "draft"
	ScenarioApplied   = "applied"
	ScenarioDiscarded = "discarded"

	ScenarioOriginArea = "area"
	ScenarioOriginTray = "tray"

	ScenarioMoveMove  = "move"
	ScenarioMovePlace = "place"

	MovementPending   = "pending"
	MovementDone      = "done"
	MovementCancelled = "cancelled"

	EquipmentHintTravelLift  = "travel_lift"
	EquipmentHintTrailer     = "trailer"
	EquipmentHintCradleMover = "cradle_mover"
	EquipmentHintForklift    = "forklift"
	EquipmentHintNone        = "none"
)

// StorageContractDetails is a row of storage_contract_details (1:1 with a storage contract).
type StorageContractDetails struct {
	bun.BaseModel `bun:"table:storage_contract_details"`

	ContractID    string        `bun:"contract_id,pk"`
	MarinaID      string        `bun:"marina_id"`
	Support       string        `bun:"support"`
	SupportOwner  string        `bun:"support_owner"`
	HaulOutOn     calendar.Date `bun:"haul_out_on,nullzero"`
	LaunchOn      calendar.Date `bun:"launch_on,nullzero"`
	NotesHandling string        `bun:"notes_handling"`
	Version       int           `bun:"version"`
	CreatedAt     time.Time     `bun:"created_at"`
	UpdatedAt     time.Time     `bun:"updated_at"`
}

// StorageScenario is a row of storage_scenarios. Seed is the optimizer uint64 seed as its int64 bit pattern.
type StorageScenario struct {
	bun.BaseModel `bun:"table:storage_scenarios"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	AreaID          string        `bun:"area_id"`
	Name            string        `bun:"name"`
	AuthorUserID    string        `bun:"author_user_id,nullzero"`
	FromOn          calendar.Date `bun:"from_on"`
	ToOn            calendar.Date `bun:"to_on"` // inclusive night
	Status          string        `bun:"status"`
	Seed            int64         `bun:"seed"`
	Score           JSON          `bun:"score"` // last indicators
	Base            JSON          `bun:"base"`  // {allocation_id: version}
	AppliedAt       time.Time     `bun:"applied_at,nullzero"`
	AppliedByUserID string        `bun:"applied_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// ScenarioItem is a row of scenario_items: one allocation of the working copy. Nil X/Y/Rotation = unplaced.
type ScenarioItem struct {
	bun.BaseModel `bun:"table:scenario_items"`

	ScenarioID   string `bun:"scenario_id,pk"`
	MarinaID     string `bun:"marina_id"`
	AllocationID string `bun:"allocation_id,pk"`
	VesselID     string `bun:"vessel_id,nullzero"`
	XCm          *int64 `bun:"x_cm"`
	YCm          *int64 `bun:"y_cm"`
	RotationDd   *int64 `bun:"rotation_dd"`
	Pinned       bool   `bun:"pinned"`
	Origin       string `bun:"origin"`
}

// ScenarioMove is a row of scenario_moves: the recomputed diff working copy vs real state, in execution order.
type ScenarioMove struct {
	bun.BaseModel `bun:"table:scenario_moves"`

	ScenarioID       string        `bun:"scenario_id,pk"`
	MarinaID         string        `bun:"marina_id"`
	Seq              int           `bun:"seq,pk"`
	AllocationID     string        `bun:"allocation_id"`
	VesselID         string        `bun:"vessel_id,nullzero"`
	FromResourceType string        `bun:"from_resource_type,nullzero"`
	FromResourceID   string        `bun:"from_resource_id,nullzero"`
	FromXCm          *int64        `bun:"from_x_cm"`
	FromYCm          *int64        `bun:"from_y_cm"`
	FromRotationDd   *int64        `bun:"from_rotation_dd"`
	ToResourceType   string        `bun:"to_resource_type"`
	ToResourceID     string        `bun:"to_resource_id"`
	ToXCm            *int64        `bun:"to_x_cm"`
	ToYCm            *int64        `bun:"to_y_cm"`
	ToRotationDd     *int64        `bun:"to_rotation_dd"`
	MoveOn           calendar.Date `bun:"move_on"`
	Kind             string        `bun:"kind"`
}

// StorageMovement is a row of storage_movements: the worklist written by a scenario apply.
type StorageMovement struct {
	bun.BaseModel `bun:"table:storage_movements"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	AreaID            string        `bun:"area_id"`
	ScenarioID        string        `bun:"scenario_id,nullzero"`
	AllocationID      string        `bun:"allocation_id"`
	VesselID          string        `bun:"vessel_id,nullzero"`
	Seq               int           `bun:"seq"`
	MoveOn            calendar.Date `bun:"move_on"`
	FromResourceType  string        `bun:"from_resource_type,nullzero"`
	FromResourceID    string        `bun:"from_resource_id,nullzero"`
	FromXCm           *int64        `bun:"from_x_cm"`
	FromYCm           *int64        `bun:"from_y_cm"`
	FromRotationDd    *int64        `bun:"from_rotation_dd"`
	ToResourceType    string        `bun:"to_resource_type"`
	ToResourceID      string        `bun:"to_resource_id"`
	ToXCm             *int64        `bun:"to_x_cm"`
	ToYCm             *int64        `bun:"to_y_cm"`
	ToRotationDd      *int64        `bun:"to_rotation_dd"`
	EquipmentHint     string        `bun:"equipment_hint"`
	TemporaryPosition bool          `bun:"temporary_position"`
	Status            string        `bun:"status"`
	DoneAt            time.Time     `bun:"done_at,nullzero"`
	DoneByUserID      string        `bun:"done_by_user_id,nullzero"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}
