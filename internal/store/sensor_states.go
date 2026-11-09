package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Device state rows and state changes of the M12 sensors (owned by INGEST, m12-plan §3.2, §4.4, §5.3). The rows are
// written only by sensor.Ingest, the silence job and device reassignment, under the row lock taken here.

// EnsureSensorDeviceStates creates the missing state rows (physical unknown) of the given devices of a marina.
func EnsureSensorDeviceStates(ctx context.Context, db bun.IDB, marinaID string, deviceIDs []string, now time.Time) error {
	if len(deviceIDs) == 0 {
		return nil
	}
	rows := make([]SensorDeviceState, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		rows = append(rows, SensorDeviceState{DeviceID: id, MarinaID: marinaID, Physical: SensorUnknown, UpdatedAt: UTC(now)})
	}
	for start := 0; start < len(rows); start += 500 {
		chunk := rows[start:min(start+500, len(rows))]
		if _, err := db.NewInsert().Model(&chunk).On("CONFLICT (device_id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// LockSensorDeviceStates locks the state rows of the given devices of a marina in ascending device id (PostgreSQL
// FOR UPDATE; SQLite has one writer) and returns them keyed by device id.
func LockSensorDeviceStates(ctx context.Context, db bun.IDB, marinaID string, deviceIDs []string) (map[string]*SensorDeviceState, error) {
	out := make(map[string]*SensorDeviceState, len(deviceIDs))
	for start := 0; start < len(deviceIDs); start += 500 {
		chunk := deviceIDs[start:min(start+500, len(deviceIDs))]
		var rows []SensorDeviceState
		q := db.NewSelect().Model(&rows).Where("marina_id = ?", marinaID).Where("device_id IN (?)", bun.List(chunk)).OrderExpr("device_id")
		if IsPostgres(db) {
			q = q.For("UPDATE")
		}
		if err := q.Scan(ctx); err != nil {
			return nil, err
		}
		for i := range rows {
			out[rows[i].DeviceID] = &rows[i]
		}
	}
	return out, nil
}

// SensorStatesOf returns the state rows of the marina's devices keyed by device id (lists and health).
func SensorStatesOf(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]*SensorDeviceState, error) {
	var rows []SensorDeviceState
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]*SensorDeviceState, len(rows))
	for i := range rows {
		out[rows[i].DeviceID] = &rows[i]
	}
	return out, nil
}

// MarinasWithKnownSensorStates returns the marinas with at least one device whose physical state is not unknown
// (the silence job visits only those).
func MarinasWithKnownSensorStates(ctx context.Context, db bun.IDB) ([]string, error) {
	var out []string
	err := db.NewSelect().Table("sensor_device_states").ColumnExpr("DISTINCT marina_id").
		Where("physical <> ?", SensorUnknown).OrderExpr("marina_id").Scan(ctx, &out)
	return out, err
}

// SilentSensorDevices returns at most limit devices of a marina whose physical state is not unknown and whose last
// observed reading is at or before cutoff, in ascending device id.
func SilentSensorDevices(ctx context.Context, db bun.IDB, marinaID string, cutoff time.Time, limit int) ([]string, error) {
	var out []string
	err := db.NewSelect().Table("sensor_device_states").Column("device_id").
		Where("marina_id = ?", marinaID).Where("physical <> ?", SensorUnknown).
		Where("last_observed_at IS NOT NULL").Where("last_observed_at <= ?", UTC(cutoff)).
		OrderExpr("device_id").Limit(int64(limit)).Scan(ctx, &out)
	return out, err
}

// ListSensorStateChangesOf returns the state changes of a device of the marina, oldest first.
func ListSensorStateChangesOf(ctx context.Context, db bun.IDB, s MarinaScope, deviceID string) ([]SensorStateChange, error) {
	out := []SensorStateChange{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("device_id = ?", deviceID).
		OrderExpr("at, created_at, id").Scan(ctx)
	return out, err
}

// SaveSensorSettings writes the sensor settings of a marina: an insert when expected is 0 (never saved; a
// concurrent first save is ErrVersionMismatch), otherwise an update when the stored version equals expected. It
// sets row.Version.
func SaveSensorSettings(ctx context.Context, db bun.IDB, row *SensorSettings, expected int) error {
	row.CreatedAt, row.UpdatedAt = UTC(row.CreatedAt), UTC(row.UpdatedAt)
	if expected == 0 {
		row.Version = 1
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrVersionMismatch
			}
			return err
		}
		return nil
	}
	res, err := db.NewUpdate().Model(row).ExcludeColumn("marina_id", "created_at", "version").Set("version = version + 1").
		Where("marina_id = ?", row.MarinaID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	row.Version = expected + 1
	return nil
}
