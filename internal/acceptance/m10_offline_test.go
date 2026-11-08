//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M10 offline kinds (m10-plan §7, D25, INV-71, INV-83): on the yard phone a timer start and stop, a task progress
// and a task photo queued offline and replayed twice through the full HTTP stack with Porto-Offline-Queued-At: one
// effect each and the same answer; a later progress replay never lowers the progress; nothing queued creates or
// changes an allocation or a lifting booking; the phone's work list shows the assignment and no running timer.

func TestAcceptance_M10_Offline(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		yard := w.clients["yard"]
		w.setClock(t, time.Date(2027, 6, 1, 9, 0, 0, 0, time.UTC))

		// The yard worker's employee, a qualification and an order with one task assigned to him today.
		var yardUser string
		for _, m := range jsonList(o.expect(t, 200, "GET", M+"/members", nil), "items") {
			if mm := m.(map[string]any); str(mm, "email") == "yard@porto.test" {
				yardUser = str(mm, "user_id")
			}
		}
		classes := jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items")
		item := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "LAB-HAND", "name": map[string]string{"it": "Ora operaio"}, "item_type": "labour", "unit": "hour",
			"revenue_class_id": str(classes[0].(map[string]any), "id"),
		})
		q := o.expect(t, 201, "POST", M+"/yard/qualifications", map[string]any{
			"code": "hand", "name": map[string]string{"it": "Operaio"}, "labour_item_id": str(item, "id"), "default_cost_cents": 2200,
		})
		emp := o.expect(t, 201, "POST", M+"/yard/employees", map[string]any{
			"code": "Y1", "first_name": "Marco", "last_name": "Esposito", "user_id": yardUser, "qualification_ids": []string{str(q, "id")},
			"hourly_cost_cents": 2200,
		})
		order := o.expect(t, 201, "POST", M+"/yard/work-orders", map[string]any{
			"customer_id": w.customer(t, "Bianchi"), "vessel_id": w.vessel(t, "Grecale", 1200, 400), "planned_delivery_on": "2027-06-10",
		})
		task := o.expect(t, 201, "POST", M+"/yard/work-orders/"+str(order, "id")+"/tasks", map[string]any{
			"title": "Carteggiatura", "qualification_id": str(q, "id"), "estimated_hours_milli": 4000, "checklist": []string{"Carteggiare"},
		})
		o.expect(t, 200, "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []map[string]any{{
			"task_id": str(task, "id"), "employee_id": str(emp, "id"), "work_date": "2027-06-01", "hours_milli": 4000,
			"task_version": task["version"],
		}}})

		count := func(table string) int {
			n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", w.marina).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		allocations, bookings := count("allocations"), count("equipment_bookings")
		replay := func(path string, body map[string]any, key, queued string) []response {
			raw, _ := json.Marshal(body)
			return hammer(t, 2, func() (response, error) {
				return yard.replayUntilDone(http.MethodPost, path, raw, "application/json", "Idempotency-Key", key, offlineHeader, queued)
			})
		}

		// Timer start and stop, each replayed twice: one entry of 1.5 h.
		starts := replay(M+"/timesheets/timer", map[string]any{
			"client_ref": "op-start-0001", "task_id": str(task, "id"),
			"started_at": "2027-06-01T06:00:00Z",
		}, "key-start-00000001", "2027-06-01T06:00:00Z")
		assertSameBodies(t, starts, http.StatusCreated)
		entry := starts[0].json(t)
		stops := replay(M+"/timesheets/"+str(entry, "id")+"/stop", map[string]any{
			"client_ref": "op-stop-00001",
			"ended_at":   "2027-06-01T07:30:00Z",
		}, "key-stop-000000001", "2027-06-01T07:30:00Z")
		assertSameBodies(t, stops, http.StatusOK)
		if h, _ := num(stops[0].json(t), "hours_milli"); h != 1500 {
			t.Fatalf("hours %s", stops[0].body)
		}
		if n := len(jsonList(o.expect(t, 200, "GET", M+"/timesheets?work_order_id="+str(order, "id"), nil), "items")); n != 1 {
			t.Fatalf("%d time entries", n)
		}

		// Progress replayed twice; an older queued progress never lowers it.
		progs := replay(M+"/yard/tasks/"+str(task, "id")+"/progress", map[string]any{
			"client_ref": "op-prog-00001", "progress_bp": 5000,
			"checklist": []map[string]any{{"key": jsonKey(t, task), "done": true}}, "recorded_at": "2027-06-01T07:31:00Z",
		},
			"key-prog-000000001", "2027-06-01T07:31:00Z")
		assertSameBodies(t, progs, http.StatusOK)
		low := replay(M+"/yard/tasks/"+str(task, "id")+"/progress", map[string]any{
			"client_ref": "op-prog-00002", "progress_bp": 2000,
			"recorded_at": "2027-06-01T07:20:00Z",
		}, "key-prog-000000002", "2027-06-01T07:20:00Z")
		if p, _ := num(low[0].json(t), "progress_bp"); p != 5000 {
			t.Fatalf("progress lowered: %s", low[0].body)
		}

		// Photo replayed twice: one file.
		photos := hammer(t, 2, func() (response, error) {
			return yard.replayUntilDone(http.MethodPost, M+"/yard/tasks/"+str(task, "id")+"/photos", testJPEG(t), "image/jpeg",
				"Idempotency-Key", "key-photo-00000001", offlineHeader, "2027-06-01T07:32:00Z")
		})
		assertSameBodies(t, photos, http.StatusOK)

		// The phone work list: today's assignment, no running timer.
		my := yard.expect(t, 200, "GET", M+"/yard/my-work", nil)
		if str(my, "employee_id") != str(emp, "id") || len(jsonList(my, "assignments")) != 1 || my["running_timer"] != nil {
			t.Fatalf("my work %v", my)
		}
		// INV-71: nothing queued offline touched an allocation or a lifting booking.
		if a, b := count("allocations"), count("equipment_bookings"); a != allocations || b != bookings {
			t.Fatalf("allocations %d → %d, bookings %d → %d", allocations, a, bookings, b)
		}
		// Booking a lift moves a machine: never accepted offline.
		r := o.do(t, "POST", M+"/equipment-bookings", map[string]any{"purpose": "other", "request_only": true}, offlineHeader, "2027-06-01T07:40:00Z")
		if r.status != http.StatusUnprocessableEntity || r.problemType() != "offline_not_allowed" {
			t.Fatalf("offline booking: %d %s", r.status, r.body)
		}
	})
}

// jsonKey returns the key of the first checklist item of a task.
func jsonKey(t *testing.T, task map[string]any) string {
	t.Helper()
	items := jsonList(task, "checklist")
	if len(items) == 0 {
		t.Fatalf("task without checklist %v", task)
	}
	return str(items[0].(map[string]any), "key")
}
