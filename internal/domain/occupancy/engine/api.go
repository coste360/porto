package engine

import (
	"cmp"
	"errors"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/layout"
)

// Plan errors (layout.Footprint errors are returned as they are).
var (
	ErrPlacementRequired   = errors.New("engine: placement required for a dynamic area")
	ErrPlacementNotAllowed = errors.New("engine: placement not allowed on a berth or land resource")
	ErrAreaRequired        = errors.New("engine: area required")
	ErrBadRange            = errors.New("engine: last night before first night")
)

// Cells returns the cells an allocation spec occupies: nil for berths and land resources, the sorted footprint
// cells for dynamic areas.
func Cells(spec AllocSpec, area *layout.Area, vessel layout.Vessel) ([]int, error) {
	if spec.ResourceType != ResourceArea {
		if spec.Placement != nil {
			return nil, ErrPlacementNotAllowed
		}
		return nil, nil
	}
	if spec.Placement == nil {
		return nil, ErrPlacementRequired
	}
	if area == nil {
		return nil, ErrAreaRequired
	}
	c, err := layout.Footprint(vessel, layout.Placement(*spec.Placement), *area)
	if err != nil {
		return nil, err
	}
	return CellList(c), nil
}

// CellList expands footprint cells to the sorted list of cell indexes.
func CellList(c layout.Cells) []int {
	if c.Indexes != nil {
		return slices.Clone(c.Indexes)
	}
	out := make([]int, 0, c.Last-c.First+1)
	for i := c.First; i <= c.Last; i++ {
		out = append(out, i)
	}
	return out
}

// PlanNights returns the nights of spec: [First, Last] minus the released ranges, ascending.
func PlanNights(spec AllocSpec) []calendar.Date {
	if spec.Last.Before(spec.First) {
		return nil
	}
	out := make([]calendar.Date, 0, Nights(spec.First, spec.Last))
	for d := spec.First; !d.After(spec.Last); d = d.AddDays(1) {
		if !Released(spec.Released, d) {
			out = append(out, d)
		}
	}
	return out
}

// Released reports whether night d falls in one of the ranges.
func Released(ranges []NightRange, d calendar.Date) bool {
	for _, r := range ranges {
		if !d.Before(r.From) && !d.After(r.To) {
			return true
		}
	}
	return false
}

// Plan returns the occupancy rows of spec, sorted by night then cell: Footprint × nights − Released (INV-03).
// area is required for dynamic areas and vessel is the effective size. Footprint errors are returned as layout
// errors.
func Plan(spec AllocSpec, area *layout.Area, vessel layout.Vessel) ([]Row, error) {
	if spec.Last.Before(spec.First) {
		return nil, ErrBadRange
	}
	cells, err := Cells(spec, area, vessel)
	if err != nil {
		return nil, err
	}
	if cells == nil {
		cells = []int{0}
	}
	nights := PlanNights(spec)
	out := make([]Row, 0, len(nights)*len(cells))
	for _, n := range nights {
		for _, c := range cells {
			out = append(out, Row{Resource: spec.ResourceID, Cell: c, Night: n})
		}
	}
	return out, nil
}

// CompareRows orders rows by resource, night, then cell (the order of Plan for one resource).
func CompareRows(a, b Row) int {
	if c := cmp.Compare(a.Resource, b.Resource); c != 0 {
		return c
	}
	if c := a.Night.Compare(b.Night); c != 0 {
		return c
	}
	return cmp.Compare(a.Cell, b.Cell)
}

// Nights returns the number of nights of the inclusive range [first, last] (0 when last < first).
func Nights(first, last calendar.Date) int {
	if last.Before(first) {
		return 0
	}
	return first.DaysUntil(last) + 1
}

// SubtractRanges returns [first, last] minus the ranges, as sorted disjoint ranges.
func SubtractRanges(first, last calendar.Date, ranges []NightRange) []NightRange {
	var out []NightRange
	var cur *NightRange
	for d := first; !d.After(last); d = d.AddDays(1) {
		if Released(ranges, d) {
			cur = nil
			continue
		}
		if cur == nil {
			out = append(out, NightRange{From: d, To: d})
			cur = &out[len(out)-1]
		} else {
			cur.To = d
		}
	}
	return out
}

// ClipRanges returns the parts of ranges inside [first, last].
func ClipRanges(ranges []NightRange, first, last calendar.Date) []NightRange {
	var out []NightRange
	for _, r := range ranges {
		f, l := r.From, r.To
		if f.Before(first) {
			f = first
		}
		if l.After(last) {
			l = last
		}
		if !l.Before(f) {
			out = append(out, NightRange{From: f, To: l})
		}
	}
	return out
}

// GroupNights compresses sorted nights into ranges.
func GroupNights(nights []calendar.Date) []NightRange {
	var out []NightRange
	for _, n := range nights {
		if k := len(out); k > 0 && out[k-1].To.AddDays(1).Equal(n) {
			out[k-1].To = n
			continue
		} else if k > 0 && out[k-1].To.Equal(n) {
			continue
		}
		out = append(out, NightRange{From: n, To: n})
	}
	return out
}

// GroupCells compresses sorted distinct cells into intervals.
func GroupCells(cells []int) []Interval {
	var out []Interval
	for _, c := range cells {
		if k := len(out); k > 0 && out[k-1].Last+1 == c {
			out[k-1].Last = c
			continue
		} else if k > 0 && out[k-1].Last == c {
			continue
		}
		out = append(out, Interval{First: c, Last: c})
	}
	return out
}

// FreeIntervals returns the cell intervals free on every night: the intersection over nights of the
// complements of busy (one slice of occupied intervals per night) within [0, cellCount).
func FreeIntervals(cellCount int, busy [][]Interval) []Interval {
	if cellCount <= 0 {
		return nil
	}
	var all []Interval
	for _, night := range busy {
		for _, iv := range night {
			f, l := max(iv.First, 0), min(iv.Last, cellCount-1)
			if f <= l {
				all = append(all, Interval{First: f, Last: l})
			}
		}
	}
	slices.SortFunc(all, func(a, b Interval) int { return cmp.Compare(a.First, b.First) })
	var out []Interval
	next := 0
	for _, iv := range all {
		if iv.First > next {
			out = append(out, Interval{First: next, Last: iv.First - 1})
		}
		next = max(next, iv.Last+1)
	}
	if next < cellCount {
		out = append(out, Interval{First: next, Last: cellCount - 1})
	}
	return out
}
