package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read helpers of the occupancy engine: resources with their fit inputs, the search snapshot (a fixed number of
// queries regardless of the harbour size, m3-plan §3.6) and the day view (m3-plan §8.3). Nothing here returns
// names or contact data except DayVesselNames, which the service gates.

// BerthByID returns a berth of the marina.
func BerthByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Berth, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	b := new(Berth)
	if err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return b, nil
}

// DynamicAreaByID returns a dynamic area of the marina.
func DynamicAreaByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*DynamicArea, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	a := new(DynamicArea)
	if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// LandResourceByID returns a land resource of the marina.
func LandResourceByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*LandResource, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	l := new(LandResource)
	if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return l, nil
}

// MapResources are the occupiable elements of maps.
type MapResources struct {
	Berths []Berth
	Areas  []DynamicArea
	Lands  []LandResource
}

// LoadMapResources returns the berths, areas and land resources of a map ("" = every map of the marina), in
// code order; activeOnly drops the deactivated ones.
func LoadMapResources(ctx context.Context, db bun.IDB, s MarinaScope, mapID string, activeOnly bool) (*MapResources, error) {
	out := &MapResources{Berths: []Berth{}, Areas: []DynamicArea{}, Lands: []LandResource{}}
	scope := func(q *bun.SelectQuery) *bun.SelectQuery {
		q = q.Where("marina_id = ?", s.ID())
		if mapID != "" {
			q = q.Where("map_id = ?", mapID)
		}
		if activeOnly {
			q = q.Where("active = ?", true)
		}
		return q.OrderExpr("code, id")
	}
	if err := scope(db.NewSelect().Model(&out.Berths)).Scan(ctx); err != nil {
		return nil, err
	}
	if err := scope(db.NewSelect().Model(&out.Areas)).Scan(ctx); err != nil {
		return nil, err
	}
	if err := scope(db.NewSelect().Model(&out.Lands)).Scan(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// GroupsOfResources returns the group ids of every grouped resource of the marina (or of one map), keyed by
// resource id.
func GroupsOfResources(ctx context.Context, db bun.IDB, s MarinaScope, mapID string) (map[string][]string, error) {
	var rows []BerthGroupMember
	q := db.NewSelect().Model(&rows).Column("resource_id", "group_id").Where("marina_id = ?", s.ID())
	if mapID != "" {
		q = q.Where("map_id = ?", mapID)
	}
	if err := q.OrderExpr("resource_id, group_id").Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.ResourceID] = append(out[r.ResourceID], r.GroupID)
	}
	return out, nil
}

// StructureParents returns the parent structure id of every structure of the marina that has one.
func StructureParents(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	var rows []HarborStructure
	if err := db.NewSelect().Model(&rows).Column("id", "parent_id").Where("marina_id = ?", s.ID()).Where("parent_id IS NOT NULL").Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.ID] = r.ParentID
	}
	return out, nil
}

// InsuranceUntil returns, per vessel, the latest expiry of its insurance_policy documents; a policy without an
// expiry is valid indefinitely (9999-12-31). Vessels with no policy are absent.
func InsuranceUntil(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string) (map[string]calendar.Date, error) {
	out := map[string]calendar.Date{}
	vesselIDs = validIDs(vesselIDs)
	if len(vesselIDs) == 0 {
		return out, nil
	}
	var docs []Document
	err := db.NewSelect().Model(&docs).Column("vessel_id", "expires_on").Where("marina_id = ?", s.ID()).
		Where("type = ?", "insurance_policy").Where("vessel_id IN (?)", bun.List(vesselIDs)).Scan(ctx)
	if err != nil {
		return nil, err
	}
	forever := calendar.New(9999, 12, 31)
	for _, d := range docs {
		exp := d.ExpiresOn
		if exp.IsZero() {
			exp = forever
		}
		if cur, ok := out[d.VesselID]; !ok || exp.After(cur) {
			out[d.VesselID] = exp
		}
	}
	return out, nil
}

// DocumentExpiry is the earliest document expiry of a vessel or customer (any document type).
type DocumentExpiry struct {
	OwnerID  string
	Earliest calendar.Date
}

// EarliestDocumentExpiries returns, per vessel and per customer id, the earliest expires_on of their documents.
func EarliestDocumentExpiries(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs, customerIDs []string) (map[string]calendar.Date, error) {
	out := map[string]calendar.Date{}
	vesselIDs, customerIDs = validIDs(vesselIDs), validIDs(customerIDs)
	if len(vesselIDs) == 0 && len(customerIDs) == 0 {
		return out, nil
	}
	var docs []Document
	q := db.NewSelect().Model(&docs).Column("vessel_id", "customer_id", "expires_on").Where("marina_id = ?", s.ID()).
		Where("expires_on IS NOT NULL")
	switch {
	case len(vesselIDs) > 0 && len(customerIDs) > 0:
		q = q.Where("(vessel_id IN (?) OR customer_id IN (?))", bun.List(vesselIDs), bun.List(customerIDs))
	case len(vesselIDs) > 0:
		q = q.Where("vessel_id IN (?)", bun.List(vesselIDs))
	default:
		q = q.Where("customer_id IN (?)", bun.List(customerIDs))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	for _, d := range docs {
		for _, owner := range []string{d.VesselID, d.CustomerID} {
			if owner == "" {
				continue
			}
			if cur, ok := out[owner]; !ok || d.ExpiresOn.Before(cur) {
				out[owner] = d.ExpiresOn
			}
		}
	}
	return out, nil
}

// SearchResource is a berth or land resource of the search snapshot (one UNION query).
type SearchResource struct {
	Type            string  `bun:"type"`
	ID              string  `bun:"id"`
	Code            string  `bun:"code"`
	MapID           string  `bun:"map_id"`
	StructureID     string  `bun:"structure_id"`
	LengthCm        int64   `bun:"length_cm"`
	WidthCm         int64   `bun:"width_cm"`
	MaxLOACm        int64   `bun:"max_loa_cm"`
	MaxBeamCm       int64   `bun:"max_beam_cm"`
	DepthCm         int64   `bun:"depth_cm"`
	MaxAirDraftCm   int64   `bun:"max_air_draft_cm"`
	OverhangAllowed bool    `bun:"overhang_allowed"`
	PowerA          int64   `bun:"power_a"`
	Water           bool    `bun:"water"`
	Attributes      Strings `bun:"attributes"`
	OnlineSellable  bool    `bun:"online_sellable"`
	LandKind        string  `bun:"land_kind"`
}

// SnapshotRow is an occupancy row of the snapshot: resource, cell (0 for berths and land), night, owner.
type SnapshotRow struct {
	Type         string        `bun:"type"`
	Resource     string        `bun:"resource"`
	Cell         int           `bun:"cell"`
	Night        calendar.Date `bun:"night"`
	AllocationID string        `bun:"allocation_id"`
	Kind         string        `bun:"kind"`
}

// OutCandidateRow is an active contract allocation on a berth with the presence state of its vessel and the
// release policy of its contract.
type OutCandidateRow struct {
	AllocationID     string        `bun:"allocation_id"`
	ResourceID       string        `bun:"resource_id"`
	ContractID       string        `bun:"contract_id"`
	FirstNight       calendar.Date `bun:"first_night"`
	LastNight        calendar.Date `bun:"last_night"`
	State            string        `bun:"state"`
	Since            time.Time     `bun:"since"`
	ExpectedReturnOn calendar.Date `bun:"expected_return_on"`
	Policy           JSON          `bun:"policy"`
}

// SearchSnapshot is the raw data of a search (m3-plan §3.6).
type SearchSnapshot struct {
	Resources []SearchResource
	Areas     []DynamicArea
	Groups    map[string][]string
	Rows      []SnapshotRow // berths and land: [first, last]; cells of dynamic areas
	Closures  []Closure
	Out       []OutCandidateRow
	Previous  string // last resource of the vessel
}

// LoadSearchSnapshot reads the search snapshot of [first, last] with at most 8 queries: berths and land
// resources (one UNION), linear areas, group members, berth and land rows (one UNION), cell rows, closures
// covering first, contract allocations whose vessel is out (only when considerOut) and the vessel's previous
// resource (only with a vessel). Rows of holds expired before now are left out (INV-05).
func LoadSearchSnapshot(ctx context.Context, db bun.IDB, s MarinaScope, first, last calendar.Date, now time.Time, considerOut bool, vesselID string) (*SearchSnapshot, error) {
	out := &SearchSnapshot{}
	m := s.ID()
	err := db.NewRaw(`SELECT 'berth' AS type, id, code, map_id, coalesce(CAST(structure_id AS TEXT), '') AS structure_id, length_cm, width_cm,
			max_loa_cm, max_beam_cm, coalesce(depth_cm, 0) AS depth_cm, coalesce(max_air_draft_cm, 0) AS max_air_draft_cm,
			overhang_allowed, coalesce(power_a, 0) AS power_a, water, attributes, online_sellable, '' AS land_kind
		FROM berths WHERE marina_id = ? AND active = ?
		UNION ALL
		SELECT 'land_resource' AS type, id, code, map_id, '' AS structure_id, length_cm, width_cm, length_cm AS max_loa_cm,
			width_cm AS max_beam_cm, 0 AS depth_cm, 0 AS max_air_draft_cm, ? AS overhang_allowed, 0 AS power_a, ? AS water,
			attributes, ? AS online_sellable, kind AS land_kind
		FROM land_resources WHERE marina_id = ? AND active = ?
		ORDER BY code, id`, m, true, false, false, false, m, true).Scan(ctx, &out.Resources)
	if err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Areas).Where("marina_id = ?", m).Where("active = ?", true).
		Where("kind = ?", "linear").OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, err
	}
	if out.Groups, err = GroupsOfResources(ctx, db, s, ""); err != nil {
		return nil, err
	}
	nowUTC := UTC(now)
	live := "(kind <> 'hold' OR expires_at >= ?)"
	var rows []SnapshotRow
	err = db.NewRaw(`SELECT 'berth' AS type, berth_id AS resource, 0 AS cell, night, allocation_id, kind FROM berth_occupancies
			WHERE marina_id = ? AND night >= ? AND night <= ? AND `+live+`
		UNION ALL
		SELECT 'land_resource' AS type, resource_id AS resource, 0 AS cell, night, allocation_id, kind FROM land_occupancies
			WHERE marina_id = ? AND night >= ? AND night <= ? AND `+live+`
		ORDER BY resource, night`, m, first, last, nowUTC, m, first, last, nowUTC).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out.Rows = rows
	var cells []SnapshotRow
	err = db.NewSelect().TableExpr("cell_occupancies").ColumnExpr("'dynamic_area' AS type, area_id AS resource, cell, night, allocation_id, kind").
		Where("marina_id = ?", m).Where("night >= ?", first).Where("night <= ?", last).Where(live, nowUTC).
		OrderExpr("area_id, night, cell").Scan(ctx, &cells)
	if err != nil {
		return nil, err
	}
	out.Rows = append(out.Rows, cells...)
	if out.Closures, err = ListClosures(ctx, db, s, first, first); err != nil {
		return nil, err
	}
	if considerOut {
		err = db.NewRaw(`SELECT a.id AS allocation_id, a.resource_id, l.contract_id, a.first_night, a.last_night, vp.state,
				vp.since, vp.expected_return_on, c.policy
			FROM allocations AS a
			JOIN contract_lines AS l ON l.id = a.contract_line_id
			JOIN contracts AS c ON c.id = l.contract_id
			JOIN vessel_presence AS vp ON vp.vessel_id = a.vessel_id
			WHERE a.marina_id = ? AND a.status = ? AND a.kind = ? AND a.resource_type = ? AND a.first_night <= ?
				AND a.last_night >= ? AND vp.state = ?`,
			m, AllocActive, AllocKindContract, ResourceBerth, first, last, "out").Scan(ctx, &out.Out)
		if err != nil {
			return nil, err
		}
	}
	if vesselID != "" && ValidID(vesselID) {
		var prev []string
		err = db.NewSelect().Model((*Allocation)(nil)).Column("resource_id").Where("marina_id = ?", m).Where("vessel_id = ?", vesselID).
			Where("status = ?", AllocActive).Where("kind <> ?", AllocKindHold).OrderExpr("last_night DESC, id DESC").Limit(1).Scan(ctx, &prev)
		if err != nil {
			return nil, err
		}
		if len(prev) > 0 {
			out.Previous = prev[0]
		}
	}
	return out, nil
}

// DayAllocation is an active allocation of a map covering a night, with its contract when it has one.
type DayAllocation struct {
	Allocation     `bun:",extend"`
	ContractID     string        `bun:"contract_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractTerm   string        `bun:"contract_term"`
	ContractStatus string        `bun:"contract_status"`
	ContractNumber int64         `bun:"contract_number"`
	ContractYear   int           `bun:"contract_year"`
	CustomerID     string        `bun:"customer_id"`
	StartsOn       calendar.Date `bun:"starts_on"`
	EndsOn         calendar.Date `bun:"ends_on"`
	HoldLost       bool          `bun:"hold_lost"`
	ArrivalTime    string        `bun:"expected_arrival_time"`
}

// DayAllocations returns the active allocations of a map (or of the marina when mapID is "") intersecting
// [from, to], with their contract (left join), ordered by resource and first night.
func DayAllocations(ctx context.Context, db bun.IDB, s MarinaScope, mapID string, from, to calendar.Date, resourceIDs []string) ([]DayAllocation, error) {
	out := []DayAllocation{}
	q := dayAllocationQuery(db, s, &out).Where("allocation.status = ?", AllocActive).
		Where("allocation.first_night <= ?", to).Where("allocation.last_night >= ?", from)
	if mapID != "" {
		q = q.Where("allocation.map_id = ?", mapID)
	}
	if resourceIDs != nil {
		ids := validIDs(resourceIDs)
		if len(ids) == 0 {
			return out, nil
		}
		q = q.Where("allocation.resource_id IN (?)", bun.List(ids))
	}
	err := q.OrderExpr("allocation.resource_type, allocation.resource_id, allocation.first_night, allocation.id").Scan(ctx)
	return out, err
}

// DayAllocationsByIDs returns allocations (any status) by id with their contract.
func DayAllocationsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]DayAllocation, error) {
	out := []DayAllocation{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := dayAllocationQuery(db, s, &out).Where("allocation.id IN (?)", bun.List(ids)).OrderExpr("allocation.id").Scan(ctx)
	return out, err
}

func dayAllocationQuery(db bun.IDB, s MarinaScope, out *[]DayAllocation) *bun.SelectQuery {
	return db.NewSelect().Model(out).ColumnExpr("allocation.*").
		ColumnExpr("coalesce(CAST(c.id AS TEXT), '') AS contract_id, coalesce(c.kind, '') AS contract_kind, coalesce(c.term, '') AS contract_term").
		ColumnExpr("coalesce(c.status, '') AS contract_status, coalesce(c.number, 0) AS contract_number, coalesce(c.year, 0) AS contract_year").
		ColumnExpr("coalesce(CAST(c.customer_id AS TEXT), '') AS customer_id, c.starts_on, c.ends_on, coalesce(c.hold_lost, ?) AS hold_lost", false).
		ColumnExpr("coalesce(c.expected_arrival_time, '') AS expected_arrival_time").
		Join("LEFT JOIN contract_lines AS l ON l.id = allocation.contract_line_id").
		Join("LEFT JOIN contracts AS c ON c.id = l.contract_id").
		Where("allocation.marina_id = ?", s.ID())
}

// ArrivedPresenceAllocations returns the ids of the allocations among ids that have an arrived presence.
func ArrivedPresenceAllocations(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var got []string
	err := db.NewSelect().Model((*Presence)(nil)).ColumnExpr("DISTINCT allocation_id").Where("marina_id = ?", s.ID()).
		Where("event = ?", "arrived").Where("allocation_id IN (?)", bun.List(ids)).Scan(ctx, &got)
	for _, id := range got {
		out[id] = true
	}
	return out, err
}

// InPortVessels returns the presence rows of the marina in state in_port.
func InPortVessels(ctx context.Context, db bun.IDB, s MarinaScope) ([]VesselPresence, error) {
	return VesselsInState(ctx, db, s, "in_port")
}

// ContractsOfAllocations returns, per allocation id, its contract id (allocations without a line are absent).
func ContractsOfAllocations(ctx context.Context, db bun.IDB, s MarinaScope, allocIDs []string) (map[string]string, error) {
	out := map[string]string{}
	allocIDs = validIDs(allocIDs)
	if len(allocIDs) == 0 {
		return out, nil
	}
	type row struct {
		ID         string `bun:"id"`
		ContractID string `bun:"contract_id"`
	}
	var rows []row
	err := db.NewSelect().TableExpr("allocations AS a").Join("JOIN contract_lines AS l ON l.id = a.contract_line_id").
		ColumnExpr("a.id, l.contract_id").Where("a.marina_id = ?", s.ID()).Where("a.id IN (?)", bun.List(allocIDs)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.ID] = r.ContractID
	}
	return out, err
}

// OccupancyRowsOfMarina returns every occupancy row of a marina (invariant checks), ordered by allocation.
func OccupancyRowsOfMarina(ctx context.Context, db bun.IDB, marinaID string) ([]SnapshotRow, error) {
	var out []SnapshotRow
	err := db.NewRaw(`SELECT 'berth' AS type, berth_id AS resource, 0 AS cell, night, allocation_id, kind FROM berth_occupancies WHERE marina_id = ?
		UNION ALL SELECT 'dynamic_area' AS type, area_id AS resource, cell, night, allocation_id, kind FROM cell_occupancies WHERE marina_id = ?
		UNION ALL SELECT 'land_resource' AS type, resource_id AS resource, 0 AS cell, night, allocation_id, kind FROM land_occupancies WHERE marina_id = ?
		ORDER BY allocation_id, night, cell`, marinaID, marinaID, marinaID).Scan(ctx, &out)
	return out, err
}
