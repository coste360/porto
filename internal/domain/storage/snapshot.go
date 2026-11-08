package storage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// area is a loaded storage area: its row, footprint grid, optimizer grid and fit target.
type area struct {
	row           store.StorageArea
	la            layout.Area
	grid          optimizer.Grid
	gates         []optimizer.GateIssue
	target        fit.Target
	weightPerCell *int64
	rules         fit.Rules
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// loadArea loads a storage area: a surface dynamic area of a yard or shed map (ErrTargetInvalid otherwise).
func loadArea(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*area, error) {
	rows, err := store.StorageAreas(ctx, db, scope, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, auth.ErrNotFound
	}
	r := rows[0]
	if r.Kind != string(layout.AreaSurface) || (r.MapKind != "yard" && r.MapKind != "shed") {
		return nil, ErrTargetInvalid
	}
	la, err := occupancy.LayoutArea(&r.DynamicArea)
	if err != nil {
		return nil, ErrTargetInvalid
	}
	var exits []int
	if len(r.Exits) > 0 {
		_ = json.Unmarshal(r.Exits, &exits)
	}
	a := &area{row: r, la: la, grid: optimizer.NewGrid(la, exits)}
	a.gates = optimizer.ValidateGates(a.grid)
	a.target = fit.Target{
		Kind: fit.TargetSurfaceArea, MaxLOA: geo.Cm(r.MaxLOACm), MaxBeam: geo.Cm(r.MaxBeamCm), MultihullOK: true,
		Supports: slices.Clone(r.Supports),
	}
	if r.MaxHeightCm > 0 {
		h := geo.Cm(r.MaxHeightCm)
		a.target.MaxAirDraft = &h
	}
	if r.MaxWeightKg > 0 {
		w := r.MaxWeightKg
		a.weightPerCell = &w
	}
	rules, err := fitRules(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	a.rules = rules
	return a, nil
}

// fitRules are the marina parameters of fit.Check (occupancy settings and harbour tolerance; defaults when unset).
func fitRules(ctx context.Context, db bun.IDB, scope store.MarinaScope) (fit.Rules, error) {
	r := fit.Rules{
		LOATolerance: layout.DefaultLOATol, SafetyClearance: 30, DepthWarnMargin: fit.DefaultDepthWarnMargin, WasteThresholdBP: 6000,
	}
	o, err := store.GetOccupancySettings(ctx, db, scope)
	switch {
	case err == nil:
		r.SafetyClearance, r.WasteThresholdBP = geo.Cm(o.SafetyClearanceCm), o.WasteThresholdBP
	case !errors.Is(err, store.ErrNotFound):
		return r, err
	}
	h, err := store.GetHarborSettings(ctx, db, scope)
	switch {
	case err == nil:
		r.LOATolerance = geo.Cm(h.LOAToleranceCm)
	case !errors.Is(err, store.ErrNotFound):
		return r, err
	}
	return r, nil
}

// targetFor is the fit target of a vessel at a placement: the area target with the weight limit of the hull cells
// (max_weight_kg × ⌈LOA/cell⌉·⌈beam/cell⌉, m9-plan D6).
func (a *area) targetFor(v fit.Vessel) fit.Target {
	t := a.target
	if a.weightPerCell != nil && a.la.Cell > 0 {
		c := a.la.Cell
		w := *a.weightPerCell * int64((v.LOA+c-1)/c) * int64((v.Beam+c-1)/c)
		t.MaxWeightKg = &w
	}
	return t
}

// check runs fit.Check of an item on the area.
func (a *area) check(it *SnapshotItem, lastNight calendar.Date) fit.Result {
	v := it.Fit
	v.Support, v.DisplacementKg = it.Support, it.Displacement
	return fit.Check(v, a.targetFor(v), lastNight, a.rules)
}

// fitParams are the optimizer fit parameters of the area.
func (a *area) fitParams() optimizer.FitParams {
	return optimizer.FitParams{Rules: a.rules, Target: a.target, MaxWeightPerCellKg: a.weightPerCell}
}

func effective(declared, measured int64, useMeasured bool) int64 {
	if useMeasured && measured > 0 {
		return measured
	}
	return declared
}

func cmPtr(v int64) *geo.Cm {
	if v <= 0 {
		return nil
	}
	c := geo.Cm(v)
	return &c
}

// vesselFit is the effective size of a vessel (measured when dimensions_source = measured, spec 11.2).
func vesselFit(v *store.Vessel) fit.Vessel {
	m := v.DimensionsSource == "measured"
	return fit.Vessel{
		LOA: geo.Cm(effective(v.LOACm, v.MeasuredLOACm, m)), Beam: geo.Cm(effective(v.BeamCm, v.MeasuredBeamCm, m)),
		Draft: cmPtr(effective(v.DraftCm, v.MeasuredDraftCm, m)), AirDraft: cmPtr(effective(v.AirDraftCm, v.MeasuredAirDraftCm, m)),
		Hull: v.HullType,
	}
}

// isObstacleKind reports whether an allocation kind is a fixed obstacle of the optimizer (no vessel to move).
func isObstacleKind(kind string, vesselID string) bool {
	return vesselID == "" || kind == store.AllocKindBlock || kind == store.AllocKindMaintenance
}

// readSnapshot reads the state of an area for nights [from, to] with a few queries (m9-plan §4): the active
// allocations of the area overlapping the window, their contracts, vessels and storage details; blocks and
// maintenance rows are obstacles. extra are allocation ids from elsewhere (scenario tray) to load as items too.
func readSnapshot(ctx context.Context, db bun.IDB, scope store.MarinaScope, a *area, from, to calendar.Date, extra []string) (*Snapshot, error) {
	rows, err := store.DayAllocations(ctx, db, scope, "", from, to, []string{a.row.ID})
	if err != nil {
		return nil, err
	}
	var others []string
	for _, id := range extra {
		if !slices.ContainsFunc(rows, func(r store.DayAllocation) bool { return r.ID == id }) {
			others = append(others, id)
		}
	}
	if len(others) > 0 {
		more, err := store.DayAllocationsByIDs(ctx, db, scope, others)
		if err != nil {
			return nil, err
		}
		rows = append(rows, more...)
	}
	snap := &Snapshot{AreaID: a.row.ID, Area: a.row, From: from, To: to, Grid: a.grid, Gates: a.gates}
	var vids, cids, obstIDs []string
	for _, r := range rows {
		if isObstacleKind(r.Kind, r.VesselID) {
			if r.ResourceID == a.row.ID {
				obstIDs = append(obstIDs, r.ID)
			}
			continue
		}
		vids = append(vids, r.VesselID)
		if r.ContractKind == contract.KindStorage {
			cids = append(cids, r.ContractID)
		}
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]store.Vessel{}
	for _, v := range vs {
		vm[v.ID] = v
	}
	details, err := store.StorageDetailsOf(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	cells, err := store.AreaCellsOf(ctx, db, scope, a.row.ID, obstIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if isObstacleKind(r.Kind, r.VesselID) {
			if r.ResourceID != a.row.ID {
				continue
			}
			snap.Obstacles = append(snap.Obstacles, Obstacle{Kind: r.Kind, Obstacle: optimizer.Obstacle{
				ID: r.ID, Cells: cells[r.ID], First: r.FirstNight, Last: r.LastNight,
			}})
			continue
		}
		snap.Items = append(snap.Items, buildItem(a, r, vm, details))
	}
	slices.SortFunc(snap.Items, func(x, y SnapshotItem) int { return compareStr(x.Allocation.ID, y.Allocation.ID) })
	return snap, nil
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// buildItem converts an allocation row to a snapshot item: effective size, support, stay events and cells.
func buildItem(a *area, r store.DayAllocation, vm map[string]store.Vessel, details map[string]store.StorageContractDetails) SnapshotItem {
	v := vm[r.VesselID]
	it := SnapshotItem{
		Allocation: r.Allocation, ContractID: r.ContractID, ContractKind: r.ContractKind, ContractStatus: r.ContractStatus,
		ContractTerm: r.ContractTerm, Vessel: v, Fit: vesselFit(&v), OnArea: r.ResourceType == store.ResourceArea && r.ResourceID == a.row.ID,
		FitOverride: len(r.FitOverride) > 0 && string(r.FitOverride) != "null",
	}
	if r.ContractID != "" {
		it.ContractCode = contract.Code(&store.Contract{Kind: r.ContractKind, Year: r.ContractYear, Number: r.ContractNumber})
	}
	if v.DisplacementKg > 0 {
		d := v.DisplacementKg
		it.Displacement = &d
	}
	it.ArrivalOn, it.ExitOn = r.FirstNight, r.LastNight.AddDays(1)
	if d, ok := details[r.ContractID]; ok && r.ContractKind == contract.KindStorage {
		it.Support = d.Support
		arr := contract.StorageArrivalOn(&d, r.StartsOn, r.EndsOn)
		exit := contract.StorageExitOn(&d, r.EndsOn)
		// An allocation split by a move starts or ends with the move.
		if arr.After(it.ArrivalOn) && !arr.After(r.LastNight) {
			it.ArrivalOn = arr
		}
		if exit.Before(it.ExitOn) && exit.After(it.ArrivalOn) {
			it.ExitOn = exit
		}
	}
	if p := it.Placement(); p != nil {
		if fp, err := layout.Footprint(layout.Vessel{LOA: it.Fit.LOA, Beam: it.Fit.Beam}, *p, a.la); err == nil {
			it.Cells = fp.Indexes
		}
	}
	return it
}

// clip returns the nights of [first, last] inside [from, to] (ok false when none).
func clip(first, last, from, to calendar.Date) (calendar.Date, calendar.Date, bool) {
	if first.Before(from) {
		first = from
	}
	if last.After(to) {
		last = to
	}
	return first, last, !first.After(last)
}

// optItem is the optimizer item of a snapshot item at a placement (nights clipped to the window).
func (it *SnapshotItem) optItem(from, to calendar.Date, current *optimizer.Placement, pinned bool) optimizer.Item {
	first, last, _ := clip(it.Allocation.FirstNight, it.Allocation.LastNight, from, to)
	return optimizer.Item{
		ID: it.Allocation.ID, LOA: it.Fit.LOA, Beam: it.Fit.Beam, Fit: it.Fit, Support: it.Support,
		DisplacementKg: it.Displacement, First: first, Last: last, ArrivalOn: it.ArrivalOn, ExitOn: it.ExitOn,
		Current: current, Pinned: pinned, FitOverridden: it.FitOverride,
	}
}

// obstacles returns the optimizer obstacles of the snapshot clipped to the window.
func (sn *Snapshot) obstacles() []optimizer.Obstacle {
	out := make([]optimizer.Obstacle, 0, len(sn.Obstacles))
	for _, o := range sn.Obstacles {
		if first, last, ok := clip(o.First, o.Last, sn.From, sn.To); ok {
			ob := o.Obstacle
			ob.First, ob.Last = first, last
			out = append(out, ob)
		}
	}
	return out
}

// item returns the snapshot item of an allocation (nil when absent).
func (sn *Snapshot) item(id string) *SnapshotItem {
	for i := range sn.Items {
		if sn.Items[i].Allocation.ID == id {
			return &sn.Items[i]
		}
	}
	return nil
}
