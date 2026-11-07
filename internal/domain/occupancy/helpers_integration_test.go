//go:build integration

package occupancy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// harbour is a small test harbour: one map with 10 berths (12 m × 4.5 m, max LOA 12 m, max beam 4 m, 3 m deep;
// berth 0 multihull_ok, berths 8–9 transit_only), a 60 m linear area (cells 50 cm, margin 50 cm, stern-to), a
// 30 × 20 m surface area (cells 100 cm) and three land resources (two parking spots and a cradle).
type harbour struct {
	*fixture.Env
	s       *Service
	scope   store.MarinaScope
	mapID   string
	berths  []string
	area    string
	surface string
	lands   []string
	today   calendar.Date
	seq     int
}

func ctxT() context.Context { return fixture.Ctx() }

func newHarbour(t testing.TB, db *store.DB) *harbour {
	t.Helper()
	e := fixture.New(t, db)
	h := &harbour{Env: e, s: NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})}
	ctx := ctxT()
	var err error
	if h.scope, err = store.SystemMarinaScope(ctx, db.DB, e.MarinaID); err != nil {
		t.Fatal(err)
	}
	loc, err := marinaLocation(ctx, db.DB, h.scope)
	if err != nil {
		t.Fatal(err)
	}
	h.today = calendar.In(e.Clock.Now(), loc)
	now := store.UTC(e.Clock.Now())
	m := &store.Map{
		ID: store.NewID(), Kind: "water", Name: store.MustJSON(map[string]string{"it": "Porto"}), Icon: "map", WidthCm: 100000,
		HeightCm: 100000, GridCm: 50, WaterPolygons: store.MustJSON([]any{}), LandPolygons: store.MustJSON([]any{}), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertMap(ctx, db.DB, h.scope, m); err != nil {
		t.Fatal(err)
	}
	h.mapID = m.ID
	for i := range 10 {
		b := &store.Berth{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: m.ID, Code: fmt.Sprintf("A-%02d", i+1), Side: "none", AnchorXCm: int64(1000 + 500*i),
			AnchorYCm: 1000, WidthCm: 450, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 400, DepthCm: 300, MooringType: "mooring_line",
			PowerA: 16, Water: true, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if i == 0 {
			b.Attributes = store.Strings{TagMultihullOK}
		}
		if i >= 8 {
			b.Attributes = store.Strings{TagTransitOnly}
		}
		if _, err := db.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		h.berths = append(h.berths, b.ID)
	}
	from, to := int64(0), int64(6000)
	a := &store.DynamicArea{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: m.ID, Kind: "linear", Code: "L-1", Side: "left", FromCm: &from, ToCm: &to,
		MooringMode: "stern_to", CellCm: 50, MarginCm: 50, MaxLOACm: 2000, Supports: store.Strings{}, DepthProfile: store.MustJSON([]any{}),
		BlockedCells: store.MustJSON([]int{}), Exits: store.MustJSON([]int{}), CellCount: 120, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(a).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	h.area = a.ID
	outline := []geo.Point{{X: 20000, Y: 20000}, {X: 23000, Y: 20000}, {X: 23000, Y: 22000}, {X: 20000, Y: 22000}}
	sf := &store.DynamicArea{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: m.ID, Kind: "surface", Code: "S-1", Outline: store.MustJSON(outline),
		CellCm: 100, MarginCm: 0, Supports: store.Strings{}, DepthProfile: store.MustJSON([]any{}), BlockedCells: store.MustJSON([]int{}),
		Exits: store.MustJSON([]int{}), CellCount: 600, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(sf).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	h.surface = sf.ID
	for i, k := range []string{"parking", "parking", "cradle"} {
		l := &store.LandResource{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: m.ID, Kind: k, Code: fmt.Sprintf("P-%d", i+1), XCm: int64(30000 + 400*i), YCm: 30000,
			WidthCm: 250, LengthCm: 500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if k == "cradle" {
			l.WidthCm, l.LengthCm = 450, 1300
		}
		if _, err := db.NewInsert().Model(l).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		h.lands = append(h.lands, l.ID)
	}
	return h
}

// vessel inserts a vessel with the given size (and hull "monohull_sail").
func (h *harbour) vessel(t testing.TB, loa, beam int64) string {
	t.Helper()
	return h.vesselHull(t, loa, beam, "monohull_sail")
}

func (h *harbour) vesselHull(t testing.TB, loa, beam int64, hull string) string {
	t.Helper()
	h.seq++
	now := store.UTC(h.Clock.Now())
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: h.MarinaID, Name: fmt.Sprintf("Barca %d", h.seq), HullType: hull, LOACm: loa, BeamCm: beam, DraftCm: 150,
		DimensionsSource: "declared", Engines: store.MustJSON([]any{}), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertVessel(ctxT(), h.DB.DB, v); err != nil {
		t.Fatal(err)
	}
	return v.ID
}

// contractLine inserts an active mooring contract (or a transit) with one resource line and returns the line id.
func (h *harbour) contractLine(t failer, kind, status, vesselID, resType, resID string, first, last calendar.Date) (contractID, lineID string) {
	t.Helper()
	h.seq++
	now := store.UTC(h.Clock.Now())
	c := &store.Contract{
		ID: store.NewID(), MarinaID: h.MarinaID, Kind: kind, Number: int64(h.seq), Year: 2026, Status: status, VesselID: vesselID,
		StartsOn: first, EndsOn: last.AddDays(1), Channel: "desk", Policy: store.MustJSON(map[string]any{}), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if kind == store.ContractKindMooring {
		c.Term = "annual"
	}
	if err := store.InsertContract(ctxT(), h.DB.DB, c); err != nil {
		t.Fatal(err)
	}
	l := store.ContractLine{
		ID: store.NewID(), MarinaID: h.MarinaID, ContractID: c.ID, Seq: 1, LineKind: "resource", ResourceType: resType, ResourceID: resID,
		QtyMilli: 1000, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertContractLines(ctxT(), h.DB.DB, []store.ContractLine{l}); err != nil {
		t.Fatal(err)
	}
	return c.ID, l.ID
}

// failer is the part of testing.TB the helpers need (also implemented by *rapid.T).
type failer interface {
	Helper()
	Fatal(args ...any)
}

func berthSpec(id string, first, last calendar.Date) engine.AllocSpec {
	return engine.AllocSpec{ResourceType: engine.ResourceBerth, ResourceID: id, First: first, Last: last}
}

func areaSpec(id string, x geo.Cm, first, last calendar.Date) engine.AllocSpec {
	return engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: id, Placement: &engine.Placement{X: x}, First: first, Last: last}
}

// allocate runs AllocateTx in its own transaction.
func (h *harbour) allocate(ctx context.Context, a NewAllocation) (*store.Allocation, error) {
	var out *store.Allocation
	err := h.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		out, err = h.s.AllocateTx(ctx, tx, h.scope, h.s.now(), a)
		return err
	})
	return out, err
}

func (h *harbour) mustAllocate(t testing.TB, a NewAllocation) *store.Allocation {
	t.Helper()
	if a.UserID == "" {
		a.UserID = h.OwnerID
	}
	out, err := h.allocate(ctxT(), a)
	if err != nil {
		t.Fatalf("allocate %+v: %v", a.Spec, err)
	}
	return out
}

func strp(s string) *string { return &s }

func timep(t time.Time) *time.Time { return &t }

// transit returns a NewAllocation of kind transit for a vessel.
func transit(vesselID string, spec engine.AllocSpec) NewAllocation {
	return NewAllocation{VesselID: strp(vesselID), Kind: KindTransit, Spec: spec, Arrival: true}
}

// assertInvariants fails on any violation of the invariant queries of m3-plan §6.
func (h *harbour) assertInvariants(t testing.TB) {
	t.Helper()
	vs, err := store.CheckOccupancyInvariants(ctxT(), h.DB.DB, h.MarinaID, h.today)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func (h *harbour) count(t testing.TB, table string) int {
	t.Helper()
	n, err := h.DB.NewSelect().TableExpr(table).Where("marina_id = ?", h.MarinaID).Count(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func (h *harbour) alloc(t testing.TB, id string) *store.Allocation {
	t.Helper()
	a, err := store.GetAllocation(ctxT(), h.DB.DB, h.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func isTaken(err error) bool { return errors.Is(err, ErrResourceTaken) }

func (h *harbour) user(t testing.TB, role auth.Role) *auth.Principal {
	t.Helper()
	h.seq++
	return h.AddUser(t, fmt.Sprintf("%s%d@porto.test", role, h.seq), role, h.OrgID, h.MarinaID)
}
