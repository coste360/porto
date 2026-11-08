//go:build integration

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// threeBoats puts three 8 m × 3 m boats in the shed: a at the far wall leaving first (its exit is blocked by b,
// in front of it in the door rows), b in front of the door leaving last, c in the back corner.
func (e *senv) threeBoats(t *testing.T) (a, b, c *ContractView) {
	t.Helper()
	a = e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-05", "2028-05-01", pl(12, 3, 0), "2028-03-01")
	b = e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-05", "2028-05-01", pl(1, 3, 0), "2028-04-15")
	c = e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-05", "2028-05-01", pl(12, 0, 0), "2028-04-20")
	return a, b, c
}

func (e *senv) newScenario(t *testing.T, name string) *ScenarioDetail {
	t.Helper()
	sd, err := e.svc.CreateScenario(fixture.Ctx(), e.Owner, e.MarinaID, ScenarioInput{AreaID: e.shed, Name: name, From: d("2027-10-05"), To: d("2028-04-30")})
	if err != nil {
		t.Fatalf("create scenario: %v", err)
	}
	return sd
}

func TestScenario_NoRealWrites(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		a, b, _ := e.threeBoats(t)
		before := e.state(t)
		sd := e.newScenario(t, "Inverno")
		if sd.Indicators.ExitsBlocked != 1 || len(sd.Items) != 3 || len(sd.Moves) != 0 {
			t.Fatalf("indicators %+v items %d moves %d", sd.Indicators, len(sd.Items), len(sd.Moves))
		}
		// Move b (leaving last) away from the door in the working copy: one move, the real state untouched.
		sd, err := e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
			{AllocationID: b.Placement.ID, At: pl(12, 7, 0)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(sd.Moves) != 1 || sd.Indicators.Moves != 1 || sd.Indicators.ExitsBlocked != 0 {
			t.Fatalf("after save: moves %d %+v", len(sd.Moves), sd.Indicators)
		}
		sd, err = e.svc.Optimize(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), nil, 200)
		if err != nil || sd.Run == nil {
			t.Fatalf("optimize: %v", err)
		}
		if sd.Indicators.Unplaced != 0 || sd.Indicators.ExitsBlocked != 0 {
			t.Fatalf("optimized %+v", sd.Indicators)
		}
		if _, err := e.svc.Compare(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, "real", nil); err != nil {
			t.Fatal(err)
		}
		other := e.newScenario(t, "Altra")
		cmp, err := e.svc.Compare(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, other.Scenario.ID, nil)
		if err != nil || cmp.Right.ExitsBlocked != 1 {
			t.Fatalf("compare: %v %+v", err, cmp)
		}
		if _, err := e.svc.Arrange(ctx, e.Owner, e.MarinaID, e.shed, ArrangeInput{From: ptr(d("2027-10-05")), To: ptr(d("2028-04-30")), MaxIter: 100}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Evaluate(ctx, e.Owner, e.MarinaID, e.shed, nil, nil, []LayoutItem{{AllocationID: a.Placement.ID, At: pl(1, 7, 0)}}); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.DiscardScenario(ctx, e.Owner, e.MarinaID, other.Scenario.ID, int64(other.Scenario.Version)); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.DiscardScenario(ctx, e.Owner, e.MarinaID, other.Scenario.ID, 0); !errors.Is(err, ErrScenarioNotDraft) {
			t.Fatalf("discard twice: %v", err)
		}
		if after := e.state(t); after != before {
			t.Fatalf("real state changed:\n%s\n%s", before, after)
		}
		list, _, err := e.svc.ListScenarios(ctx, e.Owner, e.MarinaID, ScenarioFilter{AreaID: e.shed})
		if err != nil || len(list) != 2 {
			t.Fatalf("list: %v %d", err, len(list))
		}
	})
}

func TestScenario_StaleOnVersionChange(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		_, _, c := e.threeBoats(t)
		sd := e.newScenario(t, "Inverno")
		if sd.Indicators.Stale {
			t.Fatal("fresh scenario stale")
		}
		// A desk move of c from the start night makes the copy stale.
		if _, err := e.occ.Move(ctx, e.Owner, e.MarinaID, occupancy.MoveInput{
			AllocationID: c.Placement.ID, Version: c.Placement.Version,
			Target: occupancy.Target{ResourceType: engine.ResourceArea, ResourceID: e.shed, Placement: &engine.Placement{X: 1200, Y: 700}},
		}); err != nil {
			t.Fatalf("move: %v", err)
		}
		got, err := e.svc.GetScenario(ctx, e.Owner, e.MarinaID, sd.Scenario.ID)
		if err != nil || !got.Indicators.Stale {
			t.Fatalf("stale: %v %+v", err, got.Indicators)
		}
		_, err = e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version))
		var ce *ConflictsError
		if !errors.As(err, &ce) || ce.Conflicts[0].Reason != ConflictStale {
			t.Fatalf("apply stale: %v", err)
		}
	})
}

func TestScenarioApply_MovementsOrder(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		a, b, c := e.threeBoats(t)
		sd := e.newScenario(t, "Swap")
		// a and b exchange places: a cycle, broken by a temporary position; c moves to b's free row first.
		sd, err := e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
			{AllocationID: a.Placement.ID, At: pl(1, 3, 0)},
			{AllocationID: b.Placement.ID, At: pl(12, 3, 0)},
			{AllocationID: c.Placement.ID, At: pl(1, 7, 0)},
		})
		if err != nil {
			t.Fatal(err)
		}
		before := e.Events(t, "occupancy.moved")
		res, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version))
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if res.Scenario.Status != store.ScenarioApplied || len(res.Movements) != 3 {
			t.Fatalf("result %+v %d", res.Scenario.Status, len(res.Movements))
		}
		temps := 0
		for i, m := range res.Movements {
			if m.Seq != i+1 || m.Status != store.MovementPending || m.EquipmentHint != store.EquipmentHintCradleMover {
				t.Fatalf("movement %d %+v", i, m)
			}
			if m.TemporaryPosition {
				temps++
			}
		}
		if temps != 1 {
			t.Fatalf("temporary moves %d", temps)
		}
		if len(e.Events(t, "occupancy.moved")) != len(before)+1 || len(e.Events(t, "storage.scenario_applied")) != 1 {
			t.Fatal("events")
		}
		// Every item is at its target from the apply night.
		pv, err := e.svc.Plan(ctx, e.Owner, e.MarinaID, e.shed, ptr(d("2027-10-05")), ptr(d("2028-04-30")))
		if err != nil {
			t.Fatal(err)
		}
		at := map[string]int64{}
		for _, it := range pv.Snapshot.Items {
			if it.Allocation.FirstNight == d("2027-10-05") {
				at[it.Vessel.ID] = *it.Allocation.XCm*1000 + *it.Allocation.YCm
			}
		}
		if at[a.Contract.Row.VesselID] != 100*1000+300 || at[b.Contract.Row.VesselID] != 1200*1000+300 {
			t.Fatalf("positions %v", at)
		}
		e.checkInvariants(t)
		// Worklist: list and complete.
		day := d("2027-10-05")
		list, err := e.svc.ListMovements(ctx, e.Owner, e.MarinaID, MovementFilter{Date: &day})
		if err != nil || len(list.Items) != 3 {
			t.Fatalf("list %v", err)
		}
		m := list.Items[0]
		done, err := e.svc.CompleteMovement(ctx, e.Owner, e.MarinaID, m.ID, int64(m.Version))
		if err != nil || done.Status != store.MovementDone {
			t.Fatalf("done: %v", err)
		}
		if _, err := e.svc.CompleteMovement(ctx, e.Owner, e.MarinaID, m.ID, 0); !errors.Is(err, ErrMovementNotPending) {
			t.Fatalf("done twice: %v", err)
		}
		if _, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, 0); !errors.Is(err, ErrScenarioNotDraft) {
			t.Fatalf("apply twice: %v", err)
		}
	})
}

func TestScenarioApply_ConflictListComplete(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		a, b, c := e.threeBoats(t)
		sd := e.newScenario(t, "Conflitti")
		// Plant three conflicts: a and b overlap in the copy, c is unplaced, and a block lies under b's target.
		sd, err := e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
			{AllocationID: a.Placement.ID, At: pl(5, 7, 0)},
			{AllocationID: b.Placement.ID, At: pl(8, 7, 0)},
			{AllocationID: c.Placement.ID, At: nil},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.occ.CreateBlock(ctx, e.Owner, e.MarinaID, occupancy.BlockInput{
			Target: occupancy.Target{ResourceType: engine.ResourceArea, ResourceID: e.shed, Placement: &engine.Placement{X: 1500, Y: 700}},
			From:   d("2027-10-05"), To: d("2027-10-20"), Reason: "pittura", LOA: 300, Beam: 200,
		}); err != nil {
			t.Fatalf("block: %v", err)
		}
		before := e.state(t)
		audits := e.AuditCount(t, "storage.scenario_apply")
		_, err = e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version))
		var ce *ConflictsError
		if !errors.As(err, &ce) {
			t.Fatalf("apply: %v", err)
		}
		reasons := map[string]string{}
		for _, cf := range ce.Conflicts {
			reasons[cf.AllocationID+":"+cf.Reason] = cf.Reason
		}
		for _, want := range []string{a.Placement.ID + ":overlap", b.Placement.ID + ":overlap", c.Placement.ID + ":unplaced", b.Placement.ID + ":resource_taken"} {
			if _, ok := reasons[want]; !ok {
				t.Fatalf("missing %s in %+v", want, ce.Conflicts)
			}
		}
		if e.state(t) != before || e.AuditCount(t, "storage.scenario_apply") != audits {
			t.Fatal("a conflicting apply wrote something")
		}
		got, err := e.svc.GetScenario(ctx, e.Owner, e.MarinaID, sd.Scenario.ID)
		if err != nil || got.Scenario.Status != store.ScenarioDraft {
			t.Fatalf("scenario %v", err)
		}
	})
}

// faultTrigger installs a test-only trigger that fails the k-th insert into the tables an apply writes (m9-plan
// §14.3, D16): no production hook.
func faultTrigger(t *testing.T, db *store.DB) (arm func(k int), drop func()) {
	t.Helper()
	ctx := context.Background()
	tables := []string{"cell_occupancies", "allocations", "storage_movements", "audit_log"}
	exec := func(q string) {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("CREATE TABLE test_fault_counter (n INTEGER NOT NULL)")
	exec("INSERT INTO test_fault_counter (n) VALUES (-1)")
	if db.IsPostgres() {
		exec(`CREATE FUNCTION test_fault() RETURNS trigger AS $$
BEGIN
  UPDATE test_fault_counter SET n = n - 1 WHERE n > 0;
  IF (SELECT n FROM test_fault_counter) = 0 THEN
    RAISE EXCEPTION 'test fault';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql`)
		for _, tb := range tables {
			exec(fmt.Sprintf("CREATE TRIGGER test_fault_%s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION test_fault()", tb, tb))
		}
	} else {
		for _, tb := range tables {
			exec(fmt.Sprintf(`CREATE TRIGGER test_fault_%s BEFORE INSERT ON %s
BEGIN
  UPDATE test_fault_counter SET n = n - 1 WHERE n > 0;
  SELECT RAISE(ABORT, 'test fault') WHERE (SELECT n FROM test_fault_counter) = 0;
END`, tb, tb))
		}
	}
	arm = func(k int) { exec(fmt.Sprintf("UPDATE test_fault_counter SET n = %d", k)) }
	drop = func() {
		for _, tb := range tables {
			if db.IsPostgres() {
				exec(fmt.Sprintf("DROP TRIGGER test_fault_%s ON %s", tb, tb))
			} else {
				exec("DROP TRIGGER test_fault_" + tb)
			}
		}
		if db.IsPostgres() {
			exec("DROP FUNCTION test_fault()")
		}
		exec("DROP TABLE test_fault_counter")
	}
	return arm, drop
}

func nextFault(k int) int {
	if k < 20 {
		return k + 1
	}
	return k + 251
}

func (e *senv) fullSnapshot(t *testing.T, scenarioID string) string {
	t.Helper()
	ctx := context.Background()
	sc, err := store.GetScenario(ctx, e.DB.DB, e.scope(t), scenarioID)
	if err != nil {
		t.Fatal(err)
	}
	mv, err := e.DB.NewSelect().TableExpr("storage_movements").Where("marina_id = ?", e.MarinaID).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	au, err := e.DB.NewSelect().TableExpr("audit_log").Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := e.DB.NewSelect().TableExpr("outbox_events").Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s|%s %d|%d|%d|%d", e.state(t), sc.Status, sc.Version, mv, au, ob)
}

// TestScenarioApply_AllOrNothing fails the apply at every write (fault injection) and checks that nothing changed;
// finally the apply succeeds and every item is at its target (INV-06).
func TestScenarioApply_AllOrNothing(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		a, b, c := e.threeBoats(t)
		sd := e.newScenario(t, "Fault")
		sd, err := e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
			{AllocationID: a.Placement.ID, At: pl(1, 3, 0)},
			{AllocationID: b.Placement.ID, At: pl(12, 3, 0)},
			{AllocationID: c.Placement.ID, At: pl(1, 7, 0)},
		})
		if err != nil {
			t.Fatal(err)
		}
		arm, drop := faultTrigger(t, db)
		before := e.fullSnapshot(t, sd.Scenario.ID)
		failures := 0
		// Every write up to 20, then a prime step through the thousands of occupancy rows.
		for k := 1; ; k = nextFault(k) {
			arm(k)
			_, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version))
			if err == nil {
				break // k is beyond the writes of the apply
			}
			failures++
			if got := e.fullSnapshot(t, sd.Scenario.ID); got != before {
				t.Fatalf("fault %d left a partial apply:\n%s\n%s", k, before, got)
			}
			if k > 200000 {
				t.Fatal("never succeeds")
			}
		}
		arm(-1)
		drop()
		if failures < 20 {
			t.Fatalf("only %d writes failed", failures)
		}
		e.checkInvariants(t)
		got, err := e.svc.GetScenario(ctx, e.Owner, e.MarinaID, sd.Scenario.ID)
		if err != nil || got.Scenario.Status != store.ScenarioApplied {
			t.Fatalf("final: %v", err)
		}
	})
}

// 100 goroutines on one area: 40 apply the same scenario, 30 apply three other scenarios, 30 move boats at the
// desk. Every scenario moves the same allocations, so at most one applies overall and at most once; after the
// race the invariants hold and the applied scenario is applied.
func TestConcurrency_ScenarioApply(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		a, b, c := e.threeBoats(t)
		mk := func(name string, ax, bx, cy int64) *ScenarioDetail {
			sd := e.newScenario(t, name)
			sd, err := e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
				{AllocationID: a.Placement.ID, At: pl(ax, 7, 0)},
				{AllocationID: b.Placement.ID, At: pl(bx, 7, 0)},
				{AllocationID: c.Placement.ID, At: pl(12, cy, 0)},
			})
			if err != nil {
				t.Fatal(err)
			}
			return sd
		}
		type layout struct{ ax, bx, cy int64 }
		want := map[string]layout{}
		mkL := func(name string, l layout) *ScenarioDetail {
			sd := mk(name, l.ax, l.bx, l.cy)
			want[sd.Scenario.ID] = l
			return sd
		}
		main := mkL("Main", layout{1, 10, 0})
		others := []*ScenarioDetail{mkL("O1", layout{2, 11, 0}), mkL("O2", layout{1, 11, 0}), mkL("O3", layout{0, 10, 0})}
		// The desk moves start once the first apply has returned: they still race the other applies, and a
		// scenario always wins (the first apply cannot be made stale by a desk move).
		firstDone := make(chan struct{})
		var firstOnce sync.Once
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := map[string]int{}
		var unexpected []error
		record := func(id string, err error) {
			mu.Lock()
			defer mu.Unlock()
			var ce *ConflictsError
			switch {
			case err == nil:
				wins[id]++
			case errors.Is(err, ErrScenarioNotDraft), errors.As(err, &ce), errors.Is(err, ErrVersionMismatch):
			default:
				unexpected = append(unexpected, err)
			}
		}
		for i := range 100 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				switch {
				case i < 40:
					_, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, main.Scenario.ID, int64(main.Scenario.Version))
					record(main.Scenario.ID, err)
					firstOnce.Do(func() { close(firstDone) })
				case i < 70:
					o := others[i%3]
					_, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, o.Scenario.ID, int64(o.Scenario.Version))
					record(o.Scenario.ID, err)
					firstOnce.Do(func() { close(firstDone) })
				default:
					<-firstDone
					// Desk moves of c (they make the scenarios stale or take their target cells); refusals are fine.
					_, _ = e.occ.Move(ctx, e.Owner, e.MarinaID, occupancy.MoveInput{
						AllocationID: c.Placement.ID, Version: c.Placement.Version,
						Target: occupancy.Target{ResourceType: engine.ResourceArea, ResourceID: e.shed, Placement: &engine.Placement{X: 100, Y: 0}},
					})
				}
			}(i)
		}
		wg.Wait()
		if len(unexpected) > 0 {
			t.Fatalf("unexpected: %v", unexpected[0])
		}
		total := 0
		for id, n := range wins {
			if n > 1 {
				t.Fatalf("scenario %s applied %d times", id, n)
			}
			total += n
		}
		if total != 1 {
			t.Fatalf("%d scenarios applied, want exactly one", total)
		}
		e.checkInvariants(t) // cell rows = Footprint × nights of every allocation
		var winner string
		for id := range wins {
			winner = id
			got, err := e.svc.GetScenario(ctx, e.Owner, e.MarinaID, id)
			if err != nil || got.Scenario.Status != store.ScenarioApplied {
				t.Fatalf("winner %v", err)
			}
		}
		// The real layout is the winner's, whole: every boat at its scenario placement from the first night.
		l := want[winner]
		pv, err := e.svc.Plan(ctx, e.Owner, e.MarinaID, e.shed, ptr(d("2027-10-05")), ptr(d("2027-10-05")))
		if err != nil {
			t.Fatal(err)
		}
		at := map[string][2]int64{}
		for _, it := range pv.Snapshot.Items {
			if it.Allocation.XCm != nil && it.Allocation.YCm != nil {
				at[it.Vessel.ID] = [2]int64{*it.Allocation.XCm, *it.Allocation.YCm}
			}
		}
		for vid, w := range map[string][2]int64{a.Contract.Row.VesselID: {l.ax * 100, 700}, b.Contract.Row.VesselID: {l.bx * 100, 700}} {
			if at[vid] != w {
				t.Fatalf("winner %s: vessel %s at %v, want %v (all %v)", winner, vid, at[vid], w, at)
			}
		}
		// c keeps its place in every scenario (the apply does not touch it), so a later desk move may take it.
		if got := at[c.Contract.Row.VesselID]; got != [2]int64{1200, l.cy * 100} && got != [2]int64{100, 0} {
			t.Fatalf("c at %v", got)
		}
	})
}

// TestScenarioApply_TrayItemRepriced: a boat brought in from the tray (an uncovered yard, another price class) is
// repriced by the apply like a desk layout save (m9-plan §7.3 step 1): contract total and audit diff change.
func TestScenarioApply_TrayItemRepriced(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		e.occ.SetMovePricer(e.ctr)
		ctx := fixture.Ctx()
		yard := e.addShed(t, e.shedMp, "P1", 12, 8, 2, 5, store.Strings{"cradle", "trailer"}, 0)
		if _, err := e.DB.NewUpdate().Table("dynamic_areas").Set("price_class = ?", "yard").Where("id = ?", yard).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		ends, reprice := d("2028-05-01"), true
		cv, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: e.addVessel(t, 800, 300), StartsOn: d("2027-10-05"), EndsOn: &ends,
			AreaID: yard, Placement: pl(1, 2, 0), Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
			Policy: &contract.PolicyInput{RepriceOnMove: &reprice},
		})
		if err != nil {
			t.Fatalf("yard contract: %v", err)
		}
		oldTotal := cv.Contract.Row.TotalCents
		sd := e.newScenario(t, "Dal piazzale")
		cands, err := e.svc.TrayCandidates(ctx, e.Owner, e.MarinaID, sd.Scenario.ID)
		if err != nil || len(cands) != 1 || cands[0].Allocation.ID != cv.Placement.ID || cands[0].ResourceCode != "P1" || cands[0].Vessel == nil {
			t.Fatalf("tray candidates: %v %+v", err, cands)
		}
		sd, err = e.svc.AddTrayItem(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), cv.Placement.ID)
		if err != nil {
			t.Fatalf("tray: %v", err)
		}
		if cands, err := e.svc.TrayCandidates(ctx, e.Owner, e.MarinaID, sd.Scenario.ID); err != nil || len(cands) != 0 {
			t.Fatalf("tray candidates after add: %v %+v", err, cands)
		}
		sd, err = e.svc.SaveItems(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), []LayoutItem{
			{AllocationID: cv.Placement.ID, At: pl(12, 7, 0)},
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version))
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if res.PriceEffect == nil || !res.PriceEffect.Applied || res.PriceEffect.DeltaCents <= 0 {
			t.Fatalf("price effect %+v", res.PriceEffect)
		}
		got, err := e.ctr.GetContract(ctx, e.Owner, e.MarinaID, cv.Contract.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Row.TotalCents != oldTotal+res.PriceEffect.DeltaCents || got.Row.TotalCents <= oldTotal {
			t.Fatalf("total %d → %d (effect %+v)", oldTotal, got.Row.TotalCents, res.PriceEffect)
		}
		var diffs []store.AuditEntry
		if err := e.DB.NewSelect().Model(&diffs).Where("action = ?", "allocation.layout").Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		priced := false
		for _, a := range diffs {
			var diff map[string]any
			if json.Unmarshal(a.Diff, &diff) == nil && diff["price_delta_cents"] != nil {
				priced = true
			}
		}
		if !priced {
			t.Fatalf("no price effect in the layout audit: %d rows", len(diffs))
		}
		e.checkInvariants(t)
	})
}

// TestScenarioApply_FitOverriddenBoat: a boat accepted with a fit override (LOA above the area max) is kept by the
// optimizer and the area can be optimized and applied.
func TestScenarioApply_FitOverriddenBoat(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		e.threeBoats(t)
		if _, err := e.DB.NewUpdate().TableExpr("dynamic_areas").Set("max_loa_cm = 900").Where("id = ?", e.shed).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		ends := d("2028-05-01")
		_, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: e.addVessel(t, 1000, 300), StartsOn: d("2027-10-05"), EndsOn: &ends,
			AreaID: e.shed, Placement: pl(5, 7, 0), Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
			FitOverride: &contract.FitOverrideInput{Reason: "prua corta"},
		})
		if err != nil {
			t.Fatalf("override: %v", err)
		}
		sd := e.newScenario(t, "Con deroga")
		sd, err = e.svc.Optimize(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version), nil, 200)
		if err != nil {
			t.Fatalf("optimize: %v", err)
		}
		if sd.Indicators.Unplaced != 0 {
			t.Fatalf("overridden boat unplaced: %+v", sd.Indicators)
		}
		if _, err := e.svc.Apply(ctx, e.Owner, e.MarinaID, sd.Scenario.ID, int64(sd.Scenario.Version)); err != nil {
			t.Fatalf("apply: %v", err)
		}
		e.checkInvariants(t)
	})
}
