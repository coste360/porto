//go:build integration

package acceptance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/sensorsim"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// openAlertOf returns the open alert of a kind on a device code (nil when none).
func openAlertOf(t *testing.T, c *client, base, kind, code string) map[string]any {
	t.Helper()
	for _, a := range jsonList(c.expect(t, 200, "GET", base+"/sensors/alerts?kind="+kind, nil), "items") {
		am := a.(map[string]any)
		if str(am, "device_code") == code && str(am, "status") == "open" {
			return am
		}
	}
	return nil
}

func checkAlertQ(t *testing.T, db *store.DB, marina string) {
	t.Helper()
	vs, err := store.CheckSensorAlertInvariants(context.Background(), db.DB, marina)
	if err != nil || len(vs) > 0 {
		t.Fatalf("Q42/Q43: %+v %v", vs, err)
	}
}

// TestAcceptance_M12_Reconcile (spec 24.4, m12-plan §8, INV-100, INV-104) through the API with a fake clock: an
// unexpected occupation opens an alert seen on SSE (sensor.anomaly); a transit arrival proposal confirmed by the
// dock records the arrival through the contract; an annual vessel away 6 h gets the automatic presence out, after 3
// days a release proposal confirmed with consent creates the release; the early return on the released night is
// revoked and the return is recorded automatically; a low battery and a silent sensor become maintenance tasks.
func TestAcceptance_M12_Reconcile(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 4)
		o, M := w.owner, w.M
		dock := w.clients["dock"]
		base := w.h.srv.URL
		b := w.harbour.Berths
		cur := o.expect(t, 200, "GET", M+"/sensors/settings", nil)
		set := map[string]any{"presence_effect_enabled": true}
		for _, k := range []string{
			"hysteresis_readings", "hysteresis_minutes", "offline_after_minutes", "silent_alert_minutes", "battery_low_pct",
			"unexpected_after_minutes", "departure_cutoff_minutes", "presence_out_after_hours", "release_proposal_after_days",
			"release_proposal_nights",
		} {
			set[k] = cur[k]
		}
		o.expect(t, 200, "PUT", M+"/sensors/settings", set, "If-Match", `"0"`)
		keys := map[string]string{}
		for i, code := range []string{"S-A1", "S-A2", "S-A3", "S-A4"} {
			d := o.expect(t, 201, "POST", M+"/sensors/devices", map[string]any{
				"external_id": "EXT-" + code, "code": code, "kind": "ultrasonic", "resource_type": "berth", "resource_id": b[i],
			})
			keys[code] = str(d, "key")
		}
		read := func(code string, at time.Time, state string, battery *int) {
			t.Helper()
			r := sensorReading("EXT-"+code, at, state)
			r.BatteryPct = battery
			r2 := sensorReading("EXT-"+code, at.Add(time.Minute), state)
			r2.BatteryPct = battery
			if status, res := w.ingest(t, base, keys[code], []sensorsim.Reading{r, r2}...); status != 200 || res.Accepted == 0 && res.Duplicates == 0 {
				t.Fatalf("ingest %s %d %+v", code, status, res)
			}
		}
		// A transit arriving today on A2, an annual contract on A3.
		v1, rossi := w.vessel(t, "Libeccio", 1100, 380), w.customer(t, "Rossi")
		tr := dock.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": v1, "customer_id": rossi, "arrival": "2027-06-01", "departure": "2027-06-04",
			"placements": []any{berthPlacement(b[1], "2027-06-01", "2027-06-03")},
		}, "Idempotency-Key", "acc-m12-transit-0001")
		tr = dock.expect(t, 200, "POST", M+"/transits/"+str(tr, "id")+"/confirm", map[string]any{"reference": "POS 1"}, "If-Match", etagOf(tr["version"]))
		v2 := w.vessel(t, "Grecale", 1000, 350)
		ct := w.clients["office"].expect(t, 201, "POST", M+"/contracts", map[string]any{
			"term": "annual", "customer_id": rossi, "vessel_id": v2, "starts_on": "2027-01-01",
			"placements": []any{berthPlacement(b[2], "2027-01-01", "2027-12-31")},
		})
		w.clients["office"].expect(t, 200, "POST", M+"/contracts/"+str(ct, "id")+"/activate", nil, "If-Match", etagOf(ct["version"]))

		sse := openStream(t, w.h, dock, w.marina)
		now := w.clock.Now()
		read("S-A1", now.Add(-40*time.Minute), "occupied", nil)
		read("S-A2", now.Add(-30*time.Minute), "occupied", nil)
		read("S-A3", now.Add(-7*time.Hour), "free", nil)
		read("S-A3", now.Add(-2*time.Minute), "free", nil)
		w.h.runJobs("sensors.reconcile", "outbox.dispatch")
		data := waitData(t, sse, "sensor.anomaly")
		if strings.Contains(data, "Libeccio") || strings.Contains(data, "EXT-") {
			t.Fatalf("SSE data %q", data)
		}
		if openAlertOf(t, o, M, "unexpected_occupied", "S-A1") == nil {
			t.Fatal("unexpected occupied alert")
		}
		// Transit arrival confirmed from the dock.
		ta := openAlertOf(t, dock, M, "transit_arrival", "S-A2")
		if ta == nil {
			t.Fatal("transit arrival alert")
		}
		dock.expect(t, 200, "POST", M+"/sensors/alerts/"+str(ta, "id")+"/actions", map[string]any{"action": "confirm_arrival"}, "If-Match", etagOf(ta["version"]))
		scope, _ := store.ResolveMarinaScope(context.Background(), db.DB, w.marina)
		if got, err := store.GetContract(context.Background(), db.DB, scope, str(tr, "id")); err != nil || got.Status != "arrived" {
			t.Fatalf("transit %v %+v", err, got)
		}
		// The annual vessel has been away 7 hours: automatic presence out.
		vp, err := store.GetVesselPresence(context.Background(), db.DB, scope, v2)
		if err != nil || vp.State != "out" {
			t.Fatalf("presence %v %+v", err, vp)
		}
		checkAlertQ(t, db, w.marina)

		// Three days later: release proposal, confirmed with consent.
		w.setClock(t, now.Add(3*24*time.Hour))
		now = w.clock.Now()
		read("S-A3", now.Add(-5*time.Minute), "free", nil)
		w.h.runJobs("sensors.reconcile")
		rp := openAlertOf(t, o, M, "release_proposal", "S-A3")
		if rp == nil {
			t.Fatal("release proposal")
		}
		refs := rp["refs"].(map[string]any)
		o.expect(t, 422, "POST", M+"/sensors/alerts/"+str(rp, "id")+"/actions", map[string]any{"action": "confirm_release"}, "If-Match", etagOf(rp["version"]))
		w.clients["viewer"].expect(t, 403, "POST", M+"/sensors/alerts/"+str(rp, "id")+"/actions", map[string]any{"action": "confirm_release", "consent_confirmed": true}, "If-Match", etagOf(rp["version"]))
		done := o.expect(t, 200, "POST", M+"/sensors/alerts/"+str(rp, "id")+"/actions", map[string]any{
			"action": "confirm_release", "consent_confirmed": true, "from": refs["proposed_from"], "to": refs["proposed_from"],
		}, "If-Match", etagOf(rp["version"]))
		rel, err := store.GetRelease(context.Background(), db.DB, scope, str(done, "outcome_ref"))
		if err != nil || rel.Status != "active" || rel.FromNight.String() != refs["proposed_from"] {
			t.Fatalf("release %v %+v", err, rel)
		}
		// Next day the berth is occupied on the released night: early return → revoke; then the return is automatic.
		w.setClock(t, now.Add(24*time.Hour))
		now = w.clock.Now()
		read("S-A3", now.Add(-30*time.Minute), "occupied", nil)
		w.h.runJobs("sensors.reconcile")
		er := openAlertOf(t, o, M, "early_return", "S-A3")
		if er == nil || er["refs"].(map[string]any)["resold"] != false {
			t.Fatalf("early return %v", er)
		}
		o.expect(t, 200, "POST", M+"/sensors/alerts/"+str(er, "id")+"/actions", map[string]any{"action": "revoke_release"}, "If-Match", etagOf(er["version"]))
		w.h.runJobs("sensors.reconcile")
		if vp, _ = store.GetVesselPresence(context.Background(), db.DB, scope, v2); vp.State != "in_port" {
			t.Fatalf("automatic return %+v", vp)
		}
		// Low battery and a silent sensor: maintenance tasks on confirm.
		low := 9
		read("S-A1", now.Add(-3*time.Minute), "occupied", &low)
		w.h.runJobs("sensors.reconcile")
		for _, x := range []struct{ kind, code string }{{"battery_low", "S-A1"}, {"device_silent", "S-A4"}} {
			a := openAlertOf(t, o, M, x.kind, x.code)
			if a == nil {
				t.Fatalf("%s alert", x.kind)
			}
			res := o.expect(t, 200, "POST", M+"/sensors/alerts/"+str(a, "id")+"/actions", map[string]any{"action": "create_task"}, "If-Match", etagOf(a["version"]))
			task := o.expect(t, 200, "GET", M+"/maintenance/tasks/"+str(res, "outcome_ref"), nil)
			if str(task, "source") != "sensor" {
				t.Fatalf("task %v", task)
			}
		}
		checkAlertQ(t, db, w.marina)
		checkSensorQ(t, db, w.marina)
	})
}
