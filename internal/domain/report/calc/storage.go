package calc

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// ---- R-storage (m9-plan §11, D30) ----
//
// Surfaces are measured in cm²-nights inside the builder (exact integers: cells × cell_cm² per night, LOA × beam
// per night); the API carries m²-nights in milli (cm² / 10, half-even), converted at every level from the exact
// sums (CmToM2Milli), so the occupancy ratio never depends on the rounding.

// IndStorageOccupancy and the other M9 indicator keys (OpenAPI TileKey).
const (
	IndStorageOccupancy    = string(TileStorageOccupancy)
	IndStorageExitsBlocked = string(TileStorageExitsBlocked)
	IndMaintenanceOverdue  = string(TileMaintenanceOverdue)
	IndUtilityUnbilled     = string(TileUtilityUnbilled)
)

func init() {
	Indicators[IndStorageOccupancy] = Indicator{Unit: UnitBP, Fraction: true, Scale: ScaleBP}
	Indicators[IndStorageExitsBlocked] = Indicator{Unit: UnitCount}
	Indicators[IndMaintenanceOverdue] = Indicator{Unit: UnitCount}
	Indicators[IndUtilityUnbilled] = Indicator{Unit: UnitCents}
}

// CmToM2Milli converts cm² (or cm²-nights) to m² in milli, half-even.
func CmToM2Milli(cm2 int64) int64 { return Scaled(cm2, 10, 1).Value }

// StorageArea is a surface area of a yard or shed map with its usable cells (cell_count − blocked cells).
type StorageArea struct {
	ID, Code    string
	MapID       string
	MapKind     string // yard, shed
	CellCm      geo.Cm
	UsableCells int64
}

func (a *StorageArea) cellCm2() int64 { return int64(a.CellCm) * int64(a.CellCm) }

// StorageAllocation is an allocation holding cells of a storage area: its contract, vessel, exit date and the
// accessibility of that exit (computed by the caller with optimizer.AccessibilityTimeline).
type StorageAllocation struct {
	ID           string
	AreaID       string
	Kind         string // contract, transit, owner
	ContractID   string
	ContractCode string
	ContractKind string
	VesselID     string
	LOA, Beam    geo.Cm
	ExitOn       calendar.Date // zero when unknown
	ExitBlocked  bool
	Production   Opt[money.Cents] // competence of the contract in the period (reports.all), on one record per contract
}

// StorageInput is the input of BuildStorage. Rows are the cell rows (cells per allocation, area and night) of the
// Areas in [From, To].
type StorageInput struct {
	From, To         calendar.Date
	GroupBy          string // area, map_kind, contract_kind, month
	ExitsBlockedOnly bool
	Full             bool // reports.all: production
	Areas            []StorageArea
	Rows             []NightRow
	Allocations      map[string]*StorageAllocation
}

// StorageTotals (OpenAPI StorageReportTotals): the exact surfaces in cm²-nights, and the m²-nights in milli of
// the API, where occupied and hull are the sums of the records' milli values (so a total, and the tile, is the
// sum of the drill records as the client reads them, INV-62) and the occupancy is their ratio.
type StorageTotals struct {
	AvailableCm2Nights int64
	OccupiedCm2Nights  int64
	HullCm2Nights      int64
	AvailableM2Milli   int64
	OccupiedM2Milli    int64
	HullM2Milli        int64
	OccupancyBP        BP
	Boats              int64
	ExitsBlocked       int64
	Production         Opt[money.Cents]
}

// StorageRecord (OpenAPI StorageReportRecord).
type StorageRecord struct {
	AllocationID       string
	AreaID             string
	AreaCode           string
	MapKind            string
	VesselID           string
	LOA, Beam          geo.Cm
	ContractID         string
	ContractKind       string
	ContractCode       string
	Nights             int64
	HullCm2Nights      int64
	FootprintCm2Nights int64
	HullM2Milli        int64 // CmToM2Milli(HullCm2Nights)
	FootprintM2Milli   int64 // CmToM2Milli(FootprintCm2Nights)
	ExitOn             calendar.Date
	ExitBlocked        bool
	Production         Opt[money.Cents]
	Ref                Ref
}

// StorageResult is the storage report.
type StorageResult = Result[StorageTotals, StorageRecord]

// FoldStorage is the only producer of storage totals: the records' sums with the available surface of the
// bucket (available is not a record value: it is the usable surface of the areas of the bucket).
func FoldStorage(recs []StorageRecord, availableCm2Nights int64, full bool) StorageTotals {
	t := StorageTotals{AvailableCm2Nights: availableCm2Nights, AvailableM2Milli: CmToM2Milli(availableCm2Nights)}
	var prod money.Cents
	for i := range recs {
		r := &recs[i]
		t.Boats++
		t.OccupiedCm2Nights += r.FootprintCm2Nights
		t.HullCm2Nights += r.HullCm2Nights
		t.OccupiedM2Milli += r.FootprintM2Milli
		t.HullM2Milli += r.HullM2Milli
		if r.ExitBlocked {
			t.ExitsBlocked++
		}
		prod += r.Production.Value
	}
	t.OccupancyBP = Ratio(t.OccupiedM2Milli, t.AvailableM2Milli)
	if full {
		t.Production = Some(prod)
	}
	return t
}

type storageAcc struct {
	cells  int64                   // occupied cell-nights
	nights map[calendar.Date]int64 // cells by night
}

func (a *storageAcc) days() []calendar.Date {
	out := make([]calendar.Date, 0, len(a.nights))
	for d := range a.nights {
		out = append(out, d)
	}
	slices.SortFunc(out, cmpDate)
	return out
}

// BuildStorage builds the storage report: one record per allocation occupying cells (contract, transit, owner)
// of the areas in the period; available = usable cells × cell² per night minus the cells of block and maintenance
// rows. Facts: storage_occupancy by night (the records' footprint, available cm²) and storage_exits_blocked by
// exit date; Σ facts = totals by construction.
func BuildStorage(in StorageInput) (StorageResult, Facts) {
	areas := make(map[string]*StorageArea, len(in.Areas))
	for i := range in.Areas {
		areas[in.Areas[i].ID] = &in.Areas[i]
	}
	nights := int64(in.From.DaysUntil(in.To) + 1)
	availByArea := map[string]int64{}
	availByNight := map[calendar.Date]int64{}
	for i := range in.Areas {
		a := &in.Areas[i]
		availByArea[a.ID] = a.UsableCells * a.cellCm2() * nights
		for d := in.From; !d.After(in.To); d = d.AddDays(1) {
			availByNight[d] += a.UsableCells * a.cellCm2()
		}
	}
	accs := map[string]*storageAcc{}
	for _, row := range in.Rows {
		a := areas[row.ResourceID]
		if a == nil || !inPeriod(row.Night, in.From, in.To) {
			continue
		}
		switch {
		case OutOfService(row.Kind):
			availByArea[a.ID] -= row.Cells * a.cellCm2()
			availByNight[row.Night] -= row.Cells * a.cellCm2()
		case Occupied(row.Kind):
			if al := in.Allocations[row.AllocationID]; al == nil || al.AreaID != a.ID {
				continue
			}
			acc := accs[row.AllocationID]
			if acc == nil {
				acc = &storageAcc{nights: map[calendar.Date]int64{}}
				accs[row.AllocationID] = acc
			}
			acc.cells += row.Cells
			acc.nights[row.Night] += row.Cells
		}
	}
	ids := make([]string, 0, len(accs))
	for id := range accs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	recs := make([]StorageRecord, 0, len(ids))
	for _, id := range ids {
		al, acc := in.Allocations[id], accs[id]
		a := areas[al.AreaID]
		n := int64(len(acc.nights))
		rec := StorageRecord{
			AllocationID: id, AreaID: a.ID, AreaCode: a.Code, MapKind: a.MapKind, VesselID: al.VesselID,
			LOA: al.LOA, Beam: al.Beam, ContractID: al.ContractID, ContractKind: al.ContractKind, ContractCode: al.ContractCode,
			Nights: n, HullCm2Nights: int64(al.LOA) * int64(al.Beam) * n, FootprintCm2Nights: acc.cells * a.cellCm2(),
			ExitOn: al.ExitOn, ExitBlocked: al.ExitBlocked && !al.ExitOn.IsZero() && inPeriod(al.ExitOn, in.From, in.To),
			Ref: Ref{Type: "allocation", ID: id},
		}
		rec.HullM2Milli, rec.FootprintM2Milli = CmToM2Milli(rec.HullCm2Nights), CmToM2Milli(rec.FootprintCm2Nights)
		if in.Full {
			rec.Production = Some(al.Production.Value)
		}
		if al.ContractID != "" {
			rec.Ref = Ref{Type: "contract", ID: al.ContractID, Label: al.ContractCode}
			if al.ContractKind == ContractTransit {
				rec.Ref.Type = "transit"
			}
		}
		if in.ExitsBlockedOnly && !rec.ExitBlocked {
			continue
		}
		recs = append(recs, rec)
	}
	occ, exits := Daily{}, Daily{}
	for d := in.From; !d.After(in.To); d = d.AddDays(1) {
		occ.Add(d, 0, availByNight[d])
	}
	for i := range recs {
		r := &recs[i]
		cell := areas[r.AreaID].cellCm2()
		for d, c := range accs[r.AllocationID].nights {
			occ.Add(d, c*cell, 0)
		}
		if r.ExitBlocked {
			exits.Add(r.ExitOn, 1, 0)
		}
	}
	slices.SortStableFunc(recs, func(a, b StorageRecord) int {
		return compareKeys([]string{a.AreaCode, a.AreaID, a.ExitOn.String(), a.AllocationID},
			[]string{b.AreaCode, b.AreaID, b.ExitOn.String(), b.AllocationID})
	})
	var total int64
	for _, v := range availByArea {
		total += v
	}
	res := StorageResult{Records: recs, Totals: FoldStorage(recs, total, in.Full)}
	switch in.GroupBy {
	case "area":
		res.Groups = storageGroups(recs, in.Full, func(r *StorageRecord) string { return r.AreaID }, func() map[string][2]string {
			m := map[string][2]string{}
			for i := range in.Areas {
				m[in.Areas[i].ID] = [2]string{in.Areas[i].Code, ""}
			}
			return m
		}(), availByArea)
	case "map_kind":
		avail, labels := map[string]int64{}, map[string][2]string{}
		for i := range in.Areas {
			k := in.Areas[i].MapKind
			avail[k] += availByArea[in.Areas[i].ID]
			labels[k] = [2]string{k, ""}
		}
		res.Groups = storageGroups(recs, in.Full, func(r *StorageRecord) string { return r.MapKind }, labels, avail)
	case "contract_kind":
		res.Groups = GroupRecords(recs, func(r StorageRecord) []string { return []string{orNone(r.ContractKind)} },
			func(rs []StorageRecord) StorageTotals { return FoldStorage(rs, total, in.Full) })
	case "month":
		res.Groups = storageMonths(in, recs, accs, areas, availByNight)
	}
	return res, Facts{IndStorageOccupancy: occ, IndStorageExitsBlocked: exits}
}

// storageGroups groups by a dimension of the areas: every area bucket appears (also without records), with the
// available surface of its areas; groups are ordered by label, then key.
func storageGroups(recs []StorageRecord, full bool, key func(*StorageRecord) string, labels map[string][2]string,
	avail map[string]int64,
) []Group[StorageTotals] {
	by := map[string][]StorageRecord{}
	for i := range recs {
		k := key(&recs[i])
		by[k] = append(by[k], recs[i])
	}
	out := make([]Group[StorageTotals], 0, len(labels))
	for k, l := range labels {
		out = append(out, Group[StorageTotals]{Key: k, Label: l[0], Totals: FoldStorage(by[k], avail[k], full)})
	}
	slices.SortFunc(out, func(a, b Group[StorageTotals]) int {
		if c := cmp.Compare(a.Label, b.Label); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// storageMonths splits the records' nights by month: surfaces of the nights of the month, boats present in the
// month, blocked exits by the month of the exit date; production is not split (whole record on its first month).
func storageMonths(in StorageInput, recs []StorageRecord, accs map[string]*storageAcc, areas map[string]*StorageArea,
	availByNight map[calendar.Date]int64,
) []Group[StorageTotals] {
	type month struct {
		t    StorageTotals
		prod money.Cents
	}
	ms := map[string]*month{}
	get := func(k string) *month {
		m := ms[k]
		if m == nil {
			m = &month{}
			ms[k] = m
		}
		return m
	}
	for d := in.From; !d.After(in.To); d = d.AddDays(1) {
		get(MonthKey(d)).t.AvailableCm2Nights += availByNight[d]
	}
	for i := range recs {
		r := &recs[i]
		acc := accs[r.AllocationID]
		cell := areas[r.AreaID].cellCm2()
		days := acc.days()
		seen := map[string]bool{}
		for _, d := range days {
			k := MonthKey(d)
			m := get(k)
			m.t.OccupiedCm2Nights += acc.nights[d] * cell
			m.t.HullCm2Nights += int64(r.LOA) * int64(r.Beam)
			if !seen[k] {
				seen[k] = true
				m.t.Boats++
			}
		}
		if len(days) > 0 {
			get(MonthKey(days[0])).prod += r.Production.Value
		}
		if r.ExitBlocked {
			get(MonthKey(r.ExitOn)).t.ExitsBlocked++
		}
	}
	keys := make([]string, 0, len(ms))
	for k := range ms {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]Group[StorageTotals], 0, len(keys))
	for _, k := range keys {
		m := ms[k]
		m.t.AvailableM2Milli, m.t.OccupiedM2Milli = CmToM2Milli(m.t.AvailableCm2Nights), CmToM2Milli(m.t.OccupiedCm2Nights)
		m.t.HullM2Milli = CmToM2Milli(m.t.HullCm2Nights)
		m.t.OccupancyBP = Ratio(m.t.OccupiedM2Milli, m.t.AvailableM2Milli)
		if in.Full {
			m.t.Production = Some(m.prod)
		}
		out = append(out, Group[StorageTotals]{Key: k, Label: k, Totals: m.t})
	}
	return out
}

// Competence is the competence in [from, to] of a contract value spread over its nights [startsOn, endsOn)
// (accrual day quotas, as R6); 0 outside or for an empty or too long contract.
func Competence(total money.Cents, startsOn, endsOn, from, to calendar.Date) money.Cents {
	c := Contract{Total: total, StartsOn: startsOn, EndsOn: endsOn}
	l := c.Line()
	days := l.Days()
	if days <= 0 || days > accrual.MaxDays || overlapDays(l.From, l.To, from, to) == 0 {
		return 0
	}
	var out money.Cents
	for d := maxDate(l.From, from); !d.After(l.To) && !d.After(to); d = d.AddDays(1) {
		out += accrual.DayQuota(total, days, l.From.DaysUntil(d))
	}
	return out
}
