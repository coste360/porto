package occupancy

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// UndoWindow is how long the last move of an allocation can be undone (m3-plan §13 D16).
const UndoWindow = 24 * time.Hour

// Move operations of the undo log and of occupancy.moved.
const (
	OpMove   = "move"
	OpSwap   = "swap"
	OpLayout = "layout"
	OpUndo   = "undo"
)

// moveOp is one allocation leaving its resource from a night (m3-plan §3.5).
type moveOp struct {
	src       *store.Allocation
	target    Target
	from      calendar.Date
	to        *calendar.Date // interval move: back on the source after to
	override  *FitOverride
	vesselRef string // for placement errors of swaps
}

type undoEntry struct {
	AllocationID string        `json:"allocation_id"`
	Status       string        `json:"status"`
	FirstNight   calendar.Date `json:"first_night"`
	LastNight    calendar.Date `json:"last_night"`
	Version      int           `json:"version"`       // before the move
	AfterVersion int           `json:"after_version"` // right after the move: anything newer blocks the undo
}

type undoCreated struct {
	AllocationID string        `json:"allocation_id"`
	FirstNight   calendar.Date `json:"first_night"`
	LastNight    calendar.Date `json:"last_night"`
}

type undoRelease struct {
	ReleaseID string        `json:"release_id"`
	From      calendar.Date `json:"from"`
	To        calendar.Date `json:"to"`
	Status    string        `json:"status"`
	Copy      bool          `json:"copy"` // a copy made for a new allocation: revoked on undo
}

// undoPayload is move_undo_log.payload.
type undoPayload struct {
	Entries  []undoEntry   `json:"entries"`
	Created  []undoCreated `json:"created"`
	Releases []undoRelease `json:"releases,omitempty"`
	// Price is the repriced contracts of the move (MovePrice.Undo), reversed by the undo (INV-27).
	Price json.RawMessage `json:"price,omitempty"`
}

// moveRun accumulates what a move, swap or layout save writes.
type moveRun struct {
	s        *Service
	tx       bun.IDB
	scope    store.MarinaScope
	now      time.Time
	userID   string
	undo     undoPayload
	created  []*store.Allocation
	changed  []*store.Allocation
	fromRes  []string
	override []string   // allocations created with a fit override
	price    *MovePrice // computed before the transaction (m3-plan §3.5 step 6)
}

// detach removes the rows of op.src from op.from on and truncates (or supersedes) it; its releases from op.from
// on are kept aside (returned) for the new allocations.
func (m *moveRun) detach(ctx context.Context, op *moveOp) ([]store.Release, error) {
	src := op.src
	m.undo.Entries = append(m.undo.Entries, undoEntry{
		AllocationID: src.ID, Status: src.Status, FirstNight: src.FirstNight, LastNight: src.LastNight, Version: src.Version,
	})
	if _, err := store.DeleteAllocationRows(ctx, m.tx, m.scope, src, op.from, calendar.Date{}); err != nil {
		return nil, err
	}
	rels, err := store.ActiveReleasesOf(ctx, m.tx, m.scope, []string{src.ID})
	if err != nil {
		return nil, err
	}
	var moved []store.Release
	for i := range rels {
		r := &rels[i]
		if r.ToNight.Before(op.from) {
			continue
		}
		m.undo.Releases = append(m.undo.Releases, undoRelease{ReleaseID: r.ID, From: r.FromNight, To: r.ToNight, Status: r.Status})
		moved = append(moved, *r)
		v := r.Version
		if r.FromNight.Before(op.from) {
			r.ToNight = op.from.AddDays(-1)
		} else {
			r.Status, r.RevokedAt = store.ReleaseRevoked, m.now
		}
		if err := store.UpdateRelease(ctx, m.tx, m.scope, r, v, m.now); err != nil {
			return nil, err
		}
	}
	v := src.Version
	if !op.from.After(src.FirstNight) {
		src.Status = store.AllocSuperseded
	} else {
		src.LastNight = op.from.AddDays(-1)
	}
	if err := store.UpdateAllocation(ctx, m.tx, m.scope, src, v, m.now); err != nil {
		return nil, err
	}
	m.undo.Entries[len(m.undo.Entries)-1].AfterVersion = src.Version
	m.changed = append(m.changed, src)
	m.fromRes = append(m.fromRes, src.ResourceID)
	return moved, nil
}

// place creates the allocation of src on target for [first, last] with the parts of rels in that range.
func (m *moveRun) place(ctx context.Context, src *store.Allocation, target Target, first, last calendar.Date, movedFrom string,
	override *FitOverride, keepOverride bool, rels []store.Release, vesselRef string,
) (*store.Allocation, error) {
	var released []engine.NightRange
	for _, r := range rels {
		released = append(released, engine.ClipRanges([]engine.NightRange{{From: r.FromNight, To: r.ToNight}}, first, last)...)
	}
	r, err := loadResource(ctx, m.tx, m.scope, target.ResourceType, target.ResourceID)
	if err != nil {
		return nil, err
	}
	if !r.Active {
		return nil, &PlacementError{Reason: PlacementNotAllowed, AllocationID: src.ID}
	}
	v, err := loadVessel(ctx, m.tx, m.scope, src.VesselID)
	if err != nil {
		return nil, err
	}
	var stored *FitOverride
	if keepOverride {
		if len(src.FitOverride) > 0 && string(src.FitOverride) != "null" {
			o := unmarshalJSON[FitOverride](src.FitOverride)
			stored = &o
		}
	} else {
		_, rules, err := settings(ctx, m.tx, m.scope, m.now)
		if err != nil {
			return nil, err
		}
		if _, stored, err = checkFit(v, r, target.Placement, last, rules, override); err != nil {
			return nil, err
		}
	}
	spec := engine.AllocSpec{ResourceType: target.ResourceType, ResourceID: target.ResourceID, Placement: target.Placement, First: first, Last: last, Released: released}
	rows, err := engine.Plan(spec, r.Area, v.Layout)
	if err != nil {
		if pe := placementError(err); pe != nil {
			pe.AllocationID, pe.VesselID = src.ID, vesselRef
			return nil, pe
		}
		return nil, err
	}
	a := &store.Allocation{
		ID: store.NewID(), ContractLineID: src.ContractLineID, VesselID: src.VesselID, ResourceType: string(target.ResourceType),
		ResourceID: target.ResourceID, MapID: r.MapID, FirstNight: first, LastNight: last, Kind: src.Kind, Status: store.AllocActive,
		ExpiresAt: src.ExpiresAt, MovedFromID: movedFrom, CreatedByUserID: m.userID, Version: 1, CreatedAt: m.now, UpdatedAt: m.now,
	}
	if stored != nil {
		a.FitOverride = store.MustJSON(stored)
		if !keepOverride {
			m.override = append(m.override, a.ID)
		}
	}
	setPlacement(a, target.Placement)
	if err := store.InsertAllocation(ctx, m.tx, m.scope, a); err != nil {
		return nil, err
	}
	if err := insertPlan(ctx, m.tx, m.scope, m.now, a, rows); err != nil {
		return nil, err
	}
	if err := m.s.markResold(ctx, m.tx, m.scope, m.now, a, rows, m.userID); err != nil {
		return nil, err
	}
	for _, nr := range released {
		var src0 store.Release
		for _, r := range rels {
			if !nr.From.Before(r.FromNight) && !nr.To.After(r.ToNight) {
				src0 = r
			}
		}
		cp := &store.Release{
			ID: store.NewID(), AllocationID: a.ID, ContractID: src0.ContractID, ResourceType: a.ResourceType, ResourceID: a.ResourceID,
			FromNight: nr.From, ToNight: nr.To, Status: store.ReleaseActive, Source: src0.Source, Notes: src0.Notes,
			CreatedByUserID: m.userID, CreatedAt: m.now, UpdatedAt: m.now,
		}
		if err := store.InsertRelease(ctx, m.tx, m.scope, cp); err != nil {
			return nil, err
		}
		m.undo.Releases = append(m.undo.Releases, undoRelease{ReleaseID: cp.ID, From: cp.FromNight, To: cp.ToNight, Status: cp.Status, Copy: true})
	}
	m.created = append(m.created, a)
	m.undo.Created = append(m.undo.Created, undoCreated{AllocationID: a.ID, FirstNight: first, LastNight: last})
	return a, nil
}

// run executes the ops: every source is detached first, then every new allocation is placed, so that vessels can
// exchange places in one transaction (INV-06). Any error leaves the caller's transaction to roll back.
func (m *moveRun) run(ctx context.Context, ops []*moveOp) error {
	moved := make([][]store.Release, len(ops))
	quoteHold := make([]bool, len(ops))
	for i, op := range ops {
		// A quote option hold moves as a whole (its placement names one allocation) and stays the option's hold.
		q, err := store.QuoteOfAllocation(ctx, m.tx, m.scope, op.src.ID)
		if err != nil {
			return err
		}
		if q != "" {
			if !op.from.Equal(op.src.FirstNight) || op.to != nil {
				return invalid("from_night", "quote_hold_partial")
			}
			quoteHold[i] = true
		}
	}
	for i, op := range ops {
		rels, err := m.detach(ctx, op)
		if err != nil {
			return err
		}
		moved[i] = rels
	}
	for i, op := range ops {
		last := m.undo.Entries[i].LastNight
		end := last
		if op.to != nil {
			end = *op.to
		}
		a, err := m.place(ctx, op.src, op.target, op.from, end, op.src.ID, op.override, false, moved[i], op.vesselRef)
		if err != nil {
			return err
		}
		if quoteHold[i] {
			if _, err := store.RelinkQuotePlacement(ctx, m.tx, m.scope, op.src.ID, a, m.now); err != nil {
				return err
			}
		}
		if op.to != nil && op.to.Before(last) {
			back := Target{ResourceType: engine.ResourceType(op.src.ResourceType), ResourceID: op.src.ResourceID, Placement: placementOf(op.src)}
			if _, err := m.place(ctx, op.src, back, op.to.AddDays(1), last, a.ID, nil, true, moved[i], op.vesselRef); err != nil {
				return err
			}
		}
	}
	return nil
}

// finish writes the undo entry, the audit rows and occupancy.moved, and returns the result.
func (m *moveRun) finish(ctx context.Context, p *auth.Principal, operation string, from, to calendar.Date) (MoveResult, error) {
	if err := m.price.apply(ctx, m.tx, m.scope, m.now); err != nil {
		return MoveResult{}, err
	}
	effect := m.price.effect()
	m.undo.Price = m.price.undoData()
	u := &store.MoveUndo{
		ID: store.NewID(), UserID: m.userID, Operation: operation, Payload: store.MustJSON(m.undo),
		ExpiresAt: m.now.Add(UndoWindow), CreatedAt: m.now, UpdatedAt: m.now,
	}
	if err := store.InsertMoveUndo(ctx, m.tx, m.scope, u); err != nil {
		return MoveResult{}, err
	}
	var ids, res, maps []string
	for _, a := range m.created {
		ids, res, maps = append(ids, a.ID), append(res, a.ResourceID), append(maps, a.MapID)
	}
	for _, a := range m.changed {
		ids, maps = append(ids, a.ID), append(maps, a.MapID)
	}
	action := map[string]string{OpMove: "allocation.move", OpSwap: "allocation.swap", OpLayout: "allocation.layout"}[operation]
	for _, src := range m.changed {
		diff := map[string]any{"status": src.Status, "last_night": src.LastNight, "created": uniq(ids), "from_night": from}
		effect.auditDiff(diff)
		if err := m.s.d.Audit.Write(ctx, m.tx, audit.Entry{
			OrganizationID: m.scope.OrgID(), MarinaID: m.scope.ID(), Actor: p.Actor(), Action: action, EntityType: "allocation",
			EntityID: src.ID, Diff: diff,
		}); err != nil {
			return MoveResult{}, err
		}
	}
	for _, id := range m.override {
		if err := m.s.d.Audit.Write(ctx, m.tx, audit.Entry{
			OrganizationID: m.scope.OrgID(), MarinaID: m.scope.ID(), Actor: p.Actor(), Action: "allocation.fit_override",
			EntityType: "allocation", EntityID: id,
		}); err != nil {
			return MoveResult{}, err
		}
	}
	mapID := ""
	if len(m.created) > 0 {
		mapID = m.created[0].MapID
	}
	if _, err := m.s.d.Outbox.Write(ctx, m.tx, &m.scope, event.OccupancyMoved, event.OccupancyMovedData{
		AllocationIDs: uniq(ids), FromResourceIDs: uniq(m.fromRes), ResourceIDs: uniq(res), MapIDs: uniq(maps), MapID: mapID,
		From: from, To: to, Operation: operation,
	}); err != nil {
		return MoveResult{}, err
	}
	all := make([]store.Allocation, 0, len(m.created)+len(m.changed))
	for _, a := range m.created {
		all = append(all, *a)
	}
	for _, a := range m.changed {
		all = append(all, *a)
	}
	views, err := allocationsOf(ctx, m.tx, m.scope, all)
	if err != nil {
		return MoveResult{}, err
	}
	return MoveResult{Allocations: views, UndoID: u.ID, UndoUntil: u.ExpiresAt, PriceEffect: effect}, nil
}

func uniq(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// checkVersion compares the locked allocation with the version the client saw.
func checkVersion(a *store.Allocation, expected int) error {
	if a.Status != store.AllocActive {
		return ErrAllocationNotActive
	}
	if a.Version != expected {
		return ErrVersionMismatch
	}
	if a.VesselID == "" {
		return &PlacementError{Reason: PlacementNotAllowed, AllocationID: a.ID}
	}
	return nil
}

// fromNight resolves the first night moved: default today (or the first night of a future allocation); it must
// be inside the allocation and never in the past (m3-plan §13 D16).
func fromNight(a *store.Allocation, requested *calendar.Date, today calendar.Date) (calendar.Date, error) {
	from := today
	if requested != nil {
		from = *requested
	} else if a.FirstNight.After(today) {
		from = a.FirstNight
	}
	if from.Before(today) {
		return from, invalid("from_night", "in_the_past")
	}
	if from.Before(a.FirstNight) || from.After(a.LastNight) {
		return from, invalid("from_night", "outside_allocation")
	}
	return from, nil
}

func sameTarget(a *store.Allocation, t Target) bool {
	if a.ResourceType != string(t.ResourceType) || a.ResourceID != t.ResourceID {
		return false
	}
	p := placementOf(a)
	if (p == nil) != (t.Placement == nil) {
		return false
	}
	return p == nil || *p == *t.Placement
}

// MoveTx moves an allocation from a night (m3-plan §3.5): the source keeps the nights before FromNight (or is
// superseded), a new allocation takes [FromNight, ToNight ?? last] on the target and, for an interval move, a
// third one brings the vessel back after ToNight. One undo entry; audit and occupancy.moved are written by the
// caller of MoveTx through finish (Move does both).
func (s *Service) MoveTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, m MoveSpec) (MoveResult, error) {
	res, _, err := s.moveTx(ctx, tx, scope, now, m, nil, nil)
	return res, err
}

func (s *Service) moveTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, m MoveSpec, p *auth.Principal, mp *MovePrice) (MoveResult, *moveRun, error) {
	as, err := store.LockAllocations(ctx, tx, scope, []string{m.AllocationID})
	if err != nil {
		return MoveResult{}, nil, err
	}
	src := &as[0]
	if err := checkVersion(src, m.ExpectedVersion); err != nil {
		return MoveResult{}, nil, err
	}
	loc, err := marinaLocation(ctx, tx, scope)
	if err != nil {
		return MoveResult{}, nil, err
	}
	from, err := fromNight(src, m.FromNight, calendar.In(now, loc))
	if err != nil {
		return MoveResult{}, nil, err
	}
	var to *calendar.Date
	if m.ToNight != nil {
		if m.ToNight.Before(from) || m.ToNight.After(src.LastNight) {
			return MoveResult{}, nil, invalid("to_night", "outside_allocation")
		}
		if m.ToNight.Before(src.LastNight) {
			t := *m.ToNight
			to = &t
		}
	}
	if sameTarget(src, m.Target) {
		return MoveResult{}, nil, invalid("target", "unchanged")
	}
	run := &moveRun{s: s, tx: tx, scope: scope, now: store.UTC(now), userID: m.UserID, price: mp}
	if err := run.run(ctx, []*moveOp{{src: src, target: m.Target, from: from, to: to, override: m.FitOverride}}); err != nil {
		return MoveResult{}, nil, err
	}
	end := run.undo.Entries[0].LastNight
	if to != nil {
		end = *to
	}
	res, err := run.finish(ctx, orSystem(p), OpMove, from, end)
	return res, run, err
}

func orSystem(p *auth.Principal) *auth.Principal {
	if p == nil {
		return &auth.Principal{}
	}
	return p
}

// authorizeOverride requires occupancy.override_fit for a request carrying a fit override.
func authorizeOverride(p *auth.Principal, scope store.MarinaScope, o *FitOverride) error {
	if o != nil && !p.Can(scope.OrgID(), scope.ID(), auth.PermOccupancyOverrideFit) {
		return ErrOverrideForbidden
	}
	return nil
}

func overrideFor(p *auth.Principal, reason *string) *FitOverride {
	if reason == nil {
		return nil
	}
	return NewFitOverride(p, *reason)
}

// MoveInput is a move request (moveAllocation).
type MoveInput struct {
	AllocationID string
	Version      int
	Target       Target
	FromNight    *calendar.Date
	ToNight      *calendar.Date
	FitOverride  *string // reason
	Reprice      *bool
}

// Move moves an allocation (occupancy.move; a fit override also needs occupancy.override_fit).
func (s *Service) Move(ctx context.Context, p *auth.Principal, marinaID string, in MoveInput) (MoveResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return MoveResult{}, err
	}
	o := overrideFor(p, in.FitOverride)
	if err := authorizeOverride(p, scope, o); err != nil {
		return MoveResult{}, err
	}
	if err := authorizeReprice(p, scope, in.Reprice); err != nil {
		return MoveResult{}, err
	}
	mp, err := s.priceMove(ctx, p, scope, in)
	if err != nil {
		return MoveResult{}, err
	}
	var out MoveResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res, _, err := s.moveTx(ctx, tx, scope, s.now(), MoveSpec{
			AllocationID: in.AllocationID, ExpectedVersion: in.Version, Target: in.Target, FromNight: in.FromNight,
			ToNight: in.ToNight, FitOverride: o, Reprice: in.Reprice, UserID: p.UserID, Operation: OpMove,
		}, p, mp)
		out = res
		return err
	})
	return out, err
}

// priceMove prices a move before its transaction (nil when there is nothing to price).
func (s *Service) priceMove(ctx context.Context, p *auth.Principal, scope store.MarinaScope, in MoveInput) (*MovePrice, error) {
	if s.pricer == nil {
		return nil, nil
	}
	a, err := store.GetAllocation(ctx, s.d.DB.DB, scope, in.AllocationID)
	if err != nil {
		return nil, nil //nolint:nilerr // the write transaction reports a missing allocation
	}
	today, err := s.todayIn(ctx, scope)
	if err != nil {
		return nil, err
	}
	from, err := fromNight(a, in.FromNight, today)
	if err != nil {
		return nil, nil //nolint:nilerr // the write transaction reports an invalid from_night
	}
	leg, ok := legOf(a, in.Version, in.Target, from, in.ToNight)
	if !ok {
		return nil, nil
	}
	return s.priceLegs(ctx, p, scope.ID(), []MoveLeg{leg}, in.Reprice)
}

// SwapInput is a swap request (swapAllocations).
type SwapInput struct {
	A, B        BaseVersion
	FromNight   *calendar.Date
	FitOverride *string
}

// Swap exchanges two vessels from a night (occupancy.move): one transaction, locks in id order, both sources
// detached before both new allocations are inserted; on areas each vessel takes the other's placement.
func (s *Service) Swap(ctx context.Context, p *auth.Principal, marinaID string, in SwapInput) (MoveResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return MoveResult{}, err
	}
	o := overrideFor(p, in.FitOverride)
	if err := authorizeOverride(p, scope, o); err != nil {
		return MoveResult{}, err
	}
	if in.A.AllocationID == in.B.AllocationID {
		return MoveResult{}, invalid("b.allocation_id", "same_allocation")
	}
	mp, err := s.priceSwap(ctx, p, scope, in)
	if err != nil {
		return MoveResult{}, err
	}
	var out MoveResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		as, err := store.LockAllocations(ctx, tx, scope, []string{in.A.AllocationID, in.B.AllocationID})
		if err != nil {
			return err
		}
		byID := map[string]*store.Allocation{as[0].ID: &as[0], as[1].ID: &as[1]}
		a, b := byID[in.A.AllocationID], byID[in.B.AllocationID]
		if err := checkVersion(a, in.A.Version); err != nil {
			return err
		}
		if err := checkVersion(b, in.B.Version); err != nil {
			return err
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		today := calendar.In(now, loc)
		from := swapFrom(a, b, in.FromNight, today)
		for _, x := range []*store.Allocation{a, b} {
			if _, err := fromNight(x, &from, today); err != nil {
				return err
			}
		}
		ta := Target{ResourceType: engine.ResourceType(b.ResourceType), ResourceID: b.ResourceID, Placement: placementOf(b)}
		tb := Target{ResourceType: engine.ResourceType(a.ResourceType), ResourceID: a.ResourceID, Placement: placementOf(a)}
		run := &moveRun{s: s, tx: tx, scope: scope, now: now, userID: p.UserID, price: mp}
		ops := []*moveOp{
			{src: a, target: ta, from: from, override: o, vesselRef: a.VesselID},
			{src: b, target: tb, from: from, override: o, vesselRef: b.VesselID},
		}
		if a.ID > b.ID {
			ops[0], ops[1] = ops[1], ops[0]
		}
		if err := run.run(ctx, ops); err != nil {
			return err
		}
		end := run.undo.Entries[0].LastNight
		if l := run.undo.Entries[1].LastNight; l.After(end) {
			end = l
		}
		out, err = run.finish(ctx, p, OpSwap, from, end)
		return err
	})
	return out, err
}

// swapFrom is the first night a swap moves: the requested one, else today or the later first night of the two.
func swapFrom(a, b *store.Allocation, requested *calendar.Date, today calendar.Date) calendar.Date {
	if requested != nil {
		return *requested
	}
	from := today
	for _, x := range []*store.Allocation{a, b} {
		if x.FirstNight.After(from) {
			from = x.FirstNight
		}
	}
	return from
}

// priceSwap prices both legs of a swap before its transaction (policy only: swaps take no reprice flag).
func (s *Service) priceSwap(ctx context.Context, p *auth.Principal, scope store.MarinaScope, in SwapInput) (*MovePrice, error) {
	if s.pricer == nil {
		return nil, nil
	}
	a, errA := store.GetAllocation(ctx, s.d.DB.DB, scope, in.A.AllocationID)
	b, errB := store.GetAllocation(ctx, s.d.DB.DB, scope, in.B.AllocationID)
	if errA != nil || errB != nil {
		return nil, nil //nolint:nilerr // the write transaction reports a missing allocation
	}
	today, err := s.todayIn(ctx, scope)
	if err != nil {
		return nil, err
	}
	from := swapFrom(a, b, in.FromNight, today)
	var legs []MoveLeg
	for _, x := range []struct {
		src, other *store.Allocation
		version    int
	}{{a, b, in.A.Version}, {b, a, in.B.Version}} {
		t := Target{ResourceType: engine.ResourceType(x.other.ResourceType), ResourceID: x.other.ResourceID, Placement: placementOf(x.other)}
		if leg, ok := legOf(x.src, x.version, t, from, nil); ok {
			legs = append(legs, leg)
		}
	}
	return s.priceLegs(ctx, p, scope.ID(), legs, nil)
}

// LayoutTx saves a layout session of a dynamic area (m3-plan §3.5, §13 D17): every vessel allocation of the
// area covering FromNight must be in Base with its current version (else ErrVersionMismatch) and must be placed
// by an op (else 422 unplaced); ops may bring allocations from other resources. All touched rows are deleted
// first, then the new allocations are inserted: vessels can exchange places in one save.
func (s *Service) LayoutTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, l LayoutSpec) (MoveResult, error) {
	res, err := s.layoutTx(ctx, tx, scope, now, l, nil, nil)
	return res, err
}

// PriceLayout prices, outside any transaction, the vessels a layout brings into the area from other resources
// (the storage scenario apply, m9-plan §7.3 step 1). The result goes to LayoutPricedTx; nil when nothing is priced.
func (s *Service) PriceLayout(ctx context.Context, p *auth.Principal, scope store.MarinaScope, l LayoutSpec) (*MovePrice, error) {
	return s.priceLayout(ctx, p, scope, l)
}

// LayoutPricedTx is LayoutTx acting as p with the price computed by PriceLayout: the repriced contracts are written
// in the same transaction and the effect is in MoveResult.PriceEffect and in the audit diff.
func (s *Service) LayoutPricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, l LayoutSpec,
	p *auth.Principal, mp *MovePrice,
) (MoveResult, error) {
	return s.layoutTx(ctx, tx, scope, now, l, p, mp)
}

// layoutFrom is the first night a layout save changes: the requested one (default the date), never in the past.
func layoutFrom(l LayoutSpec, today calendar.Date) calendar.Date {
	from := l.Date
	if l.FromNight != nil {
		from = *l.FromNight
	}
	if from.Before(today) {
		from = today
	}
	return from
}

// priceLayout prices the vessels a layout save brings into the area from other resources (moves inside the area
// keep the price).
func (s *Service) priceLayout(ctx context.Context, p *auth.Principal, scope store.MarinaScope, l LayoutSpec) (*MovePrice, error) {
	if s.pricer == nil {
		return nil, nil
	}
	today, err := s.todayIn(ctx, scope)
	if err != nil {
		return nil, err
	}
	from := layoutFrom(l, today)
	base := map[string]int{}
	for _, b := range l.Base {
		base[b.AllocationID] = b.Version
	}
	var legs []MoveLeg
	for _, op := range l.Ops {
		a, err := store.GetAllocation(ctx, s.d.DB.DB, scope, op.AllocationID)
		if err != nil || (a.ResourceType == string(store.ResourceArea) && a.ResourceID == l.AreaID) {
			continue
		}
		f := from
		if a.FirstNight.After(f) {
			f = a.FirstNight
		}
		pl := op.Placement
		if leg, ok := legOf(a, base[a.ID], Target{ResourceType: engine.ResourceArea, ResourceID: l.AreaID, Placement: &pl}, f, nil); ok {
			legs = append(legs, leg)
		}
	}
	return s.priceLegs(ctx, p, scope.ID(), legs, nil)
}

func (s *Service) layoutTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, l LayoutSpec, p *auth.Principal, mp *MovePrice) (MoveResult, error) {
	area, err := store.DynamicAreaByID(ctx, tx, scope, l.AreaID)
	if err != nil {
		return MoveResult{}, err
	}
	loc, err := marinaLocation(ctx, tx, scope)
	if err != nil {
		return MoveResult{}, err
	}
	from := layoutFrom(l, calendar.In(now, loc))
	present, err := store.ListAllocations(ctx, tx, scope, store.AllocationFilter{
		ResourceType: store.ResourceArea, ResourceIDs: []string{area.ID}, Status: store.AllocActive, From: from, To: from,
	})
	if err != nil {
		return MoveResult{}, err
	}
	ids := []string{}
	for _, a := range present {
		ids = append(ids, a.ID)
	}
	base := map[string]int{}
	for _, b := range l.Base {
		base[b.AllocationID] = b.Version
		ids = append(ids, b.AllocationID)
	}
	for _, op := range l.Ops {
		if _, ok := base[op.AllocationID]; !ok {
			return MoveResult{}, ErrVersionMismatch
		}
	}
	locked, err := store.LockAllocations(ctx, tx, scope, ids)
	if errors.Is(err, store.ErrNotFound) {
		return MoveResult{}, ErrVersionMismatch
	}
	if err != nil {
		return MoveResult{}, err
	}
	byID := map[string]*store.Allocation{}
	for i := range locked {
		byID[locked[i].ID] = &locked[i]
	}
	for _, a := range present {
		if a.VesselID == "" {
			continue // blocks stay where they are
		}
		v, ok := base[a.ID]
		if !ok || byID[a.ID].Version != v {
			return MoveResult{}, ErrVersionMismatch
		}
	}
	for id, v := range base {
		if a := byID[id]; a == nil || a.Status != store.AllocActive || a.Version != v {
			return MoveResult{}, ErrVersionMismatch
		}
	}
	placed := map[string]bool{}
	var ops []*moveOp
	for _, op := range l.Ops {
		a := byID[op.AllocationID]
		if a.VesselID == "" {
			return MoveResult{}, &PlacementError{Reason: PlacementNotAllowed, AllocationID: a.ID}
		}
		placed[a.ID] = true
		pl := op.Placement
		t := Target{ResourceType: engine.ResourceArea, ResourceID: area.ID, Placement: &pl}
		if sameTarget(a, t) {
			continue
		}
		f := from
		if a.FirstNight.After(f) {
			f = a.FirstNight
		}
		if f.After(a.LastNight) {
			return MoveResult{}, invalid("placements", "outside_allocation")
		}
		ops = append(ops, &moveOp{src: a, target: t, from: f, override: op.FitOverride})
	}
	for _, a := range present {
		if a.VesselID != "" && !placed[a.ID] {
			return MoveResult{}, invalid("unplaced", a.ID)
		}
	}
	slices.SortFunc(ops, func(x, y *moveOp) int {
		switch {
		case x.src.ID < y.src.ID:
			return -1
		case x.src.ID > y.src.ID:
			return 1
		}
		return 0
	})
	run := &moveRun{s: s, tx: tx, scope: scope, now: store.UTC(now), userID: l.UserID, price: mp}
	if err := run.run(ctx, ops); err != nil {
		return MoveResult{}, err
	}
	end := from
	for _, e := range run.undo.Entries {
		if e.LastNight.After(end) {
			end = e.LastNight
		}
	}
	return run.finish(ctx, orSystem(p), OpLayout, from, end)
}

// LayoutInput is a layout save request (saveAreaLayout).
type LayoutInput struct {
	AreaID       string
	Date         calendar.Date
	FromNight    *calendar.Date
	Base         []BaseVersion
	Placements   []LayoutOp
	FitOverrides map[string]string // allocation id → reason
}

// SaveLayout saves a layout session (occupancy.move; overrides also occupancy.override_fit).
func (s *Service) SaveLayout(ctx context.Context, p *auth.Principal, marinaID string, in LayoutInput) (MoveResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return MoveResult{}, err
	}
	if len(in.FitOverrides) > 0 {
		if err := authorizeOverride(p, scope, &FitOverride{}); err != nil {
			return MoveResult{}, err
		}
	}
	ops := make([]LayoutOp, len(in.Placements))
	for i, op := range in.Placements {
		ops[i] = op
		if reason, ok := in.FitOverrides[op.AllocationID]; ok {
			ops[i].FitOverride = NewFitOverride(p, reason)
		}
	}
	spec := LayoutSpec{AreaID: in.AreaID, Date: in.Date, FromNight: in.FromNight, Base: in.Base, Ops: ops, UserID: p.UserID}
	mp, err := s.priceLayout(ctx, p, scope, spec)
	if err != nil {
		return MoveResult{}, err
	}
	var out MoveResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res, err := s.layoutTx(ctx, tx, scope, s.now(), spec, p, mp)
		out = res
		return err
	})
	return out, err
}

// Undo undoes a move, swap or layout save (occupancy.move): allowed before expires_at, when every created
// allocation is still active at version 1 with its nights, and every source is as the move left it. The new
// allocations become superseded, the sources get back their status, nights and rows (a taken night is
// ErrUndoUnavailable), the moved releases their ranges.
func (s *Service) Undo(ctx context.Context, p *auth.Principal, marinaID, undoID string) ([]Allocation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return nil, err
	}
	var out []Allocation
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		u, err := store.GetMoveUndo(ctx, tx, scope, undoID)
		if err != nil {
			return err
		}
		if !u.UndoneAt.IsZero() || !now.Before(u.ExpiresAt) {
			return ErrUndoUnavailable
		}
		pl := unmarshalJSON[undoPayload](u.Payload)
		if len(pl.Created) == 0 {
			return ErrUndoUnavailable // a layout save that changed nothing
		}
		ids := []string{}
		for _, e := range pl.Entries {
			ids = append(ids, e.AllocationID)
		}
		for _, c := range pl.Created {
			ids = append(ids, c.AllocationID)
		}
		locked, err := store.LockAllocations(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		byID := map[string]*store.Allocation{}
		for i := range locked {
			byID[locked[i].ID] = &locked[i]
		}
		for _, c := range pl.Created {
			a := byID[c.AllocationID]
			if a.Status != store.AllocActive || a.Version != 1 || !a.FirstNight.Equal(c.FirstNight) || !a.LastNight.Equal(c.LastNight) {
				return ErrUndoUnavailable
			}
		}
		for _, e := range pl.Entries {
			if byID[e.AllocationID].Version != e.AfterVersion {
				return ErrUndoUnavailable
			}
		}
		var changed []store.Allocation
		var res, fromRes, maps []string
		for _, c := range pl.Created {
			a := byID[c.AllocationID]
			if err := s.cancelLocked(ctx, tx, scope, now, a, StatusSuperseded); err != nil {
				return err
			}
			changed = append(changed, *a)
			fromRes, maps = append(fromRes, a.ResourceID), append(maps, a.MapID)
		}
		for _, r := range pl.Releases {
			rel, err := store.GetRelease(ctx, tx, scope, r.ReleaseID)
			if err != nil {
				return err
			}
			v := rel.Version
			if r.Copy {
				rel.Status, rel.RevokedAt = store.ReleaseRevoked, now
			} else {
				rel.FromNight, rel.ToNight, rel.Status, rel.RevokedAt = r.From, r.To, r.Status, time.Time{}
			}
			if err := store.UpdateRelease(ctx, tx, scope, rel, v, now); err != nil {
				return err
			}
		}
		for _, e := range pl.Entries {
			a := byID[e.AllocationID]
			if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, calendar.Date{}, calendar.Date{}); err != nil {
				return err
			}
			v := a.Version
			a.Status, a.FirstNight, a.LastNight = e.Status, e.FirstNight, e.LastNight
			if err := store.UpdateAllocation(ctx, tx, scope, a, v, now); err != nil {
				return err
			}
			if err := s.replan(ctx, tx, scope, now, a); err != nil {
				if errors.Is(err, ErrResourceTaken) {
					return ErrUndoUnavailable
				}
				return err
			}
			changed = append(changed, *a)
			res, maps = append(res, a.ResourceID), append(maps, a.MapID)
		}
		// Quote option holds moved by the undone operation point back to their source.
		for _, c := range pl.Created {
			a := byID[c.AllocationID]
			if src := byID[a.MovedFromID]; src != nil && src.Status == store.AllocActive {
				if _, err := store.RelinkQuotePlacement(ctx, tx, scope, a.ID, src, now); err != nil {
					return err
				}
			}
		}
		// The price the move applied is reversed in the same transaction (INV-27).
		if len(pl.Price) > 0 {
			if s.pricer == nil {
				return errors.New("occupancy: undo of a repriced move without a move pricer")
			}
			if err := s.pricer.UndoMovePriceTx(ctx, tx, scope, p, now, pl.Price); err != nil {
				return err
			}
		}
		if err := store.MarkMoveUndone(ctx, tx, scope, u.ID, now); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrUndoUnavailable
			}
			return err
		}
		allIDs := []string{}
		for _, a := range changed {
			allIDs = append(allIDs, a.ID)
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "allocation.undo", EntityType: "move_undo",
			EntityID: u.ID, Diff: map[string]any{"operation": u.Operation, "allocations": uniq(allIDs)},
		}); err != nil {
			return err
		}
		first, last := calendar.Date{}, calendar.Date{}
		for _, e := range pl.Entries {
			if first.IsZero() || e.FirstNight.Before(first) {
				first = e.FirstNight
			}
			if e.LastNight.After(last) {
				last = e.LastNight
			}
		}
		mapID := ""
		if len(changed) > 0 {
			mapID = changed[len(changed)-1].MapID
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.OccupancyMoved, event.OccupancyMovedData{
			AllocationIDs: uniq(allIDs), FromResourceIDs: uniq(fromRes), ResourceIDs: uniq(res), MapIDs: uniq(maps), MapID: mapID,
			From: first, To: last, Operation: OpUndo, Undo: true,
		}); err != nil {
			return err
		}
		out, err = allocationsOf(ctx, tx, scope, changed)
		return err
	})
	return out, err
}

// replan inserts the full plan of an active allocation (with its active releases); its rows must have been
// deleted before.
func (s *Service) replan(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a *store.Allocation) error {
	if a.Status != store.AllocActive {
		return nil
	}
	r, v, err := s.specInputs(ctx, tx, scope, a)
	if err != nil {
		return err
	}
	rels, err := store.ActiveReleasesOf(ctx, tx, scope, []string{a.ID})
	if err != nil {
		return err
	}
	rows, err := engine.Plan(specOf(a, rels), r.Area, v)
	if err != nil {
		if pe := placementError(err); pe != nil {
			return pe
		}
		return err
	}
	return insertPlan(ctx, tx, scope, now, a, rows)
}
