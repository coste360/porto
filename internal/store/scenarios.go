package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of storage scenarios (what-if layouts, m9-plan §7). A scenario writes only storage_scenarios,
// scenario_items and scenario_moves (INV-14). Every function is scoped by the marina.

// ScenarioFilter narrows ListScenarios.
type ScenarioFilter struct {
	AreaID string
	Status string
}

// InsertScenario inserts a scenario.
func InsertScenario(ctx context.Context, tx bun.IDB, sc *StorageScenario) error {
	sc.CreatedAt, sc.UpdatedAt = UTC(sc.CreatedAt), UTC(sc.UpdatedAt)
	_, err := tx.NewInsert().Model(sc).Exec(ctx)
	return err
}

// GetScenario returns one scenario.
func GetScenario(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*StorageScenario, error) {
	return scenario(ctx, db, s, id, false)
}

// LockScenario returns one scenario, locked for update on PostgreSQL.
func LockScenario(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*StorageScenario, error) {
	return scenario(ctx, tx, s, id, true)
}

func scenario(ctx context.Context, db bun.IDB, s MarinaScope, id string, lock bool) (*StorageScenario, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	sc := new(StorageScenario)
	q := db.NewSelect().Model(sc).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return sc, nil
}

// UpdateScenario writes the mutable columns when the version equals expected and bumps it (ErrVersionMismatch
// otherwise).
func UpdateScenario(ctx context.Context, tx bun.IDB, s MarinaScope, sc *StorageScenario, expected int, now time.Time) error {
	sc.UpdatedAt, sc.AppliedAt = UTC(now), UTC(sc.AppliedAt)
	res, err := tx.NewUpdate().Model(sc).
		Column("name", "from_on", "to_on", "status", "seed", "score", "base", "applied_at", "applied_by_user_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", sc.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	sc.Version = expected + 1
	return nil
}

// ListScenarios returns a page of scenarios, newest first.
func ListScenarios(ctx context.Context, db bun.IDB, s MarinaScope, f ScenarioFilter, cursor string, limit int) (Page[StorageScenario], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[StorageScenario]{}, err
	}
	rows := []StorageScenario{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.AreaID != "" {
		if !ValidID(f.AreaID) {
			return Page[StorageScenario]{Items: rows}, nil
		}
		q = q.Where("area_id = ?", f.AreaID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[StorageScenario]{}, err
	}
	return pageOf(rows, limit, func(sc StorageScenario) string { return sc.ID }), nil
}

// DraftScenarioCounts returns the number of draft scenarios per area.
func DraftScenarioCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	var rows []struct {
		AreaID string `bun:"area_id"`
		N      int    `bun:"n"`
	}
	err := db.NewSelect().Model((*StorageScenario)(nil)).ColumnExpr("area_id, COUNT(*) AS n").Where("marina_id = ?", s.ID()).
		Where("status = ?", ScenarioDraft).GroupExpr("area_id").Scan(ctx, &rows)
	out := map[string]int{}
	for _, r := range rows {
		out[r.AreaID] = r.N
	}
	return out, err
}

// ScenarioItems returns the working copy of a scenario, ordered by allocation id.
func ScenarioItems(ctx context.Context, db bun.IDB, s MarinaScope, scenarioID string) ([]ScenarioItem, error) {
	out := []ScenarioItem{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("scenario_id = ?", scenarioID).
		OrderExpr("allocation_id").Scan(ctx)
	return out, err
}

// ReplaceScenarioItems replaces the working copy of a scenario.
func ReplaceScenarioItems(ctx context.Context, tx bun.IDB, s MarinaScope, scenarioID string, items []ScenarioItem) error {
	if _, err := tx.NewDelete().Model((*ScenarioItem)(nil)).Where("marina_id = ?", s.ID()).Where("scenario_id = ?", scenarioID).
		Exec(ctx); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	_, err := tx.NewInsert().Model(&items).Exec(ctx)
	return err
}

// ScenarioMoves returns the moves of a scenario in execution order.
func ScenarioMoves(ctx context.Context, db bun.IDB, s MarinaScope, scenarioID string) ([]ScenarioMove, error) {
	out := []ScenarioMove{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("scenario_id = ?", scenarioID).OrderExpr("seq").Scan(ctx)
	return out, err
}

// ReplaceScenarioMoves replaces the moves of a scenario.
func ReplaceScenarioMoves(ctx context.Context, tx bun.IDB, s MarinaScope, scenarioID string, moves []ScenarioMove) error {
	if _, err := tx.NewDelete().Model((*ScenarioMove)(nil)).Where("marina_id = ?", s.ID()).Where("scenario_id = ?", scenarioID).
		Exec(ctx); err != nil {
		return err
	}
	if len(moves) == 0 {
		return nil
	}
	_, err := tx.NewInsert().Model(&moves).Exec(ctx)
	return err
}
