package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Gateway writes and reads of the M12 sensor service (owned by INGEST, m12-plan §3.2).

// ListSensorGateways returns the gateways of the marina by code.
func ListSensorGateways(ctx context.Context, db bun.IDB, s MarinaScope) ([]SensorGateway, error) {
	out := []SensorGateway{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("code, id").Scan(ctx)
	return out, err
}

// GetSensorGateway returns a gateway of the marina.
func GetSensorGateway(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorGateway, error) {
	return getSensorRow[SensorGateway](ctx, db, s, id, false)
}

// LockSensorGateway locks a gateway row (PostgreSQL FOR UPDATE) and returns it.
func LockSensorGateway(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SensorGateway, error) {
	return getSensorRow[SensorGateway](ctx, db, s, id, true)
}

// InsertSensorGateway inserts a gateway (a unique violation is a taken code).
func InsertSensorGateway(ctx context.Context, db bun.IDB, g *SensorGateway) error {
	g.CreatedAt, g.UpdatedAt = UTC(g.CreatedAt), UTC(g.UpdatedAt)
	_, err := db.NewInsert().Model(g).Exec(ctx)
	return err
}

// UpdateSensorGateway writes every column of g when its version equals expected and sets g.Version.
func UpdateSensorGateway(ctx context.Context, db bun.IDB, s MarinaScope, g *SensorGateway, expected int) error {
	g.UpdatedAt = UTC(g.UpdatedAt)
	res, err := db.NewUpdate().Model(g).ExcludeColumn("id", "marina_id", "created_at", "version", "last_contact_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", g.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetSensorGateway(ctx, db, s, g.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	g.Version = expected + 1
	return nil
}

// SensorGatewayByKeyID returns the gateway whose current or previous key id is keyID, in any marina.
func SensorGatewayByKeyID(ctx context.Context, db bun.IDB, keyID string) (*SensorGateway, error) {
	if keyID == "" {
		return nil, ErrNotFound
	}
	g := new(SensorGateway)
	if err := db.NewSelect().Model(g).Where("key_id = ? OR prev_key_id = ?", keyID, keyID).Limit(1).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return g, nil
}

// TouchSensorGateway records the last contact of a gateway (no version bump: health, not configuration).
func TouchSensorGateway(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("sensor_gateways").Set("last_contact_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}
