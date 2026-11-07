package store

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the M4 management mandates and their revenues (m4-plan §4.3).

// MandateFilter narrows ListMandates.
type MandateFilter struct {
	RightID string
	Status  string
}

// InsertMandate inserts a mandate.
func InsertMandate(ctx context.Context, db bun.IDB, s MarinaScope, m *ManagementMandate) error {
	m.MarinaID = s.ID()
	_, err := db.NewInsert().Model(m).Exec(ctx)
	return err
}

// GetMandate returns a mandate of the marina.
func GetMandate(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ManagementMandate, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(ManagementMandate)
	if err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// UpdateMandate writes status, to_on and ended_on when the version equals expected.
func UpdateMandate(ctx context.Context, db bun.IDB, s MarinaScope, m *ManagementMandate, expected int) error {
	if err := versionedUpdate(ctx, db, s, m, "management_mandates", m.ID, expected, "status", "to_on", "ended_on", "updated_at"); err != nil {
		return err
	}
	m.Version = expected + 1
	return nil
}

// ListMandates returns a page of mandates by id.
func ListMandates(ctx context.Context, db bun.IDB, s MarinaScope, f MandateFilter, cursor string, limit int) (Page[ManagementMandate], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ManagementMandate]{}, err
	}
	limit = ClampLimit(limit)
	var rows []ManagementMandate
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.RightID != "" {
		q = q.Where("right_id = ?", f.RightID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ManagementMandate]{}, err
	}
	return pageOf(rows, limit, func(r ManagementMandate) string { return r.ID }), nil
}

// MandatesOfRights returns the mandates of rights (every status), by start.
func MandatesOfRights(ctx context.Context, db bun.IDB, s MarinaScope, rightIDs []string) ([]ManagementMandate, error) {
	out := []ManagementMandate{}
	ids := validIDs(rightIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("right_id IN (?)", bun.List(ids)).OrderExpr("from_on, id").Scan(ctx)
	return out, err
}

// OverlappingMandates returns the non-cancelled mandates of a right that share a night with [from, to].
func OverlappingMandates(ctx context.Context, db bun.IDB, s MarinaScope, rightID string, from, to calendar.Date, excludeID string) ([]ManagementMandate, error) {
	out := []ManagementMandate{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("right_id = ?", rightID).
		Where("status <> 'cancelled'").Where("from_on <= ? AND to_on >= ?", to, from)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Scan(ctx)
	return out, err
}

// MandatesToAccrue returns the mandates whose revenues may still change: active ones and the ones ended or
// cancelled less than 60 days before since.
func MandatesToAccrue(ctx context.Context, db bun.IDB, s MarinaScope, since calendar.Date) ([]ManagementMandate, error) {
	out := []ManagementMandate{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("(status = 'active' OR to_on >= ?)", since).OrderExpr("id").Scan(ctx)
	return out, err
}

// MarinasWithMandates returns the ids of the marinas that have at least one mandate (the accrual job).
func MarinasWithMandates(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().Model((*ManagementMandate)(nil)).ColumnExpr("DISTINCT marina_id").Scan(ctx, &ids)
	return ids, err
}

// SoldAllocation is an allocation (transit or contract) sold on a berth with its contract.
type SoldAllocation struct {
	AllocationID string        `bun:"allocation_id"`
	Status       string        `bun:"status"`
	First        calendar.Date `bun:"first_night"`
	Last         calendar.Date `bun:"last_night"`
	ContractID   string        `bun:"contract_id"`
	ContractKind string        `bun:"contract_kind"`
	ContractYear int           `bun:"contract_year"`
	ContractNo   int64         `bun:"contract_number"`
	Term         string        `bun:"term"`
	ContractStat string        `bun:"contract_status"`
	StartsOn     calendar.Date `bun:"starts_on"`
	EndsOn       calendar.Date `bun:"ends_on"` // exclusive
	TotalCents   int64         `bun:"total_cents"`
}

// SoldAllocationsOn returns the active transit and contract allocations of a berth that share a night with
// [from, to], with the contract of their line.
func SoldAllocationsOn(ctx context.Context, db bun.IDB, s MarinaScope, berthID string, from, to calendar.Date) ([]SoldAllocation, error) {
	out := []SoldAllocation{}
	err := db.NewRaw(`SELECT a.id AS allocation_id, a.status, a.first_night, a.last_night, c.id AS contract_id,
			c.kind AS contract_kind, c.year AS contract_year, c.number AS contract_number, coalesce(c.term, '') AS term, c.status AS contract_status, c.starts_on, c.ends_on, c.total_cents
		FROM allocations AS a JOIN contract_lines AS l ON l.id = a.contract_line_id JOIN contracts AS c ON c.id = l.contract_id
		WHERE a.marina_id = ? AND a.resource_type = 'berth' AND a.resource_id = ? AND a.status = 'active'
			AND a.kind IN ('transit','contract') AND a.first_night <= ? AND a.last_night >= ?
		ORDER BY a.first_night, a.id`, s.ID(), berthID, to, from).Scan(ctx, &out)
	return out, err
}

// ---- mandate_revenues ----

// UpsertMandateRevenue inserts or updates the revenue row of (mandate, allocation) and reports whether a value
// changed (idempotent: the same values change nothing).
func UpsertMandateRevenue(ctx context.Context, db bun.IDB, s MarinaScope, r *MandateRevenue) (bool, error) {
	r.MarinaID = s.ID()
	cur := new(MandateRevenue)
	err := db.NewSelect().Model(cur).Where("marina_id = ?", s.ID()).Where("mandate_id = ?", r.MandateID).
		Where("allocation_id = ?", r.AllocationID).Scan(ctx)
	if err != nil && !errors.Is(notFound(err), ErrNotFound) {
		return false, err
	}
	if err == nil {
		if cur.Nights == r.Nights && cur.RevenueCents == r.RevenueCents && cur.OwnerShareCents == r.OwnerShareCents &&
			cur.ThroughOn.Equal(r.ThroughOn) && cur.ContractID == r.ContractID {
			*r = *cur
			return false, nil
		}
		r.ID, r.CreatedAt = cur.ID, cur.CreatedAt
		_, err := db.NewUpdate().Model(r).Column("contract_id", "nights", "revenue_cents", "owner_share_cents", "through_on", "updated_at").
			Where("marina_id = ?", s.ID()).Where("id = ?", cur.ID).Exec(ctx)
		return err == nil, err
	}
	_, err = db.NewInsert().Model(r).On("CONFLICT (mandate_id, allocation_id) DO NOTHING").Exec(ctx)
	return err == nil, err
}

// RevenuesOfMandates returns the revenue rows of mandates, by mandate and allocation.
func RevenuesOfMandates(ctx context.Context, db bun.IDB, s MarinaScope, mandateIDs []string) ([]MandateRevenue, error) {
	out := []MandateRevenue{}
	ids := validIDs(mandateIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("mandate_id IN (?)", bun.List(ids)).
		OrderExpr("mandate_id, id").Scan(ctx)
	return out, err
}
