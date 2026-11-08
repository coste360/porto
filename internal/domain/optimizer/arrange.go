package optimizer

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// stallIterations stops the local search when the best score has not improved for this many iterations (a
// deterministic budget, reported as StoppedByIterations).
const stallIterations = 1000

// checkEvery is how often (in iterations) the caller deadline is checked.
const checkEvery = 64

// topCandidates is the size of the candidate shortlist of a relocation (m9-plan §6.3).
const topCandidates = 8

// placeOneTries bounds the candidate positions PlaceOne evaluates with the full accessibility timeline.
const placeOneTries = 48

// rItem is an item of a run in working cells and day numbers.
type rItem struct {
	src       *Item
	l, b      geo.Cm // footprint size (margins included)
	dw, dh    int    // orientation-0 box size in working cells
	first     int
	last      int
	arr, exit int
	area      int64
	cur       *box // box of the current placement when it is valid in the area
	pinned    bool
	fixed     bool // PlaceOne: an existing boat that stays where it is
	fitOK     bool
	fitCodes  []string
	dist      [2][]int32 // lazily computed gate distance per top-left state
}

// pos is the position of an item in a layout state; keep = output the item's Current placement unchanged.
type pos struct {
	set  bool
	b    box
	keep bool
}

type run struct {
	in       Input
	w        *wgrid
	s        *searcher
	tl       *timeline
	items    []rItem
	obst     []tObst
	base     calendar.Date
	from, to int
	blk      []bool
	t        *sat
	titems   []tItem
	rnd      *rng
	freeArea int64 // free cells on the start night without boats (for the optimal stop)
}

func (r *run) day(d calendar.Date) int { return r.base.DaysUntil(d) }

// prepare converts the input; pinned items that are not valid are ErrPinnedInvalid.
func prepare(in Input, fixedIDs map[string]bool) (*run, error) {
	if len(in.Items) > MaxItems {
		return nil, ErrTooManyItems
	}
	g := in.Grid
	if g.Area.Kind != layout.AreaSurface || g.Area.Cols <= 0 || g.Area.Rows <= 0 || g.Area.Cell <= 0 {
		return nil, fmt.Errorf("%w: not a surface grid", ErrInvalidLayout)
	}
	if len(g.Dist) == 0 {
		ng := NewGrid(g.Area, g.Exits)
		ng.Coarse = max(g.Coarse, ng.Coarse)
		g = ng
		in.Grid = g
	}
	w := newWork(g)
	r := &run{in: in, w: w, s: newSearcher(w), rnd: newRNG(in.Seed)}
	r.tl = &timeline{d: newDay(r.s)}
	from, to := in.From, in.To
	for _, it := range in.Items {
		if in.From.IsZero() && (from.IsZero() || it.First.Before(from)) {
			from = it.First
		}
		if in.To.IsZero() && (to.IsZero() || it.Last.After(to)) {
			to = it.Last
		}
	}
	if from.IsZero() {
		from = calendar.New(2000, 1, 1)
	}
	if to.IsZero() || to.Before(from) {
		to = from
	}
	r.base = from
	r.from, r.to = 0, r.day(to)
	r.obst = workObstacles(w, in.Fixed, r.day)
	n := w.cols * w.rows
	r.blk = make([]bool, n)
	r.t = w.newSAT()
	margin := g.Area.Margin
	r.items = make([]rItem, len(in.Items))
	for i := range in.Items {
		src := &in.Items[i]
		it := &r.items[i]
		it.src = src
		it.l, it.b = src.LOA+margin, src.Beam+margin
		it.dw, it.dh = int((it.l+w.wc-1)/w.wc), int((it.b+w.wc-1)/w.wc)
		it.first, it.last = r.day(src.First), r.day(src.Last)
		arr, exit := src.ArrivalOn, src.ExitOn
		if arr.IsZero() {
			arr = src.First
		}
		if exit.IsZero() {
			exit = src.Last.AddDays(1)
		}
		it.arr, it.exit = r.day(arr), r.day(exit)
		it.area = int64(it.l) * int64(it.b)
		it.pinned = src.Pinned
		it.fixed = fixedIDs[src.ID]
		res, ok := itemFits(in, src)
		it.fitOK = ok
		if !ok {
			it.fitCodes = res.BlockingCodeList()
		}
		if src.Current != nil && src.LOA > 0 && src.Beam > 0 {
			if _, err := layout.Footprint(layout.Vessel{LOA: src.LOA, Beam: src.Beam}, *src.Current, g.Area); err == nil {
				if b, ok := w.boxOf(it.l, it.b, *src.Current); ok && w.boxUsable(b) {
					it.cur = &b
				}
			}
		}
		if it.pinned && (it.cur == nil || !it.fitOK) {
			return nil, fmt.Errorf("%w: %s", ErrPinnedInvalid, src.ID)
		}
	}
	// Pinned items must not overlap a fixed obstacle or each other.
	for i := range r.items {
		a := &r.items[i]
		if !a.pinned {
			continue
		}
		for _, o := range r.obst {
			if o.first <= a.last && o.last >= a.first && boxHits(w, *a.cur, o.cells) {
				return nil, fmt.Errorf("%w: %s", ErrPinnedInvalid, a.src.ID)
			}
		}
		for j := i + 1; j < len(r.items); j++ {
			b := &r.items[j]
			if b.pinned && overlapsNights(a, b) && a.cur.overlaps(*b.cur) {
				return nil, fmt.Errorf("%w: %s", ErrPinnedInvalid, b.src.ID)
			}
		}
	}
	// Free cells of the start night without boats.
	r.fillBlocked(nil, -1, r.from, r.from, false)
	for _, b := range r.blk {
		if !b {
			r.freeArea++
		}
	}
	r.freeArea *= int64(w.wc) * int64(w.wc)
	r.titems = make([]tItem, len(r.items))
	return r, nil
}

// itemFits reports whether an item may stand on the area: fit passes, or it fails only overridably and the item
// carries a fit override.
func itemFits(in Input, src *Item) (fit.Result, bool) {
	res := checkItemFit(in, src)
	return res, res.OK || (src.FitOverridden && res.Overridable)
}

// checkItemFit runs fit.Check of an item against the area target (support and weight included, m9-plan §3.3).
// The weight limit is max_weight_kg per cell × the hull cells (LOA × beam without margin, cell-aligned).
func checkItemFit(in Input, src *Item) fit.Result {
	v := src.Fit
	if v.LOA == 0 && v.Beam == 0 {
		v.LOA, v.Beam = src.LOA, src.Beam
	}
	v.Support, v.DisplacementKg = src.Support, src.DisplacementKg
	t := in.Fit.Target
	t.Kind = fit.TargetSurfaceArea
	if in.Fit.MaxWeightPerCellKg != nil && in.Grid.Area.Cell > 0 {
		c := in.Grid.Area.Cell
		hull := int64((src.LOA+c-1)/c) * int64((src.Beam+c-1)/c)
		mw := *in.Fit.MaxWeightPerCellKg * hull
		t.MaxWeightKg = &mw
	}
	return fit.Check(v, t, src.Last, in.Fit.Rules)
}

func (w *wgrid) boxUsable(b box) bool {
	for r := b.r; r < b.r+b.h; r++ {
		for c := b.c; c < b.c+b.w; c++ {
			if !w.usable[r*w.cols+c] {
				return false
			}
		}
	}
	return true
}

func boxHits(w *wgrid, b box, cells []int) bool {
	for _, i := range cells {
		c, r := i%w.cols, i/w.cols
		if c >= b.c && c < b.c+b.w && r >= b.r && r < b.r+b.h {
			return true
		}
	}
	return false
}

func overlapsNights(a, b *rItem) bool { return a.first <= b.last && b.first <= a.last }

// fillBlocked marks r.blk for nights [first, last]: unusable cells, obstacles and the boxes of the placed items
// of st overlapping those nights (except skip). With presentOnly, items count only when present on the date first
// (arrived before, exiting on or after it).
func (r *run) fillBlocked(st []pos, skip, first, last int, presentOnly bool) {
	for i, u := range r.w.usable {
		r.blk[i] = !u
	}
	for _, o := range r.obst {
		lo, hi := o.first, o.last
		if presentOnly {
			hi++ // an obstacle covering the night before the date is still there in the morning
		}
		if lo <= last && hi >= first {
			for _, c := range o.cells {
				r.blk[c] = true
			}
		}
	}
	for j := range st {
		if j == skip || !st[j].set {
			continue
		}
		it := &r.items[j]
		if presentOnly {
			if it.arr >= first || it.exit < first {
				continue
			}
		} else if it.first > last || it.last < first {
			continue
		}
		b := st[j].b
		cols := r.w.cols
		for rr := b.r; rr < b.r+b.h; rr++ {
			for c := b.c; c < b.c+b.w; c++ {
				r.blk[rr*cols+c] = true
			}
		}
	}
}

func (r *run) table() *sat {
	r.t.build(r.blk, r.w.cols, r.w.rows)
	return r.t
}

// gateDist is the cached gate distance of the box of item i at state (o, c, r).
func (r *run) gateDist(i, o, c, rr int) int32 {
	it := &r.items[i]
	if it.dist[o] == nil {
		it.dist[o] = make([]int32, r.w.cols*r.w.rows)
		for k := range it.dist[o] {
			it.dist[o][k] = -2
		}
	}
	k := rr*r.w.cols + c
	if d := it.dist[o][k]; d != -2 {
		return d
	}
	w, h := dims(o, it.dw, it.dh)
	d := r.w.boxDist(box{c: c, r: rr, w: w, h: h})
	it.dist[o][k] = d
	return d
}

// cand is a candidate top-left state of an item; touch counts the cells around the box that are blocked or
// outside (the contact variant of the construction prefers snug positions).
type cand struct {
	b     box
	dist  int32
	touch int32
}

// Construction preferences.
const (
	prefDeep    = iota // deepest first (largest gate distance)
	prefContact        // deepest, then most contact with walls and boats
)

// contact counts the cells along the outside of box b that are blocked in r.blk or off the grid.
func (r *run) contact(b box) int32 {
	var n int32
	cols, rows := r.w.cols, r.w.rows
	at := func(c, rr int) {
		if c < 0 || rr < 0 || c >= cols || rr >= rows || r.blk[rr*cols+c] {
			n++
		}
	}
	for c := b.c; c < b.c+b.w; c++ {
		at(c, b.r-1)
		at(c, b.r+b.h)
	}
	for rr := b.r; rr < b.r+b.h; rr++ {
		at(b.c-1, rr)
		at(b.c+b.w, rr)
	}
	return n
}

// prefer orders candidates by a construction preference.
func prefer(pref int) func(a, b cand) int {
	if pref == prefContact {
		return func(a, b cand) int {
			return cmp.Or(cmp.Compare(b.dist, a.dist), cmp.Compare(b.touch, a.touch), deeper(a, b))
		}
	}
	return deeper
}

// candidates lists the free states of item i in t (both orientations); filter (optional) keeps a state.
func (r *run) candidates(i int, t *sat, filter func(o, c, rr int) bool) []cand {
	it := &r.items[i]
	var out []cand
	for o := range 2 {
		w, h := dims(o, it.dw, it.dh)
		if o == 1 && w == h {
			break
		}
		for rr := 0; rr+h <= r.w.rows; rr++ {
			for c := 0; c+w <= r.w.cols; c++ {
				if !t.free(c, rr, w, h) || (filter != nil && !filter(o, c, rr)) {
					continue
				}
				out = append(out, cand{b: box{c: c, r: rr, w: w, h: h, o: o}, dist: r.gateDist(i, o, c, rr)})
			}
		}
	}
	return out
}

// deeper orders candidates deepest first (largest gate distance), then by row, column and orientation.
func deeper(a, b cand) int {
	return cmp.Or(cmp.Compare(b.dist, a.dist), cmp.Compare(a.b.r, b.b.r), cmp.Compare(a.b.c, b.b.c), cmp.Compare(a.b.o, b.b.o))
}

// exitRelevant reports whether the exit of item i is an event of the window.
func (r *run) exitRelevant(i int) bool {
	it := &r.items[i]
	return it.arr < it.exit && it.exit >= r.from && it.exit <= r.to+1
}

// placeReverse places item i on st (the boats already in st leave after it or are fixed): the deepest position free
// for its whole stay from which it can leave on its exit date with the boats present that day. Without such a
// position, the deepest free one (its exit will be blocked); false when none is free.
func (r *run) placeReverse(st []pos, i, pref int) bool {
	it := &r.items[i]
	var stamp int32
	if r.exitRelevant(i) {
		r.fillBlocked(st, i, it.exit, it.exit, true)
		stamp = r.s.reach(r.table(), it.dw, it.dh)
	}
	r.fillBlocked(st, i, it.first, it.last, false)
	t := r.table()
	less := prefer(pref)
	var best, fallback *cand
	for _, cd := range r.candidates(i, t, nil) {
		if pref == prefContact {
			cd.touch = r.contact(cd.b)
		}
		if stamp != 0 && !r.s.reached(stamp, cd.b.o, cd.b.c, cd.b.r) {
			if fallback == nil || less(cd, *fallback) < 0 {
				c := cd
				fallback = &c
			}
			continue
		}
		if best == nil || less(cd, *best) < 0 {
			c := cd
			best = &c
		}
	}
	if best == nil {
		best = fallback
	}
	if best == nil {
		return false
	}
	st[i] = r.posOf(i, best.b)
	return true
}

// posOf returns the position of item i at box b (keep when b is its current box).
func (r *run) posOf(i int, b box) pos {
	it := &r.items[i]
	if it.cur != nil && *it.cur == b {
		return pos{set: true, b: b, keep: true}
	}
	return pos{set: true, b: b}
}

// reverseOrder is the construction order: latest exit first, then larger, then id.
func (r *run) reverseOrder(idx []int) {
	slices.SortFunc(idx, func(a, b int) int {
		x, y := &r.items[a], &r.items[b]
		return cmp.Or(cmp.Compare(y.exit, x.exit), cmp.Compare(y.area, x.area), cmp.Compare(x.src.ID, y.src.ID))
	})
}

// construct builds a layout: pinned (and fixed) items at their current position, then, when keepCurrent, every
// item whose current position conflicts with nothing kept, then the rest in reverse exit order.
func (r *run) construct(keepCurrent bool, pref int) []pos {
	st := make([]pos, len(r.items))
	for i := range r.items {
		if r.items[i].pinned || r.items[i].fixed {
			st[i] = pos{set: true, b: *r.items[i].cur, keep: true}
		}
	}
	var rest []int
	if keepCurrent {
		order := make([]int, 0, len(r.items))
		for i := range r.items {
			if !st[i].set {
				order = append(order, i)
			}
		}
		r.reverseOrder(order)
		for _, i := range order {
			it := &r.items[i]
			if it.cur == nil || !it.fitOK || !r.freeFor(st, i, *it.cur) {
				rest = append(rest, i)
				continue
			}
			st[i] = pos{set: true, b: *it.cur, keep: true}
		}
	} else {
		for i := range r.items {
			if !st[i].set {
				rest = append(rest, i)
			}
		}
	}
	r.reverseOrder(rest)
	for _, i := range rest {
		if r.items[i].fitOK {
			r.placeReverse(st, i, pref)
		}
	}
	return st
}

// freeFor reports whether box b is free for item i's stay in st.
func (r *run) freeFor(st []pos, i int, b box) bool {
	r.fillBlocked(st, i, r.items[i].first, r.items[i].last, false)
	return r.table().free(b.c, b.r, b.w, b.h)
}

// score evaluates a layout (m9-plan §6.2).
func (r *run) score(st []pos) Score {
	var s Score
	for i := range r.items {
		it := &r.items[i]
		p := st[i]
		r.titems[i] = tItem{id: it.src.ID, b: p.b, placed: p.set, arr: it.arr, exit: it.exit}
		switch {
		case !p.set:
			s.Unplaced++
		case it.cur != nil && !p.keep && !sameRect(p.b, *it.cur):
			s.Moves++
		}
	}
	r.tl.run(r.titems, r.obst, r.from, r.to, false, func(ev string, _ int, _ int, a Access) {
		if a.Accessible {
			return
		}
		if ev == EventExit {
			s.ExitsBlocked++
		} else {
			s.ArrivalsBlocked++
		}
	})
	s.NegFreeRectCm2 = -r.freeRect(st)
	return s
}

func sameRect(a, b box) bool { return a.c == b.c && a.r == b.r && a.w == b.w && a.h == b.h }

// freeRect is the largest axis-aligned rectangle of free usable cells on the start night, in cm².
func (r *run) freeRect(st []pos) int64 {
	r.fillBlocked(st, -1, r.from, r.from, false)
	return int64(largestRect(r.blk, r.w.cols, r.w.rows)) * int64(r.w.wc) * int64(r.w.wc)
}

// movable lists the items the local search may change.
func (r *run) movable(st []pos, placed bool) []int {
	var out []int
	for i := range r.items {
		it := &r.items[i]
		if it.pinned || it.fixed || !it.fitOK || st[i].set != placed {
			continue
		}
		out = append(out, i)
	}
	return out
}

// neighbour returns a modified copy of st, or nil when the chosen move does not apply.
func (r *run) neighbour(st []pos) []pos {
	switch r.rnd.intn(4) {
	case 0:
		return r.relocate(st)
	case 1:
		return r.swap(st)
	case 2:
		return r.insert(st)
	default:
		return r.restore(st)
	}
}

func (r *run) relocate(st []pos) []pos {
	m := r.movable(st, true)
	if len(m) == 0 {
		return r.insert(st)
	}
	i := m[r.rnd.intn(len(m))]
	r.fillBlocked(st, i, r.items[i].first, r.items[i].last, false)
	cs := r.candidates(i, r.table(), nil)
	cs = slices.DeleteFunc(cs, func(c cand) bool { return c.b == st[i].b })
	if len(cs) == 0 {
		return nil
	}
	var pick cand
	if r.rnd.intn(2) == 0 {
		slices.SortFunc(cs, deeper)
		pick = cs[r.rnd.intn(min(topCandidates, len(cs)))]
	} else {
		pick = cs[r.rnd.intn(len(cs))]
	}
	out := slices.Clone(st)
	out[i] = r.posOf(i, pick.b)
	return out
}

func (r *run) swap(st []pos) []pos {
	m := r.movable(st, true)
	if len(m) < 2 {
		return nil
	}
	i := m[r.rnd.intn(len(m))]
	j := m[r.rnd.intn(len(m))]
	if i == j || !overlapsNights(&r.items[i], &r.items[j]) {
		return nil
	}
	out := slices.Clone(st)
	bi, bj := st[i].b, st[j].b
	out[i].set, out[j].set = false, false
	for _, oi := range []int{bj.o, 1 - bj.o} {
		wi, hi := dims(oi, r.items[i].dw, r.items[i].dh)
		ni := box{c: bj.c, r: bj.r, w: wi, h: hi, o: oi}
		if ni.c+ni.w > r.w.cols || ni.r+ni.h > r.w.rows || !r.freeFor(out, i, ni) {
			continue
		}
		out[i] = r.posOf(i, ni)
		for _, oj := range []int{bi.o, 1 - bi.o} {
			wj, hj := dims(oj, r.items[j].dw, r.items[j].dh)
			nj := box{c: bi.c, r: bi.r, w: wj, h: hj, o: oj}
			if nj.c+nj.w > r.w.cols || nj.r+nj.h > r.w.rows || !r.freeFor(out, j, nj) {
				continue
			}
			out[j] = r.posOf(j, nj)
			return out
		}
		out[i].set = false
	}
	return nil
}

// insert places an unplaced item at a random position, evicting at most one movable item.
func (r *run) insert(st []pos) []pos {
	u := r.movable(st, false)
	if len(u) == 0 {
		return nil
	}
	i := u[r.rnd.intn(len(u))]
	it := &r.items[i]
	// Only fixed things (unusable, obstacles, pinned and fixed items) block; boats are counted.
	keep := make([]pos, len(st))
	for j := range st {
		if st[j].set && (r.items[j].pinned || r.items[j].fixed) {
			keep[j] = st[j]
		}
	}
	r.fillBlocked(keep, i, it.first, it.last, false)
	cs := r.candidates(i, r.table(), nil)
	if len(cs) == 0 {
		return nil
	}
	for range 32 {
		cd := cs[r.rnd.intn(len(cs))]
		evict := -1
		ok := true
		for j := range st {
			if j == i || !st[j].set || keep[j].set || !overlapsNights(it, &r.items[j]) || !st[j].b.overlaps(cd.b) {
				continue
			}
			if evict >= 0 {
				ok = false
				break
			}
			evict = j
		}
		if !ok {
			continue
		}
		out := slices.Clone(st)
		if evict >= 0 {
			out[evict] = pos{}
		}
		out[i] = r.posOf(i, cd.b)
		return out
	}
	return nil
}

func (r *run) restore(st []pos) []pos {
	var m []int
	for i := range r.items {
		it := &r.items[i]
		if it.cur != nil && !it.pinned && !it.fixed && it.fitOK && !st[i].keep {
			m = append(m, i)
		}
	}
	if len(m) == 0 {
		return r.relocate(st)
	}
	i := m[r.rnd.intn(len(m))]
	out := slices.Clone(st)
	out[i] = pos{}
	if !r.freeFor(out, i, *r.items[i].cur) {
		return nil
	}
	out[i] = pos{set: true, b: *r.items[i].cur, keep: true}
	return out
}

// optimal reports the stop condition of m9-plan §6.3.
func (r *run) optimal(s Score) bool {
	return s.Unplaced == 0 && s.ExitsBlocked == 0 && s.ArrivalsBlocked == 0 && s.Moves == 0 && -s.NegFreeRectCm2 == r.freeArea
}

// placement converts a position to the output placement.
func (r *run) placement(i int, p pos) *Placement {
	if !p.set {
		return nil
	}
	if p.keep && r.items[i].src.Current != nil {
		c := *r.items[i].src.Current
		return &c
	}
	return &Placement{X: geo.Cm(p.b.c) * r.w.wc, Y: geo.Cm(p.b.r) * r.w.wc, Rotation: geo.Angle(p.b.o * 900)}
}

// result builds the Result of a layout, with the accessibility timeline.
func (r *run) result(st []pos, s Score) Result {
	res := Result{Score: s, Placements: make([]ItemResult, len(r.items))}
	for i := range r.items {
		it := &r.items[i]
		ir := ItemResult{ID: it.src.ID, At: r.placement(i, st[i]), FitCodes: []string{}}
		if ir.At == nil {
			if it.fitOK {
				ir.UnplacedReason = UnplacedNoSpace
			} else {
				ir.UnplacedReason, ir.FitCodes = UnplacedFit, it.fitCodes
			}
		}
		res.Placements[i] = ir
	}
	for i := range r.items {
		r.titems[i] = tItem{id: r.items[i].src.ID, b: st[i].b, placed: st[i].set, arr: r.items[i].arr, exit: r.items[i].exit}
	}
	res.Access = []Access{}
	r.tl.run(r.titems, r.obst, r.from, r.to, true, func(ev string, dd int, ix int, a Access) {
		a.ID, a.Event, a.On = r.titems[ix].id, ev, r.base.AddDays(dd)
		res.Access = append(res.Access, a)
	})
	sortAccess(res.Access)
	return res
}

// Arrange runs the optimizer; ctx carries the caller deadline (m9-plan §6.3). Two constructions (keep current;
// reverse exit order from the deepest reachable position) are improved by late-acceptance hill climbing; the best
// layout is checked with Validate. With an iteration budget the deadline does not cut, the result depends only on
// (Input, Seed).
func Arrange(ctx context.Context, in Input) (Result, error) {
	r, err := prepare(in, nil)
	if err != nil {
		return Result{}, err
	}
	// Constructions: keep current then reverse exit order, and reverse exit order alone, each with the deep and
	// the contact preference; the best score starts the local search.
	var cur []pos
	var curScore Score
	for _, keep := range []bool{true, false} {
		for _, pref := range []int{prefDeep, prefContact} {
			st := r.construct(keep, pref)
			if s := r.score(st); cur == nil || s.Less(curScore) {
				cur, curScore = st, s
			}
		}
	}
	best, bestScore := cur, curScore
	maxIter := in.MaxIter
	if maxIter <= 0 {
		maxIter = DefaultMaxIter
	}
	hist := make([]Score, LateAcceptanceLength)
	for k := range hist {
		hist[k] = curScore
	}
	stopped := StoppedByIterations
	iter, lastGain := 0, 0
	for ; iter < maxIter; iter++ {
		if r.optimal(bestScore) {
			stopped = StoppedByOptimal
			break
		}
		if iter%checkEvery == 0 && ctx.Err() != nil {
			stopped = StoppedByDeadline
			break
		}
		if iter-lastGain > stallIterations {
			break
		}
		next := r.neighbour(cur)
		if next == nil {
			continue
		}
		s := r.score(next)
		v := iter % LateAcceptanceLength
		if !curScore.Less(s) || !hist[v].Less(s) {
			cur, curScore = next, s
			if s.Less(bestScore) {
				best, bestScore, lastGain = next, s, iter
			}
		}
		hist[v] = curScore
	}
	res := r.result(best, bestScore)
	res.Iterations, res.StoppedBy = iter, stopped
	if err := Validate(r.in, res); err != nil {
		return res, err
	}
	return res, nil
}

// PlaceOne returns the construction-step position of item with every other item of in fixed at its current
// position (m9-plan §3.2): among the free positions for its stay, it prefers one from which it can leave on its
// exit date and that blocks no exit or arrival of the others, deepest first. Nil when no position is free or the
// item fails fit.
func PlaceOne(in Input, item Item) (*Placement, error) {
	fixed := map[string]bool{}
	items := make([]Item, 0, len(in.Items)+1)
	for _, it := range in.Items {
		if it.ID == item.ID || it.Current == nil {
			continue
		}
		it.Pinned = false
		fixed[it.ID] = true
		items = append(items, it)
	}
	item.Pinned, item.Current = false, nil
	items = append(items, item)
	in.Items = items
	r, err := prepare(in, fixed)
	if err != nil {
		return nil, err
	}
	i := len(items) - 1
	if !r.items[i].fitOK {
		return nil, nil
	}
	st := make([]pos, len(items))
	for k := range r.items {
		if r.items[k].fixed && r.items[k].cur != nil {
			st[k] = pos{set: true, b: *r.items[k].cur, keep: true}
		}
	}
	base := r.score(st)
	var stamp int32
	it := &r.items[i]
	if r.exitRelevant(i) {
		r.fillBlocked(st, i, it.exit, it.exit, true)
		stamp = r.s.reach(r.table(), it.dw, it.dh)
	}
	r.fillBlocked(st, i, it.first, it.last, false)
	cs := r.candidates(i, r.table(), nil)
	if len(cs) == 0 {
		return nil, nil
	}
	reached := func(c cand) bool { return stamp == 0 || r.s.reached(stamp, c.b.o, c.b.c, c.b.r) }
	slices.SortStableFunc(cs, func(a, b cand) int {
		ra, rb := reached(a), reached(b)
		if ra != rb {
			if ra {
				return -1
			}
			return 1
		}
		return deeper(a, b)
	})
	var bestPos *box
	var bestScore Score
	for k, cd := range cs {
		if k >= placeOneTries {
			break
		}
		st[i] = pos{set: true, b: cd.b}
		s := r.score(st)
		s.NegFreeRectCm2, s.Moves = 0, 0 // only the accessibility of the others matters here
		if bestPos == nil || s.Less(bestScore) {
			b := cd.b
			bestPos, bestScore = &b, s
		}
		if s.ExitsBlocked <= base.ExitsBlocked && s.ArrivalsBlocked <= base.ArrivalsBlocked {
			break
		}
	}
	st[i] = pos{set: true, b: *bestPos}
	return r.placement(i, st[i]), nil
}
