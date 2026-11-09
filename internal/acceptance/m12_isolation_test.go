//go:build integration

package acceptance

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M12_WatchIsolation (INV-73, m12-plan §17.4): two marinas with sensors and alerts; the watch routes
// of marina B (map, alerts, history, device history, sensors report) never show marina A's devices, alerts or
// resources, and A's alert and device ids are 404 under B's marina and under A's marina for B's owner.
func TestAcceptance_M12_WatchIsolation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		ownerB, mB := w.secondMarina(t)
		MB := A + "/marinas/" + mB
		berthB := insertHarbourB(t, db, mB, w.clock.Now())
		devA := o.expect(t, 201, "POST", M+"/sensors/devices", map[string]any{
			"external_id": "EXT-A", "code": "SA", "kind": "radar", "resource_type": "berth", "resource_id": w.harbour.Berths[0],
		})
		devB := ownerB.expect(t, 201, "POST", MB+"/sensors/devices", map[string]any{
			"external_id": "EXT-B", "code": "SB", "kind": "radar", "resource_type": "berth", "resource_id": berthB,
		})
		now := w.clock.Now()
		for _, x := range []struct{ ext, key string }{{"EXT-A", str(devA, "key")}, {"EXT-B", str(devB, "key")}} {
			if status, _ := w.ingest(t, w.h.srv.URL, x.key, sensorReading(x.ext, now.Add(-time.Hour), "occupied"), sensorReading(x.ext, now.Add(-59*time.Minute), "occupied")); status != 200 {
				t.Fatalf("ingest %s: %d", x.ext, status)
			}
		}
		w.h.runJobs("sensors.reconcile")
		alertsA := jsonList(o.expect(t, 200, "GET", M+"/sensors/alerts", nil), "items")
		alertsB := jsonList(ownerB.expect(t, 200, "GET", MB+"/sensors/alerts", nil), "items")
		if len(alertsA) != 1 || len(alertsB) != 1 {
			t.Fatalf("alerts A %d B %d", len(alertsA), len(alertsB))
		}
		idA := str(alertsA[0].(map[string]any), "id")
		devIDA := str(devA["device"].(map[string]any), "id")
		for _, path := range []string{
			MB + "/sensors/map", MB + "/sensors/alerts", MB + "/sensors/history?resource_type=berth&resource_id=" + berthB,
			MB + "/sensors/devices/" + str(devB["device"].(map[string]any), "id") + "/history", MB + "/reports/sensors",
		} {
			body := string(ownerB.do(t, "GET", path, nil).body)
			if strings.Contains(body, idA) || strings.Contains(body, devIDA) || strings.Contains(body, w.harbour.Berths[0]) || strings.Contains(body, "\"SA\"") {
				t.Fatalf("%s shows marina A: %s", path, body)
			}
		}
		for _, path := range []string{
			MB + "/sensors/alerts/" + idA, M + "/sensors/alerts/" + idA, MB + "/sensors/devices/" + devIDA + "/history",
			MB + "/sensors/history?resource_type=berth&resource_id=" + w.harbour.Berths[0], M + "/sensors/map", M + "/reports/sensors",
		} {
			if r := ownerB.do(t, "GET", path, nil); r.status != 404 {
				t.Fatalf("%s: %d", path, r.status)
			}
		}
		if r := ownerB.do(t, "POST", MB+"/sensors/alerts/"+idA+"/actions", map[string]any{"action": "dismiss"}, "If-Match", `"1"`); r.status != 404 {
			t.Fatalf("act across marinas: %d", r.status)
		}
		for _, m := range []string{w.marina, mB} {
			checkAlertQ(t, db, m)
		}
	})
}
