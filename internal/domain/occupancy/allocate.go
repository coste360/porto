package occupancy

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

func toStoreRows(rows []engine.Row) []store.OccRow {
	out := make([]store.OccRow, len(rows))
	for i, r := range rows {
		out[i] = store.OccRow{Resource: r.Resource, Cell: r.Cell, Night: r.Night}
	}
	return out
}

// conflictOf reads the rows of the plan held by other allocations and builds the *ConflictError.
func conflictOf(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a *store.Allocation, rows []engine.Row) error {
	if len(rows) == 0 {
		return &ConflictError{ResourceType: a.ResourceType, ResourceID: a.ResourceID}
	}
	first, last := rows[0].Night, rows[0].Night
	cellSet := map[int]bool{}
	for _, r := range rows {
		if r.Night.Before(first) {
			first = r.Night
		}
		if r.Night.After(last) {
			last = r.Night
		}
		cellSet[r.Cell] = true
	}
	var cells []int
	if a.ResourceType == store.ResourceArea {
		for c := range cellSet {
			cells = append(cells, c)
		}
		slices.Sort(cells)
	}
	owners, err := store.OccupancyOwners(ctx, tx, scope, a.ResourceType, a.ResourceID, first, last, cells, []string{a.ID})
	if err != nil {
		return err
	}
	planned := map[store.OccRow]bool{}
	for _, r := range rows {
		planned[store.OccRow{Resource: r.Resource, Cell: r.Cell, Night: r.Night}] = true
	}
	ce := &ConflictError{ResourceType: a.ResourceType, ResourceID: a.ResourceID, From: last, To: first}
	ids := map[string]bool{}
	conflictCells := map[int]bool{}
	for _, o := range owners {
		if !planned[store.OccRow{Resource: o.Resource, Cell: o.Cell, Night: o.Night}] {
			continue
		}
		if o.Night.Before(ce.From) {
			ce.From = o.Night
		}
		if o.Night.After(ce.To) {
			ce.To = o.Night
		}
		ids[o.AllocationID] = true
		conflictCells[o.Cell] = true
	}
	if len(ids) == 0 {
		// The rows were taken and released between the insert and the read: still a conflict for this attempt.
		ce.From, ce.To = first, last
	}
	for id := range ids {
		ce.AllocationIDs = append(ce.AllocationIDs, id)
	}
	slices.Sort(ce.AllocationIDs)
	if a.ResourceType == store.ResourceArea {
		for c := range conflictCells {
			ce.Cells = append(ce.Cells, c)
		}
		slices.Sort(ce.Cells)
	}
	return ce
}

// insertPlan writes the rows of an allocation (§3.1 steps 5, 7, 8): expired holds touching the plan are reclaimed
// first (INV-05), then the rows are inserted with ON CONFLICT DO NOTHING and a shortfall is a *ConflictError.
func insertPlan(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a *store.Allocation, rows []engine.Row) error {
	if len(rows) == 0 {
		return nil
	}
	first, last := rows[0].Night, rows[len(rows)-1].Night
	if _, err := store.ReclaimExpiredHolds(ctx, tx, scope, a.ResourceType, a.ResourceID, first, last, now); err != nil {
		return err
	}
	n, err := store.InsertOccupancyRows(ctx, tx, scope, a, toStoreRows(rows))
	if err != nil {
		return err
	}
	if n < int64(len(rows)) {
		return conflictOf(ctx, tx, scope, a, rows)
	}
	return nil
}

// closureOn returns the closure covering night d on r (scopes marina, map, structure and its parent, group,
// resource), or nil.
func closureOn(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *resource, d calendar.Date) (*store.Closure, error) {
	cs, err := store.ListClosures(ctx, tx, scope, d, d)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	var parents map[string]string
	for i := range cs {
		c := &cs[i]
		match := false
		switch c.ScopeType {
		case store.ClosureMarina:
			match = true
		case store.ClosureMap:
			match = c.ScopeID == r.MapID
		case store.ClosureStructure:
			if r.StructureID != "" {
				if parents == nil {
					if parents, err = store.StructureParents(ctx, tx, scope); err != nil {
						return nil, err
					}
				}
				match = c.ScopeID == r.StructureID || c.ScopeID == parents[r.StructureID]
			}
		case store.ClosureGroup:
			match = slices.Contains(r.GroupIDs, c.ScopeID)
		case store.ClosureResource:
			match = c.ScopeID == r.ID
		}
		if match {
			return c, nil
		}
	}
	return nil, nil
}

// checkFit runs fit.Check and applies the override (INV-07): the result, the override to store (nil when the
// vessel fits) or the error.
func checkFit(v *vesselInfo, r *resource, p *engine.Placement, last calendar.Date, rules fit.Rules, o *FitOverride) (fit.Result, *FitOverride, error) {
	res := fit.Check(v.Fit, r.target(p, v.Layout), last, rules)
	if res.OK {
		return res, nil, nil
	}
	if o == nil || !res.Overridable {
		return res, nil, &FitError{Result: res}
	}
	if o.Reason == "" {
		return res, nil, ErrOverrideReasonRequired
	}
	ov := *o
	ov.Codes = res.BlockingCodeList()
	return res, &ov, nil
}

// AllocateTx writes an allocation and its occupancy rows (m3-plan §3.1). The caller has authorised the
// operation (including occupancy.override_fit when a.FitOverride is set) and writes the audit row and the
// outbox event of its operation; this primitive writes only release.resold when it resells released nights.
func (s *Service) AllocateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a NewAllocation) (*store.Allocation, error) {
	spec := a.Spec
	if spec.Last.Before(spec.First) {
		return nil, invalid("last_night", "before_first_night")
	}
	r, err := loadResource(ctx, tx, scope, spec.ResourceType, spec.ResourceID)
	if err != nil {
		return nil, err
	}
	if !r.Active {
		return nil, &PlacementError{Reason: PlacementNotAllowed}
	}
	if a.Arrival {
		c, err := closureOn(ctx, tx, scope, r, spec.First)
		if err != nil {
			return nil, err
		}
		if c != nil {
			return nil, &ClosureError{ClosureID: c.ID, FromDate: c.FromDate, ToDate: c.ToDate}
		}
	}
	var lv layout.Vessel
	var override *FitOverride
	switch {
	case a.Kind == KindBlock:
		if a.BlockReason == "" {
			return nil, invalid("reason", "required")
		}
		if a.BlockSize != nil {
			lv = *a.BlockSize
		}
	case a.VesselID == nil:
		return nil, invalid("vessel_id", "required")
	default:
		v, err := loadVessel(ctx, tx, scope, *a.VesselID)
		if err != nil {
			return nil, err
		}
		lv = v.Layout
		_, rules, err := settings(ctx, tx, scope, now)
		if err != nil {
			return nil, err
		}
		if _, override, err = checkFit(v, r, spec.Placement, spec.Last, rules, a.FitOverride); err != nil {
			return nil, err
		}
	}
	rows, err := engine.Plan(spec, r.Area, lv)
	if err != nil {
		if pe := placementError(err); pe != nil {
			return nil, pe
		}
		return nil, err
	}
	row := &store.Allocation{
		ID: store.NewID(), ResourceType: string(spec.ResourceType), ResourceID: spec.ResourceID, MapID: r.MapID,
		FirstNight: spec.First, LastNight: spec.Last, Kind: string(a.Kind), Status: store.AllocActive,
		BlockReason: a.BlockReason, CreatedByUserID: a.UserID, Version: 1, CreatedAt: store.UTC(now), UpdatedAt: store.UTC(now),
	}
	if a.ContractLineID != nil {
		row.ContractLineID = *a.ContractLineID
	}
	if a.VesselID != nil && a.Kind != KindBlock {
		row.VesselID = *a.VesselID
	}
	if a.MovedFromID != nil {
		row.MovedFromID = *a.MovedFromID
	}
	if a.ExpiresAt != nil {
		row.ExpiresAt = *a.ExpiresAt
	}
	if override != nil {
		row.FitOverride = store.MustJSON(override)
	}
	setPlacement(row, spec.Placement)
	if err := store.InsertAllocation(ctx, tx, scope, row); err != nil {
		return nil, err
	}
	if err := insertPlan(ctx, tx, scope, now, row, rows); err != nil {
		return nil, err
	}
	if err := s.markResold(ctx, tx, scope, now, row, rows, a.UserID); err != nil {
		return nil, err
	}
	return row, nil
}

// markResold links the active releases of other allocations on the same resource whose nights (and cells) the
// new rows take (m3-plan §3.1 step 9): resold_allocation_id is the first reseller, resold_nights grows.
func (s *Service) markResold(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a *store.Allocation, rows []engine.Row, userID string) error {
	if len(rows) == 0 || a.Kind == store.AllocKindBlock || a.Kind == store.AllocKindHold {
		return nil
	}
	rels, err := store.ReleasesOnResource(ctx, tx, scope, a.ResourceType, a.ResourceID, rows[0].Night, rows[len(rows)-1].Night)
	if err != nil || len(rels) == 0 {
		return err
	}
	nights := map[calendar.Date]map[int]bool{}
	for _, r := range rows {
		if nights[r.Night] == nil {
			nights[r.Night] = map[int]bool{}
		}
		nights[r.Night][r.Cell] = true
	}
	for i := range rels {
		rel := &rels[i]
		if rel.AllocationID == a.ID {
			continue
		}
		var relCells map[int]bool
		if a.ResourceType == store.ResourceArea {
			src, err := store.GetAllocation(ctx, tx, scope, rel.AllocationID)
			if err != nil {
				return err
			}
			cells, err := s.cellsOf(ctx, tx, scope, src)
			if err != nil {
				return err
			}
			relCells = map[int]bool{}
			for _, c := range cells {
				relCells[c] = true
			}
		}
		count := int64(0)
		for d := rel.FromNight; !d.After(rel.ToNight); d = d.AddDays(1) {
			cs := nights[d]
			if cs == nil {
				continue
			}
			hit := relCells == nil
			for c := range cs {
				if relCells[c] {
					hit = true
				}
			}
			if hit {
				count++
			}
		}
		if count == 0 {
			continue
		}
		first := rel.ResoldAllocationID == ""
		if first {
			rel.ResoldAllocationID = a.ID
		}
		rel.ResoldNights += count
		if err := store.UpdateRelease(ctx, tx, scope, rel, rel.Version, now); err != nil {
			return err
		}
		if !first {
			continue
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: userActor(userID), Action: "release.resold",
			EntityType: "release", EntityID: rel.ID, Diff: map[string]any{"resold_allocation_id": a.ID, "resold_nights": rel.ResoldNights},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ReleaseResold, event.ReleaseData{
			ReleaseID: rel.ID, AllocationID: rel.AllocationID, ResoldAllocationID: a.ID, ResourceIDs: []string{a.ResourceID},
			MapID: a.MapID, From: rel.FromNight, To: rel.ToNight,
		}); err != nil {
			return err
		}
	}
	return nil
}

func userActor(userID string) audit.Actor {
	if userID == "" {
		return audit.System
	}
	return audit.Actor{Type: audit.ActorUser, ID: userID}
}

// cellsOf returns the cells of an area allocation (nil for berths and land resources). A block has no vessel:
// its cells are read from its rows.
func (s *Service) cellsOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, a *store.Allocation) ([]int, error) {
	if a.ResourceType != store.ResourceArea {
		return nil, nil
	}
	if a.VesselID == "" {
		rows, err := store.RowsOfAllocation(ctx, db, scope, a)
		if err != nil {
			return nil, err
		}
		var cells []int
		for _, r := range rows {
			cells = append(cells, r.Cell)
		}
		slices.Sort(cells)
		return slices.Compact(cells), nil
	}
	r, v, err := s.specInputs(ctx, db, scope, a)
	if err != nil {
		return nil, err
	}
	return engine.Cells(engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: a.ResourceID, Placement: placementOf(a)}, r.Area, v)
}

// specInputs returns the resource and the footprint size of a vessel allocation. Blocks have no vessel: their
// footprint lives only in their rows, so they are never re-planned (they are not moved nor resized).
func (s *Service) specInputs(ctx context.Context, db bun.IDB, scope store.MarinaScope, a *store.Allocation) (*resource, layout.Vessel, error) {
	r, err := loadResource(ctx, db, scope, engine.ResourceType(a.ResourceType), a.ResourceID)
	if err != nil {
		return nil, layout.Vessel{}, err
	}
	if a.VesselID == "" {
		if a.ResourceType == store.ResourceArea {
			return nil, layout.Vessel{}, &PlacementError{Reason: PlacementNotAllowed, AllocationID: a.ID}
		}
		return r, layout.Vessel{}, nil
	}
	v, err := loadVessel(ctx, db, scope, a.VesselID)
	if err != nil {
		return nil, layout.Vessel{}, err
	}
	return r, v.Layout, nil
}

// spec returns the allocation spec of a stored allocation with its active releases.
func specOf(a *store.Allocation, releases []store.Release) engine.AllocSpec {
	sp := engine.AllocSpec{
		ResourceType: engine.ResourceType(a.ResourceType), ResourceID: a.ResourceID, Placement: placementOf(a),
		First: a.FirstNight, Last: a.LastNight,
	}
	for _, r := range releases {
		if r.AllocationID == a.ID && r.Status == store.ReleaseActive {
			sp.Released = append(sp.Released, engine.NightRange{From: r.FromNight, To: r.ToNight})
		}
	}
	slices.SortFunc(sp.Released, func(x, y engine.NightRange) int { return x.From.Compare(y.From) })
	return sp
}

// lockActive locks one allocation and checks it is active.
func lockActive(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (*store.Allocation, error) {
	as, err := store.LockAllocations(ctx, tx, scope, []string{id})
	if err != nil {
		return nil, err
	}
	a := &as[0]
	if a.Status != store.AllocActive {
		return nil, ErrAllocationNotActive
	}
	return a, nil
}

// ConvertTx changes the kind (and expiry) of allocations and their rows, e.g. hold → contract (m3-plan §3.1).
// A hold that expired (or was reclaimed) is ErrHoldExpired: the caller re-allocates.
func (s *Service) ConvertTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocIDs []string, kind Kind, expiresAt *time.Time) error {
	if len(allocIDs) == 0 {
		return nil
	}
	as, err := store.LockAllocations(ctx, tx, scope, allocIDs)
	if err != nil {
		return err
	}
	for i := range as {
		a := &as[i]
		if a.Status != store.AllocActive {
			if a.Status == store.AllocExpired {
				return ErrHoldExpired
			}
			return ErrAllocationNotActive
		}
		if a.Kind == store.AllocKindHold && !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(now) {
			return ErrHoldExpired
		}
		v := a.Version
		wasHold := a.Kind == store.AllocKindHold
		a.Kind = string(kind)
		a.ExpiresAt = time.Time{}
		if expiresAt != nil {
			a.ExpiresAt = *expiresAt
		}
		if kind == KindHold && a.ExpiresAt.IsZero() {
			return invalid("expires_at", "required")
		}
		if err := store.UpdateAllocation(ctx, tx, scope, a, v, now); err != nil {
			return err
		}
		if err := store.SetRowsKind(ctx, tx, scope, a); err != nil {
			return err
		}
		// A hold never resells a release (markResold skips holds): the sale happens when it becomes a transit or
		// a contract, so the releases its rows take are linked now.
		if wasHold && kind != KindHold {
			occ, err := store.RowsOfAllocation(ctx, tx, scope, a)
			if err != nil {
				return err
			}
			rows := make([]engine.Row, len(occ))
			for j, r := range occ {
				rows[j] = engine.Row{Resource: r.Resource, Cell: r.Cell, Night: r.Night}
			}
			if err := s.markResold(ctx, tx, scope, now, a, rows, a.CreatedByUserID); err != nil {
				return err
			}
		}
	}
	return nil
}

// TruncateTx drops the nights ≥ fromNight of an allocation; from its first night or earlier the allocation is
// cancelled. Releases of the dropped nights are closed.
func (s *Service) TruncateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, allocID string, fromNight calendar.Date) error {
	now := s.d.Clock.Now()
	a, err := lockActive(ctx, tx, scope, allocID)
	if err != nil {
		return err
	}
	if fromNight.After(a.LastNight) {
		return nil
	}
	if !fromNight.After(a.FirstNight) {
		return s.cancelLocked(ctx, tx, scope, now, a, StatusCancelled)
	}
	return truncateLocked(ctx, tx, scope, now, a, fromNight)
}

func truncateLocked(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a *store.Allocation, fromNight calendar.Date) error {
	if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, fromNight, calendar.Date{}); err != nil {
		return err
	}
	if err := clipReleases(ctx, tx, scope, now, a.ID, a.FirstNight, fromNight.AddDays(-1)); err != nil {
		return err
	}
	v := a.Version
	a.LastNight = fromNight.AddDays(-1)
	return store.UpdateAllocation(ctx, tx, scope, a, v, now)
}

// clipReleases keeps the active releases of an allocation inside [first, last]: releases outside are revoked,
// releases crossing a bound are shortened.
func clipReleases(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocID string, first, last calendar.Date) error {
	rels, err := store.ActiveReleasesOf(ctx, tx, scope, []string{allocID})
	if err != nil {
		return err
	}
	for i := range rels {
		r := &rels[i]
		v := r.Version
		switch {
		case r.ToNight.Before(first) || r.FromNight.After(last) || last.Before(first):
			r.Status, r.RevokedAt = store.ReleaseRevoked, store.UTC(now)
		case r.FromNight.Before(first) || r.ToNight.After(last):
			if r.FromNight.Before(first) {
				r.FromNight = first
			}
			if r.ToNight.After(last) {
				r.ToNight = last
			}
		default:
			continue
		}
		if err := store.UpdateRelease(ctx, tx, scope, r, v, now); err != nil {
			return err
		}
	}
	return nil
}

// CancelTx ends an allocation with status (cancelled, expired or superseded) and deletes its rows.
func (s *Service) CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, allocID string, status AllocStatus) error {
	if status == StatusActive {
		return invalid("status", "invalid")
	}
	a, err := lockActive(ctx, tx, scope, allocID)
	if err != nil {
		return err
	}
	return s.cancelLocked(ctx, tx, scope, s.d.Clock.Now(), a, status)
}

func (s *Service) cancelLocked(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a *store.Allocation, status AllocStatus) error {
	if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, calendar.Date{}, calendar.Date{}); err != nil {
		return err
	}
	v := a.Version
	a.Status = string(status)
	return store.UpdateAllocation(ctx, tx, scope, a, v, now)
}

// ResizeTx extends or shortens an allocation in place to [first, last]: nights leaving the range lose their
// rows, added nights are inserted (a taken night is a *ConflictError).
func (s *Service) ResizeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocID string, first, last calendar.Date) error {
	if last.Before(first) {
		return invalid("last_night", "before_first_night")
	}
	a, err := lockActive(ctx, tx, scope, allocID)
	if err != nil {
		return err
	}
	if first.Equal(a.FirstNight) && last.Equal(a.LastNight) {
		return nil
	}
	// a contract line is in one place per night (Q6) and a moved_from chain does not overlap itself (Q12): the
	// new range may not reach another allocation of the line or of the chain
	if a.ContractLineID != "" || a.VesselID != "" {
		f := store.AllocationFilter{Status: store.AllocActive, From: first, To: last}
		if a.ContractLineID != "" {
			f.LineIDs = []string{a.ContractLineID}
		} else {
			f.VesselID = a.VesselID
		}
		others, err := store.ListAllocations(ctx, tx, scope, f)
		if err != nil {
			return err
		}
		for _, o := range others {
			if o.ID != a.ID && (a.ContractLineID != "" || o.MovedFromID == a.ID || o.ID == a.MovedFromID) {
				return invalid("last_night", "overlaps_line")
			}
		}
	}
	if err := checkChainOrder(ctx, tx, scope, a, first, last); err != nil {
		return err
	}
	if first.Before(a.FirstNight) {
		if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, calendar.Date{}, first.AddDays(-1)); err != nil {
			return err
		}
	}
	if first.After(a.FirstNight) {
		if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, calendar.Date{}, first.AddDays(-1)); err != nil {
			return err
		}
	}
	if last.Before(a.LastNight) {
		if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, last.AddDays(1), calendar.Date{}); err != nil {
			return err
		}
	}
	if err := clipReleases(ctx, tx, scope, now, a.ID, first, last); err != nil {
		return err
	}
	r, v, err := s.specInputs(ctx, tx, scope, a)
	if err != nil {
		return err
	}
	rels, err := store.ActiveReleasesOf(ctx, tx, scope, []string{a.ID})
	if err != nil {
		return err
	}
	spec := specOf(a, rels)
	var added []engine.Row
	before := a.FirstNight.AddDays(-1)
	if before.After(last) {
		before = last
	}
	after := a.LastNight.AddDays(1)
	if after.Before(first) {
		after = first
	}
	for _, part := range []engine.NightRange{{From: first, To: before}, {From: after, To: last}} {
		if part.To.Before(part.From) {
			continue
		}
		sp := spec
		sp.First, sp.Last = part.From, part.To
		rows, err := engine.Plan(sp, r.Area, v)
		if err != nil {
			if pe := placementError(err); pe != nil {
				return pe
			}
			return err
		}
		added = append(added, rows...)
	}
	version := a.Version
	a.FirstNight, a.LastNight = first, last
	if err := store.UpdateAllocation(ctx, tx, scope, a, version, now); err != nil {
		return err
	}
	if len(added) > 0 {
		slices.SortFunc(added, engine.CompareRows)
		if err := insertPlan(ctx, tx, scope, now, a, added); err != nil {
			return err
		}
		if err := s.markResold(ctx, tx, scope, now, a, added, ""); err != nil {
			return err
		}
	}
	return nil
}

// checkChainOrder keeps the moved_from_id chain ordered (INV-11, query Q12 of m3-plan §6): the new range
// [first, last] of a may not start on or before the last night of its active predecessor, nor end on or after
// the first night of an active successor. Overlaps are refused above; this also refuses a range that jumps
// entirely to the other side of a neighbour of the chain.
func checkChainOrder(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a *store.Allocation, first, last calendar.Date) error {
	if a.MovedFromID != "" {
		p, err := store.GetAllocation(ctx, tx, scope, a.MovedFromID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if p != nil && p.Status == store.AllocActive && !first.After(p.LastNight) {
			return invalid("first_night", "before_predecessor")
		}
	}
	succ, err := store.ActiveSuccessors(ctx, tx, scope, a.ID)
	if err != nil {
		return err
	}
	for _, n := range succ {
		if !last.Before(n.FirstNight) {
			return invalid("last_night", "after_successor")
		}
	}
	return nil
}

// ExpireHoldsTx marks the expired holds of the marina expired and deletes their rows (at most 500 per call).
func (s *Service) ExpireHoldsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time) ([]store.Allocation, error) {
	ids, err := store.ExpiredHoldIDs(ctx, tx, scope, now, 500)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return store.ExpireAllocations(ctx, tx, scope, ids, now)
}
