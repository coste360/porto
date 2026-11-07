//go:build integration

package contract

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func (e *tenv) violations(t *testing.T, query string) []store.Violation {
	t.Helper()
	vs, err := store.CheckOccupancyInvariants(context.Background(), e.DB.DB, e.MarinaID, occupancy.Today(e.Clock, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Violation
	for _, v := range vs {
		if v.Query == query {
			out = append(out, v)
		}
	}
	return out
}

// TestInvariants_Q7AfterHoldLapse: a transit confirmed after its hold lapsed keeps its expired allocation in the
// history, and its coverage is still checked (INV-10): a gap in the reallocation is a Q7 violation.
func TestInvariants_Q7AfterHoldLapse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		a := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-13", false)
		e.Clock.Advance(25 * time.Hour)
		c, err := e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, a.Row.ID, a.Row.Version, ManualConfirmation{})
		if err != nil {
			t.Fatal(err)
		}
		e.invariants(t)
		act := activeOnly(c.Allocations)
		if len(act) != 1 {
			t.Fatalf("allocations %+v", c.Allocations)
		}
		// Tamper: the reallocation loses its last night.
		bg := context.Background()
		if _, err := db.NewUpdate().TableExpr("allocations").Set("last_night = ?", d("2027-06-11")).Where("id = ?", act[0].ID).Exec(bg); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewDelete().TableExpr("berth_occupancies").Where("allocation_id = ? AND night = ?", act[0].ID, d("2027-06-12")).Exec(bg); err != nil {
			t.Fatal(err)
		}
		if vs := e.violations(t, "Q7"); len(vs) == 0 {
			t.Fatal("Q7 missed the gap of a transit confirmed after its hold lapsed")
		}
	})
}

// TestInvariants_Q8QuoteHold: a valid quote whose hold option lost its hold is a Q8 violation; a moved quote hold
// stays the option's hold, also after undo.
func TestInvariants_Q8QuoteHold(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		q := e.quote(t, e.Owner, KindTransit, "", "2027-07-01", "2027-07-04", true, e.berths[0], e.berths[1])
		hold := q.Options[0].Placements[0].AllocationID
		al, err := store.GetAllocation(ctx, db.DB, e.scope(t), hold)
		if err != nil {
			t.Fatal(err)
		}
		// Move the hold as a whole: the placement follows it.
		res, err := e.occ.Move(ctx, e.Owner, e.MarinaID, occupancy.MoveInput{
			AllocationID: hold, Version: al.Version, Target: occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: e.berths[2]},
		})
		if err != nil {
			t.Fatal(err)
		}
		e.invariants(t)
		got, err := e.svc.GetQuote(ctx, e.Owner, e.MarinaID, q.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		pl := got.Options[0].Placements[0]
		if pl.AllocationID == hold || pl.ResourceID != e.berths[2] {
			t.Fatalf("placement after move %+v", pl)
		}
		// A partial move of a quote hold is refused.
		moved, _ := store.GetAllocation(ctx, db.DB, e.scope(t), pl.AllocationID)
		from := d("2027-07-02")
		if _, err := e.occ.Move(ctx, e.Owner, e.MarinaID, occupancy.MoveInput{
			AllocationID: moved.ID, Version: moved.Version, FromNight: &from, Target: occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: e.berths[3]},
		}); err == nil {
			t.Fatal("partial move of a quote hold accepted")
		}
		if _, err := e.occ.Undo(ctx, e.Owner, e.MarinaID, res.UndoID); err != nil {
			t.Fatal(err)
		}
		e.invariants(t)
		got, _ = e.svc.GetQuote(ctx, e.Owner, e.MarinaID, q.Row.ID)
		if pl := got.Options[0].Placements[0]; pl.AllocationID != hold || pl.ResourceID != e.berths[0] {
			t.Fatalf("placement after undo %+v", pl)
		}
		// Tamper: the hold of option 2 is cancelled behind the quote's back.
		other := q.Options[1].Placements[0].AllocationID
		bg := context.Background()
		if _, err := db.NewDelete().TableExpr("berth_occupancies").Where("allocation_id = ?", other).Exec(bg); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().TableExpr("allocations").Set("status = 'cancelled'").Where("id = ?", other).Exec(bg); err != nil {
			t.Fatal(err)
		}
		if vs := e.violations(t, "Q8"); len(vs) != 1 {
			t.Fatalf("Q8 violations %+v", vs)
		}
	})
}

// lapsedAfterIntervalMove asserts the outcome of converting a contract whose holds lapsed after an interval move:
// each of the three lapsed legs (before, during and after the interval) is allocated again exactly once, on its
// own resource and nights, moved from the lapsed leg, and Q7 (coverage) and Q12 (chains) find nothing.
func lapsedAfterIntervalMove(t *testing.T, e *tenv, legs []occupancy.Allocation, after []store.Allocation, kind string) {
	t.Helper()
	act := activeOnly(after)
	if len(act) != len(legs) {
		t.Fatalf("%d active allocations after the lapse of %d legs: %+v", len(act), len(legs), after)
	}
	byFrom := map[string]store.Allocation{}
	for _, a := range act {
		byFrom[a.MovedFromID] = a
	}
	for _, l := range legs {
		n, ok := byFrom[l.ID]
		if !ok || n.Kind != kind || n.ResourceID != l.ResourceID || !n.FirstNight.Equal(l.First) || !n.LastNight.Equal(l.Last) {
			t.Fatalf("leg %s [%s, %s] on %s not re-allocated once: %+v", l.ID, l.First, l.Last, l.ResourceID, act)
		}
		if got := e.rows(t, n.ID); got != l.First.DaysUntil(l.Last)+1 {
			t.Fatalf("re-allocated leg %s has %d rows", n.ID, got)
		}
	}
	for _, q := range []string{"Q6", "Q7", "Q12"} {
		if vs := e.violations(t, q); len(vs) != 0 {
			t.Fatalf("%s: %+v", q, vs)
		}
	}
	e.invariants(t)
}

// intervalMove moves the nights [from, to] of the only active allocation of c to berth and returns the three
// active legs of the line (source before, moved interval, return).
func (e *tenv) intervalMove(t *testing.T, c *Detail, berth, from, to string) []occupancy.Allocation {
	t.Helper()
	al := activeOnly(c.Allocations)[0]
	f, l := d(from), d(to)
	res, err := e.occ.Move(fixture.Ctx(), e.Owner, e.MarinaID, occupancy.MoveInput{
		AllocationID: al.ID, Version: al.Version, FromNight: &f, ToNight: &l,
		Target: occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: berth},
	})
	if err != nil {
		t.Fatalf("interval move: %v", err)
	}
	var legs []occupancy.Allocation
	for _, a := range res.Allocations {
		if a.Status == occupancy.StatusActive {
			legs = append(legs, a)
		}
	}
	if len(legs) != 3 {
		t.Fatalf("interval move legs %+v", res.Allocations)
	}
	return legs
}

// TestContract_ConfirmAfterLapseWithIntervalMove (INV-10): a held transit moved for an interval has three hold
// allocations on its line; when they lapse, confirming re-allocates all three (not only the first one found).
func TestContract_ConfirmAfterLapseWithIntervalMove(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		a := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-15", false)
		legs := e.intervalMove(t, a, e.berths[1], "2027-06-11", "2027-06-12")
		e.Clock.Advance(25 * time.Hour)
		cur, err := store.GetContract(fixture.Ctx(), db.DB, e.scope(t), a.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		c, err := e.svc.ConfirmTransit(fixture.Ctx(), e.Owner, e.MarinaID, a.Row.ID, cur.Version, ManualConfirmation{})
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.Status != string(StatusConfirmed) {
			t.Fatalf("status %s", c.Row.Status)
		}
		lapsedAfterIntervalMove(t, e, legs, c.Allocations, store.AllocKindTransit)
	})
}

// TestContract_ActivateAfterLapseWithIntervalMove (INV-10): the same for activating a draft mooring contract.
func TestContract_ActivateAfterLapseWithIntervalMove(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		m := e.mooring(t, e.Owner, e.berths[0], "2027-07-01", false)
		legs := e.intervalMove(t, m, e.berths[1], "2027-07-10", "2027-07-20")
		e.Clock.Advance(8 * 24 * time.Hour)
		cur, err := store.GetContract(fixture.Ctx(), db.DB, e.scope(t), m.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		c, err := e.svc.ActivateContract(fixture.Ctx(), e.Owner, e.MarinaID, m.Row.ID, cur.Version)
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.Status != string(StatusActive) {
			t.Fatalf("status %s", c.Row.Status)
		}
		lapsedAfterIntervalMove(t, e, legs, c.Allocations, store.AllocKindContract)
	})
}
