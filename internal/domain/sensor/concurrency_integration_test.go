//go:build integration

package sensor

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// tickClock advances one microsecond on every call: instants read under a lock give the lock order.
type tickClock struct {
	base time.Time
	n    atomic.Int64
}

func (c *tickClock) Now() time.Time { return c.base.Add(time.Duration(c.n.Add(1)) * time.Microsecond) }

// TestConcurrency_IngestIdempotency: 100 goroutines post the same 3-reading batch for one device, half signed with
// the device key and half through its gateway: the readings are stored once, accepted sums to 3, exactly one
// state change and one event (INV-101).
func TestConcurrency_IngestIdempotency(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
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
		now := e.Clock.Now()
		b := body(t,
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-3 * time.Minute)), State: "occupied"},
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-2 * time.Minute)), State: "occupied"},
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "occupied"},
		)
		reqs := []IngestRequest{e.signed(d.Key, b), e.signed(g.Key, b)}
		var accepted, duplicates atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Go(func() {
				res, err := e.s.Ingest(context.Background(), reqs[i%2])
				if err != nil {
					errs <- err
					return
				}
				accepted.Add(int64(res.Accepted))
				duplicates.Add(int64(res.Duplicates))
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if accepted.Load() != 3 || duplicates.Load() != 297 {
			t.Fatalf("accepted %d duplicates %d", accepted.Load(), duplicates.Load())
		}
		if n, _ := store.CountSensorReadings(context.Background(), db.DB, e.scope, d.Device.ID); n != 3 {
			t.Fatalf("readings %d", n)
		}
		if cs := e.changes(t, d.Device.ID); len(cs) != 1 {
			t.Fatalf("changes %d", len(cs))
		}
		if evs := e.Events(t, event.SensorStateChanged); len(evs) != 1 {
			t.Fatalf("events %d", len(evs))
		}
		checkInvariants(t, db)
	})
}

// TestConcurrency_IngestSameDevice: 100 goroutines post distinct readings of one device in random order. The
// stored changes equal the fold of the readings in commit order (received_at), out-of-order arrivals ignored, and
// Q40/Q41 hold.
func TestConcurrency_IngestSameDevice(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		d := e.device(t, "D1", "DEV1", 0)
		tick := &tickClock{base: e.Clock.Now()}
		svc := NewService(Deps{DB: db, Clock: tick, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Log: e.s.d.Log, RawRetention: 30 * 24 * time.Hour})
		rng := rand.New(rand.NewPCG(12, 34))
		states := []string{"occupied", "free"}
		cur := "occupied"
		rs := make([]reading, 100)
		for i := range rs {
			if rng.IntN(6) == 0 {
				cur = states[rng.IntN(2)]
			}
			rs[i] = reading{DeviceID: "DEV1", ObservedAt: ts(tick.base.Add(-time.Duration(100-i) * 20 * time.Second)), State: cur}
		}
		rng.Shuffle(len(rs), func(i, j int) { rs[i], rs[j] = rs[j], rs[i] })
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Go(func() {
				// Each request signs at the base second (tick advances microseconds only).
				req := e.signed(d.Key, body(t, rs[i]))
				if res, err := svc.Ingest(context.Background(), req); err != nil {
					errs <- err
				} else if res.Accepted != 1 {
					errs <- context.Canceled
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		var stored []store.SensorReading
		if err := db.NewSelect().Model(&stored).Where("device_id = ?", d.Device.ID).OrderExpr("received_at, observed_at").Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(stored) != 100 {
			t.Fatalf("stored %d", len(stored))
		}
		set := store.DefaultSensorSettings(e.MarinaID)
		p := hysteresis.Params{Readings: set.HysteresisReadings, Window: time.Duration(set.HysteresisMinutes) * time.Minute}
		h := hysteresis.Initial()
		var want []hysteresis.Change
		for _, r := range stored {
			var ch *hysteresis.Change
			h, ch, _ = hysteresis.Step(h, hysteresis.Reading{ObservedAt: r.ObservedAt.UTC(), State: r.State}, p)
			if ch != nil {
				want = append(want, *ch)
			}
		}
		got := e.changes(t, d.Device.ID)
		if len(got) != len(want) {
			t.Fatalf("changes %d, fold in commit order %d", len(got), len(want))
		}
		for i := range got {
			if got[i].ToState != want[i].To || !got[i].At.Equal(want[i].At) {
				t.Fatalf("change %d: %+v vs %+v", i, got[i], want[i])
			}
		}
		if st := e.state(t, d.Device.ID); st.Physical != h.Confirmed {
			t.Fatalf("physical %s, fold %s", st.Physical, h.Confirmed)
		}
		checkInvariants(t, db)
	})
}
