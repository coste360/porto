package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Scenarios (m9-plan §7): a working copy of the placements of an area for a night window. Create, save, optimize,
// compare and discard write only storage_scenarios, scenario_items and scenario_moves (INV-14).

// scenarioState is a loaded scenario: its area, the snapshot of its window (with the tray allocations), the items
// and the base versions.
type scenarioState struct {
	sc     *store.StorageScenario
	area   *area
	snap   *Snapshot
	items  []store.ScenarioItem
	base   map[string]int
	extras []string // vessel allocations of the area in the window that are not items (arrived since the copy)
}

func decodeBase(raw store.JSON) map[string]int {
	out := map[string]int{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func itemAt(it *store.ScenarioItem) *optimizer.Placement {
	if it.XCm == nil || it.YCm == nil || it.RotationDd == nil {
		return nil
	}
	return &optimizer.Placement{X: geo.Cm(*it.XCm), Y: geo.Cm(*it.YCm), Rotation: geo.Angle(*it.RotationDd)}
}

func setItemAt(it *store.ScenarioItem, p *optimizer.Placement) {
	if p == nil {
		it.XCm, it.YCm, it.RotationDd = nil, nil, nil
		return
	}
	x, y, r := int64(p.X), int64(p.Y), int64(p.Rotation)
	it.XCm, it.YCm, it.RotationDd = &x, &y, &r
}

// loadScenario reads a scenario with its area, items and the snapshot of its window.
func (s *Service) loadScenario(ctx context.Context, db bun.IDB, scope store.MarinaScope, sc *store.StorageScenario) (*scenarioState, error) {
	ar, err := loadArea(ctx, db, scope, sc.AreaID)
	if err != nil {
		return nil, err
	}
	items, err := store.ScenarioItems(ctx, db, scope, sc.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].AllocationID
	}
	sn, err := readSnapshot(ctx, db, scope, ar, sc.FromOn, sc.ToOn, ids)
	if err != nil {
		return nil, err
	}
	st := &scenarioState{sc: sc, area: ar, snap: sn, items: items, base: decodeBase(sc.Base)}
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	for i := range sn.Items {
		it := &sn.Items[i]
		if it.OnArea && it.Allocation.Status == store.AllocActive && !in[it.Allocation.ID] {
			st.extras = append(st.extras, it.Allocation.ID)
		}
	}
	return st, nil
}

// layout is the working copy as an evaluation layout (items whose allocation is gone are skipped).
func (st *scenarioState) layout() []evalItem {
	out := make([]evalItem, 0, len(st.items))
	for i := range st.items {
		it := &st.items[i]
		si := st.snap.item(it.AllocationID)
		if si == nil {
			continue
		}
		out = append(out, evalItem{it: si, at: itemAt(it), pinned: it.Pinned})
	}
	return out
}

// staleItems returns the items whose allocation changed since the copy (version, status), and the newcomers.
func (st *scenarioState) staleItems() []string {
	var out []string
	for i := range st.items {
		id := st.items[i].AllocationID
		si := st.snap.item(id)
		if si == nil || si.Allocation.Status != store.AllocActive || si.Allocation.Version != st.base[id] {
			out = append(out, id)
		}
	}
	return append(out, st.extras...)
}

// snapWithExtras is the snapshot where the newcomers are fixed obstacles (they are not part of the copy).
func (st *scenarioState) snapWithExtras() *Snapshot {
	if len(st.extras) == 0 {
		return st.snap
	}
	sn := *st.snap
	sn.Obstacles = slices.Clone(sn.Obstacles)
	for _, id := range st.extras {
		it := sn.item(id)
		sn.Obstacles = append(sn.Obstacles, Obstacle{Kind: it.Allocation.Kind, Obstacle: optimizer.Obstacle{
			ID: id, Cells: it.Cells, First: it.Allocation.FirstNight, Last: it.Allocation.LastNight,
		}})
	}
	return &sn
}

// view builds the scenario detail: indicators (stale computed on read), issues, accessibility and moves.
func (s *Service) view(ctx context.Context, db bun.IDB, scope store.MarinaScope, st *scenarioState) (*ScenarioDetail, error) {
	moves, err := store.ScenarioMoves(ctx, db, scope, st.sc.ID)
	if err != nil {
		return nil, err
	}
	ev := evaluate(st.area, st.snapWithExtras(), st.layout())
	stale := st.staleItems()
	ev.ind.Stale = len(stale) > 0
	for _, id := range stale {
		ev.issues = append(ev.issues, ItemIssue{AllocationID: id, Reason: IssueStale})
	}
	d := &ScenarioDetail{Scenario: *st.sc, Indicators: &ev.ind, Moves: moves, Issues: ev.issues, Access: ev.access}
	for i := range st.items {
		it := st.items[i]
		v := ScenarioItemView{ScenarioItem: it, Snapshot: st.snap.item(it.AllocationID), BaseVersion: st.base[it.AllocationID]}
		if v.Snapshot != nil {
			v.CurrentVersion = v.Snapshot.Allocation.Version
		}
		d.Items = append(d.Items, v)
	}
	return d, nil
}

// refresh recomputes and stores the moves and the indicators of a draft (in tx).
func (s *Service) refresh(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *scenarioState, today calendar.Date) error {
	ms := computeMoves(st, today)
	rows := make([]store.ScenarioMove, len(ms))
	for i := range ms {
		rows[i] = ms[i].ScenarioMove
	}
	if err := store.ReplaceScenarioMoves(ctx, tx, scope, st.sc.ID, rows); err != nil {
		return err
	}
	ev := evaluate(st.area, st.snapWithExtras(), st.layout())
	ev.ind.Stale = len(st.staleItems()) > 0
	st.sc.Score = store.MustJSON(ev.ind)
	return nil
}

func checkName(v *auth.ValidationError, name string) string {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > maxScenarioName {
		v.Add("name", "invalid")
	}
	return n
}

func checkWindow(from, to calendar.Date) error {
	if from.IsZero() || to.IsZero() || to.Before(from) || from.DaysUntil(to)+1 > MaxScenarioNights {
		return ErrWindowInvalid
	}
	return nil
}

// ListScenarios lists scenarios, newest first (storage.read).
func (s *Service) ListScenarios(ctx context.Context, p *auth.Principal, marinaID string, f ScenarioFilter) ([]ScenarioSummary, string, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListScenarios(ctx, db, scope, store.ScenarioFilter{AreaID: f.AreaID, Status: f.Status}, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]ScenarioSummary, 0, len(page.Items))
	for _, sc := range page.Items {
		sum := ScenarioSummary{Scenario: sc}
		if len(sc.Score) > 0 && string(sc.Score) != "{}" {
			var ind Indicators
			if json.Unmarshal(sc.Score, &ind) == nil {
				sum.Indicators = &ind
			}
		}
		out = append(out, sum)
	}
	return out, page.NextCursor, nil
}

// ScenarioSummary is a row of the scenario list: the scenario with its last stored indicators.
type ScenarioSummary struct {
	Scenario   store.StorageScenario
	Indicators *Indicators
}

// CreateScenario creates a scenario from the real state of the area (storage.manage).
func (s *Service) CreateScenario(ctx context.Context, p *auth.Principal, marinaID string, in ScenarioInput) (*ScenarioDetail, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	name := checkName(v, in.Name)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := checkWindow(in.From, in.To); err != nil {
		return nil, err
	}
	ar, err := loadArea(ctx, db, scope, in.AreaID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sc := &store.StorageScenario{
		ID: store.NewID(), MarinaID: scope.ID(), AreaID: ar.row.ID, Name: name, AuthorUserID: p.UserID, FromOn: in.From,
		ToOn: in.To, Status: store.ScenarioDraft, Version: 1, CreatedAt: now, UpdatedAt: now, Score: store.JSON("{}"),
	}
	sc.Seed = int64(SeedOf(sc.ID)) //nolint:gosec // the uint64 seed is stored as its int64 bit pattern
	var out *ScenarioDetail
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		sn, err := readSnapshot(ctx, tx, scope, ar, in.From, in.To, nil)
		if err != nil {
			return err
		}
		st := &scenarioState{sc: sc, area: ar, snap: sn, base: map[string]int{}}
		for i := range sn.Items {
			it := &sn.Items[i]
			if !it.OnArea {
				continue
			}
			row := store.ScenarioItem{
				ScenarioID: sc.ID, MarinaID: scope.ID(), AllocationID: it.Allocation.ID, VesselID: it.Allocation.VesselID,
				Origin: store.ScenarioOriginArea,
			}
			setItemAt(&row, it.Placement())
			st.items = append(st.items, row)
			st.base[it.Allocation.ID] = it.Allocation.Version
		}
		sc.Base = store.MustJSON(st.base)
		ev := evaluate(st.area, st.snap, st.layout())
		sc.Score = store.MustJSON(ev.ind)
		if err := store.InsertScenario(ctx, tx, sc); err != nil {
			return err
		}
		if err := store.ReplaceScenarioItems(ctx, tx, scope, sc.ID, st.items); err != nil {
			return err
		}
		if err := s.refresh(ctx, tx, scope, st, s.today(ctx, tx, scope)); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "storage.scenario_create", "storage_scenario", sc.ID, map[string]any{
			"area_id": sc.AreaID, "from": sc.FromOn, "to": sc.ToOn, "items": len(st.items),
		}); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, st)
		return err
	})
	return out, err
}

// GetScenario reads a scenario; stale is computed on read (storage.read).
func (s *Service) GetScenario(ctx context.Context, p *auth.Principal, marinaID, scenarioID string) (*ScenarioDetail, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	sc, err := store.GetScenario(ctx, db, scope, scenarioID)
	if err != nil {
		return nil, notFound(err)
	}
	st, err := s.loadScenario(ctx, db, scope, sc)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, db, scope, st)
}

// draftTx locks a draft scenario at version (0 = no check) and loads it, in tx.
func (s *Service) draftTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, version int64) (*scenarioState, error) {
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
	return s.loadScenario(ctx, tx, scope, sc)
}

// mutate runs fn on a locked draft, then stores the items, recomputes moves and indicators, bumps the version
// and returns the detail.
func (s *Service) mutate(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, perm auth.Permission,
	fn func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *scenarioState) error,
) (*ScenarioDetail, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, perm)
	if err != nil {
		return nil, err
	}
	var out *ScenarioDetail
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		st, err := s.draftTx(ctx, tx, scope, id, version)
		if err != nil {
			return err
		}
		expected := st.sc.Version
		if err := fn(ctx, tx, scope, st); err != nil {
			return err
		}
		if err := store.ReplaceScenarioItems(ctx, tx, scope, st.sc.ID, st.items); err != nil {
			return err
		}
		if err := s.refresh(ctx, tx, scope, st, s.today(ctx, tx, scope)); err != nil {
			return err
		}
		if err := store.UpdateScenario(ctx, tx, scope, st.sc, expected, s.now()); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, st)
		return err
	})
	if errors.Is(err, store.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	return out, err
}

// UpdateScenario renames a draft or changes its window (storage.manage, If-Match): the items not touched yet are
// reloaded from the real state of the new window; touched and tray items are kept.
func (s *Service) UpdateScenario(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64,
	patch ScenarioPatch,
) (*ScenarioDetail, error) {
	return s.mutate(ctx, p, marinaID, scenarioID, version, auth.PermStorageManage,
		func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *scenarioState) error {
			if patch.Name != nil {
				v := &auth.ValidationError{}
				st.sc.Name = checkName(v, *patch.Name)
				if err := v.Err(); err != nil {
					return err
				}
			}
			if patch.From == nil && patch.To == nil {
				return nil
			}
			from, to := st.sc.FromOn, st.sc.ToOn
			if patch.From != nil {
				from = *patch.From
			}
			if patch.To != nil {
				to = *patch.To
			}
			if err := checkWindow(from, to); err != nil {
				return err
			}
			st.sc.FromOn, st.sc.ToOn = from, to
			keep := map[string]store.ScenarioItem{}
			for i := range st.items {
				it := st.items[i]
				si := st.snap.item(it.AllocationID)
				touched := it.Origin == store.ScenarioOriginTray || it.Pinned || si == nil ||
					!samePtr(itemAt(&it), si.Placement())
				if touched {
					keep[it.AllocationID] = it
				}
			}
			var ids []string
			for id := range keep {
				ids = append(ids, id)
			}
			sn, err := readSnapshot(ctx, tx, scope, st.area, from, to, ids)
			if err != nil {
				return err
			}
			st.snap, st.extras = sn, nil
			var items []store.ScenarioItem
			base := map[string]int{}
			for i := range sn.Items {
				it := &sn.Items[i]
				if k, ok := keep[it.Allocation.ID]; ok {
					items = append(items, k)
					base[it.Allocation.ID] = st.base[it.Allocation.ID]
					continue
				}
				if !it.OnArea || it.Allocation.Status != store.AllocActive {
					continue
				}
				row := store.ScenarioItem{
					ScenarioID: st.sc.ID, MarinaID: scope.ID(), AllocationID: it.Allocation.ID, VesselID: it.Allocation.VesselID,
					Origin: store.ScenarioOriginArea,
				}
				setItemAt(&row, it.Placement())
				items = append(items, row)
				base[it.Allocation.ID] = it.Allocation.Version
			}
			slices.SortFunc(items, func(a, b store.ScenarioItem) int { return cmp.Compare(a.AllocationID, b.AllocationID) })
			st.items, st.base = items, base
			st.sc.Base = store.MustJSON(base)
			return nil
		})
}

func samePtr(a, b *optimizer.Placement) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameRect(*a, *b)
}

// DiscardScenario sets a draft discarded (storage.manage, If-Match).
func (s *Service) DiscardScenario(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64) error {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		sc, err := store.LockScenario(ctx, tx, scope, scenarioID)
		if err != nil {
			return notFound(err)
		}
		if sc.Status != store.ScenarioDraft {
			return ErrScenarioNotDraft
		}
		if version != 0 && int64(sc.Version) != version {
			return ErrVersionMismatch
		}
		expected := sc.Version
		sc.Status = store.ScenarioDiscarded
		if err := store.UpdateScenario(ctx, tx, scope, sc, expected, s.now()); err != nil {
			return err
		}
		return s.writeAudit(ctx, tx, scope, p, "storage.scenario_discard", "storage_scenario", sc.ID, map[string]any{"area_id": sc.AreaID})
	})
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return err
}

// SaveItems saves the placements of the working copy and recomputes moves and indicators (storage.manage, If-Match).
// Each placed item must pass Footprint and fit (422 with the item); overlaps are allowed and flagged.
func (s *Service) SaveItems(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64,
	items []LayoutItem,
) (*ScenarioDetail, error) {
	return s.mutate(ctx, p, marinaID, scenarioID, version, auth.PermStorageManage,
		func(_ context.Context, _ bun.IDB, _ store.MarinaScope, st *scenarioState) error {
			idx := map[string]int{}
			for i := range st.items {
				idx[st.items[i].AllocationID] = i
			}
			v := &auth.ValidationError{}
			seen := map[string]bool{}
			for _, li := range items {
				i, ok := idx[li.AllocationID]
				if !ok || seen[li.AllocationID] {
					v.Add("items.allocation_id", "invalid")
					continue
				}
				seen[li.AllocationID] = true
				it := &st.items[i]
				if li.At != nil {
					si := st.snap.item(li.AllocationID)
					if si == nil {
						v.Add("items."+li.AllocationID, IssueStale)
						continue
					}
					if _, err := layout.Footprint(layout.Vessel{LOA: si.Fit.LOA, Beam: si.Fit.Beam}, *li.At, st.area.la); err != nil {
						reason := IssueOutside
						if errors.Is(err, layout.ErrBlockedCell) {
							reason = IssueBlockedCell
						}
						v.Add("items."+li.AllocationID, reason)
						continue
					}
					if r := st.area.check(si, si.Allocation.LastNight); !r.OK && (!si.FitOverride || !r.Overridable) {
						v.Add("items."+li.AllocationID, IssueFit)
						continue
					}
				}
				setItemAt(it, li.At)
				it.Pinned = li.Pinned && li.At != nil
			}
			return v.Err()
		})
}

// AddTrayItem brings an allocation of another surface area or land resource into the scenario (storage.manage).
func (s *Service) AddTrayItem(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64,
	allocationID string,
) (*ScenarioDetail, error) {
	return s.mutate(ctx, p, marinaID, scenarioID, version, auth.PermStorageManage,
		func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *scenarioState) error {
			for i := range st.items {
				if st.items[i].AllocationID == allocationID {
					v := &auth.ValidationError{}
					v.Add("allocation_id", "duplicate")
					return v
				}
			}
			as, err := store.AllocationsByIDs(ctx, tx, scope, []string{allocationID})
			if err != nil {
				return err
			}
			if len(as) == 0 {
				return auth.ErrNotFound
			}
			a := as[0]
			v := &auth.ValidationError{}
			switch {
			case a.Status != store.AllocActive || isObstacleKind(a.Kind, a.VesselID):
				v.Add("allocation_id", "invalid")
			case a.ResourceType == store.ResourceBerth:
				v.Add("allocation_id", "not_ashore")
			case a.LastNight.Before(st.sc.FromOn) || a.FirstNight.After(st.sc.ToOn):
				v.Add("allocation_id", "outside_window")
			}
			if err := v.Err(); err != nil {
				return err
			}
			st.items = append(st.items, store.ScenarioItem{
				ScenarioID: st.sc.ID, MarinaID: scope.ID(), AllocationID: a.ID, VesselID: a.VesselID, Origin: store.ScenarioOriginTray,
			})
			slices.SortFunc(st.items, func(x, y store.ScenarioItem) int { return cmp.Compare(x.AllocationID, y.AllocationID) })
			st.base[a.ID] = a.Version
			st.sc.Base = store.MustJSON(st.base)
			sn, err := readSnapshot(ctx, tx, scope, st.area, st.sc.FromOn, st.sc.ToOn, itemIDs(st.items))
			if err != nil {
				return err
			}
			st.snap = sn
			return nil
		})
}

func itemIDs(items []store.ScenarioItem) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].AllocationID
	}
	return out
}

// RemoveTrayItem drops a tray item (storage.manage); area items can only be unplaced.
func (s *Service) RemoveTrayItem(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64,
	allocationID string,
) (*ScenarioDetail, error) {
	return s.mutate(ctx, p, marinaID, scenarioID, version, auth.PermStorageManage,
		func(_ context.Context, _ bun.IDB, _ store.MarinaScope, st *scenarioState) error {
			i := slices.IndexFunc(st.items, func(it store.ScenarioItem) bool { return it.AllocationID == allocationID })
			if i < 0 {
				return auth.ErrNotFound
			}
			if st.items[i].Origin != store.ScenarioOriginTray {
				v := &auth.ValidationError{}
				v.Add("allocation_id", "not_tray")
				return v
			}
			st.items = slices.Delete(st.items, i, i+1)
			delete(st.base, allocationID)
			st.sc.Base = store.MustJSON(st.base)
			return nil
		})
}

// Optimize runs the optimizer on the working copy and stores the result (storage.manage, If-Match). The run reads
// the copy outside any write transaction (the SQLite writer is not held for the optimizer budget); the result is
// stored under the lock only if the scenario is still at version.
func (s *Service) Optimize(ctx context.Context, p *auth.Principal, marinaID, scenarioID string, version int64,
	seed *uint64, maxIter int,
) (*ScenarioDetail, error) {
	// The deadline covers the whole request: the reads before the run count against the budget.
	octx, cancel := s.budget(ctx)
	defer cancel()
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	sc, err := store.GetScenario(ctx, db, scope, scenarioID)
	if err != nil {
		return nil, notFound(err)
	}
	if sc.Status != store.ScenarioDraft {
		return nil, ErrScenarioNotDraft
	}
	if version != 0 && int64(sc.Version) != version {
		return nil, ErrVersionMismatch
	}
	st, err := s.loadScenario(ctx, db, scope, sc)
	if err != nil {
		return nil, err
	}
	sd := uint64(sc.Seed) //nolint:gosec // the int64 bit pattern of the uint64 seed
	if seed != nil {
		sd = *seed
	}
	res, err := s.arrange(octx, st.area, st.snapWithExtras(), st.layout(), sd, maxIter)
	if err != nil {
		return nil, err
	}
	byID := map[string]*optimizer.Placement{}
	for _, pl := range res.Placements {
		byID[pl.ID] = pl.At
	}
	d, err := s.mutate(ctx, p, marinaID, scenarioID, int64(sc.Version), auth.PermStorageManage,
		func(_ context.Context, _ bun.IDB, _ store.MarinaScope, st *scenarioState) error {
			st.sc.Seed = int64(sd) //nolint:gosec // stored as its int64 bit pattern
			for i := range st.items {
				if at, ok := byID[st.items[i].AllocationID]; ok {
					setItemAt(&st.items[i], at)
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	d.Run = &ArrangeResult{
		Items: res.Placements, Seed: sd, Iterations: res.Iterations, StoppedBy: res.StoppedBy, Indicators: *d.Indicators, Access: d.Access,
	}
	return d, nil
}

// Compare compares a scenario with another scenario of the area or with the real state (with = "" or "real").
func (s *Service) Compare(ctx context.Context, p *auth.Principal, marinaID, scenarioID, with string, date *calendar.Date) (*Comparison, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	sc, err := store.GetScenario(ctx, db, scope, scenarioID)
	if err != nil {
		return nil, notFound(err)
	}
	left, err := s.loadScenario(ctx, db, scope, sc)
	if err != nil {
		return nil, err
	}
	if with == "" {
		with = "real"
	}
	cmpOut := &Comparison{ScenarioID: sc.ID, With: with, Date: sc.FromOn}
	if date != nil {
		cmpOut.Date = *date
	}
	lpos := positions(left.snap, left.layout())
	cmpOut.Left = evaluate(left.area, left.snapWithExtras(), left.layout()).ind
	var rpos map[string]*MoveEnd
	if with == "real" {
		rl := realLayout(left.snap)
		cmpOut.Right = evaluate(left.area, left.snap, rl).ind
		rpos = map[string]*MoveEnd{}
		for i := range left.snap.Items {
			it := &left.snap.Items[i]
			rpos[it.Allocation.ID] = endOf(it)
		}
	} else {
		other, err := store.GetScenario(ctx, db, scope, with)
		if err != nil {
			return nil, notFound(err)
		}
		if other.AreaID != sc.AreaID {
			v := &auth.ValidationError{}
			v.Add("with", "other_area")
			return nil, v
		}
		right, err := s.loadScenario(ctx, db, scope, other)
		if err != nil {
			return nil, err
		}
		cmpOut.Right = evaluate(right.area, right.snapWithExtras(), right.layout()).ind
		rpos = positions(right.snap, right.layout())
	}
	ids := map[string]bool{}
	for id := range lpos {
		ids[id] = true
	}
	for id := range rpos {
		if _, ok := lpos[id]; ok || with != "real" {
			ids[id] = true
		}
	}
	var all []string
	for id := range ids {
		all = append(all, id)
	}
	slices.Sort(all)
	for _, id := range all {
		l, r := lpos[id], rpos[id]
		if sameEnd(l, r) {
			continue
		}
		d := PositionDiff{AllocationID: id, Left: l, Right: r}
		if it := left.snap.item(id); it != nil {
			d.VesselID = it.Vessel.ID
		}
		cmpOut.Diffs = append(cmpOut.Diffs, d)
	}
	return cmpOut, nil
}

// positions maps the items of a layout to their scenario position (nil end = unplaced).
func positions(sn *Snapshot, lay []evalItem) map[string]*MoveEnd {
	out := map[string]*MoveEnd{}
	for _, e := range lay {
		if e.at == nil {
			out[e.it.Allocation.ID] = nil
			continue
		}
		at := *e.at
		out[e.it.Allocation.ID] = &MoveEnd{ResourceType: store.ResourceArea, ResourceID: sn.AreaID, At: &at}
	}
	return out
}

// endOf is the current position of a snapshot item.
func endOf(it *SnapshotItem) *MoveEnd {
	return &MoveEnd{ResourceType: it.Allocation.ResourceType, ResourceID: it.Allocation.ResourceID, At: it.Placement()}
}

func sameEnd(a, b *MoveEnd) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ResourceType == b.ResourceType && a.ResourceID == b.ResourceID && samePtr(a.At, b.At)
}

// TrayCandidate is an allocation that AddTrayItem accepts: a vessel's active allocation on another dynamic area or
// a land resource of the marina, overlapping the scenario window, not yet in the scenario.
type TrayCandidate struct {
	Allocation   store.Allocation
	ResourceCode string
	Vessel       *store.Vessel
}

// TrayCandidates lists the "Da collocare" candidates of a draft scenario (storage.read), by resource code and
// first night.
func (s *Service) TrayCandidates(ctx context.Context, p *auth.Principal, marinaID, scenarioID string) ([]TrayCandidate, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	sc, err := store.GetScenario(ctx, db, scope, scenarioID)
	if err != nil {
		return nil, notFound(err)
	}
	if sc.Status != store.ScenarioDraft {
		return nil, ErrScenarioNotDraft
	}
	items, err := store.ScenarioItems(ctx, db, scope, sc.ID)
	if err != nil {
		return nil, err
	}
	in := map[string]bool{}
	for _, it := range items {
		in[it.AllocationID] = true
	}
	var as []store.Allocation
	for _, rt := range []string{store.ResourceArea, store.ResourceLand} {
		rows, err := store.ListAllocations(ctx, db, scope, store.AllocationFilter{ResourceType: rt, Status: store.AllocActive, From: sc.FromOn, To: sc.ToOn})
		if err != nil {
			return nil, err
		}
		for _, a := range rows {
			if in[a.ID] || isObstacleKind(a.Kind, a.VesselID) || (rt == store.ResourceArea && a.ResourceID == sc.AreaID) {
				continue
			}
			as = append(as, a)
		}
	}
	res := make([]string, 0, len(as))
	vids := make([]string, 0, len(as))
	for _, a := range as {
		res, vids = append(res, a.ResourceID), append(vids, a.VesselID)
	}
	refs, err := store.ResourceRefs(ctx, db, scope, res)
	if err != nil {
		return nil, err
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]*store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = &vs[i]
	}
	out := make([]TrayCandidate, 0, len(as))
	for _, a := range as {
		out = append(out, TrayCandidate{Allocation: a, ResourceCode: refs[a.ResourceID].Code, Vessel: vm[a.VesselID]})
	}
	slices.SortFunc(out, func(x, y TrayCandidate) int {
		return cmp.Or(cmp.Compare(x.ResourceCode, y.ResourceCode), x.Allocation.FirstNight.Compare(y.Allocation.FirstNight),
			cmp.Compare(x.Allocation.ID, y.Allocation.ID))
	})
	return out, nil
}
