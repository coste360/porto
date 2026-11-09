//go:build integration

package watch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestConcurrency_ReconcileAlertsOnce: 100 concurrent reconcile runs on a scenario where every rule fires open one
// alert per (device, kind) and record one sensor presence per vessel (INV-104, Q42).
func TestConcurrency_ReconcileAlertsOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		e.enableEffect(t)
		e.Transit(t, e.Berths[1], "2027-06-02", "2027-06-04", true)
		annual := e.mooring(t, e.Berths[2], contract.TermAnnual)
		free := e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil)
		arr := e.device(t, store.SensorResourceBerth, e.Berths[1], nil, nil)
		away := e.device(t, store.SensorResourceBerth, e.Berths[2], nil, nil)
		silent := e.device(t, store.SensorResourceBerth, e.Berths[3], nil, nil)
		e.set(t, free, store.SensorOccupied, e.now().Add(-time.Hour))
		e.battery(t, free, 5)
		e.set(t, arr, store.SensorOccupied, e.now().Add(-30*time.Minute))
		e.set(t, away, store.SensorFree, e.now().Add(-7*time.Hour))
		_ = silent
		sys, err := store.SystemMarinaScope(context.Background(), db.DB, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var failed atomic.Int64
		for range 100 {
			wg.Go(func() {
				if _, err := e.w.ReconcileMarina(context.Background(), sys); err != nil {
					failed.Add(1)
				}
			})
		}
		wg.Wait()
		if failed.Load() > 0 {
			t.Fatalf("%d runs failed", failed.Load())
		}
		for _, x := range []struct{ dev, kind string }{
			{free.ID, store.SensorAlertUnexpectedOccupied},
			{free.ID, store.SensorAlertBatteryLow},
			{arr.ID, store.SensorAlertTransitArrival},
			{silent.ID, store.SensorAlertDeviceSilent},
		} {
			e.openAlert(t, x.dev, x.kind)
		}
		n, err := db.NewSelect().Table("presences").Where("source = 'sensor'").Where("vessel_id = ?", annual.Row.VesselID).Count(context.Background())
		if err != nil || n != 1 {
			t.Fatalf("sensor presences %d %v", n, err)
		}
		if got := e.countAlerts(t, "1 = 1"); got != 4 {
			t.Fatalf("alerts %d", got)
		}
		e.invariants(t)
	})
}

// TestConcurrency_AlertActionOnce: 100 confirms of one transit_arrival alert record one presence and one transit
// transition; the others are version_conflict or alert_stale.
func TestConcurrency_AlertActionOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		tr := e.Transit(t, e.Berths[1], "2027-06-02", "2027-06-04", true)
		d := e.device(t, store.SensorResourceBerth, e.Berths[1], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorOccupied, e.now().Add(-30*time.Minute))
		e.reconcile(t)
		a := e.openAlert(t, d.ID, store.SensorAlertTransitArrival)
		var wg sync.WaitGroup
		var ok, conflict, other atomic.Int64
		for range 100 {
			wg.Go(func() {
				_, err := e.w.Act(fixture.Ctx(), e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmArrival})
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrVersionMismatch), errors.Is(err, ErrAlertStale), errors.Is(err, ErrActionNotAllowed):
					conflict.Add(1)
				default:
					other.Add(1)
					t.Logf("unexpected: %v", err)
				}
			})
		}
		wg.Wait()
		if ok.Load() != 1 || other.Load() != 0 {
			t.Fatalf("ok %d conflict %d other %d", ok.Load(), conflict.Load(), other.Load())
		}
		n, _ := db.NewSelect().Table("presences").Where("contract_id = ?", tr.Row.ID).Where("event = 'arrived'").Count(context.Background())
		if n != 1 {
			t.Fatalf("presences %d", n)
		}
		c, _ := store.GetContract(context.Background(), db.DB, e.scope, tr.Row.ID)
		if c.Status != string(contract.StatusArrived) || e.AuditCount(t, "contract.arrived")+e.AuditCount(t, "transit.arrived") > 1 {
			t.Fatalf("transit %s", c.Status)
		}
		e.invariants(t)
	})
}
