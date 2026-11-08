// Package fixture builds the seeded acceptance shed of the optimizer (m9-plan §6.4): a 50 m × 40 m shed
// (2,000 m²), cell 100 cm, margin 60 cm, one 6 m door on a short side, two rows of pillar cells, 40 boats of LOA
// 6–14 m and beam 2.3–4.5 m staying from the autumn to their launch in March–June.
//
// The generator first builds a witness layout that is valid with every exit accessible: an aisle in front of the
// door, boats lying lengthwise in shelves above and below it, launch dates assigned in an order where every boat's
// path (straight to the aisle, then along it to the door) is free on its launch day. Boats that do not fit are
// drawn again smaller. The current placements of the input are then shuffled among the boats, so the optimizer
// starts from a disordered (often invalid) layout while a valid solution is known to exist.
package fixture

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/geo"
)

// Shed geometry.
const (
	Cols               = 50
	Rows               = 40
	Cell        geo.Cm = 100
	Margin      geo.Cm = 60
	Boats              = 40
	aisleTop           = 17 // the door and the aisle are rows 17..22 on the left side
	aisleBottom        = 22
)

// pillarRows and pillarCols are the blocked pillar cells.
var (
	pillarRows = []int{8, 31}
	pillarCols = []int{9, 19, 29, 39}
)

// Fixture is a generated shed: the optimizer input (current placements shuffled) and the witness layout.
type Fixture struct {
	Input   optimizer.Input
	Witness []optimizer.Placement // per item, valid with no blocked exit
}

type splitmix struct{ x uint64 }

func (g *splitmix) next() uint64 {
	g.x += 0x9e3779b97f4a7c15
	z := g.x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (g *splitmix) in(lo, hi int64) int64 { return lo + int64(g.next()%uint64(hi-lo+1)) } //nolint:gosec // small ranges

// Area returns the shed grid.
func Area() layout.Area {
	a := layout.Area{Kind: layout.AreaSurface, Cell: Cell, Margin: Margin, Cols: Cols, Rows: Rows, Usable: make([]bool, Cols*Rows)}
	for i := range a.Usable {
		a.Usable[i] = true
	}
	for _, r := range pillarRows {
		for _, c := range pillarCols {
			i := r*Cols + c
			a.Usable[i] = false
			a.Blocked = append(a.Blocked, i)
		}
	}
	slices.Sort(a.Blocked)
	return a
}

// Exits returns the door cells (left side, rows 17..22).
func Exits() []int {
	var ex []int
	for r := aisleTop; r <= aisleBottom; r++ {
		ex = append(ex, r*Cols)
	}
	return ex
}

type rect struct{ c, r, w, h int }

func (a rect) overlaps(b rect) bool {
	return a.c < b.c+b.w && b.c < a.c+a.w && a.r < b.r+b.h && b.r < a.r+a.h
}

// sweep is the region a boat crosses to leave: straight to the aisle, then along it to the door.
func sweep(b rect) []rect {
	aisle := rect{c: 0, r: aisleTop, w: b.c + b.w, h: aisleBottom - aisleTop + 1}
	switch {
	case b.r+b.h-1 < aisleTop:
		return []rect{{c: b.c, r: b.r, w: b.w, h: aisleBottom - b.r + 1}, aisle}
	case b.r > aisleBottom:
		return []rect{{c: b.c, r: aisleTop, w: b.w, h: b.r + b.h - aisleTop}, aisle}
	default:
		return []rect{aisle}
	}
}

func inAisle(b rect) bool { return b.r >= aisleTop && b.r+b.h-1 <= aisleBottom }

func straddles(b rect) bool {
	return !inAisle(b) && b.r <= aisleBottom && b.r+b.h-1 >= aisleTop
}

func blockedIn(a layout.Area, b rect) bool {
	for r := b.r; r < b.r+b.h; r++ {
		for c := b.c; c < b.c+b.w; c++ {
			if !a.Usable[r*Cols+c] {
				return true
			}
		}
	}
	return false
}

// place finds the first witness position (rows top to bottom, columns from the far wall) of a w × h box.
func place(a layout.Area, placed []rect, w, h int) (rect, bool) {
	for r := 0; r+h <= Rows; r++ {
		for c := Cols - w; c >= 0; c-- {
			b := rect{c: c, r: r, w: w, h: h}
			if straddles(b) || blockedIn(a, b) {
				continue
			}
			ok := true
			for _, p := range placed {
				if p.overlaps(b) || (inAisle(b) && inAisle(p) && p.c < b.c+b.w && b.c < p.c+p.w) {
					ok = false
					break
				}
			}
			for _, s := range sweep(b) {
				if ok && blockedIn(a, s) {
					ok = false
				}
			}
			if ok {
				return b, true
			}
		}
	}
	return rect{}, false
}

func cells(l geo.Cm) int { return int((l + Margin + Cell - 1) / Cell) }

// Generate builds the fixture of a seed.
func Generate(seed uint64) Fixture {
	g := &splitmix{x: seed}
	a := Area()
	type boat struct {
		loa, beam geo.Cm
		at        rect
	}
	boats := make([]boat, 0, Boats)
	var placed []rect
	for len(boats) < Boats {
		// Skewed towards small boats (u·v), every size of the range still occurs.
		loa := geo.Cm(600 + g.in(0, 800)*g.in(0, 800)/800)
		beam := geo.Cm(min(450, max(230, 230+(int64(loa)-600)*220/800+g.in(-30, 30))))
		for {
			b, ok := place(a, placed, cells(loa), cells(beam))
			if ok {
				placed = append(placed, b)
				boats = append(boats, boat{loa: loa, beam: beam, at: b})
				break
			}
			if loa == 600 && beam == 230 {
				panic(fmt.Sprintf("fixture: seed %d: no room for boat %d", seed, len(boats)+1))
			}
			loa, beam = max(600, loa-100), max(230, beam-20)
		}
	}
	// Launch order: repeatedly the boat whose path no remaining boat crosses, nearest the door first.
	left := make([]int, Boats)
	for i := range left {
		left[i] = i
	}
	order := make([]int, 0, Boats)
	for len(left) > 0 {
		best := -1
		for k, i := range left {
			free := true
			for _, s := range sweep(boats[i].at) {
				for _, j := range left {
					if j != i && s.overlaps(boats[j].at) {
						free = false
					}
				}
			}
			if free && (best < 0 || boats[i].at.c < boats[left[best]].at.c) {
				best = k
			}
		}
		if best < 0 {
			panic(fmt.Sprintf("fixture: seed %d: cyclic witness", seed))
		}
		order = append(order, left[best])
		left = slices.Delete(left, best, best+1)
	}
	launch0 := calendar.New(2027, 3, 1)
	arrive0 := calendar.New(2026, 10, 1)
	items := make([]optimizer.Item, Boats)
	witness := make([]optimizer.Placement, Boats)
	for k, i := range order {
		b := boats[i]
		exit := launch0.AddDays(3 * k)            // March 1 … June 26, one launch every three days
		arrival := arrive0.AddDays(Boats - 1 - k) // last out arrives first
		items[i] = optimizer.Item{
			ID: fmt.Sprintf("B%02d", i+1), LOA: b.loa, Beam: b.beam,
			Fit:     fit.Vessel{LOA: b.loa, Beam: b.beam, Hull: "monohull_motor"},
			Support: "cradle", First: arrival, Last: exit.AddDays(-1), ArrivalOn: arrival, ExitOn: exit,
		}
		witness[i] = optimizer.Placement{X: geo.Cm(b.at.c) * Cell, Y: geo.Cm(b.at.r) * Cell}
	}
	// Shuffle the current placements among the boats (Fisher–Yates).
	perm := make([]int, Boats)
	for i := range perm {
		perm[i] = i
	}
	for i := Boats - 1; i > 0; i-- {
		j := int(g.in(0, int64(i)))
		perm[i], perm[j] = perm[j], perm[i]
	}
	for i := range items {
		p := witness[perm[i]]
		items[i].Current = &p
	}
	slices.SortFunc(items, func(x, y optimizer.Item) int { return cmp.Compare(x.ID, y.ID) })
	in := optimizer.Input{
		Grid: optimizer.NewGrid(a, Exits()), Items: items, Seed: seed, Margin: Margin,
		Fit:  optimizer.FitParams{Rules: fit.Rules{SafetyClearance: 30}, Target: fit.Target{Kind: fit.TargetSurfaceArea, MultihullOK: true, Supports: []string{"cradle", "trailer"}}},
		From: arrive0, To: launch0.AddDays(3 * Boats),
	}
	return Fixture{Input: in, Witness: witness}
}

// Density is the share (bp) of usable cells the witness footprints cover.
func (f Fixture) Density() int64 {
	var used int64
	for i, it := range f.Input.Items {
		_ = i
		used += int64(cells(it.LOA)) * int64(cells(it.Beam))
	}
	return used * 10000 / int64(Cols*Rows-len(pillarRows)*len(pillarCols))
}
