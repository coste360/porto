//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// contractFixture inserts an organization, a marina, a vessel, a customer and a berth.
type contractFixture struct {
	scope    store.MarinaScope
	marina   string
	vessel   string
	customer string
	berth    string
	mapID    string
	now      time.Time
}

func newContractFixture(t *testing.T, db *store.DB, slug string) contractFixture {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Date(2027, 6, 1, 10, 0, 0, 0, time.UTC))
	org := &store.Organization{ID: store.NewID(), Name: "Org " + slug, BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
	m := &store.Marina{
		ID: store.NewID(), OrganizationID: org.ID, Slug: slug, Name: "Marina", Kind: "marina",
		Timezone: "Europe/Rome", Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"},
		Contacts: store.JSON(`{}`), Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1,
		Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mp := &store.Map{
		ID: store.NewID(), MarinaID: m.ID, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map", WidthCm: 100000,
		HeightCm: 100000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	b := &store.Berth{
		ID: store.NewID(), MarinaID: m.ID, MapID: mp.ID, Code: "A1", Side: "none", WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200,
		MaxBeamCm: 400, MooringType: "mooring_line", Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: m.ID, Name: "Libeccio", HullType: "monohull_sail", LOACm: 1134, BeamCm: 380,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), SearchText: "libeccio", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	c := &store.Customer{
		ID: store.NewID(), MarinaID: m.ID, Kind: "person", LastName: "Rossi", BillingAddress: store.JSON(`{}`), Locale: "it",
		Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []any{org, m, mp, b, v, c} {
		if _, err := db.NewInsert().Model(r).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
	s, err := store.ResolveMarinaScope(ctx, db.DB, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return contractFixture{scope: s, marina: m.ID, vessel: v.ID, customer: c.ID, berth: b.ID, mapID: mp.ID, now: now}
}

func (f contractFixture) contract(kind, status string, number int64) *store.Contract {
	c := &store.Contract{
		ID: store.NewID(), MarinaID: f.marina, Kind: kind, Number: number, Year: 2027, Status: status, CustomerID: f.customer,
		VesselID: f.vessel, StartsOn: calendar.MustParse("2027-06-10"), EndsOn: calendar.MustParse("2027-06-13"), Channel: "desk",
		Policy: store.JSON(`{}`), Version: 1, CreatedAt: f.now, UpdatedAt: f.now,
	}
	if kind == store.ContractKindMooring {
		c.Term = "annual"
	}
	return c
}

func TestStore_ContractsScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := newContractFixture(t, db, "marina-c1")
		g := newContractFixture(t, db, "marina-c2")

		// Numbering per marina, series and year.
		for i, want := range []int64{1, 2, 3} {
			n, err := store.NextNumber(ctx, db.DB, f.scope, "transit", 2027)
			if err != nil || n != want {
				t.Fatalf("next %d: %d %v", i, n, err)
			}
		}
		if n, _ := store.NextNumber(ctx, db.DB, f.scope, "transit", 2028); n != 1 {
			t.Fatalf("new year: %d", n)
		}
		if n, _ := store.NextNumber(ctx, db.DB, g.scope, "transit", 2027); n != 1 {
			t.Fatalf("other marina: %d", n)
		}

		c := f.contract(store.ContractKindTransit, "held", 1)
		if err := store.InsertContract(ctx, db.DB, c); err != nil {
			t.Fatal(err)
		}
		dup := f.contract(store.ContractKindTransit, "held", 1)
		if err := store.InsertContract(ctx, db.DB, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate number: %v", err)
		}
		// The CHECKs tie statuses to kinds and confirmed transits to a customer.
		bad := f.contract(store.ContractKindTransit, "active", 9)
		if err := store.InsertContract(ctx, db.DB, bad); err == nil {
			t.Fatal("transit with a mooring status")
		}
		noCust := f.contract(store.ContractKindTransit, "confirmed", 10)
		noCust.CustomerID = ""
		if err := store.InsertContract(ctx, db.DB, noCust); err == nil {
			t.Fatal("confirmed transit without customer")
		}
		// Scoping: another marina does not see it.
		if _, err := store.GetContract(ctx, db.DB, g.scope, c.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
		// Version-checked update.
		c.Status = "confirmed"
		if err := store.UpdateContract(ctx, db.DB, f.scope, 1, c); err != nil || c.Version != 2 {
			t.Fatalf("update %v v%d", err, c.Version)
		}
		if err := store.UpdateContract(ctx, db.DB, f.scope, 1, c); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale update: %v", err)
		}
		// Lines, allocations and the join.
		line := store.ContractLine{
			ID: store.NewID(), MarinaID: f.marina, ContractID: c.ID, Seq: 1, LineKind: "resource", ResourceType: "berth",
			ResourceID: f.berth, QtyMilli: 1000, CreatedAt: f.now, UpdatedAt: f.now,
		}
		if err := store.InsertContractLines(ctx, db.DB, []store.ContractLine{line}); err != nil {
			t.Fatal(err)
		}
		al := &store.Allocation{
			ID: store.NewID(), MarinaID: f.marina, ContractLineID: line.ID, VesselID: f.vessel, ResourceType: "berth",
			ResourceID: f.berth, MapID: f.mapID, FirstNight: c.StartsOn, LastNight: calendar.MustParse("2027-06-12"),
			Kind: "transit", Status: "active", Version: 1, CreatedAt: f.now, UpdatedAt: f.now,
		}
		if _, err := db.NewInsert().Model(al).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := store.AllocationsOfContracts(ctx, db.DB, f.scope, []string{c.ID})
		if err != nil || len(got) != 1 || got[0].ContractID != c.ID || got[0].ID != al.ID {
			t.Fatalf("allocations %+v %v", got, err)
		}
		if cid, _ := store.ContractOfAllocation(ctx, db.DB, f.scope, al.ID); cid != c.ID {
			t.Fatalf("contract of allocation %q", cid)
		}
		refs, err := store.ResourceRefs(ctx, db.DB, f.scope, []string{f.berth})
		if err != nil || refs[f.berth].Code != "A1" || refs[f.berth].MapID != f.mapID {
			t.Fatalf("refs %+v %v", refs, err)
		}
		if refs, _ := store.ResourceRefs(ctx, db.DB, g.scope, []string{f.berth}); len(refs) != 0 {
			t.Fatal("refs across marinas")
		}
		if seq, _ := store.NextLineSeq(ctx, db.DB, f.scope, c.ID); seq != 2 {
			t.Fatalf("next seq %d", seq)
		}
		on := calendar.MustParse("2027-06-11")
		page, err := store.ListContracts(ctx, db.DB, f.scope, store.ContractFilter{Kind: "transit", ActiveOn: &on, Search: "libec"}, "", 10)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("list %+v %v", page.Items, err)
		}
		if a, err := store.VesselAllocationOn(ctx, db.DB, f.scope, f.vessel, on); err != nil || a.ID != al.ID {
			t.Fatalf("vessel allocation %v", err)
		}
		mv, err := store.MovementAllocations(ctx, db.DB, f.scope, c.StartsOn, true, "")
		if err != nil || len(mv) != 1 || mv[0].ContractID != c.ID {
			t.Fatalf("movements %+v %v", mv, err)
		}
		// Settings default lazily.
		st, err := store.ContractOccupancySettings(ctx, db.DB, f.scope, f.now)
		if err != nil || st.DeskHoldHours != 24 || st.DraftHoldDays != 7 {
			t.Fatalf("settings %+v %v", st, err)
		}
	})
}

func TestStore_PresenceStateUpsert(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := newContractFixture(t, db, "marina-p1")
		for i, state := range []string{"in_port", "out", "in_port"} {
			p := &store.Presence{
				ID: store.NewID(), MarinaID: f.marina, VesselID: f.vessel, Event: map[string]string{"in_port": "arrived", "out": "departed"}[state],
				At: f.now.Add(time.Duration(i) * time.Hour), Night: calendar.MustParse("2027-06-01"), Source: "staff", CreatedAt: f.now, UpdatedAt: f.now,
			}
			if err := store.InsertPresence(ctx, db.DB, p); err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertVesselPresence(ctx, db.DB, &store.VesselPresence{
				VesselID: f.vessel, MarinaID: f.marina, State: state, Since: p.At, LastPresenceID: p.ID, UpdatedAt: f.now,
			}); err != nil {
				t.Fatal(err)
			}
			vp, err := store.GetVesselPresence(ctx, db.DB, f.scope, f.vessel)
			if err != nil || vp.State != state || vp.LastPresenceID != p.ID {
				t.Fatalf("state %d: %+v %v", i, vp, err)
			}
		}
		n := calendar.MustParse("2027-06-01")
		page, err := store.ListPresences(ctx, db.DB, f.scope, store.PresenceFilter{Night: &n}, "", 2)
		if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
			t.Fatalf("page %+v %v", page, err)
		}
	})
}
