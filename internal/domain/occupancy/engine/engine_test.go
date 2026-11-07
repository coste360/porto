package engine

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

var rules = fit.Rules{LOATolerance: 50, SafetyClearance: 30, DepthWarnMargin: 10, WasteThresholdBP: 0}

func linear(length, cell, margin geo.Cm) layout.Area {
	return layout.Area{Kind: layout.AreaLinear, Length: length, Mode: layout.ModeSternTo, Cell: cell, Margin: margin}
}

func TestPlan_RowsAreFootprintTimesNights(t *testing.T) {
	area := linear(6000, 50, 50)
	cases := []struct {
		name  string
		spec  AllocSpec
		area  *layout.Area
		v     layout.Vessel
		rows  int
		cells []int
		err   error
	}{
		{"berth 3 nights", AllocSpec{ResourceType: ResourceBerth, ResourceID: "b", First: d("2027-07-01"), Last: d("2027-07-03")}, nil, layout.Vessel{LOA: 1000, Beam: 350}, 3, nil, nil},
		{"land 1 night", AllocSpec{ResourceType: ResourceLand, ResourceID: "l", First: d("2027-07-01"), Last: d("2027-07-01")}, nil, layout.Vessel{LOA: 1, Beam: 1}, 1, nil, nil},
		{"linear 400+50 cm on 50 cm cells", AllocSpec{ResourceType: ResourceArea, ResourceID: "a", Placement: &Placement{X: 100}, First: d("2027-07-01"), Last: d("2027-07-02")}, &area, layout.Vessel{LOA: 1000, Beam: 400}, 18, []int{2, 3, 4, 5, 6, 7, 8, 9, 10}, nil},
		{"linear unsnapped", AllocSpec{ResourceType: ResourceArea, ResourceID: "a", Placement: &Placement{X: 105}, First: d("2027-07-01"), Last: d("2027-07-02")}, &area, layout.Vessel{LOA: 1000, Beam: 400}, 0, nil, layout.ErrUnsnapped},
		{"linear outside", AllocSpec{ResourceType: ResourceArea, ResourceID: "a", Placement: &Placement{X: 5800}, First: d("2027-07-01"), Last: d("2027-07-02")}, &area, layout.Vessel{LOA: 1000, Beam: 400}, 0, nil, layout.ErrOutsideArea},
		{"area without placement", AllocSpec{ResourceType: ResourceArea, ResourceID: "a", First: d("2027-07-01"), Last: d("2027-07-02")}, &area, layout.Vessel{LOA: 1000, Beam: 400}, 0, nil, ErrPlacementRequired},
		{"berth with placement", AllocSpec{ResourceType: ResourceBerth, ResourceID: "b", Placement: &Placement{}, First: d("2027-07-01"), Last: d("2027-07-02")}, nil, layout.Vessel{LOA: 1000, Beam: 400}, 0, nil, ErrPlacementNotAllowed},
		{"bad range", AllocSpec{ResourceType: ResourceBerth, ResourceID: "b", First: d("2027-07-02"), Last: d("2027-07-01")}, nil, layout.Vessel{LOA: 1000, Beam: 400}, 0, nil, ErrBadRange},
		{"released middle", AllocSpec{ResourceType: ResourceBerth, ResourceID: "b", First: d("2027-07-01"), Last: d("2027-07-10"), Released: []NightRange{{d("2027-07-03"), d("2027-07-05")}}}, nil, layout.Vessel{LOA: 1000, Beam: 350}, 7, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := Plan(c.spec, c.area, c.v)
			if !errors.Is(err, c.err) {
				t.Fatalf("err %v, want %v", err, c.err)
			}
			if len(rows) != c.rows {
				t.Fatalf("%d rows, want %d", len(rows), c.rows)
			}
			if c.cells != nil {
				cells, _ := Cells(c.spec, c.area, c.v)
				if !reflect.DeepEqual(cells, c.cells) {
					t.Fatalf("cells %v", cells)
				}
			}
			if !slices.IsSortedFunc(rows, CompareRows) {
				t.Fatal("rows not sorted")
			}
		})
	}
}

func genRanges(t *rapid.T, first, last calendar.Date) []NightRange {
	n := Nights(first, last)
	var out []NightRange
	at := 0
	for at < n && rapid.Bool().Draw(t, "more") {
		from := at + rapid.IntRange(0, 3).Draw(t, "gap")
		to := from + rapid.IntRange(0, 3).Draw(t, "len")
		if to >= n {
			break
		}
		out = append(out, NightRange{From: first.AddDays(from), To: first.AddDays(to)})
		at = to + 2
	}
	return out
}

func TestRapid_PlanExcludesReleased(t *testing.T) {
	area := linear(3000, 50, 50)
	rapid.Check(t, func(t *rapid.T) {
		first := d("2027-06-01").AddDays(rapid.IntRange(0, 30).Draw(t, "first"))
		last := first.AddDays(rapid.IntRange(0, 40).Draw(t, "len"))
		rel := genRanges(t, first, last)
		x := geo.Cm(rapid.IntRange(3, 200).Draw(t, "x")) * 10
		spec := AllocSpec{ResourceType: ResourceArea, ResourceID: "a", Placement: &Placement{X: x}, First: first, Last: last, Released: rel}
		v := layout.Vessel{LOA: 900, Beam: geo.Cm(rapid.IntRange(150, 500).Draw(t, "beam"))}
		rows, err := Plan(spec, &area, v)
		if err != nil {
			return // placement outside: no rows to check
		}
		cells, _ := Cells(spec, &area, v)
		nights := PlanNights(spec)
		if len(rows) != len(cells)*len(nights) {
			t.Fatalf("%d rows for %d cells × %d nights", len(rows), len(cells), len(nights))
		}
		for _, r := range rows {
			if Released(rel, r.Night) || r.Night.Before(first) || r.Night.After(last) {
				t.Fatalf("row on night %s", r.Night)
			}
		}
		released := 0
		for _, r := range rel {
			released += Nights(r.From, r.To)
		}
		if len(nights)+released != Nights(first, last) {
			t.Fatalf("nights %d + released %d != %d", len(nights), released, Nights(first, last))
		}
	})
}

func FuzzPlan(f *testing.F) {
	f.Add(int64(100), int64(400), int64(1000), 3, 1, 1)
	f.Add(int64(0), int64(1), int64(1), 0, 0, 0)
	area := linear(6000, 50, 50)
	f.Fuzz(func(t *testing.T, x, beam, loa int64, nights, relFrom, relLen int) {
		if nights < 0 || nights > 400 || relFrom < 0 || relLen < 0 || relLen > 400 {
			return
		}
		first := d("2027-01-01")
		spec := AllocSpec{
			ResourceType: ResourceArea, ResourceID: "a", Placement: &Placement{X: geo.Cm(x)}, First: first,
			Last: first.AddDays(nights), Released: []NightRange{{first.AddDays(relFrom), first.AddDays(relFrom + relLen)}},
		}
		rows, err := Plan(spec, &area, layout.Vessel{LOA: geo.Cm(loa), Beam: geo.Cm(beam)})
		if err != nil {
			return
		}
		seen := map[Row]bool{}
		for _, r := range rows {
			if seen[r] {
				t.Fatalf("duplicate row %+v", r)
			}
			seen[r] = true
			if r.Cell < 0 || r.Cell >= layout.CellCount(area) {
				t.Fatalf("cell %d out of area", r.Cell)
			}
		}
	})
}

func TestFreeIntervals_Scenario(t *testing.T) {
	iv := func(a, b int) Interval { return Interval{a, b} }
	cases := []struct {
		name string
		n    int
		busy [][]Interval
		want []Interval
	}{
		{"empty area", 10, nil, []Interval{iv(0, 9)}},
		{"no cells", 0, nil, nil},
		{"one busy", 10, [][]Interval{{iv(3, 5)}}, []Interval{iv(0, 2), iv(6, 9)}},
		{"union over nights", 10, [][]Interval{{iv(0, 1)}, {iv(4, 4)}, {iv(8, 9)}}, []Interval{iv(2, 3), iv(5, 7)}},
		{"overlapping busy", 10, [][]Interval{{iv(2, 5), iv(4, 7)}}, []Interval{iv(0, 1), iv(8, 9)}},
		{"full", 5, [][]Interval{{iv(0, 4)}}, nil},
		{"clipped", 5, [][]Interval{{iv(-3, 1), iv(3, 99)}}, []Interval{iv(2, 2)}},
		{"adjacent", 6, [][]Interval{{iv(0, 1), iv(2, 3)}}, []Interval{iv(4, 5)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FreeIntervals(c.n, c.busy); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestRapid_FreeIntervalsDisjointFromBusy(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 200).Draw(t, "n")
		nights := rapid.IntRange(0, 5).Draw(t, "nights")
		busy := make([][]Interval, nights)
		taken := make([]bool, n)
		for i := range busy {
			for range rapid.IntRange(0, 4).Draw(t, "k") {
				f := rapid.IntRange(0, n-1).Draw(t, "f")
				l := rapid.IntRange(f, n-1).Draw(t, "l")
				busy[i] = append(busy[i], Interval{f, l})
				for c := f; c <= l; c++ {
					taken[c] = true
				}
			}
		}
		free := make([]bool, n)
		prev := -2
		for _, iv := range FreeIntervals(n, busy) {
			if iv.First <= prev+1 {
				t.Fatalf("intervals not disjoint and separated: %v", iv)
			}
			prev = iv.Last
			for c := iv.First; c <= iv.Last; c++ {
				free[c] = true
			}
		}
		for c := range n {
			if free[c] == taken[c] {
				t.Fatalf("cell %d free %v taken %v", c, free[c], taken[c])
			}
		}
	})
}

func p64(v int64) *int64 { return &v }

func berth(id string, maxLOA, maxBeam geo.Cm, dedicated bool) BerthCand {
	depth := geo.Cm(400)
	return BerthCand{
		ID: id, Code: strings.ToUpper(id), MapID: "m", StructureID: "s1", TransitDedicated: dedicated, Water: true,
		Target: fit.Target{Kind: fit.TargetBerth, MaxLOA: maxLOA, MaxBeam: maxBeam, Length: maxLOA, Width: maxBeam + 50, Depth: &depth, PowerA: p64(16)},
	}
}

func areaCand(id string, length geo.Cm) AreaCand {
	return AreaCand{
		ID: id, Code: strings.ToUpper(id), MapID: "m", Mode: layout.ModeSternTo, CellCm: 50, MarginCm: 50, LengthCm: length,
		CellCount: int(length / 50), Target: fit.Target{Kind: fit.TargetLinearArea, MultihullOK: true},
	}
}

func req(arr, dep string) Request {
	return Request{
		Vessel: fit.Vessel{LOA: 1000, Beam: 350, Hull: "monohull_sail"}, VesselID: "v1", Arrival: d(arr), Departure: d(dep),
		Purpose: PurposeTransit, Channel: "desk",
	}
}

func keys(sols []Solution) []string {
	out := make([]string, len(sols))
	for i, s := range sols {
		out[i] = s.Key
	}
	return out
}

func TestSolve_Scenario(t *testing.T) {
	t.Run("fixed berth: fit and free, transit-dedicated first", func(t *testing.T) {
		s := Snapshot{Berths: []BerthCand{berth("b1", 1200, 400, false), berth("b2", 1200, 400, true), berth("b3", 900, 400, true)}}
		sols := Solve(s, req("2027-07-01", "2027-07-04"), rules)
		if len(sols) != 2 || sols[0].Segments[0].ResourceID != "b2" || sols[1].Segments[0].ResourceID != "b1" {
			t.Fatalf("%v", keys(sols))
		}
		if sols[1].Score-sols[0].Score != PenaltyNotTransitDedicated {
			t.Fatalf("scores %d %d", sols[0].Score, sols[1].Score)
		}
		if seg := sols[0].Segments[0]; !seg.First.Equal(d("2027-07-01")) || !seg.Last.Equal(d("2027-07-03")) {
			t.Fatalf("segment %+v", seg)
		}
	})
	t.Run("busy berth excluded; departure exclusive", func(t *testing.T) {
		s := Snapshot{
			Berths: []BerthCand{berth("b1", 1200, 400, true), berth("b2", 1200, 400, true)},
			Busy:   map[string][]NightRange{"b1": {{d("2027-07-03"), d("2027-07-05")}}, "b2": {{d("2027-07-04"), d("2027-07-04")}}},
		}
		sols := Solve(s, req("2027-07-01", "2027-07-04"), rules)
		if len(sols) != 1 || sols[0].Segments[0].ResourceID != "b2" {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("best fit by waste", func(t *testing.T) {
		s := Snapshot{Berths: []BerthCand{berth("big", 2000, 600, true), berth("small", 1100, 380, true)}}
		sols := Solve(s, req("2027-07-01", "2027-07-02"), rules)
		if len(sols) != 2 || sols[0].Segments[0].ResourceID != "small" {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("linear area best fit and adjacency", func(t *testing.T) {
		a := areaCand("a", 3000)
		s := Snapshot{
			Areas: []AreaCand{a},
			// a vessel on cells 10..19 (500–1000 cm) on both nights; another on 40..49 the first night only
			AreaBusy: map[string]map[calendar.Date][]Interval{"a": {d("2027-07-01"): {{10, 19}, {40, 49}}, d("2027-07-02"): {{10, 19}}}},
		}
		sols := Solve(s, req("2027-07-01", "2027-07-03"), rules)
		if len(sols) == 0 {
			t.Fatal("no solution")
		}
		// free intervals: [0,9] (0–500 cm, footprint 400 fits), [20,39] (1000–2000), [50,59] (2500–3000)
		xs := map[geo.Cm]bool{}
		for _, s := range sols {
			xs[s.Segments[0].Placement.X] = true
		}
		for _, want := range []geo.Cm{30, 100, 1000, 1600, 2500, 2600} {
			if !xs[want] {
				t.Errorf("missing x=%d in %v", want, keys(sols))
			}
		}
		if got := sols[0].Segments[0].Placement.X; got != 30 && got != 100 {
			t.Fatalf("best is x=%d, want the smallest interval", got)
		}
	})
	t.Run("surface areas and unwanted types excluded", func(t *testing.T) {
		s := Snapshot{Berths: []BerthCand{berth("b1", 1200, 400, true)}, Areas: []AreaCand{areaCand("a", 3000)}}
		r := req("2027-07-01", "2027-07-02")
		r.ResourceTypes = []ResourceType{ResourceArea}
		for _, sol := range Solve(s, r, rules) {
			if sol.Segments[0].ResourceType != ResourceArea {
				t.Fatalf("%v", sol.Key)
			}
		}
	})
	t.Run("closure on arrival excludes; closure later does not", func(t *testing.T) {
		s := Snapshot{
			Berths:   []BerthCand{berth("b1", 1200, 400, true), berth("b2", 1200, 400, true)},
			Closures: []Closure{{ID: "c1", ResourceIDs: []string{"b1"}, From: d("2027-07-01"), To: d("2027-07-01")}, {ID: "c2", ResourceIDs: []string{"b2"}, From: d("2027-07-02"), To: d("2027-07-05")}},
		}
		sols := Solve(s, req("2027-07-01", "2027-07-04"), rules)
		if len(sols) != 1 || sols[0].Segments[0].ResourceID != "b2" {
			t.Fatalf("%v", keys(sols))
		}
		s.Closures = []Closure{{ID: "all", From: d("2027-06-30"), To: d("2027-07-01")}}
		if sols := Solve(s, req("2027-07-01", "2027-07-04"), rules); len(sols) != 0 {
			t.Fatalf("marina closure: %v", keys(sols))
		}
	})
	t.Run("continuity with the previous berth", func(t *testing.T) {
		s := Snapshot{Berths: []BerthCand{berth("b1", 1200, 400, true), berth("b2", 1200, 400, true)}, Previous: map[string]string{"v1": "b2"}}
		sols := Solve(s, req("2027-07-01", "2027-07-02"), rules)
		if sols[0].Segments[0].ResourceID != "b2" || sols[1].Score-sols[0].Score != PenaltyNotPrevious {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("fit blocked berths are not proposed", func(t *testing.T) {
		s := Snapshot{Berths: []BerthCand{berth("b1", 800, 300, true)}}
		if sols := Solve(s, req("2027-07-01", "2027-07-02"), rules); len(sols) != 0 {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("segments only when no single resource; at most max segments changes", func(t *testing.T) {
		s := Snapshot{
			Berths: []BerthCand{berth("b1", 1200, 400, true), berth("b2", 1200, 400, true), berth("b3", 1200, 400, true)},
			Busy: map[string][]NightRange{
				"b1": {{d("2027-07-03"), d("2027-07-09")}},
				"b2": {{d("2027-07-01"), d("2027-07-02")}, {d("2027-07-05"), d("2027-07-09")}},
				"b3": {{d("2027-07-01"), d("2027-07-04")}},
			},
		}
		r := req("2027-07-01", "2027-07-07")
		if sols := Solve(s, r, rules); len(sols) != 0 {
			t.Fatalf("segments with max 0: %v", keys(sols))
		}
		r.MaxSegments = 1
		if sols := Solve(s, r, rules); len(sols) != 0 {
			t.Fatalf("one change cannot cover: %v", keys(sols))
		}
		r.MaxSegments = 2
		sols := Solve(s, r, rules)
		if len(sols) == 0 {
			t.Fatal("no two-change solution")
		}
		for _, sol := range sols {
			if len(sol.Segments) > 3 {
				t.Fatalf("too many segments %v", sol.Key)
			}
			assertContiguous(t, sol, r)
		}
		// b1 1–2, b2 3–4, b3 5–6
		if sols[0].Key != "berth:b1:2027-07-01:2027-07-02|berth:b2:2027-07-03:2027-07-04|berth:b3:2027-07-05:2027-07-06" {
			t.Fatalf("%v", keys(sols))
		}
		if sols[0].Score < 2*PenaltySegment {
			t.Fatalf("score %d", sols[0].Score)
		}
		s.Busy["b3"] = nil
		r.MaxSegments = 1
		if sols := Solve(s, r, rules); len(sols) != 1 || len(sols[0].Segments) != 1 {
			t.Fatalf("a single resource wins: %v", keys(sols))
		}
	})
	t.Run("segments over a berth and a linear area", func(t *testing.T) {
		full := []Interval{{0, 9}}
		s := Snapshot{
			Berths: []BerthCand{berth("b1", 1200, 400, true)},
			Busy:   map[string][]NightRange{"b1": {{d("2027-07-04"), d("2027-07-09")}}},
			Areas:  []AreaCand{areaCand("a", 500)},
			AreaBusy: map[string]map[calendar.Date][]Interval{"a": {
				d("2027-07-01"): full, d("2027-07-02"): full, d("2027-07-03"): full,
			}},
		}
		r := req("2027-07-01", "2027-07-07")
		r.MaxSegments = 1
		sols := Solve(s, r, rules)
		if len(sols) == 0 {
			t.Fatal("no berth-then-area solution")
		}
		sol := sols[0]
		assertContiguous(t, sol, r)
		if len(sol.Segments) != 2 || sol.Segments[0].ResourceID != "b1" || sol.Segments[1].ResourceID != "a" ||
			sol.Segments[1].Placement == nil || !sol.Segments[1].First.Equal(d("2027-07-04")) {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("consider out", func(t *testing.T) {
		s := Snapshot{
			Berths: []BerthCand{berth("b1", 1200, 400, true)},
			Busy:   map[string][]NightRange{"b1": {{d("2027-01-01"), d("2027-12-31")}}},
			OutOK:  map[string]OutCand{"b1": {AllocationID: "al", ContractID: "c"}},
		}
		r := req("2027-07-01", "2027-07-04")
		if sols := Solve(s, r, rules); len(sols) != 0 {
			t.Fatalf("%v", keys(sols))
		}
		r.ConsiderOut = true
		sols := Solve(s, r, rules)
		if len(sols) != 1 || sols[0].RequiresRelease == nil || sols[0].RequiresRelease.AllocationID != "al" || sols[0].Score < PenaltyRequiresRelease {
			t.Fatalf("%+v", sols)
		}
	})
	t.Run("preferences", func(t *testing.T) {
		b1, b2 := berth("b1", 1200, 400, true), berth("b2", 1200, 400, true)
		b2.GroupIDs = []string{"g"}
		s := Snapshot{Berths: []BerthCand{b1, b2}}
		r := req("2027-07-01", "2027-07-02")
		r.Prefs.GroupIDs = []string{"g"}
		r.Prefs.PowerA = p64(32)
		sols := Solve(s, r, rules)
		if sols[0].Segments[0].ResourceID != "b2" || sols[1].Score-sols[0].Score != PenaltyPrefMiss {
			t.Fatalf("%v", keys(sols))
		}
	})
	t.Run("limit", func(t *testing.T) {
		var bs []BerthCand
		for i := range 30 {
			bs = append(bs, berth("b"+strconv.Itoa(100+i), 1200, 400, true))
		}
		r := req("2027-07-01", "2027-07-02")
		if n := len(Solve(Snapshot{Berths: bs}, r, rules)); n != DefaultLimit {
			t.Fatalf("%d solutions", n)
		}
		r.Limit = 5
		if n := len(Solve(Snapshot{Berths: bs}, r, rules)); n != 5 {
			t.Fatalf("%d solutions", n)
		}
	})
	t.Run("land", func(t *testing.T) {
		s := Snapshot{Lands: []LandCand{{ID: "p1", Code: "P1", Target: fit.Target{Kind: fit.TargetLand, LandKind: "parking", Length: 500, Width: 250}}}}
		r := req("2027-07-01", "2027-07-02")
		r.ResourceTypes = []ResourceType{ResourceLand}
		if sols := Solve(s, r, rules); len(sols) != 1 {
			t.Fatalf("%v", keys(sols))
		}
	})
}

func assertContiguous(t interface {
	Helper()
	Fatalf(string, ...any)
}, sol Solution, r Request,
) {
	t.Helper()
	next := r.Arrival
	for _, sg := range sol.Segments {
		if !sg.First.Equal(next) || sg.Last.Before(sg.First) {
			t.Fatalf("not contiguous: %s", sol.Key)
		}
		next = sg.Last.AddDays(1)
	}
	if !next.Equal(r.Departure) {
		t.Fatalf("does not cover the stay: %s", sol.Key)
	}
}

// Every solution's plan is disjoint from the snapshot's busy rows and passes fit.
func TestRapid_SolutionsAreFreeAndFit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := d("2027-07-01")
		s := Snapshot{Busy: map[string][]NightRange{}, AreaBusy: map[string]map[calendar.Date][]Interval{}}
		for i := range rapid.IntRange(1, 6).Draw(t, "berths") {
			id := "b" + strconv.Itoa(i)
			s.Berths = append(s.Berths, berth(id, geo.Cm(rapid.IntRange(600, 2000).Draw(t, "maxloa")), geo.Cm(rapid.IntRange(200, 600).Draw(t, "maxbeam")), rapid.Bool().Draw(t, "ded")))
			s.Busy[id] = genRanges(t, base, base.AddDays(12))
		}
		a := areaCand("a", geo.Cm(rapid.IntRange(10, 60).Draw(t, "alen"))*100)
		s.Areas = []AreaCand{a}
		s.AreaBusy["a"] = map[calendar.Date][]Interval{}
		n := int(a.LengthCm / a.CellCm)
		for k := range 12 {
			for range rapid.IntRange(0, 3).Draw(t, "ivs") {
				f := rapid.IntRange(0, n-1).Draw(t, "f")
				s.AreaBusy["a"][base.AddDays(k)] = append(s.AreaBusy["a"][base.AddDays(k)], Interval{f, min(n-1, f+rapid.IntRange(0, 12).Draw(t, "w"))})
			}
		}
		r := req("2027-07-01", "2027-07-01")
		r.Arrival = base.AddDays(rapid.IntRange(0, 5).Draw(t, "arr"))
		r.Departure = r.Arrival.AddDays(rapid.IntRange(1, 6).Draw(t, "nights"))
		r.Vessel.LOA = geo.Cm(rapid.IntRange(500, 1800).Draw(t, "loa"))
		r.Vessel.Beam = geo.Cm(rapid.IntRange(150, 550).Draw(t, "beam"))
		r.MaxSegments = rapid.IntRange(0, 2).Draw(t, "segs")
		for _, sol := range Solve(s, r, rules) {
			assertContiguous(t, sol, r)
			for _, sg := range sol.Segments {
				switch sg.ResourceType {
				case ResourceBerth:
					var b BerthCand
					for _, x := range s.Berths {
						if x.ID == sg.ResourceID {
							b = x
						}
					}
					if !fit.Check(r.Vessel, b.Target, r.Departure.AddDays(-1), rules).OK {
						t.Fatalf("%s does not fit", sol.Key)
					}
					for _, bz := range s.Busy[sg.ResourceID] {
						if !bz.To.Before(sg.First) && !bz.From.After(sg.Last) {
							t.Fatalf("%s overlaps busy %v", sol.Key, bz)
						}
					}
				case ResourceArea:
					la := a.LinearArea()
					cells, err := Cells(AllocSpec{ResourceType: ResourceArea, Placement: sg.Placement}, &la, layout.Vessel{LOA: r.Vessel.LOA, Beam: r.Vessel.Beam})
					if err != nil {
						t.Fatalf("%s: %v", sol.Key, err)
					}
					for night := sg.First; !night.After(sg.Last); night = night.AddDays(1) {
						for _, iv := range s.AreaBusy["a"][night] {
							for _, c := range cells {
								if c >= iv.First && c <= iv.Last {
									t.Fatalf("%s takes busy cell %d on %s", sol.Key, c, night)
								}
							}
						}
					}
				}
			}
		}
	})
}

func TestCompact_Scenario(t *testing.T) {
	a := linear(3000, 50, 50)
	got := Compact(a, []Placed{
		{AllocationID: "x", X: 1000, Width: 450},
		{AllocationID: "y", X: 1700, Width: 400, Fixed: true},
		{AllocationID: "z", X: 2400, Width: 420},
		{AllocationID: "w", X: 30, Width: 300},
	}, 0)
	// w stays (already first at margin/2 = 30); x goes after w (cells 0..6 → x = 350); y fixed; z after y (2100)
	want := []Proposal{{AllocationID: "x", X: 350}, {AllocationID: "z", X: 2100}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := Compact(a, nil, 0); got != nil {
		t.Fatalf("empty: %+v", got)
	}
	if got := Compact(layout.Area{Kind: layout.AreaSurface, Cell: 100}, []Placed{{AllocationID: "x", X: 500, Width: 100}}, 0); got != nil {
		t.Fatalf("surface: %+v", got)
	}
}

func TestRapid_CompactKeepsOrderAndFrees(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := linear(geo.Cm(rapid.IntRange(20, 80).Draw(t, "len"))*100, geo.Cm(rapid.SampledFrom([]int{10, 50, 100}).Draw(t, "cell")), 50)
		var ps []Placed
		cursor := geo.Cm(30)
		for i := range rapid.IntRange(0, 8).Draw(t, "n") {
			x := snapUp(cursor + geo.Cm(rapid.IntRange(0, 400).Draw(t, "gap")))
			w := geo.Cm(rapid.IntRange(150, 600).Draw(t, "w"))
			if x+w > a.Length {
				break
			}
			ps = append(ps, Placed{AllocationID: "a" + strconv.Itoa(i), X: x, Width: w, Fixed: rapid.IntRange(0, 4).Draw(t, "fixed") == 0})
			cursor = (x + w + a.Cell - 1) / a.Cell * a.Cell
		}
		props := Compact(a, ps, 0)
		pos := map[string]geo.Cm{}
		for _, p := range ps {
			pos[p.AllocationID] = p.X
		}
		for _, pr := range props {
			if pr.X >= pos[pr.AllocationID] {
				t.Fatalf("%s did not move left", pr.AllocationID)
			}
			pos[pr.AllocationID] = pr.X
		}
		after := slices.Clone(ps)
		for i := range after {
			after[i].X = pos[after[i].AllocationID]
		}
		v := func(p Placed) layout.Vessel { return layout.Vessel{LOA: 1000, Beam: p.Width - a.Margin} }
		prevLast := -1
		for _, p := range after {
			c, err := layout.Footprint(v(p), layout.Placement{X: p.X}, a)
			if err != nil {
				t.Fatalf("%s at %d: %v", p.AllocationID, p.X, err)
			}
			if c.First <= prevLast {
				t.Fatalf("order not kept or cells shared at %s", p.AllocationID)
			}
			prevLast = c.Last
		}
		if FreeTail(a, after) < FreeTail(a, ps) {
			t.Fatal("compaction freed less space at the end")
		}
	})
}

func TestAnomalies_Scenario(t *testing.T) {
	date := d("2027-07-10")
	in := AnomalyInput{Date: date, NowMinutes: 17 * 60, GraceMinutes: 120, Elements: []AnomalyElement{
		{ResourceID: "r1", PresenceInPort: true},
		{ResourceID: "r2", AllocationID: "a2", HasAllocation: true, ContractKind: "transit", ContractStatus: "confirmed", Arrival: date, Departure: date.AddDays(2), ExpectedArrivalMins: 14 * 60, InsuranceOK: true},
		{ResourceID: "r3", AllocationID: "a3", HasAllocation: true, ContractKind: "transit", ContractStatus: "confirmed", Arrival: date, Departure: date.AddDays(2), ExpectedArrivalMins: 15 * 60, InsuranceOK: true},
		{ResourceID: "r4", AllocationID: "a4", HasAllocation: true, ContractKind: "transit", ContractStatus: "confirmed", Arrival: date.AddDays(-1), Departure: date.AddDays(2), ExpectedArrivalMins: 23 * 60, InsuranceOK: true},
		{ResourceID: "r5", AllocationID: "a5", PresenceInPort: true, ContractKind: "transit", ContractStatus: "arrived", Arrival: date.AddDays(-3), Departure: date.AddDays(-1), InsuranceOK: true},
		{ResourceID: "r6", AllocationID: "a6", HasAllocation: true, FitBlocking: true, ContractKind: "mooring", ContractStatus: "active"},
		{ResourceID: "r7", AllocationID: "a7", HasAllocation: true, ContractKind: "mooring", ContractStatus: "draft", HoldLost: true, InsuranceOK: true},
		{ResourceID: "r8", AllocationID: "a8", HasAllocation: true, ContractKind: "transit", ContractStatus: "confirmed", Arrival: date, Departure: date.AddDays(1), ExpectedArrivalMins: 14 * 60, ArrivedPresence: true, InsuranceOK: true},
	}}
	got := Anomalies(in)
	want := []Anomaly{
		{ResourceID: "r1", Code: AnomalyPresenceWithoutAllocation},
		{ResourceID: "r2", AllocationID: "a2", Code: AnomalyNotArrived},
		{ResourceID: "r4", AllocationID: "a4", Code: AnomalyNotArrived},
		{ResourceID: "r5", AllocationID: "a5", Code: AnomalyDepartureOverdue},
		{ResourceID: "r6", AllocationID: "a6", Code: AnomalyInsuranceExpiredActive},
		{ResourceID: "r6", AllocationID: "a6", Code: AnomalyVesselExceedsLimits},
		{ResourceID: "r7", AllocationID: "a7", Code: AnomalyHoldLostDraft},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestEngine_Purity(t *testing.T) {
	allowed := map[string]bool{
		"github.com/coste360/porto/internal/geo": true, "github.com/coste360/porto/internal/calendar": true,
		"github.com/coste360/porto/internal/domain/fit": true, "github.com/coste360/porto/internal/domain/layout": true,
	}
	forbidden := map[string]bool{"time": true, "math/rand": true, "math/rand/v2": true, "crypto/rand": true, "os": true, "net": true, "net/http": true, "database/sql": true, "io/fs": true}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if (strings.Contains(path, ".") && !allowed[path]) || forbidden[path] {
				t.Errorf("%s imports %q", name, path)
			}
		}
	}
}
