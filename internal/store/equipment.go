package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the equipment registry (M9, m9-plan §8.1). Owned by UTILITIES. A duplicate code is a unique violation
// of equipment_code_uidx (the service maps it to code_taken).

// InsertEquipment inserts equipment of the marina.
func InsertEquipment(ctx context.Context, tx bun.IDB, s MarinaScope, e *Equipment) error {
	e.MarinaID = s.ID()
	if e.Version == 0 {
		e.Version = 1
	}
	if e.DefaultMinutes == 0 {
		e.DefaultMinutes = 30
	}
	e.CreatedAt, e.UpdatedAt = UTC(e.CreatedAt), UTC(e.UpdatedAt)
	_, err := tx.NewInsert().Model(e).Exec(ctx)
	return err
}

// GetEquipment returns equipment of the marina.
func GetEquipment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Equipment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(Equipment)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// UpdateEquipment writes the mutable columns when the version equals expected and bumps it.
func UpdateEquipment(ctx context.Context, tx bun.IDB, s MarinaScope, e *Equipment, expected int, now time.Time) error {
	e.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(e).
		Column("code", "name", "kind", "map_id", "x_cm", "y_cm", "capacity_kg", "active", "bookable", "default_minutes", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetEquipment(ctx, tx, s, e.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	e.Version = expected + 1
	return nil
}

// EquipmentFilter filters the registry.
type EquipmentFilter struct {
	Kind   string
	Active *bool
	IDs    []string
}

// ListEquipment returns a page of equipment ordered by id (creation order).
func ListEquipment(ctx context.Context, db bun.IDB, s MarinaScope, f EquipmentFilter, cursor string, limit int) (Page[Equipment], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Equipment]{}, err
	}
	limit = ClampLimit(limit)
	var rows []Equipment
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return Page[Equipment]{Items: []Equipment{}}, nil
		}
		q = q.Where("id IN (?)", bun.List(f.IDs))
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Equipment]{}, err
	}
	return pageOf(rows, limit, func(e Equipment) string { return e.ID }), nil
}
