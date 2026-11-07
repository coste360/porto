package store

import (
	"context"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of allocations (m3-plan §3). Every update bumps the version; only active allocations have rows.

// InsertAllocation inserts an allocation of the marina.
func InsertAllocation(ctx context.Context, tx bun.IDB, s MarinaScope, a *Allocation) error {
	a.MarinaID = s.ID()
	a.ExpiresAt = UTC(a.ExpiresAt)
	if a.Version == 0 {
		a.Version = 1
	}
	if a.Status == "" {
		a.Status = AllocActive
	}
	_, err := tx.NewInsert().Model(a).Exec(ctx)
	return err
}

// GetAllocation returns an allocation of the marina.
func GetAllocation(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Allocation, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	a := new(Allocation)
	if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// LockAllocations reads allocations of the marina ordered by id, locking them FOR UPDATE on PostgreSQL (SQLite
// write transactions are serialised). A missing id is ErrNotFound.
func LockAllocations(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string) ([]Allocation, error) {
	uniq := slices.Clone(ids)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	for _, id := range uniq {
		if !ValidID(id) {
			return nil, ErrNotFound
		}
	}
	var out []Allocation
	q := tx.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(uniq)).OrderExpr("id")
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	if len(out) != len(uniq) {
		return nil, ErrNotFound
	}
	return out, nil
}

// UpdateAllocation writes the mutable columns of an allocation when its version equals expected and bumps the
// version (ErrVersionMismatch otherwise).
func UpdateAllocation(ctx context.Context, tx bun.IDB, s MarinaScope, a *Allocation, expected int, now time.Time) error {
	a.UpdatedAt = UTC(now)
	a.ExpiresAt = UTC(a.ExpiresAt)
	res, err := tx.NewUpdate().Model(a).
		Column("status", "kind", "first_night", "last_night", "expires_at", "fit_override", "x_cm", "y_cm", "rotation_dd", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", a.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetAllocation(ctx, tx, s, a.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	a.Version = expected + 1
	return nil
}

// AllocationFilter selects allocations.
type AllocationFilter struct {
	MapID        string
	ResourceType string
	ResourceIDs  []string
	VesselID     string
	LineIDs      []string
	Status       string        // "" = any
	From, To     calendar.Date // intersecting [From, To] when set
}

// ListAllocations returns the allocations of the marina matching f, ordered by resource and first night.
func ListAllocations(ctx context.Context, db bun.IDB, s MarinaScope, f AllocationFilter) ([]Allocation, error) {
	var out []Allocation
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if f.MapID != "" {
		q = q.Where("map_id = ?", f.MapID)
	}
	if f.ResourceType != "" {
		q = q.Where("resource_type = ?", f.ResourceType)
	}
	if f.ResourceIDs != nil {
		if len(f.ResourceIDs) == 0 {
			return nil, nil
		}
		q = q.Where("resource_id IN (?)", bun.List(f.ResourceIDs))
	}
	if f.VesselID != "" {
		q = q.Where("vessel_id = ?", f.VesselID)
	}
	if f.LineIDs != nil {
		if len(f.LineIDs) == 0 {
			return nil, nil
		}
		q = q.Where("contract_line_id IN (?)", bun.List(f.LineIDs))
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if !f.From.IsZero() {
		q = q.Where("last_night >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("first_night <= ?", f.To)
	}
	err := q.OrderExpr("resource_type, resource_id, first_night, id").Scan(ctx)
	return out, err
}

// ActiveSuccessors returns the active allocations whose moved_from_id is id (the next links of its chain).
func ActiveSuccessors(ctx context.Context, db bun.IDB, s MarinaScope, id string) ([]Allocation, error) {
	var out []Allocation
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("moved_from_id = ?", id).
		Where("status = ?", AllocActive).OrderExpr("first_night, id").Scan(ctx)
	return out, err
}

// HasCurrentAllocations returns the ids of the resources (of one type) that have an active allocation with
// last_night ≥ today (harbour guard, m3-plan §3.7).
func HasCurrentAllocations(ctx context.Context, db bun.IDB, s MarinaScope, resourceType string, ids []string, today calendar.Date) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []string
	err := db.NewSelect().Model((*Allocation)(nil)).ColumnExpr("DISTINCT resource_id").Where("marina_id = ?", s.ID()).
		Where("resource_type = ?", resourceType).Where("resource_id IN (?)", bun.List(ids)).Where("status = ?", AllocActive).
		Where("last_night >= ?", today).Scan(ctx, &out)
	slices.Sort(out)
	return out, err
}

// ContractOfLine is the contract a contract line belongs to (read-only join used by occupancy views and events).
type ContractOfLine struct {
	LineID       string        `bun:"line_id"`
	ContractID   string        `bun:"contract_id"`
	Kind         string        `bun:"kind"`
	Term         string        `bun:"term"`
	Status       string        `bun:"status"`
	Number       int64         `bun:"number"`
	Year         int           `bun:"year"`
	CustomerID   string        `bun:"customer_id"`
	StartsOn     calendar.Date `bun:"starts_on"`
	EndsOn       calendar.Date `bun:"ends_on"`
	HoldLost     bool          `bun:"hold_lost"`
	ArrivalTime  string        `bun:"expected_arrival_time"`
	Policy       JSON          `bun:"policy"`
	TerminatedOn calendar.Date `bun:"terminated_on"`
}

// ContractsOfLines returns, per contract line id, its contract.
func ContractsOfLines(ctx context.Context, db bun.IDB, s MarinaScope, lineIDs []string) (map[string]ContractOfLine, error) {
	out := map[string]ContractOfLine{}
	if len(lineIDs) == 0 {
		return out, nil
	}
	var rows []ContractOfLine
	err := db.NewSelect().TableExpr("contract_lines AS l").Join("JOIN contracts AS c ON c.id = l.contract_id").
		ColumnExpr("l.id AS line_id, c.id AS contract_id, c.kind, coalesce(c.term, '') AS term, c.status, c.number, c.year").
		ColumnExpr("coalesce(CAST(c.customer_id AS TEXT), '') AS customer_id, c.starts_on, c.ends_on, c.hold_lost").
		ColumnExpr("coalesce(c.expected_arrival_time, '') AS expected_arrival_time, c.policy, c.terminated_on").
		Where("l.marina_id = ?", s.ID()).Where("l.id IN (?)", bun.List(lineIDs)).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.LineID] = r
	}
	return out, nil
}
