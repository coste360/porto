//go:build integration

package sensor

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/backup"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestBackup_SensorsRoundTrip (m12-plan §7, D8): devices, gateways, sources, settings, states and state changes are
// backed up and restored; raw readings are excluded. On the same PORTO_SECRET_KEY the restored keys verify; under
// another key the devices answer 401 and the key status is unusable (docs/sensor-protocol.md §8).
func TestBackup_SensorsRoundTrip(t *testing.T) {
	testdb.Run(t, func(t *testing.T, dst *store.DB) {
		src := testdb.SQLite(t)
		e := newSenv(t, src)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "DEV1", 0)
		if _, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayOther}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "fake", Name: "Src", Config: map[string]string{"token": "x"}}); err != nil {
			t.Fatal(err)
		}
		now := e.Clock.Now()
		e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "occupied"}, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "occupied"})
		var buf bytes.Buffer
		m, err := backup.Backup(context.Background(), src, &buf, "test", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if m.Rows("sensor_readings") != 0 || m.Rows("sensor_devices") != 1 || m.Rows("sensor_gateways") != 1 || m.Rows("sensor_sources") != 1 ||
			m.Rows("sensor_device_states") != 1 || m.Rows("sensor_state_changes") != 1 {
			t.Fatalf("manifest %+v", m.Tables)
		}
		if _, err := backup.Restore(context.Background(), dst, bytes.NewReader(buf.Bytes()), false, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		scope, err := store.ResolveMarinaScope(context.Background(), dst.DB, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := store.CountSensorReadings(context.Background(), dst.DB, scope, ""); n != 0 {
			t.Fatalf("raw readings restored: %d", n)
		}
		st, err := store.GetSensorDeviceState(context.Background(), dst.DB, scope, d.Device.ID)
		if err != nil || st.Physical != store.SensorOccupied {
			t.Fatalf("state %+v %v", st, err)
		}
		checkInvariants(t, dst)
		// Same key ring: the device keeps working.
		same := NewService(Deps{DB: dst, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Log: e.s.d.Log, RawRetention: 30 * 24 * time.Hour})
		b := body(t, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(time.Second)), State: "occupied"})
		if r, err := same.Ingest(context.Background(), e.signed(d.Key, b)); err != nil || r.Accepted != 1 {
			t.Fatalf("same key ring: %v %+v", err, r)
		}
		// Another key ring: 401 and key status unusable.
		k, _ := crypto.GenerateKey()
		otherKeys, _ := crypto.ParseKeyRing(k)
		other := NewService(Deps{DB: dst, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: otherKeys, Log: e.s.d.Log, RawRetention: 30 * 24 * time.Hour})
		if _, err := other.Ingest(context.Background(), e.signed(d.Key, b)); !errors.Is(err, ErrIngestUnauthorized) {
			t.Fatalf("other key ring: %v", err)
		}
		row, err := store.GetSensorDevice(context.Background(), dst.DB, scope, d.Device.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := other.keyStatus(deviceKeyCols(row), aadDevice+row.ID, now); got != KeyUnusable {
			t.Fatalf("key status %s", got)
		}
	})
}
