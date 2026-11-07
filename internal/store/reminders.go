package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Reminder runs and items, and dunning exclusions (m5-plan §5.5, INV-58): one run per marina and business day
// (reminder_runs_day_uidx), each (charge, level) at most once (reminder_items_level_uidx). Owned by CASH-DRIVERS.

// Reminder run statuses.
const (
	ReminderRunRunning = "running"
	ReminderRunDone    = "done"
	ReminderRunFailed  = "failed"
)

// InsertReminderRun creates the run of a day; created is false when the day already has its run (returned).
func InsertReminderRun(ctx context.Context, tx bun.IDB, s MarinaScope, r *ReminderRun) (*ReminderRun, bool, error) {
	res, err := tx.NewInsert().Model(r).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if affected(res) == 1 {
		return r, true, nil
	}
	existing, err := ReminderRunOn(ctx, tx, s, r.RunOn)
	return existing, false, err
}

// ReminderRunOn returns the run of a day.
func ReminderRunOn(ctx context.Context, db bun.IDB, s MarinaScope, day calendar.Date) (*ReminderRun, error) {
	r := new(ReminderRun)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("run_on = ?", day).Scan(ctx)
	return r, notFound(err)
}

// GetReminderRun returns a run of the marina.
func GetReminderRun(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ReminderRun, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(ReminderRun)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return r, notFound(err)
}

// FinishReminderRun stores the counts and the final status.
func FinishReminderRun(ctx context.Context, tx bun.IDB, s MarinaScope, id, status string, counts JSON, now time.Time) error {
	now = UTC(now)
	_, err := tx.NewUpdate().Model((*ReminderRun)(nil)).Set("status = ?", status).Set("counts = ?", counts).
		Set("finished_at = ?", now).Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// ListReminderRuns returns runs newest first (cursor = last id).
func ListReminderRuns(ctx context.Context, db bun.IDB, s MarinaScope, cursor string, limit int) (Page[ReminderRun], error) {
	limit = ClampLimit(limit)
	rows := []ReminderRun{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[ReminderRun]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ReminderRun]{}, err
	}
	return pageOf(rows, limit, func(r ReminderRun) string { return r.ID }), nil
}

// InsertReminderItem inserts an item; false when the (charge, level) pair already exists (INV-58).
func InsertReminderItem(ctx context.Context, tx bun.IDB, it *ReminderItem) (bool, error) {
	res, err := tx.NewInsert().Model(it).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// GetReminderItem returns an item of the marina.
func GetReminderItem(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ReminderItem, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	it := new(ReminderItem)
	err := db.NewSelect().Model(it).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return it, notFound(err)
}

// ReminderItemFilter narrows ListReminderItems.
type ReminderItemFilter struct {
	RunID    string
	Status   string
	ChargeID string
}

// ListReminderItems returns items newest first (cursor = last id).
func ListReminderItems(ctx context.Context, db bun.IDB, s MarinaScope, f ReminderItemFilter, cursor string, limit int) (Page[ReminderItem], error) {
	limit = ClampLimit(limit)
	rows := []ReminderItem{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.RunID != "" {
		q = q.Where("run_id = ?", f.RunID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.ChargeID != "" {
		q = q.Where("charge_id = ?", f.ChargeID)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[ReminderItem]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ReminderItem]{}, err
	}
	return pageOf(rows, limit, func(r ReminderItem) string { return r.ID }), nil
}

// ReminderLevelsSent maps each charge to the levels already recorded for it (any status).
func ReminderLevelsSent(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	var rows []ReminderItem
	for start := 0; start < len(chargeIDs); start += 500 {
		end := min(start+500, len(chargeIDs))
		var part []ReminderItem
		if err := db.NewSelect().Model(&part).Column("charge_id", "level").Where("marina_id = ?", s.ID()).
			Where("charge_id IN (?)", bun.List(chargeIDs[start:end])).Scan(ctx); err != nil {
			return nil, err
		}
		rows = append(rows, part...)
	}
	for _, r := range rows {
		out[r.ChargeID] = append(out[r.ChargeID], r.Level)
	}
	return out, nil
}

// UpdateReminderItem moves an item from one of from to status, recording the request, message and decision.
func UpdateReminderItem(ctx context.Context, tx bun.IDB, s MarinaScope, it *ReminderItem, from []string, now time.Time) (bool, error) {
	now = UTC(now)
	q := tx.NewUpdate().Model((*ReminderItem)(nil)).Set("status = ?", it.Status).
		Set("request_id = ?", nullString(it.RequestID)).Set("message_id = ?", nullString(it.MessageID)).
		Set("decided_by_user_id = ?", nullString(it.DecidedByUserID)).Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", it.ID).Where("status IN (?)", bun.List(from))
	if it.DecidedByUserID != "" {
		q = q.Set("decided_at = ?", now)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	if affected(res) == 0 {
		return false, nil
	}
	it.Version++
	return true, nil
}

// InsertDunningExclusion inserts an exclusion.
func InsertDunningExclusion(ctx context.Context, tx bun.IDB, e *DunningExclusion) error {
	_, err := tx.NewInsert().Model(e).Exec(ctx)
	return err
}

// GetDunningExclusion returns an exclusion of the marina.
func GetDunningExclusion(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*DunningExclusion, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(DunningExclusion)
	err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return e, notFound(err)
}

// DunningExclusionFilter narrows ListDunningExclusions. ActiveOn non-zero keeps the exclusions active that day.
type DunningExclusionFilter struct {
	CustomerID string
	ChargeID   string
	ActiveOn   calendar.Date
}

// ListDunningExclusions returns exclusions newest first (cursor = last id).
func ListDunningExclusions(ctx context.Context, db bun.IDB, s MarinaScope, f DunningExclusionFilter, cursor string, limit int) (Page[DunningExclusion], error) {
	limit = ClampLimit(limit)
	rows := []DunningExclusion{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if f.ChargeID != "" {
		q = q.Where("charge_id = ?", f.ChargeID)
	}
	if !f.ActiveOn.IsZero() {
		q = q.Where("removed_at IS NULL").WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("until_on IS NULL").WhereOr("until_on >= ?", f.ActiveOn)
		})
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[DunningExclusion]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[DunningExclusion]{}, err
	}
	return pageOf(rows, limit, func(e DunningExclusion) string { return e.ID }), nil
}

// RemoveDunningExclusion marks an exclusion removed (conditional on not removed yet).
func RemoveDunningExclusion(ctx context.Context, tx bun.IDB, s MarinaScope, id, userID string, now time.Time) (bool, error) {
	now = UTC(now)
	res, err := tx.NewUpdate().Model((*DunningExclusion)(nil)).Set("removed_at = ?", now).
		Set("removed_by_user_id = ?", nullString(userID)).Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("removed_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}
