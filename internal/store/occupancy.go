package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Occupancy rows (berth_occupancies, cell_occupancies, land_occupancies; m3-plan §3.1). No double occupancy is
// the primary key of each table (INV-01): rows are inserted with ON CONFLICT DO NOTHING and the caller compares
// the inserted count with its plan. A shortfall is a conflict; there is no "check then insert" anywhere.

// OccRow is one planned occupancy row: Cell is 0 for berths and land resources.
type OccRow struct {
	Resource string
	Cell     int
	Night    calendar.Date
}

// OccOwner is an occupancy row with the allocation that holds it.
type OccOwner struct {
	Resource     string        `bun:"resource"`
	Cell         int           `bun:"cell"`
	Night        calendar.Date `bun:"night"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
	ExpiresAt    time.Time     `bun:"expires_at,nullzero"`
}

// Family is the table and resource column of a resource type.
type Family struct {
	Table, Column string
	Cells         bool
}

// FamilyOf returns the row family of an allocation resource type.
func FamilyOf(resourceType string) Family {
	switch resourceType {
	case ResourceArea:
		return Family{Table: "cell_occupancies", Column: "area_id", Cells: true}
	case ResourceLand:
		return Family{Table: "land_occupancies", Column: "resource_id"}
	default:
		return Family{Table: "berth_occupancies", Column: "berth_id"}
	}
}

// Families returns the three families with their resource type.
func Families() []struct {
	ResourceType string
	Family
} {
	return []struct {
		ResourceType string
		Family
	}{{ResourceBerth, FamilyOf(ResourceBerth)}, {ResourceArea, FamilyOf(ResourceArea)}, {ResourceLand, FamilyOf(ResourceLand)}}
}

// occChunk keeps every insert well below the SQLite variable limit (500 rows × 6 columns).
const occChunk = 500

// InsertOccupancyRows inserts the rows of an allocation with ON CONFLICT DO NOTHING and returns how many were
// inserted. Rows carry the allocation's marina, id, kind and expiry.
func InsertOccupancyRows(ctx context.Context, tx bun.IDB, s MarinaScope, a *Allocation, rows []OccRow) (int64, error) {
	var total int64
	for start := 0; start < len(rows); start += occChunk {
		part := rows[start:min(len(rows), start+occChunk)]
		var q *bun.InsertQuery
		switch a.ResourceType {
		case ResourceBerth:
			ms := make([]BerthOccupancy, len(part))
			for i, r := range part {
				ms[i] = BerthOccupancy{MarinaID: s.ID(), BerthID: r.Resource, Night: r.Night, AllocationID: a.ID, Kind: a.Kind, ExpiresAt: UTC(a.ExpiresAt)}
			}
			q = tx.NewInsert().Model(&ms)
		case ResourceArea:
			ms := make([]CellOccupancy, len(part))
			for i, r := range part {
				ms[i] = CellOccupancy{MarinaID: s.ID(), AreaID: r.Resource, Cell: int64(r.Cell), Night: r.Night, AllocationID: a.ID, Kind: a.Kind, ExpiresAt: UTC(a.ExpiresAt)}
			}
			q = tx.NewInsert().Model(&ms)
		case ResourceLand:
			ms := make([]LandOccupancy, len(part))
			for i, r := range part {
				ms[i] = LandOccupancy{MarinaID: s.ID(), ResourceID: r.Resource, Night: r.Night, AllocationID: a.ID, Kind: a.Kind, ExpiresAt: UTC(a.ExpiresAt)}
			}
			q = tx.NewInsert().Model(&ms)
		default:
			return total, fmt.Errorf("store: unknown resource type %q", a.ResourceType)
		}
		res, err := q.On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return total, err
		}
		total += affected(res)
	}
	return total, nil
}

func ownerSelect(tx bun.IDB, f Family) *bun.SelectQuery {
	cell := "0"
	if f.Cells {
		cell = "o.cell"
	}
	return tx.NewSelect().TableExpr(f.Table + " AS o").
		ColumnExpr("o." + f.Column + " AS resource").ColumnExpr(cell + " AS cell").
		ColumnExpr("o.night, o.allocation_id, o.kind, o.expires_at")
}

// OccupancyOwners returns the rows of resourceID in [first, last] held by allocations other than exclude
// (cells nil = every cell), ordered by night and cell.
func OccupancyOwners(ctx context.Context, tx bun.IDB, s MarinaScope, resourceType, resourceID string, first, last calendar.Date, cells []int, exclude []string) ([]OccOwner, error) {
	f := FamilyOf(resourceType)
	q := ownerSelect(tx, f).Where("o.marina_id = ?", s.ID()).Where("o."+f.Column+" = ?", resourceID).
		Where("o.night >= ?", first).Where("o.night <= ?", last)
	if f.Cells && cells != nil {
		q = q.Where("o.cell IN (?)", bun.List(cells))
	}
	if len(exclude) > 0 {
		q = q.Where("o.allocation_id NOT IN (?)", bun.List(exclude))
	}
	var out []OccOwner
	if err := q.OrderExpr("o.night, cell").Scan(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RowsOfAllocation returns every occupancy row of an allocation, sorted by night and cell.
func RowsOfAllocation(ctx context.Context, db bun.IDB, s MarinaScope, a *Allocation) ([]OccRow, error) {
	f := FamilyOf(a.ResourceType)
	var owners []OccOwner
	// Sorted here, not by ORDER BY night: with that ORDER BY the SQLite planner walks every row of the marina
	// through the (marina_id, night) index to save the sort instead of looking the allocation up by its index,
	// which made each transit confirmation scan the season of a busy marina.
	err := ownerSelect(db, f).Where("o.marina_id = ?", s.ID()).Where("o.allocation_id = ?", a.ID).Scan(ctx, &owners)
	if err != nil {
		return nil, err
	}
	out := make([]OccRow, len(owners))
	for i, o := range owners {
		out[i] = OccRow{Resource: o.Resource, Cell: o.Cell, Night: o.Night}
	}
	slices.SortStableFunc(out, func(x, y OccRow) int {
		switch {
		case x.Night.Before(y.Night):
			return -1
		case y.Night.Before(x.Night):
			return 1
		}
		return cmp.Compare(x.Cell, y.Cell)
	})
	return out, nil
}

// DeleteAllocationRows deletes the rows of an allocation on nights in [from, to] (zero dates = unbounded) and
// returns how many were deleted.
func DeleteAllocationRows(ctx context.Context, tx bun.IDB, s MarinaScope, a *Allocation, from, to calendar.Date) (int64, error) {
	f := FamilyOf(a.ResourceType)
	q := tx.NewDelete().TableExpr(f.Table).Where("marina_id = ?", s.ID()).Where("allocation_id = ?", a.ID)
	if !from.IsZero() {
		q = q.Where("night >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("night <= ?", to)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// SetRowsKind copies a new kind and expiry of an allocation onto its rows.
func SetRowsKind(ctx context.Context, tx bun.IDB, s MarinaScope, a *Allocation) error {
	f := FamilyOf(a.ResourceType)
	q := tx.NewUpdate().TableExpr(f.Table).Set("kind = ?", a.Kind)
	if a.ExpiresAt.IsZero() {
		q = q.Set("expires_at = NULL")
	} else {
		q = q.Set("expires_at = ?", UTC(a.ExpiresAt))
	}
	_, err := q.Where("marina_id = ?", s.ID()).Where("allocation_id = ?", a.ID).Exec(ctx)
	return err
}

// ReclaimExpiredHolds ends the hold allocations whose rows on resourceID in [first, last] expired before now
// (INV-05): all their rows are deleted (on every night: an expired hold is gone as a whole) and they become
// expired. It returns the reclaimed allocations. Concurrent reclaims of the same hold are harmless.
func ReclaimExpiredHolds(ctx context.Context, tx bun.IDB, s MarinaScope, resourceType, resourceID string, first, last calendar.Date, now time.Time) ([]Allocation, error) {
	f := FamilyOf(resourceType)
	var ids []string
	// "marina_id || ''" keeps the marina filter out of index selection: without statistics SQLite would pick the
	// (marina_id, night) index and scan the whole marina over the range instead of the (resource, night) key.
	err := tx.NewSelect().TableExpr(f.Table).ColumnExpr("DISTINCT allocation_id").Where("marina_id || '' = ?", s.ID()).
		Where(f.Column+" = ?", resourceID).Where("night >= ?", first).Where("night <= ?", last).
		Where("kind = ?", AllocKindHold).Where("expires_at < ?", UTC(now)).Scan(ctx, &ids)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return ExpireAllocations(ctx, tx, s, ids, now)
}

// ExpireAllocations marks expired hold allocations expired and deletes their rows; it returns the ones it
// changed. The ids are candidates read before any lock: the UPDATE re-checks status, kind and expiry, so a hold
// converted in the meantime by a concurrent ConvertTx (PostgreSQL READ COMMITTED re-evaluates the WHERE after
// waiting for the row lock) is left alone, and rows are deleted only for the allocations actually updated
// (RETURNING, both dialects).
func ExpireAllocations(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string, now time.Time) ([]Allocation, error) {
	slices.Sort(ids)
	var out []Allocation
	err := tx.NewUpdate().Model((*Allocation)(nil)).Set("status = ?", AllocExpired).Set("version = version + 1").
		Set("updated_at = ?", UTC(now)).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).
		Where("status = ?", AllocActive).Where("kind = ?", AllocKindHold).Where("expires_at IS NOT NULL").
		Where("expires_at < ?", UTC(now)).Returning("*").Scan(ctx, &out)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if _, err := DeleteAllocationRows(ctx, tx, s, &out[i], calendar.Date{}, calendar.Date{}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ExpiredHoldIDs returns up to limit active hold allocations of the marina whose expiry is before now.
func ExpiredHoldIDs(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]string, error) {
	var ids []string
	err := db.NewSelect().Model((*Allocation)(nil)).Column("id").Where("marina_id = ?", s.ID()).
		Where("status = ?", AllocActive).Where("kind = ?", AllocKindHold).Where("expires_at < ?", UTC(now)).
		OrderExpr("id").Limit(int64(limit)).Scan(ctx, &ids)
	return ids, err
}

// CountExpiredHoldRows counts the hold rows of the marina whose expiry is before now (query Q11 of m3-plan §6:
// zero after the contracts.expire job ran).
func CountExpiredHoldRows(ctx context.Context, db bun.IDB, marinaID string, now time.Time) (int, error) {
	total := 0
	for _, f := range Families() {
		n, err := db.NewSelect().TableExpr(f.Table).Where("marina_id = ?", marinaID).Where("kind = ?", AllocKindHold).
			Where("expires_at < ?", UTC(now)).Count(ctx)
		if err != nil {
			return 0, err
		}
		total += int(n)
	}
	return total, nil
}

// CountOccupancyRows returns the number of rows per family table of a marina.
func CountOccupancyRows(ctx context.Context, db bun.IDB, marinaID string) (map[string]int, error) {
	out := map[string]int{}
	for _, f := range Families() {
		n, err := db.NewSelect().TableExpr(f.Table).Where("marina_id = ?", marinaID).Count(ctx)
		if err != nil {
			return nil, err
		}
		out[f.Table] = int(n)
	}
	return out, nil
}
