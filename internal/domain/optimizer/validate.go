package optimizer

import (
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/domain/layout"
)

// Validate checks a result independently: Footprint inside usable cells, no blocked cell, pairwise disjoint cells
// for time-overlapping items and against obstacles, pinned unchanged, fit OK (ErrInvalidLayout otherwise).
func Validate(in Input, r Result) error {
	if len(r.Placements) != len(in.Items) {
		return fmt.Errorf("%w: %d placements for %d items", ErrInvalidLayout, len(r.Placements), len(in.Items))
	}
	a := in.Grid.Area
	cells := make([][]int, len(in.Items))
	for i := range in.Items {
		it := &in.Items[i]
		p := r.Placements[i]
		if p.ID != it.ID {
			return fmt.Errorf("%w: placement %d is %s, item is %s", ErrInvalidLayout, i, p.ID, it.ID)
		}
		if it.Pinned && (p.At == nil || it.Current == nil || *p.At != *it.Current) {
			return fmt.Errorf("%w: pinned %s moved", ErrInvalidLayout, it.ID)
		}
		if p.At == nil {
			continue
		}
		fp, err := layout.Footprint(layout.Vessel{LOA: it.LOA, Beam: it.Beam}, *p.At, a)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidLayout, it.ID, err)
		}
		if res, ok := itemFits(in, it); !ok {
			return fmt.Errorf("%w: %s fails fit %v", ErrInvalidLayout, it.ID, res.BlockingCodeList())
		}
		cells[i] = fp.Indexes
		for _, o := range in.Fixed {
			if !o.First.After(it.Last) && !it.First.After(o.Last) && intersects(fp.Indexes, sorted(o.Cells)) {
				return fmt.Errorf("%w: %s on obstacle %s", ErrInvalidLayout, it.ID, o.ID)
			}
		}
	}
	for i := range in.Items {
		if cells[i] == nil {
			continue
		}
		for j := i + 1; j < len(in.Items); j++ {
			x, y := &in.Items[i], &in.Items[j]
			if cells[j] == nil || x.First.After(y.Last) || y.First.After(x.Last) {
				continue
			}
			if intersects(cells[i], cells[j]) {
				return fmt.Errorf("%w: %s overlaps %s", ErrInvalidLayout, x.ID, y.ID)
			}
		}
	}
	return nil
}

func sorted(c []int) []int {
	if slices.IsSorted(c) {
		return c
	}
	s := slices.Clone(c)
	slices.Sort(s)
	return s
}

// intersects reports whether two sorted index lists share an element.
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
