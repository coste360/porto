//go:build integration

package contract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestContract_MoveReprice covers the price effect of a move (m3-plan §3.5 step 6): reported only by default,
// an explicit reprice needs contracts.write, and reprice (explicit or by policy) replaces the stored total, with
// the adjustment in the snapshot, the audit rows and transit.changed in the move transaction.
func TestContract_MoveReprice(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		e.occ.SetMovePricer(e.svc)
		if _, err := db.NewUpdate().TableExpr("berths").Set("price_class = 'L15'").Where("id = ?", e.berths[3]).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		toA4 := occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: e.berths[3]}
		move := func(p *auth.Principal, c *Detail, reprice *bool) (occupancy.MoveResult, error) {
			al := c.Allocations[0]
			return e.occ.Move(ctx, p, e.MarinaID, occupancy.MoveInput{AllocationID: al.ID, Version: al.Version, Target: toA4, Reprice: reprice})
		}
		reload := func(c *Detail) *store.Contract {
			row, err := store.GetContract(ctx, db.DB, e.scope(t), c.Row.ID)
			if err != nil {
				t.Fatal(err)
			}
			return row
		}
		yes := true

		// Reported only: the total stays, the delta is audited on the move.
		c := e.transit(t, e.Owner, e.berths[0], "2027-06-02", "2027-06-05", true)
		check, err := e.occ.Check(ctx, e.Owner, e.MarinaID, occupancy.CheckInput{AllocationID: c.Allocations[0].ID, Target: toA4, From: d("2027-06-02"), To: d("2027-06-04")})
		if err != nil || check.PriceEffect == nil || check.PriceEffect.DeltaCents <= 0 || check.PriceEffect.Applied {
			t.Fatalf("check price effect %+v: %v", check, err)
		}
		res, err := move(e.Owner, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		eff := res.PriceEffect
		if eff == nil || eff.Applied || eff.DeltaCents != check.PriceEffect.DeltaCents || eff.OldTotalCents != c.Row.TotalCents ||
			eff.NewTotalCents != c.Row.TotalCents+eff.DeltaCents {
			t.Fatalf("reported effect %+v (total %d)", eff, c.Row.TotalCents)
		}
		if row := reload(c); row.TotalCents != c.Row.TotalCents || row.Version != c.Row.Version {
			t.Fatalf("reported move changed the contract: %+v", row)
		}
		var diffs []string
		if err := db.NewSelect().TableExpr("audit_log").Column("diff").Where("action = 'allocation.move'").Scan(context.Background(), &diffs); err != nil {
			t.Fatal(err)
		}
		var diff map[string]any
		if len(diffs) == 1 {
			_ = json.Unmarshal([]byte(diffs[0]), &diff)
		}
		if delta, _ := diff["price_delta_cents"].(float64); int64(delta) != eff.DeltaCents || diff["repriced"] != false {
			t.Fatalf("allocation.move audit diff %v", diffs)
		}

		// Explicit reprice without contracts.write: 403, nothing moves.
		c2 := e.transit(t, e.Owner, e.berths[1], "2027-06-10", "2027-06-12", true)
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := move(dock, c2, &yes); !errors.Is(err, occupancy.ErrRepriceForbidden) {
			t.Fatalf("dock reprice: %v", err)
		}
		if n := e.rows(t, c2.Allocations[0].ID); n != 2 {
			t.Fatalf("forbidden reprice moved rows: %d", n)
		}

		// Explicit reprice by the owner: the total moves by the delta.
		res, err = move(e.Owner, c2, &yes)
		if err != nil {
			t.Fatal(err)
		}
		row := reload(c2)
		if res.PriceEffect == nil || !res.PriceEffect.Applied || res.PriceEffect.DeltaCents <= 0 ||
			row.TotalCents != c2.Row.TotalCents+res.PriceEffect.DeltaCents {
			t.Fatalf("explicit reprice %+v total %d → %d", res.PriceEffect, c2.Row.TotalCents, row.TotalCents)
		}
		snap := DecodeSnapshot(row.PriceSnapshot)
		if snap == nil || len(snap.Adjustments) != 1 || snap.Adjustments[0].NewResourceID != e.berths[3] ||
			snap.Adjustments[0].NewCents-snap.Adjustments[0].OldCents != res.PriceEffect.DeltaCents {
			t.Fatalf("snapshot adjustments %+v", snap)
		}
		if n := e.AuditCount(t, "transit.reprice"); n != 1 {
			t.Fatalf("transit.reprice audit rows %d", n)
		}
		if n := countChange(t, e, event.TransitChanged, event.ChangeRepriced); n != 1 {
			t.Fatalf("transit.changed repriced events %d", n)
		}

		// Undo of the explicit reprice: the total is back to the original, the snapshot keeps the adjustment and its
		// opposite move_undo entry (INV-27), with a transit.reprice audit row (undo) and transit.changed.
		if _, err := e.occ.Undo(ctx, e.Owner, e.MarinaID, res.UndoID); err != nil {
			t.Fatalf("undo repriced move: %v", err)
		}
		row = reload(c2)
		if row.TotalCents != c2.Row.TotalCents {
			t.Fatalf("undo left the total at %d, original %d", row.TotalCents, c2.Row.TotalCents)
		}
		snap = DecodeSnapshot(row.PriceSnapshot)
		if snap == nil || len(snap.Adjustments) != 2 || snap.Adjustments[1].Kind != AdjustmentMoveUndo ||
			snap.Adjustments[1].NewCents-snap.Adjustments[1].OldCents != -res.PriceEffect.DeltaCents {
			t.Fatalf("snapshot adjustments after undo %+v", snap)
		}
		assertTotalMatchesSnapshot(t, row)
		if n := e.AuditCount(t, "transit.reprice"); n != 2 {
			t.Fatalf("transit.reprice audit rows after undo %d", n)
		}
		var undoDiffs []string
		if err := db.NewSelect().TableExpr("audit_log").Column("diff").Where("action = 'transit.reprice'").OrderExpr("id").Scan(context.Background(), &undoDiffs); err != nil {
			t.Fatal(err)
		}
		var ud map[string]any
		if err := json.Unmarshal([]byte(undoDiffs[len(undoDiffs)-1]), &ud); err != nil || ud["undo"] != true {
			t.Fatalf("undo reprice audit diff %v", undoDiffs)
		}
		if n := countChange(t, e, event.TransitChanged, event.ChangeRepriced); n != 2 {
			t.Fatalf("transit.changed repriced events after undo %d", n)
		}

		// A reprice that would bring the total below zero, or a contract without a price snapshot, is refused and
		// the move writes nothing.
		c4 := e.transit(t, e.Owner, e.berths[3], "2027-07-01", "2027-07-03", true)
		toA1 := occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: e.berths[0]}
		for _, tamper := range []string{"total_cents = 1", "price_snapshot = NULL"} {
			if _, err := db.NewUpdate().TableExpr("contracts").Set(tamper).Where("id = ?", c4.Row.ID).Exec(context.Background()); err != nil {
				t.Fatal(err)
			}
			al := c4.Allocations[0]
			before := reload(c4)
			if _, err := e.occ.Move(ctx, e.Owner, e.MarinaID, occupancy.MoveInput{AllocationID: al.ID, Version: al.Version, Target: toA1, Reprice: &yes}); !errors.Is(err, ErrRepriceRefused) {
				t.Fatalf("%s: reprice not refused: %v", tamper, err)
			}
			if after := reload(c4); after.TotalCents != before.TotalCents || after.Version != before.Version || e.rows(t, al.ID) != 2 {
				t.Fatalf("%s: refused reprice wrote something", tamper)
			}
		}

		// Policy reprice_on_move: applied without the flag.
		c3 := e.transit(t, e.Owner, e.berths[2], "2027-06-20", "2027-06-22", true)
		if _, err := db.NewUpdate().TableExpr("contracts").Set("policy = ?", `{"version":1,"reprice_on_move":true,"release_policy":"with_consent","allow_segments":true}`).
			Where("id = ?", c3.Row.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		res, err = move(e.Owner, c3, nil)
		if err != nil {
			t.Fatal(err)
		}
		if row := reload(c3); res.PriceEffect == nil || !res.PriceEffect.Applied || row.TotalCents != c3.Row.TotalCents+res.PriceEffect.DeltaCents {
			t.Fatalf("policy reprice %+v total %d", res.PriceEffect, row.TotalCents)
		}
		e.invariants(t)
	})
}

// assertTotalMatchesSnapshot checks INV-27 on one contract: total_cents = Breakdown.Total + Σ(new − old).
func assertTotalMatchesSnapshot(t interface {
	Helper()
	Fatalf(format string, args ...any)
}, c *store.Contract,
) {
	t.Helper()
	snap := DecodeSnapshot(c.PriceSnapshot)
	if snap == nil {
		t.Fatalf("contract %s has no snapshot", c.ID)
	}
	want := int64(snap.Breakdown.Total)
	for _, ad := range snap.Adjustments {
		want += ad.NewCents - ad.OldCents
	}
	if c.TotalCents != want {
		t.Fatalf("contract %s total %d, snapshot says %d (%+v)", c.ID, c.TotalCents, want, snap.Adjustments)
	}
}

func countChange(t *testing.T, e *tenv, typ, change string) int {
	t.Helper()
	n := 0
	for _, ev := range e.Events(t, typ) {
		var m map[string]any
		if err := json.Unmarshal(ev.Payload, &m); err != nil {
			t.Fatal(err)
		}
		if d, ok := m["data"].(map[string]any); ok {
			m = d
		}
		if m["change"] == change {
			n++
		}
	}
	return n
}

// TestContract_TotalMatchesSnapshot (INV-27): random sequences of moves (whole and interval, reported or
// repriced, explicitly or by policy), undos and stay changes on a confirmed transit; after every step
// total_cents = Breakdown.Total + Σ(new − old) over the snapshot adjustments, including after undo.
func TestContract_TotalMatchesSnapshot(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		bg := context.Background()
		e.occ.SetMovePricer(e.svc)
		if _, err := db.NewUpdate().TableExpr("berths").Set("price_class = 'L15'").Where("id IN (?)", bun.List(e.berths[2:])).Exec(bg); err != nil {
			t.Fatal(err)
		}
		expected := func(err error) bool {
			var ce *occupancy.ConflictError
			var fe *occupancy.FitError
			return err == nil || errors.As(err, &ce) || errors.As(err, &fe) || errors.Is(err, occupancy.ErrResourceTaken) ||
				errors.Is(err, occupancy.ErrUndoUnavailable) || errors.Is(err, occupancy.ErrVersionMismatch) ||
				errors.Is(err, occupancy.ErrAllocationNotActive) || errors.Is(err, auth.ErrInvalidInput) ||
				errors.Is(err, ErrVersionMismatch) || errors.Is(err, ErrRepriceRefused)
		}
		policies := []string{
			`{"version":1,"reprice_on_move":false,"release_policy":"with_consent","allow_segments":true}`,
			`{"version":1,"reprice_on_move":true,"release_policy":"with_consent","allow_segments":true}`,
		}
		scope := e.scope(t)
		rapid.Check(t, func(rt *rapid.T) {
			berth := rapid.SampledFrom(e.berths).Draw(rt, "berth")
			c, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, TransitInput{
				VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-07-01"), Departure: d("2027-07-06"),
				Placements: []Segment{berthSeg(berth, "2027-07-01", "2027-07-05")}, Confirm: &ManualConfirmation{Reference: "cassa"},
			})
			if err != nil {
				rt.Fatalf("create transit: %v", err)
			}
			// free the nights for the next case even when this one fails
			defer func() {
				if row, err := store.GetContract(ctx, db.DB, scope, c.Row.ID); err == nil && row.Status != string(StatusCancelled) {
					if _, err := e.svc.CancelTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, row.Version, ""); err != nil {
						t.Errorf("cancel: %v", err)
					}
				}
			}()
			if _, err := db.NewUpdate().TableExpr("contracts").Set("policy = ?", rapid.SampledFrom(policies).Draw(rt, "policy")).Where("id = ?", c.Row.ID).Exec(bg); err != nil {
				rt.Fatal(err)
			}
			var undos []string
			// model: the price delta each move applied and the stay generation it was applied in (a stay change
			// re-prices from scratch, so an undo after it has nothing to reverse)
			type applied struct {
				delta int64
				gen   int
			}
			moves := map[string]applied{}
			gen := 0
			for range rapid.IntRange(1, 8).Draw(rt, "steps") {
				row, err := store.GetContract(ctx, db.DB, scope, c.Row.ID)
				if err != nil {
					rt.Fatal(err)
				}
				allocs, err := contractAllocs(ctx, db.DB, scope, c.Row.ID)
				if err != nil {
					rt.Fatal(err)
				}
				act := activeOnly(allocs)
				op := rapid.SampledFrom([]string{"move", "move", "undo", "undo", "stay"}).Draw(rt, "op")
				switch {
				case op == "move" && len(act) > 0:
					al := act[rapid.IntRange(0, len(act)-1).Draw(rt, "alloc")]
					in := occupancy.MoveInput{
						AllocationID: al.ID, Version: al.Version,
						Target: occupancy.Target{ResourceType: store.ResourceBerth, ResourceID: rapid.SampledFrom(e.berths).Draw(rt, "to")},
					}
					if rapid.Bool().Draw(rt, "interval") {
						f := al.FirstNight.AddDays(rapid.IntRange(0, 2).Draw(rt, "from_night"))
						l := f.AddDays(rapid.IntRange(0, 2).Draw(rt, "to_night"))
						in.FromNight, in.ToNight = &f, &l
					}
					switch rapid.IntRange(0, 2).Draw(rt, "reprice") {
					case 1:
						yes := true
						in.Reprice = &yes
					case 2:
						no := false
						in.Reprice = &no
					}
					var res occupancy.MoveResult
					res, err = e.occ.Move(ctx, e.Owner, e.MarinaID, in)
					if err == nil {
						undos = append(undos, res.UndoID)
						a := applied{gen: gen}
						if res.PriceEffect != nil && res.PriceEffect.Applied {
							a.delta = res.PriceEffect.DeltaCents
						}
						moves[res.UndoID] = a
					}
				case op == "undo" && len(undos) > 0:
					id := rapid.SampledFrom(undos).Draw(rt, "undo")
					if _, err = e.occ.Undo(ctx, e.Owner, e.MarinaID, id); err == nil {
						want := row.TotalCents
						if m := moves[id]; m.gen == gen {
							want -= m.delta
						}
						after, gerr := store.GetContract(ctx, db.DB, scope, c.Row.ID)
						if gerr != nil {
							rt.Fatal(gerr)
						}
						if after.TotalCents != want {
							rt.Fatalf("undo %s: total %d, want %d (before %d, move %+v)", id, after.TotalCents, want, row.TotalCents, moves[id])
						}
					}
				case op == "stay":
					dep := d("2027-07-06").AddDays(rapid.IntRange(-2, 0).Draw(rt, "departure"))
					if _, _, err = e.svc.ChangeStay(ctx, e.Owner, e.MarinaID, c.Row.ID, row.Version, StayChange{Departure: &dep}); err == nil {
						gen++
					}
				}
				if !expected(err) {
					rt.Fatalf("%s: unexpected error %v", op, err)
				}
				row, err = store.GetContract(ctx, db.DB, scope, c.Row.ID)
				if err != nil {
					rt.Fatal(err)
				}
				assertTotalMatchesSnapshot(rt, row)
			}
			vs, err := store.CheckOccupancyInvariants(bg, db.DB, e.MarinaID, occupancy.Today(e.Clock, time.UTC))
			if err != nil || len(vs) != 0 {
				rt.Fatalf("invariant violations %+v (%v)", vs, err)
			}
		})
	})
}
