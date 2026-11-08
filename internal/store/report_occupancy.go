package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Report reads of M7 (m7-plan §9): raw rows, every query filtered by the marina of the scope first. The SQL
// filters, joins for labels and dimensions and groups identity keys only to count physical rows (cells of one
// allocation on one night, D2); money, ratios and indicators are computed by the pure report packages.

// ReportFilter is the period of a report read (inclusive nights or dates).
type ReportFilter struct {
	From, To calendar.Date
}

// ReportResourceSet is every active occupiable resource of the marina with its map kind and group memberships.
type ReportResourceSet struct {
	Berths   []Berth
	Areas    []DynamicArea
	Lands    []LandResource
	MapKinds map[string]string   // map id → kind
	Groups   map[string][]string // resource id → group ids
}

// ReportResources returns the active berths, dynamic areas and land resources of the marina, the kind of every
// map and the group memberships (5 queries).
func ReportResources(ctx context.Context, db bun.IDB, s MarinaScope) (*ReportResourceSet, error) {
	res, err := LoadMapResources(ctx, db, s, "", true)
	if err != nil {
		return nil, err
	}
	var maps []Map
	if err := db.NewSelect().Model(&maps).Column("id", "kind").Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	groups, err := GroupsOfResources(ctx, db, s, "")
	if err != nil {
		return nil, err
	}
	out := &ReportResourceSet{Berths: res.Berths, Areas: res.Areas, Lands: res.Lands, MapKinds: map[string]string{}, Groups: groups}
	for _, m := range maps {
		out.MapKinds[m.ID] = m.Kind
	}
	return out, nil
}

// ReportClosures returns the closures overlapping the period.
func ReportClosures(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]Closure, error) {
	out := []Closure{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("from_date <= ?", f.To).
		Where("to_date >= ?", f.From).OrderExpr("from_date, id").Scan(ctx)
	return out, err
}

// ReportNightRow is one occupancy row of a berth or land resource, or the cells of one allocation on one area
// night (Cells counts the physical rows, D2).
type ReportNightRow struct {
	ResourceID   string        `bun:"resource_id"`
	Night        calendar.Date `bun:"night"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
	Cells        int64         `bun:"cells"`
}

// ReportBerthNights returns the berth_occupancies rows of the period.
func ReportBerthNights(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportNightRow, error) {
	out := []ReportNightRow{}
	err := db.NewSelect().TableExpr("berth_occupancies").
		ColumnExpr("berth_id AS resource_id, night, allocation_id, kind, 1 AS cells").
		Where("marina_id = ?", s.ID()).Where("night >= ?", f.From).Where("night <= ?", f.To).
		OrderExpr("berth_id, night").Scan(ctx, &out)
	return out, err
}

// ReportLandNights returns the land_occupancies rows of the period.
func ReportLandNights(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportNightRow, error) {
	out := []ReportNightRow{}
	err := db.NewSelect().TableExpr("land_occupancies").
		ColumnExpr("resource_id, night, allocation_id, kind, 1 AS cells").
		Where("marina_id = ?", s.ID()).Where("night >= ?", f.From).Where("night <= ?", f.To).
		OrderExpr("resource_id, night").Scan(ctx, &out)
	return out, err
}

// ReportCellNights returns, per area, night, allocation and kind, the number of cell_occupancies rows of the
// period.
func ReportCellNights(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportNightRow, error) {
	out := []ReportNightRow{}
	err := db.NewSelect().TableExpr("cell_occupancies").
		ColumnExpr("area_id AS resource_id, night, allocation_id, kind, COUNT(*) AS cells").
		Where("marina_id = ?", s.ID()).Where("night >= ?", f.From).Where("night <= ?", f.To).
		GroupExpr("area_id, night, allocation_id, kind").OrderExpr("area_id, night, allocation_id").Scan(ctx, &out)
	return out, err
}

// ReportAllocationNight is a night held by an allocation (any family).
type ReportAllocationNight struct {
	AllocationID string        `bun:"allocation_id"`
	Night        calendar.Date `bun:"night"`
}

// ReportAllocationNights returns the nights held by the given allocations (berth, land and area rows).
func ReportAllocationNights(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]ReportAllocationNight, error) {
	out := []ReportAllocationNight{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewRaw(`SELECT allocation_id, night FROM berth_occupancies WHERE marina_id = ? AND allocation_id IN (?)
		UNION SELECT allocation_id, night FROM land_occupancies WHERE marina_id = ? AND allocation_id IN (?)
		UNION SELECT DISTINCT allocation_id, night FROM cell_occupancies WHERE marina_id = ? AND allocation_id IN (?)
		ORDER BY allocation_id, night`,
		s.ID(), bun.List(ids), s.ID(), bun.List(ids), s.ID(), bun.List(ids)).Scan(ctx, &out)
	return out, err
}

// ReportAllocationRow is an allocation with its contract (through the contract line) and its vessel.
type ReportAllocationRow struct {
	ID               string        `bun:"id"`
	Kind             string        `bun:"kind"`
	Status           string        `bun:"status"`
	ResourceType     string        `bun:"resource_type"`
	ResourceID       string        `bun:"resource_id"`
	MapID            string        `bun:"map_id"`
	XCm              *int64        `bun:"x_cm"`
	YCm              *int64        `bun:"y_cm"`
	RotationDd       *int64        `bun:"rotation_dd"`
	FirstNight       calendar.Date `bun:"first_night"`
	LastNight        calendar.Date `bun:"last_night"`
	ContractID       string        `bun:"contract_id"`
	ContractKind     string        `bun:"contract_kind"`
	ContractTerm     string        `bun:"contract_term"`
	ContractStatus   string        `bun:"contract_status"`
	ContractYear     int           `bun:"contract_year"`
	ContractNumber   int64         `bun:"contract_number"`
	ContractStartsOn calendar.Date `bun:"contract_starts_on"`
	ContractEndsOn   calendar.Date `bun:"contract_ends_on"`
	ContractTotal    int64         `bun:"contract_total_cents"`
	CustomerID       string        `bun:"customer_id"`
	VesselID         string        `bun:"vessel_id"`
	VesselName       string        `bun:"vessel_name"`
	HullType         string        `bun:"hull_type"`
	Flag             string        `bun:"flag"`
	LOACm            int64         `bun:"loa_cm"`
	BeamCm           int64         `bun:"beam_cm"`
	DraftCm          *int64        `bun:"draft_cm"`
	AirDraftCm       *int64        `bun:"air_draft_cm"`
	MeasuredLOACm    *int64        `bun:"measured_loa_cm"`
	MeasuredBeamCm   *int64        `bun:"measured_beam_cm"`
	MeasuredDraftCm  *int64        `bun:"measured_draft_cm"`
	DimensionsSource string        `bun:"dimensions_source"`
	PowerRequiredA   *int64        `bun:"power_required_a"`
}

// ReportAllocationFilter selects allocations: the active ones overlapping [From, To], or the given ids.
type ReportAllocationFilter struct {
	From, To calendar.Date
	IDs      []string
}

// ReportAllocations returns the allocations of the filter with contract and vessel.
func ReportAllocations(ctx context.Context, db bun.IDB, s MarinaScope, f ReportAllocationFilter) ([]ReportAllocationRow, error) {
	out := []ReportAllocationRow{}
	q := db.NewSelect().TableExpr("allocations AS a").
		ColumnExpr("a.id, a.kind, a.status, a.resource_type, a.resource_id, a.map_id, a.x_cm, a.y_cm, a.rotation_dd").
		ColumnExpr("a.first_night, a.last_night").
		ColumnExpr("c.id AS contract_id, COALESCE(c.kind, '') AS contract_kind, COALESCE(c.term, '') AS contract_term").
		ColumnExpr("COALESCE(c.status, '') AS contract_status, COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
		ColumnExpr("c.starts_on AS contract_starts_on, c.ends_on AS contract_ends_on, COALESCE(c.total_cents, 0) AS contract_total_cents").
		ColumnExpr("c.customer_id AS customer_id").
		ColumnExpr("a.vessel_id AS vessel_id, COALESCE(v.name, '') AS vessel_name, COALESCE(v.hull_type, '') AS hull_type").
		ColumnExpr("COALESCE(v.flag, '') AS flag, COALESCE(v.loa_cm, 0) AS loa_cm, COALESCE(v.beam_cm, 0) AS beam_cm").
		ColumnExpr("v.draft_cm, v.air_draft_cm, v.measured_loa_cm, v.measured_beam_cm, v.measured_draft_cm").
		ColumnExpr("COALESCE(v.dimensions_source, '') AS dimensions_source, v.power_required_a").
		Join("LEFT JOIN contract_lines AS cl ON cl.id = a.contract_line_id").
		Join("LEFT JOIN contracts AS c ON c.id = cl.contract_id").
		Join("LEFT JOIN vessels AS v ON v.id = a.vessel_id").
		Where("a.marina_id = ?", s.ID())
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return out, nil
		}
		q = q.Where("a.id IN (?)", bun.List(f.IDs))
	} else {
		q = q.Where("a.status = ?", "active").Where("a.first_night <= ?", f.To).Where("a.last_night >= ?", f.From)
	}
	err := q.OrderExpr("a.id").Scan(ctx, &out)
	return out, err
}

// ReportReleaseRow is an active release with its contract number.
type ReportReleaseRow struct {
	ID                 string        `bun:"id"`
	AllocationID       string        `bun:"allocation_id"`
	ContractID         string        `bun:"contract_id"`
	ContractKind       string        `bun:"contract_kind"`
	ContractYear       int           `bun:"contract_year"`
	ContractNumber     int64         `bun:"contract_number"`
	VesselID           string        `bun:"vessel_id"`
	ResourceType       string        `bun:"resource_type"`
	ResourceID         string        `bun:"resource_id"`
	FromNight          calendar.Date `bun:"from_night"`
	ToNight            calendar.Date `bun:"to_night"`
	ResoldAllocationID string        `bun:"resold_allocation_id"`
	CreditCents        int64         `bun:"credit_cents"`
}

// ReportReleases returns the active releases overlapping the period.
func ReportReleases(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportReleaseRow, error) {
	out := []ReportReleaseRow{}
	err := db.NewSelect().TableExpr("releases AS r").
		ColumnExpr("r.id, r.allocation_id, r.contract_id, c.kind AS contract_kind, c.year AS contract_year, c.number AS contract_number").
		ColumnExpr("c.vessel_id, r.resource_type, r.resource_id, r.from_night, r.to_night").
		ColumnExpr("r.resold_allocation_id AS resold_allocation_id, r.credit_cents").
		Join("JOIN contracts AS c ON c.id = r.contract_id").
		Where("r.marina_id = ?", s.ID()).Where("r.status = ?", "active").
		Where("r.from_night <= ?", f.To).Where("r.to_night >= ?", f.From).
		OrderExpr("r.from_night, r.id").Scan(ctx, &out)
	return out, err
}
