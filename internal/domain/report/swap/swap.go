package swap

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/report/calc"
)

// DefaultLimit is the maximum number of suggestions (m7-plan §3.3).
const DefaultLimit = 50

// Berth is a berth that may receive a vessel.
type Berth struct {
	ID, Code, MapID string
	Target          fit.Target
}

// Alloc is an allocation of an occupied kind on a berth with every night it holds (its occupancy rows).
type Alloc struct {
	ID       string
	BerthID  string
	MapID    string
	Kind     string // contract, transit (owner allocations are never moved)
	Nights   []calendar.Date
	Vessel   fit.Vessel
	Listed   bool // above the waste threshold
	VesselID string
}

// Input is everything the suggestions read: Busy maps a berth and a night to the allocation holding it (any row
// kind: holds and blocks are busy too), Closed the closed nights of each berth.
type Input struct {
	Berths []Berth
	Allocs []Alloc
	Busy   map[string]map[calendar.Date]string
	Closed map[string]map[calendar.Date]bool
	Rules  fit.Rules
	Today  calendar.Date
	Limit  int
}

// unused is the free surface (cm²) of berth b around vessel v.
func unused(v fit.Vessel, b *Berth) int64 {
	return max(0, int64(b.Target.Length)*int64(b.Target.Width)-int64(v.LOA)*int64(v.Beam))
}

func area(b *Berth) int64 { return int64(b.Target.Length) * int64(b.Target.Width) }

func movable(a *Alloc) bool {
	return (a.Kind == calc.KindContract || a.Kind == calc.KindTransit) && len(a.Nights) > 0 && a.Vessel.LOA > 0
}

// fits reports whether v fits b without blocking issues.
func fits(in *Input, v fit.Vessel, b *Berth, last calendar.Date) bool {
	return fit.Check(v, b.Target, last, in.Rules).OK
}

// freeFor reports whether every night of a is free on berth b, or held by the allocation other (a swap).
func freeFor(in *Input, a *Alloc, b *Berth, other string) bool {
	busy, closed := in.Busy[b.ID], in.Closed[b.ID]
	for _, n := range a.Nights {
		if closed[n] {
			return false
		}
		if h, ok := busy[n]; ok && h != other && h != a.ID {
			return false
		}
	}
	return true
}

type candidate struct {
	s    calc.WasteSuggestion
	gain int64
	ids  []string
}

// Suggest returns swaps and moves that strictly reduce the wasted surface × nights, best gain first, every
// allocation in at most one suggestion, at most Limit (truncated reports whether more were found). It never
// changes anything (D21).
func Suggest(in Input) (out []calc.WasteSuggestion, truncated bool) {
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	berths := make(map[string]*Berth, len(in.Berths))
	for i := range in.Berths {
		berths[in.Berths[i].ID] = &in.Berths[i]
	}
	var cands []candidate
	for i := range in.Allocs {
		a := &in.Allocs[i]
		x := berths[a.BerthID]
		if !a.Listed || !movable(a) || x == nil {
			continue
		}
		for j := range in.Allocs {
			b := &in.Allocs[j]
			y := berths[b.BerthID]
			if i == j || !movable(b) || y == nil || y.ID == x.ID || b.MapID != a.MapID || (b.Listed && j < i) {
				continue
			}
			na, nb := int64(len(a.Nights)), int64(len(b.Nights))
			before := unused(a.Vessel, x)*na + unused(b.Vessel, y)*nb
			after := unused(a.Vessel, y)*na + unused(b.Vessel, x)*nb
			if after >= before {
				continue
			}
			if !fits(&in, a.Vessel, y, a.Nights[len(a.Nights)-1]) || !fits(&in, b.Vessel, x, b.Nights[len(b.Nights)-1]) {
				continue
			}
			if !freeFor(&in, a, y, b.ID) || !freeFor(&in, b, x, a.ID) {
				continue
			}
			areaBefore := area(x)*na + area(y)*nb
			areaAfter := area(y)*na + area(x)*nb
			cands = append(cands, candidate{
				gain: before - after, ids: []string{a.ID, b.ID},
				s: calc.WasteSuggestion{
					Kind: "swap", AllocationIDs: []string{a.ID, b.ID},
					FromBerths:    []calc.ResourceBrief{{ID: x.ID, Code: x.Code}, {ID: y.ID, Code: y.Code}},
					ToBerths:      []calc.ResourceBrief{{ID: y.ID, Code: y.Code}, {ID: x.ID, Code: x.Code}},
					WasteBeforeBP: calc.Ratio(before, areaBefore).Value, WasteAfterBP: calc.Ratio(after, areaAfter).Value,
					GainCm2Nights: before - after,
				},
			})
		}
	}
	cands = append(cands, moves(&in, berths)...)
	slices.SortStableFunc(cands, func(p, q candidate) int {
		if c := cmp.Compare(q.gain, p.gain); c != 0 {
			return c
		}
		if c := cmp.Compare(p.s.Kind, q.s.Kind); c != 0 {
			return -c // swaps before moves on equal gain
		}
		return slices.Compare(p.ids, q.ids)
	})
	used := map[string]bool{}
	for _, c := range cands {
		if slices.ContainsFunc(c.ids, func(id string) bool { return used[id] }) {
			continue
		}
		if len(out) == limit {
			return out, true
		}
		for _, id := range c.ids {
			used[id] = true
		}
		out = append(out, c.s)
	}
	return out, false
}

// moves runs the occupancy engine for each listed allocation on a snapshot of the berths and keeps the best
// single-berth solution on another berth with less waste.
func moves(in *Input, berths map[string]*Berth) []candidate {
	snap := engine.Snapshot{Today: in.Today, Busy: map[string][]engine.NightRange{}}
	for _, b := range in.Berths {
		snap.Berths = append(snap.Berths, engine.BerthCand{ID: b.ID, Code: b.Code, MapID: b.MapID, Target: b.Target})
		var nights []calendar.Date
		for n := range in.Busy[b.ID] {
			nights = append(nights, n)
		}
		for n := range in.Closed[b.ID] {
			if _, ok := in.Busy[b.ID][n]; !ok {
				nights = append(nights, n)
			}
		}
		slices.SortFunc(nights, func(p, q calendar.Date) int { return p.Compare(q) })
		snap.Busy[b.ID] = engine.GroupNights(nights)
	}
	var out []candidate
	for i := range in.Allocs {
		a := &in.Allocs[i]
		x := berths[a.BerthID]
		if !a.Listed || !movable(a) || x == nil {
			continue
		}
		purpose := engine.PurposeContract
		if a.Kind == calc.KindTransit {
			purpose = engine.PurposeTransit
		}
		first, last := a.Nights[0], a.Nights[len(a.Nights)-1]
		req := engine.Request{
			Vessel: a.Vessel, VesselID: a.VesselID, Arrival: first, Departure: last.AddDays(1), Purpose: purpose,
			ResourceTypes: []engine.ResourceType{engine.ResourceBerth}, Channel: "desk", Limit: 10,
		}
		n := int64(len(a.Nights))
		for _, sol := range engine.Solve(snap, req, in.Rules) {
			if len(sol.Segments) != 1 || sol.RequiresRelease != nil {
				continue
			}
			y := berths[sol.Segments[0].ResourceID]
			if y == nil || y.ID == x.ID || y.MapID != x.MapID || !freeFor(in, a, y, "") {
				continue
			}
			before, after := unused(a.Vessel, x)*n, unused(a.Vessel, y)*n
			if after >= before {
				continue
			}
			out = append(out, candidate{
				gain: before - after, ids: []string{a.ID},
				s: calc.WasteSuggestion{
					Kind: "move", AllocationIDs: []string{a.ID},
					FromBerths:    []calc.ResourceBrief{{ID: x.ID, Code: x.Code}},
					ToBerths:      []calc.ResourceBrief{{ID: y.ID, Code: y.Code}},
					WasteBeforeBP: calc.Ratio(before, area(x)*n).Value, WasteAfterBP: calc.Ratio(after, area(y)*n).Value,
					GainCm2Nights: before - after,
				},
			})
			break
		}
	}
	return out
}
