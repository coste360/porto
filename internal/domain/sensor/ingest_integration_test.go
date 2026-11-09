//go:build integration

package sensor

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func TestIngest_HappyPath(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		d := e.device(t, "S-A1", "A81758FFFE0312AB", 0)
		now := e.Clock.Now()
		bat, rssi := 87, -97
		res := e.post(t, d.Key,
			reading{DeviceID: "A81758FFFE0312AB", ObservedAt: ts(now.Add(-90 * time.Second)), State: "occupied", DistanceCm: i64(142)},
			reading{DeviceID: "A81758FFFE0312AB", ObservedAt: now.Add(-30*time.Second + 123456789).Format(time.RFC3339Nano), State: "occupied", BatteryPct: &bat, RSSI: &rssi, Firmware: "2.4.1"},
		)
		if res.Accepted != 2 || res.Duplicates != 0 || len(res.Rejected) != 0 || !res.ServerTime.Equal(now) {
			t.Fatalf("result %+v", res)
		}
		st := e.state(t, d.Device.ID)
		if st.Physical != store.SensorOccupied || !st.PhysicalSince.Equal(now.Add(-90*time.Second)) || st.LastContactAt == nil ||
			*st.BatteryPct != 87 || *st.RSSI != -97 || *st.DistanceCm != 142 || st.Firmware != "2.4.1" {
			t.Fatalf("state %+v", st)
		}
		// observed_at is truncated to milliseconds.
		if want := now.Add(-30*time.Second + 123*time.Millisecond); !st.LastObservedAt.Equal(want) {
			t.Fatalf("last observed %v want %v", st.LastObservedAt, want)
		}
		cs := e.changes(t, d.Device.ID)
		if len(cs) != 1 || cs[0].FromState != store.SensorUnknown || cs[0].ToState != store.SensorOccupied || cs[0].Reason != store.SensorReasonReadings {
			t.Fatalf("changes %+v", cs)
		}
		evs := e.Events(t, event.SensorStateChanged)
		if len(evs) != 1 {
			t.Fatalf("events %d", len(evs))
		}
		var data event.SensorStateChangedData
		if err := json.Unmarshal(evs[0].Payload, &data); err != nil {
			t.Fatal(err)
		}
		if len(data.ResourceIDs) != 1 || data.ResourceIDs[0] != e.harbour.Berths[0] || data.From != "unknown" || data.To != "occupied" || data.MapID != e.harbour.MapID {
			t.Fatalf("event data %+v", data)
		}
		for _, leak := range []string{"A81758FFFE0312AB", d.Key, "142", "2.4.1"} {
			if strings.Contains(string(evs[0].Payload), leak) {
				t.Fatalf("event leaks %q", leak)
			}
		}
		select {
		case <-e.wake:
		default:
			t.Fatal("reconcile not woken")
		}
		// Replay: duplicates, no new change, no wake.
		res = e.post(t, d.Key, reading{DeviceID: "A81758FFFE0312AB", ObservedAt: ts(now.Add(-90 * time.Second)), State: "occupied"})
		if res.Accepted != 0 || res.Duplicates != 1 {
			t.Fatalf("replay %+v", res)
		}
		select {
		case <-e.wake:
			t.Fatal("woken without a change")
		default:
		}
		// Raw readings neither audited nor evented besides the state change.
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, ""); n != 2 {
			t.Fatalf("readings %d", n)
		}
		if e.AuditCount(t, "sensor_reading.create") != 0 {
			t.Fatal("readings audited")
		}
		checkInvariants(t, db)
	})
}

func TestIngest_ItemCodesAndLimits(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayLoRaWAN})
		if err != nil {
			t.Fatal(err)
		}
		for i, ext := range []string{"G1", "G2"} {
			if _, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
				ExternalID: ext, Code: ext, Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[i], GatewayID: g.Gateway.ID,
			}); err != nil {
				t.Fatal(err)
			}
		}
		own := e.device(t, "OWN", "OWN1", 2)
		off, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "OFF1", Code: "OFF", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[3], GatewayID: g.Gateway.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		f := false
		if _, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, off.Device.ID, 1, DevicePatch{Active: &f}); err != nil {
			t.Fatal(err)
		}
		now := e.Clock.Now()
		b200 := 200
		raw := `{"readings":[` +
			`{"device_id":"G1","observed_at":"` + ts(now) + `","state":"free","extra":"ignored"},` +
			`{"device_id":"NOPE","observed_at":"` + ts(now) + `","state":"free"},` +
			`{"device_id":"OWN1","observed_at":"` + ts(now) + `","state":"free"},` +
			`{"device_id":"OFF1","observed_at":"` + ts(now) + `","state":"free"},` +
			`{"device_id":"G2","observed_at":"` + ts(now.Add(121*time.Second)) + `","state":"free"},` +
			`{"device_id":"G2","observed_at":"` + ts(now.Add(-73*time.Hour)) + `","state":"free"},` +
			`{"device_id":"G2","observed_at":"` + ts(now) + `","state":"busy"},` +
			`{"device_id":"G2","observed_at":"` + ts(now) + `","state":"free","battery_pct":` + strconv.Itoa(b200) + `},` +
			`{"device_id":"G2","observed_at":"2027-06-12 08:00:00","state":"free"},` +
			`{"device_id":"G2","observed_at":"` + ts(now) + `","state":"free","rssi":5},` +
			`{"device_id":"G2","observed_at":"` + ts(now) + `","state":"free","distance_cm":"far"},` +
			`7,` +
			`{"device_id":"G2","observed_at":"` + ts(now.Add(120*time.Second)) + `","state":"unknown"},` +
			`{"device_id":"G1","observed_at":"` + ts(now) + `","state":"free"}` +
			`]}`
		req := e.signed(g.Key, []byte(raw))
		res, err := e.s.Ingest(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		got := map[int]string{}
		for _, r := range res.Rejected {
			got[r.Index] = r.Code
		}
		want := map[int]string{
			1: ItemUnknownDevice, 2: ItemDeviceNotBound, 3: ItemDeviceInactive, 4: ItemObservedInFuture, 5: ItemObservedTooOld,
			6: ItemInvalidState, 7: ItemInvalidValue, 8: ItemInvalidValue, 9: ItemInvalidValue, 10: ItemInvalidValue, 11: ItemInvalidValue,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("rejected %v", got)
		}
		if res.Accepted != 2 || res.Duplicates != 1 {
			t.Fatalf("accepted %d duplicates %d", res.Accepted, res.Duplicates)
		}
		// Device key: only its own id.
		r := e.post(t, own.Key, reading{DeviceID: "G1", ObservedAt: ts(now.Add(time.Second)), State: "free"})
		if len(r.Rejected) != 1 || r.Rejected[0].Code != ItemDeviceMismatch || r.Rejected[0].DeviceID != "G1" {
			t.Fatalf("mismatch %+v", r)
		}
		// Batch limits.
		for name, b := range map[string]string{
			"not json": `{"readings":`, "no readings": `{}`, "empty": `{"readings":[]}`, "object": `{"readings":{}}`, "trailing": `{"readings":[]} {}`,
		} {
			if _, err := e.s.Ingest(context.Background(), e.signed(own.Key, []byte(b))); !errors.Is(err, ErrInvalidPayload) {
				t.Errorf("%s: %v", name, err)
			}
		}
		many := make([]reading, 501)
		for i := range many {
			many[i] = reading{DeviceID: "OWN1", ObservedAt: ts(now.Add(-time.Duration(i) * time.Second)), State: "free"}
		}
		if _, err := e.s.Ingest(context.Background(), e.signed(own.Key, body(t, many...))); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("501: %v", err)
		}
		r = e.post(t, own.Key, many[:500]...)
		if r.Accepted != 500 {
			t.Fatalf("500: %+v", r.Accepted)
		}
		checkInvariants(t, db)
	})
}

func TestIngest_AuthKeysAndRateLimit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "DEV1", 0)
		now := e.Clock.Now()
		b := body(t, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "free"})
		unauthorized := func(name string, req IngestRequest) {
			t.Helper()
			if _, err := e.s.Ingest(context.Background(), req); !errors.Is(err, ErrIngestUnauthorized) {
				t.Errorf("%s: %v", name, err)
			}
		}
		unauthorized("no headers", IngestRequest{Method: "POST", Path: ingestPath, Body: b})
		bad := e.signed(d.Key, b)
		bad.PortoSignature = "t=" + strconv.FormatInt(now.Unix(), 10) + ",v1=" + strings.Repeat("ab", 32)
		unauthorized("bad signature", bad)
		other, _ := ingestauth.FormatKey(ingestauth.DevicePrefix, [10]byte{1}, [32]byte{1})
		unauthorized("unknown key", e.signed(other, b))
		unauthorized("device key as bearer", IngestRequest{Method: "POST", Path: ingestPath, Body: b, Authorization: "Bearer " + d.Key, Secure: true})
		tampered := e.signed(d.Key, b)
		tampered.Body = body(t, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "occupied"})
		unauthorized("tampered body", tampered)
		// Timestamp out of range: a valid signature gets the server time.
		old := e.signed(d.Key, b)
		e.Clock.Advance(301 * time.Second)
		_, err := e.s.Ingest(context.Background(), old)
		var te *TimestampError
		if !errors.As(err, &te) || !te.ServerTime.Equal(e.Clock.Now()) {
			t.Fatalf("timestamp: %v", err)
		}
		e.Clock.Set(now)
		// Rotation with grace: both keys verify until the grace ends.
		rot, err := e.s.RotateDeviceKey(ctx, e.Owner, e.MarinaID, d.Device.ID, 1, 1)
		if err != nil || rot.Device.KeyStatus != KeyRotating {
			t.Fatalf("rotate: %v", err)
		}
		if _, err := e.s.Ingest(context.Background(), e.signed(d.Key, b)); err != nil {
			t.Fatalf("old key in grace: %v", err)
		}
		e.post(t, rot.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(time.Second)), State: "free"})
		e.Clock.Advance(time.Hour)
		b2 := body(t, reading{DeviceID: "DEV1", ObservedAt: ts(e.Clock.Now()), State: "free"})
		unauthorized("old key after grace", e.signed(d.Key, b2))
		if _, err := e.s.Ingest(context.Background(), e.signed(rot.Key, b2)); err != nil {
			t.Fatalf("new key: %v", err)
		}
		// Revoke: 401 for every key.
		if _, err := e.s.RevokeDeviceKey(ctx, e.Owner, e.MarinaID, d.Device.ID, 2); err != nil {
			t.Fatal(err)
		}
		unauthorized("revoked", e.signed(rot.Key, b2))
		// Gateway bearer: HTTPS only, inactive gateway refused.
		g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayLoRaWAN})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "G1", Code: "G1", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[1], GatewayID: g.Gateway.ID,
		}); err != nil {
			t.Fatal(err)
		}
		gb := body(t, reading{DeviceID: "G1", ObservedAt: ts(e.Clock.Now()), State: "free"})
		bearer := IngestRequest{Method: "POST", Path: ingestPath, Body: gb, Authorization: "Bearer " + g.Key, Secure: true}
		if r, err := e.s.Ingest(context.Background(), bearer); err != nil || r.Accepted != 1 {
			t.Fatalf("bearer: %v %+v", err, r)
		}
		insecure := bearer
		insecure.Secure = false
		unauthorized("bearer over http", insecure)
		gs, _ := store.GetSensorGateway(context.Background(), db.DB, e.scope, g.Gateway.ID)
		if gs.LastContactAt == nil {
			t.Fatal("gateway contact not recorded")
		}
		f := false
		if _, err := e.s.UpdateGateway(ctx, e.Owner, e.MarinaID, g.Gateway.ID, 1, GatewayPatch{Active: &f}); err != nil {
			t.Fatal(err)
		}
		unauthorized("inactive gateway", bearer)
		// Per-key limit counted before the signature check.
		e.limiter.budget = 2
		d3 := e.device(t, "S-A3", "DEV3", 2)
		badSig := e.signed(d3.Key, b)
		badSig.PortoSignature = "t=" + strconv.FormatInt(e.Clock.Now().Unix(), 10) + ",v1=" + strings.Repeat("cd", 32)
		unauthorized("bad 1", badSig)
		unauthorized("bad 2", badSig)
		_, err = e.s.Ingest(context.Background(), e.signed(d3.Key, body(t, reading{DeviceID: "DEV3", ObservedAt: ts(e.Clock.Now()), State: "free"})))
		var rl *RateLimitError
		if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
			t.Fatalf("rate limit: %v", err)
		}
		checkInvariants(t, db)
	})
}

// TestIngest_CrossMarina: a key of marina A posting marina B's external id gets unknown_device and nothing is
// written in B (INV-102).
func TestIngest_CrossMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayLoRaWAN})
		if err != nil {
			t.Fatal(err)
		}
		_, bMarina, bOwner := e.OtherOrganization(t)
		bh := insertHarbourFor(t, e, bMarina)
		bdev, err := e.s.CreateDevice(ctx, bOwner, bMarina, DeviceInput{
			ExternalID: "SHARED-ID", Code: "B1", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: bh.Berths[0],
		})
		if err != nil {
			t.Fatal(err)
		}
		now := e.Clock.Now()
		r := e.post(t, g.Key, reading{DeviceID: "SHARED-ID", ObservedAt: ts(now), State: "occupied"}, reading{DeviceID: "SHARED-ID", ObservedAt: ts(now.Add(time.Second)), State: "occupied"})
		if r.Accepted != 0 || len(r.Rejected) != 2 || r.Rejected[0].Code != ItemUnknownDevice {
			t.Fatalf("result %+v", r)
		}
		// The same with a device key of A (mismatch), and B's own key works only in B.
		ad := e.device(t, "A1", "A-ONLY", 0)
		r = e.post(t, ad.Key, reading{DeviceID: "SHARED-ID", ObservedAt: ts(now), State: "occupied"})
		if r.Rejected[0].Code != ItemDeviceMismatch {
			t.Fatalf("device key %+v", r)
		}
		bScope, _ := store.ResolveMarinaScope(context.Background(), db.DB, bMarina)
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, bScope, ""); n != 0 {
			t.Fatalf("readings written in B: %d", n)
		}
		r = e.post(t, bdev.Key, reading{DeviceID: "SHARED-ID", ObservedAt: ts(now), State: "occupied"})
		if r.Accepted != 1 {
			t.Fatalf("B's own key %+v", r)
		}
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, ""); n != 0 {
			t.Fatalf("readings written in A: %d", n)
		}
		checkInvariants(t, db)
	})
}

func insertHarbourFor(t *testing.T, e *senv, marinaID string) harbourIDs {
	t.Helper()
	h := insertHarbour(t, e.DB, marinaID, store.UTC(e.Clock.Now()))
	return h
}

func TestIngest_OutOfOrderAndAtomicity(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		d := e.device(t, "S-A1", "DEV1", 0)
		now := e.Clock.Now()
		e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "free"}, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "free"})
		// Out of order: stored, accepted, no state effect.
		r := e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-10 * time.Minute)), State: "occupied"},
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-9 * time.Minute)), State: "occupied"})
		if r.Accepted != 2 {
			t.Fatalf("%+v", r)
		}
		st := e.state(t, d.Device.ID)
		if st.Physical != store.SensorFree || st.Candidate != "" || !st.LastObservedAt.Equal(now) {
			t.Fatalf("state %+v", st)
		}
		// A failing outbox write rolls back readings, state and state change together.
		ctx := context.Background()
		if db.IsPostgres() {
			mustExec(t, db, `CREATE FUNCTION porto_fail_outbox() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'boom'; END $$ LANGUAGE plpgsql`)
			mustExec(t, db, `CREATE TRIGGER porto_fail_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION porto_fail_outbox()`)
		} else {
			mustExec(t, db, `CREATE TRIGGER porto_fail_outbox BEFORE INSERT ON outbox_events BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		}
		before, _ := store.CountSensorReadings(ctx, db.DB, e.scope, "")
		_, err := e.s.Ingest(ctx, e.signed(d.Key, body(t,
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(time.Second)), State: "occupied"},
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(2 * time.Second)), State: "occupied"})))
		if err == nil {
			t.Fatal("expected a failure")
		}
		after, _ := store.CountSensorReadings(ctx, db.DB, e.scope, "")
		if after != before || e.state(t, d.Device.ID).Physical != store.SensorFree || len(e.changes(t, d.Device.ID)) != 1 {
			t.Fatal("partial write")
		}
		mustExec(t, db, `DROP TRIGGER porto_fail_outbox`+map[bool]string{true: " ON outbox_events", false: ""}[db.IsPostgres()])
		checkInvariants(t, db)
	})
}

func mustExec(t *testing.T, db *store.DB, q string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestJobs_SilenceRetentionPoll(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "DEV1", 0)
		now := e.Clock.Now()
		e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "occupied"}, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "occupied"})
		<-e.wake
		// Silence: nothing before 30 minutes, unknown at last + 30 after.
		e.Clock.Advance(29 * time.Minute)
		if err := e.s.RunSilence(ctx); err != nil {
			t.Fatal(err)
		}
		if e.state(t, d.Device.ID).Physical != store.SensorOccupied {
			t.Fatal("silenced too early")
		}
		e.Clock.Advance(5 * time.Minute)
		if err := e.s.RunSilence(ctx); err != nil {
			t.Fatal(err)
		}
		st := e.state(t, d.Device.ID)
		cs := e.changes(t, d.Device.ID)
		if st.Physical != store.SensorUnknown || len(cs) != 2 || cs[1].Reason != store.SensorReasonSilence || !cs[1].At.Equal(now.Add(30*time.Minute)) {
			t.Fatalf("silence %+v %+v", st, cs)
		}
		if len(e.Events(t, event.SensorStateChanged)) != 2 {
			t.Fatal("silence event")
		}
		if err := e.s.RunSilence(ctx); err != nil || len(e.changes(t, d.Device.ID)) != 2 {
			t.Fatal("silence twice")
		}
		if v, _ := e.s.GetDevice(ctx, e.Owner, e.MarinaID, d.Device.ID); v.Health != HealthOffline {
			t.Fatalf("health %s", v.Health)
		}
		// Retention: old raw readings go, state changes stay.
		old := make([]store.SensorReading, 0, 12000)
		base := now.Add(-40 * 24 * time.Hour)
		for i := range 12000 {
			old = append(old, store.SensorReading{
				DeviceID: d.Device.ID, ObservedAt: base.Add(time.Duration(i) * time.Second), MarinaID: e.MarinaID, ReceivedAt: base,
				State: store.SensorFree, Origin: store.SensorOriginDevice,
			})
		}
		if err := store.BulkInsertSensorReadings(context.Background(), db.DB, old); err != nil {
			t.Fatal(err)
		}
		if err := e.s.RunRetention(ctx); err != nil {
			t.Fatal(err)
		}
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, ""); n != 2 {
			t.Fatalf("readings after retention %d", n)
		}
		if len(e.changes(t, d.Device.ID)) != 2 {
			t.Fatal("state changes purged")
		}
		// Poll: readings of bound devices through the same path, cursor stored with them.
		src, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "fake", Name: "Vendor", PollSeconds: 30})
		if err != nil {
			t.Fatal(err)
		}
		pd, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "POLL1", Code: "P1", Kind: store.SensorKindCamera, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[1], SourceID: src.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		pnow := e.Clock.Now()
		e.fake.Push(e.MarinaID,
			ext.SensorReading{DeviceExternalID: "POLL1", ObservedAt: pnow.Add(-2 * time.Minute), State: "occupied"},
			ext.SensorReading{DeviceExternalID: "POLL1", ObservedAt: pnow.Add(-time.Minute), State: "occupied"},
			ext.SensorReading{DeviceExternalID: "DEV1", ObservedAt: pnow, State: "free"},
		)
		if err := e.s.RunPoll(ctx); err != nil {
			t.Fatal(err)
		}
		s1, _ := store.GetSensorSource(context.Background(), db.DB, e.scope, src.ID)
		if s1.Cursor != "3" || s1.LastPolledAt == nil || s1.LastErrorCode != "" {
			t.Fatalf("source %+v", s1)
		}
		if e.state(t, pd.Device.ID).Physical != store.SensorOccupied {
			t.Fatal("polled readings not folded")
		}
		if e.fake.Polls() == 0 || e.fake.PollsWithoutHTTP() != 0 {
			t.Fatalf("source polled without the egress client (%d of %d)", e.fake.PollsWithoutHTTP(), e.fake.Polls())
		}
		if !strings.Contains(e.logs.String(), "rejected=1") || strings.Contains(e.logs.String(), "DEV1") {
			t.Fatalf("job log %q", e.logs.String())
		}
		// Not due yet: no poll.
		polls := e.fake.Polls()
		if err := e.s.RunPoll(ctx); err != nil || e.fake.Polls() != polls {
			t.Fatal("polled before poll_seconds")
		}
		// A crash between the poll and the commit re-polls the same cursor: idempotent.
		if _, err := db.NewUpdate().Table("sensor_sources").Set("cursor = ''").Where("id = ?", src.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		r, err := e.s.PollSourceNow(ctx, e.Owner, e.MarinaID, src.ID)
		if err != nil || r.Accepted != 0 || r.Duplicates != 2 || r.Rejected != 1 {
			t.Fatalf("re-poll %v %+v", err, r)
		}
		// A driver error keeps the cursor and stores only the class.
		e.fake.Fail(errors.New("vendor said: token tok-secret-xyz invalid"))
		r, err = e.s.PollSourceNow(ctx, e.Owner, e.MarinaID, src.ID)
		if err != nil || r.ErrorCode != store.SensorSourceErrUnavailable {
			t.Fatalf("fail %v %+v", err, r)
		}
		s2, _ := store.GetSensorSource(context.Background(), db.DB, e.scope, src.ID)
		if s2.Cursor != "3" || s2.LastErrorCode != store.SensorSourceErrUnavailable || strings.Contains(e.logs.String(), "tok-secret-xyz") {
			t.Fatalf("after failure %+v", s2)
		}
		checkInvariants(t, db)
	})
}

// TestSensorKeys_NotStoredOrLogged: no column, audit row, event or log line contains a key or its secret part
// (INV-105).
func TestSensorKeys_NotStoredOrLogged(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "DEV1", 0)
		rot, err := e.s.RotateDeviceKey(ctx, e.Owner, e.MarinaID, d.Device.ID, 1, 24)
		if err != nil {
			t.Fatal(err)
		}
		g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayOther})
		if err != nil {
			t.Fatal(err)
		}
		now := e.Clock.Now()
		e.post(t, rot.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "free"})
		_, _ = e.s.Ingest(context.Background(), IngestRequest{Method: "POST", Path: ingestPath, Body: []byte(`{}`), Authorization: "Bearer " + g.Key})
		var secrets []string
		for _, k := range []string{d.Key, rot.Key, g.Key} {
			id, _ := ingestauth.KeyIDOf(k)
			secrets = append(secrets, k, k[len(id)+1:])
		}
		dump := e.logs.String()
		for _, table := range []string{"sensor_devices", "sensor_gateways", "audit_log", "outbox_events", "sensor_readings", "sensor_device_states"} {
			var rows []map[string]any
			if err := db.NewSelect().Table(table).Scan(context.Background(), &rows); err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			for _, r := range rows {
				for _, v := range r {
					switch x := v.(type) {
					case string:
						dump += "\n" + x
					case []byte:
						dump += "\n" + string(x)
					}
				}
			}
		}
		for _, s := range secrets {
			if strings.Contains(dump, s) {
				t.Fatalf("secret %q found in the database or logs", s[:12])
			}
		}
	})
}

// TestIngest_KeyScopes (INV-102): a device key writes only its own external id, a gateway key only the devices bound
// to it, a source only its own devices; the marina always comes from the key.
func TestIngest_KeyScopes(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		g1, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw1", Kind: store.SensorGatewayLoRaWAN})
		if err != nil {
			t.Fatal(err)
		}
		g2, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW2", Name: "Gw2", Kind: store.SensorGatewayLoRaWAN})
		if err != nil {
			t.Fatal(err)
		}
		src, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "fake", Name: "Src"})
		if err != nil {
			t.Fatal(err)
		}
		mk := func(ext string, berth int, gw, source string) *DeviceWithKey {
			d, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
				ExternalID: ext, Code: ext, Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[berth],
				GatewayID: gw, SourceID: source,
			})
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
		own := mk("OWN", 0, "", "")
		mk("G1D", 1, g1.Gateway.ID, "")
		mk("SRC", 2, "", src.ID)
		now := e.Clock.Now()
		rs := func(ids ...string) []reading {
			out := make([]reading, 0, len(ids))
			for _, id := range ids {
				out = append(out, reading{DeviceID: id, ObservedAt: ts(now), State: "free"})
			}
			return out
		}
		codes := func(r *IngestResult) []string {
			out := make([]string, len(r.Rejected))
			for i, x := range r.Rejected {
				out[i] = x.Code
			}
			return out
		}
		if r := e.post(t, own.Key, rs("OWN", "G1D", "SRC")...); r.Accepted != 1 || !reflect.DeepEqual(codes(r), []string{ItemDeviceMismatch, ItemDeviceMismatch}) {
			t.Fatalf("device key %+v", r)
		}
		if r := e.post(t, g1.Key, rs("G1D", "OWN", "SRC", "NONE")...); r.Accepted != 1 ||
			!reflect.DeepEqual(codes(r), []string{ItemDeviceNotBound, ItemDeviceNotBound, ItemUnknownDevice}) {
			t.Fatalf("gateway 1 %+v", r)
		}
		if r := e.post(t, g2.Key, rs("G1D")...); r.Accepted != 0 || !reflect.DeepEqual(codes(r), []string{ItemDeviceNotBound}) {
			t.Fatalf("gateway 2 %+v", r)
		}
		pr, err := e.s.IngestSource(context.Background(), e.MarinaID, src.ID, []ext.SensorReading{
			{DeviceExternalID: "SRC", ObservedAt: now.Add(time.Second), State: "free"},
			{DeviceExternalID: "OWN", ObservedAt: now.Add(time.Second), State: "free"},
			{DeviceExternalID: "G1D", ObservedAt: now.Add(time.Second), State: "free"},
		}, "1")
		if err != nil || pr.Accepted != 1 || pr.Rejected != 2 {
			t.Fatalf("source %v %+v", err, pr)
		}
		checkInvariants(t, db)
	})
}
