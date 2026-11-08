package engine

import (
	"testing"

	"github.com/coste360/porto/internal/domain/fit"
)

func TestSolve_OnlineFilters(t *testing.T) {
	berth := func(id string, sellable bool, groups ...string) BerthCand {
		return BerthCand{
			ID: id, Code: id, GroupIDs: groups, OnlineSellable: sellable,
			Target: fit.Target{Kind: fit.TargetBerth, MaxLOA: 1500, MaxBeam: 500, Length: 1500, Width: 500},
		}
	}
	snap := Snapshot{
		Berths: []BerthCand{berth("a", true), berth("b", false), berth("c", true, "g1")},
		Lands:  []LandCand{{ID: "l", Code: "l", OnlineSellable: true, Target: fit.Target{Kind: fit.TargetLand, LandKind: fit.LandCradle, MaxLOA: 2000, MaxBeam: 800, Length: 2000, Width: 800}}},
		Busy:   map[string][]NightRange{}, QuotaFull: map[string]bool{"g1": true},
	}
	req := Request{Vessel: fit.Vessel{LOA: 1000, Beam: 350}, Arrival: d("2027-07-01"), Departure: d("2027-07-03"), Purpose: PurposeTransit}
	ids := func(sols []Solution) map[string]bool {
		out := map[string]bool{}
		for _, s := range sols {
			out[s.Segments[0].ResourceID] = true
		}
		return out
	}
	desk := ids(Solve(snap, req, rules))
	if !desk["a"] || !desk["b"] || !desk["c"] {
		t.Fatalf("desk ignores online flags and quotas: %v", desk)
	}
	for _, ch := range []string{"online", "external"} {
		req.Channel = ch
		req.ResourceTypes = []ResourceType{ResourceBerth, ResourceArea, ResourceLand}
		got := ids(Solve(snap, req, rules))
		if len(got) != 1 || !got["a"] {
			t.Fatalf("%s: got %v, want only a", ch, got)
		}
	}
}
