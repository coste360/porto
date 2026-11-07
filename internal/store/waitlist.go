package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of waitlist_entries (spec 13.9; m4-plan §3.6).

// InsertWaitlistEntry inserts an entry.
func InsertWaitlistEntry(ctx context.Context, db bun.IDB, e *WaitlistEntry) error {
	_, err := db.NewInsert().Model(e).Exec(ctx)
	return err
}

// GetWaitlistEntry returns an entry of a marina.
func GetWaitlistEntry(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WaitlistEntry, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(WaitlistEntry)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// UpdateWaitlistEntry writes e when its version equals expected and bumps the version.
func UpdateWaitlistEntry(ctx context.Context, db bun.IDB, s MarinaScope, expected int, e *WaitlistEntry, now time.Time) error {
	e.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(e).ExcludeColumn("id", "marina_id", "created_at", "version").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	e.Version = expected + 1
	return nil
}

// ListWaitlistEntries returns the entries of a marina ordered by priority (desc) and date, filtered by status
// and term when set.
func ListWaitlistEntries(ctx context.Context, db bun.IDB, s MarinaScope, status, term string, limit int) ([]WaitlistEntry, error) {
	out := []WaitlistEntry{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if term != "" {
		q = q.Where("term = ?", term)
	}
	err := q.OrderExpr("priority DESC, created_at, id").Limit(int64(ClampLimit(limit))).Scan(ctx)
	return out, err
}

// BerthDims are the dimensions of a berth the waiting list compares with.
type BerthDims struct {
	ID        string `bun:"id"`
	MaxLOACm  int64  `bun:"max_loa_cm"`
	MaxBeamCm int64  `bun:"max_beam_cm"`
	DepthCm   int64  `bun:"depth_cm"`
}

// BerthDimsByIDs returns the dimensions of berths of a marina.
func BerthDimsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]BerthDims, error) {
	out := []BerthDims{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("berths").ColumnExpr("id, max_loa_cm, max_beam_cm, COALESCE(depth_cm, 0) AS depth_cm").
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx, &out)
	return out, err
}

// BerthGroupIDs returns the groups of a berth.
func BerthGroupIDs(ctx context.Context, db bun.IDB, s MarinaScope, berthID string) ([]string, error) {
	var out []string
	err := db.NewSelect().TableExpr("berth_group_members").Column("group_id").Where("marina_id = ?", s.ID()).
		Where("resource_type = 'berth'").Where("resource_id = ?", berthID).Scan(ctx, &out)
	return out, err
}

// SeedCustomerIDs returns up to n customer ids of a marina, oldest first (demo seed of the waiting list).
func SeedCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope, n int) ([]string, error) {
	var out []string
	err := db.NewSelect().TableExpr("customers").Column("id").Where("marina_id = ?", s.ID()).OrderExpr("id").
		Limit(int64(n)).Scan(ctx, &out)
	return out, err
}
