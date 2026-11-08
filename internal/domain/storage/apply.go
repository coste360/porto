package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// plannedMove is a scenario move with what the worklist needs.
type plannedMove struct {
	store.ScenarioMove
	temporary bool
	hint      string
	gap       int32
	src, dst  []int
}

func ptr64(v int64) *int64 { return &v }

// equipmentHint is the machine a move needs (m9-plan §7.4): a travel lift from the water, else by support.
func equipmentHint(it *SnapshotItem, fromLinear bool) string {
	if it.Allocation.ResourceType == store.ResourceBerth || fromLinear {
		return store.EquipmentHintTravelLift
	}
	switch it.Support {
	case store.StorageSupportTrailer:
		return store.EquipmentHintTrailer
	case store.StorageSupportCradle:
		return store.EquipmentHintCradleMover
	case store.StorageSupportRack:
		return store.EquipmentHintForklift
	}
	return store.EquipmentHintNone
}

// computeMoves is the diff working copy vs real state in recommended execution order (m9-plan §7.4): by move_on,
// then the boats whose place another boat needs leave first (a cycle is broken by moving one boat to a temporary
// position first), then the deepest target first, then by allocation.
func computeMoves(st *scenarioState, today calendar.Date) []plannedMove {
	var ms []plannedMove
	for i := range st.items {
		row := &st.items[i]
		at := itemAt(row)
		si := st.snap.item(row.AllocationID)
		if at == nil || si == nil {
			continue
		}
		cur := si.Placement()
		if cur != nil && sameRect(*at, *cur) {
			continue
		}
		on := st.sc.FromOn
		if si.Allocation.FirstNight.After(on) {
			on = si.Allocation.FirstNight
		}
		if today.After(on) {
			on = today
		}
		m := plannedMove{ScenarioMove: store.ScenarioMove{
			ScenarioID: st.sc.ID, MarinaID: st.sc.MarinaID, AllocationID: si.Allocation.ID, VesselID: si.Allocation.VesselID,
			FromResourceType: si.Allocation.ResourceType, FromResourceID: si.Allocation.ResourceID,
			ToResourceType: store.ResourceArea, ToResourceID: st.area.row.ID, ToXCm: ptr64(int64(at.X)), ToYCm: ptr64(int64(at.Y)),
			ToRotationDd: ptr64(int64(at.Rotation)), MoveOn: on, Kind: store.ScenarioMovePlace,
		}}
		if si.OnArea {
			m.Kind = store.ScenarioMoveMove
			m.FromXCm, m.FromYCm, m.FromRotationDd = si.Allocation.XCm, si.Allocation.YCm, si.Allocation.RotationDd
			m.src = si.Cells
		}
		m.hint = equipmentHint(si, false)
		if fp, err := layout.Footprint(layout.Vessel{LOA: si.Fit.LOA, Beam: si.Fit.Beam}, *at, st.area.la); err == nil {
			m.dst = fp.Indexes
			m.gap = 1 << 30
			for _, c := range m.dst {
				if d := st.area.grid.Dist[c]; d >= 0 && d < m.gap {
					m.gap = d
				}
			}
		}
		ms = append(ms, m)
	}
	key := func(a, b *plannedMove) int {
		return cmp.Or(a.MoveOn.Compare(b.MoveOn), cmp.Compare(b.gap, a.gap), cmp.Compare(a.AllocationID, b.AllocationID))
	}
	// before[i] = moves that must precede i (their source cells are i's target).
	n := len(ms)
	need := make([]int, n)
	after := make([][]int, n)
	for i := range ms {
		for j := range ms {
			if i != j && ms[j].MoveOn.Equal(ms[i].MoveOn) && intersects(ms[j].src, ms[i].dst) {
				after[j] = append(after[j], i)
				need[i]++
			}
		}
	}
	done := make([]bool, n)
	var order []int
	for len(order) < n {
		best := -1
		for i := range ms {
			if !done[i] && need[i] == 0 && (best < 0 || key(&ms[i], &ms[best]) < 0) {
				best = i
			}
		}
		if best < 0 {
			// A cycle: the first remaining move goes to a temporary position, freeing its place.
			for i := range ms {
				if !done[i] && (best < 0 || key(&ms[i], &ms[best]) < 0) {
					best = i
				}
			}
			ms[best].temporary = true
			for _, k := range after[best] {
				need[k]--
			}
			after[best] = nil
			// It still has to reach its target: it waits for its predecessors like the others.
			need[best] = 0
		}
		done[best] = true
		order = append(order, best)
		for _, k := range after[best] {
			need[k]--
		}
	}
	out := make([]plannedMove, 0, n)
	for k, i := range order {
		m := ms[i]
		m.Seq = k + 1
		out = append(out, m)
	}
	return out
}

// Apply applies a draft scenario in one transaction (m9-plan §7.3): under the locks every placement is re-validated
// on the current state and every conflict is collected (*ConflictsError, nothing written); then occupancy.LayoutTx
// moves every allocation, the worklist is written, the scenario becomes applied, with audit and outbox. Any error
// rolls back everything (INV-06).
func (s *Service) Apply(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64) (*ApplyResult, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	// The price of the vessels brought in from the tray is computed read-only before the transaction, as a desk
	// layout save does (m9-plan §7.3 step 1); a contract changed meanwhile fails the apply inside the transaction.
	mp, err := s.priceTray(ctx, p, scope, scenarioID)
	if err != nil {
		return nil, err
	}
	var out *ApplyResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res, err := s.applyTx(ctx, tx, scope, p, scenarioID, version, mp)
		out = res
		return err
	})
	if errors.Is(err, store.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	return out, err
}

// priceTray prices the placed scenario items that are not on the area yet (tray items from another surface area or
// land resource); items already on the area keep their price (occupancy.PriceLayout skips them).
func (s *Service) priceTray(ctx context.Context, p *auth.Principal, scope store.MarinaScope, id string) (*occupancy.MovePrice, error) {
	db := s.d.DB.DB
	sc, err := store.GetScenario(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if sc.Status != store.ScenarioDraft {
		return nil, nil // applyTx reports it
	}
	items, err := store.ScenarioItems(ctx, db, scope, sc.ID)
	if err != nil {
		return nil, err
	}
	from := sc.FromOn
	if today := s.today(ctx, db, scope); today.After(from) {
		from = today
	}
	base := decodeBase(sc.Base)
	spec := occupancy.LayoutSpec{AreaID: sc.AreaID, Date: from, FromNight: &from, UserID: p.UserID}
	for i := range items {
		it := &items[i]
		at := itemAt(it)
		if it.Origin != store.ScenarioOriginTray || at == nil {
			continue
		}
		spec.Base = append(spec.Base, occupancy.BaseVersion{AllocationID: it.AllocationID, Version: base[it.AllocationID]})
		spec.Ops = append(spec.Ops, occupancy.LayoutOp{
			AllocationID: it.AllocationID, Placement: engine.Placement{X: at.X, Y: at.Y, Rotation: geo.Angle(at.Rotation)},
		})
	}
	if len(spec.Ops) == 0 {
		return nil, nil
	}
	return s.d.Occupancy.PriceLayout(ctx, p, scope, spec)
}

func (s *Service) applyTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, id string, version int64,
	mp *occupancy.MovePrice,
) (*ApplyResult, error) {
	sc, err := store.LockScenario(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if sc.Status != store.ScenarioDraft {
		return nil, ErrScenarioNotDraft
	}
	if version != 0 && int64(sc.Version) != version {
		return nil, ErrVersionMismatch
	}
	ar, err := loadArea(ctx, tx, scope, sc.AreaID)
	if err != nil {
		return nil, err
	}
	if len(ar.gates) > 0 || len(ar.grid.Gates) == 0 {
		return nil, ErrExitsInvalid
	}
	today := s.today(ctx, tx, scope)
	from := sc.FromOn
	if today.After(from) {
		from = today
	}
	items, err := store.ScenarioItems(ctx, tx, scope, sc.ID)
	if err != nil {
		return nil, err
	}
	base := decodeBase(sc.Base)
	// Lock every item and every allocation of the area present from the first night on (id order).
	present, err := store.ListAllocations(ctx, tx, scope, store.AllocationFilter{
		ResourceType: store.ResourceArea, ResourceIDs: []string{ar.row.ID}, Status: store.AllocActive, From: from, To: sc.ToOn,
	})
	if err != nil {
		return nil, err
	}
	lockIDs := itemIDs(items)
	for _, a := range present {
		lockIDs = append(lockIDs, a.ID)
	}
	if len(lockIDs) > 0 {
		if _, err := store.LockAllocations(ctx, tx, scope, lockIDs); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	st, err := s.loadScenario(ctx, tx, scope, sc)
	if err != nil {
		return nil, err
	}
	conflicts, ops, err := s.validateOnCurrent(ctx, tx, scope, st, base, from)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, &ConflictsError{Conflicts: conflicts}
	}
	moves := computeMoves(st, today)
	spec := occupancy.LayoutSpec{AreaID: ar.row.ID, Date: from, FromNight: &from, Ops: ops, UserID: p.UserID}
	for _, it := range items {
		spec.Base = append(spec.Base, occupancy.BaseVersion{AllocationID: it.AllocationID, Version: base[it.AllocationID]})
	}
	lres, err := s.d.Occupancy.LayoutPricedTx(ctx, tx, scope, s.now(), spec, p, mp)
	if err != nil {
		if ce, ok := errors.AsType[*occupancy.ConflictError](err); ok {
			// The DB unique keys found what the validation missed: report it, everything rolls back.
			out := make([]Conflict, 0, len(ce.AllocationIDs))
			f, t := ce.From, ce.To
			out = append(out, Conflict{AllocationID: firstOr(ce.AllocationIDs), Reason: ConflictResourceTaken, Cells: ce.Cells, From: &f, To: &t, WithAllocationID: ce.AllocationIDs})
			return nil, &ConflictsError{Conflicts: out}
		}
		if errors.Is(err, occupancy.ErrVersionMismatch) {
			return nil, &ConflictsError{Conflicts: []Conflict{{AllocationID: firstOr(itemIDs(items)), Reason: ConflictStale}}}
		}
		return nil, err
	}
	now := s.now()
	rows := make([]store.StorageMovement, 0, len(moves))
	var moved []string
	for _, m := range moves {
		allocID := m.AllocationID
		succ, err := store.ActiveSuccessors(ctx, tx, scope, m.AllocationID)
		if err != nil {
			return nil, err
		}
		for _, a := range succ {
			if a.ResourceType == store.ResourceArea && a.ResourceID == ar.row.ID {
				allocID = a.ID
			}
		}
		moved = append(moved, allocID)
		rows = append(rows, store.StorageMovement{
			ID: store.NewID(), MarinaID: scope.ID(), AreaID: ar.row.ID, ScenarioID: sc.ID, AllocationID: allocID,
			VesselID: m.VesselID, Seq: m.Seq, MoveOn: m.MoveOn, FromResourceType: m.FromResourceType, FromResourceID: m.FromResourceID,
			FromXCm: m.FromXCm, FromYCm: m.FromYCm, FromRotationDd: m.FromRotationDd, ToResourceType: m.ToResourceType,
			ToResourceID: m.ToResourceID, ToXCm: m.ToXCm, ToYCm: m.ToYCm, ToRotationDd: m.ToRotationDd, EquipmentHint: m.hint,
			TemporaryPosition: m.temporary, Status: store.MovementPending, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := store.InsertMovements(ctx, tx, rows); err != nil {
		return nil, err
	}
	expected := sc.Version
	sc.Status, sc.AppliedAt, sc.AppliedByUserID = store.ScenarioApplied, now, p.UserID
	if err := store.UpdateScenario(ctx, tx, scope, sc, expected, now); err != nil {
		return nil, err
	}
	slices.Sort(moved)
	if err := s.writeAudit(ctx, tx, scope, p, "storage.scenario_apply", "storage_scenario", sc.ID, map[string]any{
		"area_id": sc.AreaID, "moves": len(rows), "allocation_ids": moved, "from_night": from,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.StorageScenarioApplied, event.StorageScenarioAppliedData{
		ScenarioID: sc.ID, AreaID: sc.AreaID, Moves: len(rows), AllocationIDs: moved,
	}); err != nil {
		return nil, err
	}
	views, err := s.movementViews(ctx, tx, scope, rows)
	if err != nil {
		return nil, err
	}
	return &ApplyResult{Scenario: *sc, Movements: views, PriceEffect: lres.PriceEffect}, nil
}

func firstOr(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// validateOnCurrent re-reads the current rows of the window (except the scenario's own allocations) and checks
// every placement with Footprint, fit and cell conflicts, collecting every conflict (m9-plan §7.3 c). It returns
// the layout ops when there is none.
func (s *Service) validateOnCurrent(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *scenarioState,
	base map[string]int, from calendar.Date,
) ([]Conflict, []occupancy.LayoutOp, error) {
	var conflicts []Conflict
	add := func(c Conflict) { conflicts = append(conflicts, c) }
	for _, id := range st.extras {
		add(Conflict{AllocationID: id, Reason: ConflictStale})
	}
	type op struct {
		id          string
		cells       []int
		first, last calendar.Date
	}
	var placed []op
	var ops []occupancy.LayoutOp
	maxLast := from
	for i := range st.items {
		row := &st.items[i]
		si := st.snap.item(row.AllocationID)
		if si == nil || si.Allocation.Status != store.AllocActive || si.Allocation.Version != base[row.AllocationID] {
			add(Conflict{AllocationID: row.AllocationID, Reason: ConflictStale})
			continue
		}
		at := itemAt(row)
		first := from
		if si.Allocation.FirstNight.After(first) {
			first = si.Allocation.FirstNight
		}
		last := si.Allocation.LastNight
		if first.After(last) {
			continue // over before the apply night: nothing to move
		}
		if at == nil {
			if si.OnArea {
				f, l := first, last
				add(Conflict{AllocationID: row.AllocationID, Reason: ConflictUnplaced, From: &f, To: &l})
			}
			continue
		}
		fp, err := layout.Footprint(layout.Vessel{LOA: si.Fit.LOA, Beam: si.Fit.Beam}, *at, st.area.la)
		if err != nil {
			add(Conflict{AllocationID: row.AllocationID, Reason: ConflictFit})
			continue
		}
		var override *occupancy.FitOverride
		if si.FitOverride {
			o := occupancy.FitOverride{}
			if json.Unmarshal(si.Allocation.FitOverride, &o) == nil {
				override = &o
			}
		}
		if r := st.area.check(si, last); !r.OK && (override == nil || !r.Overridable) {
			add(Conflict{AllocationID: row.AllocationID, Reason: ConflictFit, FitCodes: r.BlockingCodeList()})
			continue
		}
		placed = append(placed, op{id: row.AllocationID, cells: fp.Indexes, first: first, last: last})
		if last.After(maxLast) {
			maxLast = last
		}
		ops = append(ops, occupancy.LayoutOp{
			AllocationID: row.AllocationID, FitOverride: override,
			Placement: engine.Placement{X: at.X, Y: at.Y, Rotation: geo.Angle(at.Rotation)},
		})
	}
	for i := range placed {
		x := &placed[i]
		var with []string
		for j := range placed {
			y := &placed[j]
			if i != j && nightsOverlap(x.first, x.last, y.first, y.last) && intersects(x.cells, y.cells) {
				with = append(with, y.id)
			}
		}
		if len(with) > 0 {
			add(Conflict{AllocationID: x.id, Reason: ConflictOverlap, WithAllocationID: with})
		}
	}
	// Cells taken by rows that are not the scenario's (blocks, maintenance, other allocations).
	rows, err := store.AreaRowsBetween(ctx, tx, scope, st.area.row.ID, from, maxLast, itemIDs(st.items))
	if err != nil {
		return nil, nil, err
	}
	taken := map[int][]store.AreaRow{}
	for _, r := range rows {
		taken[r.Cell] = append(taken[r.Cell], r)
	}
	for _, x := range placed {
		var cells []int
		var with []string
		var f, l calendar.Date
		for _, c := range x.cells {
			for _, r := range taken[c] {
				if r.Night.Before(x.first) || r.Night.After(x.last) {
					continue
				}
				if len(cells) == 0 || cells[len(cells)-1] != c {
					cells = append(cells, c)
				}
				with = append(with, r.AllocationID)
				if f.IsZero() || r.Night.Before(f) {
					f = r.Night
				}
				if l.IsZero() || r.Night.After(l) {
					l = r.Night
				}
			}
		}
		if len(cells) > 0 {
			slices.Sort(with)
			add(Conflict{AllocationID: x.id, Reason: ConflictResourceTaken, Cells: cells, From: &f, To: &l, WithAllocationID: slices.Compact(with)})
		}
	}
	slices.SortStableFunc(conflicts, func(a, b Conflict) int { return cmp.Compare(a.AllocationID, b.AllocationID) })
	return conflicts, ops, nil
}

// ---- movements (m9-plan §7.4) ----

// movementViews adds the vessels to movements.
func (s *Service) movementViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.StorageMovement) ([]MovementView, error) {
	var vids []string
	for _, r := range rows {
		vids = append(vids, r.VesselID)
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]*store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = &vs[i]
	}
	out := make([]MovementView, 0, len(rows))
	for _, r := range rows {
		out = append(out, MovementView{StorageMovement: r, Vessel: vm[r.VesselID]})
	}
	return out, nil
}

// ListMovements returns the worklist of a day (storage.read) with the exit_blocked warnings of the day.
func (s *Service) ListMovements(ctx context.Context, p *auth.Principal, marinaID string, f MovementFilter) (*MovementList, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	d := s.today(ctx, db, scope)
	if f.Date != nil {
		d = *f.Date
	}
	rows, err := store.ListMovements(ctx, db, scope, store.MovementFilter{Date: &d, AreaID: f.AreaID, Status: f.Status})
	if err != nil {
		return nil, err
	}
	views, err := s.movementViews(ctx, db, scope, rows)
	if err != nil {
		return nil, err
	}
	out := &MovementList{Date: d, Items: views, Warnings: []Access{}}
	var areaIDs []string
	if f.AreaID != "" {
		areaIDs = []string{f.AreaID}
	} else {
		all, err := store.StorageAreas(ctx, db, scope, "")
		if err != nil {
			return nil, err
		}
		for _, a := range all {
			areaIDs = append(areaIDs, a.ID)
		}
	}
	for _, id := range areaIDs {
		ar, err := loadArea(ctx, db, scope, id)
		if errors.Is(err, ErrTargetInvalid) || errors.Is(err, auth.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sn, err := readSnapshot(ctx, db, scope, ar, d.AddDays(-1), d, nil)
		if err != nil {
			return nil, err
		}
		ev := evaluate(ar, sn, realLayout(sn))
		for _, a := range ev.access {
			if a.Event == "exit" && !a.Accessible && a.On.Equal(d) {
				out.Warnings = append(out.Warnings, a)
			}
		}
	}
	return out, nil
}

// CompleteMovement marks a movement done (storage.manage, online only, If-Match).
func (s *Service) CompleteMovement(ctx context.Context, p *auth.Principal, marinaID, movementID string, version int64) (*MovementView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	var out *MovementView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.LockMovement(ctx, tx, scope, movementID)
		if err != nil {
			return notFound(err)
		}
		if version != 0 && int64(m.Version) != version {
			return ErrVersionMismatch
		}
		if m.Status != store.MovementPending {
			return ErrMovementNotPending
		}
		expected := m.Version
		m.Status, m.DoneAt, m.DoneByUserID = store.MovementDone, s.now(), p.UserID
		if err := store.UpdateMovement(ctx, tx, scope, m, expected, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "storage.movement_done", "storage_movement", m.ID, map[string]any{
			"allocation_id": m.AllocationID, "area_id": m.AreaID,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.StorageMovementDone, event.StorageMovementDoneData{
			MovementID: m.ID, AreaID: m.AreaID, AllocationID: m.AllocationID,
		}); err != nil {
			return err
		}
		vs, err := s.movementViews(ctx, tx, scope, []store.StorageMovement{*m})
		if err != nil {
			return err
		}
		out = &vs[0]
		return nil
	})
	if errors.Is(err, store.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	return out, err
}
