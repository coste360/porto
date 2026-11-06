package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Queries of harbor_settings (one row per marina, inserted lazily by the harbour service).

// GetHarborSettings returns the settings of a marina, or ErrNotFound when none were stored yet.
func GetHarborSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*HarborSettings, error) {
	h := new(HarborSettings)
	if err := db.NewSelect().Model(h).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return h, nil
}

// InsertHarborSettings inserts the settings row of a marina; a row already present is ErrConflict.
func InsertHarborSettings(ctx context.Context, db bun.IDB, s MarinaScope, h *HarborSettings) error {
	h.MarinaID = s.ID()
	_, err := db.NewInsert().Model(h).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateHarborSettings writes the settings when the stored version equals expected, and bumps the version.
func UpdateHarborSettings(ctx context.Context, db bun.IDB, s MarinaScope, h *HarborSettings, expected int) error {
	res, err := db.NewUpdate().Model(h).Column("price_class_rule", "loa_tolerance_cm", "default_grid_cm", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetHarborSettings(ctx, db, s); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	h.Version = expected + 1
	return nil
}
