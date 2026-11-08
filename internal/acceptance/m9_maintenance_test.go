//go:build integration

package acceptance

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M9 maintenance cycle (m9-plan §8, §14.4): a plan with lead days, the hourly job generating the task once (with
// its deadline), scheduling refused while a vessel holds the berth (409 with the vessel and its alternatives), the
// vessel moved by the desk, the schedule booking maintenance nights, the outcome replayed offline twice and done
// once, the nights kept after completion (INV-71) and released by the office.

func TestAcceptance_M9_MaintenanceCycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 4)
		o, M := w.owner, w.M
		berth := w.harbour.Berths[0]
		o.expect(t, 201, "POST", M+"/maintenance/plans", map[string]any{
			"name": "Ispezione corpi morti", "intervention": "underwater_inspection", "lead_days": 10, "requires_free": true,
			"targets":     []map[string]any{{"element_type": "berth", "element_id": berth}},
			"periodicity": map[string]any{"kind": "months", "every": 6, "starts_on": "2026-12-10"},
			"checklist":   []map[string]any{{"key": "catena", "label": "Catena"}},
		})
		// Due 2027-06-10, lead 10 days: the job on 2027-06-01 creates it, a second run does nothing.
		w.h.runJobs("maintenance.schedule")
		w.h.runJobs("maintenance.schedule")
		tasks := jsonList(o.expect(t, 200, "GET", M+"/maintenance/tasks", nil), "items")
		if len(tasks) != 1 {
			t.Fatalf("%d tasks", len(tasks))
		}
		task := tasks[0].(map[string]any)
		if str(task, "due_on") != "2027-06-10" || str(task, "source") != "plan" {
			t.Fatalf("task %v", task)
		}
		dls := jsonList(o.expect(t, 200, "GET", M+"/deadlines?status=any", nil), "items")
		if !strings.Contains(stringify(dls), str(task, "id")) {
			t.Fatalf("no deadline for the task: %v", dls)
		}

		// A transit holds the berth on 2027-06-10: the schedule is refused and names it.
		v := w.vessel(t, "Libeccio", 1134, 380)
		cust := w.customer(t, "Rossi")
		tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": v, "customer_id": cust, "arrival": "2027-06-09", "departure": "2027-06-12",
			"placements": []any{berthPlacement(berth, "2027-06-09", "2027-06-11")}, "confirm": map[string]any{},
		})
		r := o.do(t, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/schedule",
			map[string]any{"from_night": "2027-06-10", "to_night": "2027-06-10"}, "If-Match", etagOf(task["version"]))
		if r.status != http.StatusConflict || r.problemType() != "maintenance_conflicts" || !strings.Contains(string(r.body), str(tr, "id")) {
			t.Fatalf("schedule over a transit: %d %s", r.status, r.body)
		}
		// The desk moves the transit to A2; the schedule now books the night.
		alloc := firstAllocation(t, tr)
		o.expect(t, 200, "POST", M+"/occupancies/move", map[string]any{
			"allocation_id": str(alloc, "id"), "version": alloc["version"],
			"target": map[string]any{"resource_type": "berth", "resource_id": w.harbour.Berths[1]}, "from_night": "2027-06-09",
		})
		r = o.do(t, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/schedule",
			map[string]any{"from_night": "2027-06-10", "to_night": "2027-06-10"}, "If-Match", etagOf(task["version"]))
		if r.status != http.StatusOK {
			t.Fatalf("schedule: %d %s", r.status, r.body)
		}
		task = r.json(t)
		if len(jsonList(task, "allocation_ids")) != 1 {
			t.Fatalf("scheduled %v", task)
		}

		// Outcome queued offline, replayed twice: done once; the night stays booked until released.
		w.setClock(t, time.Date(2027, 6, 10, 9, 0, 0, 0, time.UTC))
		yard := w.clients["yard"]
		for i := range 2 {
			done := yard.expect(t, 200, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/complete",
				map[string]any{"outcome": "ok", "client_ref": "op-1", "checklist_results": []map[string]any{{"key": "catena", "done": true}}},
				"Idempotency-Key", "outcome-key-00000"+string(rune('1'+i)), offlineHeader, "2027-06-10T08:30:00Z")
			if str(done, "status") != "done" {
				t.Fatalf("complete %v", done)
			}
			task = done
		}
		o.expect(t, 200, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/release", map[string]any{}, "If-Match", etagOf(task["version"]))
		w.utilityInvariants(t)
	})
}

func stringify(v []any) string {
	var b strings.Builder
	for _, x := range v {
		if m, ok := x.(map[string]any); ok {
			for _, s := range m {
				if ss, ok := s.(string); ok {
					b.WriteString(ss + " ")
				}
			}
		}
	}
	return b.String()
}
