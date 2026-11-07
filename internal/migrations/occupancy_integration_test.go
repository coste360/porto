//go:build integration

package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type occupancyFixture struct {
	marinaID, mapID, berthID, areaID, landID, vesselID string
}

// occupancyRows inserts a marina with a map holding one berth, one linear area and one land resource, and a
// vessel.
func occupancyRows(t *testing.T, db *store.DB) occupancyFixture {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	org := &store.Organization{ID: store.NewID(), Name: "O", BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
	m := &store.Marina{
		ID: store.NewID(), OrganizationID: org.ID, Slug: "porto-o", Name: "Porto O", Kind: "marina",
		Timezone: "Europe/Rome", Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"},
		Contacts: store.JSON(`{}`), Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1,
		Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mp := &store.Map{
		ID: store.NewID(), MarinaID: m.ID, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map",
		WidthCm: 100000, HeightCm: 100000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	b := &store.Berth{
		ID: store.NewID(), MarinaID: m.ID, MapID: mp.ID, Code: "A1", Side: "none", AnchorXCm: 1000, AnchorYCm: 1000,
		WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 400, MooringType: "mooring_line",
		Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	from, to := int64(0), int64(6000)
	a := &store.DynamicArea{
		ID: store.NewID(), MarinaID: m.ID, MapID: mp.ID, Kind: "linear", Code: "L1", Side: "left", FromCm: &from, ToCm: &to,
		MooringMode: "stern_to", CellCm: 50, MarginCm: 50, Supports: store.Strings{}, DepthProfile: store.JSON(`[]`),
		BlockedCells: store.JSON(`[]`), Exits: store.JSON(`[]`), CellCount: 120, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	l := &store.LandResource{
		ID: store.NewID(), MarinaID: m.ID, MapID: mp.ID, Kind: "parking", Code: "P1", XCm: 5000, YCm: 5000,
		WidthCm: 250, LengthCm: 500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: m.ID, Name: "Vessel", HullType: "monohull_sail", LOACm: 1134, BeamCm: 380,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []any{org, m, mp, b, a, l, v} {
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
	return occupancyFixture{marinaID: m.ID, mapID: mp.ID, berthID: b.ID, areaID: a.ID, landID: l.ID, vesselID: v.ID}
}

func newAllocation(f occupancyFixture, resourceType, resourceID string, first, last calendar.Date) *store.Allocation {
	now := store.UTC(time.Now())
	a := &store.Allocation{
		ID: store.NewID(), MarinaID: f.marinaID, VesselID: f.vesselID, ResourceType: resourceType, ResourceID: resourceID,
		MapID: f.mapID, FirstNight: first, LastNight: last, Kind: store.AllocKindTransit, Status: store.AllocActive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if resourceType == store.ResourceArea {
		x, y := int64(100), int64(0)
		a.XCm, a.YCm = &x, &y
	}
	return a
}

// TestMigrations_OccupancyPrimaryKeys: the three occupancy row tables enforce no double occupancy with their
// primary key on both dialects (INV-01, ADR 0003), and an element with occupancy rows cannot be deleted (RESTRICT).
func TestMigrations_OccupancyPrimaryKeys(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := occupancyRows(t, db)
		night := calendar.MustParse("2027-07-01")
		a1 := newAllocation(f, store.ResourceBerth, f.berthID, night, night)
		a2 := newAllocation(f, store.ResourceBerth, f.berthID, night, night)
		c1 := newAllocation(f, store.ResourceArea, f.areaID, night, night)
		c2 := newAllocation(f, store.ResourceArea, f.areaID, night, night)
		l1 := newAllocation(f, store.ResourceLand, f.landID, night, night)
		l2 := newAllocation(f, store.ResourceLand, f.landID, night, night)
		for _, a := range []*store.Allocation{a1, a2, c1, c2, l1, l2} {
			if _, err := db.NewInsert().Model(a).Exec(ctx); err != nil {
				t.Fatalf("insert allocation: %v", err)
			}
		}
		cases := []struct {
			name        string
			first, dupe any
		}{
			{
				"berth",
				&store.BerthOccupancy{MarinaID: f.marinaID, BerthID: f.berthID, Night: night, AllocationID: a1.ID, Kind: a1.Kind},
				&store.BerthOccupancy{MarinaID: f.marinaID, BerthID: f.berthID, Night: night, AllocationID: a2.ID, Kind: a2.Kind},
			},
			{
				"cell",
				&store.CellOccupancy{MarinaID: f.marinaID, AreaID: f.areaID, Cell: 3, Night: night, AllocationID: c1.ID, Kind: c1.Kind},
				&store.CellOccupancy{MarinaID: f.marinaID, AreaID: f.areaID, Cell: 3, Night: night, AllocationID: c2.ID, Kind: c2.Kind},
			},
			{
				"land",
				&store.LandOccupancy{MarinaID: f.marinaID, ResourceID: f.landID, Night: night, AllocationID: l1.ID, Kind: l1.Kind},
				&store.LandOccupancy{MarinaID: f.marinaID, ResourceID: f.landID, Night: night, AllocationID: l2.ID, Kind: l2.Kind},
			},
		}
		for _, c := range cases {
			if _, err := db.NewInsert().Model(c.first).Exec(ctx); err != nil {
				t.Fatalf("%s: first insert: %v", c.name, err)
			}
			_, err := db.NewInsert().Model(c.dupe).Exec(ctx)
			if !store.IsUniqueViolation(err) {
				t.Fatalf("%s: duplicate insert: want unique violation, got %v", c.name, err)
			}
			res, err := db.NewInsert().Model(c.dupe).On("CONFLICT DO NOTHING").Exec(ctx)
			if err != nil {
				t.Fatalf("%s: insert on conflict do nothing: %v", c.name, err)
			}
			if n, _ := res.RowsAffected(); n != 0 {
				t.Fatalf("%s: on conflict do nothing inserted %d rows", c.name, n)
			}
		}
		// Another cell of the same area and night is free.
		other := &store.CellOccupancy{MarinaID: f.marinaID, AreaID: f.areaID, Cell: 4, Night: night, AllocationID: c2.ID, Kind: c2.Kind}
		if _, err := db.NewInsert().Model(other).Exec(ctx); err != nil {
			t.Fatalf("other cell: %v", err)
		}
		// RESTRICT: an element with occupancy rows cannot be deleted.
		for table, id := range map[string]string{"berths": f.berthID, "dynamic_areas": f.areaID, "land_resources": f.landID} {
			_, err := db.NewDelete().Table(table).Where("id = ?", id).Exec(ctx)
			if !store.IsForeignKeyViolation(err) {
				t.Fatalf("delete %s with occupancy rows: want FK violation, got %v", table, err)
			}
		}
		// CASCADE: deleting the allocation removes its rows.
		if _, err := db.NewDelete().Model((*store.Allocation)(nil)).Where("id = ?", a1.ID).Exec(ctx); err != nil {
			t.Fatalf("delete allocation: %v", err)
		}
		n, err := db.NewSelect().Model((*store.BerthOccupancy)(nil)).Where("berth_id = ?", f.berthID).Count(ctx)
		if err != nil || n != 0 {
			t.Fatalf("berth rows after allocation delete = %d, %v", n, err)
		}
		// A block has no vessel and a reason; a vessel allocation has no block reason.
		bad := newAllocation(f, store.ResourceBerth, f.berthID, night, night)
		bad.Kind = store.AllocKindBlock
		if _, err := db.NewInsert().Model(bad).Exec(ctx); err == nil {
			t.Fatal("block with a vessel accepted")
		}
		hold := newAllocation(f, store.ResourceBerth, f.berthID, night, night)
		hold.Kind = store.AllocKindHold
		if _, err := db.NewInsert().Model(hold).Exec(ctx); err == nil {
			t.Fatal("hold without expires_at accepted")
		}
	})
}
