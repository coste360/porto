//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M3_SeasonSimulation (spec 32.3 M3 acceptance, m3-plan §9.4, §13 D30): a season of desk work
// through the HTTP API of the real app with a seeded generator. Every operation either succeeds or fails with an
// expected problem type and no effect; an oracle built only from the responses of successful operations (and the
// known effect of hold expiry) is compared exactly, cell by cell, with the occupancy rows; the invariant queries
// run every 100 operations and at the end; every counted status change has its audit row and outbox event.

const (
	seasonDefaultOps = 2000
	seasonMinOK      = 1400 // m3-plan §9.4, §13 D30
)

// seasonExpected are the problem types of m3-plan §9.4 that any operation may meet.
var seasonExpected = []string{
	"resource_taken", "fit_blocked", "placement_invalid", "closure_active", "invalid_transition", "version_conflict",
	"undo_unavailable", "quote_expired",
}

// seasonOpExpected are the problems expected for one operation kind only: a release overlapping another one or a
// revoke of a release that a move clipped (the generator does not track release ids across moves), a stay change
// of a held transit whose hold lapsed before the job reconciled it.
var seasonOpExpected = map[string][]string{
	"release": {"release_invalid"},
	"revoke":  {"release_invalid"},
	"stay":    {"hold_expired"},
}

// seasonAlloc is an allocation as the last successful response described it.
type seasonAlloc struct {
	ID, Type, Resource, Status, Kind, Contract, Vessel string
	X, Y, Rot                                          *int64
	First, Last                                        calendar.Date
	Released                                           [][2]calendar.Date
	Expires                                            time.Time
	Version                                            int
}

// seasonChange is a status change the generator made: its audit action and outbox events (INV-30).
type seasonChange struct {
	entity, audit string
	events        []string
}

type season struct {
	t       *testing.T
	db      *store.DB
	h       *harness
	clock   *clock.Fake
	rng     *rand.Rand
	c       *client
	M       string
	marina  string
	mapID   string
	loc     *time.Location
	berths  []seasonBerth
	linear  []string
	surface string
	layouts map[string]layout.Area
	lands   []string
	vessels []seasonVessel
	dims    map[string]layout.Vessel // vessel id → declared size
	inPort  map[string]bool          // vessel id → in port after the last recorded presence
	custs   []string
	// oracle: allocation id → allocation as last returned by a successful mutation.
	oracle map[string]seasonAlloc
	// contracts, transits and quotes known to the generator.
	transits   []string
	moorings   []string
	quotes     []string
	quoteHolds map[string][]string // quote id → hold allocation ids
	blocks     []string
	releases   []string
	undos      []seasonUndo
	ok, fail   map[string]int
	ledger     []seasonChange
}

type seasonUndo struct {
	id      string
	until   time.Time
	created []string // allocations the operation created (untouched ⇒ undoable)
}

type seasonBerth struct {
	ID              string
	MaxLOA, MaxBeam int64
}

type seasonVessel struct {
	ID        string
	LOA, Beam int64
	Customer  string
}

func (s *season) today() calendar.Date { return calendar.In(s.clock.Now(), s.loc) }

func (s *season) changed(entity, audit string, events ...string) {
	s.ledger = append(s.ledger, seasonChange{entity: entity, audit: audit, events: events})
}

func TestAcceptance_M3_SeasonSimulation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		seed := uint64(20260401)
		if v := os.Getenv("PORTO_SEASON_SEED"); v != "" {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			seed = n
		}
		ops := seasonDefaultOps
		if v := os.Getenv("PORTO_SEASON_OPS"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatal(err)
			}
			ops = n
		}
		t.Logf("season seed %d, %d operations", seed, ops)
		loc, _ := calendar.LoadLocation("Europe/Rome")
		c := clock.NewFake(time.Date(2027, 3, 25, 9, 0, 0, 0, loc))
		h := newRegistryHarness(t, db, c)
		own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Stagione SRL", MarinaName: "Marina Stagione",
			MarinaSlug: "stagione", Timezone: "Europe/Rome",
		})
		if err != nil {
			t.Fatal(err)
		}
		s := &season{
			t: t, db: db, h: h, clock: c, rng: rand.New(rand.NewPCG(seed, 3)), M: A + "/marinas/" + own.MarinaID, //nolint:gosec // seeded test generator
			marina: own.MarinaID, loc: loc, oracle: map[string]seasonAlloc{}, ok: map[string]int{}, fail: map[string]int{},
			layouts: map[string]layout.Area{}, dims: map[string]layout.Vessel{}, inPort: map[string]bool{}, quoteHolds: map[string][]string{},
		}
		w := &deskWorld{h: h, clock: c, marina: own.MarinaID, M: s.M, owner: h.cookieClient(), clients: map[string]*client{}}
		w.owner.login(t, "owner@porto.test")
		w.prices(t, true)
		key := w.owner.expect(t, 201, "POST", s.M+"/api-keys", map[string]any{"name": "season", "scopes": []string{
			"marina.read", "map.view", "contracts.read", "contracts.write", "transits.desk", "occupancy.move", "presence.write",
			"occupancy.override_fit", "customers.read", "pricing.read",
		}})
		s.c = h.bearerClient(str(key, "secret"))
		s.setup()

		// Weights of m3-plan §9.4 (percent of the operations).
		weights := []struct {
			name string
			w    int
			fn   func() (string, bool)
		}{
			{"annual", 8, s.opAnnual},
			{"draft", 2, s.opDraft},
			{"transit", 30, s.opTransit},
			{"held", 4, s.opHeld},
			{"stay", 6, s.opStay},
			{"presence", 14, s.opPresence},
			{"unexpected", 2, s.opUnexpected},
			{"move", 6, s.opMove},
			{"area_move", 4, s.opAreaMove},
			{"swap", 2, s.opSwap},
			{"layout", 3, s.opLayout},
			{"compact", 1, s.opCompact},
			{"undo", 2, s.opUndo},
			{"release", 4, s.opRelease},
			{"revoke", 2, s.opRevoke},
			{"terminate", 1, s.opTerminate},
			{"cancel", 3, s.opCancel},
			{"quote", 5, s.opQuote},
			{"block", 1, s.opBlock},
		}
		total := 0
		for _, wt := range weights {
			total += wt.w
		}
		end := time.Date(2027, 10, 31, 18, 0, 0, 0, loc)
		for i := range ops {
			pick := s.rng.IntN(total)
			var name string
			var fn func() (string, bool)
			for _, wt := range weights {
				if pick < wt.w {
					name, fn = wt.name, wt.fn
					break
				}
				pick -= wt.w
			}
			before := s.snapshot()
			problem, ok := fn()
			switch {
			case ok:
				s.ok[name]++
			case problem == "skip":
				s.fail[name+":skip"]++
			default:
				if !slices.Contains(seasonExpected, problem) && !slices.Contains(seasonOpExpected[name], problem) {
					t.Fatalf("op %d %s: unexpected problem %q (seed %d)", i, name, problem, seed)
				}
				s.fail[name+":"+problem]++
				if after := s.snapshot(); after != before {
					t.Fatalf("op %d %s failed with %s but changed the database: %+v → %+v (seed %d)", i, name, problem, before, after, seed)
				}
			}
			if (i+1)%100 == 0 {
				s.invariants(fmt.Sprintf("after op %d", i+1))
			}
			if c.Now().Before(end) {
				c.Advance(time.Duration(2+s.rng.IntN(5)) * time.Hour / 3)
			}
			if i%10 == 9 {
				h.runJobs("contracts.expire")
			}
		}
		h.runJobs("contracts.expire")
		s.invariants("end")
		s.compareOracle()
		succeeded := 0
		for _, wt := range weights {
			succeeded += s.ok[wt.name]
			if s.ok[wt.name] == 0 {
				t.Errorf("operation %s never succeeded (seed %d)", wt.name, seed)
			}
		}
		t.Logf("ok %v", s.ok)
		t.Logf("expected errors and skips %v", s.fail)
		if ops == seasonDefaultOps && succeeded < seasonMinOK {
			t.Errorf("%d of %d operations succeeded, want ≥ %d (seed %d)", succeeded, ops, seasonMinOK, seed)
		}
		s.checkLedger()
	})
}

// setup inserts the harbour, the vessels and the customers through the store (the API paths for them are covered
// by the M1 acceptance): 2 piers × 60 berths in 8 sizes, a 120 m stern-to and an 80 m alongside linear area
// (cells 50 cm), a 30 × 20 m surface area (cells 100 cm), 20 land resources, 300 vessels (10 % multihulls),
// 200 customers.
func (s *season) setup() {
	t := s.t
	ctx := context.Background()
	now := store.UTC(s.clock.Now())
	ins := func(rows ...any) {
		for _, r := range rows {
			if _, err := s.db.NewInsert().Model(r).Exec(ctx); err != nil {
				t.Fatalf("insert %T: %v", r, err)
			}
		}
	}
	mp := &store.Map{
		ID: store.NewID(), MarinaID: s.marina, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map", WidthCm: 200000,
		HeightCm: 200000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	ins(mp)
	s.mapID = mp.ID
	sizes := []struct{ loa, beam int64 }{{800, 300}, {1000, 350}, {1200, 400}, {1400, 450}, {1600, 500}, {1800, 550}, {2000, 600}, {2500, 700}}
	for pier := range 2 {
		for i := range 60 {
			sz := sizes[(i/8)%len(sizes)]
			attrs := store.Strings{}
			if i < 2 {
				attrs = store.Strings{"multihull_ok"}
			}
			b := &store.Berth{
				ID: store.NewID(), MarinaID: s.marina, MapID: mp.ID, Code: fmt.Sprintf("%c-%02d", 'A'+pier, i+1), Side: "none",
				AnchorXCm: int64(1000 + 2000*pier), AnchorYCm: int64(1000 + 500*i), WidthCm: sz.beam, LengthCm: sz.loa, MaxLOACm: sz.loa,
				MaxBeamCm: sz.beam, MooringType: "mooring_line", PriceClass: "L12", Attributes: attrs, Active: true, CreatedAt: now, UpdatedAt: now,
			}
			ins(b)
			s.berths = append(s.berths, seasonBerth{ID: b.ID, MaxLOA: sz.loa, MaxBeam: sz.beam})
		}
	}
	addArea := func(a *store.DynamicArea) {
		ins(a)
		la, err := occupancy.LayoutArea(a)
		if err != nil {
			t.Fatal(err)
		}
		s.layouts[a.ID] = la
	}
	for _, ln := range []struct {
		code string
		to   int64
		mode string
	}{{"L1", 12000, "stern_to"}, {"L2", 8000, "alongside"}} {
		from, to := int64(0), ln.to
		a := &store.DynamicArea{
			ID: store.NewID(), MarinaID: s.marina, MapID: mp.ID, Kind: "linear", Code: ln.code, Side: "left", FromCm: &from, ToCm: &to,
			MooringMode: ln.mode, CellCm: 50, MarginCm: 50, Supports: store.Strings{}, DepthProfile: store.JSON(`[]`),
			BlockedCells: store.JSON(`[]`), Exits: store.JSON(`[]`), CellCount: ln.to / 50, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		addArea(a)
		s.linear = append(s.linear, a.ID)
	}
	outline := []geo.Point{{X: 60000, Y: 60000}, {X: 63000, Y: 60000}, {X: 63000, Y: 62000}, {X: 60000, Y: 62000}}
	sf := &store.DynamicArea{
		ID: store.NewID(), MarinaID: s.marina, MapID: mp.ID, Kind: "surface", Code: "S1", Outline: store.MustJSON(outline),
		CellCm: 100, MarginCm: 0, Supports: store.Strings{}, DepthProfile: store.JSON(`[]`), BlockedCells: store.JSON(`[]`),
		Exits: store.JSON(`[]`), CellCount: 600, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	addArea(sf)
	s.surface = sf.ID
	for i := range 20 {
		l := &store.LandResource{
			ID: store.NewID(), MarinaID: s.marina, MapID: mp.ID, Kind: "parking", Code: fmt.Sprintf("P%02d", i+1), XCm: int64(10000 + 300*i),
			YCm: 50000, WidthCm: 250, LengthCm: 500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		ins(l)
		s.lands = append(s.lands, l.ID)
	}
	for i := range 200 {
		c := &store.Customer{
			ID: store.NewID(), MarinaID: s.marina, Kind: "person", LastName: fmt.Sprintf("Cliente%03d", i), BillingAddress: store.JSON(`{}`),
			Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		ins(c)
		s.custs = append(s.custs, c.ID)
	}
	for i := range 300 {
		loa := int64(500 + s.rng.IntN(1900))
		beam := max(int64(180), loa*(28+int64(s.rng.IntN(8)))/100)
		hull := "monohull_sail"
		if i%10 == 0 {
			hull = "catamaran"
			beam = loa * 50 / 100
		}
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: s.marina, Name: fmt.Sprintf("Barca %03d", i), HullType: hull, LOACm: loa, BeamCm: beam,
			DimensionsSource: "declared", Engines: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		ins(v)
		s.vessels = append(s.vessels, seasonVessel{ID: v.ID, LOA: loa, Beam: beam, Customer: s.custs[i%len(s.custs)]})
		s.dims[v.ID] = layout.Vessel{LOA: geo.Cm(loa), Beam: geo.Cm(beam)}
	}
}

// ---- API helpers ----

func (s *season) call(method, path string, body any, headers ...string) (map[string]any, string, bool) {
	s.clock.Advance(time.Second) // keeps the per-key rate limit windows moving
	r := s.c.do(s.t, method, path, body, headers...)
	if r.status >= 200 && r.status < 300 {
		if len(r.body) == 0 || r.body[0] != '{' {
			return nil, "", true
		}
		return r.json(s.t), "", true
	}
	p := r.problemType()
	if p == "" || r.status >= 500 || p == "bad_request" || p == "validation_failed" || p == "not_found" || p == "forbidden" {
		s.t.Fatalf("%s %s: %d %s", method, path, r.status, r.body)
	}
	return nil, p, false
}

func num(m map[string]any, k string) (int64, bool) {
	f, ok := m[k].(float64)
	return int64(f), ok
}

func numPtr(m map[string]any, k string) *int64 {
	if v, ok := num(m, k); ok {
		return &v
	}
	return nil
}

// learn records the allocations of a contract or transit returned by a successful mutation.
func (s *season) learn(c map[string]any) {
	id := str(c, "id")
	for _, a := range jsonList(c, "allocations") {
		s.learnAlloc(a.(map[string]any), id)
	}
}

func (s *season) learnAlloc(a map[string]any, contractID string) {
	al := seasonAlloc{
		ID: str(a, "id"), Type: str(a, "resource_type"), Resource: str(a, "resource_id"), Status: str(a, "status"), Kind: str(a, "kind"),
		Contract: contractID, Vessel: str(a, "vessel_id"), X: numPtr(a, "x_cm"), Y: numPtr(a, "y_cm"), Rot: numPtr(a, "rotation_dd"),
		First: calendar.MustParse(str(a, "first_night")), Last: calendar.MustParse(str(a, "last_night")),
	}
	if v, ok := num(a, "version"); ok {
		al.Version = int(v)
	}
	if e := str(a, "expires_at"); e != "" {
		at, err := time.Parse(time.RFC3339Nano, e)
		if err != nil {
			s.t.Fatal(err)
		}
		al.Expires = at
	}
	if al.Contract == "" {
		al.Contract = str(a, "contract_id")
	}
	if al.Contract == "" {
		al.Contract = s.oracle[al.ID].Contract
	}
	for _, r := range jsonList(a, "released") {
		rm := r.(map[string]any)
		al.Released = append(al.Released, [2]calendar.Date{calendar.MustParse(str(rm, "from")), calendar.MustParse(str(rm, "to"))})
	}
	s.oracle[al.ID] = al
}

// learnQuoteHolds records the holds of a created quote from its option placements (expiring with the quote).
func (s *season) learnQuoteHolds(q map[string]any) {
	exp, err := time.Parse(time.RFC3339Nano, str(q, "expires_at"))
	if err != nil {
		s.t.Fatal(err)
	}
	vessel := str(q["vessel"].(map[string]any), "id")
	for _, o := range jsonList(q, "options") {
		for _, p := range jsonList(o.(map[string]any), "placements") {
			pm := p.(map[string]any)
			aid := str(pm, "allocation_id")
			if aid == "" {
				continue
			}
			s.oracle[aid] = seasonAlloc{
				ID: aid, Type: str(pm, "resource_type"), Resource: str(pm, "resource_id"), Status: "active", Kind: "hold", Vessel: vessel,
				X: numPtr(pm, "x_cm"), Y: numPtr(pm, "y_cm"), Rot: numPtr(pm, "rotation_dd"),
				First: calendar.MustParse(str(pm, "first_night")), Last: calendar.MustParse(str(pm, "last_night")), Expires: exp, Version: 1,
			}
			s.quoteHolds[str(q, "id")] = append(s.quoteHolds[str(q, "id")], aid)
		}
	}
}

// gone marks allocations of the oracle as without rows (cancelled with their quote).
func (s *season) gone(ids ...string) {
	for _, id := range ids {
		if al, ok := s.oracle[id]; ok {
			al.Status = "cancelled"
			s.oracle[id] = al
		}
	}
}

// read reads a contract back through the API (decision data for the generator; never fed to the oracle).
func (s *season) read(contractID string) map[string]any {
	c, _, ok := s.call("GET", s.M+"/contracts/"+contractID, nil)
	if !ok {
		return nil
	}
	return c
}

type seasonCounts struct {
	allocations, berthRows, cellRows, landRows, contracts, quotes int
	maxOutbox                                                     string
}

func (s *season) snapshot() seasonCounts {
	ctx := context.Background()
	count := func(table string) int {
		n, err := s.db.NewSelect().TableExpr(table).Where("marina_id = ?", s.marina).Count(ctx)
		if err != nil {
			s.t.Fatal(err)
		}
		return int(n)
	}
	var maxID []string
	if err := s.db.NewRaw("SELECT COALESCE(MAX(CAST(id AS TEXT)), '') FROM outbox_events WHERE marina_id = ?", s.marina).Scan(ctx, &maxID); err != nil {
		s.t.Fatal(err)
	}
	out := seasonCounts{
		allocations: count("allocations"), berthRows: count("berth_occupancies"), cellRows: count("cell_occupancies"),
		landRows: count("land_occupancies"), contracts: count("contracts"), quotes: count("quotes"),
	}
	if len(maxID) > 0 {
		out.maxOutbox = maxID[0]
	}
	return out
}

func (s *season) invariants(when string) {
	vs, err := store.CheckOccupancyInvariants(context.Background(), s.db.DB, s.marina, s.today())
	if err != nil {
		s.t.Fatal(err)
	}
	if len(vs) != 0 {
		s.t.Fatalf("%s: invariant violations %+v", when, vs)
	}
}

type oracleKey struct {
	family, resource string
	cell             int
	night            string
}

// expectedRows is the oracle: (family, resource, cell, night) → allocation, from the responses. Cells of dynamic
// areas come from the returned placement through layout.Footprint; holds whose expiry has passed have no rows (the
// job ran, or a sale reclaimed them).
func (s *season) expectedRows() map[oracleKey]string {
	now := s.clock.Now()
	want := map[oracleKey]string{}
	for _, al := range s.oracle {
		if al.Status != "active" || (al.Kind == "hold" && !al.Expires.IsZero() && al.Expires.Before(now)) {
			continue
		}
		family, cells := al.Type, []int{0}
		if al.Type == store.ResourceArea {
			if al.X == nil {
				s.t.Fatalf("area allocation %s without placement", al.ID)
			}
			p := engine.Placement{X: geo.Cm(*al.X)}
			if al.Y != nil {
				p.Y = geo.Cm(*al.Y)
			}
			if al.Rot != nil {
				p.Rotation = geo.Angle(*al.Rot)
			}
			la := s.layouts[al.Resource]
			v, ok := s.dims[al.Vessel]
			if !ok {
				s.t.Fatalf("area allocation %s of an unknown vessel %q", al.ID, al.Vessel)
			}
			var err error
			cells, err = engine.Cells(engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: al.Resource, Placement: &p}, &la, v)
			if err != nil {
				s.t.Fatalf("placement of %s: %v", al.ID, err)
			}
		}
		for d := al.First; !d.After(al.Last); d = d.AddDays(1) {
			released := false
			for _, r := range al.Released {
				if !d.Before(r[0]) && !d.After(r[1]) {
					released = true
				}
			}
			if released {
				continue
			}
			for _, c := range cells {
				k := oracleKey{family, al.Resource, c, d.String()}
				if prev, dup := want[k]; dup {
					s.t.Fatalf("oracle double occupancy at %+v: %s and %s", k, prev, al.ID)
				}
				want[k] = al.ID
			}
		}
	}
	return want
}

// compareOracle checks that the occupancy rows are exactly the oracle: same keys, same allocation per key.
func (s *season) compareOracle() {
	want := s.expectedRows()
	got := map[oracleKey]string{}
	type row struct {
		Alloc    string        `bun:"allocation_id"`
		Resource string        `bun:"resource"`
		Cell     int           `bun:"cell"`
		Night    calendar.Date `bun:"night"`
	}
	for family, q := range map[string]string{
		store.ResourceBerth: "SELECT allocation_id, berth_id AS resource, 0 AS cell, night FROM berth_occupancies WHERE marina_id = ?",
		store.ResourceArea:  "SELECT allocation_id, area_id AS resource, cell, night FROM cell_occupancies WHERE marina_id = ?",
		store.ResourceLand:  "SELECT allocation_id, resource_id AS resource, 0 AS cell, night FROM land_occupancies WHERE marina_id = ?",
	} {
		var rows []row
		if err := s.db.NewRaw(q, s.marina).Scan(context.Background(), &rows); err != nil {
			s.t.Fatal(err)
		}
		for _, r := range rows {
			got[oracleKey{family, r.Resource, r.Cell, r.Night.String()}] = r.Alloc
		}
	}
	bad := 0
	for k, a := range want {
		if got[k] != a {
			bad++
			if bad < 6 {
				s.t.Errorf("oracle %+v → %s, database → %q", k, a, got[k])
			}
		}
	}
	for k, a := range got {
		if _, ok := want[k]; !ok {
			bad++
			if bad < 6 {
				s.t.Errorf("database row unknown to the oracle: %+v → %s", k, a)
			}
		}
	}
	if bad > 0 {
		s.t.Fatalf("oracle differs on %d keys (%d expected, %d in the database)", bad, len(want), len(got))
	}
	s.t.Logf("oracle: %d rows match", len(want))
}

// checkLedger asserts INV-30 for every status change the generator made: an audit row with the action and the
// outbox events of the transition, on that contract, transit or quote.
func (s *season) checkLedger() {
	ctx := context.Background()
	type idAct struct{ id, what string }
	var audits []store.AuditEntry
	if err := s.db.NewSelect().Model(&audits).Column("entity_id", "action").Where("marina_id = ?", s.marina).Scan(ctx); err != nil {
		s.t.Fatal(err)
	}
	have := map[idAct]int{}
	for _, a := range audits {
		have[idAct{a.EntityID, "audit:" + a.Action}]++
	}
	var evs []store.OutboxEvent
	if err := s.db.NewSelect().Model(&evs).Column("type", "payload").Where("marina_id = ?", s.marina).Scan(ctx); err != nil {
		s.t.Fatal(err)
	}
	for _, e := range evs {
		var m map[string]any
		if err := json.Unmarshal(e.Payload, &m); err != nil {
			s.t.Fatal(err)
		}
		for _, id := range idsIn(m) {
			have[idAct{id, "event:" + e.Type}]++
		}
	}
	need := map[idAct]int{}
	for _, c := range s.ledger {
		need[idAct{c.entity, "audit:" + c.audit}]++
		for _, ev := range c.events {
			need[idAct{c.entity, "event:" + ev}]++
		}
	}
	missing := 0
	for k, n := range need {
		if have[k] < n {
			missing++
			if missing < 6 {
				s.t.Errorf("%s %s: %d recorded, %d status changes", k.id, k.what, have[k], n)
			}
		}
	}
	if missing > 0 {
		s.t.Fatalf("%d status changes without their audit row or outbox event (of %d changes)", missing, len(s.ledger))
	}
}

// idsIn returns the contract_id and quote_id values found anywhere in a decoded outbox payload.
func idsIn(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if sv, ok := val.(string); ok && (k == "contract_id" || k == "quote_id") {
				out = append(out, sv)
				continue
			}
			out = append(out, idsIn(val)...)
		}
	case []any:
		for _, val := range x {
			out = append(out, idsIn(val)...)
		}
	}
	return out
}

// ---- generator ----

func (s *season) vessel() seasonVessel { return s.vessels[s.rng.IntN(len(s.vessels))] }

func (s *season) berthFor(v seasonVessel) (seasonBerth, bool) {
	for range 20 {
		b := s.berths[s.rng.IntN(len(s.berths))]
		if v.LOA <= b.MaxLOA && v.Beam <= b.MaxBeam {
			return b, true
		}
	}
	return seasonBerth{}, false
}

func pick[T any](rng *rand.Rand, xs []T) (T, bool) {
	var zero T
	if len(xs) == 0 {
		return zero, false
	}
	return xs[rng.IntN(len(xs))], true
}

// search returns the single-segment solutions of a search on the given resource types (decision data).
func (s *season) search(purpose, vesselID string, arrival, departure calendar.Date, types ...string) []map[string]any {
	r, _, ok := s.call("POST", s.M+"/occupancies/search", map[string]any{
		"purpose": purpose, "vessel_id": vesselID, "arrival": arrival.String(), "departure": departure.String(),
		"resource_types": types, "limit": 8,
	})
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, sol := range jsonList(r, "solutions") {
		segs := jsonList(sol.(map[string]any), "segments")
		if len(segs) == 1 {
			out = append(out, segs[0].(map[string]any))
		}
	}
	return out
}

func targetOf(seg map[string]any) map[string]any {
	t := map[string]any{"resource_type": seg["resource_type"], "resource_id": seg["resource_id"]}
	for _, k := range []string{"x_cm", "y_cm", "rotation_dd"} {
		if seg[k] != nil {
			t[k] = seg[k]
		}
	}
	return t
}

// placementsFor returns the placements of a stay: from the search (≈ 80 %) or a random berth (≈ 20 %).
func (s *season) placementsFor(purpose string, v seasonVessel, arrival, departure calendar.Date) []any {
	if s.rng.IntN(5) > 0 {
		r, _, ok := s.call("POST", s.M+"/occupancies/search", map[string]any{
			"purpose": purpose, "vessel_id": v.ID, "arrival": arrival.String(), "departure": departure.String(),
			"resource_types": []string{"berth", "dynamic_area"}, "limit": 5,
		})
		if ok {
			if sol, found := pick(s.rng, jsonList(r, "solutions")); found {
				var out []any
				for _, sg := range jsonList(sol.(map[string]any), "segments") {
					m := sg.(map[string]any)
					p := targetOf(m)
					p["first_night"], p["last_night"] = m["first_night"], m["last_night"]
					out = append(out, p)
				}
				return out
			}
		}
	}
	b, ok := s.berthFor(v)
	if !ok {
		b = s.berths[s.rng.IntN(len(s.berths))]
	}
	return []any{berthPlacement(b.ID, arrival.String(), departure.AddDays(-1).String())}
}

func (s *season) opTransit() (string, bool) {
	v := s.vessel()
	arr := s.today().AddDays(s.rng.IntN(10))
	dep := arr.AddDays(1 + s.rng.IntN(6))
	c, p, ok := s.call("POST", s.M+"/transits", map[string]any{
		"vessel_id": v.ID, "customer_id": v.Customer, "arrival": arr.String(), "departure": dep.String(),
		"placements": s.placementsFor("transit", v, arr, dep), "confirm": map[string]any{"reference": "pos"},
	})
	if !ok {
		return p, false
	}
	s.transits = append(s.transits, str(c, "id"))
	s.learn(c)
	s.changed(str(c, "id"), "transit.create", "transit.held")
	s.changed(str(c, "id"), "transit.confirm", "transit.confirmed")
	return "", true
}

func (s *season) opHeld() (string, bool) {
	v := s.vessel()
	arr := s.today().AddDays(2 + s.rng.IntN(10))
	dep := arr.AddDays(1 + s.rng.IntN(3))
	c, p, ok := s.call("POST", s.M+"/transits", map[string]any{
		"vessel_id": v.ID, "arrival": arr.String(), "departure": dep.String(), "placements": s.placementsFor("transit", v, arr, dep),
	})
	if !ok {
		return p, false
	}
	s.transits = append(s.transits, str(c, "id"))
	s.learn(c)
	s.changed(str(c, "id"), "transit.create", "transit.held")
	return "", true
}

func (s *season) mooring(activate bool) (string, bool) {
	v := s.vessel()
	terms := []string{"annual", "annual", "seasonal", "monthly"}
	term := terms[s.rng.IntN(len(terms))]
	start := s.today().AddDays(s.rng.IntN(20))
	var ends calendar.Date
	switch term {
	case "annual":
		start = calendar.New(2027, time.Month(1+s.rng.IntN(12)), 1)
		if start.Before(s.today()) {
			start = s.today()
		}
		ends = start.AddDays(365)
	case "seasonal":
		start, ends = calendar.New(2027, 5, 1), calendar.New(2027, 10, 1)
		if start.Before(s.today()) {
			start = s.today()
		}
		if !ends.After(start.AddDays(7)) {
			return "skip", false // the season is over
		}
	default:
		ends = start.AddDays(30)
	}
	var placement map[string]any
	if s.rng.IntN(5) > 0 {
		if seg, found := pick(s.rng, s.search("contract", v.ID, start, ends, "berth")); found {
			placement = berthPlacement(str(seg, "resource_id"), start.String(), ends.AddDays(-1).String())
		}
	}
	if placement == nil {
		b, found := s.berthFor(v)
		if !found {
			return "skip", false
		}
		placement = berthPlacement(b.ID, start.String(), ends.AddDays(-1).String())
	}
	body := map[string]any{
		"term": term, "customer_id": v.Customer, "vessel_id": v.ID, "starts_on": start.String(), "ends_on": ends.String(),
		"placements": []any{placement}, "activate": activate,
	}
	if s.rng.IntN(6) == 0 {
		body["extra_resources"] = []any{map[string]any{"land_resource_id": s.lands[s.rng.IntN(len(s.lands))]}}
	}
	c, p, ok := s.call("POST", s.M+"/contracts", body)
	if !ok {
		return p, false
	}
	s.moorings = append(s.moorings, str(c, "id"))
	s.learn(c)
	if activate {
		s.changed(str(c, "id"), "contract.create", "contract.created", "contract.activated")
	} else {
		s.changed(str(c, "id"), "contract.create", "contract.created")
	}
	return "", true
}

func (s *season) opAnnual() (string, bool) { return s.mooring(true) }

func (s *season) opDraft() (string, bool) {
	p, ok := s.mooring(false)
	if !ok || s.rng.IntN(2) == 0 {
		return p, ok
	}
	id := s.moorings[len(s.moorings)-1]
	c := s.read(id)
	if c == nil {
		return "skip", false
	}
	c2, p, ok := s.call("POST", s.M+"/contracts/"+id+"/activate", nil, "If-Match", etagOf(c["version"]))
	if ok {
		s.learn(c2)
		s.changed(id, "contract.activate", "contract.activated")
	}
	return p, ok
}

// transitOf returns a known transit in one of the statuses (read back to decide).
func (s *season) transitOf(statuses ...string) (map[string]any, bool) {
	for range 25 {
		id, ok := pick(s.rng, s.transits)
		if !ok {
			return nil, false
		}
		c := s.read(id)
		if c != nil && slices.Contains(statuses, str(c, "status")) {
			return c, true
		}
	}
	return nil, false
}

func (s *season) opStay() (string, bool) {
	c, ok := s.transitOf("held", "confirmed", "arrived")
	if !ok {
		return "skip", false
	}
	dep := calendar.MustParse(str(c, "ends_on")).AddDays(s.rng.IntN(5) - 1)
	if !dep.After(calendar.MustParse(str(c, "starts_on"))) || !dep.After(s.today()) {
		dep = calendar.MustParse(str(c, "ends_on")).AddDays(1 + s.rng.IntN(3))
	}
	r, p, ok := s.call("POST", s.M+"/transits/"+str(c, "id")+"/stay", map[string]any{"departure": dep.String()}, "If-Match", etagOf(c["version"]))
	if !ok {
		return p, false
	}
	s.learn(r["contract"].(map[string]any))
	s.changed(str(c, "id"), "transit.stay", "transit.changed")
	return "", true
}

func (s *season) opPresence() (string, bool) {
	for range 5 {
		c, ok := s.transitOf("confirmed", "arrived")
		if !ok {
			return "skip", false
		}
		vessel := str(c["vessel"].(map[string]any), "id")
		event := "arrived"
		if str(c, "status") == "arrived" {
			event = "departed"
		} else if calendar.MustParse(str(c, "starts_on")).After(s.today()) || s.inPort[vessel] {
			continue // not due yet, or the vessel is in port for another stay
		}
		r, p, ok := s.call("POST", s.M+"/presences", map[string]any{"vessel_id": vessel, "event": event})
		if !ok {
			return p, false
		}
		s.inPort[vessel] = event == "arrived"
		if rc, has := r["contract"].(map[string]any); has {
			s.learn(rc)
			if event == "arrived" && str(rc, "status") == "arrived" {
				s.changed(str(rc, "id"), "transit.arrive", "transit.arrived")
			} else if event == "departed" && str(rc, "status") == "departed" {
				s.changed(str(rc, "id"), "transit.depart", "transit.departed")
			}
		}
		return "", true
	}
	return "skip", false
}

func (s *season) opUnexpected() (string, bool) {
	b := s.berths[s.rng.IntN(len(s.berths))]
	loa, beam := min(b.MaxLOA, 900), min(b.MaxBeam, 300)
	r, p, ok := s.call("POST", s.M+"/presences/unexpected-arrival", map[string]any{
		"new_vessel": map[string]any{"name": "Ospite", "loa_cm": loa, "beam_cm": beam, "hull_type": "rib"},
		"target":     map[string]any{"resource_type": "berth", "resource_id": b.ID},
	})
	if !ok {
		return p, false
	}
	c := r["contract"].(map[string]any)
	vid := str(c["vessel"].(map[string]any), "id")
	s.dims[vid] = layout.Vessel{LOA: geo.Cm(loa), Beam: geo.Cm(beam)}
	s.inPort[vid] = true
	s.transits = append(s.transits, str(c, "id"))
	s.learn(c)
	s.changed(str(c, "id"), "transit.create", "transit.held")
	return "", true
}

// activeAlloc returns a live allocation of a known contract that still has nights from today, on one of the
// resource types (read back to decide; the move itself is checked against the oracle through its response).
func (s *season) activeAlloc(types ...string) (map[string]any, bool) {
	for range 15 {
		id, ok := pick(s.rng, append(slices.Clone(s.transits), s.moorings...))
		if !ok {
			return nil, false
		}
		c := s.read(id)
		if c == nil {
			continue
		}
		for _, a := range jsonList(c, "allocations") {
			am := a.(map[string]any)
			last := calendar.MustParse(str(am, "last_night"))
			if str(am, "status") == "active" && !last.Before(s.today()) && slices.Contains(types, str(am, "resource_type")) {
				return am, true
			}
		}
	}
	return nil, false
}

// remaining is the first night a move of a can take (today or its first night) and its last night.
func (s *season) remaining(a map[string]any) (calendar.Date, calendar.Date) {
	from, last := calendar.MustParse(str(a, "first_night")), calendar.MustParse(str(a, "last_night"))
	if from.Before(s.today()) {
		from = s.today()
	}
	return from, last
}

func (s *season) learnMove(r map[string]any) {
	var created []string
	for _, a := range jsonList(r, "allocations") {
		am := a.(map[string]any)
		s.learnAlloc(am, "")
		if str(am, "moved_from_id") != "" && str(am, "status") == "active" {
			created = append(created, str(am, "id"))
		}
	}
	if u := str(r, "undo_id"); u != "" && len(created) > 0 {
		until, err := time.Parse(time.RFC3339Nano, str(r, "undo_until"))
		if err != nil {
			s.t.Fatal(err)
		}
		s.undos = append(s.undos, seasonUndo{id: u, until: until, created: created})
	}
}

func (s *season) move(a map[string]any, target map[string]any, from calendar.Date) (string, bool) {
	r, p, ok := s.call("POST", s.M+"/occupancies/move", map[string]any{
		"allocation_id": str(a, "id"), "version": a["version"], "target": target, "from_night": from.String(),
	})
	if !ok {
		return p, false
	}
	s.learnMove(r)
	return "", true
}

// opMove moves a berth allocation to another berth: a berth free for the remaining nights from the search
// (≈ 80 %) or a random one (≈ 20 %).
func (s *season) opMove() (string, bool) {
	a, ok := s.activeAlloc(store.ResourceBerth)
	if !ok {
		return "skip", false
	}
	from, last := s.remaining(a)
	var target map[string]any
	if s.rng.IntN(5) > 0 {
		for _, seg := range s.search("transit", str(a, "vessel_id"), from, last.AddDays(1), "berth") {
			if str(seg, "resource_id") != str(a, "resource_id") {
				target = targetOf(seg)
				break
			}
		}
	}
	if target == nil {
		b := s.berths[s.rng.IntN(len(s.berths))]
		if b.ID == str(a, "resource_id") {
			return "skip", false
		}
		target = map[string]any{"resource_type": "berth", "resource_id": b.ID}
	}
	return s.move(a, target, from)
}

// opAreaMove moves a berth or area allocation into a dynamic area or to another place of its area: a placement
// from the search on the linear areas (≈ 80 %) or a random placement on any area, the surface included (≈ 20 %).
func (s *season) opAreaMove() (string, bool) {
	a, ok := s.activeAlloc(store.ResourceBerth, store.ResourceArea)
	if !ok {
		return "skip", false
	}
	from, last := s.remaining(a)
	var target map[string]any
	if s.rng.IntN(5) > 0 {
		if seg, found := pick(s.rng, s.search("transit", str(a, "vessel_id"), from, last.AddDays(1), "dynamic_area")); found {
			target = targetOf(seg)
		}
	}
	if target == nil {
		if s.rng.IntN(2) == 0 {
			area := s.linear[s.rng.IntN(len(s.linear))]
			n := int64(s.layouts[area].Length) / 10
			target = map[string]any{"resource_type": "dynamic_area", "resource_id": area, "x_cm": 10 * s.rng.Int64N(n)}
		} else {
			target = map[string]any{
				"resource_type": "dynamic_area", "resource_id": s.surface, "x_cm": 100 * s.rng.Int64N(28), "y_cm": 100 * s.rng.Int64N(18),
				"rotation_dd": 900 * s.rng.Int64N(4),
			}
		}
	}
	if target["resource_id"] == str(a, "resource_id") && target["x_cm"] == a["x_cm"] && target["y_cm"] == a["y_cm"] {
		return "skip", false
	}
	return s.move(a, target, from)
}

func (s *season) opSwap() (string, bool) {
	a, ok1 := s.activeAlloc(store.ResourceBerth, store.ResourceArea)
	b, ok2 := s.activeAlloc(store.ResourceBerth, store.ResourceArea)
	if !ok1 || !ok2 || str(a, "id") == str(b, "id") || str(a, "resource_id") == str(b, "resource_id") {
		return "skip", false
	}
	fa, la := s.remaining(a)
	fb, lb := s.remaining(b)
	from := fa
	if fb.After(from) {
		from = fb
	}
	if from.After(la) || from.After(lb) {
		return "skip", false
	}
	r, p, ok := s.call("POST", s.M+"/occupancies/swap", map[string]any{
		"a": map[string]any{"allocation_id": str(a, "id"), "version": a["version"]}, "b": map[string]any{"allocation_id": str(b, "id"), "version": b["version"]},
		"from_night": from.String(),
	})
	if !ok {
		return p, false
	}
	s.learnMove(r)
	return "", true
}

// areaToday reads the day view of today and returns the vessel placements of an area (decision data: the layout
// base, as the layout session of the operational view takes it).
func (s *season) areaToday(area string) []map[string]any {
	day, _, ok := s.call("GET", s.M+"/maps/"+s.mapID+"/day?date="+s.today().String(), nil)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, p := range jsonList(day, "area_placements") {
		pm := p.(map[string]any)
		if str(pm, "area_id") == area && pm["vessel"] != nil {
			out = append(out, pm)
		}
	}
	return out
}

// saveLayout saves a layout session of area with the current placements and the changes in moves (allocation
// id → placement).
func (s *season) saveLayout(area string, present []map[string]any, moves map[string]map[string]any) (string, bool) {
	var base, placements []any
	for _, p := range present {
		id := str(p, "allocation_id")
		base = append(base, map[string]any{"allocation_id": id, "version": p["allocation_version"]})
		pl := map[string]any{"allocation_id": id, "x_cm": p["x_cm"]}
		for _, k := range []string{"y_cm", "rotation_dd"} {
			if p[k] != nil {
				pl[k] = p[k]
			}
		}
		for k, v := range moves[id] {
			pl[k] = v
		}
		placements = append(placements, pl)
	}
	r, p, ok := s.call("POST", s.M+"/dynamic-areas/"+area+"/layout", map[string]any{
		"date": s.today().String(), "base": base, "placements": placements,
	})
	if !ok {
		return p, false
	}
	s.learnMove(r)
	return "", true
}

// opLayout saves a layout session on an area with vessels today: two vessels exchange their places, or one
// vessel shifts a little.
func (s *season) opLayout() (string, bool) {
	areas := append(slices.Clone(s.linear), s.surface)
	for range 3 {
		area := areas[s.rng.IntN(len(areas))]
		present := s.areaToday(area)
		if len(present) == 0 {
			continue
		}
		moves := map[string]map[string]any{}
		if len(present) >= 2 && s.rng.IntN(2) == 0 {
			i := s.rng.IntN(len(present))
			j := (i + 1 + s.rng.IntN(len(present)-1)) % len(present)
			a, b := present[i], present[j]
			moves[str(a, "allocation_id")] = map[string]any{"x_cm": b["x_cm"], "y_cm": b["y_cm"], "rotation_dd": b["rotation_dd"]}
			moves[str(b, "allocation_id")] = map[string]any{"x_cm": a["x_cm"], "y_cm": a["y_cm"], "rotation_dd": a["rotation_dd"]}
			for _, m := range moves {
				for k, v := range m {
					if v == nil {
						delete(m, k)
					}
				}
			}
		} else {
			p := present[s.rng.IntN(len(present))]
			step := int64(10 * (1 + s.rng.IntN(5)))
			if area == s.surface {
				step = 100
			}
			if s.rng.IntN(2) == 0 {
				step = -step
			}
			x, _ := num(p, "x_cm")
			moves[str(p, "allocation_id")] = map[string]any{"x_cm": max(0, x+step)}
		}
		return s.saveLayout(area, present, moves)
	}
	return "skip", false
}

// opCompact asks the compaction of a linear area for today and applies it through a layout save.
func (s *season) opCompact() (string, bool) {
	for range 3 {
		area := s.linear[s.rng.IntN(len(s.linear))]
		present := s.areaToday(area)
		if len(present) == 0 {
			continue
		}
		prop, p, ok := s.call("POST", s.M+"/dynamic-areas/"+area+"/compact", map[string]any{"from": s.today().String(), "to": s.today().String()})
		if !ok {
			return p, false
		}
		moves := map[string]map[string]any{}
		for _, pl := range jsonList(prop, "placements") {
			pm := pl.(map[string]any)
			moves[str(pm, "allocation_id")] = map[string]any{"x_cm": pm["x_cm"]}
		}
		return s.saveLayout(area, present, moves)
	}
	return "skip", false
}

// opUndo undoes the latest move whose created allocations are untouched since (as far as the oracle knows) and
// whose window is open; older or touched ones are dropped (they are undo_unavailable by design).
func (s *season) opUndo() (string, bool) {
	for len(s.undos) > 0 {
		u := s.undos[len(s.undos)-1]
		s.undos = s.undos[:len(s.undos)-1]
		untouched := s.clock.Now().Before(u.until)
		for _, id := range u.created {
			if al := s.oracle[id]; al.Status != "active" || al.Version != 1 {
				untouched = false
			}
		}
		if !untouched && s.rng.IntN(4) > 0 {
			continue
		}
		r, p, ok := s.call("POST", s.M+"/move-undo/"+u.id, nil)
		if !ok {
			return p, false
		}
		for _, a := range jsonList(r, "allocations") {
			s.learnAlloc(a.(map[string]any), "")
		}
		return "", true
	}
	return "skip", false
}

func overlaps(rs [][2]calendar.Date, from, to calendar.Date) bool {
	for _, r := range rs {
		if !from.After(r[1]) && !r[0].After(to) {
			return true
		}
	}
	return false
}

func (s *season) opRelease() (string, bool) {
	for range 10 {
		id, ok := pick(s.rng, s.moorings)
		if !ok {
			return "skip", false
		}
		c := s.read(id)
		if c == nil || str(c, "status") != "active" {
			continue
		}
		for _, a := range jsonList(c, "allocations") {
			am := a.(map[string]any)
			al := s.oracle[str(am, "id")]
			from := s.today().AddDays(1 + s.rng.IntN(10))
			to := from.AddDays(s.rng.IntN(7))
			if al.Status != "active" || al.Kind != "contract" || to.After(al.Last) || from.Before(al.First) || overlaps(al.Released, from, to) {
				continue
			}
			r, p, ok := s.call("POST", s.M+"/releases", map[string]any{
				"allocation_id": al.ID, "from_night": from.String(), "to_night": to.String(), "source": "office",
			})
			if !ok {
				return p, false
			}
			s.releases = append(s.releases, str(r, "id"))
			al.Released = append(al.Released, [2]calendar.Date{from, to})
			s.oracle[al.ID] = al
			return "", true
		}
	}
	return "skip", false
}

func (s *season) opRevoke() (string, bool) {
	page, _, ok := s.call("GET", s.M+"/releases?status=active", nil)
	if !ok {
		return "skip", false
	}
	var cands []map[string]any
	for _, it := range jsonList(page, "items") {
		r := it.(map[string]any)
		if slices.Contains(s.releases, str(r, "id")) && !calendar.MustParse(str(r, "to_night")).Before(s.today()) {
			cands = append(cands, r)
		}
	}
	r, found := pick(s.rng, cands)
	if !found {
		return "skip", false
	}
	res, p, ok := s.call("POST", s.M+"/releases/"+str(r, "id")+"/revoke", map[string]any{}, "If-Match", etagOf(r["version"]))
	if !ok {
		return p, false
	}
	// The revoked nights come back to the allocation: replace the release range by what is left of it.
	al := s.oracle[str(r, "allocation_id")]
	old := [2]calendar.Date{calendar.MustParse(str(r, "from_night")), calendar.MustParse(str(r, "to_night"))}
	i := slices.Index(al.Released, old)
	if i < 0 {
		s.t.Fatalf("revoked release %s %v unknown to the oracle of %s (%v)", str(r, "id"), old, al.ID, al.Released)
	}
	al.Released = slices.Delete(al.Released, i, i+1)
	for _, k := range []string{"release", "remaining"} {
		if rm, has := res[k].(map[string]any); has && str(rm, "status") == "active" {
			al.Released = append(al.Released, [2]calendar.Date{calendar.MustParse(str(rm, "from_night")), calendar.MustParse(str(rm, "to_night"))})
		}
	}
	s.oracle[al.ID] = al
	return "", true
}

func (s *season) opTerminate() (string, bool) {
	for range 5 {
		id, ok := pick(s.rng, s.moorings)
		if !ok {
			return "skip", false
		}
		c := s.read(id)
		if c == nil || str(c, "status") != "active" {
			continue
		}
		eff := s.today().AddDays(1 + s.rng.IntN(20))
		if !eff.Before(calendar.MustParse(str(c, "ends_on"))) || eff.Before(calendar.MustParse(str(c, "starts_on"))) {
			continue
		}
		r, p, ok := s.call("POST", s.M+"/contracts/"+id+"/terminate", map[string]any{"effective_on": eff.String(), "reason": "vendita"}, "If-Match", etagOf(c["version"]))
		if !ok {
			return p, false
		}
		s.learn(r)
		s.changed(id, "contract.terminate", "contract.terminated")
		return "", true
	}
	return "skip", false
}

func (s *season) opCancel() (string, bool) {
	c, ok := s.transitOf("held", "confirmed")
	if !ok {
		return "skip", false
	}
	action := "/cancel"
	var body any = map[string]any{}
	if str(c, "status") == "confirmed" && s.rng.IntN(2) == 0 {
		action, body = "/no-show", nil
	}
	r, p, ok := s.call("POST", s.M+"/transits/"+str(c, "id")+action, body, "If-Match", etagOf(c["version"]))
	if !ok {
		return p, false
	}
	s.learn(r)
	if action == "/cancel" {
		s.changed(str(c, "id"), "transit.cancel", "transit.cancelled")
	} else {
		s.changed(str(c, "id"), "transit.no_show", "transit.changed")
	}
	return "", true
}

func (s *season) opQuote() (string, bool) {
	v := s.vessel()
	start := s.today().AddDays(3 + s.rng.IntN(20))
	ends := start.AddDays(1 + s.rng.IntN(5))
	var opts []any
	for range 1 + s.rng.IntN(2) {
		b, found := s.berthFor(v)
		if !found {
			continue
		}
		opts = append(opts, map[string]any{"hold": s.rng.IntN(2) == 0, "placements": []any{berthPlacement(b.ID, start.String(), ends.AddDays(-1).String())}})
	}
	if len(opts) == 0 {
		return "skip", false
	}
	q, p, ok := s.call("POST", s.M+"/quotes", map[string]any{
		"kind": "transit", "customer_id": v.Customer, "vessel_id": v.ID, "starts_on": start.String(), "ends_on": ends.String(),
		"valid_until": s.today().AddDays(s.rng.IntN(3)).String(), "options": opts,
	})
	if !ok {
		return p, false
	}
	qid := str(q, "id")
	s.quotes = append(s.quotes, qid)
	s.learnQuoteHolds(q)
	s.changed(qid, "quote.create", "quote.changed")
	// The quote exists: a refused follow-up (accept on a taken berth, an expired quote) must leave no effect of
	// its own.
	before := s.snapshot()
	followUp := func(p string) {
		if !slices.Contains(seasonExpected, p) {
			s.t.Fatalf("quote follow-up: unexpected problem %q", p)
		}
		if after := s.snapshot(); after != before {
			s.t.Fatalf("quote follow-up failed with %s but changed the database", p)
		}
		s.fail["quote_followup:"+p]++
	}
	switch s.rng.IntN(3) {
	case 0: // accept with the manual driver: the option's hold becomes the transit, the other holds go
		opt := jsonList(q, "options")[0].(map[string]any)
		r, p, ok := s.call("POST", s.M+"/quotes/"+qid+"/accept", map[string]any{"option_id": opt["id"], "confirm": map[string]any{}}, "If-Match", etagOf(q["version"]))
		if !ok {
			followUp(p)
			break
		}
		c := r["contract"].(map[string]any)
		s.transits = append(s.transits, str(c, "id"))
		kept := map[string]bool{}
		for _, a := range jsonList(c, "allocations") {
			kept[str(a.(map[string]any), "id")] = true
		}
		for _, h := range s.quoteHolds[qid] {
			if !kept[h] {
				s.gone(h)
			}
		}
		s.learn(c)
		s.changed(qid, "quote.accept", "quote.accepted")
		s.changed(str(c, "id"), "transit.create", "transit.held")
		s.changed(str(c, "id"), "transit.confirm", "transit.confirmed")
	case 1:
		if _, p, ok := s.call("POST", s.M+"/quotes/"+qid+"/decline", map[string]any{}, "If-Match", etagOf(q["version"])); !ok {
			followUp(p)
			break
		}
		s.gone(s.quoteHolds[qid]...)
		s.changed(qid, "quote.decline", "quote.changed")
	}
	return "", true
}

func (s *season) opBlock() (string, bool) {
	if id, ok := pick(s.rng, s.blocks); ok && s.rng.IntN(2) == 0 {
		al := s.oracle[id]
		if al.Status != "active" || al.Last.Before(s.today()) {
			return "skip", false // already ended: nothing to unblock
		}
		if _, p, ok := s.call("DELETE", s.M+"/blocks/"+id, nil, "If-Match", fmt.Sprintf(`"%d"`, al.Version)); !ok {
			return p, false
		}
		// Rows from today are removed, past nights stay.
		al.Last = s.today().AddDays(-1)
		if al.Last.Before(al.First) {
			al.Status = "cancelled"
		}
		al.Version++
		s.oracle[id] = al
		return "", true
	}
	b := s.berths[s.rng.IntN(len(s.berths))]
	from := s.today().AddDays(s.rng.IntN(10))
	a, p, ok := s.call("POST", s.M+"/blocks", map[string]any{
		"target": map[string]any{"resource_type": "berth", "resource_id": b.ID}, "from": from.String(), "to": from.AddDays(s.rng.IntN(4)).String(),
		"reason": "lavori",
	})
	if !ok {
		return p, false
	}
	s.blocks = append(s.blocks, str(a, "id"))
	s.learnAlloc(a, "")
	return "", true
}
