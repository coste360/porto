//go:build integration

package sensor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// withHook installs testHookBeforeStoreTx for one test.
func withHook(t *testing.T, f func()) {
	t.Helper()
	var once sync.Once
	testHookBeforeStoreTx = func() { once.Do(f) }
	t.Cleanup(func() { testHookBeforeStoreTx = nil })
}

func (e *senv) gatewayDevice(t *testing.T) (*GatewayWithKey, *DeviceWithKey) {
	t.Helper()
	ctx := fixture.Ctx()
	g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "Gw", Kind: store.SensorGatewayLoRaWAN})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
		ExternalID: "DEV1", Code: "D1", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth,
		ResourceID: e.harbour.Berths[0], GatewayID: g.Gateway.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g, d
}

func (e *senv) patch(t *testing.T, deviceID string, in DevicePatch) {
	t.Helper()
	cur, err := e.s.GetDevice(fixture.Ctx(), e.Owner, e.MarinaID, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateDevice(fixture.Ctx(), e.Owner, e.MarinaID, deviceID, cur.Version, in); err != nil {
		t.Fatal(err)
	}
}

// TestIngest_RecheckAfterLock: a device change committed between the device reads of a batch and its transaction
// is seen inside the transaction (INV-102 lock then re-check): unbound or deactivated devices have their items
// rejected, a moved device records its state change on the new resource.
func TestIngest_RecheckAfterLock(t *testing.T) {
	{
		cases := []struct {
			name  string
			patch func(e *senv) DevicePatch
			code  string
		}{
			{"unbind", func(*senv) DevicePatch { return DevicePatch{Binding: &Binding{}} }, ItemDeviceNotBound},
			{"deactivate", func(*senv) DevicePatch { f := false; return DevicePatch{Active: &f} }, ItemDeviceInactive},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				testdb.Run(t, func(t *testing.T, db *store.DB) {
					e := newSenv(t, db)
					g, d := e.gatewayDevice(t)
					withHook(t, func() { e.patch(t, d.Device.ID, tc.patch(e)) })
					now := e.Clock.Now()
					res := e.post(t, g.Key,
						reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-2 * time.Minute)), State: "occupied"},
						reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "occupied"},
					)
					if res.Accepted != 0 || len(res.Rejected) != 2 || res.Rejected[0].Code != tc.code || res.Rejected[1].Index != 1 {
						t.Fatalf("result %+v", res)
					}
					if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, d.Device.ID); n != 0 {
						t.Fatalf("readings stored after %s: %d", tc.name, n)
					}
					if cs := e.changes(t, d.Device.ID); len(cs) != 0 {
						t.Fatalf("changes %+v", cs)
					}
				})
			})
		}
		t.Run("move", func(t *testing.T) {
			testdb.Run(t, func(t *testing.T, db *store.DB) {
				e := newSenv(t, db)
				g, d := e.gatewayDevice(t)
				to := e.harbour.Berths[1]
				withHook(t, func() { e.patch(t, d.Device.ID, DevicePatch{ResourceID: &to}) })
				now := e.Clock.Now()
				res := e.post(t, g.Key,
					// Observed after the move instant (the reset ignores older readings).
					reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(30 * time.Second)), State: "occupied"},
					reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(60 * time.Second)), State: "occupied"},
				)
				if res.Accepted != 2 {
					t.Fatalf("result %+v", res)
				}
				occupied := 0
				for _, c := range e.changes(t, d.Device.ID) {
					if c.ToState == store.SensorOccupied {
						occupied++
						if c.ResourceID != to {
							t.Fatalf("change recorded on the old resource %s, want %s", c.ResourceID, to)
						}
					}
				}
				if occupied != 1 {
					t.Fatalf("occupied changes %d", occupied)
				}
			})
		})
	}
}

// TestConcurrency_IngestVsDeviceUpdate: 100 goroutines ingest through a gateway while the device is moved and then
// unbound. No batch started after the unbind committed stores anything, and every state change carries a resource
// the device had (INV-102).
func TestConcurrency_IngestVsDeviceUpdate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		g, d := e.gatewayDevice(t)
		to := e.harbour.Berths[1]
		now := e.Clock.Now()
		var unbound atomic.Bool
		var lateAccepted, accepted atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, 101)
		for i := range 100 {
			wg.Go(func() {
				late := unbound.Load()
				st := store.SensorOccupied
				if i%7 == 0 {
					st = store.SensorFree
				}
				b := body(t, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Duration(100-i) * time.Second)), State: st})
				res, err := e.s.Ingest(context.Background(), e.signed(g.Key, b))
				if err != nil {
					errs <- err
					return
				}
				accepted.Add(int64(res.Accepted))
				if late {
					lateAccepted.Add(int64(res.Accepted))
				}
			})
			if i == 30 {
				wg.Go(func() {
					ctx := fixture.Ctx()
					cur, err := e.s.GetDevice(ctx, e.Owner, e.MarinaID, d.Device.ID)
					if err != nil {
						errs <- err
						return
					}
					v, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, cur.Version, DevicePatch{ResourceID: &to})
					if err != nil {
						errs <- err
						return
					}
					if _, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, v.Version, DevicePatch{Binding: &Binding{}}); err != nil {
						errs <- err
						return
					}
					unbound.Store(true)
				})
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if lateAccepted.Load() != 0 {
			t.Fatalf("%d readings accepted after the unbind", lateAccepted.Load())
		}
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, d.Device.ID); int64(n) != accepted.Load() {
			t.Fatalf("readings %d accepted %d", n, accepted.Load())
		}
		for _, c := range e.changes(t, d.Device.ID) {
			if c.ResourceID != e.harbour.Berths[0] && c.ResourceID != to {
				t.Fatalf("change on unexpected resource %+v", c)
			}
		}
		checkInvariants(t, db)
	})
}

// TestIngest_AuditOutboxDesign: raw readings are neither audited nor evented; each confirmed state change writes one
// sensor_state_changes row and one sensor.state_changed event in the ingest transaction (INV-101).
func TestIngest_AuditOutboxDesign(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		d := e.device(t, "D1", "DEV1", 0)
		var audits, events int
		count := func() (int, int) {
			a, err := db.NewSelect().Table("audit_log").Where("marina_id = ?", e.MarinaID).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			n, err := db.NewSelect().Table("outbox_events").Where("marina_id = ?", e.MarinaID).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return int(a), int(n)
		}
		audits, events = count()
		now := e.Clock.Now()
		rs := []reading{}
		for i := range 10 {
			rs = append(rs, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Duration(10-i) * time.Minute)), State: "occupied"})
		}
		if res := e.post(t, d.Key, rs...); res.Accepted != 10 {
			t.Fatalf("result %+v", res)
		}
		a, n := count()
		cs := e.changes(t, d.Device.ID)
		if a != audits || len(cs) != 1 || n != events+1 || len(e.Events(t, event.SensorStateChanged)) != 1 {
			t.Fatalf("audit %d->%d, events %d->%d, changes %d", audits, a, events, n, len(cs))
		}
	})
}
