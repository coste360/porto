//go:build integration

package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: the shed is inserted as map data
)

// storageSetup adds a shed (20 m × 10 m, a 4 m door on the left) and an active storage price list to a desk setup.
func storageSetup(t *testing.T) (*deskSetup, string) {
	t.Helper()
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	now := time.Now().UTC()
	mp := &store.Map{
		ID: store.NewID(), MarinaID: ds.marinaID, Kind: "shed", Name: store.JSON(`{"it":"Capannone"}`), Icon: "warehouse",
		WidthCm: 5000, HeightCm: 5000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	area := &store.DynamicArea{
		ID: store.NewID(), MarinaID: ds.marinaID, MapID: mp.ID, Kind: "surface", Code: "C1",
		Outline: store.JSON(`[{"x":0,"y":0},{"x":2000,"y":0},{"x":2000,"y":1000},{"x":0,"y":1000}]`), CellCm: 100, MarginCm: 0,
		Supports: store.Strings{"cradle"}, DepthProfile: store.JSON(`[]`), BlockedCells: store.JSON(`[]`),
		Exits: store.JSON(`[60,80,100,120]`), CellCount: 200, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []any{mp, area} {
		if _, err := ds.s.app.DB.NewInsert().Model(r).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	var it api.Item
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "RIM", "name": map[string]string{"it": "Rimessaggio"}, "item_type": "berth_fee", "unit": "month",
		"revenue_class_id": classes.Items[1].Id, "price_includes_vat": true,
	}, &it)
	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": "R2027", "name": map[string]string{"it": "R2027"}, "valid_from": "2027-01-01", "valid_to": "2028-12-31",
		"contract_types": []string{"storage"},
	}, &doc)
	body := saveBody(doc)
	body["settings"] = map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"}, "min_beam_ratio": map[string]int{"num": 1, "den": 3},
		"primary_items": map[string]string{"storage": it.Id},
	}
	body["periods"], body["bands"] = []any{}, []any{}
	body["rules"] = []map[string]any{
		{"key": "r1", "stage": "base", "name": "Rimessaggio", "item_id": it.Id, "basis": "flat", "unit": "month", "amount_cents": 20000, "scope": map[string]any{"contract_types": []string{"storage"}}},
	}
	var res api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, body, &res, "If-Match", etag(doc.Version))
	c.expect(t, 200, "", "POST", M+"/price-lists/"+doc.Id+"/activate", map[string]any{}, nil, "If-Match", etag(res.Document.Version))
	return ds, area.ID
}

func TestStorageHandlers_Scenario(t *testing.T) {
	ds, area := storageSetup(t)
	c, M, X := ds.owner, ds.M, ds.otherM
	var vs [3]string
	for i, loa := range []int{800, 800, 800} {
		var v api.Vessel
		c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Barca", "loa_cm": loa, "beam_cm": 300, "hull_type": "monohull_motor"}, &v)
		vs[i] = v.Id
	}
	create := func(vessel string, x, y int, launch string) api.StorageContract {
		var sc api.StorageContract
		c.expect(t, 201, "", "POST", M+"/storage/contracts", map[string]any{
			"term": "period", "customer_id": ds.customer, "vessel_id": vessel, "starts_on": "2027-10-05", "ends_on": "2028-05-01",
			"area_id": area, "placement": map[string]any{"x_cm": x * 100, "y_cm": y * 100, "rotation_dd": 0}, "support": "cradle",
			"launch_on": launch, "activate": true,
		}, &sc)
		return sc
	}
	// Contracts: validation, permissions, other marina, support refused.
	base := map[string]any{"term": "period", "customer_id": ds.customer, "vessel_id": vs[0], "starts_on": "2027-10-05", "ends_on": "2028-05-01", "area_id": area, "support": "rack"}
	c.expect(t, 422, "fit_blocked", "POST", M+"/storage/contracts", base, nil)
	base["support"] = "cradle"
	base["starts_on"] = "2027-02-30"
	c.expect(t, 400, "bad_request", "POST", M+"/storage/contracts", base, nil)
	base["starts_on"] = "2027-10-05"
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/storage/contracts", base, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/storage/contracts", base, nil)
	base["area_id"] = ds.area // a linear area is not a storage target
	c.expect(t, 422, "storage_target_invalid", "POST", M+"/storage/contracts", base, nil)

	a := create(vs[0], 12, 3, "2028-03-01")
	b := create(vs[1], 1, 3, "2028-04-15")
	create(vs[2], 12, 0, "2028-04-20")
	if a.Placement == nil || a.Details.Version != 1 || a.Details.ExitOn != "2028-03-01" || a.Contract.Kind != "storage" {
		t.Fatalf("storage contract %+v", a)
	}
	var got api.StorageContract
	r := ds.dock.expect(t, 200, "", "GET", M+"/storage/contracts/"+a.Contract.Id, nil, &got)
	if r.header.Get("ETag") != etag(1) {
		t.Fatalf("etag %q", r.header.Get("ETag"))
	}
	ds.other.expect(t, 404, "not_found", "GET", M+"/storage/contracts/"+a.Contract.Id, nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", X+"/storage/contracts/"+a.Contract.Id, nil, nil)
	c.expect(t, 428, "precondition_required", "PATCH", M+"/storage/contracts/"+a.Contract.Id, map[string]any{"notes_handling": "x"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/storage/contracts/"+a.Contract.Id, map[string]any{"notes_handling": "x"}, nil, "If-Match", etag(7))
	c.expect(t, 200, "", "PATCH", M+"/storage/contracts/"+a.Contract.Id, map[string]any{"launch_on": "2028-02-20"}, &got, "If-Match", etag(1))
	if got.Details.ExitOn != "2028-02-20" || got.Details.Version != 2 {
		t.Fatalf("patched %+v", got.Details)
	}

	// Areas, plan, accessibility, preset, evaluate.
	var areas api.StorageAreaList
	ds.dock.expect(t, 200, "", "GET", M+"/storage/areas", nil, &areas)
	if len(areas.Items) != 1 || !areas.Items[0].GatesValid {
		t.Fatalf("areas %+v", areas)
	}
	ds.yard.expect(t, 403, "forbidden", "GET", M+"/storage/areas", nil, nil)
	var plan api.StoragePlan
	c.expect(t, 200, "", "GET", M+"/storage/areas/"+area+"/plan?from=2027-10-05&to=2028-04-30", nil, &plan)
	blocked := 0
	for _, e := range plan.Access {
		if !e.Accessible {
			blocked++
		}
	}
	if len(plan.Items) != 3 || len(plan.Gates) != 1 || blocked != 1 {
		t.Fatalf("plan items %d gates %d blocked %d", len(plan.Items), len(plan.Gates), blocked)
	}
	c.expect(t, 422, "scenario_window_invalid", "GET", M+"/storage/areas/"+area+"/plan?from=2028-01-01&to=2027-01-01", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", M+"/storage/areas/"+area+"/plan", nil, nil)
	var acc api.StorageAccessibility
	c.expect(t, 200, "", "GET", M+"/storage/areas/"+area+"/accessibility?date=2028-02-20", nil, &acc)
	if len(acc.Entries) != 3 || len(acc.ExtractionOrder) != 0 {
		t.Fatalf("access %+v", acc)
	}
	var arr api.StorageArrangeResult
	c.expect(t, 200, "", "POST", M+"/storage/areas/"+area+"/arrange", map[string]any{"from_on": "2027-10-05", "to_on": "2028-04-30", "seed": 7, "max_iterations": 100}, &arr)
	if arr.Indicators.ExitsBlocked != 0 || len(arr.Items) != 3 || arr.Seed != 7 {
		t.Fatalf("arrange %+v", arr.Indicators)
	}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/storage/areas/"+area+"/arrange", map[string]any{}, nil)
	var ev api.StorageEvaluation
	c.expect(t, 200, "", "POST", M+"/storage/areas/"+area+"/evaluate", map[string]any{
		"from_on": "2027-10-05", "to_on": "2028-04-30",
		"items": []any{map[string]any{"allocation_id": a.Placement.AllocationId, "x_cm": 100, "y_cm": 300, "rotation_dd": 0}},
	}, &ev)
	if len(ev.Issues) == 0 {
		t.Fatalf("evaluate %+v", ev)
	}

	// Scenarios.
	sbody := map[string]any{"area_id": area, "name": "Inverno", "from_on": "2027-10-05", "to_on": "2028-04-30"}
	c.expect(t, 422, "scenario_window_invalid", "POST", M+"/storage/scenarios", map[string]any{"area_id": area, "name": "x", "from_on": "2027-10-05", "to_on": "2029-04-30"}, nil)
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/storage/scenarios", sbody, nil)
	var sd api.StorageScenarioDetail
	c.expect(t, 201, "", "POST", M+"/storage/scenarios", sbody, &sd)
	if len(sd.Items) != 3 || sd.Scenario.Indicators == nil || sd.Scenario.Indicators.ExitsBlocked != 1 {
		t.Fatalf("scenario %+v", sd.Scenario)
	}
	S := M + "/storage/scenarios/" + sd.Scenario.Id
	ds.other.expect(t, 404, "not_found", "GET", S, nil, nil)
	c.expect(t, 200, "", "PATCH", S, map[string]any{"name": "Inverno 27"}, &sd, "If-Match", etag(int(sd.Scenario.Version)))
	c.expect(t, 200, "", "PUT", S+"/items", map[string]any{"items": []any{
		map[string]any{"allocation_id": b.Placement.AllocationId, "x_cm": 1200, "y_cm": 700, "rotation_dd": 0},
	}}, &sd, "If-Match", etag(int(sd.Scenario.Version)))
	if len(sd.Moves) != 1 || sd.Scenario.Indicators.ExitsBlocked != 0 {
		t.Fatalf("saved %+v moves %d", sd.Scenario.Indicators, len(sd.Moves))
	}
	c.expect(t, 400, "validation_failed", "PUT", S+"/items", map[string]any{"items": []any{
		map[string]any{"allocation_id": b.Placement.AllocationId, "x_cm": 1900, "y_cm": 700, "rotation_dd": 0},
	}}, nil, "If-Match", etag(int(sd.Scenario.Version)))
	c.expect(t, 200, "", "POST", S+"/optimize", map[string]any{"seed": 3, "max_iterations": 50}, &sd, "If-Match", etag(int(sd.Scenario.Version)))
	if sd.Run == nil || sd.Run.Seed != 3 {
		t.Fatalf("optimize run %+v", sd.Run)
	}
	var cmp api.StorageScenarioComparison
	c.expect(t, 200, "", "GET", S+"/compare", nil, &cmp)
	if cmp.With != "real" || cmp.Right.ExitsBlocked != 1 {
		t.Fatalf("compare %+v", cmp)
	}
	var tray api.StorageTrayCandidateList
	ds.dock.expect(t, 200, "", "GET", S+"/tray-candidates", nil, &tray)
	for _, it := range tray.Items {
		if it.ResourceId == area {
			t.Fatalf("tray candidate on the scenario area: %+v", it)
		}
	}
	ds.other.expect(t, 404, "not_found", "GET", S+"/tray-candidates", nil, nil)
	var page api.StorageScenarioPage
	ds.dock.expect(t, 200, "", "GET", M+"/storage/scenarios?area_id="+area, nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("scenarios %d", len(page.Items))
	}
	c.expect(t, 400, "validation_failed", "DELETE", S+"/items/"+a.Placement.AllocationId, nil, nil, "If-Match", etag(int(sd.Scenario.Version)))
	var ap api.StorageApplyResult
	r = c.expect(t, 200, "", "POST", S+"/apply", nil, &ap, "If-Match", etag(int(sd.Scenario.Version)))
	if ap.Scenario.Status != "applied" || len(ap.Movements) == 0 || r.header.Get("ETag") == "" {
		t.Fatalf("apply %+v", ap.Scenario)
	}
	c.expect(t, 409, "scenario_not_draft", "POST", S+"/apply", nil, nil, "If-Match", etag(int(ap.Scenario.Version)))
	c.expect(t, 409, "scenario_not_draft", "DELETE", S, nil, nil, "If-Match", etag(int(ap.Scenario.Version)))
	c.expect(t, 409, "scenario_not_draft", "GET", S+"/tray-candidates", nil, nil)

	// Movements.
	var ml api.StorageMovementList
	ds.dock.expect(t, 200, "", "GET", M+"/storage/movements?date="+ap.Movements[0].MoveOn, nil, &ml)
	if len(ml.Items) != len(ap.Movements) {
		t.Fatalf("movements %d", len(ml.Items))
	}
	mv := ml.Items[0]
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/storage/movements/"+mv.Id+"/done", nil, nil, "If-Match", etag(1))
	ds.other.expect(t, 404, "not_found", "POST", M+"/storage/movements/"+mv.Id+"/done", nil, nil, "If-Match", etag(1))
	var done api.StorageMovement
	c.expect(t, 200, "", "POST", M+"/storage/movements/"+mv.Id+"/done", nil, &done, "If-Match", etag(1))
	if done.Status != "done" || done.DoneAt == nil {
		t.Fatalf("done %+v", done)
	}
	c.expect(t, 409, "movement_not_pending", "POST", M+"/storage/movements/"+mv.Id+"/done", nil, nil, "If-Match", etag(2))

	// Discard a second scenario.
	c.expect(t, 201, "", "POST", M+"/storage/scenarios", sbody, &sd)
	c.expect(t, 204, "", "DELETE", M+"/storage/scenarios/"+sd.Scenario.Id, nil, nil, "If-Match", etag(int(sd.Scenario.Version)))
}
