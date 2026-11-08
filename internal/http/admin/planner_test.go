//go:build integration

package admin_test

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/yardplan/yardtest"
	"github.com/coste360/porto/internal/http/api"
)

// planSetup adds two qualifications (one for lift operators) with their labour items and a work order with two
// dependent tasks (rows: the work order service is not needed here).
type planSetup struct {
	*deskSetup
	paint, lift string
	order       string
	tasks       []string
	monday      calendar.Date // a Monday four weeks ahead
}

func newPlanSetup(t *testing.T) *planSetup {
	t.Helper()
	ds := newDeskSetup(t)
	p := &planSetup{deskSetup: ds}
	c, M := ds.owner, ds.M
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	for i, code := range []string{"PAINT", "LIFT"} {
		var it api.Item
		c.expect(t, 201, "", "POST", M+"/items", map[string]any{
			"code": "LAB-" + code, "name": map[string]string{"it": code}, "item_type": "labour", "unit": "hour",
			"revenue_class_id": classes.Items[0].Id,
		}, &it)
		id := yardtest.InsertQualification(t, ds.s.app.DB, time.Now(), ds.marinaID, it.Id, code, i == 1)
		if i == 0 {
			p.paint = id
		} else {
			p.lift = id
		}
	}
	today := calendar.In(time.Now(), time.UTC)
	p.monday = today.AddDays(28 + 1 - today.ISOWeekday())
	o := yardtest.InsertPlannedOrder(t, ds.s.app.DB, time.Now(), ds.marinaID, ds.customer, ds.vessel, p.monday.AddDays(4),
		yardtest.TaskSpec{Title: "Carteggiatura", QualificationID: p.paint, EstHoursMilli: 8000},
		yardtest.TaskSpec{Title: "Antivegetativa", QualificationID: p.paint, EstHoursMilli: 8000, DependsOn: []int{0}},
	)
	p.order, p.tasks = o.ID, o.Tasks
	return p
}

func TestPlannerHandlers_Scenario(t *testing.T) {
	p := newPlanSetup(t)
	c, M, X := p.owner, p.M, p.otherM
	day := func(n int) string { return p.monday.AddDays(n).String() }

	// Employees.
	var e1, e2 api.Employee
	c.expect(t, 201, "", "POST", M+"/yard/employees", map[string]any{
		"code": "P1", "first_name": "Paolo", "last_name": "Neri", "qualification_ids": []string{p.paint}, "hourly_cost_cents": 2600,
	}, &e1)
	if len(e1.Calendar) != 7 || e1.Calendar[0].Minutes != 480 || e1.Calendar[5].Minutes != 0 || e1.Version != 1 {
		t.Fatalf("employee %+v", e1)
	}
	c.expect(t, 201, "", "POST", M+"/yard/employees", map[string]any{
		"code": "G1", "first_name": "Gino", "last_name": "Gru", "qualification_ids": []string{p.lift},
		"calendar": []map[string]int{{"weekday": 1, "minutes": 240}},
	}, &e2)
	c.expect(t, 409, "code_taken", "POST", M+"/yard/employees", map[string]any{"code": "P1", "first_name": "A", "last_name": "B"}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/employees", map[string]any{
		"code": "P2", "first_name": "A", "last_name": "B",
		"qualification_ids": []string{unknownID},
	}, nil)
	p.yard.expect(t, 403, "forbidden", "GET", M+"/yard/employees", nil, nil)
	p.other.expect(t, 404, "not_found", "GET", M+"/yard/employees", nil, nil)
	p.other.expect(t, 404, "not_found", "GET", X+"/yard/employees/"+e1.Id, nil, nil)
	var page api.EmployeePage
	c.expect(t, 200, "", "GET", M+"/yard/employees?qualification_id="+p.paint, nil, &page)
	if len(page.Items) != 1 || page.Items[0].Id != e1.Id {
		t.Fatalf("employees %+v", page)
	}
	c.expect(t, 428, "precondition_required", "PATCH", M+"/yard/employees/"+e1.Id, map[string]any{"department": "Vernici"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/yard/employees/"+e1.Id, map[string]any{"department": "Vernici"}, nil, "If-Match", etag(5))
	c.expect(t, 200, "", "PATCH", M+"/yard/employees/"+e1.Id, map[string]any{"department": "Vernici"}, &e1, "If-Match", etag(1))
	c.expect(t, 200, "", "PUT", M+"/yard/employees/"+e1.Id+"/calendar", map[string]any{"days": []map[string]int{
		{"weekday": 1, "minutes": 480}, {"weekday": 2, "minutes": 480}, {"weekday": 3, "minutes": 480}, {"weekday": 4, "minutes": 480}, {"weekday": 5, "minutes": 240},
	}}, &e1, "If-Match", etag(int(e1.Version)))
	if e1.Calendar[4].Minutes != 240 || e1.Version != 3 {
		t.Fatalf("calendar %+v", e1)
	}
	c.expect(t, 200, "", "GET", M+"/yard/employees/"+e1.Id, nil, &e1)
	p.other.expect(t, 404, "not_found", "PUT", M+"/yard/employees/"+e1.Id+"/calendar", map[string]any{"days": []any{}}, nil, "If-Match", etag(3))

	// Absences.
	var ab api.Absence
	c.expect(t, 201, "", "POST", M+"/yard/employees/"+e1.Id+"/absences", map[string]any{"from_on": day(2), "to_on": day(2), "kind": "leave", "minutes_per_day": 240}, &ab)
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/employees/"+e1.Id+"/absences", map[string]any{"from_on": day(3), "to_on": day(2), "kind": "sick"}, nil)
	c.expect(t, 400, "", "POST", M+"/yard/employees/"+e1.Id+"/absences", map[string]any{"from_on": "2027-02-30", "to_on": day(2), "kind": "sick"}, nil)
	var abl api.AbsenceList
	c.expect(t, 200, "", "GET", M+"/yard/employees/"+e1.Id+"/absences?from="+day(0), nil, &abl)
	if len(abl.Items) != 1 {
		t.Fatalf("absences %+v", abl)
	}
	p.other.expect(t, 404, "not_found", "DELETE", M+"/yard/employees/"+e1.Id+"/absences/"+ab.Id, nil, nil)

	// Assignments.
	ch := func(task, emp, date string, h int, v int64) map[string]any {
		return map[string]any{"task_id": task, "employee_id": emp, "work_date": date, "hours_milli": h, "task_version": v}
	}
	c.expect(t, 422, "qualification_mismatch", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[0], e2.Id, day(0), 4000, 1)}}, nil)
	r := c.expect(t, 409, "capacity_exceeded", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[0], e1.Id, day(2), 6000, 1)}}, nil)
	if !containsStr(string(r.body), `"capacity_milli":4000`) {
		t.Fatalf("capacity details %s", r.body)
	}
	c.expect(t, 412, "version_conflict", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[0], e1.Id, day(0), 4000, 7)}}, nil)
	var res api.AssignmentBatchResult
	c.expect(t, 200, "", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[0], e1.Id, day(1), 8000, 1)}}, &res)
	if len(res.Assignments) != 1 || len(res.Tasks) != 1 || res.Tasks[0].Version != 2 {
		t.Fatalf("batch %+v", res)
	}
	c.expect(t, 422, "dependency_violation", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[1], e1.Id, day(0), 2000, 1)}}, nil)
	p.other.expect(t, 404, "not_found", "PUT", X+"/yard/planner/assignments", map[string]any{"changes": []any{ch(p.tasks[1], e1.Id, day(3), 2000, 1)}}, nil)
	c.expect(t, 404, "not_found", "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []any{ch(unknownID, e1.Id, day(3), 2000, 1)}}, nil)

	var pl api.Planner
	c.expect(t, 200, "", "GET", M+"/yard/planner?from="+day(0)+"&to="+day(6), nil, &pl)
	if len(pl.Rows) != 2 || len(pl.Assignments) != 1 || len(pl.Tasks) != 2 || len(pl.Rows[0].Days) != 7 {
		t.Fatalf("planner %+v", pl)
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/yard/planner?from="+day(6)+"&to="+day(0), nil, nil)
	p.other.expect(t, 404, "not_found", "GET", M+"/yard/planner", nil, nil)

	var ld api.YardLoad
	c.expect(t, 200, "", "GET", M+"/yard/load?group_by=department&period=week&from="+day(0)+"&to="+day(6), nil, &ld)
	if len(ld.Rows) != 2 {
		t.Fatalf("load %+v", ld)
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/yard/load?group_by=bay&from=2027-13-01", nil, nil)
	var risk api.AtRiskOrderList
	c.expect(t, 200, "", "GET", M+"/yard/at-risk", nil, &risk)
	var prop api.YardPlanProposal
	c.expect(t, 200, "", "POST", M+"/yard/planner/proposal", map[string]any{"from": day(2), "horizon_days": 10}, &prop)
	if len(prop.Changes) == 0 {
		t.Fatalf("proposal %+v", prop)
	}
	c.expect(t, 200, "", "POST", M+"/yard/planner/proposal", nil, &prop)
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/planner/proposal", map[string]any{"horizon_days": 0, "from": "2000-01-01"}, nil)
	p.yard.expect(t, 403, "forbidden", "POST", M+"/yard/planner/proposal", nil, nil)

	a := res.Assignments[0]
	c.expect(t, 428, "precondition_required", "DELETE", M+"/yard/planner/assignments/"+a.Id, nil, nil)
	p.other.expect(t, 404, "not_found", "DELETE", M+"/yard/planner/assignments/"+a.Id, nil, nil, "If-Match", etag(1))
	c.expect(t, 204, "", "DELETE", M+"/yard/planner/assignments/"+a.Id, nil, nil, "If-Match", etag(int(a.Version)))
	c.expect(t, 204, "", "DELETE", M+"/yard/employees/"+e1.Id+"/absences/"+ab.Id, nil, nil)

	// Lifting.
	var eq api.Equipment
	c.expect(t, 201, "", "POST", M+"/equipment", map[string]any{"code": "TL-1", "name": "Travel lift", "kind": "travel_lift", "capacity_kg": 50000}, &eq)
	var ops api.EquipmentOperators
	c.expect(t, 422, "operator_not_enabled", "PUT", M+"/equipment/"+eq.Id+"/operators", map[string]any{"employee_ids": []string{e1.Id}}, nil)
	c.expect(t, 200, "", "PUT", M+"/equipment/"+eq.Id+"/operators", map[string]any{"employee_ids": []string{e2.Id}}, &ops)
	c.expect(t, 200, "", "GET", M+"/equipment/"+eq.Id+"/operators", nil, &ops)
	if len(ops.EmployeeIds) != 1 {
		t.Fatalf("operators %+v", ops)
	}
	p.other.expect(t, 404, "not_found", "GET", M+"/equipment/"+eq.Id+"/operators", nil, nil)
	book := map[string]any{
		"equipment_id": eq.Id, "date": day(0), "from_slot": 32, "to_slot": 36, "purpose": "haul_out", "vessel_id": p.vessel,
		"operator_employee_id": e2.Id,
	}
	var b api.EquipmentBooking
	c.expect(t, 201, "", "POST", M+"/equipment-bookings", book, &b)
	if b.Status != "booked" || b.VesselName == nil || *b.VesselName != "Libeccio" {
		t.Fatalf("booking %+v", b)
	}
	book["from_slot"], book["to_slot"], book["operator_employee_id"] = 34, 38, nil
	r = c.expect(t, 409, "equipment_slot_taken", "POST", M+"/equipment-bookings", book, nil)
	if !containsStr(string(r.body), b.Id) {
		t.Fatalf("conflict without holder %s", r.body)
	}
	book["from_slot"], book["to_slot"] = 10, 12
	c.expect(t, 422, "outside_lift_window", "POST", M+"/equipment-bookings", book, nil)
	book["purpose"] = "nope"
	c.expect(t, 400, "validation_failed", "POST", M+"/equipment-bookings", book, nil)
	p.other.expect(t, 404, "not_found", "POST", M+"/equipment-bookings", book, nil)
	var dayView api.EquipmentBookingDay
	p.yard.expect(t, 200, "", "GET", M+"/equipment-bookings?date="+day(0), nil, &dayView)
	if len(dayView.Bookings) != 1 || len(dayView.Equipment) != 1 || dayView.WindowFromSlot != 28 {
		t.Fatalf("day %+v", dayView)
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/equipment-bookings?status=nope", nil, nil)
	p.other.expect(t, 404, "not_found", "GET", M+"/equipment-bookings", nil, nil)
	c.expect(t, 428, "precondition_required", "PATCH", M+"/equipment-bookings/"+b.Id, map[string]any{"from_slot": 40, "to_slot": 44}, nil)
	c.expect(t, 200, "", "PATCH", M+"/equipment-bookings/"+b.Id, map[string]any{"from_slot": 40, "to_slot": 44}, &b, "If-Match", etag(int(b.Version)))
	p.other.expect(t, 404, "not_found", "PATCH", M+"/equipment-bookings/"+b.Id, map[string]any{"from_slot": 40}, nil, "If-Match", etag(int(b.Version)))
	c.expect(t, 200, "", "POST", M+"/equipment-bookings/"+b.Id+"/complete", nil, &b, "If-Match", etag(int(b.Version)))
	c.expect(t, 409, "invalid_transition", "POST", M+"/equipment-bookings/"+b.Id+"/cancel", nil, nil, "If-Match", etag(int(b.Version)))
	var req api.EquipmentBooking
	c.expect(t, 201, "", "POST", M+"/equipment-bookings", map[string]any{"purpose": "launch", "vessel_id": p.vessel, "request_only": true}, &req)
	c.expect(t, 200, "", "POST", M+"/equipment-bookings/"+req.Id+"/cancel", map[string]any{"reason": "rinviato"}, &req, "If-Match", etag(int(req.Version)))
	p.other.expect(t, 404, "not_found", "POST", M+"/equipment-bookings/"+req.Id+"/complete", nil, nil, "If-Match", etag(1))
	if req.Status != "cancelled" {
		t.Fatalf("cancelled %+v", req)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
