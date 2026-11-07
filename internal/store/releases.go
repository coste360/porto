package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of releases (spec 10.6; m3-plan §3.4).

// Release statuses.
const (
	ReleaseActive  = "active"
	ReleaseRevoked = "revoked"
)

// InsertRelease inserts a release of the marina.
func InsertRelease(ctx context.Context, tx bun.IDB, s MarinaScope, r *Release) error {
	r.MarinaID = s.ID()
	if r.Version == 0 {
		r.Version = 1
	}
	_, err := tx.NewInsert().Model(r).Exec(ctx)
	return err
}

// GetRelease returns a release of the marina.
func GetRelease(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Release, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(Release)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// UpdateRelease writes the mutable columns of a release when its version equals expected.
func UpdateRelease(ctx context.Context, tx bun.IDB, s MarinaScope, r *Release, expected int, now time.Time) error {
	r.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(r).
		Column("from_night", "to_night", "status", "resold_allocation_id", "resold_nights", "revoked_at", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRelease(ctx, tx, s, r.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	r.Version = expected + 1
	return nil
}

// ActiveReleasesOf returns the active releases of the allocations, ordered by allocation and first night.
func ActiveReleasesOf(ctx context.Context, db bun.IDB, s MarinaScope, allocIDs []string) ([]Release, error) {
	if len(allocIDs) == 0 {
		return nil, nil
	}
	var out []Release
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("allocation_id IN (?)", bun.List(allocIDs)).
		Where("status = ?", ReleaseActive).OrderExpr("allocation_id, from_night").Scan(ctx)
	return out, err
}

// ReleasesOnResource returns the active releases of a resource intersecting [first, last].
func ReleasesOnResource(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, resourceID string, first, last calendar.Date) ([]Release, error) {
	var out []Release
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("resource_type = ?", resourceType).
		Where("resource_id = ?", resourceID).Where("status = ?", ReleaseActive).Where("from_night <= ?", last).
		Where("to_night >= ?", first).OrderExpr("from_night, id").Scan(ctx)
	return out, err
}

// ReleasesCovering returns the active releases of resources covering a night (day view).
func ReleasesCovering(ctx context.Context, db bun.IDB, s MarinaScope, night calendar.Date) ([]Release, error) {
	var out []Release
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", ReleaseActive).
		Where("from_night <= ?", night).Where("to_night >= ?", night).OrderExpr("id").Scan(ctx)
	return out, err
}

// ReleaseFilter selects releases.
type ReleaseFilter struct {
	From, To   calendar.Date
	ResourceID string
	Status     string
}

// ListReleases returns a page of releases of the marina ordered by id (newest last).
func ListReleases(ctx context.Context, db bun.IDB, s MarinaScope, f ReleaseFilter, cursor string, limit int) (Page[Release], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Release]{}, err
	}
	rows := []Release{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if !f.From.IsZero() {
		q = q.Where("to_night >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("from_night <= ?", f.To)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_id = ?", f.ResourceID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Release]{}, err
	}
	return pageOf(rows, limit, func(r Release) string { return r.ID }), nil
}
