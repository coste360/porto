//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Element rows are written only through harbor.(*Service).applyDocument (TestHarbor_SingleWritePath); these
// tests cover the other harbour queries and insert element rows directly where they need some.

func newMapRow(t *testing.T, db *store.DB, s store.MarinaScope, order int) *store.Map {
	t.Helper()
	m := &store.Map{
		ID: store.NewID(), Kind: "water", Name: store.JSON(`{"it":"Mappa"}`), Icon: "map", WidthCm: 10000, HeightCm: 10000,
		GridCm: 50, WaterPolygons: store.JSON("[]"), LandPolygons: store.JSON("[]"), SortOrder: order, Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertMap(context.Background(), db.DB, s, m); err != nil {
		t.Fatal(err)
	}
	return m
}

func berthRow(mapID, code string, x int64) *store.Berth {
	return &store.Berth{
		ID: store.NewID(), MapID: mapID, Code: code, Side: "none", AnchorXCm: x, AnchorYCm: 100, WidthCm: 300, LengthCm: 800,
		MaxLOACm: 800, MaxBeamCm: 250, MooringType: "mooring_line", Active: true, CreatedAt: t0, UpdatedAt: t0,
	}
}

func TestStore_MapsScopedAndVersioned(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		_, b := seedOrgMarina(t, db, "beta")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		sb, _ := store.ResolveMarinaScope(ctx, db.DB, b)
		if n, err := store.NextMapSortOrder(ctx, db.DB, sa); err != nil || n != 0 {
			t.Fatalf("next order %d %v", n, err)
		}
		m1 := newMapRow(t, db, sa, 0)
		m2 := newMapRow(t, db, sa, 1)
		if n, _ := store.NextMapSortOrder(ctx, db.DB, sa); n != 2 {
			t.Fatalf("next order %d", n)
		}
		if _, err := store.GetMap(ctx, db.DB, sb, m1.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if _, err := store.GetMap(ctx, db.DB, sa, "not-a-uuid"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("bad id: %v", err)
		}
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			m, err := store.LockMap(ctx, tx, sa, m1.ID)
			if err != nil {
				return err
			}
			m.GridCm = 100
			if err := store.UpdateMap(ctx, tx, sa, m, 1); err != nil {
				return err
			}
			if m.Version != 2 {
				t.Errorf("version %d", m.Version)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		m1.GridCm = 10
		if err := store.UpdateMap(ctx, db.DB, sa, m1, 1); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale update: %v", err)
		}
		if err := store.UpdateMap(ctx, db.DB, sb, m1, 2); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("update through another marina: %v", err)
		}
		if err := store.SetMapSortOrders(ctx, db.DB, sa, []string{m2.ID, m1.ID}, t0); err != nil {
			t.Fatal(err)
		}
		list, _ := store.ListMaps(ctx, db.DB, sa)
		if len(list) != 2 || list[0].ID != m2.ID {
			t.Fatalf("list %v", list)
		}
		if _, err := db.NewInsert().Model(berthRow(m1.ID, "B-1", 100)).Value("marina_id", "?", a).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		counts, err := store.CountMapElements(ctx, db.DB, sa, "")
		if err != nil || counts[m1.ID].Berths != 1 || counts[m1.ID].Occupiable() != 1 || counts[m2.ID].Berths != 0 {
			t.Fatalf("counts %+v %v", counts, err)
		}
		el, err := store.LoadMapElements(ctx, db.DB, sa, m1.ID)
		if err != nil || len(el.Berths) != 1 || el.Berths[0].Code != "B-1" {
			t.Fatalf("elements %+v %v", el, err)
		}
		page, err := store.ListBerths(ctx, db.DB, sa, store.BerthFilter{Q: "b-"}, "", 10)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("berths %+v %v", page, err)
		}
		if page, _ := store.ListBerths(ctx, db.DB, sb, store.BerthFilter{}, "", 10); len(page.Items) != 0 {
			t.Fatal("berths of another marina listed")
		}
		if err := store.DeleteMap(ctx, db.DB, sa, m1.ID, 1); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
		if err := store.DeleteMap(ctx, db.DB, sa, m1.ID, 2); err != nil {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().Table("berths").Count(ctx); n != 0 {
			t.Fatal("berths not cascaded")
		}
	})
}

func TestStore_BerthGroupsAndSettings(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		g := &store.BerthGroup{ID: store.NewID(), Code: "PONT-A", Name: store.JSON(`{"it":"A"}`), Kind: "structure", Version: 1, CreatedAt: t0, UpdatedAt: t0}
		if err := store.InsertBerthGroup(ctx, db.DB, sa, g); err != nil {
			t.Fatal(err)
		}
		dup := *g
		dup.ID = store.NewID()
		if err := store.InsertBerthGroup(ctx, db.DB, sa, &dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate code: %v", err)
		}
		m := newMapRow(t, db, sa, 0)
		members := []store.BerthGroupMember{{ID: store.NewID(), GroupID: g.ID, ResourceType: "berth", ResourceID: store.NewID(), CreatedAt: t0}}
		if err := store.ReplaceGroupMembers(ctx, db.DB, sa, m.ID, members); err != nil {
			t.Fatal(err)
		}
		counts, _ := store.GroupMemberCounts(ctx, db.DB, sa)
		if counts[g.ID]["berth"] != 1 {
			t.Fatalf("counts %v", counts)
		}
		bad := []store.BerthGroupMember{{ID: store.NewID(), GroupID: store.NewID(), ResourceType: "berth", ResourceID: store.NewID(), CreatedAt: t0}}
		if err := store.ReplaceGroupMembers(ctx, db.DB, sa, m.ID, bad); err == nil {
			t.Fatal("member of an unknown group accepted")
		}
		g.Kind = "transit"
		if err := store.UpdateBerthGroup(ctx, db.DB, sa, g, 1); err != nil || g.Version != 2 {
			t.Fatalf("update %v", err)
		}
		if err := store.DeleteBerthGroup(ctx, db.DB, sa, g.ID, 2); err != nil {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().Table("berth_group_members").Count(ctx); n != 0 {
			t.Fatal("members not cascaded")
		}
		if _, err := store.GetHarborSettings(ctx, db.DB, sa); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("no settings yet: %v", err)
		}
		h := &store.HarborSettings{
			ID: store.NewID(), PriceClassRule: store.JSON(`{"basis":"width","bands":[],"fallback":"X"}`), LOAToleranceCm: 50,
			DefaultGridCm: 50, Version: 1, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := store.InsertHarborSettings(ctx, db.DB, sa, h); err != nil {
			t.Fatal(err)
		}
		h.LOAToleranceCm = 70
		if err := store.UpdateHarborSettings(ctx, db.DB, sa, h, 2); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if err := store.UpdateHarborSettings(ctx, db.DB, sa, h, 1); err != nil || h.Version != 2 {
			t.Fatalf("update %v", err)
		}
	})
}

// TestStore_ElementReferencesStayInMarina: the composite FKs (structure_id, marina_id), (pedestal_id,
// marina_id) and (parent_id, marina_id) refuse a reference to a row of another marina at commit (INV-73), and
// a removed structure may still be referenced inside the transaction as long as the reference is gone at commit.
func TestStore_ElementReferencesStayInMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		_, b := seedOrgMarina(t, db, "beta")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		sb, _ := store.ResolveMarinaScope(ctx, db.DB, b)
		ma, mb := newMapRow(t, db, sa, 0), newMapRow(t, db, sb, 0)
		structure := func(m *store.Map, marinaID, code string) *store.HarborStructure {
			return &store.HarborStructure{
				ID: store.NewID(), MarinaID: marinaID, MapID: m.ID, Kind: "pier", Code: code, Geometry: store.JSON(`{"points":[],"closed":false}`),
				Attributes: store.JSON(`{}`), CreatedAt: t0, UpdatedAt: t0,
			}
		}
		foreign := structure(mb, b, "PB")
		if _, err := db.NewInsert().Model(foreign).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		insertBerth := func(structureID string) error {
			return db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				r := berthRow(ma.ID, "X-"+structureID[len(structureID)-4:], 100)
				r.StructureID = structureID
				_, err := tx.NewInsert().Model(r).Value("marina_id", "?", a).Exec(ctx)
				return err
			})
		}
		if err := insertBerth(foreign.ID); err == nil {
			t.Fatal("berth of marina A referencing a structure of marina B was committed")
		}
		child := structure(ma, a, "CA")
		child.ParentID = foreign.ID
		if _, err := db.NewInsert().Model(child).Exec(ctx); err == nil {
			t.Fatal("structure of marina A with a parent in marina B was committed")
		}
		own := structure(ma, a, "PA")
		if _, err := db.NewInsert().Model(own).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if err := insertBerth(own.ID); err != nil {
			t.Fatalf("same-marina reference refused: %v", err)
		}
		// Delete the structure first, then clear the reference: valid at commit (deferred check).
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewDelete().Table("harbor_structures").Where("id = ?", own.ID).Exec(ctx); err != nil {
				return err
			}
			_, err := tx.NewUpdate().Table("berths").Set("structure_id = NULL").Where("structure_id = ?", own.ID).Exec(ctx)
			return err
		})
		if err != nil {
			t.Fatalf("delete then unlink: %v", err)
		}
	})
}
