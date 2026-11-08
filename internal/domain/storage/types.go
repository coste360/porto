// Package storage is the dry storage service (spec 17; m9-plan §3–§7): storage contracts on surface areas (through
// contract.CreateStorageTx, INV-30), the area snapshot, plan and accessibility views, the optimizer preset, the
// scenarios (working copies that never write allocations or occupancy rows, INV-14), their transactional apply
// through occupancy.LayoutTx (all or nothing, re-validated under the locks, INV-06) and the movement worklist.
// Permissions: storage.manage for changes, storage.read for reads (spec 26.1 "Rimessaggio e ipotesi").
package storage

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Limits and defaults (m9-plan §6.4, §7.1, D11).
const (
	// MaxScenarioNights is the longest scenario window.
	MaxScenarioNights = 400
	// DefaultOptimizeBudget is the default caller deadline around optimizer.Arrange.
	DefaultOptimizeBudget = 2500 * time.Millisecond
	// PlanDefaultDays is the default plan window (today .. today + 30).
	PlanDefaultDays = 30
	// maxScenarioName bounds a scenario name.
	maxScenarioName = 80
)

// Item issue reasons (StorageItemIssue.reason).
const (
	IssueOverlap     = "overlap"
	IssueBlockedCell = "blocked_cell"
	IssueOutside     = "outside"
	IssueFit         = "fit"
	IssueUnplaced    = "unplaced"
	IssueStale       = "stale"
)

// Row types the handlers render (aliases, so that internal/http does not import internal/store).
type (
	// Vessel is a vessels row.
	Vessel = store.Vessel
	// Allocation is an allocations row.
	Allocation = store.Allocation
	// Scenario is a storage_scenarios row.
	Scenario = store.StorageScenario
)

// ContractInput is POST storage/contracts.
type ContractInput struct {
	Term              string
	CustomerID        string
	BillingCustomerID string
	VesselID          string
	StartsOn          calendar.Date
	EndsOn            *calendar.Date
	AreaID            string // surface dynamic area; or TrailerSpotID
	TrailerSpotID     string
	Placement         *optimizer.Placement // nil on an area = automatic (optimizer.PlaceOne)
	CradleResourceID  string
	Details           contract.StorageDetailsInput
	Services          []contract.ServiceRequest
	Policy            *contract.PolicyInput
	Notes             string
	Activate          bool
	FitOverride       *contract.FitOverrideInput
}

// ContractView is a storage contract with its details, placement and cradle allocation.
type ContractView struct {
	Contract  *contract.Detail
	Details   store.StorageContractDetails
	ExitOn    calendar.Date
	ArrivalOn calendar.Date
	Placement *store.Allocation
	Cradle    *store.Allocation
	Fit       *fit.Result // fit of the vessel at its placement (areas only)
}

// AreaSummary is one row of GET storage/areas.
type AreaSummary struct {
	Area               store.DynamicArea
	MapKind            string
	CellCm             geo.Cm
	Cols, Rows         int
	UsableCells        int
	UsableCm2          int64
	OccupiedCellsToday int
	BoatsToday         int
	ExitsBlockedNext30 int
	DraftScenarios     int
	GatesValid         bool
}

// SnapshotItem is an allocation of the area (or a tray allocation) with what the optimizer and the views need.
type SnapshotItem struct {
	Allocation     store.Allocation
	ContractID     string
	ContractKind   string
	ContractCode   string
	ContractStatus string
	ContractTerm   string
	Vessel         store.Vessel
	Fit            fit.Vessel // effective size
	Support        string
	Displacement   *int64
	ArrivalOn      calendar.Date
	ExitOn         calendar.Date
	Cells          []int // fine cells of the current placement on this area (nil when elsewhere or invalid)
	OnArea         bool  // the allocation is on the snapshot's area
	FitOverride    bool  // the allocation carries a fit override
}

// Placement returns the current placement on the area (nil when the allocation is elsewhere).
func (it *SnapshotItem) Placement() *optimizer.Placement {
	a := &it.Allocation
	if !it.OnArea || a.XCm == nil || a.YCm == nil {
		return nil
	}
	p := optimizer.Placement{X: geo.Cm(*a.XCm), Y: geo.Cm(*a.YCm)}
	if a.RotationDd != nil {
		p.Rotation = geo.Angle(*a.RotationDd)
	}
	return &p
}

// Obstacle is a fixed occupation of the area (block, maintenance).
type Obstacle struct {
	optimizer.Obstacle
	Kind string
}

// Snapshot is the state of a surface area for a night window, read with a few queries (m9-plan §4).
type Snapshot struct {
	AreaID    string
	Area      store.StorageArea
	From, To  calendar.Date
	Grid      optimizer.Grid
	Gates     []optimizer.GateIssue
	Items     []SnapshotItem // vessel allocations (contract, transit, owner, hold)
	Obstacles []Obstacle     // blocks and maintenance rows
}

// LayoutItem is one position of a draft layout (nil At = unplaced).
type LayoutItem struct {
	AllocationID string
	At           *optimizer.Placement
	Pinned       bool
}

// Indicators are the scenario and draft indicators (m9-plan §7.2).
type Indicators struct {
	UsableCm2          int64         `json:"usable_cm2"`
	UsedHullCm2        int64         `json:"used_hull_cm2"`
	UsedFootprintCm2   int64         `json:"used_footprint_cm2"`
	Moves              int           `json:"moves"`
	Unplaced           int           `json:"unplaced"`
	Overlaps           int           `json:"overlaps"`
	ExitsBlocked       int           `json:"exits_blocked"`
	ArrivalsBlocked    int           `json:"arrivals_blocked"`
	LargestFreeRectCm2 int64         `json:"largest_free_rect_cm2"`
	BlockedByDate      []DateBlocked `json:"blocked_by_date"`
	Stale              bool          `json:"stale"`
}

// DateBlocked counts the blocked events of a date.
type DateBlocked struct {
	Date            calendar.Date `json:"date"`
	ExitsBlocked    int           `json:"exits_blocked"`
	ArrivalsBlocked int           `json:"arrivals_blocked"`
}

// ItemIssue is a per-item problem of a draft (overlap, blocked_cell, outside, fit, unplaced, stale).
type ItemIssue struct {
	AllocationID     string
	Reason           string
	FitCodes         []string
	WithAllocationID []string
}

// Access is an accessibility entry with the vessel of the allocation.
type Access struct {
	optimizer.Access
	VesselID string
}

// ArrangeInput is the preset request (draft nil = the real state of the window).
type ArrangeInput struct {
	From, To *calendar.Date
	Draft    []LayoutItem
	Seed     *uint64
	MaxIter  int
}

// ArrangeResult is the preset proposal (nothing saved).
type ArrangeResult struct {
	Items      []optimizer.ItemResult
	Indicators Indicators
	Access     []Access
	Seed       uint64
	Iterations int
	StoppedBy  string
}

// Evaluation is the evaluation of a draft layout.
type Evaluation struct {
	Indicators Indicators
	Access     []Access
	Issues     []ItemIssue
}

// PlanView is GET storage/areas/{id}/plan.
type PlanView struct {
	Summary  AreaSummary
	Snapshot *Snapshot
	Access   []Access
}

// AccessView is GET storage/areas/{id}/accessibility.
type AccessView struct {
	AreaID          string
	Date            calendar.Date
	Entries         []Access
	ExtractionOrder []string // allocation ids in extraction order
}

// ScenarioInput creates a scenario.
type ScenarioInput struct {
	AreaID   string
	Name     string
	From, To calendar.Date
}

// ScenarioPatch renames a scenario or changes its window.
type ScenarioPatch struct {
	Name     *string
	From, To *calendar.Date
}

// ScenarioFilter filters the scenario list.
type ScenarioFilter struct {
	AreaID, Status, Cursor string
	Limit                  int
}

// ScenarioItemView is an item of the working copy with its allocation and vessel.
type ScenarioItemView struct {
	store.ScenarioItem
	Snapshot       *SnapshotItem // nil when the allocation is gone from the window
	BaseVersion    int
	CurrentVersion int
}

// ScenarioDetail is a scenario with its working copy, moves, issues and accessibility.
type ScenarioDetail struct {
	Scenario   store.StorageScenario
	Indicators *Indicators
	Items      []ScenarioItemView
	Moves      []store.ScenarioMove
	Issues     []ItemIssue
	Access     []Access
	Run        *ArrangeResult // set by Optimize
}

// Comparison compares a scenario with another or with the real state ("real").
type Comparison struct {
	ScenarioID  string
	With        string
	Date        calendar.Date
	Left, Right Indicators
	Diffs       []PositionDiff
}

// PositionDiff is a boat whose position differs.
type PositionDiff struct {
	AllocationID string
	VesselID     string
	Left, Right  *MoveEnd
}

// MoveEnd is one end of a move: a resource and, on areas, a position.
type MoveEnd struct {
	ResourceType string
	ResourceID   string
	At           *optimizer.Placement
}

// Conflict is one entry of ConflictsError.
type Conflict struct {
	AllocationID     string
	Reason           string
	Cells            []int
	From, To         *calendar.Date
	FitCodes         []string
	WithAllocationID []string
}

// ApplyResult is the outcome of an apply: the scenario and the movement worklist.
type ApplyResult struct {
	Scenario  store.StorageScenario
	Movements []MovementView
	// PriceEffect is the repricing of the vessels brought in from the tray (nil when nothing was priced).
	PriceEffect *occupancy.PriceEffect
}

// MovementFilter filters the movement worklist.
type MovementFilter struct {
	Date           *calendar.Date
	AreaID, Status string
}

// MovementView is a movement with its vessel.
type MovementView struct {
	store.StorageMovement
	Vessel *store.Vessel
}

// MovementList is the worklist of a day with its accessibility warnings.
type MovementList struct {
	Date     calendar.Date
	Items    []MovementView
	Warnings []Access
}
