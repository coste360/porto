package storage

import (
	"context"
	"errors"
	"hash/fnv"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/store"
)

// evalItem is an item of a layout evaluation: a snapshot item at a position (nil = unplaced).
type evalItem struct {
	it     *SnapshotItem
	at     *optimizer.Placement
	pinned bool
}

// evalResult is the evaluation of a layout.
type evalResult struct {
	ind    Indicators
	access []Access
	issues []ItemIssue
	cells  map[string][]int // valid positions only
}

// sameRect reports whether two placements cover the same rectangle (0/180 and 90/270 are the same, m9-plan D13).
func sameRect(a, b optimizer.Placement) bool {
	return a.X == b.X && a.Y == b.Y && a.Rotation%1800 == b.Rotation%1800
}

func intersects(a, b []int) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			return true
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return false
}

func nightsOverlap(f1, l1, f2, l2 calendar.Date) bool { return !f1.After(l2) && !f2.After(l1) }

// evaluate computes indicators, accessibility and per-item issues of a layout of the snapshot window (m9-plan
// §7.2). Items not in the layout are not part of it; obstacles are the snapshot's.
func evaluate(a *area, sn *Snapshot, items []evalItem) evalResult {
	res := evalResult{cells: map[string][]int{}, issues: []ItemIssue{}}
	ind := &res.ind
	cell2 := int64(a.la.Cell) * int64(a.la.Cell)
	for _, u := range a.la.Usable {
		if u {
			ind.UsableCm2 += cell2
		}
	}
	type valid struct {
		e           evalItem
		cells       []int
		first, last calendar.Date
	}
	var placed []valid
	for _, e := range items {
		it := e.it
		id := it.Allocation.ID
		if cur := it.Placement(); e.at != nil && (cur == nil || !sameRect(*e.at, *cur)) {
			ind.Moves++
		}
		if e.at == nil {
			ind.Unplaced++
			res.issues = append(res.issues, ItemIssue{AllocationID: id, Reason: IssueUnplaced})
			continue
		}
		fp, err := layout.Footprint(layout.Vessel{LOA: it.Fit.LOA, Beam: it.Fit.Beam}, *e.at, a.la)
		if err != nil {
			reason := IssueOutside
			if errors.Is(err, layout.ErrBlockedCell) {
				reason = IssueBlockedCell
			}
			res.issues = append(res.issues, ItemIssue{AllocationID: id, Reason: reason})
			continue
		}
		first, last, ok := clip(it.Allocation.FirstNight, it.Allocation.LastNight, sn.From, sn.To)
		if !ok {
			first, last = it.Allocation.FirstNight, it.Allocation.LastNight
		}
		if r := a.check(it, it.Allocation.LastNight); !r.OK && (!it.FitOverride || !r.Overridable) {
			res.issues = append(res.issues, ItemIssue{AllocationID: id, Reason: IssueFit, FitCodes: r.BlockingCodeList()})
		}
		res.cells[id] = fp.Indexes
		placed = append(placed, valid{e: e, cells: fp.Indexes, first: first, last: last})
	}
	overlapping := map[string][]string{}
	for i := range placed {
		x := &placed[i]
		for j := i + 1; j < len(placed); j++ {
			y := &placed[j]
			if nightsOverlap(x.first, x.last, y.first, y.last) && intersects(x.cells, y.cells) {
				xi, yi := x.e.it.Allocation.ID, y.e.it.Allocation.ID
				overlapping[xi] = append(overlapping[xi], yi)
				overlapping[yi] = append(overlapping[yi], xi)
			}
		}
		for _, o := range sn.Obstacles {
			if nightsOverlap(x.first, x.last, o.First, o.Last) && intersects(x.cells, sortedCopy(o.Cells)) {
				xi := x.e.it.Allocation.ID
				overlapping[xi] = append(overlapping[xi], o.ID)
			}
		}
	}
	for _, p := range placed {
		id := p.e.it.Allocation.ID
		if with := overlapping[id]; len(with) > 0 {
			ind.Overlaps++
			slices.Sort(with)
			res.issues = append(res.issues, ItemIssue{AllocationID: id, Reason: IssueOverlap, WithAllocationID: slices.Compact(with)})
		}
	}
	// Surfaces on the start night.
	var occupied []int
	for _, p := range placed {
		if !nightsOverlap(p.first, p.last, sn.From, sn.From) {
			continue
		}
		ind.UsedHullCm2 += int64(p.e.it.Fit.LOA) * int64(p.e.it.Fit.Beam)
		ind.UsedFootprintCm2 += int64(len(p.cells)) * cell2
		occupied = append(occupied, p.cells...)
	}
	for _, o := range sn.Obstacles {
		if nightsOverlap(o.First, o.Last, sn.From, sn.From) {
			occupied = append(occupied, o.Cells...)
		}
	}
	ind.LargestFreeRectCm2 = optimizer.LargestFreeRect(a.la, occupied)
	// Accessibility timeline.
	tis := make([]optimizer.TimelineItem, 0, len(placed))
	vessel := map[string]string{}
	for _, p := range placed {
		it := p.e.it
		vessel[it.Allocation.ID] = it.Vessel.ID
		tis = append(tis, optimizer.TimelineItem{Placed: optimizer.Placed{
			ID: it.Allocation.ID, LOA: it.Fit.LOA, Beam: it.Fit.Beam, At: *p.e.at, First: it.Allocation.FirstNight,
			Last: it.Allocation.LastNight, ExitOn: it.ExitOn,
		}, ArrivalOn: it.ArrivalOn})
	}
	acc := optimizer.AccessibilityTimeline(a.grid, sn.obstacles(), tis, sn.From, sn.To)
	byDate := map[calendar.Date]*DateBlocked{}
	var dates []calendar.Date
	for _, x := range acc {
		res.access = append(res.access, Access{Access: x, VesselID: vessel[x.ID]})
		if x.Accessible {
			continue
		}
		db := byDate[x.On]
		if db == nil {
			db = &DateBlocked{Date: x.On}
			byDate[x.On] = db
			dates = append(dates, x.On)
		}
		if x.Event == optimizer.EventExit {
			ind.ExitsBlocked++
			db.ExitsBlocked++
		} else {
			ind.ArrivalsBlocked++
			db.ArrivalsBlocked++
		}
	}
	slices.SortFunc(dates, func(x, y calendar.Date) int { return x.Compare(y) })
	ind.BlockedByDate = []DateBlocked{}
	for _, d := range dates {
		ind.BlockedByDate = append(ind.BlockedByDate, *byDate[d])
	}
	if res.access == nil {
		res.access = []Access{}
	}
	return res
}

func sortedCopy(c []int) []int {
	if slices.IsSorted(c) {
		return c
	}
	s := slices.Clone(c)
	slices.Sort(s)
	return s
}

// realLayout is the layout of the real state: every item of the snapshot on the area at its position.
func realLayout(sn *Snapshot) []evalItem {
	out := make([]evalItem, 0, len(sn.Items))
	for i := range sn.Items {
		if sn.Items[i].OnArea {
			out = append(out, evalItem{it: &sn.Items[i], at: sn.Items[i].Placement()})
		}
	}
	return out
}

// withFixed returns the snapshot where the boats of the area that are not in the layout are fixed obstacles at
// their real position (a partial draft keeps the others where they are).
func withFixed(sn *Snapshot, lay []evalItem) *Snapshot {
	in := map[string]bool{}
	for _, e := range lay {
		in[e.it.Allocation.ID] = true
	}
	out := *sn
	out.Obstacles = slices.Clone(sn.Obstacles)
	for i := range sn.Items {
		it := &sn.Items[i]
		if in[it.Allocation.ID] || !it.OnArea || len(it.Cells) == 0 {
			continue
		}
		out.Obstacles = append(out.Obstacles, Obstacle{Kind: it.Allocation.Kind, Obstacle: optimizer.Obstacle{
			ID: it.Allocation.ID, Cells: it.Cells, First: it.Allocation.FirstNight, Last: it.Allocation.LastNight,
		}})
	}
	return &out
}

// draftLayout builds a layout from draft items (unknown allocation ids are a validation error).
func draftLayout(sn *Snapshot, draft []LayoutItem) ([]evalItem, error) {
	out := make([]evalItem, 0, len(draft))
	seen := map[string]bool{}
	v := &auth.ValidationError{}
	for _, d := range draft {
		it := sn.item(d.AllocationID)
		if it == nil || seen[d.AllocationID] {
			v.Add("items.allocation_id", "invalid")
			continue
		}
		seen[d.AllocationID] = true
		out = append(out, evalItem{it: it, at: d.At, pinned: d.Pinned})
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// window resolves an optional night window (default today .. today + PlanDefaultDays).
func window(today calendar.Date, from, to *calendar.Date) (calendar.Date, calendar.Date, error) {
	f, t := today, today.AddDays(PlanDefaultDays)
	if from != nil {
		f = *from
		if to == nil {
			t = f.AddDays(PlanDefaultDays)
		}
	}
	if to != nil {
		t = *to
	}
	if t.Before(f) || f.DaysUntil(t)+1 > MaxScenarioNights {
		return f, t, ErrWindowInvalid
	}
	return f, t, nil
}

// SeedOf is the default optimizer seed of an id (FNV-1a 64, m9-plan §6.3).
func SeedOf(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// arrange runs the optimizer on a layout of the snapshot: items of the layout are the run, the other snapshot
// items on the area stay where they are (fixed obstacles). ctx carries the deadline set by budget when the request
// started, so the reads before the run count against the budget too.
func (s *Service) arrange(ctx context.Context, a *area, sn *Snapshot, layout []evalItem, seed uint64, maxIter int) (optimizer.Result, error) {
	if len(a.gates) > 0 || len(a.grid.Gates) == 0 {
		return optimizer.Result{}, ErrExitsInvalid
	}
	if len(layout) > optimizer.MaxItems {
		return optimizer.Result{}, ErrTooManyItems
	}
	in := optimizer.Input{
		Grid: a.grid, Fixed: sn.obstacles(), Fit: a.fitParams(), Seed: seed, MaxIter: maxIter, Margin: a.la.Margin,
		From: sn.From, To: sn.To,
	}
	inRun := map[string]bool{}
	for _, e := range layout {
		inRun[e.it.Allocation.ID] = true
		in.Items = append(in.Items, e.it.optItem(sn.From, sn.To, e.at, e.pinned && e.at != nil))
	}
	for i := range sn.Items {
		it := &sn.Items[i]
		if inRun[it.Allocation.ID] || !it.OnArea || len(it.Cells) == 0 {
			continue
		}
		if first, last, ok := clip(it.Allocation.FirstNight, it.Allocation.LastNight, sn.From, sn.To); ok {
			in.Fixed = append(in.Fixed, optimizer.Obstacle{ID: it.Allocation.ID, Cells: it.Cells, First: first, Last: last})
		}
	}
	res, err := optimizer.Arrange(ctx, in)
	switch {
	case errors.Is(err, optimizer.ErrPinnedInvalid):
		return res, ErrPinnedInvalid
	case errors.Is(err, optimizer.ErrTooManyItems):
		return res, ErrTooManyItems
	case errors.Is(err, optimizer.ErrInvalidLayout):
		// A bug: never ship an invalid layout; the seed reproduces the run.
		s.d.Log.ErrorContext(ctx, "storage: optimizer produced an invalid layout", "area_id", a.row.ID, "seed", seed)
		return res, err
	}
	return res, err
}

// budget returns ctx with the optimizer deadline (Deps.OptimizeBudget) from now.
func (s *Service) budget(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.d.OptimizeBudget)
}

// layoutOf converts an optimizer result to a layout of the snapshot (pinned flags from the input layout).
func layoutOf(sn *Snapshot, in []evalItem, res optimizer.Result) []evalItem {
	pinned := map[string]bool{}
	for _, e := range in {
		pinned[e.it.Allocation.ID] = e.pinned
	}
	out := make([]evalItem, 0, len(res.Placements))
	for _, p := range res.Placements {
		out = append(out, evalItem{it: sn.item(p.ID), at: p.At, pinned: pinned[p.ID]})
	}
	return out
}

// accessOf adds the vessel ids to optimizer access entries.
func accessOf(sn *Snapshot, acc []optimizer.Access) []Access {
	out := make([]Access, 0, len(acc))
	for _, x := range acc {
		a := Access{Access: x}
		if it := sn.item(x.ID); it != nil {
			a.VesselID = it.Vessel.ID
		}
		out = append(out, a)
	}
	return out
}

// today is the marina date of now.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	loc := time.UTC
	if name, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(name); err == nil {
			loc = l
		}
	}
	return calendar.In(s.d.Clock.Now(), loc)
}
