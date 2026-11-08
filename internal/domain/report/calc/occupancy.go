package calc

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
)

// Resource families (OpenAPI ReportFamily, D6): never summed together.
const (
	FamilyBerth   = "berth"
	FamilyLand    = "land"
	FamilyLinear  = "linear_area"
	FamilySurface = "surface_area"
)

// Resource types (allocations.resource_type).
const (
	TypeBerth = "berth"
	TypeArea  = "dynamic_area"
	TypeLand  = "land_resource"
)

// Occupied kinds and out-of-service kinds of occupancy rows (§3.0, D4); holds are neither.
const (
	KindContract    = "contract"
	KindTransit     = "transit"
	KindOwner       = "owner"
	KindBlock       = "block"
	KindMaintenance = "maintenance"
	KindRelease     = "release"
)

// Occupied reports whether an occupancy row kind counts as occupied.
func Occupied(kind string) bool {
	return kind == KindContract || kind == KindTransit || kind == KindOwner
}

// OutOfService reports whether an occupancy row kind removes the night from the available set.
func OutOfService(kind string) bool { return kind == KindBlock || kind == KindMaintenance }

// Resource is an active berth, dynamic area or land resource with its dimensions (current values, D5).
type Resource struct {
	ID, Code    string
	Type        string // berth, dynamic_area, land_resource
	Family      string // berth, land, linear_area, surface_area
	MapID       string
	MapKind     string
	StructureID string
	GroupIDs    []string
	Category    string
	Length      geo.Cm // berths and land
	Width       geo.Cm
	MaxLOA      geo.Cm // berths
	MaxBeam     geo.Cm
	HoldsBoat   bool   // berths, areas, cradle and trailer_spot land
	CellCm      geo.Cm // areas
	UsableCells int64  // areas: cell_count − blocked cells
}

// capacity is the number of cells of a resource (1 for berths and land).
func (r *Resource) capacity() int64 {
	if r.Type == TypeArea {
		return r.UsableCells
	}
	return 1
}

// unit is the measure of one cell: 1 berth-night, cell_cm (linear) or cell_cm² (surface).
func (r *Resource) unit() int64 {
	switch r.Family {
	case FamilyLinear:
		return int64(r.CellCm)
	case FamilySurface:
		return int64(r.CellCm) * int64(r.CellCm)
	}
	return 1
}

// Closure is a closures row (scope marina, map, structure, group or resource).
type Closure struct {
	ScopeType string // marina, map, structure, group, resource
	ScopeID   string
	From, To  calendar.Date
}

func (c *Closure) applies(r *Resource) bool {
	switch c.ScopeType {
	case "marina":
		return true
	case "map":
		return c.ScopeID == r.MapID
	case "structure":
		return r.StructureID != "" && c.ScopeID == r.StructureID
	case "group":
		return slices.Contains(r.GroupIDs, c.ScopeID)
	case "resource":
		return c.ScopeID == r.ID
	}
	return false
}

// NightRow is one occupancy row (berths, land) or the cells of one allocation on one area night (D2).
type NightRow struct {
	ResourceID   string
	Night        calendar.Date
	AllocationID string
	Kind         string
	Cells        int64 // 1 for berths and land
}

// Allocation is an allocation referenced by the rows, with its contract and vessel (effective size, D7).
type Allocation struct {
	ID             string
	Kind           string
	ResourceID     string
	ContractID     string
	ContractNumber string
	ContractKind   string // mooring, transit, storage, charter
	VesselID       string
	VesselName     string
	HasVessel      bool
	LOA, Beam      geo.Cm
	Footprint      geo.Cm // linear areas: footprint width along the axis incl. margin (engine.FootprintWidth)
	HullType       string
	Flag           string
	Contract       ContractValue // the contract value (berth revenue, applied price)
	Qualifying     bool          // the contract has a qualifying status (§3.6)
}

// Release is an active release of nights with the cells it frees (1 for berths and land).
type Release struct {
	ID                 string
	AllocationID       string
	ResourceID         string
	ResoldAllocationID string
	ContractID         string
	ContractNumber     string
	From, To           calendar.Date
	Cells              int64
}

// OccupancyInput is the input of BuildOccupancy and BuildEfficiency (§3.1, §3.2).
type OccupancyInput struct {
	From, To     calendar.Date
	Variant      string // physical (default), contractual
	Family       string // berth (default), land, linear_area, surface_area
	ResourceType string
	Dims         Dims
	GroupBy      string
	Resources    []Resource
	Closures     []Closure
	Rows         []NightRow
	Allocations  []Allocation
	Releases     []Release
}

func (in *OccupancyInput) family() string {
	if in.Family == "" {
		return FamilyBerth
	}
	return in.Family
}

// Selected filters the resources by family, resource type and dimensions.
func (in *OccupancyInput) selected() []*Resource {
	fam := in.family()
	var out []*Resource
	for i := range in.Resources {
		r := &in.Resources[i]
		if r.Family != fam || (in.ResourceType != "" && r.Type != in.ResourceType) || !matchDims(r, in.Dims) {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *Resource) int {
		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

func matchDims(r *Resource, d Dims) bool {
	return (d.MapID == "" || r.MapID == d.MapID) && (d.MapKind == "" || r.MapKind == d.MapKind) &&
		(d.StructureID == "" || r.StructureID == d.StructureID) &&
		(d.GroupID == "" || slices.Contains(r.GroupIDs, d.GroupID)) && (d.Category == "" || r.Category == d.Category)
}

// UnitOf is the unit of a family: berth-nights, cm-nights or cm²-nights.
func UnitOf(family string) Unit {
	switch family {
	case FamilyLinear:
		return UnitCmNight
	case FamilySurface:
		return UnitCm2Night
	}
	return UnitBerthNight
}

// grid is the per-night state of the selected resources.
type grid struct {
	from    calendar.Date
	n       int
	res     []*Resource
	idx     map[string]int
	closed  [][]bool
	oos     [][]int64
	occ     [][]int64                 // occupied cells per resource and night (available nights only)
	byAlloc []map[string][]allocNight // resource → allocation → nights
	allocs  map[string]*Allocation
	cellsOf map[string]map[int]int64 // allocation → night index → cells (resold allocations)
}

type allocNight struct {
	i     int
	cells int64
}

func buildGrid(in *OccupancyInput) *grid {
	g := &grid{
		from: in.From, n: in.From.DaysUntil(in.To) + 1, res: in.selected(), idx: map[string]int{},
		allocs: map[string]*Allocation{}, cellsOf: map[string]map[int]int64{},
	}
	for i := range in.Allocations {
		g.allocs[in.Allocations[i].ID] = &in.Allocations[i]
	}
	g.closed = make([][]bool, len(g.res))
	g.oos = make([][]int64, len(g.res))
	g.occ = make([][]int64, len(g.res))
	g.byAlloc = make([]map[string][]allocNight, len(g.res))
	for i, r := range g.res {
		g.idx[r.ID] = i
		g.closed[i] = make([]bool, g.n)
		g.oos[i] = make([]int64, g.n)
		g.occ[i] = make([]int64, g.n)
		g.byAlloc[i] = map[string][]allocNight{}
		for c := range in.Closures {
			cl := &in.Closures[c]
			if !cl.applies(r) {
				continue
			}
			for d := maxDate(cl.From, in.From); !d.After(cl.To) && !d.After(in.To); d = d.AddDays(1) {
				g.closed[i][in.From.DaysUntil(d)] = true
			}
		}
	}
	for _, row := range in.Rows {
		ri, ok := g.idx[row.ResourceID]
		if !ok || !inPeriod(row.Night, in.From, in.To) {
			continue
		}
		di := in.From.DaysUntil(row.Night)
		if OutOfService(row.Kind) {
			g.oos[ri][di] += row.Cells
		}
	}
	for _, row := range in.Rows {
		ri, ok := g.idx[row.ResourceID]
		if !ok || !inPeriod(row.Night, in.From, in.To) || !Occupied(row.Kind) {
			continue
		}
		di := in.From.DaysUntil(row.Night)
		m := g.cellsOf[row.AllocationID]
		if m == nil {
			m = map[int]int64{}
			g.cellsOf[row.AllocationID] = m
		}
		m[di] += row.Cells
		if g.closed[ri][di] || g.avail(ri, di) == 0 {
			continue
		}
		g.occ[ri][di] += row.Cells
		g.byAlloc[ri][row.AllocationID] = append(g.byAlloc[ri][row.AllocationID], allocNight{i: di, cells: row.Cells})
	}
	return g
}

// avail is the number of available cells of resource ri on night di.
func (g *grid) avail(ri, di int) int64 {
	if g.closed[ri][di] {
		return 0
	}
	return max(0, g.res[ri].capacity()-g.oos[ri][di])
}

func (g *grid) day(di int) calendar.Date { return g.from.AddDays(di) }

func maxDate(a, b calendar.Date) calendar.Date {
	if a.After(b) {
		return a
	}
	return b
}

// releasedCells returns, per release record, the cells added by the contractual variant on each night: the
// release footprint minus the cells its resale already occupies, within the free capacity (union, never
// beyond the available cells).
func (g *grid) releasedCells(in *OccupancyInput) map[string][]allocNight {
	out := map[string][]allocNight{}
	extra := make([][]int64, len(g.res))
	for _, rel := range in.Releases {
		ri, ok := g.idx[rel.ResourceID]
		if !ok {
			continue
		}
		if extra[ri] == nil {
			extra[ri] = make([]int64, g.n)
		}
		cells := max(rel.Cells, 1)
		for d := maxDate(rel.From, in.From); !d.After(rel.To) && !d.After(in.To); d = d.AddDays(1) {
			di := in.From.DaysUntil(d)
			free := g.avail(ri, di) - g.occ[ri][di] - extra[ri][di]
			add := cells - g.cellsOf[rel.ResoldAllocationID][di]
			if rel.ResoldAllocationID == "" {
				add = cells
			}
			add = min(add, free)
			if add <= 0 {
				continue
			}
			extra[ri][di] += add
			out[rel.ID] = append(out[rel.ID], allocNight{i: di, cells: add})
		}
	}
	return out
}

// refOfAllocation links an occupancy record to its contract, transit or allocation.
func refOfAllocation(a *Allocation) Ref {
	switch {
	case a == nil:
		return Ref{}
	case a.ContractID != "" && a.ContractKind == "transit":
		return Ref{Type: "transit", ID: a.ContractID, Label: a.ContractNumber}
	case a.ContractID != "":
		return Ref{Type: "contract", ID: a.ContractID, Label: a.ContractNumber}
	}
	return Ref{Type: "allocation", ID: a.ID, Label: a.VesselName}
}

func refOfResource(r *Resource) Ref {
	t := map[string]string{TypeBerth: "berth", TypeArea: "area", TypeLand: "land_resource"}[r.Type]
	return Ref{Type: t, ID: r.ID, Label: r.Code}
}

// BuildOccupancy builds R1 (§3.1): the records of the numerator (resource × allocation, and the releases of the
// contractual variant) and of the denominator (one per resource); Totals are the fold of the records.
func BuildOccupancy(in OccupancyInput) (OccupancyResult, Facts) {
	g := buildGrid(&in)
	unit := UnitOf(in.family())
	daily := Daily{}
	type nightTotals struct{ occ, avail, closed, oos, released int64 }
	perNight := make([]nightTotals, g.n)
	var recs []OccupancyRecord
	for ri, r := range g.res {
		u := r.unit()
		den := OccupancyRecord{
			Role: "denominator", ResourceID: r.ID, ResourceCode: r.Code, ResourceType: r.Type,
			MapID: r.MapID, Category: r.Category, Ref: refOfResource(r),
		}
		for di := 0; di < g.n; di++ {
			a := g.avail(ri, di)
			den.AvailableUnits += a * u
			perNight[di].avail += a * u
			switch {
			case g.closed[ri][di]:
				den.ClosedUnits += r.capacity() * u
				perNight[di].closed += r.capacity() * u
			default:
				o := min(g.oos[ri][di], r.capacity()) * u
				den.OutOfServiceUnits += o
				perNight[di].oos += o
			}
		}
		recs = append(recs, den)
		for aid, ns := range g.byAlloc[ri] {
			a := g.allocs[aid]
			rec := OccupancyRecord{
				Role: "numerator", ResourceID: r.ID, ResourceCode: r.Code, ResourceType: r.Type,
				MapID: r.MapID, Category: r.Category, AllocationID: aid, Ref: refOfAllocation(a),
			}
			if a != nil {
				rec.Kind, rec.ContractID, rec.ContractNumber = a.Kind, a.ContractID, a.ContractNumber
			}
			for _, n := range ns {
				rec.OccupiedUnits += n.cells * u
				perNight[n.i].occ += n.cells * u
			}
			recs = append(recs, rec)
		}
	}
	if in.Variant == "contractual" {
		rel := g.releasedCells(&in)
		for _, rl := range in.Releases {
			ns, ok := rel[rl.ID]
			if !ok {
				continue
			}
			r := g.res[g.idx[rl.ResourceID]]
			u := r.unit()
			rec := OccupancyRecord{
				Role: "numerator", ResourceID: r.ID, ResourceCode: r.Code, ResourceType: r.Type,
				MapID: r.MapID, Category: r.Category, Kind: KindRelease, AllocationID: rl.AllocationID,
				ContractID: rl.ContractID, ContractNumber: rl.ContractNumber, Ref: Ref{Type: "release", ID: rl.ID, Label: rl.ContractNumber},
			}
			for _, n := range ns {
				rec.OccupiedUnits += n.cells * u
				perNight[n.i].occ += n.cells * u
				perNight[n.i].released += n.cells * u
			}
			recs = append(recs, rec)
		}
	}
	sortOccupancy(recs)
	for di, t := range perNight {
		daily.Add(g.day(di), t.occ, t.avail)
	}
	res := OccupancyResult{Variant: variantOr(in.Variant), Family: in.family()}
	res.Records = recs
	res.Totals = FoldOccupancy(unit, recs)
	switch in.GroupBy {
	case "month", "week", "night":
		gr := map[string]*OccupancyTotals{}
		var keys []string
		for di, t := range perNight {
			k := timeKey(g.day(di), in.GroupBy)
			tt, ok := gr[k]
			if !ok {
				tt = &OccupancyTotals{Unit: unit}
				gr[k] = tt
				keys = append(keys, k)
			}
			tt.OccupiedUnits += t.occ
			tt.AvailableUnits += t.avail
			tt.ClosedUnits += t.closed
			tt.OutOfServiceUnits += t.oos
			tt.ReleasedUnits += t.released
		}
		for _, k := range keys {
			tt := gr[k]
			tt.OccupancyBP = Ratio(tt.OccupiedUnits, tt.AvailableUnits)
			res.Groups = append(res.Groups, Group[OccupancyTotals]{Key: k, Label: k, Totals: *tt})
		}
	case "":
	default:
		byID := resourceIndex(g.res)
		res.Groups = GroupRecords(recs, func(r OccupancyRecord) []string { return dimKeys(byID[r.ResourceID], in.GroupBy) },
			func(rs []OccupancyRecord) OccupancyTotals { return FoldOccupancy(unit, rs) })
		labelGroups(res.Groups, byID, in.GroupBy)
	}
	return res, Facts{IndOccupancy: daily}
}

func variantOr(v string) string {
	if v == "" {
		return "physical"
	}
	return v
}

// FoldOccupancy is the only producer of occupancy totals: Σ numerator and denominator records (INV-62).
func FoldOccupancy(unit Unit, recs []OccupancyRecord) OccupancyTotals {
	t := OccupancyTotals{Unit: unit}
	for _, r := range recs {
		if r.Role == "numerator" {
			t.OccupiedUnits += r.OccupiedUnits
			if r.Kind == KindRelease {
				t.ReleasedUnits += r.OccupiedUnits
			}
			continue
		}
		t.AvailableUnits += r.AvailableUnits
		t.ClosedUnits += r.ClosedUnits
		t.OutOfServiceUnits += r.OutOfServiceUnits
	}
	t.OccupancyBP = Ratio(t.OccupiedUnits, t.AvailableUnits)
	return t
}

func sortOccupancy(recs []OccupancyRecord) {
	slices.SortStableFunc(recs, func(a, b OccupancyRecord) int {
		return compareKeys([]string{a.ResourceCode, a.ResourceID, a.Kind, a.ContractNumber, a.AllocationID},
			[]string{b.ResourceCode, b.ResourceID, b.Kind, b.ContractNumber, b.AllocationID})
	})
}

func timeKey(d calendar.Date, by string) string {
	switch by {
	case "month":
		return MonthKey(d)
	case "week":
		return WeekKey(d)
	}
	return d.String()
}

func resourceIndex(rs []*Resource) map[string]*Resource {
	m := make(map[string]*Resource, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// dimKeys are the group keys of a resource for a dimension group_by ("none" when the dimension is empty).
func dimKeys(r *Resource, by string) []string {
	if r == nil {
		return nil
	}
	orNone := func(s string) []string {
		if s == "" {
			return []string{"none"}
		}
		return []string{s}
	}
	switch by {
	case "resource":
		return []string{r.ID}
	case "structure":
		return orNone(r.StructureID)
	case "group":
		if len(r.GroupIDs) == 0 {
			return []string{"none"}
		}
		return r.GroupIDs
	case "category":
		return orNone(r.Category)
	case "map":
		return []string{r.MapID}
	}
	return nil
}

// labelGroups labels resource groups with the resource code (other dimensions keep their key; the client
// resolves names).
func labelGroups[T any](gs []Group[T], byID map[string]*Resource, by string) {
	if by != "resource" {
		return
	}
	for i := range gs {
		if r := byID[gs[i].Key]; r != nil {
			gs[i].Label = r.Code
		}
	}
}

// ---- R2 efficiency ----

// BuildEfficiency builds R2 (§3.2): per resource × allocation on physical occupied nights, the vessel units over
// the resource units (cm²-nights; linear areas: cm-nights).
func BuildEfficiency(in OccupancyInput) (EfficiencyResult, Facts) {
	g := buildGrid(&in)
	fam := in.family()
	unit := UnitCm2Night
	if fam == FamilyLinear {
		unit = UnitCmNight
	}
	daily := Daily{}
	type nt struct{ nights, v, r int64 }
	perNight := make([]nt, g.n)
	var recs []EfficiencyRecord
	noVessel := map[string]bool{}
	for ri, res := range g.res {
		if !res.HoldsBoat {
			continue
		}
		for aid, ns := range g.byAlloc[ri] {
			a := g.allocs[aid]
			if a == nil || !a.HasVessel || a.LOA <= 0 || a.Beam <= 0 {
				noVessel[aid] = true
				continue
			}
			rec := EfficiencyRecord{
				ResourceID: res.ID, ResourceCode: res.Code, ResourceType: res.Type, AllocationID: aid,
				Kind: a.Kind, ContractID: a.ContractID, ContractNumber: a.ContractNumber, VesselID: a.VesselID,
				VesselName: a.VesselName, LOA: a.LOA, Beam: a.Beam, Ref: refOfAllocation(a),
			}
			for _, n := range ns {
				var v, r int64
				switch fam {
				case FamilyLinear:
					v, r = int64(a.Footprint), n.cells*int64(res.CellCm)
				case FamilySurface:
					v, r = int64(a.LOA)*int64(a.Beam), n.cells*int64(res.CellCm)*int64(res.CellCm)
				default:
					v, r = int64(a.LOA)*int64(a.Beam), int64(res.Length)*int64(res.Width)
				}
				rec.Nights++
				rec.VesselUnits += v
				rec.ResourceUnits += r
				perNight[n.i].nights++
				perNight[n.i].v += v
				perNight[n.i].r += r
			}
			rec.EfficiencyBP = Ratio(rec.VesselUnits, rec.ResourceUnits)
			recs = append(recs, rec)
		}
	}
	slices.SortStableFunc(recs, func(a, b EfficiencyRecord) int {
		return compareKeys([]string{a.ResourceCode, a.ResourceID, a.ContractNumber, a.AllocationID},
			[]string{b.ResourceCode, b.ResourceID, b.ContractNumber, b.AllocationID})
	})
	for di, t := range perNight {
		daily.Add(g.day(di), t.v, t.r)
	}
	res := EfficiencyResult{Family: fam}
	res.Records = recs
	res.Totals = FoldEfficiency(unit, recs)
	if len(noVessel) > 0 {
		res.Notes = append(res.Notes, Note{Code: "no_vessel", Count: int64(len(noVessel))})
	}
	switch in.GroupBy {
	case "month", "week", "night":
		gr := map[string]*EfficiencyTotals{}
		var keys []string
		for di, t := range perNight {
			k := timeKey(g.day(di), in.GroupBy)
			tt, ok := gr[k]
			if !ok {
				tt = &EfficiencyTotals{Unit: unit}
				gr[k] = tt
				keys = append(keys, k)
			}
			tt.Nights += t.nights
			tt.VesselUnits += t.v
			tt.ResourceUnits += t.r
		}
		for _, k := range keys {
			tt := gr[k]
			tt.EfficiencyBP = Ratio(tt.VesselUnits, tt.ResourceUnits)
			res.Groups = append(res.Groups, Group[EfficiencyTotals]{Key: k, Label: k, Totals: *tt})
		}
	case "":
	default:
		byID := resourceIndex(g.res)
		res.Groups = GroupRecords(recs, func(r EfficiencyRecord) []string { return dimKeys(byID[r.ResourceID], in.GroupBy) },
			func(rs []EfficiencyRecord) EfficiencyTotals { return FoldEfficiency(unit, rs) })
		labelGroups(res.Groups, byID, in.GroupBy)
	}
	return res, Facts{IndEfficiency: daily}
}

// FoldEfficiency is the only producer of efficiency totals.
func FoldEfficiency(unit Unit, recs []EfficiencyRecord) EfficiencyTotals {
	t := EfficiencyTotals{Unit: unit}
	for _, r := range recs {
		t.Nights += r.Nights
		t.VesselUnits += r.VesselUnits
		t.ResourceUnits += r.ResourceUnits
	}
	t.EfficiencyBP = Ratio(t.VesselUnits, t.ResourceUnits)
	return t
}

// ClosedNights returns the nights of [from, to] on which r is closed.
func ClosedNights(r *Resource, cs []Closure, from, to calendar.Date) []calendar.Date {
	var out []calendar.Date
	seen := map[calendar.Date]bool{}
	for i := range cs {
		c := &cs[i]
		if !c.applies(r) {
			continue
		}
		for d := maxDate(c.From, from); !d.After(c.To) && !d.After(to); d = d.AddDays(1) {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	slices.SortFunc(out, cmpDate)
	return out
}
