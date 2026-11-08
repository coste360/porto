//go:build integration

package admin_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/workorder/workordertest"
	"github.com/coste360/porto/internal/http/api"
)

// woSetup is the desk setup plus a yard catalogue (inserted directly: the catalogue API belongs to the quote
// side), two employees (Marco linked to the yard user) and the yard settings.
type woSetup struct {
	*deskSetup
	items     map[string]string
	wt, qual  string
	luca      string
	marco     string
	extItemID string
}

func newWOSetup(t *testing.T) *woSetup {
	t.Helper()
	ds := newDeskSetup(t)
	ys := &woSetup{deskSetup: ds, items: map[string]string{}}
	c, M := ds.owner, ds.M
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	costs := map[string]int64{}
	for _, it := range []struct{ code, typ, unit string }{
		{"SRV-ALAG", "service", "m"}, {"LAB-HAND", "labour", "hour"}, {"MAT-AVEG", "material", "piece"}, {"EXT-SRV", "service", "lump"},
	} {
		var item api.Item
		c.expect(t, 201, "", "POST", M+"/items", map[string]any{
			"code": it.code, "name": map[string]string{"it": it.code}, "item_type": it.typ, "unit": it.unit,
			"revenue_class_id": classes.Items[0].Id, "price_includes_vat": false, "vat_rate_bp": 2200,
		}, &item)
		ys.items[it.code] = item.Id
		costs[item.Id] = 0
	}
	costs[ys.items["MAT-AVEG"]] = 5800
	ys.extItemID = ys.items["EXT-SRV"]
	b := workordertest.InsertYardBasics(t, ds.s.app.DB, ds.marinaID, costs, ys.items["LAB-HAND"], ys.extItemID, "yard@porto.test")
	ys.wt, ys.qual, ys.luca, ys.marco = b.WorkType, b.Qualification, b.Luca, b.Marco
	return ys
}

func TestWorkOrderHandlers_Scenario(t *testing.T) {
	ys := newWOSetup(t)
	c, M, X := ys.owner, ys.M, ys.otherM

	// An order without a quote: manual lines (12 m haul-out 18.00/m, 8 h at 38.00, 5 tins at 95.00).
	body := map[string]any{
		"customer_id": ys.customer, "vessel_id": ys.vessel, "work_type_id": ys.wt, "billing_mode": "actual",
		"manual_lines": []map[string]any{
			{"item_id": ys.items["SRV-ALAG"], "qty_milli": 12000, "unit_cents": 1800},
			{"item_id": ys.items["LAB-HAND"], "qty_milli": 8000, "unit_cents": 3800, "description": "Carteggiatura"},
			{"item_id": ys.items["MAT-AVEG"], "qty_milli": 5000, "unit_cents": 9500},
		},
	}
	var o api.WorkOrder
	c.expect(t, 201, "", "POST", M+"/yard/work-orders", body, &o, "Idempotency-Key", "wo-create-1")
	if o.Status != "planned" || len(o.Lines) != 3 || o.Lines[0].NetCents != 21600 || o.Lines[1].VatCents != 6688 || o.Number == "" {
		t.Fatalf("order %+v", o)
	}
	ys.yard.expect(t, 403, "forbidden", "POST", M+"/yard/work-orders", body, nil)
	ys.other.expect(t, 404, "not_found", "POST", M+"/yard/work-orders", body, nil)
	// Catalogue selections go through the quote line engine: an item without a price list rule has no price, an
	// unknown package is a validation error; neither writes an order.
	body["selections"] = []map[string]any{{"item_id": ys.items["SRV-ALAG"]}}
	c.expect(t, 422, "no_price", "POST", M+"/yard/work-orders", body, nil)
	body["selections"] = []map[string]any{{"package_id": "01a1bcb3-0000-7000-8000-000000000000"}}
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/work-orders", body, nil)
	delete(body, "selections")
	var page api.WorkOrderPage
	c.expect(t, 200, "", "GET", M+"/yard/work-orders?open_only=true", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("list %+v", page)
	}
	ys.dock.expect(t, 403, "forbidden", "GET", M+"/yard/work-orders", nil, nil)
	var board api.YardBoard
	c.expect(t, 200, "", "GET", M+"/yard/board", nil, &board)
	if len(board.Columns) == 0 || len(board.Columns[0].Cards) != 1 {
		t.Fatalf("board %+v", board)
	}
	c.expect(t, 428, "precondition_required", "PATCH", M+"/yard/work-orders/"+o.Id, map[string]any{"notes": "x"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/yard/work-orders/"+o.Id, map[string]any{"notes": "x"}, nil, "If-Match", etag(9))
	c.expect(t, 200, "", "PATCH", M+"/yard/work-orders/"+o.Id, map[string]any{"notes": "Antivegetativa", "position_note": "Piazzale 3"}, &o, "If-Match", etag(int(o.Version)))
	ys.other.expect(t, 404, "not_found", "GET", M+"/yard/work-orders/"+o.Id, nil, nil)
	ys.other.expect(t, 404, "not_found", "GET", X+"/yard/work-orders/"+o.Id, nil, nil)

	// Tasks: the labour line created one; add a second with a dependency.
	var tasks api.WorkTaskList
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id+"/tasks", nil, &tasks)
	if len(tasks.Items) != 1 || tasks.Items[0].EstimatedHoursMilli != 8000 || tasks.Items[0].Title != "Carteggiatura" {
		t.Fatalf("tasks %+v", tasks)
	}
	hand := tasks.Items[0]
	var tk api.WorkTask
	c.expect(t, 201, "", "POST", M+"/yard/work-orders/"+o.Id+"/tasks", map[string]any{
		"title": "Controllo", "qualification_id": ys.qual, "estimated_hours_milli": 2000, "checklist": []string{"Elica"}, "depends_on": []string{hand.Id},
	}, &tk)
	c.expect(t, 422, "dependency_violation", "PUT", M+"/yard/tasks/"+hand.Id+"/dependencies", map[string]any{"depends_on": []string{tk.Id}}, nil, "If-Match", etag(int(hand.Version)))
	c.expect(t, 200, "", "PATCH", M+"/yard/tasks/"+tk.Id, map[string]any{"title": "Controllo elica"}, &tk, "If-Match", etag(int(tk.Version)))
	ys.yard.expect(t, 403, "forbidden", "POST", M+"/yard/tasks/"+tk.Id+"/progress", map[string]any{"client_ref": "pr-1", "progress_bp": 5000}, nil)
	c.expect(t, 200, "", "POST", M+"/yard/tasks/"+tk.Id+"/progress", map[string]any{"client_ref": "pr-1", "progress_bp": 5000}, &tk)
	if tk.Status != "in_progress" || tk.ProgressBp != 5000 {
		t.Fatalf("progress %+v", tk)
	}
	// Photo upload (PNG), list, signed link, download.
	r := c.raw(t, "POST", M+"/yard/tasks/"+tk.Id+"/photos", "image/png", bytes.NewReader(testPNG), "Idempotency-Key", "photo-0001")
	if r.status != 200 {
		t.Fatalf("upload %d %s", r.status, r.body)
	}
	r = c.raw(t, "POST", M+"/yard/tasks/"+tk.Id+"/photos", "text/plain", bytes.NewReader([]byte("x")))
	if r.status != 415 {
		t.Fatalf("unsupported %d", r.status)
	}
	var files api.WorkTaskFileList
	c.expect(t, 200, "", "GET", M+"/yard/tasks/"+tk.Id+"/files", nil, &files)
	if len(files.Items) != 1 {
		t.Fatalf("files %+v", files)
	}
	var link api.FileLink
	c.expect(t, 200, "", "GET", M+"/yard/tasks/"+tk.Id+"/files/"+files.Items[0].FileId+"/link", nil, &link)
	if r := c.raw(t, "GET", link.Url, "", nil); r.status != 200 || r.header.Get("Content-Type") != "image/png" {
		t.Fatalf("download %d", r.status)
	}
	ys.other.expect(t, 404, "not_found", "GET", link.Url, nil, nil)
	c.expect(t, 204, "", "DELETE", M+"/yard/tasks/"+tk.Id, nil, nil, "If-Match", etag(int(tk.Version)))

	// Time entries: Marco (the yard user) on his assigned task from the phone; Luca by the manager.
	workordertest.Assign(t, ys.s.app.DB, ys.marinaID, hand.Id, ys.marco, calendar.In(time.Now().UTC(), time.UTC), 4000)
	var timer api.Timesheet
	ys.yard.expect(t, 201, "", "POST", M+"/timesheets/timer", map[string]any{"client_ref": "tm-1", "task_id": hand.Id}, &timer)
	ys.yard.expect(t, 409, "timer_already_running", "POST", M+"/timesheets/timer", map[string]any{"client_ref": "tm-2", "task_id": hand.Id}, nil)
	var my api.MyYardWork
	ys.yard.expect(t, 200, "", "GET", M+"/yard/my-work", nil, &my)
	if my.RunningTimer == nil || len(my.Assignments) != 1 {
		t.Fatalf("my work %+v", my)
	}
	ys.yard.expect(t, 200, "", "POST", M+"/timesheets/"+timer.Id+"/stop", map[string]any{"client_ref": "st-1"}, &timer)
	if timer.Status != "submitted" {
		t.Fatalf("stopped %+v", timer)
	}
	var manual api.Timesheet
	ys.yard.expect(t, 201, "", "POST", M+"/timesheets", map[string]any{"client_ref": "me-1", "task_id": hand.Id, "hours_milli": 4000}, &manual)
	ys.yard.expect(t, 400, "validation_failed", "POST", M+"/timesheets", map[string]any{"client_ref": "me-2", "task_id": hand.Id, "hours_milli": 4100}, nil)
	ys.yard.expect(t, 422, "duration_too_long", "POST", M+"/timesheets", map[string]any{"client_ref": "me-3", "task_id": hand.Id, "hours_milli": 16250}, nil)
	var luca api.Timesheet
	c.expect(t, 201, "", "POST", M+"/timesheets", map[string]any{"client_ref": "me-l", "employee_id": ys.luca, "task_id": hand.Id, "hours_milli": 5000}, &luca)
	ys.yard.expect(t, 403, "forbidden", "POST", M+"/timesheets/approve", map[string]any{"entries": []map[string]any{{"id": luca.Id, "version": luca.Version}}}, nil)
	var approved api.TimesheetList
	c.expect(t, 200, "", "POST", M+"/timesheets/approve", map[string]any{"entries": []map[string]any{
		{"id": luca.Id, "version": luca.Version}, {"id": manual.Id, "version": manual.Version},
	}}, &approved)
	if len(approved.Items) != 2 || approved.Items[0].CostCents == nil {
		t.Fatalf("approved %+v", approved)
	}
	c.expect(t, 200, "", "POST", M+"/timesheets/"+timer.Id+"/reject", map[string]any{"reason": "doppione"}, &timer, "If-Match", etag(int(timer.Version)))
	var mine api.TimesheetPage
	ys.yard.expect(t, 200, "", "GET", M+"/timesheets", nil, &mine)
	if len(mine.Items) != 2 {
		t.Fatalf("own entries %d", len(mine.Items))
	}
	ys.other.expect(t, 404, "not_found", "GET", M+"/timesheets", nil, nil)

	// Materials and external costs.
	var mv api.StockMovement
	c.expect(t, 201, "", "POST", M+"/stock-movements", map[string]any{"work_order_id": o.Id, "item_id": ys.items["MAT-AVEG"], "kind": "issue", "qty_milli": 6000}, &mv)
	if mv.UnitCostCents != 5800 || mv.UnitPriceCents != 9500 {
		t.Fatalf("movement %+v", mv)
	}
	c.expect(t, 422, "return_exceeds_issued", "POST", M+"/stock-movements", map[string]any{"work_order_id": o.Id, "item_id": ys.items["MAT-AVEG"], "kind": "return", "qty_milli": 7000}, nil)
	c.expect(t, 201, "", "POST", M+"/stock-movements", map[string]any{"work_order_id": o.Id, "item_id": ys.items["MAT-AVEG"], "kind": "return", "qty_milli": 1000}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/stock-movements", map[string]any{"work_order_id": o.Id, "item_id": ys.items["SRV-ALAG"], "kind": "issue", "qty_milli": 1000}, nil)
	var movs api.StockMovementPage
	c.expect(t, 200, "", "GET", M+"/stock-movements?work_order_id="+o.Id, nil, &movs)
	if len(movs.Items) != 2 {
		t.Fatalf("movements %d", len(movs.Items))
	}
	var x api.ExternalCost
	c.expect(t, 201, "", "POST", M+"/yard/work-orders/"+o.Id+"/external-costs", map[string]any{"supplier_name": "Eliche Srl", "cost_cents": 15000}, &x)
	if x.PriceCents != 18000 || !x.Billable {
		t.Fatalf("external %+v", x)
	}
	c.expect(t, 200, "", "PATCH", M+"/yard/external-costs/"+x.Id, map[string]any{"cost_cents": 10000}, &x, "If-Match", etag(int(x.Version)))
	if x.PriceCents != 12000 {
		t.Fatalf("markup after patch %d", x.PriceCents)
	}
	var xs api.ExternalCostList
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id+"/external-costs", nil, &xs)
	ys.other.expect(t, 404, "not_found", "PATCH", M+"/yard/external-costs/"+x.Id, map[string]any{"cost_cents": 1}, nil, "If-Match", etag(int(x.Version)))

	// Final account, CSV, billing preview and final.
	var acc api.FinalAccount
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id+"/account", nil, &acc)
	// 12 × 18.00 + 9 h × 38.00 + 5 × 95.00 + 120.00 = 216 + 342 + 475 + 120.
	if acc.Totals.RevenueCents != 115300 || acc.Totals.HoursLabourMilli != 9000 || len(acc.Lines) != 4 {
		t.Fatalf("account %+v", acc.Totals)
	}
	if r := c.raw(t, "GET", M+"/yard/work-orders/"+o.Id+"/account?format=csv", "", nil); r.status != 200 || !bytes.Contains(r.body, []byte("Ricavo")) {
		t.Fatalf("csv %d", r.status)
	}
	c.expect(t, 409, "invalid_transition", "POST", M+"/yard/work-orders/"+o.Id+"/billings", map[string]any{"kind": "final"}, nil)
	var prev api.WorkOrderBillingResult
	c.expect(t, 200, "", "POST", M+"/yard/work-orders/"+o.Id+"/billings?dry_run=true", map[string]any{"kind": "progress", "amount_cents": 10000}, &prev)
	if !prev.DryRun || prev.GrossCents != 10000 || prev.Billing != nil {
		t.Fatalf("preview %+v", prev)
	}
	// The first approved entry started the order (implicit start).
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id, nil, &o)
	if o.Status != "in_progress" {
		t.Fatalf("implicit start %s", o.Status)
	}
	c.expect(t, 409, "invalid_transition", "POST", M+"/yard/work-orders/"+o.Id+"/transition", map[string]any{"action": "start"}, nil, "If-Match", etag(int(o.Version)))
	c.expect(t, 200, "", "POST", M+"/yard/work-orders/"+o.Id+"/transition", map[string]any{"action": "complete"}, &o, "If-Match", etag(int(o.Version)))
	ys.yard.expect(t, 403, "forbidden", "POST", M+"/yard/work-orders/"+o.Id+"/billings", map[string]any{"kind": "final"}, nil)
	var fin api.WorkOrderBillingResult
	c.expect(t, 201, "", "POST", M+"/yard/work-orders/"+o.Id+"/billings", map[string]any{"kind": "final"}, &fin, "Idempotency-Key", "bill-final-1")
	if fin.ChargeId == nil || fin.GrossCents != 140666 { // 1,153.00 + 22 % VAT
		t.Fatalf("final %+v", fin)
	}
	var bl api.WorkOrderBillingList
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id+"/billings", nil, &bl)
	if len(bl.Items) != 1 || bl.ChargedGrossCents != 140666 || bl.Items[0].ChargeNumber == nil {
		t.Fatalf("billings %+v", bl)
	}
	c.expect(t, 200, "", "GET", M+"/yard/work-orders/"+o.Id, nil, &o)
	if o.Status != "invoiced" {
		t.Fatalf("invoiced %s", o.Status)
	}
	c.expect(t, 409, "invalid_transition", "POST", M+"/yard/work-orders/"+o.Id+"/transition", map[string]any{"action": "cancel", "reason": "x"}, nil, "If-Match", etag(int(o.Version)))

	// Variation quotes are created by the quote side; the order check runs first.
	ys.other.expect(t, 404, "not_found", "POST", M+"/yard/work-orders/"+o.Id+"/variations", map[string]any{}, nil)

	// Yard report and the cross-marina sweep of the order routes.
	var rep api.YardReport
	c.expect(t, 200, "", "GET", M+"/reports/yard?view=hours", nil, &rep)
	if rep.Totals.HoursMilli != 9000 {
		t.Fatalf("yard report %+v", rep.Totals)
	}
	c.expect(t, 422, "", "GET", M+"/reports/yard?view=hours&group_by=equipment", nil, nil)
	ys.dock.expect(t, 403, "forbidden", "GET", M+"/reports/yard", nil, nil)
	for _, rt := range []struct{ method, path string }{
		{"GET", "/yard/work-orders"},
		{"POST", "/yard/work-orders"},
		{"GET", "/yard/board"},
		{"GET", "/yard/work-orders/" + o.Id},
		{"PATCH", "/yard/work-orders/" + o.Id},
		{"POST", "/yard/work-orders/" + o.Id + "/transition"},
		{"GET", "/yard/work-orders/" + o.Id + "/tasks"},
		{"POST", "/yard/work-orders/" + o.Id + "/tasks"},
		{"PATCH", "/yard/tasks/" + hand.Id},
		{"DELETE", "/yard/tasks/" + hand.Id},
		{"PUT", "/yard/tasks/" + hand.Id + "/dependencies"},
		{"POST", "/yard/tasks/" + hand.Id + "/progress"},
		{"GET", "/yard/tasks/" + hand.Id + "/files"},
		{"GET", "/timesheets"},
		{"POST", "/timesheets"},
		{"POST", "/timesheets/timer"},
		{"POST", "/timesheets/" + luca.Id + "/stop"},
		{"PATCH", "/timesheets/" + luca.Id},
		{"POST", "/timesheets/approve"},
		{"POST", "/timesheets/" + luca.Id + "/reject"},
		{"POST", "/timesheets/" + luca.Id + "/void"},
		{"GET", "/yard/my-work"},
		{"GET", "/stock-movements"},
		{"POST", "/stock-movements"},
		{"GET", "/yard/work-orders/" + o.Id + "/external-costs"},
		{"POST", "/yard/work-orders/" + o.Id + "/external-costs"},
		{"DELETE", "/yard/external-costs/" + x.Id},
		{"GET", "/yard/work-orders/" + o.Id + "/account"},
		{"GET", "/yard/work-orders/" + o.Id + "/billings"},
		{"POST", "/yard/work-orders/" + o.Id + "/billings"},
		{"GET", "/reports/yard"},
	} {
		ys.other.expect(t, 404, "not_found", rt.method, M+rt.path, map[string]any{}, nil, "If-Match", etag(1))
	}
}
