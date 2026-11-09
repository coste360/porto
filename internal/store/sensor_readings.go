package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Raw readings of the M12 sensors (owned by INGEST, m12-plan §4.4, §7). High volume: never audited, never evented,
// excluded from backup, purged after PORTO_SENSOR_RAW_RETENTION.

// SensorReadingKey identifies a stored reading.
type SensorReadingKey struct {
	DeviceID   string    `bun:"device_id"`
	ObservedAt time.Time `bun:"observed_at"`
}

// readingChunk bounds the rows of one INSERT (9 parameters each, far under the SQLite variable limit).
const readingChunk = 250

// InsertSensorReadings inserts rows with ON CONFLICT (device_id, observed_at) DO NOTHING and returns the keys of
// the rows actually inserted (the others are duplicates). ObservedAt must already be UTC truncated to ms.
func InsertSensorReadings(ctx context.Context, db bun.IDB, rows []SensorReading) ([]SensorReadingKey, error) {
	var out []SensorReadingKey
	for start := 0; start < len(rows); start += readingChunk {
		chunk := rows[start:min(start+readingChunk, len(rows))]
		var got []SensorReadingKey
		err := db.NewInsert().Model(&chunk).On("CONFLICT (device_id, observed_at) DO NOTHING").
			Returning("device_id, observed_at").Scan(ctx, &got)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// BulkInsertSensorReadings inserts rows without returning (load-test prefill and seeds), ignoring duplicates.
func BulkInsertSensorReadings(ctx context.Context, db bun.IDB, rows []SensorReading) error {
	for start := 0; start < len(rows); start += 500 {
		chunk := rows[start:min(start+500, len(rows))]
		if _, err := db.NewInsert().Model(&chunk).On("CONFLICT (device_id, observed_at) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSensorReadingsBefore deletes at most limit readings observed before cutoff, oldest first (one retention
// chunk), and returns how many were deleted.
func DeleteSensorReadingsBefore(ctx context.Context, db bun.IDB, cutoff time.Time, limit int) (int64, error) {
	rowRef := "rowid"
	if IsPostgres(db) {
		rowRef = "ctid"
	}
	res, err := db.NewRaw("DELETE FROM sensor_readings WHERE "+rowRef+" IN (SELECT "+rowRef+
		" FROM sensor_readings WHERE observed_at < ? ORDER BY observed_at LIMIT ?)", UTC(cutoff), limit).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// CountSensorReadings returns the number of readings of the marina (deviceID "" = every device).
func CountSensorReadings(ctx context.Context, db bun.IDB, s MarinaScope, deviceID string) (int, error) {
	q := db.NewSelect().Table("sensor_readings").Where("marina_id = ?", s.ID())
	if deviceID != "" {
		q = q.Where("device_id = ?", deviceID)
	}
	n, err := q.Count(ctx)
	return int(n), err
}

// ListSensorReadingsOf returns the readings of a device of the marina observed in [from, to), newest first, at most
// limit rows.
func ListSensorReadingsOf(ctx context.Context, db bun.IDB, s MarinaScope, deviceID string, from, to time.Time, limit int) ([]SensorReading, error) {
	out := []SensorReading{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("device_id = ?", deviceID).
		Where("observed_at >= ?", UTC(from)).Where("observed_at < ?", UTC(to)).OrderExpr("observed_at DESC").Limit(int64(limit)).Scan(ctx)
	return out, err
}
