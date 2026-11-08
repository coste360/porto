package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the storage movement worklist (m9-plan §7.4). Every function is scoped by the marina.

// MovementFilter narrows ListMovements.
type MovementFilter struct {
	Date   *calendar.Date // move_on
	AreaID string
	Status string
}

// InsertMovements inserts movements.
func InsertMovements(ctx context.Context, tx bun.IDB, ms []StorageMovement) error {
	if len(ms) == 0 {
		return nil
	}
	for i := range ms {
		ms[i].CreatedAt, ms[i].UpdatedAt, ms[i].DoneAt = UTC(ms[i].CreatedAt), UTC(ms[i].UpdatedAt), UTC(ms[i].DoneAt)
	}
	_, err := tx.NewInsert().Model(&ms).Exec(ctx)
	return err
}

// ListMovements returns the movements matching f, by date, area and sequence.
func ListMovements(ctx context.Context, db bun.IDB, s MarinaScope, f MovementFilter) ([]StorageMovement, error) {
	out := []StorageMovement{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if f.Date != nil {
		q = q.Where("move_on = ?", *f.Date)
	}
	if f.AreaID != "" {
		if !ValidID(f.AreaID) {
			return out, nil
		}
		q = q.Where("area_id = ?", f.AreaID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	err := q.OrderExpr("move_on, area_id, seq, id").Limit(1000).Scan(ctx)
	return out, err
}

// MovementsOfScenario returns the movements written by a scenario apply, in order.
func MovementsOfScenario(ctx context.Context, db bun.IDB, s MarinaScope, scenarioID string) ([]StorageMovement, error) {
	out := []StorageMovement{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("scenario_id = ?", scenarioID).OrderExpr("seq").Scan(ctx)
	return out, err
}

// LockMovement returns one movement, locked for update on PostgreSQL.
func LockMovement(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*StorageMovement, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(StorageMovement)
	q := tx.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// UpdateMovement writes the status columns when the version equals expected and bumps it.
func UpdateMovement(ctx context.Context, tx bun.IDB, s MarinaScope, m *StorageMovement, expected int, now time.Time) error {
	m.UpdatedAt, m.DoneAt = UTC(now), UTC(m.DoneAt)
	res, err := tx.NewUpdate().Model(m).Column("status", "done_at", "done_by_user_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", m.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	m.Version = expected + 1
	return nil
}
