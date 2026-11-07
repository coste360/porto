package engine

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// Score weights (m3-plan §2.2, 10.7 step 4). Lower is better.
const (
	PenaltyNotTransitDedicated int64 = 50_000
	PenaltyPrefMiss            int64 = 20_000
	PenaltyNotPrevious         int64 = 10_000
	PenaltySegment             int64 = 200_000
	PenaltyRequiresRelease     int64 = 300_000
	DefaultLimit                     = 20
)

// candidate is one resource the solver may place the vessel on, with what it needs to score and to place it.
type candidate struct {
	typ        ResourceType
	id, code   string
	target     fit.Target
	groups     []string
	structure  string
	water      bool
	dedicated  bool
	area       *AreaCand
	busy       []NightRange // berth/land
	outOK      *OutCand
	closedDays []NightRange
}

// wants reports whether the request searches resources of type t. Without explicit types a search looks for a
// place in the water (berths and dynamic areas): land resources are offered only when asked for.
func (r Request) wants(t ResourceType) bool {
	if len(r.ResourceTypes) == 0 {
		return t != ResourceLand
	}
	return slices.Contains(r.ResourceTypes, t)
}

func (r Request) lastNight() calendar.Date { return r.Departure.AddDays(-1) }

func candidates(s Snapshot, r Request) []candidate {
	var out []candidate
	closedFor := func(id string) []NightRange {
		var rs []NightRange
		for _, c := range s.Closures {
			if len(c.ResourceIDs) == 0 || slices.Contains(c.ResourceIDs, id) {
				rs = append(rs, NightRange{From: c.From, To: c.To})
			}
		}
		return rs
	}
	if r.wants(ResourceBerth) {
		for i := range s.Berths {
			b := &s.Berths[i]
			c := candidate{
				typ: ResourceBerth, id: b.ID, code: b.Code, target: b.Target, groups: b.GroupIDs, structure: b.StructureID,
				water: b.Water, dedicated: b.TransitDedicated, busy: s.Busy[b.ID], closedDays: closedFor(b.ID),
			}
			if o, ok := s.OutOK[b.ID]; ok {
				c.outOK = &o
			}
			out = append(out, c)
		}
	}
	if r.wants(ResourceArea) {
		for i := range s.Areas {
			a := &s.Areas[i]
			out = append(out, candidate{
				typ: ResourceArea, id: a.ID, code: a.Code, target: a.Target, groups: a.GroupIDs, structure: a.StructureID,
				dedicated: a.TransitDedicated, area: a, closedDays: closedFor(a.ID),
			})
		}
	}
	if r.wants(ResourceLand) {
		for i := range s.Lands {
			l := &s.Lands[i]
			out = append(out, candidate{
				typ: ResourceLand, id: l.ID, code: l.Code, target: l.Target, groups: l.GroupIDs, busy: s.Busy[l.ID],
				closedDays: closedFor(l.ID),
			})
		}
	}
	return out
}

func (c *candidate) closedOn(d calendar.Date) bool { return Released(c.closedDays, d) }

func (c *candidate) busyIn(first, last calendar.Date) bool {
	for _, b := range c.busy {
		if !b.To.Before(first) && !b.From.After(last) {
			return true
		}
	}
	return false
}

// freeRun returns the last night ≥ from (up to last) such that [from, end] is free, or from − 1 when from is busy.
func (c *candidate) freeRun(from, last calendar.Date) calendar.Date {
	end := last
	for _, b := range c.busy {
		if b.To.Before(from) || b.From.After(end) {
			continue
		}
		if !b.From.After(from) {
			return from.AddDays(-1)
		}
		end = b.From.AddDays(-1)
	}
	return end
}

func (c *candidate) prefMisses(p Prefs) int64 {
	var n int64
	if len(p.GroupIDs) > 0 && !slices.ContainsFunc(c.groups, func(g string) bool { return slices.Contains(p.GroupIDs, g) }) {
		n++
	}
	if len(p.StructureIDs) > 0 && !slices.Contains(p.StructureIDs, c.structure) {
		n++
	}
	if p.PowerA != nil && (c.target.PowerA == nil || *c.target.PowerA < *p.PowerA) {
		n++
	}
	if p.Water && !c.water && c.typ == ResourceBerth {
		n++
	}
	return n
}

func (c *candidate) baseScore(s Snapshot, r Request) int64 {
	var sc int64
	if r.Purpose == PurposeTransit && !c.dedicated {
		sc += PenaltyNotTransitDedicated
	}
	sc += PenaltyPrefMiss * c.prefMisses(r.Prefs)
	if prev, ok := s.Previous[r.VesselID]; ok && r.VesselID != "" && prev != c.id {
		sc += PenaltyNotPrevious
	}
	return sc
}

// waste is the unused surface in cm² of a berth or a boat-holding land resource.
func (c *candidate) waste(v fit.Vessel) int64 {
	if c.typ == ResourceLand && c.target.LandKind != fit.LandCradle && c.target.LandKind != fit.LandTrailerSpot {
		return 0
	}
	return max(0, int64(c.target.Length)*int64(c.target.Width)-int64(v.LOA)*int64(v.Beam))
}

// Solve returns the solutions of r on the snapshot s, best first (m3-plan §2.2 scoring). Only resources that
// fit (no blocking code) and whose nights are free are proposed; a berth taken by a contract whose vessel is
// out is proposed with RequiresRelease when r.ConsiderOut. Segments are searched only when no single resource
// solution exists.
func Solve(s Snapshot, r Request, rules fit.Rules) []Solution {
	if !r.Departure.After(r.Arrival) {
		return nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	first, last := r.Arrival, r.lastNight()
	cands := candidates(s, r)
	var sols []Solution
	for i := range cands {
		c := &cands[i]
		if c.closedOn(first) {
			continue
		}
		switch c.typ {
		case ResourceArea:
			sols = append(sols, solveArea(s, r, rules, c, first, last)...)
		default:
			res := fit.Check(r.Vessel, c.target, last, rules)
			if !res.OK {
				continue
			}
			seg := Segment{ResourceType: c.typ, ResourceID: c.id, First: first, Last: last}
			sc := c.baseScore(s, r) + c.waste(r.Vessel)/100
			if !c.busyIn(first, last) {
				sols = append(sols, newSolution([]Segment{seg}, sc, res, c.code, nil))
			} else if r.ConsiderOut && c.outOK != nil {
				o := *c.outOK
				sols = append(sols, newSolution([]Segment{seg}, sc+PenaltyRequiresRelease, res, c.code, &o))
			}
		}
	}
	if len(sols) == 0 && r.MaxSegments > 0 {
		sols = solveSegments(s, r, rules, cands, first, last, limit)
	}
	sortSolutions(sols)
	if len(sols) > limit {
		sols = sols[:limit]
	}
	for i := range sols {
		sols[i].sortCode = ""
	}
	return sols
}

func newSolution(segs []Segment, score int64, res fit.Result, code string, out *OutCand) Solution {
	return Solution{Key: SolutionKey(segs), Segments: segs, Score: score, Fit: res, RequiresRelease: out, sortCode: code}
}

func sortSolutions(sols []Solution) {
	slices.SortStableFunc(sols, func(a, b Solution) int {
		if c := cmp.Compare(a.Score, b.Score); c != 0 {
			return c
		}
		if c := cmp.Compare(a.sortCode, b.sortCode); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
}

// SolutionKey is the stable key of a solution: resource ids, placements and nights of its segments.
func SolutionKey(segs []Segment) string {
	var b strings.Builder
	for i, s := range segs {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(string(s.ResourceType))
		b.WriteByte(':')
		b.WriteString(s.ResourceID)
		if s.Placement != nil {
			b.WriteString(":" + strconv.FormatInt(int64(s.Placement.X), 10) + "," + strconv.FormatInt(int64(s.Placement.Y), 10) +
				"," + strconv.FormatInt(int64(s.Placement.Rotation), 10))
		}
		b.WriteString(":" + s.First.String() + ":" + s.Last.String())
	}
	return b.String()
}

// LinearArea returns the footprint view of a linear area candidate.
func (a *AreaCand) LinearArea() layout.Area {
	return layout.Area{Kind: layout.AreaLinear, Length: a.LengthCm, Mode: a.Mode, Cell: a.CellCm, Margin: a.MarginCm}
}

// FootprintWidth is the length along the axis a vessel takes in a linear area (beam or LOA, plus the margin).
func FootprintWidth(a layout.Area, v layout.Vessel) geo.Cm {
	if a.Mode == layout.ModeAlongside {
		return v.LOA + a.Margin
	}
	return v.Beam + a.Margin
}

func snapUp(x geo.Cm) geo.Cm {
	if r := x % layout.Snap; r != 0 {
		if x < 0 {
			return x - r
		}
		return x - r + layout.Snap
	}
	return x
}

func snapDown(x geo.Cm) geo.Cm {
	if r := x % layout.Snap; r != 0 {
		if x < 0 {
			return x - r - layout.Snap
		}
		return x - r
	}
	return x
}

// MinDepth returns the minimum depth of a linear area profile over [from, to] along the axis (step function:
// each point holds until the next one; before the first point the first depth applies). nil when no profile.
func MinDepth(profile []DepthPoint, from, to geo.Cm) *geo.Cm {
	if len(profile) == 0 {
		return nil
	}
	pts := slices.Clone(profile)
	slices.SortFunc(pts, func(a, b DepthPoint) int { return cmp.Compare(a.AtCm, b.AtCm) })
	cur := pts[0].DepthCm
	best := geo.Cm(-1)
	for _, p := range pts {
		if p.AtCm <= from {
			cur = p.DepthCm
			continue
		}
		if p.AtCm > to {
			break
		}
		if best < 0 || cur < best {
			best = cur
		}
		cur = p.DepthCm
	}
	if best < 0 || cur < best {
		best = cur
	}
	return &best
}

// solveArea proposes, for each free interval of a linear area that holds the footprint, the position at the
// interval start (snapped up, at least margin/2 from the area start) and the one adjacent to the next vessel
// (interval end − width, snapped down). Smaller intervals score better (best fit).
func solveArea(s Snapshot, r Request, rules fit.Rules, c *candidate, first, last calendar.Date) []Solution {
	a := c.area
	la := a.LinearArea()
	v := layout.Vessel{LOA: r.Vessel.LOA, Beam: r.Vessel.Beam}
	w := FootprintWidth(la, v)
	n := layout.CellCount(la)
	if n == 0 || w <= 0 {
		return nil
	}
	nightly := s.AreaBusy[a.ID]
	busy := make([][]Interval, 0, len(nightly))
	for d, ivs := range nightly {
		if !d.Before(first) && !d.After(last) {
			busy = append(busy, ivs)
		}
	}
	var out []Solution
	base := c.baseScore(s, r)
	for _, iv := range FreeIntervals(n, busy) {
		start, end := geo.Cm(iv.First)*a.CellCm, min(geo.Cm(iv.Last+1)*a.CellCm, a.LengthCm)
		if end-start < w {
			continue
		}
		xs := []geo.Cm{snapUp(max(start, snapUp(a.MarginCm/2))), snapDown(end - w)}
		if xs[1] == xs[0] {
			xs = xs[:1]
		}
		for _, x := range xs {
			p := Placement{X: x}
			cells, err := layout.Footprint(v, layout.Placement(p), la)
			if err != nil || cells.First < iv.First || cells.Last > iv.Last {
				continue
			}
			t := c.target
			t.Depth = MinDepth(a.DepthProfile, x, x+w)
			res := fit.Check(r.Vessel, t, last, rules)
			if !res.OK {
				continue
			}
			slack := int64(end - start - w)
			sc := base + slack*int64(r.Vessel.Beam)/100
			seg := Segment{ResourceType: ResourceArea, ResourceID: a.ID, Placement: &p, First: first, Last: last}
			out = append(out, newSolution([]Segment{seg}, sc, res, c.code+":"+pad(int64(x)), nil))
		}
	}
	return out
}

func pad(x int64) string {
	s := strconv.FormatInt(x, 10)
	return strings.Repeat("0", max(0, 10-len(s))) + s
}

// areaRun returns the longest run [from, end] (end ≤ last) for which the linear area has a placement for the
// vessel, with the best placement of that run and its fit; end < from when there is none. A placement free for
// [from, end] is free for any shorter run, so the end is found by bisection.
func areaRun(s Snapshot, r Request, rules fit.Rules, c *candidate, from, last calendar.Date) (calendar.Date, *Placement, fit.Result) {
	best := func(end calendar.Date) *Solution {
		sols := solveArea(s, r, rules, c, from, end)
		if len(sols) == 0 {
			return nil
		}
		sortSolutions(sols)
		return &sols[0]
	}
	lo, hi := 0, Nights(from, last)-1 // offsets of the end night from `from`
	var found *Solution
	foundEnd := from.AddDays(-1)
	for lo <= hi {
		mid := (lo + hi) / 2
		end := from.AddDays(mid)
		if sol := best(end); sol != nil {
			found, foundEnd, lo = sol, end, mid+1
		} else {
			hi = mid - 1
		}
	}
	if found == nil {
		return foundEnd, nil, fit.Result{}
	}
	return foundEnd, found.Segments[0].Placement, found.Fit
}

// solveSegments chains up to MaxSegments changes over berths, linear areas and land resources (spec 10.7 step 5):
// each segment takes the longest free run of a resource from its first night (on a linear area, the longest run
// some placement holds), the next one continues on another resource.
func solveSegments(s Snapshot, r Request, rules fit.Rules, cands []candidate, first, last calendar.Date, limit int) []Solution {
	var fitting []*candidate
	fits := map[string]fit.Result{}
	for i := range cands {
		c := &cands[i]
		if c.typ == ResourceArea {
			if c.area != nil { // linear areas only (surface areas are not searched, §13 D7)
				fitting = append(fitting, c)
			}
			continue
		}
		res := fit.Check(r.Vessel, c.target, last, rules)
		if res.OK {
			fitting = append(fitting, c)
			fits[c.id] = res
		}
	}
	var out []Solution
	var walk func(from calendar.Date, changes int, segs []Segment, score int64, worst fit.Result, code string)
	walk = func(from calendar.Date, changes int, segs []Segment, score int64, worst fit.Result, code string) {
		if len(out) >= limit*4 {
			return
		}
		for _, c := range fitting {
			if len(segs) > 0 && segs[len(segs)-1].ResourceID == c.id {
				continue
			}
			if len(segs) == 0 && c.closedOn(from) {
				continue
			}
			end, res := c.freeRun(from, last), fits[c.id]
			var pl *Placement
			if c.typ == ResourceArea {
				end, pl, res = areaRun(s, r, rules, c, from, last)
			}
			if end.Before(from) {
				continue
			}
			seg := Segment{ResourceType: c.typ, ResourceID: c.id, Placement: pl, First: from, Last: end}
			next := append(slices.Clone(segs), seg)
			sc := score + c.baseScore(s, r) + c.waste(r.Vessel)/100
			w := fit.Worse(worst, res)
			if len(segs) == 0 {
				w = res
			}
			cd := code + c.code
			if pl != nil {
				cd += ":" + pad(int64(pl.X))
			}
			if end.Equal(last) {
				if len(next) > 1 {
					out = append(out, newSolution(next, sc+PenaltySegment*int64(len(next)-1), w, cd, nil))
				}
				continue
			}
			if changes > 0 {
				walk(end.AddDays(1), changes-1, next, sc, w, cd+"/")
			}
		}
	}
	walk(first, min(r.MaxSegments, 2), nil, 0, fit.Result{OK: true}, "")
	return out
}
