package optimizer

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// Limits (m9-plan §4, §6.4, D11, D12).
const (
	// MaxCells is the largest grid the optimizer works on; larger grids are coarsened.
	MaxCells = 40000
	// MaxItems is the largest number of items of one run (422 too_many_items beyond).
	MaxItems = 300
	// DefaultMaxIter is the default deterministic iteration budget.
	DefaultMaxIter = 20000
	// LateAcceptanceLength is the history length of the late-acceptance hill climbing.
	LateAcceptanceLength = 50
)

// Stop reasons of Result.StoppedBy.
const (
	StoppedByIterations = "iterations"
	StoppedByDeadline   = "deadline"
	StoppedByOptimal    = "optimal"
)

// Unplaced reasons.
const (
	UnplacedFit     = "fit"
	UnplacedNoSpace = "no_space"
)

// Access events.
const (
	EventExit    = "exit"
	EventArrival = "arrival"
)

var (
	// ErrTooManyItems means more than MaxItems items (422 too_many_items).
	ErrTooManyItems = errors.New("optimizer: too many items")
	// ErrPinnedInvalid means a pinned item fails fit or Footprint in its position, or overlaps a fixed obstacle or
	// another pinned item (422 pinned_invalid).
	ErrPinnedInvalid = errors.New("optimizer: pinned item invalid")
	// ErrGatesInvalid means a gate is not usable or not on the grid border (422 area_exits_invalid).
	ErrGatesInvalid = errors.New("optimizer: exit gates invalid")
	// ErrInvalidLayout is returned by Validate; from Arrange it is a bug (the service answers 500 and logs the seed).
	ErrInvalidLayout = errors.New("optimizer: invalid layout")
)

// Grid is the optimizer view of a surface area: the layout.Area (usable and blocked cells, cell size, margin),
// the exit cells and their gates, the gate distance field and the coarsening factor.
type Grid struct {
	Area   layout.Area
	Exits  []int   // exit cell indexes (maps.exits of the area, M1), sorted
	Gates  [][]int // 4-connected components of Exits, each sorted; ordered by smallest cell
	Dist   []int32 // multi-source BFS distance from the gate cells over usable non-blocked cells; -1 = unreachable
	Coarse int     // coarsening factor k ≥ 1 (1 = the fine grid)
}

// GateIssue is a gate refused by ValidateGates.
type GateIssue struct {
	Gate   int    // index in Grid.Gates
	Reason string // "not_usable", "not_on_border"
}

// Obstacle is a fixed occupation of cells for a night range (blocks, maintenance rows, allocations not part of
// the run).
type Obstacle struct {
	ID          string
	Cells       []int // fine cell indexes, sorted
	First, Last calendar.Date
}

// Placement is a surface placement (top-left corner of the footprint box in grid cm, rotation 0/900/1800/2700).
type Placement = layout.Placement

// Item is a boat of the run: an allocation with its size, support, weight and stay. Current nil = from outside the
// area or unplaced. Pinned items keep Current (it must be valid).
type Item struct {
	ID             string // allocation id
	LOA, Beam      geo.Cm
	Fit            fit.Vessel // size, hull, air draft for the area target
	Support        string     // storage support (cradle, trailer, rack, none)
	DisplacementKg *int64
	First, Last    calendar.Date // inclusive nights of the stay in the window
	ArrivalOn      calendar.Date // arrival event date (haul-out or first night)
	ExitOn         calendar.Date // exit event date (launch, contract end, or last night + 1)
	Current        *Placement
	Pinned         bool
	// FitOverridden is an allocation accepted with a fit override (occupancy.override_fit): an overridable fit
	// failure (size, weight) does not exclude it; support_not_allowed still does (m9-plan D6).
	FitOverridden bool
}

// Placed is an item at a position, as used by Accessibility.
type Placed struct {
	ID          string
	LOA, Beam   geo.Cm
	At          Placement
	First, Last calendar.Date
	ExitOn      calendar.Date
}

// FitParams are the fit rules and the area target of the run (max LOA, max height, supports, weight per cell).
type FitParams struct {
	Rules  fit.Rules
	Target fit.Target // TargetSurfaceArea; MaxWeightKg per hull cell is scaled by the caller per placement
	// MaxWeightPerCellKg is the area max_weight_kg per cell (nil = no limit).
	MaxWeightPerCellKg *int64
}

// Input is one optimizer run.
type Input struct {
	Grid    Grid
	Fixed   []Obstacle
	Items   []Item
	Fit     FitParams
	Seed    uint64
	MaxIter int    // 0 = DefaultMaxIter
	Margin  geo.Cm // footprint margin (layout.Area.Margin)
	// From and To bound the night window of the run (scenario window); the free rectangle term is measured on From.
	From, To calendar.Date
}

// ItemResult is the outcome of one item: At set when placed, else UnplacedReason (and the fit codes when "fit").
type ItemResult struct {
	ID             string
	At             *Placement
	UnplacedReason string
	FitCodes       []string
}

// Score is the lexicographic objective (smaller is better, m9-plan §6.2).
type Score struct {
	Unplaced        int64
	ExitsBlocked    int64
	ArrivalsBlocked int64
	Moves           int64
	NegFreeRectCm2  int64 // −largest free rectangle on the start night
}

// Less reports whether s is strictly better than o.
func (s Score) Less(o Score) bool {
	switch {
	case s.Unplaced != o.Unplaced:
		return s.Unplaced < o.Unplaced
	case s.ExitsBlocked != o.ExitsBlocked:
		return s.ExitsBlocked < o.ExitsBlocked
	case s.ArrivalsBlocked != o.ArrivalsBlocked:
		return s.ArrivalsBlocked < o.ArrivalsBlocked
	case s.Moves != o.Moves:
		return s.Moves < o.Moves
	default:
		return s.NegFreeRectCm2 < o.NegFreeRectCm2
	}
}

// Access is the accessibility of one event of one boat (m9-plan §5). Order is the extraction order among the exits
// of that day (1 = first, 0 = not extracted); Blocking are the ids of the blocking boats.
type Access struct {
	ID         string
	Event      string // EventExit or EventArrival
	On         calendar.Date
	Accessible bool
	Blocking   []string
	Order      int
}

// TimelineItem is a boat of AccessibilityTimeline.
type TimelineItem struct {
	Placed
	ArrivalOn calendar.Date
}

// Result is the outcome of Arrange.
type Result struct {
	Placements []ItemResult // one per input item, in input order
	Score      Score
	Access     []Access // timeline of the result
	Iterations int
	StoppedBy  string
}
