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

// M9 offline kinds (m9-plan §10, INV-71, INV-83): a reading with its photo, a field report and a task outcome
// queued on the phone and replayed twice through the full HTTP stack with Porto-Offline-Queued-At: one effect
// each, the same answer, read_at taken from the queued instant, and no allocation created or changed.

func TestAcceptance_M9_Offline(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w, meter := newUtilityWorld(t, db)
		o, M, dock := w.owner, w.M, w.clients["dock"]
		w.setClock(t, time.Date(2027, 6, 5, 10, 0, 0, 0, time.UTC))
		yard := w.member(t, "yard")
		queued := "2027-06-05T08:15:00Z"
		allocations := func() int {
			n, err := db.NewSelect().TableExpr("allocations").Where("marina_id = ?", w.marina).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		before := allocations()

		replay := func(c *client, path string, body map[string]any, key string) []response {
			raw, _ := json.Marshal(body)
			return hammer(t, 2, func() (response, error) {
				return c.replayUntilDone(http.MethodPost, path, raw, "application/json", "Idempotency-Key", key, offlineHeader, queued)
			})
		}

		// Reading: the same op twice is one reading, read at the queued instant.
		rs := replay(dock, M+"/meter-readings", map[string]any{"meter_id": meter, "value": "12,5", "client_ref": "op-r1"}, "key-r1-000000001")
		assertSameBodies(t, rs, http.StatusCreated)
		reading := rs[0].json(t)
		if str(reading, "read_at") != queued {
			t.Fatalf("read_at %v", reading["read_at"])
		}
		// The same client_ref under another key (the idempotency row was lost) still answers the stored reading.
		again := dock.expect(t, 201, "POST", M+"/meter-readings", map[string]any{"meter_id": meter, "value": "12,5", "client_ref": "op-r1"},
			"Idempotency-Key", "key-r1-000000002", offlineHeader, queued)
		if str(again, "id") != str(reading, "id") {
			t.Fatalf("second key gave %v", again)
		}
		page := o.expect(t, 200, "GET", M+"/meter-readings?meter_id="+meter, nil)
		if n := len(jsonList(page, "items")); n != 1 {
			t.Fatalf("%d readings", n)
		}

		// Field report and its photo.
		reportBody := map[string]any{"client_ref": "op-m1", "title": "Bitta allentata", "map_id": w.harbour.MapID, "position_x_cm": 1200, "position_y_cm": 900}
		reps := replay(yard, M+"/maintenance/reports", reportBody, "key-m1-000000001")
		assertSameBodies(t, reps, http.StatusCreated)
		task := reps[0].json(t)
		png := testJPEG(t)
		photos := hammer(t, 2, func() (response, error) {
			return yard.replayUntilDone(http.MethodPost, M+"/maintenance/tasks/"+str(task, "id")+"/files", png, "image/jpeg",
				"Idempotency-Key", "key-m1-photo-0001", offlineHeader, queued)
		})
		assertSameBodies(t, photos, http.StatusOK)

		// Outcome: replayed twice, done once; another completion is a conflict.
		outs := replay(yard, M+"/maintenance/tasks/"+str(task, "id")+"/complete", map[string]any{"outcome": "fixed", "client_ref": "op-o1"}, "key-o1-000000001")
		assertSameBodies(t, outs, http.StatusOK)
		if str(outs[0].json(t), "status") != "done" {
			t.Fatalf("outcome %s", outs[0].body)
		}
		r := yard.do(t, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/complete", map[string]any{"outcome": "ok", "client_ref": "op-o2"},
			"Idempotency-Key", "key-o2-000000001", offlineHeader, queued)
		if r.status != http.StatusConflict || r.problemType() != "task_already_done" {
			t.Fatalf("second completion: %d %s", r.status, r.body)
		}

		// INV-71: nothing queued offline touched an allocation.
		if after := allocations(); after != before {
			t.Fatalf("allocations %d → %d", before, after)
		}
		// Scheduling books nights: never accepted offline.
		r = o.do(t, "POST", M+"/maintenance/tasks/"+str(task, "id")+"/schedule", map[string]any{"from_night": "2027-06-06", "to_night": "2027-06-06"},
			"If-Match", `"1"`, offlineHeader, queued)
		if r.status != http.StatusUnprocessableEntity || r.problemType() != "offline_not_allowed" {
			t.Fatalf("offline schedule: %d %s", r.status, r.body)
		}
		w.utilityInvariants(t)
	})
}
