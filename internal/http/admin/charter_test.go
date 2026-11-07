//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestCharterHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	var small api.Vessel
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Maestrale", "loa_cm": 1100, "beam_cm": 370, "hull_type": "monohull_sail"}, &small)
	body := map[string]any{"customer_id": ds.customer, "name": "Flotta Demo", "vessel_ids": []string{ds.vessel, small.Id}}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/charter-fleets", body, nil)
	var fl api.CharterFleet
	c.expect(t, 201, "", "POST", M+"/charter-fleets", body, &fl)
	if len(fl.Vessels) != 2 {
		t.Fatalf("fleet %+v", fl)
	}
	c.expect(t, 409, "vessel_in_other_fleet", "POST", M+"/charter-fleets", body, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/charter-fleets/"+fl.Id, nil, nil)
	c.expect(t, 200, "", "PATCH", M+"/charter-fleets/"+fl.Id, map[string]any{"name": "Flotta"}, &fl, "If-Match", etag(fl.Version))
	var list api.CharterFleetList
	c.expect(t, 200, "", "GET", M+"/charter-fleets", nil, &list)
	if len(list.Items) != 1 || list.Items[0].Name != "Flotta" {
		t.Fatalf("fleets %+v", list)
	}
	ch := map[string]any{
		"fleet_id": fl.Id, "starts_on": "2027-06-05", "ends_on": "2027-09-01", "changeover_weekday": 6, "pricing_mode": "flat",
		"flat_cents": 900000, "flat_reason": "accordo", "activate": true,
		"placements": []any{map[string]any{"target": map[string]any{"resource_type": "berth", "resource_id": ds.berths[1]}, "vessel_id": ds.vessel}},
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/charters", map[string]any{"fleet_id": fl.Id, "starts_on": "2027-06-05", "ends_on": "2027-09-01", "changeover_weekday": 9, "pricing_mode": "flat", "placements": []any{}}, nil)
	var ct api.Contract
	c.expect(t, 201, "", "POST", M+"/charters", ch, &ct)
	if ct.Kind != "charter" || ct.Charter == nil || ct.TotalCents != 900000 {
		t.Fatalf("charter %+v", ct)
	}
	rot := map[string]any{"from_night": "2027-07-03", "assignments": []any{map[string]any{"line_id": ct.Lines[0].Id, "vessel_id": small.Id}}}
	c.expect(t, 428, "precondition_required", "POST", M+"/charters/"+ct.Id+"/rotate", rot, nil)
	c.expect(t, 200, "", "POST", M+"/charters/"+ct.Id+"/rotate", rot, &ct, "If-Match", etag(ct.Version))
	if ct.TotalCents != 900000 {
		t.Fatal("rotation repriced")
	}
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/charters/"+ct.Id+"/rotate", rot, nil, "If-Match", etag(ct.Version))
}
