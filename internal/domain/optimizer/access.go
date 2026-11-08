package optimizer

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
)

// searcher runs the moving-rectangle searches of a working grid (m9-plan §5.2–5.4) with reusable buffers.
type searcher struct {
	w       *wgrid
	visited []int32 // stamp per state (o·N + r·cols + c)
	stamp   int32
	queue   []int32
}

func newSearcher(w *wgrid) *searcher {
	n := w.cols * w.rows
	return &searcher{w: w, visited: make([]int32, 2*n), queue: make([]int32, 0, 2*n)}
}

func (s *searcher) next() int32 {
	s.stamp++
	if s.stamp == 1<<30 {
		clear(s.visited)
		s.stamp = 1
	}
	return s.stamp
}

// dims returns the box size of orientation o for a footprint whose orientation-0 size is (w0, h0).
func dims(o, w0, h0 int) (int, int) {
	if o == 0 {
		return w0, h0
	}
	return h0, w0
}

// side reports whether every cell of a box side lies in the same gate and opens outside in direction dir.
func (s *searcher) side(dir, c, r, w, h int) bool {
	g := s.w
	var i0, step, n int
	switch dir {
	case dirLeft:
		i0, step, n = r*g.cols+c, g.cols, h
	case dirRight:
		i0, step, n = r*g.cols+c+w-1, g.cols, h
	case dirUp:
		i0, step, n = r*g.cols+c, 1, w
	default:
		i0, step, n = (r+h-1)*g.cols+c, 1, w
	}
	gate := g.gate[i0]
	if gate < 0 {
		return false
	}
	open := g.open[dir]
	for k, i := 0, i0; k < n; k, i = k+1, i+step {
		if g.gate[i] != gate || !open[i] {
			return false
		}
	}
	return true
}

// goal reports whether a box can leave: one whole side on the border inside one gate (m9-plan §5.3).
func (s *searcher) goal(c, r, w, h int) bool {
	return s.side(dirLeft, c, r, w, h) || s.side(dirRight, c, r, w, h) || s.side(dirUp, c, r, w, h) ||
		s.side(dirDown, c, r, w, h)
}

// expand visits the neighbours of state st (N, E, S, W, rotate) that are free in t; visit returns true to stop.
func (s *searcher) expand(t *sat, st int32, w0, h0 int, stamp int32, visit func(o, c, r, w, h int) bool) bool {
	g := s.w
	n := int32(g.cols * g.rows) //nolint:gosec // grids are at most MaxSurfaceCells
	o := int(st / n)
	rem := int(st % n)
	r, c := rem/g.cols, rem%g.cols
	w, h := dims(o, w0, h0)
	try := func(o2, c2, r2, w2, h2 int) bool {
		id := int32(o2)*n + int32(r2*g.cols+c2) //nolint:gosec // bounded by 2N
		if s.visited[id] == stamp {
			return false
		}
		s.visited[id] = stamp
		s.queue = append(s.queue, id)
		return visit(o2, c2, r2, w2, h2)
	}
	if r > 0 && t.free(c, r-1, w, 1) && try(o, c, r-1, w, h) {
		return true
	}
	if c+w < g.cols && t.free(c+w, r, 1, h) && try(o, c+1, r, w, h) {
		return true
	}
	if r+h < g.rows && t.free(c, r+h, w, 1) && try(o, c, r+1, w, h) {
		return true
	}
	if c > 0 && t.free(c-1, r, 1, h) && try(o, c-1, r, w, h) {
		return true
	}
	if w != h {
		m := max(w, h)
		if c+m <= g.cols && r+m <= g.rows && t.free(c, r, m, m) && try(1-o, c, r, h, w) {
			return true
		}
	}
	return false
}

// accessible reports whether box b can reach a goal state through the free cells of t (b's own cells must be
// free in t).
func (s *searcher) accessible(t *sat, b box) bool {
	if s.goal(b.c, b.r, b.w, b.h) {
		return true
	}
	w0, h0 := dims(b.o, b.w, b.h) // orientation-0 size of this box
	stamp := s.next()
	n := int32(s.w.cols * s.w.rows)                 //nolint:gosec // bounded
	start := int32(b.o)*n + int32(b.r*s.w.cols+b.c) //nolint:gosec // bounded
	s.queue = s.queue[:0]
	s.visited[start] = stamp
	s.queue = append(s.queue, start)
	found := false
	visit := func(_, c, r, w, h int) bool {
		if s.goal(c, r, w, h) {
			found = true
		}
		return found
	}
	for q := 0; q < len(s.queue) && !found; q++ {
		s.expand(t, s.queue[q], w0, h0, stamp, visit)
	}
	return found
}

// reach marks every state of a (w0 × h0) box from which a goal is reachable through the free cells of t (moves
// are reversible, so this is a multi-source search from the free goal states). It returns the stamp of the
// reachable states in s.visited.
func (s *searcher) reach(t *sat, w0, h0 int) int32 {
	g := s.w
	stamp := s.next()
	s.queue = s.queue[:0]
	n := g.cols * g.rows
	for o := range 2 {
		w, h := dims(o, w0, h0)
		if o == 1 && w == h {
			break
		}
		for r := 0; r+h <= g.rows; r++ {
			for c := 0; c+w <= g.cols; c++ {
				if !t.free(c, r, w, h) || !s.goal(c, r, w, h) {
					continue
				}
				id := int32(o*n + r*g.cols + c) //nolint:gosec // bounded
				s.visited[id] = stamp
				s.queue = append(s.queue, id)
			}
		}
	}
	none := func(_, _, _, _, _ int) bool { return false }
	for q := 0; q < len(s.queue); q++ {
		s.expand(t, s.queue[q], w0, h0, stamp, none)
	}
	return stamp
}

// reached reports whether state (o, c, r) was marked by reach with stamp.
func (s *searcher) reached(stamp int32, o, c, r int) bool {
	return s.visited[o*s.w.cols*s.w.rows+r*s.w.cols+c] == stamp
}

// day is the occupation of one date: base blocked cells (unusable and fixed obstacles) plus a count of present
// boxes per cell.
type day struct {
	s    *searcher
	base []bool
	occ  []int16
	tmp  []bool
	t    *sat
}

func newDay(s *searcher) *day {
	n := s.w.cols * s.w.rows
	return &day{s: s, base: make([]bool, n), occ: make([]int16, n), tmp: make([]bool, n), t: s.w.newSAT()}
}

// reset clears the day to the unusable cells only.
func (d *day) reset() {
	for i, u := range d.s.w.usable {
		d.base[i] = !u
	}
	clear(d.occ)
}

func (d *day) block(cells []int) {
	for _, c := range cells {
		d.base[c] = true
	}
}

func (d *day) add(b box, delta int16) {
	cols := d.s.w.cols
	for r := b.r; r < b.r+b.h; r++ {
		for c := b.c; c < b.c+b.w; c++ {
			d.occ[r*cols+c] += delta
		}
	}
}

// table builds the SAT of the current occupation.
func (d *day) table() *sat {
	for i := range d.tmp {
		d.tmp[i] = d.base[i] || d.occ[i] > 0
	}
	d.t.build(d.tmp, d.s.w.cols, d.s.w.rows)
	return d.t
}

// accessible reports whether present box b can leave, the others staying.
func (d *day) accessible(b box) bool {
	d.add(b, -1)
	ok := d.s.accessible(d.table(), b)
	d.add(b, 1)
	return ok
}

// pbox is a boat on a day.
type pbox struct {
	id  string
	b   box
	gap int32 // gate distance of the box
	ix  int   // index of the item in the caller's list
}

// extract runs the greedy extraction among the candidates (indexes into present; all are in the day): the
// accessible one nearest to a gate leaves first (ties by id), until none is accessible. It returns the extraction
// order and the candidates left (blocked); the extracted boxes are removed from the day.
func (d *day) extract(present []pbox, cand []int) (order, left []int) {
	left = slices.Clone(cand)
	for len(left) > 0 {
		best := -1
		for k, i := range left {
			if best >= 0 {
				pb, pi := present[left[best]], present[i]
				if c := cmp.Or(cmp.Compare(pi.gap, pb.gap), cmp.Compare(pi.id, pb.id)); c >= 0 {
					continue
				}
			}
			if d.accessible(present[i].b) {
				best = k
			}
		}
		if best < 0 {
			break
		}
		i := left[best]
		d.add(present[i].b, -1)
		order = append(order, i)
		left = slices.Delete(left, best, best+1)
	}
	return order, left
}

// blocking returns the blocking boats of present[i] (m9-plan §5.6): remove the other boats still in the day in
// gate-distance order until i is accessible, then put each removed boat back in reverse order and keep it when i
// stays accessible. Nil when removing every boat does not free it (the area itself blocks it).
func (d *day) blocking(present []pbox, in []bool, i int) []string {
	others := make([]int, 0, len(present))
	for j := range present {
		if j != i && in[j] {
			others = append(others, j)
		}
	}
	slices.SortFunc(others, func(a, b int) int {
		return cmp.Or(cmp.Compare(present[a].gap, present[b].gap), cmp.Compare(present[a].id, present[b].id))
	})
	var removed []int
	ok := false
	for _, j := range others {
		d.add(present[j].b, -1)
		removed = append(removed, j)
		if d.accessible(present[i].b) {
			ok = true
			break
		}
	}
	if !ok {
		for _, j := range removed {
			d.add(present[j].b, 1)
		}
		return nil
	}
	var keep []int
	for k := len(removed) - 1; k >= 0; k-- {
		j := removed[k]
		d.add(present[j].b, 1)
		if d.accessible(present[i].b) {
			continue // not blocking: stays back
		}
		d.add(present[j].b, -1)
		keep = append(keep, j)
	}
	for _, j := range keep {
		d.add(present[j].b, 1)
	}
	ids := make([]string, len(keep))
	for k, j := range keep {
		ids[k] = present[j].id
	}
	slices.Sort(ids)
	return ids
}

// Accessibility evaluates the exits of date: present are the boats on the area that night, exiting the ids leaving
// that day (m9-plan §5). One Access per exiting id that is present, in the order of exiting.
func Accessibility(g Grid, present []Placed, exiting []string, date calendar.Date) []Access {
	w := newWork(g)
	d := newDay(newSearcher(w))
	d.reset()
	pb := make([]pbox, 0, len(present))
	idx := map[string]int{}
	for _, p := range present {
		b, ok := w.boxOf(p.LOA+g.Area.Margin, p.Beam+g.Area.Margin, p.At)
		if !ok {
			continue
		}
		idx[p.ID] = len(pb)
		pb = append(pb, pbox{id: p.ID, b: b, gap: w.boxDist(b)})
		d.add(b, 1)
	}
	var cand []int
	for _, id := range exiting {
		if i, ok := idx[id]; ok && !slices.Contains(cand, i) {
			cand = append(cand, i)
		}
	}
	res := evalDay(d, pb, cand, true)
	out := make([]Access, 0, len(cand))
	for _, i := range cand {
		a := res[i]
		a.ID, a.Event, a.On = pb[i].id, EventExit, date
		out = append(out, a)
	}
	return out
}

// evalDay extracts the candidates greedily and, when withBlocking, computes the blocking set of each blocked
// one. It returns the Access of every candidate by present index (ID, Event, On left to the caller).
func evalDay(d *day, present []pbox, cand []int, withBlocking bool) map[int]Access {
	order, left := d.extract(present, cand)
	res := make(map[int]Access, len(cand))
	for k, i := range order {
		res[i] = Access{Accessible: true, Order: k + 1, Blocking: []string{}}
	}
	if len(left) == 0 {
		return res
	}
	in := make([]bool, len(present))
	for i := range present {
		in[i] = true
	}
	for _, i := range order {
		in[i] = false
	}
	for _, i := range left {
		a := Access{Blocking: []string{}}
		if withBlocking {
			if b := d.blocking(present, in, i); b != nil {
				a.Blocking = b
			}
		}
		res[i] = a
	}
	return res
}

// tItem is a boat of a timeline evaluation, in day numbers.
type tItem struct {
	id        string
	b         box
	placed    bool
	arr, exit int // arrival and exit days
}

// tObst is a fixed obstacle in working cells and day numbers.
type tObst struct {
	cells       []int
	first, last int
}

// timeline evaluates every exit event of [from, to+1] and arrival event of [from, to] (m9-plan §5.5, §5.7).
// emit receives each event; withBlocking computes the blocking sets.
type timeline struct {
	d *day
}

// prepare loads the boats kept and the obstacles present on day date. Exits happen before arrivals (m9-plan D7):
// on an exit day only the obstacles covering night date−1 are present; on an arrival day also the ones starting
// that night.
func (tl *timeline) prepare(items []tItem, obst []tObst, date int, exit bool, keep func(it *tItem) bool) []pbox {
	d := tl.d
	d.reset()
	for _, o := range obst {
		present := o.first <= date && o.last >= date-1
		if exit {
			present = o.first <= date-1 && o.last >= date-1
		}
		if present {
			d.block(o.cells)
		}
	}
	var pb []pbox
	for k := range items {
		it := &items[k]
		if !it.placed || !keep(it) {
			continue
		}
		pb = append(pb, pbox{id: it.id, b: it.b, gap: tl.d.s.w.boxDist(it.b), ix: k})
		d.add(it.b, 1)
	}
	return pb
}

// eventDays returns the sorted distinct exit days in [from, to+1] and arrival days in [from, to] of placed items.
func eventDays(items []tItem, from, to int) (exits, arrivals []int) {
	for _, it := range items {
		if !it.placed || it.arr >= it.exit {
			continue
		}
		if it.exit >= from && it.exit <= to+1 {
			exits = append(exits, it.exit)
		}
		if it.arr >= from && it.arr <= to {
			arrivals = append(arrivals, it.arr)
		}
	}
	slices.Sort(exits)
	slices.Sort(arrivals)
	return slices.Compact(exits), slices.Compact(arrivals)
}

// run evaluates the events; emit gets (event, day, item index, access).
func (tl *timeline) run(items []tItem, obst []tObst, from, to int, withBlocking bool, emit func(event string, dayN int, ix int, a Access)) {
	exits, arrivals := eventDays(items, from, to)
	for _, dd := range exits {
		pb := tl.prepare(items, obst, dd, true, func(it *tItem) bool { return it.arr < dd && it.exit >= dd })
		var cand []int
		for i, p := range pb {
			if items[p.ix].exit == dd {
				cand = append(cand, i)
			}
		}
		for i, a := range evalDay(tl.d, pb, cand, withBlocking) {
			emit(EventExit, dd, pb[i].ix, a)
		}
	}
	for _, dd := range arrivals {
		pb := tl.prepare(items, obst, dd, false, func(it *tItem) bool { return it.arr <= dd && it.exit > dd })
		var cand []int
		for i, p := range pb {
			if items[p.ix].arr == dd {
				cand = append(cand, i)
			}
		}
		res := evalDay(tl.d, pb, cand, withBlocking)
		// The extraction order reversed is the arrival order.
		n := 0
		for _, a := range res {
			if a.Accessible {
				n++
			}
		}
		for i, a := range res {
			if a.Accessible {
				a.Order = n - a.Order + 1
			}
			emit(EventArrival, dd, pb[i].ix, a)
		}
	}
}

// AccessibilityTimeline evaluates every exit and arrival event of [from, to] (exits before arrivals on a day).
// Exit events are the exit dates in [from, to+1] (the morning after the last night included), arrival events the
// arrival dates in [from, to]. The result is sorted by date, exits first, then id.
func AccessibilityTimeline(g Grid, fixed []Obstacle, items []TimelineItem, from, to calendar.Date) []Access {
	w := newWork(g)
	base := from
	day := base.DaysUntil
	ti := make([]tItem, 0, len(items))
	for _, it := range items {
		b, ok := w.boxOf(it.LOA+g.Area.Margin, it.Beam+g.Area.Margin, it.At)
		arr := it.ArrivalOn
		if arr.IsZero() {
			arr = it.First
		}
		exit := it.ExitOn
		if exit.IsZero() {
			exit = it.Last.AddDays(1)
		}
		ti = append(ti, tItem{id: it.ID, b: b, placed: ok, arr: day(arr), exit: day(exit)})
	}
	obst := workObstacles(w, fixed, day)
	tl := &timeline{d: newDay(newSearcher(w))}
	var out []Access
	tl.run(ti, obst, 0, day(to), true, func(ev string, dd int, ix int, a Access) {
		a.ID, a.Event, a.On = ti[ix].id, ev, base.AddDays(dd)
		out = append(out, a)
	})
	sortAccess(out)
	return out
}

func sortAccess(out []Access) {
	slices.SortFunc(out, func(a, b Access) int {
		return cmp.Or(a.On.Compare(b.On), cmp.Compare(eventRank(a.Event), eventRank(b.Event)), cmp.Compare(a.ID, b.ID))
	})
}

func eventRank(e string) int {
	if e == EventExit {
		return 0
	}
	return 1
}

// workObstacles maps fixed obstacles to working cells and day numbers.
func workObstacles(w *wgrid, fixed []Obstacle, day func(calendar.Date) int) []tObst {
	out := make([]tObst, 0, len(fixed))
	for _, o := range fixed {
		cells := make([]int, 0, len(o.Cells))
		for _, fi := range o.Cells {
			if fi < 0 || fi >= w.area.Cols*w.area.Rows {
				continue
			}
			cells = append(cells, (fi/w.area.Cols/w.k)*w.cols+(fi%w.area.Cols)/w.k)
		}
		slices.Sort(cells)
		out = append(out, tObst{cells: slices.Compact(cells), first: day(o.First), last: day(o.Last)})
	}
	return out
}
