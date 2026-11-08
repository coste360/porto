package swap

import (
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/geo"
)

var day0 = calendar.MustParse("2026-06-01")

func nights(first, n int) []calendar.Date {
	out := make([]calendar.Date, n)
	for i := range n {
		out[i] = day0.AddDays(first + i)
	}
	return out
}

func berth(id string, length, width geo.Cm) Berth {
	return Berth{ID: id, Code: id, MapID: "m", Target: fit.Target{Kind: fit.TargetBerth, MaxLOA: length, MaxBeam: width, Length: length, Width: width, MultihullOK: true}}
}

func busyOf(as []Alloc) map[string]map[calendar.Date]string {
	out := map[string]map[calendar.Date]string{}
	for _, a := range as {
		if out[a.BerthID] == nil {
			out[a.BerthID] = map[calendar.Date]string{}
		}
		for _, n := range a.Nights {
			out[a.BerthID][n] = a.ID
		}
	}
	return out
}

func TestSwap_Simple(t *testing.T) {
	// Equal stays: swapping two boats between two berths never changes the free surface (Σ area − Σ vessel), so
	// no swap is suggested and, without a free smaller berth, no move either.
	as := []Alloc{
		{ID: "small", BerthID: "big", MapID: "m", Kind: calc.KindContract, Nights: nights(0, 10), Vessel: fit.Vessel{LOA: 600, Beam: 250}, Listed: true},
		{ID: "mid", BerthID: "mid", MapID: "m", Kind: calc.KindTransit, Nights: nights(0, 10), Vessel: fit.Vessel{LOA: 1400, Beam: 450}},
	}
	in := Input{Berths: []Berth{berth("big", 1500, 500), berth("mid", 1400, 450)}, Allocs: as, Busy: busyOf(as), Today: day0}
	if sugs, truncated := Suggest(in); truncated || len(sugs) != 0 {
		t.Fatalf("no strict gain expected: %+v", sugs)
	}
	// A free smaller berth: move.
	in.Berths = append(in.Berths, berth("tiny", 700, 300))
	sugs, _ := Suggest(in)
	if len(sugs) != 1 || sugs[0].Kind != "move" || sugs[0].ToBerths[0].ID != "tiny" || sugs[0].GainCm2Nights <= 0 {
		t.Fatalf("move expected: %+v", sugs)
	}
	// The long stay of the small boat on the big berth against a short stay on the medium berth (whose nights the
	// small boat covers): the swap moves the long stay to the smaller berth, gain (A_big − A_mid) × (10 − 4).
	as = []Alloc{
		{ID: "small", BerthID: "big", MapID: "m", Kind: calc.KindContract, Nights: nights(0, 10), Vessel: fit.Vessel{LOA: 600, Beam: 250}, Listed: true},
		{ID: "mid", BerthID: "mid", MapID: "m", Kind: calc.KindTransit, Nights: nights(2, 4), Vessel: fit.Vessel{LOA: 1400, Beam: 450}},
		{ID: "other", BerthID: "mid", MapID: "m", Kind: calc.KindTransit, Nights: nights(8, 2), Vessel: fit.Vessel{LOA: 1400, Beam: 450}},
	}
	in = Input{Berths: []Berth{berth("big", 1500, 500), berth("mid", 1400, 450)}, Allocs: as, Busy: busyOf(as), Today: day0}
	if sugs, _ := Suggest(in); len(sugs) != 0 {
		t.Fatalf("the medium berth is taken by another allocation on nights of the small boat: %+v", sugs)
	}
	as = as[:2]
	in = Input{Berths: []Berth{berth("big", 1500, 500), berth("mid", 1400, 450)}, Allocs: as, Busy: busyOf(as), Today: day0}
	sugs, _ = Suggest(in)
	want := int64(1500*500-1400*450) * 6
	if len(sugs) != 1 || sugs[0].Kind != "swap" || sugs[0].GainCm2Nights != want {
		t.Fatalf("swap expected (gain %d): %+v", want, sugs)
	}
}

// TestSwap_SuggestionsValid: every suggestion is conflict-free, fits without blocking issues, has a strictly
// positive gain, and suggestions are disjoint.
func TestSwap_SuggestionsValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nb := rapid.IntRange(2, 6).Draw(t, "berths")
		var berths []Berth
		for i := range nb {
			l := geo.Cm(rapid.IntRange(6, 20).Draw(t, "l"+strconv.Itoa(i)) * 100)
			berths = append(berths, berth("b"+strconv.Itoa(i), l, l/3))
		}
		var as []Alloc
		taken := map[string]map[calendar.Date]bool{}
		for i := range rapid.IntRange(1, 8).Draw(t, "allocs") {
			id := "a" + strconv.Itoa(i)
			b := berths[rapid.IntRange(0, nb-1).Draw(t, "b"+id)]
			first, n := rapid.IntRange(0, 20).Draw(t, "f"+id), rapid.IntRange(1, 10).Draw(t, "n"+id)
			ns := nights(first, n)
			if taken[b.ID] == nil {
				taken[b.ID] = map[calendar.Date]bool{}
			}
			conflict := false
			for _, x := range ns {
				conflict = conflict || taken[b.ID][x]
			}
			if conflict {
				continue
			}
			for _, x := range ns {
				taken[b.ID][x] = true
			}
			loa := geo.Cm(rapid.IntRange(4, int(b.Target.Length/100)).Draw(t, "loa"+id) * 100)
			as = append(as, Alloc{
				ID: id, BerthID: b.ID, MapID: "m", Kind: calc.KindContract, Nights: ns,
				Vessel: fit.Vessel{LOA: loa, Beam: min(loa/3, b.Target.Width)}, Listed: rapid.Bool().Draw(t, "listed"+id),
			})
		}
		in := Input{Berths: berths, Allocs: as, Busy: busyOf(as), Today: day0}
		sugs, _ := Suggest(in)
		byID := map[string]Alloc{}
		for _, a := range as {
			byID[a.ID] = a
		}
		bs := map[string]Berth{}
		for _, b := range berths {
			bs[b.ID] = b
		}
		used := map[string]bool{}
		for _, s := range sugs {
			if s.GainCm2Nights <= 0 || s.WasteAfterBP > s.WasteBeforeBP {
				t.Fatalf("no gain: %+v", s)
			}
			for i, id := range s.AllocationIDs {
				if used[id] {
					t.Fatalf("allocation %s in two suggestions", id)
				}
				used[id] = true
				a := byID[id]
				to := bs[s.ToBerths[i].ID]
				if !fit.Check(a.Vessel, to.Target, a.Nights[len(a.Nights)-1], fit.Rules{}).OK {
					t.Fatalf("does not fit: %+v", s)
				}
				other := ""
				if s.Kind == "swap" {
					other = s.AllocationIDs[1-i]
				}
				for _, n := range a.Nights {
					if h, ok := in.Busy[to.ID][n]; ok && h != other && h != a.ID {
						t.Fatalf("conflict on %s %s: %+v", to.ID, n, s)
					}
				}
			}
		}
	})
}
