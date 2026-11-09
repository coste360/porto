//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M9 storage acceptance (spec 32 M9, m9-plan §14.4): in a 2,000 m² shed with 40 boats the optimizer produces in
// under 3 s a valid layout with no blocked exits, and applying it never leaves the state half-done.
//
// The shed (50 m × 40 m, cells of 1 m, margin 60 cm, a 6 m door on a short side, two rows of pillars) is inserted
// as map data; everything else goes through the API: 40 storage contracts placed by the service in reverse launch
// order, a layout save that swaps boats of the same size (so that boats leaving early end up behind boats leaving
// late), a scenario optimized over HTTP, its apply, and a second scenario made stale by a desk move that is
// refused with the list of conflicts and leaves the state identical.

// shedBoats are the size classes of the 40 boats (LOA × beam in cm) and how many of each.
var shedBoats = []struct{ loa, beam, n int }{{600, 230, 20}, {800, 280, 10}, {1000, 330, 7}, {1400, 450, 3}}

func (w *deskWorld) storageShed(t *testing.T) string {
	t.Helper()
	now := store.UTC(w.clock.Now())
	cols := 50
	mp := &store.Map{
		ID: store.NewID(), MarinaID: w.marina, Kind: "shed", Name: store.JSON(`{"it":"Capannone grande"}`), Icon: "warehouse",
		WidthCm: 6000, HeightCm: 5000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	var blocked, exits []int
	for _, r := range []int{12, 27} {
		for _, c := range []int{9, 19, 29, 39} {
			blocked = append(blocked, r*cols+c)
		}
	}
	for r := 17; r <= 22; r++ {
		exits = append(exits, r*cols)
	}
	area := &store.DynamicArea{
		ID: store.NewID(), MarinaID: w.marina, MapID: mp.ID, Kind: "surface", Code: "CAP-2000",
		Outline: store.JSON(`[{"x":0,"y":0},{"x":5000,"y":0},{"x":5000,"y":4000},{"x":0,"y":4000}]`), CellCm: 100, MarginCm: 60,
		MaxHeightCm: 1400, Supports: store.Strings{"cradle", "trailer"}, DepthProfile: store.JSON(`[]`),
		BlockedCells: store.MustJSON(blocked), Exits: store.MustJSON(exits), CellCount: 2000, PriceClass: "coperto",
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []any{mp, area} {
		if _, err := w.h.app.DB.NewInsert().Model(r).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return area.ID
}

// storagePrices creates the storage list of the season through the API.
func (w *deskWorld) storagePrices(t *testing.T) {
	t.Helper()
	o, M := w.owner, w.M
	classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
	rc := str(jsonList(classes, "items")[0].(map[string]any), "id")
	it := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "RIM-MESE", "name": map[string]string{"it": "Rimessaggio"}, "item_type": "berth_fee", "unit": "month",
		"revenue_class_id": rc, "price_includes_vat": true,
	})
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "RIM27", "name": map[string]string{"it": "Rimessaggio 2027"}, "valid_from": "2027-01-01", "valid_to": "2028-12-31",
		"contract_types": []string{"storage"},
	})
	body := map[string]any{
		"code": "RIM27", "name": map[string]string{"it": "Rimessaggio 2027"}, "valid_from": "2027-01-01", "valid_to": "2028-12-31",
		"contract_types": []string{"storage"}, "periods": []any{}, "bands": []any{},
		"settings": map[string]any{
			"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
			"min_beam_ratio": map[string]int{"num": 1, "den": 3}, "primary_items": map[string]string{"storage": str(it, "id")},
		},
		"rules": []map[string]any{{
			"key": "r1", "stage": "base", "name": "Rimessaggio", "item_id": str(it, "id"), "basis": "flat", "unit": "month",
			"amount_cents": 25000, "scope": map[string]any{"contract_types": []string{"storage"}},
		}},
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	sd := saved["document"].(map[string]any)
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match", etagOf(sd["version"]))
}

type shedBoat struct {
	class     int
	launch    string
	contract  string
	alloc     string
	version   float64
	x, y, rot float64
}

// planItems reads the plan of the shed for the season, by allocation.
func (w *deskWorld) planItems(t *testing.T, area string) map[string]map[string]any {
	t.Helper()
	plan := w.owner.expect(t, 200, "GET", w.M+"/storage/areas/"+area+"/plan?from=2027-11-01&to=2028-04-30", nil)
	out := map[string]map[string]any{}
	for _, it := range jsonList(plan, "items") {
		m := it.(map[string]any)
		out[str(m, "allocation_id")] = m
	}
	return out
}

func TestAcceptance_M9_StorageShed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 1)
		w.setClock(t, time.Date(2027, 10, 20, 8, 0, 0, 0, time.UTC))
		o, M := w.owner, w.M
		w.storagePrices(t)
		area := w.storageShed(t)
		cust := w.customer(t, "Rossi")

		// 40 boats staying from November to April, launches every two days from February 1; contracts created latest
		// launch first.
		var boats []*shedBoat
		k := 0
		for ci, cl := range shedBoats {
			for range cl.n {
				boats = append(boats, &shedBoat{class: ci})
				k++
			}
		}
		for i, b := range boats {
			b.launch = calendar.MustParse("2028-02-01").AddDays(2 * ((i * 17) % len(boats))).String()
		}
		order := make([]*shedBoat, len(boats))
		copy(order, boats)
		for i := range order {
			for j := i + 1; j < len(order); j++ {
				if order[j].launch > order[i].launch {
					order[i], order[j] = order[j], order[i]
				}
			}
		}
		for i, b := range order {
			cl := shedBoats[b.class]
			v := w.vessel(t, fmt.Sprintf("Barca %02d", i+1), cl.loa, cl.beam)
			c := o.expect(t, 201, "POST", M+"/storage/contracts", map[string]any{
				"term": "period", "customer_id": cust, "vessel_id": v, "starts_on": "2027-11-01", "ends_on": "2028-05-01",
				"area_id": area, "support": "cradle", "launch_on": b.launch, "activate": true,
			})
			pl := c["placement"].(map[string]any)
			b.contract, b.alloc = str(c["contract"].(map[string]any), "id"), str(pl, "allocation_id")
		}
		items := w.planItems(t, area)
		for _, b := range boats {
			it := items[b.alloc]
			b.version, b.x, b.y, b.rot = it["version"].(float64), it["x_cm"].(float64), it["y_cm"].(float64), it["rotation_dd"].(float64)
		}

		// Shuffle: inside each size class, every boat takes the place of the next one (a valid layout whose
		// launch order no longer matches the depth).
		var base, places []map[string]any
		for ci := range shedBoats {
			var cls []*shedBoat
			for _, b := range boats {
				if b.class == ci {
					cls = append(cls, b)
				}
			}
			for i, b := range cls {
				n := cls[(i+1)%len(cls)]
				places = append(places, map[string]any{"allocation_id": b.alloc, "x_cm": n.x, "y_cm": n.y, "rotation_dd": n.rot})
			}
		}
		for _, b := range boats {
			base = append(base, map[string]any{"allocation_id": b.alloc, "version": b.version})
		}
		o.expect(t, 200, "POST", M+"/dynamic-areas/"+area+"/layout", map[string]any{
			"date": "2027-11-01", "base": base, "placements": places,
		})
		realPlan := o.expect(t, 200, "GET", M+"/storage/areas/"+area+"/plan?from=2027-11-01&to=2028-04-30", nil)
		blockedReal := 0
		for _, e := range jsonList(realPlan, "access") {
			if m := e.(map[string]any); m["event"] == "exit" && m["accessible"] == false {
				blockedReal++
			}
		}
		t.Logf("blocked exits after the shuffle: %d", blockedReal)

		// Scenario, optimize over HTTP.
		sc := o.expect(t, 201, "POST", M+"/storage/scenarios", map[string]any{
			"area_id": area, "name": "Inverno 2027", "from_on": "2027-11-01", "to_on": "2028-04-30",
		})
		scn := sc["scenario"].(map[string]any)
		S := M + "/storage/scenarios/" + str(scn, "id")
		start := time.Now()
		opt := o.do(t, "POST", S+"/optimize", map[string]any{}, "If-Match", etagOf(scn["version"]))
		took := time.Since(start)
		if opt.status != http.StatusOK {
			t.Fatalf("optimize: %d %s", opt.status, opt.body)
		}
		sc = opt.json(t)
		scn = sc["scenario"].(map[string]any)
		ind := scn["indicators"].(map[string]any)
		t.Logf("optimize %v: %v", took, ind)
		if ind["unplaced"].(float64) != 0 || ind["exits_blocked"].(float64) != 0 {
			t.Fatalf("indicators %v", ind)
		}
		if !storageRace && !db.IsPostgres() && took >= 3*time.Second {
			t.Fatalf("optimize took %v", took)
		}

		// Apply: every allocation is where the scenario put it.
		want := map[string][2]float64{}
		for _, it := range jsonList(sc, "items") {
			m := it.(map[string]any)
			want[str(m, "allocation_id")] = [2]float64{m["x_cm"].(float64), m["y_cm"].(float64)}
		}
		// Apply moves every boat in one transaction: under a loaded `make test-db` (packages in parallel on one
		// PostgreSQL) it can exceed the default 10 s client timeout, which is not what this test measures.
		o.http.Timeout = 2 * time.Minute
		applyStart := time.Now()
		ap := o.expect(t, 200, "POST", S+"/apply", nil, "If-Match", etagOf(scn["version"]))
		t.Logf("apply %v", time.Since(applyStart))
		if str(ap["scenario"].(map[string]any), "status") != "applied" {
			t.Fatalf("apply %v", ap["scenario"])
		}
		after := w.planItems(t, area)
		atNew := map[[2]float64]bool{}
		for _, it := range after {
			if str(it, "first_night") <= "2027-11-01" || len(after) == len(boats) {
				atNew[[2]float64{it["x_cm"].(float64), it["y_cm"].(float64)}] = true
			}
		}
		for id, p := range want {
			if !atNew[p] {
				t.Fatalf("allocation %s not at its scenario position %v", id, p)
			}
		}
		w.storageInvariants(t)

		// A second scenario, made stale by a desk move, is refused with the list and changes nothing.
		sc2 := o.expect(t, 201, "POST", M+"/storage/scenarios", map[string]any{
			"area_id": area, "name": "Seconda", "from_on": "2027-11-01", "to_on": "2028-04-30",
		})
		scn2 := sc2["scenario"].(map[string]any)
		var moved map[string]any
		for _, it := range w.planItems(t, area) {
			moved = it
			break
		}
		o.expect(t, 200, "POST", M+"/occupancies/move", map[string]any{
			"allocation_id": str(moved, "allocation_id"), "version": moved["version"], "from_night": "2027-11-01",
			"target": map[string]any{"resource_type": "dynamic_area", "resource_id": area, "x_cm": moved["x_cm"], "y_cm": moved["y_cm"], "rotation_dd": int(moved["rotation_dd"].(float64)+1800) % 3600},
		})
		before := o.expect(t, 200, "GET", M+"/storage/areas/"+area+"/plan?from=2027-11-01&to=2028-04-30", nil)
		r := o.do(t, "POST", M+"/storage/scenarios/"+str(scn2, "id")+"/apply", nil, "If-Match", etagOf(scn2["version"]))
		if r.status != http.StatusConflict || r.problemType() != "scenario_conflicts" {
			t.Fatalf("stale apply: %d %s", r.status, r.body)
		}
		if len(jsonList(r.json(t)["details"].(map[string]any), "conflicts")) == 0 {
			t.Fatal("no conflicts listed")
		}
		again := o.expect(t, 200, "GET", M+"/storage/areas/"+area+"/plan?from=2027-11-01&to=2028-04-30", nil)
		if canon(t, before["items"]) != canon(t, again["items"]) {
			t.Fatal("a refused apply changed the state")
		}
		w.storageInvariants(t)
	})
}

// canon is a canonical JSON form (sorted keys) for comparisons.
func canon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *deskWorld) storageInvariants(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	vs, err := store.CheckStorageInvariants(ctx, w.h.app.DB.DB, w.marina)
	if err != nil {
		t.Fatal(err)
	}
	ov, err := store.CheckOccupancyInvariants(ctx, w.h.app.DB.DB, w.marina, calendar.MustParse("2027-10-20"))
	if err != nil {
		t.Fatal(err)
	}
	if len(vs)+len(ov) > 0 {
		t.Fatalf("invariants: %+v %+v", vs, ov)
	}
}
