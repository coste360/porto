package optimizer

import (
	"slices"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// Gate issue reasons (GateIssue.Reason).
const (
	GateNotUsable   = "not_usable"
	GateNotOnBorder = "not_on_border"
)

// NewGrid builds the grid of an area: gates from the exit cells, the gate distance field and the coarsening factor.
func NewGrid(a layout.Area, exits []int) Grid {
	n := a.Cols * a.Rows
	ex := make([]int, 0, len(exits))
	for _, i := range exits {
		if i >= 0 && i < n {
			ex = append(ex, i)
		}
	}
	slices.Sort(ex)
	ex = slices.Compact(ex)
	g := Grid{Area: a, Exits: ex, Coarse: coarseFactor(a.Cols, a.Rows)}
	g.Gates = components(a.Cols, a.Rows, ex)
	usable := func(i int) bool { return i < len(a.Usable) && a.Usable[i] }
	g.Dist = distField(a.Cols, a.Rows, usable, ex)
	return g
}

// coarseFactor is the smallest k with ⌈cols/k⌉·⌈rows/k⌉ ≤ MaxCells.
func coarseFactor(cols, rows int) int {
	k := 1
	for ceilInt(cols, k)*ceilInt(rows, k) > MaxCells {
		k++
	}
	return k
}

func ceilInt(a, b int) int { return (a + b - 1) / b }

// components returns the 4-connected components of cells (sorted indexes), each sorted, ordered by smallest cell.
func components(cols, rows int, cells []int) [][]int {
	in := make(map[int]bool, len(cells))
	for _, c := range cells {
		in[c] = true
	}
	seen := make(map[int]bool, len(cells))
	var out [][]int
	for _, start := range cells {
		if seen[start] {
			continue
		}
		comp := []int{start}
		seen[start] = true
		for q := 0; q < len(comp); q++ {
			i := comp[q]
			c, r := i%cols, i/cols
			for _, nb := range [4][2]int{{c, r - 1}, {c + 1, r}, {c, r + 1}, {c - 1, r}} {
				if nb[0] < 0 || nb[0] >= cols || nb[1] < 0 || nb[1] >= rows {
					continue
				}
				j := nb[1]*cols + nb[0]
				if in[j] && !seen[j] {
					seen[j] = true
					comp = append(comp, j)
				}
			}
		}
		slices.Sort(comp)
		out = append(out, comp)
	}
	return out
}

// distField is the multi-source BFS distance (in cells, 4-connected) from the usable source cells over usable
// cells; -1 = unreachable.
func distField(cols, rows int, usable func(int) bool, sources []int) []int32 {
	n := cols * rows
	d := make([]int32, n)
	for i := range d {
		d[i] = -1
	}
	q := make([]int, 0, n)
	for _, s := range sources {
		if usable(s) && d[s] < 0 {
			d[s] = 0
			q = append(q, s)
		}
	}
	for h := 0; h < len(q); h++ {
		i := q[h]
		c, r := i%cols, i/cols
		for _, nb := range [4][2]int{{c, r - 1}, {c + 1, r}, {c, r + 1}, {c - 1, r}} {
			if nb[0] < 0 || nb[0] >= cols || nb[1] < 0 || nb[1] >= rows {
				continue
			}
			j := nb[1]*cols + nb[0]
			if d[j] < 0 && usable(j) {
				d[j] = d[i] + 1
				q = append(q, j)
			}
		}
	}
	return d
}

// outsideCell reports whether fine cell (c, r) is off the grid or outside the outline (not usable, not blocked).
func outsideCell(a layout.Area, c, r int) bool {
	if c < 0 || r < 0 || c >= a.Cols || r >= a.Rows {
		return true
	}
	i := r*a.Cols + c
	if i < len(a.Usable) && a.Usable[i] {
		return false
	}
	_, blocked := slices.BinarySearch(a.Blocked, i)
	return !blocked
}

// ValidateGates returns the gates that are not usable or not on the grid border (m9-plan §4): every cell of a gate
// must be usable and have a side on the grid edge or next to a cell outside the outline.
func ValidateGates(g Grid) []GateIssue {
	a := g.Area
	out := []GateIssue{}
	for gi, gate := range g.Gates {
		reason := ""
		for _, i := range gate {
			if i >= len(a.Usable) || !a.Usable[i] {
				reason = GateNotUsable
				break
			}
			c, r := i%a.Cols, i/a.Cols
			if !outsideCell(a, c-1, r) && !outsideCell(a, c+1, r) && !outsideCell(a, c, r-1) && !outsideCell(a, c, r+1) {
				reason = GateNotOnBorder
			}
		}
		if reason != "" {
			out = append(out, GateIssue{Gate: gi, Reason: reason})
		}
	}
	return out
}

// Directions of an open gate side.
const (
	dirLeft = iota
	dirRight
	dirUp
	dirDown
)

// wgrid is the working grid of a run: the fine grid or its coarsening by k (a coarse cell is usable only when all
// its fine cells are). Boxes, gates and distances are in working cells.
type wgrid struct {
	area       layout.Area
	k          int
	wc         geo.Cm // working cell size (cell · k)
	cols, rows int
	usable     []bool
	gate       []int32   // gate id of the cell, -1 none
	open       [4][]bool // gate cell whose neighbour in the direction is outside
	dist       []int32
}

func newWork(g Grid) *wgrid {
	a := g.Area
	k := max(g.Coarse, 1)
	w := &wgrid{area: a, k: k, wc: a.Cell * geo.Cm(k), cols: ceilInt(a.Cols, k), rows: ceilInt(a.Rows, k)}
	n := w.cols * w.rows
	w.usable = make([]bool, n)
	w.gate = make([]int32, n)
	for i := range w.gate {
		w.gate[i] = -1
	}
	for r := range w.rows {
		for c := range w.cols {
			ok := true
			for fr := r * k; fr < (r+1)*k && ok; fr++ {
				for fc := c * k; fc < (c+1)*k; fc++ {
					fi := fr*a.Cols + fc
					if fr >= a.Rows || fc >= a.Cols || fi >= len(a.Usable) || !a.Usable[fi] {
						ok = false
						break
					}
				}
			}
			w.usable[r*w.cols+c] = ok
		}
	}
	for gi, gate := range g.Gates {
		for _, fi := range gate {
			wi := (fi/a.Cols/k)*w.cols + (fi%a.Cols)/k
			if w.usable[wi] && w.gate[wi] < 0 {
				w.gate[wi] = int32(gi) //nolint:gosec // gate count is small
			}
		}
	}
	outside := func(c, r int) bool {
		if c < 0 || r < 0 || c >= w.cols || r >= w.rows {
			return true
		}
		// A working cell is outside when one of its fine cells is outside the outline.
		for fr := r * k; fr < (r+1)*k; fr++ {
			for fc := c * k; fc < (c+1)*k; fc++ {
				if outsideCell(a, fc, fr) {
					return true
				}
			}
		}
		return false
	}
	for d := range w.open {
		w.open[d] = make([]bool, n)
	}
	for i := range n {
		if w.gate[i] < 0 {
			continue
		}
		c, r := i%w.cols, i/w.cols
		w.open[dirLeft][i] = outside(c-1, r)
		w.open[dirRight][i] = outside(c+1, r)
		w.open[dirUp][i] = outside(c, r-1)
		w.open[dirDown][i] = outside(c, r+1)
	}
	if k == 1 && len(g.Dist) == n {
		w.dist = g.Dist
	} else {
		var src []int
		for i := range n {
			if w.gate[i] >= 0 {
				src = append(src, i)
			}
		}
		w.dist = distField(w.cols, w.rows, func(i int) bool { return w.usable[i] }, src)
	}
	return w
}

// box is a rectangle of working cells; o is the orientation (0 = length along the columns, 1 = rotated 90°).
type box struct{ c, r, w, h, o int }

func (b box) overlaps(o box) bool {
	return b.c < o.c+o.w && o.c < b.c+b.w && b.r < o.r+o.h && o.r < b.r+b.h
}

// boxOf is the working-cell box a placement of a footprint L × B (cm, margins included) touches, clipped to the
// grid; ok is false when the footprint leaves the grid.
func (w *wgrid) boxOf(l, b geo.Cm, p Placement) (box, bool) {
	o := 0
	fw, fh := l, b
	if p.Rotation == 900 || p.Rotation == 2700 {
		fw, fh, o = b, l, 1
	}
	if p.X < 0 || p.Y < 0 {
		return box{}, false
	}
	c0, r0 := int(p.X/w.wc), int(p.Y/w.wc)
	c1 := int((p.X+fw+w.wc-1)/w.wc) - 1
	r1 := int((p.Y+fh+w.wc-1)/w.wc) - 1
	if c1 >= w.cols || r1 >= w.rows {
		return box{}, false
	}
	return box{c: c0, r: r0, w: c1 - c0 + 1, h: r1 - r0 + 1, o: o}, true
}

// boxDist is the smallest gate distance of the cells of a box (a large value when none reaches a gate).
func (w *wgrid) boxDist(b box) int32 {
	best := int32(1 << 30)
	for r := b.r; r < b.r+b.h; r++ {
		row := r * w.cols
		for c := b.c; c < b.c+b.w; c++ {
			if d := w.dist[row+c]; d >= 0 && d < best {
				best = d
			}
		}
	}
	return best
}

// sat is a summed-area table of blocked cells: a box is free when its sum is 0.
type sat struct {
	cols int
	s    []int32
}

func (w *wgrid) newSAT() *sat {
	return &sat{cols: w.cols + 1, s: make([]int32, (w.cols+1)*(w.rows+1))}
}

// build fills the table from blocked (len cols·rows).
func (t *sat) build(blocked []bool, cols, rows int) {
	sc := t.cols
	for r := range rows {
		var run int32
		for c := range cols {
			if blocked[r*cols+c] {
				run++
			}
			t.s[(r+1)*sc+c+1] = t.s[r*sc+c+1] + run
		}
	}
}

// free reports whether box (c, r, w, h) has no blocked cell; the caller keeps it inside the grid.
func (t *sat) free(c, r, w, h int) bool {
	sc := t.cols
	return t.s[(r+h)*sc+c+w]-t.s[r*sc+c+w]-t.s[(r+h)*sc+c]+t.s[r*sc+c] == 0
}

// LargestFreeRect is the largest axis-aligned rectangle of usable cells of the area that are not in occupied (fine
// cell indexes), in cm² (histogram and stack, O(N)).
func LargestFreeRect(a layout.Area, occupied []int) int64 {
	cols, rows := a.Cols, a.Rows
	blk := make([]bool, cols*rows)
	for i := range blk {
		blk[i] = i >= len(a.Usable) || !a.Usable[i]
	}
	for _, i := range occupied {
		if i >= 0 && i < len(blk) {
			blk[i] = true
		}
	}
	return int64(largestRect(blk, cols, rows)) * int64(a.Cell) * int64(a.Cell)
}

// largestRect is the cell count of the largest rectangle of false cells of blk.
func largestRect(blk []bool, cols, rows int) int {
	hgt := make([]int, cols)
	stack := make([]int, 0, cols+1)
	best := 0
	for r := range rows {
		for c := range cols {
			if blk[r*cols+c] {
				hgt[c] = 0
			} else {
				hgt[c]++
			}
		}
		stack = stack[:0]
		for c := 0; c <= cols; c++ {
			h := 0
			if c < cols {
				h = hgt[c]
			}
			for len(stack) > 0 && hgt[stack[len(stack)-1]] >= h {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				left := -1
				if len(stack) > 0 {
					left = stack[len(stack)-1]
				}
				best = max(best, hgt[top]*(c-left-1))
			}
			if c < cols {
				stack = append(stack, c)
			}
		}
	}
	return best
}
