//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M10 lifting (m10-plan §11, §16.4, INV-17): a travel lift with two operators, a booking, the same slots refused
// with 409 equipment_slot_taken naming the holder, the same operator on the crane refused with operator_busy, a
// reschedule that frees the old slots, a machine held out of service by maintenance, the day calendar read by the
// yard worker on the phone, and Q31 holding at the end. Everything goes through the API.

// plnrLiftWorld is the desk world with a lift-operator qualification, two operators, a travel lift and a crane.
type plnrLiftWorld struct {
	*deskWorld
	lift, crane string
	ops         []string
	vessel      string
}

func newPlnrLiftWorld(t *testing.T, db *store.DB) *plnrLiftWorld {
	t.Helper()
	w := &plnrLiftWorld{deskWorld: newDeskWorld(t, db, 2)}
	o, M := w.owner, w.M
	classes := jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items")
	item := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "LAB-LIFT", "name": map[string]string{"it": "Ora gruista"}, "item_type": "labour", "unit": "hour",
		"revenue_class_id": str(classes[0].(map[string]any), "id"),
	})
	q := o.expect(t, 201, "POST", M+"/yard/qualifications", map[string]any{
		"code": "lift", "name": map[string]string{"it": "Gruista"}, "labour_item_id": str(item, "id"), "default_cost_cents": 2400,
		"lift_operator": true,
	})
	for _, code := range []string{"G1", "G2"} {
		e := o.expect(t, 201, "POST", M+"/yard/employees", map[string]any{
			"code": code, "first_name": "Gru", "last_name": code, "qualification_ids": []string{str(q, "id")},
		})
		w.ops = append(w.ops, str(e, "id"))
	}
	for i, kind := range []string{"travel_lift", "crane"} {
		eq := o.expect(t, 201, "POST", M+"/equipment", map[string]any{"code": kind, "name": kind, "kind": kind, "capacity_kg": 40000})
		o.expect(t, 200, "PUT", M+"/equipment/"+str(eq, "id")+"/operators", map[string]any{"employee_ids": w.ops})
		if i == 0 {
			w.lift = str(eq, "id")
		} else {
			w.crane = str(eq, "id")
		}
	}
	w.vessel = w.vessel0(t)
	return w
}

func (w *plnrLiftWorld) vessel0(t *testing.T) string {
	t.Helper()
	return w.deskWorld.vessel(t, "Maestrale", 1200, 400)
}

func (w *plnrLiftWorld) liftInvariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckLiftingInvariants(context.Background(), w.h.app.DB.DB, w.marina)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func TestAcceptance_M10_Lifting(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newPlnrLiftWorld(t, db)
		o, M := w.owner, w.M
		// 2027-06-01 07:00 UTC = 09:00 in Rome; book tomorrow 08:00-09:00 (slots 32..36).
		b := o.expect(t, 201, "POST", M+"/equipment-bookings", map[string]any{
			"equipment_id": w.lift, "date": "2027-06-02", "from_slot": 32, "to_slot": 36, "purpose": "haul_out",
			"vessel_id": w.vessel, "operator_employee_id": w.ops[0],
		}, "Idempotency-Key", "book-lift-00000001")
		if str(b, "status") != "booked" {
			t.Fatalf("booking %v", b)
		}
		r := o.do(t, "POST", M+"/equipment-bookings", map[string]any{
			"equipment_id": w.lift, "date": "2027-06-02", "from_slot": 35, "to_slot": 37, "purpose": "launch", "vessel_id": w.vessel,
		})
		if r.status != http.StatusConflict || r.problemType() != "equipment_slot_taken" || !strings.Contains(string(r.body), str(b, "id")) {
			t.Fatalf("double booking: %d %s", r.status, r.body)
		}
		r = o.do(t, "POST", M+"/equipment-bookings", map[string]any{
			"equipment_id": w.crane, "date": "2027-06-02", "from_slot": 33, "to_slot": 34, "purpose": "other", "operator_employee_id": w.ops[0],
		})
		if r.status != http.StatusConflict || r.problemType() != "operator_busy" {
			t.Fatalf("operator twice: %d %s", r.status, r.body)
		}
		// Reschedule to 10:00; the old slots are free again.
		b = o.expect(t, 200, "PATCH", M+"/equipment-bookings/"+str(b, "id"), map[string]any{"from_slot": 40, "to_slot": 44}, "If-Match", etagOf(b["version"]))
		o.expect(t, 201, "POST", M+"/equipment-bookings", map[string]any{
			"equipment_id": w.lift, "date": "2027-06-02", "from_slot": 32, "to_slot": 36, "purpose": "launch", "vessel_id": w.vessel,
			"operator_employee_id": w.ops[0],
		})
		// The crane goes out of service (maintenance started): no booking on it.
		task := o.expect(t, 201, "POST", M+"/maintenance/tasks", map[string]any{
			"title": "Revisione gru", "element": map[string]any{"element_type": "equipment", "element_id": w.crane}, "out_of_service": true,
		})
		o.expect(t, 200, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/start", nil, "If-Match", etagOf(task["version"]))
		r = o.do(t, "POST", M+"/equipment-bookings", map[string]any{
			"equipment_id": w.crane, "date": "2027-06-02", "from_slot": 50, "to_slot": 52, "purpose": "other",
		})
		if r.status != http.StatusConflict || r.problemType() != "equipment_out_of_service" {
			t.Fatalf("out of service: %d %s", r.status, r.body)
		}
		// The yard worker reads the day calendar on the phone (read only).
		w.setClock(t, time.Date(2027, 6, 2, 6, 0, 0, 0, time.UTC))
		yard := w.clients["yard"]
		day := yard.expect(t, 200, "GET", M+"/equipment-bookings", nil)
		if str(day, "date") != "2027-06-02" || len(jsonList(day, "bookings")) != 2 || len(jsonList(day, "equipment")) != 2 {
			t.Fatalf("day %v", day)
		}
		for _, e := range jsonList(day, "equipment") {
			em := e.(map[string]any)
			if (str(em, "id") == w.crane) != (em["out_of_service"] == true) {
				t.Fatalf("out of service flag %v", em)
			}
		}
		if r := yard.do(t, "POST", M+"/equipment-bookings/"+str(b, "id")+"/complete", nil, "If-Match", etagOf(b["version"])); r.status != http.StatusForbidden {
			t.Fatalf("worker completes: %d", r.status)
		}
		o.expect(t, 200, "POST", M+"/equipment-bookings/"+str(b, "id")+"/complete", nil, "If-Match", etagOf(b["version"]))
		w.liftInvariants(t)
	})
}
