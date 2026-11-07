package store

import (
	"context"
	"errors"

	"github.com/uptrace/bun"
)

// Queries of occupancy_settings (one row per marina, created lazily with the defaults of the migration).

// GetOccupancySettings returns the settings of a marina, or ErrNotFound when none were stored yet.
func GetOccupancySettings(ctx context.Context, db bun.IDB, s MarinaScope) (*OccupancySettings, error) {
	o := new(OccupancySettings)
	if err := db.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// InsertOccupancySettings inserts the settings row of a marina; a row already present is ErrConflict.
func InsertOccupancySettings(ctx context.Context, tx bun.IDB, s MarinaScope, o *OccupancySettings) error {
	o.MarinaID = s.ID()
	_, err := tx.NewInsert().Model(o).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateOccupancySettings writes the settings when the stored version equals expected and bumps the version.
func UpdateOccupancySettings(ctx context.Context, tx bun.IDB, s MarinaScope, o *OccupancySettings, expected int) error {
	res, err := tx.NewUpdate().Model(o).ExcludeColumn("marina_id", "version", "created_at").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetOccupancySettings(ctx, tx, s); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrVersionMismatch
			}
			return err
		}
		return ErrVersionMismatch
	}
	o.Version = expected + 1
	return nil
}
