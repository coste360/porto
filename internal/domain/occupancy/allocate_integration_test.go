//go:build integration

package occupancy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestOccupancy_AllocateScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(10)
		v := h.vessel(t, 1100, 380)
		big := h.vessel(t, 1300, 420)
		cat := h.vesselHull(t, 1100, 380, fit.HullCatamaran)

		t.Run("berth", func(t *testing.T) {
			a := h.mustAllocate(t, transit(v, berthSpec(h.berths[1], d0, d0.AddDays(2))))
			if a.Status != store.AllocActive || a.Version != 1 || a.MapID != h.mapID {
				t.Fatalf("%+v", a)
			}
			if n := h.count(t, "berth_occupancies"); n != 3 {
				t.Fatalf("%d rows", n)
			}
			_, err := h.allocate(ctxT(), transit(v, berthSpec(h.berths[1], d0.AddDays(2), d0.AddDays(4))))
			var ce *ConflictError
			if !errors.As(err, &ce) || ce.ResourceID != h.berths[1] || !ce.From.Equal(d0.AddDays(2)) || !ce.To.Equal(d0.AddDays(2)) ||
				len(ce.AllocationIDs) != 1 || ce.AllocationIDs[0] != a.ID {
				t.Fatalf("conflict %+v %v", ce, err)
			}
			if n := h.count(t, "berth_occupancies"); n != 3 {
				t.Fatalf("conflict left rows: %d", n)
			}
		})
		t.Run("linear area", func(t *testing.T) {
			a := h.mustAllocate(t, transit(v, areaSpec(h.area, 100, d0, d0)))
			// beam 380 + margin 50 = 430 cm from x=100: cells 2..10 (100–530)
			rows, err := store.RowsOfAllocation(ctxT(), db.DB, h.scope, a)
			if err != nil || len(rows) != 9 || rows[0].Cell != 2 || rows[8].Cell != 10 {
				t.Fatalf("rows %+v %v", rows, err)
			}
			// a neighbour adjacent at 550 (cells 11..19) does not share a cell; one at 900 does
			h.mustAllocate(t, transit(v, areaSpec(h.area, 550, d0, d0)))
			_, err = h.allocate(ctxT(), transit(v, areaSpec(h.area, 900, d0, d0)))
			var ce *ConflictError
			if !errors.As(err, &ce) || len(ce.Cells) == 0 {
				t.Fatalf("conflict %v", err)
			}
		})
		t.Run("surface area", func(t *testing.T) {
			sp := engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: h.surface, Placement: &engine.Placement{X: 0, Y: 0, Rotation: 900}, First: d0, Last: d0}
			a := h.mustAllocate(t, transit(v, sp))
			rows, _ := store.RowsOfAllocation(ctxT(), db.DB, h.scope, a)
			// rotated: 380 × 1100 → 4 × 11 cells
			if len(rows) != 44 {
				t.Fatalf("%d rows", len(rows))
			}
		})
		t.Run("land", func(t *testing.T) {
			h.mustAllocate(t, NewAllocation{VesselID: strp(v), Kind: KindContract, Spec: engine.AllocSpec{ResourceType: engine.ResourceLand, ResourceID: h.lands[0], First: d0, Last: d0.AddDays(1)}})
			if n := h.count(t, "land_occupancies"); n != 2 {
				t.Fatalf("%d", n)
			}
		})
		t.Run("fit block and override", func(t *testing.T) {
			_, err := h.allocate(ctxT(), transit(big, berthSpec(h.berths[2], d0, d0)))
			var fe *FitError
			if !errors.As(err, &fe) || !fe.Result.Overridable || len(fe.Result.Blocking) != 2 {
				t.Fatalf("fit %v", err)
			}
			_, err = h.allocate(ctxT(), NewAllocation{VesselID: strp(big), Kind: KindTransit, Spec: berthSpec(h.berths[2], d0, d0), FitOverride: &FitOverride{UserID: h.OwnerID}})
			if !errors.Is(err, ErrOverrideReasonRequired) {
				t.Fatalf("empty reason: %v", err)
			}
			a := h.mustAllocate(t, NewAllocation{VesselID: strp(big), Kind: KindTransit, Spec: berthSpec(h.berths[2], d0, d0), FitOverride: &FitOverride{UserID: h.OwnerID, Reason: "misure in deroga"}})
			o := unmarshalJSON[FitOverride](a.FitOverride)
			if o.Reason == "" || len(o.Codes) != 2 {
				t.Fatalf("override %+v", o)
			}
			if _, err := h.allocate(ctxT(), transit(cat, berthSpec(h.berths[3], d0, d0))); !errors.Is(err, ErrFitBlocked) {
				t.Fatalf("catamaran: %v", err)
			}
			h.mustAllocate(t, transit(cat, berthSpec(h.berths[0], d0, d0)))
		})
		t.Run("override needs the permission", func(t *testing.T) {
			office := h.user(t, auth.RoleOffice)
			_, err := h.s.Move(ctxT(), office, h.MarinaID, MoveInput{AllocationID: store.NewID(), FitOverride: strp("x")})
			if !errors.Is(err, ErrOverrideForbidden) {
				t.Fatalf("office override: %v", err)
			}
		})
		t.Run("closure", func(t *testing.T) {
			now := store.UTC(h.Clock.Now())
			c := &store.Closure{ID: store.NewID(), ScopeType: store.ClosureResource, ScopeID: h.berths[4], ResourceType: store.ResourceBerth, FromDate: d0, ToDate: d0, Reason: "regata", Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := store.InsertClosure(ctxT(), db.DB, h.scope, c); err != nil {
				t.Fatal(err)
			}
			_, err := h.allocate(ctxT(), transit(v, berthSpec(h.berths[4], d0, d0.AddDays(1))))
			var cl *ClosureError
			if !errors.As(err, &cl) || cl.ClosureID != c.ID {
				t.Fatalf("closure %v", err)
			}
			// a stay already running is not an arrival
			na := transit(v, berthSpec(h.berths[4], d0.AddDays(-1), d0.AddDays(1)))
			h.mustAllocate(t, na)
		})
		t.Run("placement errors", func(t *testing.T) {
			for _, c := range []struct {
				spec   engine.AllocSpec
				reason string
			}{
				{areaSpec(h.area, 105, d0.AddDays(5), d0.AddDays(5)), PlacementUnsnapped},
				{areaSpec(h.area, 5800, d0.AddDays(5), d0.AddDays(5)), PlacementOutsideArea},
				{engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: h.area, First: d0.AddDays(5), Last: d0.AddDays(5)}, PlacementRequired},
				{engine.AllocSpec{ResourceType: engine.ResourceBerth, ResourceID: h.berths[5], Placement: &engine.Placement{}, First: d0.AddDays(5), Last: d0.AddDays(5)}, PlacementNotAllowed},
			} {
				_, err := h.allocate(ctxT(), transit(v, c.spec))
				var pe *PlacementError
				if !errors.As(err, &pe) || pe.Reason != c.reason {
					t.Errorf("%s: %v", c.reason, err)
				}
			}
			if _, err := h.allocate(ctxT(), transit(v, berthSpec(store.NewID(), d0, d0))); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown berth: %v", err)
			}
		})
		t.Run("block on area", func(t *testing.T) {
			b := h.mustAllocate(t, NewAllocation{Kind: KindBlock, BlockReason: "lavori", Spec: areaSpec(h.area, 3000, d0, d0.AddDays(1)), BlockSize: &layout.Vessel{LOA: 100, Beam: 450}})
			rows, _ := store.RowsOfAllocation(ctxT(), db.DB, h.scope, b)
			if len(rows) != 20 || b.VesselID != "" {
				t.Fatalf("block rows %d", len(rows))
			}
		})
		h.assertInvariants(t)
	})
}

func TestOccupancy_ExpiredHoldIsReclaimed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(3)
		v1, v2 := h.vessel(t, 1000, 350), h.vessel(t, 1000, 350)
		exp := h.Clock.Now().Add(time.Hour)
		hold := h.mustAllocate(t, NewAllocation{VesselID: strp(v1), Kind: KindHold, ExpiresAt: &exp, Spec: berthSpec(h.berths[1], d0, d0.AddDays(4))})
		if _, err := h.allocate(ctxT(), transit(v2, berthSpec(h.berths[1], d0.AddDays(2), d0.AddDays(6)))); !isTaken(err) {
			t.Fatalf("live hold must block: %v", err)
		}
		h.Clock.Advance(2 * time.Hour)
		// the job has not run: the sale reclaims the expired hold as a whole (INV-05)
		sale := h.mustAllocate(t, transit(v2, berthSpec(h.berths[1], d0.AddDays(2), d0.AddDays(6))))
		if a := h.alloc(t, hold.ID); a.Status != store.AllocExpired {
			t.Fatalf("hold %s", a.Status)
		}
		rows, _ := store.RowsOfAllocation(ctxT(), db.DB, h.scope, hold)
		if len(rows) != 0 {
			t.Fatalf("expired hold keeps %d rows", len(rows))
		}
		if err := db.RunInTx(ctxT(), func(ctx context.Context, tx bun.Tx) error {
			return h.s.ConvertTx(ctx, tx, h.scope, h.s.now(), []string{hold.ID}, KindContract, nil)
		}); !errors.Is(err, ErrHoldExpired) {
			t.Fatalf("convert expired: %v", err)
		}
		_ = sale
		h.assertInvariants(t)
	})
}

func TestOccupancy_ConflictRollsBackAll(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(1)
		v1, v2 := h.vessel(t, 1000, 350), h.vessel(t, 1000, 350)
		h.mustAllocate(t, transit(v1, berthSpec(h.berths[2], d0.AddDays(3), d0.AddDays(3))))
		allocs, rows := h.count(t, "allocations"), h.count(t, "berth_occupancies")
		err := db.RunInTx(ctxT(), func(ctx context.Context, tx bun.Tx) error {
			now := h.s.now()
			if _, err := h.s.AllocateTx(ctx, tx, h.scope, now, transit(v2, berthSpec(h.berths[1], d0, d0.AddDays(1)))); err != nil {
				return err
			}
			_, err := h.s.AllocateTx(ctx, tx, h.scope, now, transit(v2, berthSpec(h.berths[2], d0.AddDays(2), d0.AddDays(4))))
			return err
		})
		if !isTaken(err) {
			t.Fatalf("%v", err)
		}
		if h.count(t, "allocations") != allocs || h.count(t, "berth_occupancies") != rows {
			t.Fatal("a failed second segment left rows or allocations")
		}
		h.assertInvariants(t)
	})
}

func TestOccupancy_PrimitivesScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(2)
		v := h.vessel(t, 1000, 350)
		tx := func(fn func(ctx context.Context, tx bun.Tx) error) error { return db.RunInTx(ctxT(), fn) }
		exp := h.Clock.Now().Add(24 * time.Hour)
		a := h.mustAllocate(t, NewAllocation{VesselID: strp(v), Kind: KindHold, ExpiresAt: &exp, Spec: berthSpec(h.berths[1], d0, d0.AddDays(9))})
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.ConvertTx(ctx, tx, h.scope, h.s.now(), []string{a.ID}, KindTransit, nil)
		}); err != nil {
			t.Fatal(err)
		}
		if got := h.alloc(t, a.ID); got.Kind != store.AllocKindTransit || !got.ExpiresAt.IsZero() || got.Version != 2 {
			t.Fatalf("convert %+v", got)
		}
		h.assertInvariants(t)
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.ResizeTx(ctx, tx, h.scope, h.s.now(), a.ID, d0.AddDays(1), d0.AddDays(12))
		}); err != nil {
			t.Fatal(err)
		}
		if n := h.count(t, "berth_occupancies"); n != 12 {
			t.Fatalf("resize rows %d", n)
		}
		h.assertInvariants(t)
		other := h.mustAllocate(t, transit(h.vessel(t, 900, 300), berthSpec(h.berths[1], d0.AddDays(13), d0.AddDays(14))))
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.ResizeTx(ctx, tx, h.scope, h.s.now(), a.ID, d0.AddDays(1), d0.AddDays(13))
		}); !isTaken(err) {
			t.Fatalf("extend into a taken night: %v", err)
		}
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.TruncateTx(ctx, tx, h.scope, a.ID, d0.AddDays(5))
		}); err != nil {
			t.Fatal(err)
		}
		if got := h.alloc(t, a.ID); !got.LastNight.Equal(d0.AddDays(4)) || h.count(t, "berth_occupancies") != 4+2 {
			t.Fatalf("truncate %+v", got)
		}
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.CancelTx(ctx, tx, h.scope, other.ID, StatusCancelled)
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			return h.s.CancelTx(ctx, tx, h.scope, other.ID, StatusCancelled)
		}); !errors.Is(err, ErrAllocationNotActive) {
			t.Fatalf("cancel twice: %v", err)
		}
		hold := h.mustAllocate(t, NewAllocation{VesselID: strp(v), Kind: KindHold, ExpiresAt: timep(h.Clock.Now().Add(time.Minute)), Spec: berthSpec(h.berths[3], d0, d0)})
		h.Clock.Advance(2 * time.Minute)
		var expired []store.Allocation
		if err := tx(func(ctx context.Context, tx bun.Tx) error {
			var err error
			expired, err = h.s.ExpireHoldsTx(ctx, tx, h.scope, h.s.now())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(expired) != 1 || expired[0].ID != hold.ID {
			t.Fatalf("expired %+v", expired)
		}
		if n, _ := store.CountExpiredHoldRows(ctxT(), db.DB, h.MarinaID, h.Clock.Now()); n != 0 {
			t.Fatalf("Q11: %d", n)
		}
		h.assertInvariants(t)
	})
}
