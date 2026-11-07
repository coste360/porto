//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Occupancy row queries (m3-plan §3.1): insert with ON CONFLICT DO NOTHING, owners of a conflict, reclaim of
// expired holds, scoping, and the invariant queries spotting tampered rows. The engine itself is tested in
// internal/domain/occupancy.

func occupancySetup(t *testing.T, db *store.DB) (store.MarinaScope, store.MarinaScope, *store.Berth, string) {
	t.Helper()
	ctx := context.Background()
	_, a := seedOrgMarina(t, db, "alpha")
	_, b := seedOrgMarina(t, db, "beta")
	sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
	sb, _ := store.ResolveMarinaScope(ctx, db.DB, b)
	m := newMapRow(t, db, sa, 0)
	br := berthRow(m.ID, "B-1", 100)
	br.MarinaID = sa.ID()
	if _, err := db.NewInsert().Model(br).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: sa.ID(), Name: "Libeccio", HullType: "monohull_sail", LOACm: 700, BeamCm: 240,
		DimensionsSource: "declared", Engines: store.JSON("[]"), Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertVessel(ctx, db.DB, v); err != nil {
		t.Fatal(err)
	}
	return sa, sb, br, v.ID
}

func newAllocationRow(br *store.Berth, vesselID, kind string, first, last calendar.Date, exp time.Time) *store.Allocation {
	return &store.Allocation{
		ID: store.NewID(), VesselID: vesselID, ResourceType: store.ResourceBerth, ResourceID: br.ID, MapID: br.MapID,
		FirstNight: first, LastNight: last, Kind: kind, ExpiresAt: exp, CreatedAt: t0, UpdatedAt: t0,
	}
}

func nightsRows(res string, first calendar.Date, n int) []store.OccRow {
	out := make([]store.OccRow, n)
	for i := range n {
		out[i] = store.OccRow{Resource: res, Night: first.AddDays(i)}
	}
	return out
}

func TestStore_OccupancyRows(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		sa, sb, br, vid := occupancySetup(t, db)
		d0 := calendar.MustParse("2027-07-01")
		a1 := newAllocationRow(br, vid, store.AllocKindTransit, d0, d0.AddDays(4), time.Time{})
		a2 := newAllocationRow(br, vid, store.AllocKindTransit, d0.AddDays(3), d0.AddDays(6), time.Time{})
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.InsertAllocation(ctx, tx, sa, a1); err != nil {
				return err
			}
			if n, err := store.InsertOccupancyRows(ctx, tx, sa, a1, nightsRows(br.ID, d0, 5)); err != nil || n != 5 {
				t.Fatalf("insert %d %v", n, err)
			}
			if err := store.InsertAllocation(ctx, tx, sa, a2); err != nil {
				return err
			}
			// two of the four nights are taken: DO NOTHING inserts the other two, the caller sees the shortfall
			n, err := store.InsertOccupancyRows(ctx, tx, sa, a2, nightsRows(br.ID, d0.AddDays(3), 4))
			if err != nil || n != 2 {
				t.Fatalf("conflicting insert %d %v", n, err)
			}
			owners, err := store.OccupancyOwners(ctx, tx, sa, store.ResourceBerth, br.ID, d0.AddDays(3), d0.AddDays(6), nil, []string{a2.ID})
			if err != nil || len(owners) != 2 || owners[0].AllocationID != a1.ID {
				t.Fatalf("owners %+v %v", owners, err)
			}
			return errors.New("rollback")
		})
		if err == nil || err.Error() != "rollback" {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().TableExpr("berth_occupancies").Count(ctx); n != 0 {
			t.Fatalf("rollback left %d rows", n)
		}
		// another marina sees nothing and cannot read the allocation
		if _, err := store.GetAllocation(ctx, db.DB, sb, a1.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
	})
}

func TestStore_ReclaimExpiredHolds(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		sa, _, br, vid := occupancySetup(t, db)
		d0 := calendar.MustParse("2027-07-01")
		hold := newAllocationRow(br, vid, store.AllocKindHold, d0, d0.AddDays(9), t0.Add(time.Hour))
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.InsertAllocation(ctx, tx, sa, hold); err != nil {
				return err
			}
			_, err := store.InsertOccupancyRows(ctx, tx, sa, hold, nightsRows(br.ID, d0, 10))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		err = db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			got, err := store.ReclaimExpiredHolds(ctx, tx, sa, store.ResourceBerth, br.ID, d0.AddDays(5), d0.AddDays(5), t0)
			if err != nil || len(got) != 0 {
				t.Fatalf("live hold reclaimed: %v %v", got, err)
			}
			got, err = store.ReclaimExpiredHolds(ctx, tx, sa, store.ResourceBerth, br.ID, d0.AddDays(5), d0.AddDays(5), t0.Add(2*time.Hour))
			if err != nil || len(got) != 1 || got[0].Status != store.AllocExpired {
				t.Fatalf("reclaim %v %v", got, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().TableExpr("berth_occupancies").Count(ctx); n != 0 {
			t.Fatalf("expired hold kept %d rows", n)
		}
	})
}

func TestStore_InvariantQueriesSpotTampering(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		sa, _, br, vid := occupancySetup(t, db)
		d0 := calendar.MustParse("2027-07-01")
		a := newAllocationRow(br, vid, store.AllocKindTransit, d0, d0.AddDays(2), time.Time{})
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.InsertAllocation(ctx, tx, sa, a); err != nil {
				return err
			}
			_, err := store.InsertOccupancyRows(ctx, tx, sa, a, nightsRows(br.ID, d0, 3))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		has := func(q string) bool {
			vs, err := store.CheckOccupancyInvariants(ctx, db.DB, sa.ID(), d0)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vs {
				if v.Query == q {
					return true
				}
			}
			return false
		}
		if has("Q2") || has("Q3") {
			t.Fatal("coherent rows flagged")
		}
		if _, err := db.NewUpdate().TableExpr("berth_occupancies").Set("kind = 'contract'").Where("night = ?", d0).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if !has("Q3") {
			t.Fatal("kind mismatch not spotted")
		}
		if _, err := db.NewUpdate().TableExpr("allocations").Set("status = 'cancelled'").Where("id = ?", a.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if !has("Q2") {
			t.Fatal("rows of a cancelled allocation not spotted")
		}
	})
}
