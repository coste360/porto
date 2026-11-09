package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Row types of the M12 sensor tables (internal/migrations/*/00026_sensors.sql, m12-plan §3, §5, §6, §8.3, §15) and
// the frozen helpers shared by the INGEST, RECONCILE and PUBLIC builders: GetSensorDevice, LockSensorDevice,
// GetSensorAlert, LockSensorAlert, SensorExists, LoadSensorSettings, GetSensorDeviceState,
// InsertSensorStateChange, UpsertSensorDeviceState and SensorPhysicalByBerth. INGEST owns the device, gateway,
// source, reading and state writes (sensor_devices.go, sensor_gateways.go, sensor_sources.go, sensor_readings.go,
// sensor_states.go); RECONCILE owns the alert writes (sensor_alerts.go). No sensor code path writes allocations,
// occupancy rows, releases, contracts or charges (INV-100).

// Sensor enums (CHECKs of 00026).
const (
	SensorKindUltrasonic   = "ultrasonic"
	SensorKindRadar        = "radar"
	SensorKindMagnetometer = "magnetometer"
	SensorKindInfrared     = "infrared"
	SensorKindCamera       = "camera"
	SensorKindOther        = "other"

	SensorResourceBerth       = "berth"
	SensorResourceDynamicArea = "dynamic_area"
	SensorResourcePedestal    = "pedestal"

	SensorOccupied = "occupied"
	SensorFree     = "free"
	SensorUnknown  = "unknown"

	SensorReasonReadings    = "readings"
	SensorReasonDuration    = "duration"
	SensorReasonSilence     = "silence"
	SensorReasonDeviceMoved = "device_moved"

	SensorOriginDevice  = "device"
	SensorOriginGateway = "gateway"
	SensorOriginSource  = "source"

	SensorGatewayLoRaWAN    = "lorawan"
	SensorGatewayNBIoT      = "nbiot"
	SensorGatewayMQTTBridge = "mqtt_bridge"
	SensorGatewayOther      = "other"

	SensorSourceErrTimeout     = "timeout"
	SensorSourceErrAuth        = "auth"
	SensorSourceErrBadResponse = "bad_response"
	SensorSourceErrUnavailable = "unavailable"

	SensorAlertUnexpectedOccupied = "unexpected_occupied"
	SensorAlertTransitArrival     = "transit_arrival"
	SensorAlertReturnDetected     = "return_detected"
	SensorAlertReleaseProposal    = "release_proposal"
	SensorAlertEarlyReturn        = "early_return"
	SensorAlertDeviceSilent       = "device_silent"
	SensorAlertBatteryLow         = "battery_low"

	SensorAlertOpen      = "open"
	SensorAlertConfirmed = "confirmed"
	SensorAlertDismissed = "dismissed"
	SensorAlertResolved  = "resolved"
)

// Key prefixes (m12-plan §3.3): the public key id is the prefix plus 16 base32 characters, the full key adds "_"
// and the 43-character secret.
const (
	SensorDeviceKeyPrefix  = "po_sns_"
	SensorGatewayKeyPrefix = "po_gw_"
)

// SensorSettings is the row of sensor_settings (one per marina, m12-plan §6). LoadSensorSettings returns the
// defaults (Version 0) when the marina never saved them.
type SensorSettings struct {
	bun.BaseModel `bun:"table:sensor_settings"`

	MarinaID                 string    `bun:"marina_id,pk"`
	HysteresisReadings       int       `bun:"hysteresis_readings"`
	HysteresisMinutes        int       `bun:"hysteresis_minutes"`
	OfflineAfterMinutes      int       `bun:"offline_after_minutes"`
	SilentAlertMinutes       int       `bun:"silent_alert_minutes"`
	BatteryLowPct            int       `bun:"battery_low_pct"`
	UnexpectedAfterMinutes   int       `bun:"unexpected_after_minutes"`
	DepartureCutoffMinutes   int       `bun:"departure_cutoff_minutes"`
	PresenceEffectEnabled    bool      `bun:"presence_effect_enabled"`
	PresenceOutAfterHours    int       `bun:"presence_out_after_hours"`
	ReleaseProposalAfterDays int       `bun:"release_proposal_after_days"`
	ReleaseProposalNights    int       `bun:"release_proposal_nights"`
	Version                  int       `bun:"version"`
	CreatedAt                time.Time `bun:"created_at"`
	UpdatedAt                time.Time `bun:"updated_at"`
}

// DefaultSensorSettings returns the defaults of m12-plan §6 (Version 0: never saved).
func DefaultSensorSettings(marinaID string) *SensorSettings {
	return &SensorSettings{
		MarinaID: marinaID, HysteresisReadings: 2, HysteresisMinutes: 5, OfflineAfterMinutes: 30, SilentAlertMinutes: 180,
		BatteryLowPct: 20, UnexpectedAfterMinutes: 15, DepartureCutoffMinutes: 720, PresenceOutAfterHours: 6,
		ReleaseProposalAfterDays: 3, ReleaseProposalNights: 7,
	}
}

// SensorGateway is a row of sensor_gateways.
type SensorGateway struct {
	bun.BaseModel `bun:"table:sensor_gateways"`

	ID                 string     `bun:"id,pk"`
	MarinaID           string     `bun:"marina_id"`
	Code               string     `bun:"code"`
	Name               string     `bun:"name"`
	Kind               string     `bun:"kind"`
	KeyID              string     `bun:"key_id,nullzero"`
	KeyHash            string     `bun:"key_hash,nullzero"`
	KeyVerifierEnc     string     `bun:"key_verifier_enc,nullzero"`
	KeyIssuedAt        *time.Time `bun:"key_issued_at"`
	PrevKeyID          string     `bun:"prev_key_id,nullzero"`
	PrevKeyHash        string     `bun:"prev_key_hash,nullzero"`
	PrevKeyVerifierEnc string     `bun:"prev_key_verifier_enc,nullzero"`
	PrevValidUntil     *time.Time `bun:"prev_valid_until"`
	LastContactAt      *time.Time `bun:"last_contact_at"`
	Active             bool       `bun:"active"`
	Version            int        `bun:"version"`
	CreatedAt          time.Time  `bun:"created_at"`
	UpdatedAt          time.Time  `bun:"updated_at"`
}

// SensorSource is a row of sensor_sources (ConfigEnc: KeyRing, AAD "sensor-source:<id>", never returned).
type SensorSource struct {
	bun.BaseModel `bun:"table:sensor_sources"`

	ID            string     `bun:"id,pk"`
	MarinaID      string     `bun:"marina_id"`
	Code          string     `bun:"code"`
	Name          string     `bun:"name"`
	ConfigEnc     string     `bun:"config_enc,nullzero"`
	PollSeconds   int        `bun:"poll_seconds"`
	Cursor        string     `bun:"cursor"`
	Enabled       bool       `bun:"enabled"`
	LastPolledAt  *time.Time `bun:"last_polled_at"`
	LastErrorCode string     `bun:"last_error_code,nullzero"`
	Version       int        `bun:"version"`
	CreatedAt     time.Time  `bun:"created_at"`
	UpdatedAt     time.Time  `bun:"updated_at"`
}

// SensorDevice is a row of sensor_devices (m12-plan §3.1). FromCm/ToCm are set iff ResourceType is
// dynamic_area; PedestalSocket only with pedestal; at most one of GatewayID and SourceID.
type SensorDevice struct {
	bun.BaseModel `bun:"table:sensor_devices"`

	ID                 string     `bun:"id,pk"`
	MarinaID           string     `bun:"marina_id"`
	ExternalID         string     `bun:"external_id"`
	Code               string     `bun:"code"`
	Kind               string     `bun:"kind"`
	Model              string     `bun:"model"`
	Firmware           string     `bun:"firmware"`
	ResourceType       string     `bun:"resource_type"`
	ResourceID         string     `bun:"resource_id"`
	FromCm             *int64     `bun:"from_cm"`
	ToCm               *int64     `bun:"to_cm"`
	PedestalSocket     string     `bun:"pedestal_socket,nullzero"`
	MapID              string     `bun:"map_id"`
	XCm                int64      `bun:"x_cm"`
	YCm                int64      `bun:"y_cm"`
	GatewayID          string     `bun:"gateway_id,nullzero"`
	SourceID           string     `bun:"source_id,nullzero"`
	KeyID              string     `bun:"key_id,nullzero"`
	KeyHash            string     `bun:"key_hash,nullzero"`
	KeyVerifierEnc     string     `bun:"key_verifier_enc,nullzero"`
	KeyIssuedAt        *time.Time `bun:"key_issued_at"`
	PrevKeyID          string     `bun:"prev_key_id,nullzero"`
	PrevKeyHash        string     `bun:"prev_key_hash,nullzero"`
	PrevKeyVerifierEnc string     `bun:"prev_key_verifier_enc,nullzero"`
	PrevValidUntil     *time.Time `bun:"prev_valid_until"`
	Active             bool       `bun:"active"`
	Version            int        `bun:"version"`
	CreatedAt          time.Time  `bun:"created_at"`
	UpdatedAt          time.Time  `bun:"updated_at"`
}

// SensorDeviceState is the hot per-device row of sensor_device_states (m12-plan §3.2), written only by
// sensor.Ingest, the silence job and device reassignment.
type SensorDeviceState struct {
	bun.BaseModel `bun:"table:sensor_device_states"`

	DeviceID       string     `bun:"device_id,pk"`
	MarinaID       string     `bun:"marina_id"`
	Physical       string     `bun:"physical"`
	PhysicalSince  *time.Time `bun:"physical_since"`
	Candidate      string     `bun:"candidate,nullzero"`
	CandidateSince *time.Time `bun:"candidate_since"`
	CandidateCount int        `bun:"candidate_count"`
	LastObservedAt *time.Time `bun:"last_observed_at"`
	LastContactAt  *time.Time `bun:"last_contact_at"`
	BatteryPct     *int       `bun:"battery_pct"`
	RSSI           *int       `bun:"rssi"`
	DistanceCm     *int64     `bun:"distance_cm"`
	Firmware       string     `bun:"firmware"`
	UpdatedAt      time.Time  `bun:"updated_at"`
}

// SensorReading is a row of sensor_readings (PK device_id, observed_at; ObservedAt UTC truncated to ms).
type SensorReading struct {
	bun.BaseModel `bun:"table:sensor_readings"`

	DeviceID   string    `bun:"device_id,pk"`
	ObservedAt time.Time `bun:"observed_at,pk"`
	MarinaID   string    `bun:"marina_id"`
	ReceivedAt time.Time `bun:"received_at"`
	State      string    `bun:"state"`
	DistanceCm *int64    `bun:"distance_cm"`
	BatteryPct *int      `bun:"battery_pct"`
	RSSI       *int      `bun:"rssi"`
	Origin     string    `bun:"origin"`
}

// SensorStateChange is a row of sensor_state_changes (append-only history, m12-plan §4.4, §5).
type SensorStateChange struct {
	bun.BaseModel `bun:"table:sensor_state_changes"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	DeviceID     string    `bun:"device_id"`
	ResourceType string    `bun:"resource_type"`
	ResourceID   string    `bun:"resource_id"`
	MapID        string    `bun:"map_id"`
	FromCm       *int64    `bun:"from_cm"`
	ToCm         *int64    `bun:"to_cm"`
	FromState    string    `bun:"from_state"`
	ToState      string    `bun:"to_state"`
	At           time.Time `bun:"at"`
	Reason       string    `bun:"reason"`
	CreatedAt    time.Time `bun:"created_at"`
}

// SensorAlertRefs is the refs JSON of an alert (ids and enums only, m12-plan §8.3).
type SensorAlertRefs struct {
	AllocationID  string     `json:"allocation_id,omitempty"`
	ContractID    string     `json:"contract_id,omitempty"`
	VesselID      string     `json:"vessel_id,omitempty"`
	ReleaseID     string     `json:"release_id,omitempty"`
	PresenceID    string     `json:"presence_id,omitempty"`
	Resold        *bool      `json:"resold,omitempty"`
	ProposedFrom  string     `json:"proposed_from,omitempty"`
	ProposedTo    string     `json:"proposed_to,omitempty"`
	PhysicalSince *time.Time `json:"physical_since,omitempty"`
}

// SensorAlert is a row of sensor_alerts (m12-plan §8.3). One open alert per (device, kind) (INV-104).
type SensorAlert struct {
	bun.BaseModel `bun:"table:sensor_alerts"`

	ID              string     `bun:"id,pk"`
	MarinaID        string     `bun:"marina_id"`
	Kind            string     `bun:"kind"`
	DeviceID        string     `bun:"device_id"`
	ResourceType    string     `bun:"resource_type"`
	ResourceID      string     `bun:"resource_id"`
	MapID           string     `bun:"map_id"`
	ConditionKey    string     `bun:"condition_key"`
	Refs            JSON       `bun:"refs"` // SensorAlertRefs
	Status          string     `bun:"status"`
	OpenedAt        time.Time  `bun:"opened_at"`
	ClearedAt       *time.Time `bun:"cleared_at"`
	DecidedAt       *time.Time `bun:"decided_at"`
	DecidedByUserID string     `bun:"decided_by_user_id,nullzero"`
	OutcomeRef      string     `bun:"outcome_ref,nullzero"`
	Note            string     `bun:"note"`
	Version         int        `bun:"version"`
	CreatedAt       time.Time  `bun:"created_at"`
	UpdatedAt       time.Time  `bun:"updated_at"`
}

// Sensor tables addressable by id (SensorExists).
const (
	SensorTableDevices  = "sensor_devices"
	SensorTableGateways = "sensor_gateways"
	SensorTableSources  = "sensor_sources"
	SensorTableAlerts   = "sensor_alerts"
)

var sensorTables = map[string]bool{
	SensorTableDevices: true, SensorTableGateways: true, SensorTableSources: true, SensorTableAlerts: true,
}

// SensorExists returns ErrNotFound unless table (one of the SensorTable* constants) has a row id of the marina.
// The services use it to answer 404 for another marina's ids before anything else.
func SensorExists(ctx context.Context, db bun.IDB, s MarinaScope, table, id string) error {
	if !sensorTables[table] || !ValidID(id) {
		return ErrNotFound
	}
	n, err := db.NewSelect().Table(table).Where("marina_id = ?", s.ID()).Where("id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func getSensorRow[T any](ctx context.Context, db bun.IDB, s MarinaScope, id string, lock bool) (*T, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(T)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// GetSensorDevice returns a device of the marina.
func GetSensorDevice(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorDevice, error) {
	return getSensorRow[SensorDevice](ctx, db, s, id, false)
}

// LockSensorDevice locks a device row (PostgreSQL FOR UPDATE; SQLite has one writer) and returns it.
func LockSensorDevice(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorDevice, error) {
	return getSensorRow[SensorDevice](ctx, db, s, id, true)
}

// GetSensorAlert returns an alert of the marina.
func GetSensorAlert(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorAlert, error) {
	return getSensorRow[SensorAlert](ctx, db, s, id, false)
}

// LockSensorAlert locks an alert row and returns it (alert actions: lock the alert, then the target).
func LockSensorAlert(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorAlert, error) {
	return getSensorRow[SensorAlert](ctx, db, s, id, true)
}

// LoadSensorSettings returns the sensor settings of the marina, or the defaults (Version 0) when never saved.
func LoadSensorSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*SensorSettings, error) {
	row := new(SensorSettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultSensorSettings(s.ID()), nil
		}
		return nil, err
	}
	return row, nil
}

// GetSensorDeviceState returns the state row of a device of the marina (ErrNotFound when the device never had
// one: the physical state is then unknown).
func GetSensorDeviceState(ctx context.Context, db bun.IDB, s MarinaScope, deviceID string) (*SensorDeviceState, error) {
	if !ValidID(deviceID) {
		return nil, ErrNotFound
	}
	row := new(SensorDeviceState)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("device_id = ?", deviceID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertSensorStateChange appends a confirmed physical change. The caller writes the sensor.state_changed outbox
// event in the same transaction (m12-plan §4.4 step 5).
func InsertSensorStateChange(ctx context.Context, db bun.IDB, c *SensorStateChange) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	c.At, c.CreatedAt = UTC(c.At), UTC(c.CreatedAt)
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// UpsertSensorDeviceState inserts or replaces the state row of a device (every column). The caller holds the row
// lock (or is the SQLite writer).
func UpsertSensorDeviceState(ctx context.Context, db bun.IDB, st *SensorDeviceState) error {
	st.UpdatedAt = UTC(st.UpdatedAt)
	_, err := db.NewInsert().Model(st).
		On("CONFLICT (device_id) DO UPDATE").
		Set("physical = EXCLUDED.physical").
		Set("physical_since = EXCLUDED.physical_since").
		Set("candidate = EXCLUDED.candidate").
		Set("candidate_since = EXCLUDED.candidate_since").
		Set("candidate_count = EXCLUDED.candidate_count").
		Set("last_observed_at = EXCLUDED.last_observed_at").
		Set("last_contact_at = EXCLUDED.last_contact_at").
		Set("battery_pct = EXCLUDED.battery_pct").
		Set("rssi = EXCLUDED.rssi").
		Set("distance_cm = EXCLUDED.distance_cm").
		Set("firmware = EXCLUDED.firmware").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// sensorPhysicalRow is an active device of a berth or a pedestal with its physical state.
type sensorPhysicalRow struct {
	ResourceType string `bun:"resource_type"`
	ResourceID   string `bun:"resource_id"`
	Socket       string `bun:"pedestal_socket"`
	Physical     string `bun:"physical"`
}

// SensorPhysicalByBerth returns the physical state of the active sensors watching each berth, keyed by berth id:
// devices on the berth and pedestal devices that map to exactly one berth (the socket's berth, or the only berth
// of the pedestal, the rule of the reconciliation targets, m12-plan §8.1). Informational pedestal devices (no
// single berth) never count. When two devices watch one berth, occupied wins. PUBLIC uses this for the aggregate
// occupancy (m12-plan §11, D20).
func SensorPhysicalByBerth(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	var rows []sensorPhysicalRow
	err := db.NewSelect().TableExpr("sensor_devices AS d").
		ColumnExpr("d.resource_type, d.resource_id").
		ColumnExpr("COALESCE(d.pedestal_socket, '') AS pedestal_socket").
		ColumnExpr("COALESCE(st.physical, 'unknown') AS physical").
		Join("LEFT JOIN sensor_device_states AS st ON st.device_id = d.id").
		Where("d.marina_id = ?", s.ID()).
		Where("d.resource_type IN (?)", bun.List([]string{SensorResourceBerth, SensorResourcePedestal})).
		Where("d.active = ?", true).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	set := func(berthID, physical string) {
		if out[berthID] != SensorOccupied {
			out[berthID] = physical
		}
	}
	var pedestals []string
	for _, r := range rows {
		if r.ResourceType == SensorResourcePedestal {
			pedestals = append(pedestals, r.ResourceID)
			continue
		}
		set(r.ResourceID, r.Physical)
	}
	if len(pedestals) == 0 {
		return out, nil
	}
	berths, err := SensorPedestalBerths(ctx, db, s, pedestals)
	if err != nil {
		return nil, err
	}
	byPedestal := map[string][]SensorPedestalBerth{}
	for _, b := range berths {
		byPedestal[b.PedestalID] = append(byPedestal[b.PedestalID], b)
	}
	for _, r := range rows {
		if r.ResourceType != SensorResourcePedestal {
			continue
		}
		var match []string
		for _, b := range byPedestal[r.ResourceID] {
			if r.Socket == "" || b.Socket == r.Socket {
				match = append(match, b.BerthID)
			}
		}
		if len(match) == 1 {
			set(match[0], r.Physical)
		}
	}
	return out, nil
}
