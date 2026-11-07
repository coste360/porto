package engine

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// Family is an occupancy row family (one table each, DB-enforced, ADR 0003).
type Family string

// Families.
const (
	FamilyBerth Family = "berth" // berth_occupancies (berths and buoys)
	FamilyCell  Family = "cell"  // cell_occupancies (dynamic areas)
	FamilyLand  Family = "land"  // land_occupancies
)

// ResourceType is the type of an allocated resource (allocations.resource_type).
type ResourceType string

// Resource types.
const (
	ResourceBerth ResourceType = "berth"
	ResourceArea  ResourceType = "dynamic_area"
	ResourceLand  ResourceType = "land_resource"
)

// FamilyOf returns the row family of a resource type.
func FamilyOf(t ResourceType) Family {
	switch t {
	case ResourceArea:
		return FamilyCell
	case ResourceLand:
		return FamilyLand
	default:
		return FamilyBerth
	}
}

// Row is one occupancy row: Cell is 0 for berths and land resources.
type Row struct {
	Resource string
	Cell     int
	Night    calendar.Date
}

// Placement is where a vessel sits in a dynamic area (layout.Placement in M3 units).
type Placement struct {
	X, Y     geo.Cm
	Rotation geo.Angle
}

// NightRange is an inclusive range of nights.
type NightRange struct {
	From, To calendar.Date
}

// Interval is an inclusive range of cells [First, Last].
type Interval struct {
	First, Last int
}

// AllocSpec is what an allocation occupies: a resource, a placement (areas only) and its nights.
type AllocSpec struct {
	ResourceType ResourceType
	ResourceID   string
	Placement    *Placement    // areas only
	First, Last  calendar.Date // inclusive nights
	Released     []NightRange  // excluded nights (sorted, disjoint)
}

// Prefs are the desk preferences of a search (spec 10.7).
type Prefs struct {
	GroupIDs     []string
	StructureIDs []string
	PowerA       *int64
	Water        bool
}

// BerthCand is a berth (or buoy) considered by the search, with its fit inputs.
type BerthCand struct {
	ID, Code, MapID  string
	StructureID      string
	GroupIDs         []string
	Target           fit.Target
	Water            bool
	TransitDedicated bool // attribute tag transit_only
	OnlineSellable   bool
}

// AreaCand is a linear dynamic area considered by the search (surface areas are not searched, m3-plan §13 D7).
type AreaCand struct {
	ID, Code, MapID  string
	StructureID      string
	GroupIDs         []string
	Target           fit.Target         // Depth is filled per interval by the solver from DepthProfile
	Mode             layout.MooringMode // stern_to, bow_to, alongside, ...
	CellCm           geo.Cm
	MarginCm         geo.Cm
	LengthCm         geo.Cm
	CellCount        int
	DepthProfile     []DepthPoint
	TransitDedicated bool
	OnlineSellable   bool
}

// DepthPoint is one point of the depth profile of a linear area (offset along the axis, depth).
type DepthPoint struct {
	AtCm, DepthCm geo.Cm
}

// LandCand is a land resource considered by the search.
type LandCand struct {
	ID, Code, MapID string
	GroupIDs        []string
	Target          fit.Target
	OnlineSellable  bool
}

// OutCand is a contract allocation on a berth whose vessel is out for the whole requested period.
type OutCand struct {
	AllocationID string
	ContractID   string
}

// Closure is a closure of arrivals already resolved to resource ids (empty ResourceIDs = whole marina).
type Closure struct {
	ID          string
	ResourceIDs []string
	From, To    calendar.Date // inclusive dates
}

// Snapshot is everything the solver reads, loaded with a fixed number of queries (m3-plan §3.6).
type Snapshot struct {
	Today    calendar.Date
	Berths   []BerthCand
	Areas    []AreaCand
	Lands    []LandCand
	Busy     map[string][]NightRange                 // berth/land id → occupied nights (expired holds excluded)
	AreaBusy map[string]map[calendar.Date][]Interval // linear area → night → occupied cell intervals
	OutOK    map[string]OutCand                      // berth id → contract allocation whose vessel is out
	Closures []Closure
	Previous map[string]string // vessel id → last resource id (continuity)
}

// Search purposes.
const (
	PurposeTransit  = "transit"
	PurposeContract = "contract"
)

// Request is a search request (spec 10.7).
type Request struct {
	Vessel             fit.Vessel
	VesselID           string
	Arrival, Departure calendar.Date // Departure exclusive
	Purpose            string        // transit, contract
	ResourceTypes      []ResourceType
	Prefs              Prefs
	Channel            string // desk, api (online in M8)
	MaxSegments        int
	ConsiderOut        bool
	Limit              int // default 20
}

// Segment is one contiguous part of a solution on one resource.
type Segment struct {
	ResourceType ResourceType
	ResourceID   string
	Placement    *Placement
	First, Last  calendar.Date
}

// Solution is one candidate placement of a search.
type Solution struct {
	Key             string     // stable: resource ids + x + nights
	Segments        []Segment  // 1..MaxSegments+1, contiguous, cover [Arrival, Departure)
	Score           int64      // lower is better
	Fit             fit.Result // of the worst segment
	RequiresRelease *OutCand   // the berth is taken by a contract whose vessel is out

	sortCode string // resource code (and x) for the tie break
}

// Placed is a vessel already placed in a linear area, input of Compact.
type Placed struct {
	AllocationID string
	X            geo.Cm
	Width        geo.Cm        // footprint width along the axis (beam or LOA by mooring mode)
	First, Last  calendar.Date // nights of the allocation
	Fixed        bool          // does not cover the whole compaction range: an obstacle that stays where it is
}

// Proposal is the new position of an allocation proposed by Compact.
type Proposal struct {
	AllocationID string
	X            geo.Cm
}

// Anomaly codes of the M3 day view (m3-plan §8.4; i18n occupancyEnums:anomaly.*).
const (
	AnomalyPresenceWithoutAllocation = "presence_without_allocation"
	AnomalyNotArrived                = "not_arrived"
	AnomalyDepartureOverdue          = "departure_overdue"
	AnomalyVesselExceedsLimits       = "vessel_exceeds_limits"
	AnomalyInsuranceExpiredActive    = "insurance_expired_active"
	AnomalyHoldLostDraft             = "hold_lost_draft"
)

// AnomalyInput is the day of a map as seen by Anomalies. Times are minutes from local midnight of Date.
type AnomalyInput struct {
	Date         calendar.Date
	NowMinutes   int // now, minutes from local midnight of Date (negative or ≥ 1440 when now is another day)
	GraceMinutes int // occupancy_settings.arrival_grace_minutes
	Elements     []AnomalyElement
}

// AnomalyElement is one resource (or area placement) of the day view.
type AnomalyElement struct {
	ResourceID          string
	AllocationID        string
	HasAllocation       bool   // an active allocation covers Date
	PresenceInPort      bool   // the vessel of the element is in port
	ContractKind        string // mooring, transit ("" = none)
	ContractStatus      string
	Arrival, Departure  calendar.Date // contract starts_on, ends_on (exclusive)
	ExpectedArrivalMins int           // expected_arrival_time or default_arrival_time
	ArrivedPresence     bool          // an arrived presence exists for this stay
	FitBlocking         bool          // current fit has blocking codes (with or without override)
	InsuranceOK         bool          // valid insurance on Date
	HoldLost            bool
}

// Anomaly is one anomaly found on a resource.
type Anomaly struct {
	ResourceID   string
	AllocationID string
	Code         string
}
