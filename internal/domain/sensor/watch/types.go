package watch

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/store"
)

// JobReconcile is the reconcile job (every minute, plus the ingest wake debounced to one extra run per 2 s).
const JobReconcile = "sensors.reconcile"

// Alert actions (actOnSensorAlert).
const (
	ActionDismiss        = "dismiss"
	ActionConfirmArrival = "confirm_arrival"
	ActionConfirmRelease = "confirm_release"
	ActionRevokeRelease  = "revoke_release"
	ActionCreateTask     = "create_task"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB          *store.DB
	Clock       clock.Clock
	Audit       *audit.Writer
	Outbox      *event.Outbox
	Log         *slog.Logger
	Presence    *presence.Service
	Occupancy   *occupancy.Service
	Maintenance *maintenance.Service
	// Wake receives the ingest wake (sensor.Deps.Wake); the job runner reads it through Service.Wake.
	Wake <-chan struct{}
	// RawRetention is PORTO_SENSOR_RAW_RETENTION (raw readings in the history only inside it).
	RawRetention time.Duration
}

// Metrics are the reconcile counters (m12-plan §14.2 L6).
type Metrics struct {
	Runs     atomic.Int64
	RunNano  atomic.Int64 // total wall time
	MaxNano  atomic.Int64 // longest run
	Openings atomic.Int64
}

// MapEntry is one device of the Sensori context (m12-plan §9.2): no vessel or customer data.
type MapEntry struct {
	DeviceID, Code, ResourceType, ResourceID string
	FromCm, ToCm                             *int64
	XCm, YCm                                 int64
	Physical                                 string
	PhysicalSince                            *time.Time
	Health                                   string // "" for a past date
	Discrepancy                              bool
	Expectation                              string
	Informational                            bool
	OpenAlerts                               []string
}

// Map is the Sensori context of a map.
type Map struct {
	MapID       string
	Date        *calendar.Date // a past night
	GeneratedAt time.Time
	Entries     []MapEntry
}

// AlertFilter filters listSensorAlerts (Status "" = open).
type AlertFilter struct {
	Status, Kind, MapID, DeviceID string
}

// AlertView is an alert with its device and resource codes and the actions the principal may take.
type AlertView struct {
	store.SensorAlert
	DeviceCode     string
	ResourceCode   string
	AllowedActions []string
}

// ParsedRefs returns the refs of the alert (ids and enums only).
func (v AlertView) ParsedRefs() store.SensorAlertRefs { return parseRefs(v.Refs) }

// ActionInput decides an alert.
type ActionInput struct {
	Action           string
	Note             string
	From, To         *calendar.Date // confirm_release: inside the proposal
	ConsentConfirmed bool
	Priority         string // create_task
}

// HistoryQuery is a history request (range at most 31 days).
type HistoryQuery struct {
	ResourceType, ResourceID string
	DeviceID                 string // getSensorDeviceHistory
	From, To                 *calendar.Date
	IncludeReadings          bool
	Cursor                   string
}

// NightRow is the contractual and physical status of one night.
type NightRow struct {
	Night       calendar.Date
	Contractual string // free, contract, transit, released, released_resold, block, maintenance
	Physical    string
	Presence    string
	Discrepancy bool
}

// History is the physical history of a resource or device.
type History struct {
	ResourceType, ResourceID string
	DeviceIDs                []string
	From, To                 calendar.Date
	StateAtFrom              string
	Changes                  []store.SensorStateChange
	Alerts                   []AlertView
	Nights                   []NightRow
	Readings                 []store.SensorReading
	ReadingsFrom             *time.Time
	NextCursor               string
}
