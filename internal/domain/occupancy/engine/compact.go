package engine

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// Compact proposes positions that slide the vessels of a linear area toward the axis start (m3-plan §3.5):
// order is kept, the first vessel starts at margin/2 (snapped to 10 cm), each next one starts on the first
// cell after the previous footprint plus gapTarget, so footprints never share a cell; fixed placements (not
// covering the whole range) are obstacles that stay where they are and nothing jumps over them. Returns a
// proposal for every movable placement whose position changes.
func Compact(area layout.Area, placements []Placed, gapTarget geo.Cm) []Proposal {
	if area.Kind != layout.AreaLinear || area.Cell <= 0 {
		return nil
	}
	ps := slices.Clone(placements)
	slices.SortStableFunc(ps, func(a, b Placed) int {
		if c := cmp.Compare(a.X, b.X); c != 0 {
			return c
		}
		return cmp.Compare(a.AllocationID, b.AllocationID)
	})
	cell := area.Cell
	cursor := snapUp(area.Margin / 2)
	var out []Proposal
	after := func(x, w geo.Cm) geo.Cm {
		lastCell := (x + w + cell - 1) / cell // first cell after the footprint
		return snapUp(lastCell*cell + gapTarget)
	}
	for _, p := range ps {
		if p.Fixed {
			cursor = max(cursor, after(p.X, p.Width))
			continue
		}
		x := min(cursor, p.X)
		if x < p.X {
			out = append(out, Proposal{AllocationID: p.AllocationID, X: x})
		}
		cursor = max(cursor, after(x, p.Width))
	}
	return out
}

// FreeTail returns the free length after the last footprint of placements (the contiguous free space at the end
// of the axis).
func FreeTail(area layout.Area, placements []Placed) geo.Cm {
	end := geo.Cm(0)
	for _, p := range placements {
		end = max(end, p.X+p.Width)
	}
	return max(0, area.Length-end)
}
