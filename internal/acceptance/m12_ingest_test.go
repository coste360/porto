//go:build integration

package acceptance

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/sensorsim"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// sensorReading is one item of an ingestion batch.
func sensorReading(ext string, at time.Time, state string) sensorsim.Reading {
	return sensorsim.Reading{DeviceID: ext, ObservedAt: at.UTC().Format(time.RFC3339Nano), State: state}
}

func (w *deskWorld) ingest(t *testing.T, base string, key string, rs ...sensorsim.Reading) (int, *sensorsim.Result) {
	t.Helper()
	c := &sensorsim.Client{BaseURL: base, HTTP: &http.Client{Timeout: 10 * time.Second}}
	status, res, _ := c.Post(context.Background(), key, sensorsim.Body(rs...), w.clock.Now())
	return status, res
}

func checkSensorQ(t *testing.T, db *store.DB, marinas ...string) {
	t.Helper()
	for _, m := range marinas {
		vs, err := store.CheckSensorInvariants(context.Background(), db.DB, m)
		if err != nil || len(vs) > 0 {
			t.Fatalf("sensor invariants %s: %+v %v", m, vs, err)
		}
	}
}

// TestAcceptance_M12_Ingest (spec 24.3, m12-plan §4, INV-101, INV-105): devices and a gateway are configured through
// the API (keys shown once), signed readings confirm a physical change that reaches the SSE stream without personal
// data, a replay is a duplicate, a rotation keeps the old key valid for its grace only, a gateway may use its bearer
// key over HTTPS only, and the per-key limit answers 429.
func TestAcceptance_M12_Ingest(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 3)
		o, M := w.owner, w.M
		base := w.h.srv.URL
		dev := o.expect(t, 201, "POST", M+"/sensors/devices", map[string]any{
			"external_id": "A81758FFFE0312AB", "code": "S-A1", "kind": "ultrasonic", "resource_type": "berth", "resource_id": w.harbour.Berths[0],
		})
		key := str(dev, "key")
		d := dev["device"].(map[string]any)
		id := str(d, "id")
		if !strings.HasPrefix(key, "po_sns_") || strings.Contains(string(o.do(t, "GET", M+"/sensors/devices/"+id, nil).body), key) {
			t.Fatal("key not shown once")
		}
		gw := o.expect(t, 201, "POST", M+"/sensors/gateways", map[string]any{"code": "GW1", "name": "LoRa", "kind": "lorawan"})
		gwKey := str(gw, "key")
		o.expect(t, 201, "POST", M+"/sensors/devices", map[string]any{
			"external_id": "GWDEV1", "code": "S-A2", "kind": "radar", "resource_type": "berth", "resource_id": w.harbour.Berths[1],
			"gateway_id": str(gw["gateway"].(map[string]any), "id"),
		})

		sse := openStream(t, w.h, w.clients["dock"], w.marina)
		now := w.clock.Now()
		pair := []sensorsim.Reading{sensorReading("A81758FFFE0312AB", now.Add(-time.Minute), "occupied"), sensorReading("A81758FFFE0312AB", now, "occupied")}
		status, res := w.ingest(t, base, key, pair...)
		if status != 200 || res.Accepted != 2 || res.Duplicates != 0 {
			t.Fatalf("ingest %d %+v", status, res)
		}
		w.h.runJobs("outbox.dispatch")
		data := waitData(t, sse, "sensor.state_changed")
		if !strings.Contains(data, w.harbour.Berths[0]) || strings.Contains(data, "A81758FFFE0312AB") || strings.Contains(data, key) {
			t.Fatalf("SSE data %q", data)
		}
		got := o.expect(t, 200, "GET", M+"/sensors/devices/"+id, nil)
		if str(got, "physical") != "occupied" {
			t.Fatalf("physical %v", got["physical"])
		}
		// Replay of the same body: duplicates only.
		if status, res = w.ingest(t, base, key, pair...); status != 200 || res.Duplicates != 2 || res.Accepted != 0 {
			t.Fatalf("replay %d %+v", status, res)
		}

		// Rotation with a one-hour grace.
		rot := o.expect(t, 200, "POST", M+"/sensors/devices/"+id+"/rotate-key", map[string]any{"grace_hours": 1}, "If-Match", `"1"`)
		newKey := str(rot, "key")
		if status, _ = w.ingest(t, base, key, sensorReading("A81758FFFE0312AB", now.Add(time.Second), "occupied")); status != 200 {
			t.Fatalf("old key in grace: %d", status)
		}
		w.clock.Advance(61 * time.Minute)
		now = w.clock.Now()
		if status, _ = w.ingest(t, base, key, sensorReading("A81758FFFE0312AB", now, "occupied")); status != 401 {
			t.Fatalf("old key after grace: %d", status)
		}
		if status, _ = w.ingest(t, base, newKey, sensorReading("A81758FFFE0312AB", now, "occupied")); status != 200 {
			t.Fatalf("new key: %d", status)
		}

		// Gateway bearer: refused over plain HTTP, accepted over HTTPS.
		body := sensorsim.Body(sensorReading("GWDEV1", now, "free"))
		post := func(c *http.Client, url string) int {
			req, _ := http.NewRequest("POST", url+sensorsim.Path, strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+gwKey)
			r, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			return r.StatusCode
		}
		if s := post(http.DefaultClient, base); s != 401 {
			t.Fatalf("bearer over http: %d", s)
		}
		tlsSrv := httptest.NewTLSServer(w.h.app.Router)
		defer tlsSrv.Close()
		tc := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // test server certificate
		if s := post(tc, tlsSrv.URL); s != 200 {
			t.Fatalf("bearer over https: %d", s)
		}

		// Per-key limit: 30 requests per minute per device key.
		limited := false
		for i := range 35 {
			status, _ := w.ingest(t, base, newKey, sensorReading("A81758FFFE0312AB", now.Add(time.Duration(i+1)*time.Millisecond), "occupied"))
			if status == 429 {
				limited = true
				break
			}
		}
		if !limited {
			t.Fatal("no 429")
		}
		checkSensorQ(t, db, w.marina)
	})
}

// TestAcceptance_M12_IngestIsolation (INV-102, INV-73): keys of marina A never write in marina B, whatever external
// ids they send, and every device, gateway and source route of A is 404 for B.
func TestAcceptance_M12_IngestIsolation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		base := w.h.srv.URL
		devA := o.expect(t, 201, "POST", M+"/sensors/devices", map[string]any{
			"external_id": "DEV-A", "code": "SA", "kind": "radar", "resource_type": "berth", "resource_id": w.harbour.Berths[0],
		})
		gwA := o.expect(t, 201, "POST", M+"/sensors/gateways", map[string]any{"code": "GW", "name": "A", "kind": "other"})
		srcA := o.expect(t, 201, "POST", M+"/sensors/sources", map[string]any{"code": "fake", "name": "A"})

		// Marina B with the same external id on its own berth.
		ownerB, mB := w.secondMarina(t)
		MB := A + "/marinas/" + mB
		hB := insertHarbourB(t, db, mB, w.clock.Now())
		devB := ownerB.expect(t, 201, "POST", MB+"/sensors/devices", map[string]any{
			"external_id": "DEV-A", "code": "SB", "kind": "radar", "resource_type": "berth", "resource_id": hB,
		})
		now := w.clock.Now()
		// A's gateway posting B's (and its own unbound) external id: unknown in its marina or not bound, never B.
		status, res := w.ingest(t, base, str(gwA, "key"), sensorReading("DEV-A", now, "occupied"), sensorReading("DEV-X", now, "occupied"))
		if status != 200 || res.Accepted != 0 || len(res.Rejected) != 2 || res.Rejected[0].Code != "device_not_bound" || res.Rejected[1].Code != "unknown_device" {
			t.Fatalf("gateway %d %+v", status, res)
		}
		// Each device key writes its own marina only.
		if status, res = w.ingest(t, base, str(devA, "key"), sensorReading("DEV-A", now, "occupied")); status != 200 || res.Accepted != 1 {
			t.Fatalf("A %d %+v", status, res)
		}
		var nB int
		if err := db.NewSelect().Table("sensor_readings").ColumnExpr("COUNT(*)").Where("marina_id = ?", mB).Scan(context.Background(), &nB); err != nil || nB != 0 {
			t.Fatalf("readings in B: %d %v", nB, err)
		}
		if status, res = w.ingest(t, base, str(devB, "key"), sensorReading("DEV-A", now, "occupied")); status != 200 || res.Accepted != 1 {
			t.Fatalf("B %d %+v", status, res)
		}
		// Routes of A's ids from B: 404.
		idA := str(devA["device"].(map[string]any), "id")
		gidA := str(gwA["gateway"].(map[string]any), "id")
		sidA := str(srcA, "id")
		for _, rt := range []struct{ method, path string }{
			{"GET", "/sensors/devices/" + idA},
			{"PATCH", "/sensors/devices/" + idA},
			{"POST", "/sensors/devices/" + idA + "/rotate-key"},
			{"POST", "/sensors/devices/" + idA + "/revoke-key"},
			{"PATCH", "/sensors/gateways/" + gidA},
			{"POST", "/sensors/gateways/" + gidA + "/rotate-key"},
			{"POST", "/sensors/gateways/" + gidA + "/revoke-key"},
			{"PATCH", "/sensors/sources/" + sidA},
			{"POST", "/sensors/sources/" + sidA + "/poll"},
		} {
			for _, base := range []string{MB, M} {
				var body any
				if rt.method != "GET" {
					body = map[string]any{}
				}
				r := ownerB.do(t, rt.method, base+rt.path, body, "If-Match", `"1"`)
				if r.status != 404 || r.problemType() != "not_found" {
					t.Errorf("%s %s%s: %d %s", rt.method, base, rt.path, r.status, r.body)
				}
			}
		}
		// B's lists never show A's rows.
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(ownerB.do(t, "GET", MB+"/sensors/devices", nil).body, &list); err != nil || len(list.Items) != 1 || str(list.Items[0], "code") != "SB" {
			t.Fatalf("B devices %+v", list)
		}
		checkSensorQ(t, db, w.marina, mB)
	})
}

// secondMarina creates an unrelated organization with its own marina and returns its logged-in owner.
func (w *deskWorld) secondMarina(t *testing.T) (*client, string) {
	t.Helper()
	b, err := w.h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner-b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-sensori-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := w.h.cookieClient()
	c.login(t, "owner-b@porto.test")
	return c, b.MarinaID
}

func insertHarbourB(t *testing.T, db *store.DB, marinaID string, now time.Time) string {
	t.Helper()
	return contracttest.InsertHarbour(t, db, marinaID, store.UTC(now), 1).Berths[0]
}

// waitData waits for an SSE data line containing sub and returns it.
func waitData(t *testing.T, s *stream, sub string) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		for _, l := range s.lines {
			if strings.HasPrefix(l, "data: ") && strings.Contains(l, sub) {
				s.mu.Unlock()
				return l
			}
		}
		s.mu.Unlock()
		select {
		case <-s.cond:
		case <-deadline:
			t.Fatalf("SSE: no data line with %q", sub)
		}
	}
}

// TestAcceptance_M12_KeysNotInIdempotencyStore: creating and rotating a device or gateway key through the router
// with an Idempotency-Key never stores the plaintext key (INV-105): the operations are x-porto-idempotency:
// domain, so no response body is kept in idempotency_keys, and a replay never returns the key again (409).
func TestAcceptance_M12_KeysNotInIdempotencyStore(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 3)
		o, M := w.owner, w.M
		devBody := map[string]any{
			"external_id": "IDEMDEV1", "code": "S-I1", "kind": "ultrasonic", "resource_type": "berth", "resource_id": w.harbour.Berths[0],
		}
		dev := o.expect(t, 201, "POST", M+"/sensors/devices", devBody, "Idempotency-Key", "sensor-device-0001")
		gw := o.expect(t, 201, "POST", M+"/sensors/gateways", map[string]any{"code": "GWI", "name": "Gw", "kind": "other"}, "Idempotency-Key", "sensor-gateway-0001")
		id := str(dev["device"].(map[string]any), "id")
		gid := str(gw["gateway"].(map[string]any), "id")
		rot := o.expect(t, 200, "POST", M+"/sensors/devices/"+id+"/rotate-key", map[string]any{"grace_hours": 0}, "If-Match", `"1"`, "Idempotency-Key", "sensor-rotate-0001")
		grot := o.expect(t, 200, "POST", M+"/sensors/gateways/"+gid+"/rotate-key", map[string]any{"grace_hours": 0}, "If-Match", `"1"`, "Idempotency-Key", "sensor-grotate-001")
		keys := []string{str(dev, "key"), str(gw, "key"), str(rot, "key"), str(grot, "key")}
		for _, k := range keys {
			if len(k) < 24 {
				t.Fatalf("key %q not returned", k)
			}
		}
		// A replay with the same Idempotency-Key is a new create refused by the unique code: the key is not returned.
		if r := o.do(t, "POST", M+"/sensors/devices", devBody, "Idempotency-Key", "sensor-device-0001"); r.status != 409 || strings.Contains(string(r.body), keys[0]) {
			t.Fatalf("replay %d %s", r.status, r.body)
		}
		var rows []store.IdempotencyKey
		if err := db.NewSelect().Model(&rows).Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			blob := row.Key + row.Scope + row.RequestHash + string(row.ResponseBody)
			for _, k := range keys {
				if strings.Contains(blob, k) || strings.Contains(blob, k[len(k)-16:]) {
					t.Fatalf("idempotency row %s stores a sensor key", row.ID)
				}
			}
		}
	})
}
