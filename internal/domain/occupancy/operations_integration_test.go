//go:build integration

package occupancy

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func berthTarget(id string) Target { return Target{ResourceType: engine.ResourceBerth, ResourceID: id} }

func areaTarget(id string, x geo.Cm) Target {
	return Target{ResourceType: engine.ResourceArea, ResourceID: id, Placement: &engine.Placement{X: x}}
}

func TestOccupancy_MoveScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v := h.vessel(t, 1000, 350)
		a := h.mustAllocate(t, transit(v, berthSpec(h.berths[1], d0.AddDays(-3), d0.AddDays(9))))

		t.Run("from today keeps the past", func(t *testing.T) {
			res, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, Target: berthTarget(h.berths[2])})
			if err != nil {
				t.Fatal(err)
			}
			src := h.alloc(t, a.ID)
			if src.Status != store.AllocActive || !src.LastNight.Equal(d0.AddDays(-1)) {
				t.Fatalf("source %+v", src)
			}
			if len(res.Allocations) != 2 || res.UndoID == "" || !res.UndoUntil.Equal(h.s.now().Add(UndoWindow)) {
				t.Fatalf("result %+v", res)
			}
			var moved Allocation
			for _, x := range res.Allocations {
				if x.ID != a.ID {
					moved = x
				}
			}
			if moved.ResourceID != h.berths[2] || !moved.First.Equal(d0) || !moved.Last.Equal(d0.AddDays(9)) || moved.MovedFromID != a.ID {
				t.Fatalf("moved %+v", moved)
			}
			h.assertInvariants(t)
			// undo within 24 h
			h.Clock.Advance(time.Hour)
			if _, err := h.s.Undo(ctxT(), h.Owner, h.MarinaID, res.UndoID); err != nil {
				t.Fatal(err)
			}
			if src := h.alloc(t, a.ID); !src.LastNight.Equal(d0.AddDays(9)) || src.Status != store.AllocActive {
				t.Fatalf("undo source %+v", src)
			}
			if m := h.alloc(t, moved.ID); m.Status != store.AllocSuperseded {
				t.Fatalf("undo created %s", m.Status)
			}
			if _, err := h.s.Undo(ctxT(), h.Owner, h.MarinaID, res.UndoID); !errors.Is(err, ErrUndoUnavailable) {
				t.Fatalf("undo twice: %v", err)
			}
			h.assertInvariants(t)
		})
		a = h.alloc(t, a.ID)
		t.Run("stale version and past nights", func(t *testing.T) {
			if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version - 1, Target: berthTarget(h.berths[2])}); !errors.Is(err, ErrVersionMismatch) {
				t.Fatalf("stale: %v", err)
			}
			past := d0.AddDays(-2)
			if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, FromNight: &past, Target: berthTarget(h.berths[2])}); !errors.Is(err, auth.ErrInvalidInput) {
				t.Fatalf("past: %v", err)
			}
		})
		t.Run("interval move returns to the source", func(t *testing.T) {
			from, to := d0.AddDays(3), d0.AddDays(5)
			res, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, FromNight: &from, ToNight: &to, Target: areaTarget(h.area, 100)})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Allocations) != 3 {
				t.Fatalf("%d allocations", len(res.Allocations))
			}
			if got := onNight(t, h, v, d0.AddDays(4)); got != h.area {
				t.Fatalf("night 4 on %s", got)
			}
			if got := onNight(t, h, v, d0.AddDays(6)); got != h.berths[1] {
				t.Fatalf("night 6 on %s", got)
			}
			h.assertInvariants(t)
			// a later move of the new allocation blocks the undo
			var inArea Allocation
			for _, x := range res.Allocations {
				if x.ResourceID == h.area {
					inArea = x
				}
			}
			if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: inArea.ID, Version: inArea.Version, FromNight: &from, Target: areaTarget(h.area, 1000)}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.s.Undo(ctxT(), h.Owner, h.MarinaID, res.UndoID); !errors.Is(err, ErrUndoUnavailable) {
				t.Fatalf("undo after a later move: %v", err)
			}
			h.assertInvariants(t)
		})
		t.Run("undo after 24 h", func(t *testing.T) {
			b := h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[5], d0, d0.AddDays(3))))
			res, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: b.ID, Version: b.Version, Target: berthTarget(h.berths[6]), Reprice: boolp(true)})
			if err != nil {
				t.Fatal(err)
			}
			h.Clock.Advance(UndoWindow + time.Second)
			if _, err := h.s.Undo(ctxT(), h.Owner, h.MarinaID, res.UndoID); !errors.Is(err, ErrUndoUnavailable) {
				t.Fatalf("expired undo: %v", err)
			}
		})
		t.Run("move into a taken berth fails whole", func(t *testing.T) {
			now := h.today
			c := h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[7], now, now.AddDays(3))))
			h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[8], now.AddDays(2), now.AddDays(2))))
			before := h.count(t, "allocations")
			if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: c.ID, Version: c.Version, Target: berthTarget(h.berths[8])}); !isTaken(err) {
				t.Fatalf("%v", err)
			}
			if h.count(t, "allocations") != before || h.alloc(t, c.ID).Version != c.Version {
				t.Fatal("a failed move changed something")
			}
		})
		h.assertInvariants(t)
	})
}

func boolp(b bool) *bool { return &b }

func TestOccupancy_LayoutSaveExchangePlaces(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v1, v2, v3 := h.vessel(t, 1000, 350), h.vessel(t, 1000, 350), h.vessel(t, 1000, 300)
		a1 := h.mustAllocate(t, transit(v1, areaSpec(h.area, 100, d0, d0.AddDays(5))))
		a2 := h.mustAllocate(t, transit(v2, areaSpec(h.area, 500, d0, d0.AddDays(5))))
		b3 := h.mustAllocate(t, transit(v3, berthSpec(h.berths[1], d0, d0.AddDays(5))))
		// exchange v1 and v2, and bring v3 from the berth into the area
		res, err := h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{
			AreaID: h.area, Date: d0, Base: []BaseVersion{{a1.ID, a1.Version}, {a2.ID, a2.Version}, {b3.ID, b3.Version}},
			Placements: []LayoutOp{
				{AllocationID: a1.ID, Placement: engine.Placement{X: 500}},
				{AllocationID: a2.ID, Placement: engine.Placement{X: 100}},
				{AllocationID: b3.ID, Placement: engine.Placement{X: 1000}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.UndoID == "" || len(res.Allocations) != 6 {
			t.Fatalf("%d allocations", len(res.Allocations))
		}
		if onNight(t, h, v3, d0) != h.area {
			t.Fatal("v3 not in the area")
		}
		h.assertInvariants(t)
		// unplaced vessels make the save fail
		cur, _ := store.ListAllocations(ctxT(), db.DB, h.scope, store.AllocationFilter{ResourceIDs: []string{h.area}, Status: store.AllocActive, From: d0, To: d0})
		var base []BaseVersion
		for _, a := range cur {
			base = append(base, BaseVersion{a.ID, a.Version})
		}
		_, err = h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{AreaID: h.area, Date: d0, Base: base, Placements: []LayoutOp{{AllocationID: cur[0].ID, Placement: engine.Placement{X: 3000}}}})
		var ve *auth.ValidationError
		if !errors.As(err, &ve) || ve.Fields[0].Field != "unplaced" {
			t.Fatalf("unplaced: %v", err)
		}
		// an invalid placement names the allocation
		ops := []LayoutOp{}
		for _, a := range cur {
			ops = append(ops, LayoutOp{AllocationID: a.ID, Placement: *placementOf(&a)})
		}
		ops[0].Placement.X = 5
		_, err = h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{AreaID: h.area, Date: d0, Base: base, Placements: ops})
		var pe *PlacementError
		if !errors.As(err, &pe) || pe.AllocationID != cur[0].ID {
			t.Fatalf("placement: %v", err)
		}
		h.assertInvariants(t)
	})
}

func TestOccupancy_LayoutStaleBase412(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		a1 := h.mustAllocate(t, transit(h.vessel(t, 1000, 350), areaSpec(h.area, 100, d0, d0.AddDays(5))))
		base := []BaseVersion{{a1.ID, a1.Version}}
		// another vessel arrives in the area after the draft was taken
		h.mustAllocate(t, transit(h.vessel(t, 1000, 350), areaSpec(h.area, 2000, d0, d0.AddDays(5))))
		_, err := h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{AreaID: h.area, Date: d0, Base: base, Placements: []LayoutOp{{AllocationID: a1.ID, Placement: engine.Placement{X: 600}}}})
		if !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("missing from base: %v", err)
		}
		_, err = h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{AreaID: h.area, Date: d0, Base: []BaseVersion{{a1.ID, a1.Version + 1}}, Placements: []LayoutOp{{AllocationID: a1.ID, Placement: engine.Placement{X: 600}}}})
		if !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
	})
}

func TestOccupancy_ReleaseResaleRevoke(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v := h.vessel(t, 1000, 350)
		_, line := h.contractLine(t, store.ContractKindMooring, "active", v, store.ResourceBerth, h.berths[1], d0, d0.AddDays(90))
		a := h.mustAllocate(t, NewAllocation{ContractLineID: &line, VesselID: strp(v), Kind: KindContract, Spec: berthSpec(h.berths[1], d0, d0.AddDays(90))})
		bad := []ReleaseInput{
			{AllocationID: a.ID, From: d0.AddDays(-1), To: d0.AddDays(2), Source: "office"},
			{AllocationID: a.ID, From: d0.AddDays(80), To: d0.AddDays(95), Source: "office"},
		}
		for _, in := range bad {
			if _, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, in); !errors.Is(err, ErrReleaseInvalid) {
				t.Fatalf("%+v: %v", in, err)
			}
		}
		rel, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a.ID, From: d0.AddDays(10), To: d0.AddDays(19), Source: "customer_phone", Notes: "chiamata"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a.ID, From: d0.AddDays(15), To: d0.AddDays(25), Source: "office"}); !errors.Is(err, ErrReleaseInvalid) {
			t.Fatalf("overlapping: %v", err)
		}
		if n := h.count(t, "berth_occupancies"); n != 91-10 {
			t.Fatalf("%d rows", n)
		}
		h.assertInvariants(t)
		// a transit buys three released nights
		buyer := h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[1], d0.AddDays(12), d0.AddDays(14))))
		got, _ := store.GetRelease(ctxT(), db.DB, h.scope, rel.ID)
		if got.ResoldAllocationID != buyer.ID || got.ResoldNights != 3 {
			t.Fatalf("resale %+v", got)
		}
		if n := len(h.Events(t, event.ReleaseResold)); n != 1 {
			t.Fatalf("%d release.resold", n)
		}
		// revoking resold nights is a conflict with alternatives; nothing changes
		_, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel.ID, got.Version, nil, nil)
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.AllocationIDs[0] != buyer.ID || len(ce.Alternatives) == 0 {
			t.Fatalf("revoke resold: %v", err)
		}
		// revoking the free tail splits nothing; the head of the release is kept
		f, l := d0.AddDays(16), d0.AddDays(19)
		res, err := h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel.ID, got.Version, &f, &l)
		if err != nil {
			t.Fatal(err)
		}
		if res.RestoredNights != 4 || !res.Release.To.Equal(d0.AddDays(15)) || res.Remaining != nil {
			t.Fatalf("revoke %+v", res)
		}
		h.assertInvariants(t)
		// a middle revoke splits the release
		f, l = d0.AddDays(10), d0.AddDays(10)
		res, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel.ID, res.Release.Version, &f, &l)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Release.From.Equal(d0.AddDays(11)) {
			t.Fatalf("head revoke %+v", res.Release)
		}
		f, l = d0.AddDays(11), d0.AddDays(11)
		res, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel.ID, res.Release.Version, &f, &l)
		if err != nil || res.Remaining != nil {
			t.Fatalf("%+v %v", res, err)
		}
		h.assertInvariants(t)
		// transits cannot be released
		if _, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: buyer.ID, From: d0.AddDays(12), To: d0.AddDays(12), Source: "office"}); !errors.Is(err, ErrReleaseInvalid) {
			t.Fatalf("transit release: %v", err)
		}
		// moving the contract carries its releases
		cur := h.alloc(t, a.ID)
		from := d0.AddDays(5)
		if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: cur.Version, FromNight: &from, Target: berthTarget(h.berths[2])}); err != nil {
			t.Fatal(err)
		}
		h.assertInvariants(t)
		// revoking never rewrites past nights: an explicit past range is refused, the default starts today
		_, line2 := h.contractLine(t, store.ContractKindMooring, "active", v, store.ResourceBerth, h.berths[3], d0, d0.AddDays(90))
		a2 := h.mustAllocate(t, NewAllocation{ContractLineID: &line2, VesselID: strp(v), Kind: KindContract, Spec: berthSpec(h.berths[3], d0, d0.AddDays(90))})
		rel2, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a2.ID, From: d0.AddDays(30), To: d0.AddDays(39), Source: "office"})
		if err != nil {
			t.Fatal(err)
		}
		h.Clock.Advance(33 * 24 * time.Hour)
		f, l = d0.AddDays(30), d0.AddDays(39)
		_, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel2.ID, rel2.Version, &f, &l)
		if re, ok := errors.AsType[*ReleaseError](err); !ok || re.Reason != "past_nights" {
			t.Fatalf("past revoke: %v", err)
		}
		res, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel2.ID, rel2.Version, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.RestoredNights != 7 || !res.Release.To.Equal(d0.AddDays(32)) || res.Release.Status != store.ReleaseActive {
			t.Fatalf("default revoke after the start %+v", res)
		}
		h.assertInvariants(t)
	})
}

func TestOccupancy_BlocksAndClosures(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		b, err := h.s.CreateBlock(ctxT(), h.Owner, h.MarinaID, BlockInput{Target: berthTarget(h.berths[3]), From: d0.AddDays(-2), To: d0.AddDays(5), Reason: "lavori"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.CreateBlock(ctxT(), h.Owner, h.MarinaID, BlockInput{Target: areaTarget(h.area, 100), From: d0, To: d0, Reason: "x"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("area block without size: %v", err)
		}
		if err := h.s.DeleteBlock(ctxT(), h.Owner, h.MarinaID, b.ID, b.Version+1); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if err := h.s.DeleteBlock(ctxT(), h.Owner, h.MarinaID, b.ID, b.Version); err != nil {
			t.Fatal(err)
		}
		if got := h.alloc(t, b.ID); got.Status != store.AllocActive || !got.LastNight.Equal(d0.AddDays(-1)) {
			t.Fatalf("block after delete %+v", got)
		}
		h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[3], d0, d0)))
		c, err := h.s.CreateClosure(ctxT(), h.Owner, h.MarinaID, Closure{ScopeType: store.ClosureMap, ScopeID: h.mapID, From: d0.AddDays(10), To: d0.AddDays(11), Reason: "regata"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.allocate(ctxT(), transit(h.vessel(t, 1000, 350), berthSpec(h.berths[4], d0.AddDays(11), d0.AddDays(12)))); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed map: %v", err)
		}
		if _, err := h.s.CreateClosure(ctxT(), h.Owner, h.MarinaID, Closure{ScopeType: store.ClosureMap, ScopeID: store.NewID(), From: d0, To: d0, Reason: "x"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown scope: %v", err)
		}
		cs, err := h.s.ListClosures(ctxT(), h.Owner, h.MarinaID, d0, d0.AddDays(30))
		if err != nil || len(cs) != 1 {
			t.Fatalf("%v %v", cs, err)
		}
		if err := h.s.DeleteClosure(ctxT(), h.Owner, h.MarinaID, c.ID, c.Version); err != nil {
			t.Fatal(err)
		}
		dock := h.user(t, auth.RoleDock)
		if _, err := h.s.CreateClosure(ctxT(), dock, h.MarinaID, Closure{ScopeType: store.ClosureMarina, From: d0, To: d0, Reason: "x"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock closure: %v", err)
		}
		s, err := h.s.GetSettings(ctxT(), dock, h.MarinaID)
		if err != nil || s.Version != 1 || s.SafetyClearanceCm != 30 {
			t.Fatalf("%+v %v", s, err)
		}
		s.MaxSegments = 2
		if _, err := h.s.UpdateSettings(ctxT(), h.Owner, h.MarinaID, 1, *s); err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.UpdateSettings(ctxT(), h.Owner, h.MarinaID, 1, *s); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale settings: %v", err)
		}
		s.DefaultArrivalTime = "25:00"
		if _, err := h.s.UpdateSettings(ctxT(), h.Owner, h.MarinaID, 2, *s); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("bad time: %v", err)
		}
		h.assertInvariants(t)
	})
}

func TestOccupancy_HarbourGuard(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v := h.vessel(t, 1000, 350)
		h.mustAllocate(t, transit(v, berthSpec(h.berths[1], d0, d0.AddDays(3))))
		h.mustAllocate(t, transit(v, areaSpec(h.area, 100, d0.AddDays(5), d0.AddDays(6))))
		h.mustAllocate(t, transit(v, berthSpec(h.berths[2], d0.AddDays(-10), d0.AddDays(-5))))
		g := h.s.Guard()
		check := func(ch harbor.ElementChange) error {
			return db.RunInTx(ctxT(), func(ctx context.Context, tx bun.Tx) error {
				return g.CheckChanges(ctx, tx, h.scope, h.mapID, []harbor.ElementChange{ch})
			})
		}
		var ue *harbor.ElementInUseError
		if err := check(harbor.ElementChange{Kind: store.ResourceBerth, ID: h.berths[1], Removed: true}); !errors.As(err, &ue) || ue.References[0] != h.berths[1] {
			t.Fatalf("delete occupied berth: %v", err)
		}
		if err := check(harbor.ElementChange{Kind: store.ResourceArea, ID: h.area, GeometryChanged: true}); !errors.Is(err, ErrElementInUse) {
			t.Fatalf("reshape occupied area: %v", err)
		}
		if err := check(harbor.ElementChange{Kind: store.ResourceBerth, ID: h.berths[1], GeometryChanged: true}); err != nil {
			t.Fatalf("berth dimension change: %v", err)
		}
		if err := check(harbor.ElementChange{Kind: store.ResourceBerth, ID: h.berths[2], Removed: true}); err != nil {
			t.Fatalf("past only passes the guard: %v", err)
		}
		// past rows: the RESTRICT foreign key refuses the delete
		_, err := db.NewDelete().TableExpr("berths").Where("id = ?", h.berths[2]).Exec(ctxT())
		if err == nil || !store.IsForeignKeyViolation(err) {
			t.Fatalf("restrict: %v", err)
		}
	})
}

func TestAllocation_FitOrOverride(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		big := h.vessel(t, 1300, 420)
		a := h.mustAllocate(t, NewAllocation{VesselID: strp(big), Kind: KindTransit, Spec: berthSpec(h.berths[1], d0, d0), FitOverride: &FitOverride{UserID: h.OwnerID, Reason: "in deroga"}})
		h.assertInvariants(t)
		// tampering the override away is caught by Q14
		if _, err := db.NewUpdate().TableExpr("allocations").Set("fit_override = NULL").Where("id = ?", a.ID).Exec(ctxT()); err != nil {
			t.Fatal(err)
		}
		vs, err := store.CheckOccupancyInvariants(ctxT(), db.DB, h.MarinaID, d0)
		if err != nil || len(vs) != 1 || vs[0].Query != "Q14" {
			t.Fatalf("%+v %v", vs, err)
		}
		// moving the vessel needs a new override for the new berth
		if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, Target: berthTarget(h.berths[2])}); !errors.Is(err, ErrFitBlocked) {
			t.Fatalf("move without override: %v", err)
		}
		// an override without its author is caught by Q14 too
		if _, err := db.NewUpdate().TableExpr("allocations").Set("fit_override = ?", `{"user_id":"","reason":"in deroga","codes":[]}`).Where("id = ?", a.ID).Exec(ctxT()); err != nil {
			t.Fatal(err)
		}
		vs, err = store.CheckOccupancyInvariants(ctxT(), db.DB, h.MarinaID, d0)
		if err != nil || len(vs) != 1 || vs[0].Query != "Q14" {
			t.Fatalf("anonymous override: %+v %v", vs, err)
		}
		// an API key override records the key and its creator
		key := &auth.Principal{
			Kind: auth.KindAPIKey, APIKeyID: store.NewID(), UserID: h.OwnerID, Grants: h.Owner.Grants,
			Scopes: []auth.Permission{auth.PermOccupancyMove, auth.PermOccupancyOverrideFit, auth.PermMapView},
		}
		reason := "chiave"
		cur := h.alloc(t, a.ID)
		res, err := h.s.Move(ctxT(), key, h.MarinaID, MoveInput{AllocationID: a.ID, Version: cur.Version, Target: berthTarget(h.berths[2]), FitOverride: &reason})
		if err != nil {
			t.Fatal(err)
		}
		var moved *store.Allocation
		for i := range res.Allocations {
			if got := h.alloc(t, res.Allocations[i].ID); got.ResourceID == h.berths[2] {
				moved = got
			}
		}
		if moved == nil {
			t.Fatalf("moved allocation not found in %+v", res.Allocations)
		}
		o := unmarshalJSON[FitOverride](moved.FitOverride)
		if o.UserID != h.OwnerID || o.APIKeyID != key.APIKeyID || o.Reason != reason {
			t.Fatalf("API key override %+v", o)
		}
	})
}

// countingHook counts the queries run through a bun DB.
type countingHook struct{ n atomic.Int64 }

func (c *countingHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (c *countingHook) AfterQuery(_ context.Context, e *bun.QueryEvent) {
	if e.Err == nil || errors.Is(e.Err, sql.ErrNoRows) {
		c.n.Add(1)
	}
}

func TestOccupancy_SearchSnapshotQueryCount(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		v := h.vessel(t, 1000, 350)
		for i := range 5 {
			h.mustAllocate(t, transit(v, berthSpec(h.berths[i], h.today.AddDays(i), h.today.AddDays(i))))
		}
		hook := &countingHook{}
		_, err := store.LoadSearchSnapshot(ctxT(), db.WithQueryHook(hook), h.scope, h.today, h.today.AddDays(5), h.Clock.Now(), true, v)
		if err != nil {
			t.Fatal(err)
		}
		if n := hook.n.Load(); n > 8 {
			t.Fatalf("snapshot read with %d queries", n)
		}
	})
}

func TestOccupancy_SearchScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(2)
		v := h.vessel(t, 1100, 380)
		h.mustAllocate(t, transit(h.vessel(t, 1000, 350), berthSpec(h.berths[8], d0, d0)))
		res, err := h.s.Search(ctxT(), h.Owner, h.MarinaID, SearchInput{Purpose: engine.PurposeTransit, VesselID: v, Arrival: d0, Departure: d0.AddDays(3)})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Solutions) == 0 || res.Considered != 10+1+3 {
			t.Fatalf("%d solutions, %d considered", len(res.Solutions), res.Considered)
		}
		// the free transit-dedicated berth first; the taken one is never proposed
		if s := res.Solutions[0]; s.Segments[0].ResourceID != h.berths[9] || s.Segments[0].Code != "A-10" || s.Segments[0].MapID != h.mapID {
			t.Fatalf("first %+v", s)
		}
		for _, s := range res.Solutions {
			if s.Segments[0].ResourceID == h.berths[8] {
				t.Fatal("taken berth proposed")
			}
		}
		yard := h.user(t, auth.RoleYard)
		if _, err := h.s.Search(ctxT(), yard, h.MarinaID, SearchInput{Purpose: engine.PurposeTransit, VesselID: v, Arrival: d0, Departure: d0.AddDays(1)}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard search: %v", err)
		}
		chk, err := h.s.Check(ctxT(), h.Owner, h.MarinaID, CheckInput{VesselID: v, Target: berthTarget(h.berths[8]), From: d0, To: d0.AddDays(1)})
		if err != nil || chk.OK || len(chk.Conflicts) != 1 {
			t.Fatalf("check %+v %v", chk, err)
		}
		chk, err = h.s.Check(ctxT(), h.Owner, h.MarinaID, CheckInput{VesselID: v, Target: areaTarget(h.area, 15), From: d0, To: d0})
		if err != nil || chk.PlacementError != PlacementUnsnapped {
			t.Fatalf("check unsnapped %+v %v", chk, err)
		}
	})
}

func TestOccupancy_DayViewScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v1, v2, v3 := h.vessel(t, 1000, 350), h.vessel(t, 1300, 420), h.vessel(t, 1000, 350)
		cid, line := h.contractLine(t, store.ContractKindMooring, "active", v1, store.ResourceBerth, h.berths[1], d0.AddDays(-30), d0.AddDays(300))
		h.mustAllocate(t, NewAllocation{ContractLineID: &line, VesselID: strp(v1), Kind: KindContract, Spec: berthSpec(h.berths[1], d0.AddDays(-30), d0.AddDays(300))})
		h.mustAllocate(t, NewAllocation{VesselID: strp(v2), Kind: KindTransit, Spec: berthSpec(h.berths[2], d0, d0.AddDays(2)), FitOverride: &FitOverride{UserID: h.OwnerID, Reason: "deroga"}})
		h.mustAllocate(t, transit(v3, areaSpec(h.area, 100, d0.AddDays(-1), d0.AddDays(3))))
		h.mustAllocate(t, transit(v3, berthSpec(h.berths[4], d0.AddDays(-3), d0.AddDays(-1))))
		if _, err := h.s.CreateBlock(ctxT(), h.Owner, h.MarinaID, BlockInput{Target: berthTarget(h.berths[5]), From: d0, To: d0, Reason: "lavori"}); err != nil {
			t.Fatal(err)
		}
		dv, err := h.s.GetDay(ctxT(), h.Owner, h.MarinaID, h.mapID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !dv.Date.Equal(d0) || len(dv.Elements) != 10+2+3 || len(dv.AreaPlacements) != 1 {
			t.Fatalf("%s %d elements %d placements", dv.Date, len(dv.Elements), len(dv.AreaPlacements))
		}
		by := map[string]DayElement{}
		for _, e := range dv.Elements {
			by[e.ResourceID] = e
		}
		if e := by[h.berths[1]]; e.Status != "contract" || e.Contract == nil || e.Contract.ID != cid || e.Vessel == nil || e.Deadlines == nil || e.Deadlines.Insurance != DeadlineMissing {
			t.Fatalf("contract element %+v", e)
		}
		if e := by[h.berths[2]]; e.Status != "transit" || !e.ArrivalToday || e.Fit == nil || !e.Fit.Override || len(e.Fit.BlockingCodes) == 0 {
			t.Fatalf("transit element %+v %+v", e, e.Fit)
		}
		if e := by[h.berths[2]]; !containsS(e.Anomalies, engine.AnomalyVesselExceedsLimits) {
			t.Fatalf("anomalies %v", e.Anomalies)
		}
		if e := by[h.berths[4]]; e.Status != DayFree || !e.DepartureToday {
			t.Fatalf("departure element %+v", e)
		}
		if e := by[h.berths[5]]; e.Status != "block" || e.Vessel != nil {
			t.Fatalf("block element %+v", e)
		}
		if e := by[h.berths[1]]; e.Utilities == nil || e.Utilities.PowerA == nil || *e.Utilities.PowerA != 16 || !e.Utilities.Water {
			t.Fatalf("utilities %+v", e.Utilities)
		}
		ap := dv.AreaPlacements[0]
		if ap.Placement.X != 100 || len(ap.Cells) != 8 || ap.Vessel.ID != v3 {
			t.Fatalf("placement %+v", ap)
		}
		if dv.AnomalyCount == 0 {
			t.Fatal("no anomaly counted")
		}
		// a released night shows as released
		a := h.alloc(t, mustFirst(t, h, h.berths[1]))
		if _, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a.ID, From: d0.AddDays(1), To: d0.AddDays(2), Source: "office"}); err != nil {
			t.Fatal(err)
		}
		tomorrow := d0.AddDays(1)
		dv, err = h.s.GetDay(ctxT(), h.Owner, h.MarinaID, h.mapID, &tomorrow)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range dv.Elements {
			if e.ResourceID == h.berths[1] && (e.Status != DayReleased || e.Release == nil) {
				t.Fatalf("released element %+v", e)
			}
		}
		// the map occupancy range
		mo, err := h.s.GetMapOccupancy(ctxT(), h.Owner, h.MarinaID, h.mapID, nil, nil, nil)
		if err != nil || len(mo.Allocations) < 4 {
			t.Fatalf("%+v %v", mo, err)
		}
		far := d0.AddDays(500)
		if _, err := h.s.GetMapOccupancy(ctxT(), h.Owner, h.MarinaID, h.mapID, &d0, &far, nil); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("range too long: %v", err)
		}
		// another marina's map is not found
		_, other, otherOwner := h.OtherOrganization(t)
		if _, err := h.s.GetDay(ctxT(), otherOwner, other, h.mapID, nil); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
	})
}

func mustFirst(t *testing.T, h *harbour, resID string) string {
	t.Helper()
	as, err := store.ListAllocations(ctxT(), h.DB.DB, h.scope, store.AllocationFilter{ResourceIDs: []string{resID}, Status: store.AllocActive})
	if err != nil || len(as) == 0 {
		t.Fatalf("no allocation on %s", resID)
	}
	return as[0].ID
}

func containsS(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestOccupancy_WritesAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v := h.vessel(t, 1000, 350)
		_, line := h.contractLine(t, store.ContractKindMooring, "active", v, store.ResourceBerth, h.berths[1], d0, d0.AddDays(30))
		a := h.mustAllocate(t, NewAllocation{ContractLineID: &line, VesselID: strp(v), Kind: KindContract, Spec: berthSpec(h.berths[1], d0, d0.AddDays(30))})
		steps := []struct {
			action, event string
			run           func() error
		}{
			{"allocation.move", event.OccupancyMoved, func() error {
				_, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, Target: berthTarget(h.berths[2])})
				return err
			}},
			{"block.create", event.OccupancyAllocated, func() error {
				_, err := h.s.CreateBlock(ctxT(), h.Owner, h.MarinaID, BlockInput{Target: berthTarget(h.berths[5]), From: d0, To: d0, Reason: "lavori"})
				return err
			}},
			{"closure.create", event.ConfigOccupancyChange, func() error {
				_, err := h.s.CreateClosure(ctxT(), h.Owner, h.MarinaID, Closure{ScopeType: store.ClosureMarina, From: d0.AddDays(40), To: d0.AddDays(40), Reason: "festa"})
				return err
			}},
		}
		for _, s := range steps {
			auditBefore, eventsBefore := h.AuditCount(t, s.action), len(h.Events(t, s.event))
			if err := s.run(); err != nil {
				t.Fatalf("%s: %v", s.action, err)
			}
			if h.AuditCount(t, s.action) != auditBefore+1 || len(h.Events(t, s.event)) != eventsBefore+1 {
				t.Fatalf("%s: audit or outbox missing", s.action)
			}
		}
		// a failed operation writes neither
		auditBefore, eventsBefore := h.AuditCount(t, "allocation.move"), len(h.Events(t, event.OccupancyMoved))
		if _, err := h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, Target: berthTarget(h.berths[3])}); err == nil {
			t.Fatal("move of a superseded allocation")
		}
		if h.AuditCount(t, "allocation.move") != auditBefore || len(h.Events(t, event.OccupancyMoved)) != eventsBefore {
			t.Fatal("a failed move wrote audit or outbox")
		}
		_ = calendar.Date{}
	})
}
