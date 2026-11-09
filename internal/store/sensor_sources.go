package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Polled source writes and reads of the M12 sensor service (owned by INGEST, m12-plan §3.2, §3.4).

// ListSensorSources returns the sources of the marina by name.
func ListSensorSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]SensorSource, error) {
	out := []SensorSource{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("name, id").Scan(ctx)
	return out, err
}

// GetSensorSource returns a source of the marina.
func GetSensorSource(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorSource, error) {
	return getSensorRow[SensorSource](ctx, db, s, id, false)
}

// LockSensorSource locks a source row (PostgreSQL FOR UPDATE) and returns it.
func LockSensorSource(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorSource, error) {
	return getSensorRow[SensorSource](ctx, db, s, id, true)
}

// InsertSensorSource inserts a source (a unique violation is a taken name).
func InsertSensorSource(ctx context.Context, db bun.IDB, src *SensorSource) error {
	src.CreatedAt, src.UpdatedAt = UTC(src.CreatedAt), UTC(src.UpdatedAt)
	_, err := db.NewInsert().Model(src).Exec(ctx)
	return err
}

// UpdateSensorSource writes the configuration columns of src when its version equals expected.
func UpdateSensorSource(ctx context.Context, db bun.IDB, s MarinaScope, src *SensorSource, expected int) error {
	src.UpdatedAt = UTC(src.UpdatedAt)
	res, err := db.NewUpdate().Model(src).Column("name", "config_enc", "poll_seconds", "enabled", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", src.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetSensorSource(ctx, db, s, src.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	src.Version = expected + 1
	return nil
}

// SetSensorSourcePoll stores the outcome of a poll: the next cursor (unchanged on error), the poll instant and
// the error class ("" = none). It runs in the transaction of the readings so a crash re-polls the same cursor.
func SetSensorSourcePoll(ctx context.Context, db bun.IDB, s MarinaScope, id, cursor string, at time.Time, errCode string) error {
	q := db.NewUpdate().Table("sensor_sources").Set("last_polled_at = ?", UTC(at)).Set("cursor = ?", cursor)
	if errCode == "" {
		q = q.Set("last_error_code = NULL")
	} else {
		q = q.Set("last_error_code = ?", errCode)
	}
	_, err := q.Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// DueSensorSource is an enabled source with its marina.
type DueSensorSource struct {
	ID           string     `bun:"id"`
	MarinaID     string     `bun:"marina_id"`
	PollSeconds  int        `bun:"poll_seconds"`
	LastPolledAt *time.Time `bun:"last_polled_at"`
}

// EnabledSensorSources returns every enabled source of every marina (the poll job filters the due ones).
func EnabledSensorSources(ctx context.Context, db bun.IDB) ([]DueSensorSource, error) {
	var out []DueSensorSource
	err := db.NewSelect().Table("sensor_sources").Column("id", "marina_id", "poll_seconds", "last_polled_at").
		Where("enabled = ?", true).OrderExpr("marina_id, id").Scan(ctx, &out)
	return out, err
}
