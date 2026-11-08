//go:build integration

package admin_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestMaintenanceHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M, X := ds.owner, ds.M, ds.otherM

	// Equipment.
	var eq api.Equipment
	c.expect(t, 201, "", "POST", M+"/equipment", map[string]any{"code": "TL-1", "name": "Travel lift", "kind": "travel_lift"}, &eq)
	c.expect(t, 409, "code_taken", "POST", M+"/equipment", map[string]any{"code": "TL-1", "name": "Altro", "kind": "crane"}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/equipment", map[string]any{"code": "F1", "name": "F", "kind": "forklift", "x_cm": 10, "y_cm": 10}, nil)
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/equipment", map[string]any{"code": "Y", "name": "Y", "kind": "other"}, nil)
	var eqPage api.EquipmentPage
	ds.yard.expect(t, 200, "", "GET", M+"/equipment?kind=travel_lift", nil, &eqPage)
	if len(eqPage.Items) != 1 || eqPage.Items[0].OutOfService {
		t.Fatalf("equipment %+v", eqPage)
	}
	c.expect(t, 428, "precondition_required", "PATCH", M+"/equipment/"+eq.Id, map[string]any{"name": "TL 70"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/equipment/"+eq.Id, map[string]any{"name": "TL 70"}, nil, "If-Match", etag(9))
	c.expect(t, 200, "", "PATCH", M+"/equipment/"+eq.Id, map[string]any{"name": "TL 70"}, &eq, "If-Match", etag(1))
	ds.other.expect(t, 404, "not_found", "GET", X+"/equipment/"+eq.Id, nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", M+"/equipment/"+eq.Id, nil, nil)

	// Plans.
	plan := map[string]any{
		"name": "Ispezione", "intervention": "underwater_inspection", "lead_days": 15, "requires_free": true, "nights": 1,
		"targets":     []map[string]any{{"element_type": "berth", "element_id": ds.berths[0]}},
		"periodicity": map[string]any{"kind": "months", "every": 6, "starts_on": "2026-01-01"},
		"checklist":   []map[string]any{{"key": "catena", "label": "Catena"}},
	}
	var pl api.MaintenancePlan
	c.expect(t, 201, "", "POST", M+"/maintenance/plans", plan, &pl)
	if len(pl.Targets) != 1 || pl.Periodicity.Every == nil || *pl.Periodicity.Every != 6 {
		t.Fatalf("plan %+v", pl)
	}
	plan["periodicity"] = map[string]any{"kind": "fixed_date", "month_day": "02-30"}
	c.expect(t, 400, "validation_failed", "POST", M+"/maintenance/plans", plan, nil)
	plan["periodicity"] = map[string]any{"kind": "months", "every": 1}
	plan["targets"] = []map[string]any{{"element_type": "berth", "element_id": "01900000-0000-7000-8000-000000000000"}}
	c.expect(t, 422, "element_invalid", "POST", M+"/maintenance/plans", plan, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", M+"/maintenance/plans", nil, nil)
	var plPage api.MaintenancePlanPage
	c.expect(t, 200, "", "GET", M+"/maintenance/plans?element_type=berth", nil, &plPage)
	if len(plPage.Items) != 1 {
		t.Fatalf("plans %d", len(plPage.Items))
	}
	c.expect(t, 200, "", "PATCH", M+"/maintenance/plans/"+pl.Id, map[string]any{"lead_days": 30}, &pl, "If-Match", etag(int(pl.Version)))
	if pl.LeadDays != 30 || pl.Name != "Ispezione" {
		t.Fatalf("patched plan %+v", pl)
	}
	ds.other.expect(t, 404, "not_found", "GET", M+"/maintenance/plans/"+pl.Id, nil, nil)

	// Tasks: a vessel holds berth A1, so scheduling maintenance there conflicts.
	var tr api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-10", "departure": "2027-07-12",
		"placements": []any{seg(ds.berths[0], "2027-07-10", "2027-07-11")}, "confirm": map[string]any{},
	}, &tr)
	var tk api.MaintenanceTask
	c.expect(t, 201, "", "POST", M+"/maintenance/tasks", map[string]any{
		"title": "Sostituire cima", "element": map[string]any{"element_type": "berth", "element_id": ds.berths[0]},
		"out_of_service": true, "priority": "high", "due_on": "2027-07-01",
	}, &tk)
	if tk.Element == nil || tk.Element.Code == nil || *tk.Element.Code != "A1" || tk.Status != "open" {
		t.Fatalf("task %+v", tk)
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/maintenance/tasks", map[string]any{"title": "Senza posto"}, nil)
	r := c.expect(t, 409, "maintenance_conflicts", "POST", M+"/maintenance/tasks/"+tk.Id+"/schedule",
		map[string]any{"from_night": "2027-07-09", "to_night": "2027-07-10"}, nil, "If-Match", etag(int(tk.Version)))
	if !strings.Contains(string(r.body), tr.Id) {
		t.Fatalf("conflict details without the contract: %s", r.body)
	}
	c.expect(t, 428, "precondition_required", "POST", M+"/maintenance/tasks/"+tk.Id+"/start", nil, nil)
	c.expect(t, 200, "", "POST", M+"/maintenance/tasks/"+tk.Id+"/start", nil, &tk, "If-Match", etag(int(tk.Version)))
	c.expect(t, 200, "", "PATCH", M+"/maintenance/tasks/"+tk.Id, map[string]any{"supplier_ref": "ORD-1"}, &tk, "If-Match", etag(int(tk.Version)))
	var tPage api.MaintenanceTaskPage
	c.expect(t, 200, "", "GET", M+"/maintenance/tasks?status=in_progress", nil, &tPage)
	if len(tPage.Items) != 1 {
		t.Fatalf("tasks %d", len(tPage.Items))
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/maintenance/tasks?status=pending", nil, nil)

	// Files.
	png := harborPNG(t)
	if rr := c.raw(t, "POST", M+"/maintenance/tasks/"+tk.Id+"/files", "text/plain", strings.NewReader("x")); rr.status != 415 {
		t.Fatalf("text file: %d %s", rr.status, rr.body)
	}
	var f1, f2 api.MaintenanceFile
	for _, f := range []*api.MaintenanceFile{&f1, &f2} {
		rr := c.raw(t, "POST", M+"/maintenance/tasks/"+tk.Id+"/files", "image/png", bytes.NewReader(png))
		if rr.status != 200 {
			t.Fatalf("upload %d %s", rr.status, rr.body)
		}
		mntJSON(t, rr.body, f)
	}
	if f1.FileId != f2.FileId || f1.Kind != "photo" {
		t.Fatalf("same bytes gave %s and %s", f1.FileId, f2.FileId)
	}
	var link api.FileLink
	c.expect(t, 200, "", "GET", M+"/maintenance/tasks/"+tk.Id+"/files/"+f1.FileId+"/link", nil, &link)
	if !strings.Contains(link.Url, "sig=") {
		t.Fatalf("link %s", link.Url)
	}
	ds.other.expect(t, 404, "not_found", "GET", M+"/maintenance/tasks/"+tk.Id+"/files/"+f1.FileId+"/link", nil, nil)
	if rr := c.raw(t, "GET", link.Url, "", nil); rr.status != 200 || !bytes.Equal(rr.body, png) {
		t.Fatalf("signed file download: %d", rr.status)
	}
	c.expect(t, 403, "download_link_expired", "GET", M+"/maintenance/tasks/"+tk.Id+"/files/"+f1.FileId+"/file?exp=1&sig=x", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", link.Url, nil, nil)

	// Completion: offline, no If-Match; the same client_ref replays, another is 409.
	done := map[string]any{"outcome": "fixed", "client_ref": "out-1", "checklist_results": []any{}, "internal_minutes": 45}
	c.expect(t, 200, "", "POST", M+"/maintenance/tasks/"+tk.Id+"/complete", done, &tk)
	c.expect(t, 200, "", "POST", M+"/maintenance/tasks/"+tk.Id+"/complete", done, nil)
	done["client_ref"] = "out-2"
	c.expect(t, 409, "task_already_done", "POST", M+"/maintenance/tasks/"+tk.Id+"/complete", done, nil)
	c.expect(t, 409, "invalid_transition", "POST", M+"/maintenance/tasks/"+tk.Id+"/cancel", map[string]any{"reason": "x"}, nil, "If-Match", etag(int(tk.Version)))
	if tk.Outcome == nil || *tk.Outcome != "fixed" || tk.InternalMinutes == nil || *tk.InternalMinutes != 45 {
		t.Fatalf("done task %+v", tk)
	}

	// Field reports (offline): yard reports, the replay returns the same task.
	rep := map[string]any{"client_ref": "rep-1", "title": "Colonnina guasta", "element": map[string]any{"element_type": "berth", "element_id": ds.berths[1]}}
	var a, b api.MaintenanceTask
	ds.yard.expect(t, 201, "", "POST", M+"/maintenance/reports", rep, &a)
	ds.yard.expect(t, 201, "", "POST", M+"/maintenance/reports", rep, &b)
	if a.Id != b.Id || a.Source != "report" || len(a.AllocationIds) != 0 {
		t.Fatalf("report %+v / %+v", a, b)
	}
	ds.other.expect(t, 404, "not_found", "POST", M+"/maintenance/reports", rep, nil)
	var wl api.MaintenanceWorklist
	ds.yard.expect(t, 200, "", "GET", M+"/maintenance/worklist", nil, &wl)
	var cancelled api.MaintenanceTask
	c.expect(t, 200, "", "POST", M+"/maintenance/tasks/"+a.Id+"/cancel", map[string]any{"reason": "doppione"}, &cancelled, "If-Match", etag(int(a.Version)))
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancel %+v", cancelled)
	}

	// Map lens.
	var lens api.MaintenanceMap
	c.expect(t, 200, "", "GET", M+"/maintenance/map", nil, &lens)
	c.expect(t, 400, "bad_request", "GET", M+"/maintenance/map?date=oggi", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", M+"/maintenance/map", nil, nil)

	// Every task route of another marina is 404.
	for _, rt := range []struct{ method, path string }{
		{"GET", "/maintenance/tasks/" + tk.Id},
		{"PATCH", "/maintenance/tasks/" + tk.Id},
		{"POST", "/maintenance/tasks/" + tk.Id + "/schedule"},
		{"POST", "/maintenance/tasks/" + tk.Id + "/start"},
		{"POST", "/maintenance/tasks/" + tk.Id + "/complete"},
		{"POST", "/maintenance/tasks/" + tk.Id + "/release"},
		{"POST", "/maintenance/tasks/" + tk.Id + "/cancel"},
		{"GET", "/maintenance/tasks"},
		{"POST", "/maintenance/tasks"},
		{"GET", "/maintenance/plans"},
		{"POST", "/maintenance/plans"},
		{"PATCH", "/maintenance/plans/" + pl.Id},
		{"GET", "/equipment"},
		{"POST", "/equipment"},
		{"PATCH", "/equipment/" + eq.Id},
	} {
		ds.other.expect(t, 404, "not_found", rt.method, M+rt.path, map[string]any{}, nil, "If-Match", etag(1))
	}
}

func mntJSON(t *testing.T, b []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}
