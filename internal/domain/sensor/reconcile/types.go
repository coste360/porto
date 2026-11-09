package reconcile

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// Expectations (m12-plan §8.1).
const (
	ExpectOccupied = "occupied"
	ExpectFree     = "free"
	ExpectEither   = "either"
	ExpectNone     = "none" // informational device: no rules, no discrepancy
)

// Physical states.
const (
	Occupied = "occupied"
	Free     = "free"
	Unknown  = "unknown"
)

// Alert kinds (sensor_alerts.kind).
const (
	KindUnexpectedOccupied = "unexpected_occupied"
	KindTransitArrival     = "transit_arrival"
	KindReturnDetected     = "return_detected"
	KindReleaseProposal    = "release_proposal"
	KindEarlyReturn        = "early_return"
	KindDeviceSilent       = "device_silent"
	KindBatteryLow         = "battery_low"
)

// Presence events of RecordPresence.
const (
	EventArrived  = "arrived"
	EventDeparted = "departed"
)

// Settings are the thresholds of sensor_settings used by the rules.
type Settings struct {
	UnexpectedAfterMinutes   int
	DepartureCutoffMinutes   int
	SilentAlertMinutes       int
	BatteryLowPct            int
	PresenceEffectEnabled    bool
	PresenceOutAfterHours    int
	ReleaseProposalAfterDays int
	ReleaseProposalNights    int
}

// Stay is one contractual fact of a night: an allocation (or area placement) with its contract.
type Stay struct {
	AllocationID string
	ContractID   string
	ContractKind string // mooring, transit, storage, ... ("" = block or maintenance allocation)
	ContractTerm string // annual, seasonal, ... (mooring)
	Status       string // contract status: active, confirmed, held, arrived, ...
	VesselID     string
	FirstNight   calendar.Date
	LastNight    calendar.Date
	// Kind of allocation: contract, block, maintenance.
	AllocationKind string
	ReleasePolicy  string // never, with_consent (mooring)
	// XCm and LoaCm place an area placement on the area axis (segments).
	XCm, LoaCm int64
}

// Release is an active release covering D or D + 1.
type Release struct {
	ID           string
	AllocationID string
	FromNight    calendar.Date
	ToNight      calendar.Date
	Resold       bool   // resold to another allocation
	ResaleStay   *Stay  // the resold allocation, when Resold
	ResaleStatus string // its contract status
}

// Presence is the vessel presence of the subject's vessel.
type Presence struct {
	VesselID         string
	State            string // in_port, out, ashore, yard ("" = no row)
	LastSource       string // source of the last presence: staff, sensor
	PresenceID       string
	Since            time.Time
	ExpectedReturnOn *calendar.Date
}

// Facts are the contractual facts of a subject on night D and D - 1.
type Facts struct {
	Today     []Stay // allocations covering D intersecting the subject
	Yesterday []Stay // allocations covering D - 1 intersecting the subject
	Releases  []Release
	Closed    bool // closure covering D
	Presences map[string]Presence
}

// Subject is one device with its physical state and facts.
type Subject struct {
	DeviceID      string
	ResourceType  string // berth, dynamic_area, pedestal
	ResourceID    string
	MapID         string
	Informational bool
	Active        bool
	CreatedAt     time.Time
	Physical      string
	PhysicalSince time.Time
	LastContactAt *time.Time
	BatteryPct    *int
	Facts         Facts
}

// AlertKey is an uncleared alert condition already recorded (any status).
type AlertKey struct {
	DeviceID     string
	Kind         string
	ConditionKey string
	Open         bool
}

// Input is everything Evaluate reads.
type Input struct {
	Now      time.Time
	Loc      *time.Location
	Settings Settings
	Subjects []Subject
	Alerts   []AlertKey
}

// Refs are the ids and enums of an alert (store.SensorAlertRefs).
type Refs struct {
	AllocationID, ContractID, VesselID, ReleaseID, PresenceID string
	Resold                                                    *bool
	ProposedFrom, ProposedTo                                  *calendar.Date
	PhysicalSince                                             *time.Time
}

// Action is one effect of Evaluate: exactly one of the pointers is set.
type Action struct {
	Open     *OpenAlert
	Clear    *ClearCondition
	Presence *RecordPresence
}

// OpenAlert opens an alert unless the condition is already recorded.
type OpenAlert struct {
	DeviceID, Kind, ConditionKey string
	Refs                         Refs
}

// ClearCondition clears the uncleared rows of (device, kind) and resolves the open one.
type ClearCondition struct {
	DeviceID, Kind string
}

// RecordPresence is the automatic presence of R3/R3b (presence.RecordSensorTx).
type RecordPresence struct {
	DeviceID, VesselID, AllocationID, Event string
	At                                      time.Time
}
