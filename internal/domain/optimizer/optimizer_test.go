package optimizer_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/optimizer/internal/fixture"
	"github.com/coste360/porto/internal/geo"
)

var day0 = calendar.MustParse("2027-01-10")

// rectArea is a cols × rows surface of 100 cm cells, all usable except blocked.
func rectArea(cols, rows int, margin geo.Cm, blocked ...int) layout.Area {
	a := layout.Area{Kind: layout.AreaSurface, Cell: 100, Margin: margin, Cols: cols, Rows: rows, Usable: make([]bool, cols*rows)}
	for i := range a.Usable {
		a.Usable[i] = true
	}
	for _, b := range blocked {
		a.Usable[b] = false
		a.Blocked = append(a.Blocked, b)
	}
	slices.Sort(a.Blocked)
	return a
}

// leftDoor returns the exit cells of column 0, rows r0..r1.
func leftDoor(cols, r0, r1 int) []int {
	var ex []int
	for r := r0; r <= r1; r++ {
		ex = append(ex, r*cols)
	}
	return ex
}

func at(x, y geo.Cm, rot geo.Angle) layout.Placement {
	return layout.Placement{X: x * 100, Y: y * 100, Rotation: rot}
}

func placed(id string, loa, beam geo.Cm, p layout.Placement) optimizer.Placed {
	return optimizer.Placed{ID: id, LOA: loa, Beam: beam, At: p, First: day0.AddDays(-30), Last: day0.AddDays(-1), ExitOn: day0}
}

func TestGrid_Scenario(t *testing.T) {
	a := rectArea(10, 8, 0, 3*10+5)
	g := optimizer.NewGrid(a, []int{2 * 10, 3 * 10, 4 * 10, 5, 6, 99, 6})
	if len(g.Gates) != 2 || !slices.Equal(g.Gates[0], []int{5, 6}) || !slices.Equal(g.Gates[1], []int{20, 30, 40}) {
		t.Fatalf("gates %v", g.Gates)
	}
	if g.Dist[20] != 0 || g.Dist[21] != 1 || g.Dist[35] != -1 || g.Coarse != 1 {
		t.Fatalf("dist %d %d %d coarse %d", g.Dist[20], g.Dist[21], g.Dist[35], g.Coarse)
	}
	if issues := optimizer.ValidateGates(g); len(issues) != 0 {
		t.Fatalf("issues %v", issues)
	}
	bad := optimizer.NewGrid(a, []int{3*10 + 5, 4*10 + 4})
	issues := optimizer.ValidateGates(bad)
	if len(issues) != 2 || issues[0].Reason != optimizer.GateNotUsable || issues[1].Reason != optimizer.GateNotOnBorder {
		t.Fatalf("issues %v", issues)
	}
	big := rectArea(300, 200, 0)
	if g := optimizer.NewGrid(big, nil); g.Coarse != 2 {
		t.Fatalf("coarse %d", g.Coarse)
	}
}

func access(t *testing.T, g optimizer.Grid, present []optimizer.Placed, exiting ...string) map[string]optimizer.Access {
	t.Helper()
	out := map[string]optimizer.Access{}
	for _, a := range optimizer.Accessibility(g, present, exiting, day0) {
		out[a.ID] = a
	}
	return out
}

func TestAccessibility_Scenario(t *testing.T) {
	// A 3-cell door on the left; margin 60: beam 240 needs 3 cells, beam 241 four.
	g := optimizer.NewGrid(rectArea(20, 10, 60), leftDoor(20, 3, 5))
	if a := access(t, g, []optimizer.Placed{placed("a", 440, 240, at(10, 3, 0))}, "a")["a"]; !a.Accessible || a.Order != 1 {
		t.Fatalf("door = beam + margin must pass: %+v", a)
	}
	if a := access(t, g, []optimizer.Placed{placed("a", 440, 241, at(10, 3, 0))}, "a")["a"]; a.Accessible {
		t.Fatalf("door narrower than beam + margin must fail: %+v", a)
	}

	// Corridor of rows 3..5 between walls: b sits in front of a; c is elsewhere.
	var walls []int
	for c := 2; c < 20; c++ {
		walls = append(walls, 2*20+c, 6*20+c)
	}
	cg := optimizer.NewGrid(rectArea(20, 10, 0, walls...), leftDoor(20, 3, 5))
	boats := []optimizer.Placed{
		placed("a", 400, 300, at(12, 3, 0)), placed("b", 400, 300, at(4, 3, 0)), placed("c", 300, 100, at(10, 8, 0)),
	}
	res := access(t, cg, boats, "a")
	if res["a"].Accessible || !slices.Equal(res["a"].Blocking, []string{"b"}) {
		t.Fatalf("a must be blocked by b only: %+v", res["a"])
	}
	// Same day: b leaves first, then a.
	res = access(t, cg, boats, "a", "b")
	if !res["a"].Accessible || !res["b"].Accessible || res["b"].Order != 1 || res["a"].Order != 2 {
		t.Fatalf("extraction order: %+v", res)
	}

	// Rotation in place: a boat lying across (rotation 900) turns in the open area to leave by a narrow door.
	og := optimizer.NewGrid(rectArea(20, 20, 0), leftDoor(20, 8, 10))
	if a := access(t, og, []optimizer.Placed{placed("r", 800, 200, at(10, 5, 900))}, "r")["r"]; !a.Accessible {
		t.Fatalf("rotation in place: %+v", a)
	}
	// In a 2-cell wide vertical slot it cannot turn.
	var slot []int
	for r := range 20 {
		for c := 0; c < 20; c++ {
			if r < 15 && (c == 9 || c == 12) {
				slot = append(slot, r*20+c)
			}
		}
	}
	sg := optimizer.NewGrid(rectArea(20, 20, 0, slot...), leftDoor(20, 16, 18))
	if a := access(t, sg, []optimizer.Placed{placed("r", 800, 200, at(10, 0, 900))}, "r")["r"]; a.Accessible {
		t.Fatalf("no room to turn: %+v", a)
	}
}

func TestAccessibilityTimeline_Scenario(t *testing.T) {
	var walls []int
	for c := 2; c < 20; c++ {
		walls = append(walls, 2*20+c, 6*20+c)
	}
	g := optimizer.NewGrid(rectArea(20, 10, 0, walls...), leftDoor(20, 3, 5))
	item := func(id string, x geo.Cm, arrive, exit int) optimizer.TimelineItem {
		return optimizer.TimelineItem{Placed: optimizer.Placed{
			ID: id, LOA: 400, Beam: 300, At: at(x, 3, 0), First: day0.AddDays(arrive), Last: day0.AddDays(exit - 1), ExitOn: day0.AddDays(exit),
		}, ArrivalOn: day0.AddDays(arrive)}
	}
	// Deep boat a leaves on day 10 while b (in front) stays until day 20: a's exit is blocked by b. b arrived
	// after a: both arrivals are fine.
	acc := optimizer.AccessibilityTimeline(g, nil, []optimizer.TimelineItem{item("a", 12, 0, 10), item("b", 4, 1, 20)}, day0, day0.AddDays(30))
	var got []string
	for _, a := range acc {
		got = append(got, a.Event+":"+a.ID+":"+strconv.FormatBool(a.Accessible)+":"+strings.Join(a.Blocking, ","))
	}
	want := []string{"arrival:a:true:", "arrival:b:true:", "exit:a:false:b", "exit:b:true:"}
	if !slices.Equal(got, want) {
		t.Fatalf("timeline %v", got)
	}
	// An obstacle in the corridor blocks the exit of b.
	obst := []optimizer.Obstacle{{ID: "blk", Cells: []int{4*20 + 1}, First: day0.AddDays(15), Last: day0.AddDays(25)}}
	acc = optimizer.AccessibilityTimeline(g, obst, []optimizer.TimelineItem{item("b", 4, 1, 20)}, day0, day0.AddDays(30))
	if acc[len(acc)-1].Event != optimizer.EventExit || acc[len(acc)-1].Accessible {
		t.Fatalf("obstacle: %+v", acc)
	}
	// A block starting on the exit day in front of the door does not block the exit: exits come before the new
	// rows of the day (m9-plan §5.1, D7). An arrival on that day is blocked by it.
	gate := []optimizer.Obstacle{{ID: "blk", Cells: []int{4*20 + 1}, First: day0.AddDays(20), Last: day0.AddDays(25)}}
	acc = optimizer.AccessibilityTimeline(g, gate, []optimizer.TimelineItem{item("b", 4, 1, 20), item("c", 4, 20, 30)}, day0, day0.AddDays(30))
	for _, a := range acc {
		switch {
		case a.ID == "b" && a.Event == optimizer.EventExit && !a.Accessible:
			t.Fatalf("exit blocked by a block starting on the exit day: %+v", acc)
		case a.ID == "c" && a.Event == optimizer.EventArrival && a.Accessible:
			t.Fatalf("arrival through a block present that night: %+v", acc)
		}
	}
}

// genBoats places random non-overlapping boats on a rectangle grid.
func genBoats(t *rapid.T, cols, rows int) []optimizer.Placed {
	n := rapid.IntRange(1, 6).Draw(t, "n")
	var out []optimizer.Placed
	type rect struct{ c, r, w, h int }
	var rs []rect
	for k := range n {
		w := rapid.IntRange(2, 5).Draw(t, "w")
		h := rapid.IntRange(1, 3).Draw(t, "h")
		c := rapid.IntRange(0, cols-w).Draw(t, "c")
		r := rapid.IntRange(0, rows-h).Draw(t, "r")
		ok := true
		for _, o := range rs {
			if c < o.c+o.w && o.c < c+w && r < o.r+o.h && o.r < r+h {
				ok = false
			}
		}
		if !ok {
			continue
		}
		rs = append(rs, rect{c, r, w, h})
		out = append(out, placed(strconv.Itoa(k), geo.Cm(w)*100, geo.Cm(h)*100, at(geo.Cm(c), geo.Cm(r), 0)))
	}
	return out
}

// Removing a present boat never makes another boat inaccessible.
func TestRapid_AccessibilityMonotone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cols, rows := rapid.IntRange(6, 14).Draw(t, "cols"), rapid.IntRange(6, 12).Draw(t, "rows")
		r0 := rapid.IntRange(0, rows-1).Draw(t, "door")
		g := optimizer.NewGrid(rectArea(cols, rows, 0), leftDoor(cols, r0, min(rows-1, r0+rapid.IntRange(0, 4).Draw(t, "dw"))))
		boats := genBoats(t, cols, rows)
		if len(boats) < 2 {
			return
		}
		target := boats[0].ID
		before := optimizer.Accessibility(g, boats, []string{target}, day0)[0].Accessible
		k := rapid.IntRange(1, len(boats)-1).Draw(t, "remove")
		fewer := slices.Delete(slices.Clone(boats), k, k+1)
		after := optimizer.Accessibility(g, fewer, []string{target}, day0)[0].Accessible
		if before && !after {
			t.Fatalf("removing %s made %s inaccessible", boats[k].ID, target)
		}
	})
}

// genInput draws a small random arrange input: a grid with blocked cells and a door, boats with stays, some with a
// current placement, one pinned when its current placement is valid, some failing fit, and an obstacle.
func genInput(t *rapid.T) optimizer.Input {
	cols, rows := rapid.IntRange(8, 18).Draw(t, "cols"), rapid.IntRange(6, 14).Draw(t, "rows")
	var blocked []int
	for range rapid.IntRange(0, 6).Draw(t, "blocked") {
		blocked = append(blocked, rapid.IntRange(cols, cols*rows-1).Draw(t, "cell"))
	}
	slices.Sort(blocked)
	blocked = slices.Compact(blocked)
	a := rectArea(cols, rows, 60, blocked...)
	r0 := rapid.IntRange(0, rows-3).Draw(t, "door")
	g := optimizer.NewGrid(a, leftDoor(cols, r0, r0+2))
	n := rapid.IntRange(1, 10).Draw(t, "items")
	in := optimizer.Input{
		Grid: g, Seed: rapid.Uint64().Draw(t, "seed"), MaxIter: rapid.IntRange(0, 40).Draw(t, "iter"),
		Fit: optimizer.FitParams{Target: fit.Target{Kind: fit.TargetSurfaceArea, MaxLOA: 700, MultihullOK: true}},
	}
	for k := range n {
		first := day0.AddDays(rapid.IntRange(0, 20).Draw(t, "first"))
		last := first.AddDays(rapid.IntRange(0, 30).Draw(t, "len"))
		loa := geo.Cm(rapid.IntRange(150, 800).Draw(t, "loa"))
		beam := geo.Cm(rapid.IntRange(100, 300).Draw(t, "beam"))
		it := optimizer.Item{
			ID: "i" + strconv.Itoa(k), LOA: loa, Beam: beam, Fit: fit.Vessel{LOA: loa, Beam: beam, Hull: "rib"},
			First: first, Last: last, ArrivalOn: first, ExitOn: last.AddDays(1),
		}
		if rapid.Bool().Draw(t, "current") {
			p := at(geo.Cm(rapid.IntRange(0, cols-1).Draw(t, "x")), geo.Cm(rapid.IntRange(0, rows-1).Draw(t, "y")), geo.Angle(900*rapid.IntRange(0, 3).Draw(t, "rot")))
			it.Current = &p
		}
		in.Items = append(in.Items, it)
	}
	if rapid.Bool().Draw(t, "pin") && in.Items[0].Current != nil {
		in.Items[0].Pinned = true
	}
	if rapid.Bool().Draw(t, "obstacle") {
		c := rapid.IntRange(0, cols*rows-1).Draw(t, "obst")
		in.Fixed = []optimizer.Obstacle{{ID: "o", Cells: []int{c}, First: day0, Last: day0.AddDays(40)}}
	}
	in.From, in.To = day0, day0.AddDays(60)
	return in
}

func TestRapid_ArrangeValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t)
		res, err := optimizer.Arrange(context.Background(), in)
		if errors.Is(err, optimizer.ErrPinnedInvalid) {
			return
		}
		if err != nil {
			t.Fatalf("arrange: %v", err)
		}
		if err := optimizer.Validate(in, res); err != nil {
			t.Fatalf("validate: %v", err)
		}
		for i, p := range res.Placements {
			if in.Items[i].LOA > 700 && p.At != nil {
				t.Fatalf("%s fails fit but is placed", p.ID)
			}
			if in.Items[i].LOA > 700 && p.UnplacedReason != optimizer.UnplacedFit {
				t.Fatalf("%s: reason %q", p.ID, p.UnplacedReason)
			}
			if in.Items[i].Pinned && *p.At != *in.Items[i].Current {
				t.Fatalf("pinned %s moved", p.ID)
			}
		}
	})
}

// When the current layout is valid, the result is never worse than keeping it.
func TestRapid_ArrangeNeverWorseThanCurrent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t)
		for i := range in.Items {
			in.Items[i].Pinned = false
		}
		first, err := optimizer.Arrange(context.Background(), in)
		if err != nil {
			t.Fatalf("arrange: %v", err)
		}
		cur := make([]*layout.Placement, len(in.Items))
		for i, p := range first.Placements {
			in.Items[i].Current = p.At
			cur[i] = p.At
		}
		want, err := optimizer.ScoreOf(in, cur)
		if err != nil {
			t.Fatal(err)
		}
		got, err := optimizer.Arrange(context.Background(), in)
		if err != nil {
			t.Fatalf("arrange: %v", err)
		}
		if want.Less(got.Score) {
			t.Fatalf("worse than current: %+v > %+v", got.Score, want)
		}
	})
}

func TestArrange_Deterministic(t *testing.T) {
	f := fixture.Generate(3)
	in := f.Input
	in.MaxIter = 300
	run := func(seed uint64) []byte {
		in.Seed = seed
		res, err := optimizer.Arrange(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if res.StoppedBy == optimizer.StoppedByDeadline {
			t.Fatal("no deadline expected")
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if a, b := run(7), run(7); string(a) != string(b) {
		t.Fatal("same seed and budget must give the same result")
	}
	_ = run(8) // a different seed may differ; it must still succeed
}

func TestPlaceOne_Scenario(t *testing.T) {
	var walls []int
	for c := 2; c < 20; c++ {
		walls = append(walls, 2*20+c, 6*20+c)
	}
	g := optimizer.NewGrid(rectArea(20, 10, 0, walls...), leftDoor(20, 3, 5))
	stay := func(id string, exit int, p *layout.Placement) optimizer.Item {
		return optimizer.Item{
			ID: id, LOA: 400, Beam: 300, Fit: fit.Vessel{LOA: 400, Beam: 300}, First: day0, Last: day0.AddDays(exit - 1),
			ArrivalOn: day0, ExitOn: day0.AddDays(exit), Current: p,
		}
	}
	// b leaves early from the corridor mouth; a new boat leaving late must not go in front of it.
	front := at(1, 3, 0)
	in := optimizer.Input{Grid: g, Items: []optimizer.Item{stay("b", 5, &front)}, From: day0, To: day0.AddDays(40)}
	p, err := optimizer.PlaceOne(in, stay("new", 30, nil))
	if err != nil || p == nil {
		t.Fatalf("place one: %v %v", p, err)
	}
	acc := optimizer.Accessibility(g, []optimizer.Placed{
		{ID: "b", LOA: 400, Beam: 300, At: front}, {ID: "new", LOA: 400, Beam: 300, At: *p},
	}, []string{"b"}, day0.AddDays(5))
	if !acc[0].Accessible {
		t.Fatalf("the new boat blocks b: %+v at %+v", acc, p)
	}
	// A full area has no position.
	in.Items = append(in.Items, stay("c", 50, ptr(at(5, 3, 0))), stay("d", 50, ptr(at(9, 3, 0))), stay("e", 50, ptr(at(13, 3, 0))))
	in.Items = append(in.Items, stay("f", 50, ptr(at(0, 7, 0))), stay("g", 50, ptr(at(4, 7, 0))), stay("h", 50, ptr(at(8, 7, 0))),
		stay("i", 50, ptr(at(12, 7, 0))), stay("j", 50, ptr(at(16, 7, 0))), stay("k", 50, ptr(at(16, 3, 0))))
	if p, err := optimizer.PlaceOne(in, stay("new", 30, nil)); err != nil || p != nil {
		t.Fatalf("full area: %v %v", p, err)
	}
}

func ptr(p layout.Placement) *layout.Placement { return &p }

func TestArrange_Errors(t *testing.T) {
	g := optimizer.NewGrid(rectArea(10, 10, 0), leftDoor(10, 0, 2))
	in := optimizer.Input{Grid: g}
	for i := range optimizer.MaxItems + 1 {
		in.Items = append(in.Items, optimizer.Item{ID: strconv.Itoa(i), LOA: 100, Beam: 100, First: day0, Last: day0})
	}
	if _, err := optimizer.Arrange(context.Background(), in); !errors.Is(err, optimizer.ErrTooManyItems) {
		t.Fatalf("too many: %v", err)
	}
	outside := at(9, 9, 0)
	in.Items = []optimizer.Item{{ID: "p", LOA: 300, Beam: 100, First: day0, Last: day0, Current: &outside, Pinned: true}}
	if _, err := optimizer.Arrange(context.Background(), in); !errors.Is(err, optimizer.ErrPinnedInvalid) {
		t.Fatalf("pinned: %v", err)
	}
}

func BenchmarkArrange_2000m2_40(b *testing.B) {
	in := fixture.Generate(1).Input
	in.MaxIter = 200
	for b.Loop() {
		if _, err := optimizer.Arrange(context.Background(), in); err != nil {
			b.Fatal(err)
		}
	}
}

// TestOptimizer_Purity checks the package stays pure (m9-plan §0.4): layout, fit, geo, calendar and the standard
// library only; no clock, no math/rand, no I/O, no float.
func TestOptimizer_Purity(t *testing.T) {
	allowed := map[string]bool{
		"github.com/coste360/porto/internal/domain/layout": true, "github.com/coste360/porto/internal/domain/fit": true,
		"github.com/coste360/porto/internal/geo": true, "github.com/coste360/porto/internal/calendar": true,
		"github.com/coste360/porto/internal/domain/optimizer": true,
	}
	forbidden := []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "os", "net", "net/http", "database/sql", "io/fs", "math"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob("internal/fixture/*.go")
	files = append(files, more...)
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if (strings.Contains(path, ".") && !allowed[path] && !strings.HasPrefix(path, "github.com/coste360/porto/internal/domain/optimizer/")) || slices.Contains(forbidden, path) {
				t.Errorf("%s imports %q", name, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "float32" || x.Name == "float64" {
					t.Errorf("%s uses %s", fset.Position(x.Pos()), x.Name)
				}
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "time" && x.Sel.Name == "Now" {
					t.Errorf("%s calls time.Now", fset.Position(x.Pos()))
				}
			}
			return true
		})
	}
}

// TestArrange_FitOverride: a boat accepted with a fit override (LOA above the area max) is placed and may be pinned;
// without the override it is unplaced with reason fit; support_not_allowed stays fatal even with the override.
func TestArrange_FitOverride(t *testing.T) {
	g := optimizer.NewGrid(rectArea(20, 10, 0), leftDoor(20, 3, 6))
	long := func(override bool) optimizer.Item {
		cur := at(5, 3, 0)
		return optimizer.Item{
			ID: "long", LOA: 900, Beam: 300, Fit: fit.Vessel{LOA: 900, Beam: 300}, First: day0, Last: day0.AddDays(30),
			ExitOn: day0.AddDays(31), Current: &cur, FitOverridden: override,
		}
	}
	in := optimizer.Input{
		Grid: g, From: day0, To: day0.AddDays(40), MaxIter: 100, Seed: 1,
		Fit: optimizer.FitParams{Target: fit.Target{Kind: fit.TargetSurfaceArea, MaxLOA: 800}},
	}
	in.Items = []optimizer.Item{long(false)}
	res, err := optimizer.Arrange(context.Background(), in)
	if err != nil || res.Placements[0].At != nil || res.Placements[0].UnplacedReason != "fit" {
		t.Fatalf("without override: %v %+v", err, res.Placements)
	}
	in.Items = []optimizer.Item{long(true)}
	res, err = optimizer.Arrange(context.Background(), in)
	if err != nil || res.Placements[0].At == nil {
		t.Fatalf("with override: %v %+v", err, res.Placements)
	}
	if err := optimizer.Validate(in, res); err != nil {
		t.Fatalf("validate: %v", err)
	}
	pinned := long(true)
	pinned.Pinned = true
	in.Items = []optimizer.Item{pinned}
	if _, err := optimizer.Arrange(context.Background(), in); err != nil {
		t.Fatalf("pinned with override: %v", err)
	}
	in.Fit.Target.Supports = []string{"cradle"}
	pinned.Support = "trailer"
	in.Items = []optimizer.Item{pinned}
	if _, err := optimizer.Arrange(context.Background(), in); !errors.Is(err, optimizer.ErrPinnedInvalid) {
		t.Fatalf("support not allowed must stay fatal: %v", err)
	}
}
