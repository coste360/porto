package store

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Queries of contract_settings (m4-plan §5.1): one row per marina, created lazily with the defaults.

// GetContractSettings returns the settings of the marina, the migration defaults when the row does not exist.
func GetContractSettings(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time) (*ContractSettings, error) {
	cs := new(ContractSettings)
	err := db.NewSelect().Model(cs).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err == nil {
		cs.Stored = true
		return cs, nil
	}
	if !errors.Is(notFound(err), ErrNotFound) {
		return nil, err
	}
	return DefaultContractSettings(s.ID(), now), nil
}

// SaveContractSettings inserts the settings row (expected = 0 and the row absent) or updates it when its version
// equals expected; the version is bumped. Another writer in between is ErrVersionMismatch.
func SaveContractSettings(ctx context.Context, db bun.IDB, s MarinaScope, expected int, cs *ContractSettings, now time.Time) error {
	cs.MarinaID = s.ID()
	cs.UpdatedAt = UTC(now)
	exists, err := db.NewSelect().Model((*ContractSettings)(nil)).Where("marina_id = ?", s.ID()).Exists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		if expected > 1 {
			return ErrVersionMismatch
		}
		cs.Version = 2 // the defaults are version 1
		if cs.CreatedAt.IsZero() {
			cs.CreatedAt = UTC(now)
		}
		cs.Stored = true
		if _, err := db.NewInsert().Model(cs).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrVersionMismatch
			}
			return err
		}
		return nil
	}
	q := db.NewUpdate().Model(cs).ExcludeColumn("marina_id", "created_at", "version").Set("version = version + 1").
		Where("marina_id = ?", s.ID())
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	if expected > 0 {
		cs.Version = expected + 1
	} else if err := db.NewSelect().Model(cs).Column("version").Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return err
	}
	return nil
}
