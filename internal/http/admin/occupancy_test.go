//go:build integration

package admin_test

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/http/api"
)

// unknownID is a well-formed id that exists nowhere.
const unknownID = "01900000-0000-7000-8000-00000000beef"

// occSetup is a pricing setup plus a small harbour saved through the API (a pier with 8 berths of 4 × 12 m on its
// two sides from the root and a 20 m linear area at the head of its left side) and two vessels.
type occSetup struct {
	*pricingSetup
	mapID   string
	berths  []string
	area    string
	vessels []string
	day     func(n int) string // today + n nights (YYYY-MM-DD)
}

func newOccSetup(t *testing.T) *occSetup {
	t.Helper()
	ps := newPricingSetup(t)
	o := &occSetup{pricingSetup: ps}
	today := calendar.In(time.Now(), time.UTC)
	o.day = func(n int) string { return today.AddDays(n).String() }
	c, M := ps.owner, ps.M
	var doc map[string]any
	c.expect(t, 201, "", "POST", M+"/maps", map[string]any{"kind": "water", "name": map[string]string{"it": "Porto"}, "width_cm": 80000, "height_cm": 50000}, &doc)
	o.mapID = doc["id"].(string)
	for _, k := range []string{"id", "version", "updated_at"} {
		delete(doc, k)
	}
	var gen map[string]any
	c.expect(t, 200, "", "POST", M+"/maps/"+o.mapID+"/generate", map[string]any{"document": doc, "generator": map[string]any{
		"type": "pier", "key_prefix": "g1-", "origin": map[string]int{"x": 5000, "y": 1000}, "rotation_dd": 900, "length_cm": 4000,
		"width_cm": 300, "code": "A", "sides": []string{"left", "right"}, "berths_per_side": 4, "berth_width_cm": 450,
		"berth_length_cm": 1200, "numbering": map[string]any{"scheme": "continuous", "prefix": "A-"},
	}}, &gen)
	els := gen["elements"].(map[string]any)
	doc["structures"], doc["berths"] = els["structures"], els["berths"]
	pierKey := els["structures"].([]any)[0].(map[string]any)["key"]
	c.expect(t, 200, "", "POST", M+"/maps/"+o.mapID+"/generate", map[string]any{"document": doc, "generator": map[string]any{
		"type": "linear_area", "key_prefix": "g2-", "structure_id": pierKey, "side": "left", "from_cm": 2000, "to_cm": 4000,
		"mode": "stern_to", "cell_cm": 50, "margin_cm": 50, "code": "L1",
	}}, &gen)
	doc["dynamic_areas"] = gen["elements"].(map[string]any)["dynamic_areas"]
	var saved api.MapSaveResult
	c.expect(t, 200, "", "PUT", M+"/maps/"+o.mapID, doc, &saved, "If-Match", etag(1))
	for _, b := range saved.Document.Berths {
		o.berths = append(o.berths, *b.Id)
	}
	if len(o.berths) != 8 || len(saved.Document.DynamicAreas) != 1 {
		t.Fatalf("%d berths, %d areas", len(o.berths), len(saved.Document.DynamicAreas))
	}
	o.area = *saved.Document.DynamicAreas[0].Id
	for _, dims := range [][2]int{{1000, 340}, {1500, 450}} {
		var v api.Vessel
		c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Vela", "loa_cm": dims[0], "beam_cm": dims[1], "hull_type": "monohull_sail"}, &v)
		o.vessels = append(o.vessels, v.Id)
	}
	return o
}

func TestOccupancyHandlers_SettingsAndClosures(t *testing.T) {
	o := newOccSetup(t)
	c, M := o.owner, o.M
	var s api.OccupancySettings
	r := c.expect(t, 200, "", "GET", M+"/occupancy-settings", nil, &s)
	if s.Version != 1 || s.SafetyClearanceCm != 30 || r.header.Get("ETag") != etag(1) {
		t.Fatalf("%+v %s", s, r.header.Get("ETag"))
	}
	body := map[string]any{
		"safety_clearance_cm": 40, "waste_threshold_bp": 6000, "draft_hold_days": 7, "desk_hold_hours": 24, "quote_validity_days": 7,
		"max_segments": 1, "absent_days_threshold": 7, "arrival_grace_minutes": 120, "default_arrival_time": "14:00",
		"default_departure_time": "11:00", "reprice_on_move": false, "release_policy": "with_consent",
	}
	c.expect(t, 428, "precondition_required", "PUT", M+"/occupancy-settings", body, nil)
	c.expect(t, 200, "", "PUT", M+"/occupancy-settings", body, &s, "If-Match", etag(1))
	c.expect(t, 412, "version_conflict", "PUT", M+"/occupancy-settings", body, nil, "If-Match", etag(1))
	body["max_segments"] = 9
	c.expect(t, 400, "validation_failed", "PUT", M+"/occupancy-settings", body, nil, "If-Match", etag(2))
	o.dock.expect(t, 403, "forbidden", "PUT", M+"/occupancy-settings", body, nil, "If-Match", etag(2))
	o.other.expect(t, 404, "not_found", "GET", M+"/occupancy-settings", nil, nil)

	var cl api.Closure
	c.expect(t, 201, "", "POST", M+"/closures", map[string]any{"scope_type": "map", "scope_id": o.mapID, "from_date": o.day(20), "to_date": o.day(21), "reason": "regata"}, &cl)
	c.expect(t, 400, "validation_failed", "POST", M+"/closures", map[string]any{"scope_type": "marina", "from_date": o.day(2), "to_date": o.day(1), "reason": "x"}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/closures", map[string]any{"scope_type": "map", "scope_id": unknownID, "from_date": o.day(1), "to_date": o.day(1), "reason": "x"}, nil)
	o.dock.expect(t, 403, "forbidden", "POST", M+"/closures", map[string]any{"scope_type": "marina", "from_date": o.day(1), "to_date": o.day(1), "reason": "x"}, nil)
	var list api.ClosureList
	o.dock.expect(t, 200, "", "GET", M+"/closures", nil, &list)
	if len(list.Items) != 1 {
		t.Fatalf("%d closures", len(list.Items))
	}
	// a block is not an arrival: closures do not refuse it
	c.expect(t, 201, "", "POST", M+"/blocks", map[string]any{"target": map[string]any{"resource_type": "berth", "resource_id": o.berths[0]}, "from": o.day(20), "to": o.day(20), "reason": "x"}, nil)
	c.expect(t, 412, "version_conflict", "DELETE", M+"/closures/"+cl.Id, nil, nil, "If-Match", etag(9))
	c.expect(t, 204, "", "DELETE", M+"/closures/"+cl.Id, nil, nil, "If-Match", etag(cl.Version))
	o.other.expect(t, 404, "not_found", "DELETE", M+"/closures/"+cl.Id, nil, nil, "If-Match", etag(1))
}

func TestOccupancyHandlers_Scenario(t *testing.T) {
	o := newOccSetup(t)
	c, M := o.owner, o.M
	berth := func(id string) map[string]any { return map[string]any{"resource_type": "berth", "resource_id": id} }

	// blocks
	var blk api.Allocation
	c.expect(t, 201, "", "POST", M+"/blocks", map[string]any{"target": berth(o.berths[2]), "from": o.day(1), "to": o.day(3), "reason": "lavori"}, &blk)
	c.expect(t, 409, "resource_taken", "POST", M+"/blocks", map[string]any{"target": berth(o.berths[2]), "from": o.day(3), "to": o.day(4), "reason": "lavori"}, nil)
	c.expect(t, 422, "placement_invalid", "POST", M+"/blocks", map[string]any{
		"target": map[string]any{"resource_type": "dynamic_area", "resource_id": o.area, "x_cm": 15}, "from": o.day(1), "to": o.day(1), "reason": "x", "loa_cm": 100, "beam_cm": 300,
	}, nil)
	o.yard.expect(t, 403, "forbidden", "POST", M+"/blocks", map[string]any{"target": berth(o.berths[1]), "from": o.day(1), "to": o.day(1), "reason": "x"}, nil)
	o.other.expect(t, 404, "not_found", "POST", M+"/blocks", map[string]any{"target": berth(o.berths[1]), "from": o.day(1), "to": o.day(1), "reason": "x"}, nil)

	// allocations come from the contract service; here two transits are written through the block-free engine path
	var a1, a2 api.Allocation
	c.expect(t, 201, "", "POST", M+"/blocks", map[string]any{"target": berth(o.berths[1]), "from": o.day(10), "to": o.day(12), "reason": "manutenzione"}, &a1)
	c.expect(t, 200, "", "GET", M+"/allocations/"+a1.Id, nil, &a2)
	if a2.Kind != "block" || a2.ResourceCode == nil || *a2.ResourceCode == "" {
		t.Fatalf("%+v", a2)
	}
	o.other.expect(t, 404, "not_found", "GET", M+"/allocations/"+a1.Id, nil, nil)
	c.expect(t, 404, "not_found", "GET", M+"/allocations/"+unknownID, nil, nil)

	// day view and occupancy range
	var dv api.DayView
	o.yard.expect(t, 200, "", "GET", M+"/maps/"+o.mapID+"/day?date="+o.day(2), nil, &dv)
	if len(dv.Elements) != 9 {
		t.Fatalf("%d elements", len(dv.Elements))
	}
	found := false
	for _, e := range dv.Elements {
		if e.ResourceId == o.berths[2] {
			found = e.Status == "block"
		}
		if e.Customer != nil && e.Customer.DisplayName != nil {
			t.Fatal("customer name without customers.read")
		}
	}
	if !found {
		t.Fatal("block not in the day view")
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/maps/"+o.mapID+"/day?date=31/12/2027", nil, nil)
	o.other.expect(t, 404, "not_found", "GET", M+"/maps/"+o.mapID+"/day", nil, nil)
	var mo api.MapOccupancy
	c.expect(t, 200, "", "GET", M+"/maps/"+o.mapID+"/occupancy?from="+o.day(0)+"&to="+o.day(30), nil, &mo)
	if len(mo.Allocations) != 2 {
		t.Fatalf("%d allocations", len(mo.Allocations))
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/maps/"+o.mapID+"/occupancy?from="+o.day(0)+"&to="+o.day(500), nil, nil)

	// search, check
	var sr api.OccupancySearchResult
	o.dock.expect(t, 200, "", "POST", M+"/occupancies/search", map[string]any{"purpose": "transit", "vessel_id": o.vessels[0], "arrival": o.day(10), "departure": o.day(13)}, &sr)
	if len(sr.Solutions) == 0 || sr.Considered != 9 {
		t.Fatalf("%d solutions, %d considered", len(sr.Solutions), sr.Considered)
	}
	for _, s := range sr.Solutions {
		if s.Segments[0].ResourceId == o.berths[1] {
			t.Fatal("blocked berth proposed")
		}
		if s.Pricing == nil && s.PricingError == nil {
			t.Fatal("solution without price nor pricing_error")
		}
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/occupancies/search", map[string]any{"purpose": "transit", "vessel_id": o.vessels[0], "arrival": o.day(10), "departure": o.day(9)}, nil)
	o.yard.expect(t, 403, "forbidden", "POST", M+"/occupancies/search", map[string]any{"purpose": "transit", "vessel_id": o.vessels[0], "arrival": o.day(10), "departure": o.day(11)}, nil)
	o.dock.expect(t, 403, "forbidden", "POST", M+"/occupancies/search", map[string]any{"purpose": "contract", "vessel_id": o.vessels[0], "arrival": o.day(10), "departure": o.day(11)}, nil)
	var chk api.PlacementCheck
	c.expect(t, 200, "", "POST", M+"/occupancies/check", map[string]any{"vessel_id": o.vessels[1], "target": berth(o.berths[0]), "from_night": o.day(10), "to_night": o.day(11)}, &chk)
	if chk.Ok || chk.Fit.Ok || len(chk.Fit.Blocking) == 0 {
		t.Fatalf("check %+v", chk)
	}
	c.expect(t, 200, "", "POST", M+"/occupancies/check", map[string]any{"vessel_id": o.vessels[0], "target": berth(o.berths[1]), "from_night": o.day(10), "to_night": o.day(11)}, &chk)
	if chk.Ok || len(chk.Conflicts) != 1 {
		t.Fatalf("check conflict %+v", chk)
	}

	// moves of a block are refused; a missing allocation is 404
	c.expect(t, 422, "placement_invalid", "POST", M+"/occupancies/move", map[string]any{"allocation_id": a1.Id, "version": a1.Version, "target": berth(o.berths[0])}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/occupancies/move", map[string]any{"allocation_id": unknownID, "version": 1, "target": berth(o.berths[0])}, nil)
	o.yard.expect(t, 403, "forbidden", "POST", M+"/occupancies/move", map[string]any{"allocation_id": a1.Id, "version": 1, "target": berth(o.berths[0])}, nil)
	o.dock.expect(t, 403, "forbidden", "POST", M+"/occupancies/move", map[string]any{"allocation_id": a1.Id, "version": 1, "target": berth(o.berths[0]), "fit_override": map[string]any{"reason": "x"}}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/occupancies/swap", map[string]any{"a": map[string]any{"allocation_id": unknownID, "version": 1}, "b": map[string]any{"allocation_id": a1.Id, "version": 1}}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/move-undo/"+unknownID, nil, nil)
	c.expect(t, 412, "version_conflict", "POST", M+"/dynamic-areas/"+o.area+"/layout", map[string]any{"date": o.day(0), "base": []any{}, "placements": []map[string]any{{"allocation_id": a1.Id, "x_cm": 100}}}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/dynamic-areas/"+unknownID+"/layout", map[string]any{"date": o.day(0), "base": []any{}, "placements": []any{}}, nil)
	var cp api.CompactionProposal
	c.expect(t, 200, "", "POST", M+"/dynamic-areas/"+o.area+"/compact", map[string]any{"from": o.day(0), "to": o.day(3)}, &cp)
	o.other.expect(t, 404, "not_found", "POST", M+"/dynamic-areas/"+o.area+"/compact", map[string]any{"from": o.day(0), "to": o.day(3)}, nil)

	// releases: blocks are not contract allocations
	c.expect(t, 409, "release_invalid", "POST", M+"/releases", map[string]any{"allocation_id": a1.Id, "from_night": o.day(10), "to_night": o.day(10), "source": "office"}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/releases", map[string]any{"allocation_id": a1.Id, "from_night": o.day(10), "to_night": o.day(10), "source": "system"}, nil)
	var rp api.ReleasePage
	c.expect(t, 200, "", "GET", M+"/releases", nil, &rp)
	o.yard.expect(t, 403, "forbidden", "GET", M+"/releases", nil, nil)
	c.expect(t, 404, "not_found", "POST", M+"/releases/"+unknownID+"/revoke", map[string]any{}, nil, "If-Match", etag(1))
	c.expect(t, 428, "precondition_required", "POST", M+"/releases/"+unknownID+"/revoke", map[string]any{}, nil)

	// delete block
	c.expect(t, 412, "version_conflict", "DELETE", M+"/blocks/"+blk.Id, nil, nil, "If-Match", etag(blk.Version+1))
	c.expect(t, 204, "", "DELETE", M+"/blocks/"+blk.Id, nil, nil, "If-Match", etag(blk.Version))
	c.expect(t, 409, "conflict", "DELETE", M+"/blocks/"+blk.Id, nil, nil, "If-Match", etag(blk.Version+1))
}
