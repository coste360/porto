package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read helpers of the M12 reconciliation, map, history and day-view facts (m12-plan §8.4, §9.2, §10; owned by
// RECONCILE). The reconcile snapshot is at most eight queries per marina: devices with states, pedestal berths,
// stays, releases, vessel presences, uncleared alerts, plus the settings rows. Read only: nothing here writes
// occupancy or contracts (INV-100).

// SensorWatchDevice is a device with its hot state.
type SensorWatchDevice struct {
	ID             string     `bun:"id"`
	Code           string     `bun:"code"`
	ResourceType   string     `bun:"resource_type"`
	ResourceID     string     `bun:"resource_id"`
	FromCm         *int64     `bun:"from_cm"`
	ToCm           *int64     `bun:"to_cm"`
	PedestalSocket string     `bun:"pedestal_socket"`
	MapID          string     `bun:"map_id"`
	XCm            int64      `bun:"x_cm"`
	YCm            int64      `bun:"y_cm"`
	Active         bool       `bun:"active"`
	CreatedAt      time.Time  `bun:"created_at"`
	Physical       string     `bun:"physical"`
	PhysicalSince  *time.Time `bun:"physical_since"`
	LastContactAt  *time.Time `bun:"last_contact_at"`
	LastObservedAt *time.Time `bun:"last_observed_at"`
	BatteryPct     *int       `bun:"battery_pct"`
}

// SensorWatchDeviceFilter selects devices (empty fields: every value).
type SensorWatchDeviceFilter struct {
	MapID        string
	DeviceIDs    []string
	ResourceType string
	ResourceID   string
	ActiveOnly   bool
}

// SensorWatchDevices returns the devices of the marina with their state (physical unknown without a state row),
// ordered by code.
func SensorWatchDevices(ctx context.Context, db bun.IDB, s MarinaScope, f SensorWatchDeviceFilter) ([]SensorWatchDevice, error) {
	var out []SensorWatchDevice
	q := db.NewSelect().TableExpr("sensor_devices AS d").
		ColumnExpr("d.id, d.code, d.resource_type, d.resource_id, d.from_cm, d.to_cm").
		ColumnExpr("COALESCE(d.pedestal_socket, '') AS pedestal_socket, d.map_id, d.x_cm, d.y_cm, d.active, d.created_at").
		ColumnExpr("COALESCE(st.physical, 'unknown') AS physical, st.physical_since, st.last_contact_at, st.last_observed_at, st.battery_pct").
		Join("LEFT JOIN sensor_device_states AS st ON st.device_id = d.id").
		Where("d.marina_id = ?", s.ID())
	if f.MapID != "" {
		q = q.Where("d.map_id = ?", f.MapID)
	}
	if f.DeviceIDs != nil {
		ids := validIDs(f.DeviceIDs)
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("d.id IN (?)", bun.List(ids))
	}
	if f.ResourceType != "" {
		q = q.Where("d.resource_type = ?", f.ResourceType)
	}
	if f.ResourceID != "" {
		q = q.Where("d.resource_id = ?", f.ResourceID)
	}
	if f.ActiveOnly {
		q = q.Where("d.active = ?", true)
	}
	if err := q.OrderExpr("d.code, d.id").Scan(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarinasWithActiveSensors returns the ids of the marinas with at least one active device.
func MarinasWithActiveSensors(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().TableExpr("sensor_devices AS d").ColumnExpr("DISTINCT d.marina_id").
		Join("JOIN marinas AS m ON m.id = d.marina_id").Where("d.active = ?", true).Where("m.archived_at IS NULL").
		OrderExpr("d.marina_id").Scan(ctx, &ids)
	return ids, err
}

// SensorPedestalBerth is an active berth served by a pedestal socket.
type SensorPedestalBerth struct {
	PedestalID string `bun:"pedestal_id"`
	Socket     string `bun:"pedestal_socket"`
	BerthID    string `bun:"berth_id"`
}

// SensorPedestalBerths returns the active berths served by the pedestals, ordered by pedestal and berth.
func SensorPedestalBerths(ctx context.Context, db bun.IDB, s MarinaScope, pedestalIDs []string) ([]SensorPedestalBerth, error) {
	ids := validIDs(pedestalIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorPedestalBerth
	err := db.NewSelect().TableExpr("berths AS b").
		ColumnExpr("b.pedestal_id, COALESCE(b.pedestal_socket, '') AS pedestal_socket, b.id AS berth_id").
		Where("b.marina_id = ?", s.ID()).Where("b.pedestal_id IN (?)", bun.List(ids)).Where("b.active = ?", true).
		OrderExpr("b.pedestal_id, b.id").Scan(ctx, &out)
	return out, err
}

// SensorStay is an active allocation of a watched resource with its contract (empty for blocks, maintenance and
// berth-right owners without a contract line).
type SensorStay struct {
	AllocationID   string        `bun:"allocation_id"`
	ResourceType   string        `bun:"resource_type"`
	ResourceID     string        `bun:"resource_id"`
	MapID          string        `bun:"map_id"`
	VesselID       string        `bun:"vessel_id"`
	Kind           string        `bun:"kind"`
	FirstNight     calendar.Date `bun:"first_night"`
	LastNight      calendar.Date `bun:"last_night"`
	XCm            *int64        `bun:"x_cm"`
	LoaCm          int64         `bun:"loa_cm"`
	ContractID     string        `bun:"contract_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractTerm   string        `bun:"contract_term"`
	ContractStatus string        `bun:"contract_status"`
}

// SensorStays returns the active allocations of the resources intersecting [from, to], ordered by resource, first
// night and id.
func SensorStays(ctx context.Context, db bun.IDB, s MarinaScope, resourceIDs []string, from, to calendar.Date) ([]SensorStay, error) {
	ids := validIDs(resourceIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorStay
	err := db.NewSelect().TableExpr("allocations AS a").
		ColumnExpr("a.id AS allocation_id, a.resource_type, a.resource_id, a.map_id, COALESCE(CAST(a.vessel_id AS TEXT), '') AS vessel_id").
		ColumnExpr("a.kind, a.first_night, a.last_night, a.x_cm, COALESCE(v.loa_cm, 0) AS loa_cm").
		ColumnExpr("COALESCE(CAST(c.id AS TEXT), '') AS contract_id, COALESCE(c.kind, '') AS contract_kind, COALESCE(c.term, '') AS contract_term").
		ColumnExpr("COALESCE(c.status, '') AS contract_status").
		Join("LEFT JOIN contract_lines AS cl ON cl.id = a.contract_line_id").
		Join("LEFT JOIN contracts AS c ON c.id = cl.contract_id").
		Join("LEFT JOIN vessels AS v ON v.id = a.vessel_id").
		Where("a.marina_id = ?", s.ID()).Where("a.resource_id IN (?)", bun.List(ids)).Where("a.status = ?", AllocActive).
		Where("a.first_night <= ?", to).Where("a.last_night >= ?", from).
		OrderExpr("a.resource_id, a.first_night, a.id").Scan(ctx, &out)
	return out, err
}

// SensorRelease is an active release with the status of its resale (contract status of the resold allocation).
type SensorRelease struct {
	ID                 string        `bun:"id"`
	AllocationID       string        `bun:"allocation_id"`
	ResourceID         string        `bun:"resource_id"`
	FromNight          calendar.Date `bun:"from_night"`
	ToNight            calendar.Date `bun:"to_night"`
	ResoldAllocationID string        `bun:"resold_allocation_id"`
	ResaleStatus       string        `bun:"resale_status"`
	Version            int           `bun:"version"`
}

// SensorReleases returns the active releases of the resources intersecting [from, to].
func SensorReleases(ctx context.Context, db bun.IDB, s MarinaScope, resourceIDs []string, from, to calendar.Date) ([]SensorRelease, error) {
	ids := validIDs(resourceIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorRelease
	err := db.NewSelect().TableExpr("releases AS r").
		ColumnExpr("r.id, r.allocation_id, r.resource_id, r.from_night, r.to_night, r.version").
		ColumnExpr("COALESCE(CAST(r.resold_allocation_id AS TEXT), '') AS resold_allocation_id, COALESCE(c.status, '') AS resale_status").
		Join("LEFT JOIN allocations AS ra ON ra.id = r.resold_allocation_id").
		Join("LEFT JOIN contract_lines AS cl ON cl.id = ra.contract_line_id").
		Join("LEFT JOIN contracts AS c ON c.id = cl.contract_id").
		Where("r.marina_id = ?", s.ID()).Where("r.resource_id IN (?)", bun.List(ids)).Where("r.status = ?", ReleaseActive).
		Where("r.from_night <= ?", to).Where("r.to_night >= ?", from).
		OrderExpr("r.resource_id, r.from_night, r.id").Scan(ctx, &out)
	return out, err
}

// SensorVesselPresence is the current presence of a vessel with the source of its last presence.
type SensorVesselPresence struct {
	VesselID         string        `bun:"vessel_id"`
	State            string        `bun:"state"`
	Since            time.Time     `bun:"since"`
	LastPresenceID   string        `bun:"last_presence_id"`
	LastSource       string        `bun:"last_source"`
	ExpectedReturnOn calendar.Date `bun:"expected_return_on,nullzero"`
}

// SensorVesselPresences returns the presences of the vessels keyed by vessel id.
func SensorVesselPresences(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string) (map[string]SensorVesselPresence, error) {
	ids := validIDs(vesselIDs)
	out := map[string]SensorVesselPresence{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []SensorVesselPresence
	err := db.NewSelect().TableExpr("vessel_presence AS vp").
		ColumnExpr("vp.vessel_id, vp.state, vp.since, vp.last_presence_id, COALESCE(p.source, '') AS last_source, vp.expected_return_on").
		Join("LEFT JOIN presences AS p ON p.id = vp.last_presence_id").
		Where("vp.marina_id = ?", s.ID()).Where("vp.vessel_id IN (?)", bun.List(ids)).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.VesselID] = r
	}
	return out, nil
}

// SensorPresenceEvent is a presence of a vessel (history: state at 02:00 of a night).
type SensorPresenceEvent struct {
	VesselID string    `bun:"vessel_id"`
	Event    string    `bun:"event"`
	At       time.Time `bun:"at"`
}

// SensorPresenceEvents returns the presences of the vessels before until, ordered by vessel and instant.
func SensorPresenceEvents(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string, until time.Time) ([]SensorPresenceEvent, error) {
	ids := validIDs(vesselIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorPresenceEvent
	err := db.NewSelect().TableExpr("presences AS p").ColumnExpr("p.vessel_id, p.event, p.at").
		Where("p.marina_id = ?", s.ID()).Where("p.vessel_id IN (?)", bun.List(ids)).Where("p.at < ?", UTC(until)).
		OrderExpr("p.vessel_id, p.at, p.id").Scan(ctx, &out)
	return out, err
}

// SensorStateChangesOf returns the state changes of the devices in [from, to), ordered by device, instant and id.
func SensorStateChangesOf(ctx context.Context, db bun.IDB, s MarinaScope, deviceIDs []string, from, to time.Time) ([]SensorStateChange, error) {
	ids := validIDs(deviceIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorStateChange
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("device_id IN (?)", bun.List(ids)).
		Where("at >= ?", UTC(from)).Where("at < ?", UTC(to)).OrderExpr("device_id, at, created_at, id").Scan(ctx)
	return out, err
}

// SensorStateChangesOnResource returns the state changes recorded on a resource in [from, to) (any device),
// ordered by instant.
func SensorStateChangesOnResource(ctx context.Context, db bun.IDB, s MarinaScope, resourceID string, from, to time.Time) ([]SensorStateChange, error) {
	var out []SensorStateChange
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("resource_id = ?", resourceID).
		Where("at >= ?", UTC(from)).Where("at < ?", UTC(to)).OrderExpr("at, created_at, id").Scan(ctx)
	return out, err
}

// SensorDevicesOnResource returns the ids of the devices with a state change on the resource before to, or
// currently assigned to it.
func SensorDevicesOnResource(ctx context.Context, db bun.IDB, s MarinaScope, resourceID string, to time.Time) ([]string, error) {
	var ids []string
	err := db.NewRaw(`SELECT DISTINCT device_id FROM (
			SELECT device_id FROM sensor_state_changes WHERE marina_id = ? AND resource_id = ? AND at < ?
			UNION SELECT id AS device_id FROM sensor_devices WHERE marina_id = ? AND resource_id = ?
		) AS x ORDER BY device_id`, s.ID(), resourceID, UTC(to), s.ID(), resourceID).Scan(ctx, &ids)
	return ids, err
}

// SensorStatesBefore returns, per device, the to_state of its latest state change strictly before t (devices
// without one are absent: the state was unknown).
func SensorStatesBefore(ctx context.Context, db bun.IDB, s MarinaScope, deviceIDs []string, t time.Time) (map[string]string, error) {
	ids := validIDs(deviceIDs)
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		DeviceID string `bun:"device_id"`
		ToState  string `bun:"to_state"`
	}
	var rows []row
	err := db.NewRaw(`SELECT c.device_id, c.to_state FROM sensor_state_changes AS c
		WHERE c.marina_id = ? AND c.device_id IN (?) AND c.at < ?
		  AND c.at = (SELECT MAX(c2.at) FROM sensor_state_changes AS c2 WHERE c2.device_id = c.device_id AND c2.at < ?)
		ORDER BY c.device_id, c.created_at, c.id`, s.ID(), bun.List(ids), UTC(t), UTC(t)).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DeviceID] = r.ToState // the last row of a tie wins (ordered by created_at, id)
	}
	return out, nil
}

// SensorReadingsOf returns raw readings of the devices in [from, to), newest first, at most limit, after the
// cursor (observed_at|device_id of the last row).
func SensorReadingsOf(ctx context.Context, db bun.IDB, s MarinaScope, deviceIDs []string, from, to time.Time, before *time.Time, limit int) ([]SensorReading, error) {
	ids := validIDs(deviceIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorReading
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("device_id IN (?)", bun.List(ids)).
		Where("observed_at >= ?", UTC(from)).Where("observed_at < ?", UTC(to))
	if before != nil {
		q = q.Where("observed_at < ?", UTC(*before))
	}
	err := q.OrderExpr("observed_at DESC, device_id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// SensorResourceCodesOf returns the codes of the given berths, dynamic areas and pedestals keyed by id.
func SensorResourceCodesOf(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	ids = validIDs(ids)
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	var rows []row
	err := db.NewRaw(`SELECT id, code FROM berths WHERE marina_id = ? AND id IN (?)
		UNION ALL SELECT id, code FROM dynamic_areas WHERE marina_id = ? AND id IN (?)
		UNION ALL SELECT id, code FROM pedestals WHERE marina_id = ? AND id IN (?)`,
		s.ID(), bun.List(ids), s.ID(), bun.List(ids), s.ID(), bun.List(ids)).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}

// SensorDeviceCodes returns the codes of devices keyed by id.
func SensorDeviceCodes(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	ids = validIDs(ids)
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	var rows []row
	if err := db.NewSelect().Table("sensor_devices").Column("id", "code").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}

// FirstWaterMap returns the id of the first map of the marina by sort order (default map of the Sensori context).
func FirstWaterMap(ctx context.Context, db bun.IDB, s MarinaScope) (string, error) {
	var ids []string
	err := db.NewSelect().Table("maps").Column("id").Where("marina_id = ?", s.ID()).
		OrderExpr("CASE WHEN kind = 'water' THEN 0 ELSE 1 END, sort_order, id").Limit(1).Scan(ctx, &ids)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}
	return ids[0], nil
}

// LockSensorSettings upserts the sensor_settings row of the marina with its defaults when missing and locks it
// (PostgreSQL FOR UPDATE; SQLite has one writer): the reconcile job serialises on it (m12-plan §8.4).
func LockSensorSettings(ctx context.Context, tx bun.IDB, s MarinaScope, now time.Time) (*SensorSettings, error) {
	def := DefaultSensorSettings(s.ID())
	def.Version, def.CreatedAt, def.UpdatedAt = 1, UTC(now), UTC(now)
	if _, err := tx.NewInsert().Model(def).On("CONFLICT (marina_id) DO NOTHING").Exec(ctx); err != nil {
		return nil, err
	}
	row := new(SensorSettings)
	q := tx.NewSelect().Model(row).Where("marina_id = ?", s.ID())
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	return row, nil
}
