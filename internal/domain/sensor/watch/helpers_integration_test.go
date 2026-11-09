//go:build integration

package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// wenv is the backend of the reconciliation tests: the contracttest marina (berths A1..A4, the 60 m linear area
// L1, a transit list), the maintenance service and the watch service. Devices and physical states are written
// through the store (the ingestion is INGEST's), exactly as sensor.Ingest leaves them: a state change and the
// device state row.
type wenv struct {
	*contracttest.Env
	w     *Service
	maint *maintenance.Service
	scope store.MarinaScope
	n     int
}

func newWenv(t testing.TB, db *store.DB) *wenv {
	t.Helper()
	ce := contracttest.New(t, db)
	e := &wenv{Env: ce}
	e.maint = maintenance.NewService(maintenance.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log, Keys: ce.Keys, Files: ce.Files, Occupancy: ce.Occupancy,
	})
	e.w = NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log, Presence: ce.Presence, Occupancy: ce.Occupancy,
		Maintenance: e.maint,
	})
	ce.Occupancy.SetSensorReader(e.w)
	e.scope = ce.Scope(t)
	return e
}

func (e *wenv) now() time.Time { return store.UTC(e.Clock.Now()) }

func (e *wenv) loc() *time.Location {
	l, _ := time.LoadLocation("Europe/Rome")
	return l
}

func (e *wenv) today() calendar.Date { return calendar.In(e.now(), e.loc()) }

// device inserts an active device on a resource (created 30 days ago).
func (e *wenv) device(t testing.TB, resourceType, resourceID string, from, to *int64) *store.SensorDevice {
	t.Helper()
	e.n++
	now := e.now()
	d := &store.SensorDevice{
		ID: store.NewID(), MarinaID: e.MarinaID, ExternalID: "EXT" + strconv.Itoa(e.n), Code: "S" + strconv.Itoa(e.n),
		Kind: store.SensorKindUltrasonic, ResourceType: resourceType, ResourceID: resourceID, FromCm: from, ToCm: to,
		MapID: e.MapID, XCm: 1000, YCm: 1000, Active: true, Version: 1, CreatedAt: now.Add(-30 * 24 * time.Hour), UpdatedAt: now,
	}
	e.Insert(t, d)
	return d
}

// set confirms a physical state since an instant (state change + state row), with a contact now.
func (e *wenv) set(t testing.TB, d *store.SensorDevice, state string, since time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := store.GetSensorDeviceState(ctx, e.DB.DB, e.scope, d.ID)
	if err != nil {
		st = &store.SensorDeviceState{DeviceID: d.ID, MarinaID: e.MarinaID, Physical: store.SensorUnknown}
	}
	since = store.UTC(since)
	if st.Physical != state {
		if err := store.InsertSensorStateChange(ctx, e.DB.DB, &store.SensorStateChange{
			MarinaID: e.MarinaID, DeviceID: d.ID, ResourceType: d.ResourceType, ResourceID: d.ResourceID, MapID: d.MapID,
			FromCm: d.FromCm, ToCm: d.ToCm, FromState: st.Physical, ToState: state, At: since, Reason: store.SensorReasonReadings,
			CreatedAt: e.now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := e.now()
	b := 80
	if st.BatteryPct != nil {
		b = *st.BatteryPct
	}
	st.Physical, st.PhysicalSince, st.LastObservedAt, st.LastContactAt, st.BatteryPct, st.UpdatedAt = state, &since, &now, &now, &b, now
	if err := store.UpsertSensorDeviceState(ctx, e.DB.DB, st); err != nil {
		t.Fatal(err)
	}
}

// battery sets the battery of a device (and a contact now).
func (e *wenv) battery(t testing.TB, d *store.SensorDevice, pct int) {
	t.Helper()
	ctx := context.Background()
	st, err := store.GetSensorDeviceState(ctx, e.DB.DB, e.scope, d.ID)
	if err != nil {
		st = &store.SensorDeviceState{DeviceID: d.ID, MarinaID: e.MarinaID, Physical: store.SensorUnknown}
	}
	now := e.now()
	st.BatteryPct, st.LastContactAt, st.UpdatedAt = &pct, &now, now
	if err := store.UpsertSensorDeviceState(ctx, e.DB.DB, st); err != nil {
		t.Fatal(err)
	}
}

// mooring creates an active mooring contract of a new vessel on a berth for 2027 and returns it.
func (e *wenv) mooring(t testing.TB, berth, term string) *contract.Detail {
	t.Helper()
	v := e.AddVessel(t, "Vessel"+strconv.Itoa(e.n), 900, 300, "monohull_motor")
	e.n++
	c, err := e.Contracts.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, contract.MooringInput{
		Term: term, CustomerID: e.Customer, VesselID: v, StartsOn: contracttest.D("2027-01-01"),
		Placements: []contract.Segment{contracttest.BerthSeg(berth, "2027-01-01", "2027-12-31")}, Activate: true,
	})
	if err != nil {
		t.Fatalf("create mooring: %v", err)
	}
	return c
}

func (e *wenv) reconcile(t testing.TB) ReconcileResult {
	t.Helper()
	sys, err := store.SystemMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.w.ReconcileMarina(context.Background(), sys)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return r
}

func (e *wenv) enableEffect(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.LockSensorSettings(ctx, e.DB.DB, e.scope, e.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.NewUpdate().Table("sensor_settings").Set("presence_effect_enabled = ?", true).Where("marina_id = ?", e.MarinaID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
}

// openAlert returns the open alert of a device and kind (fails when there is none).
func (e *wenv) openAlert(t testing.TB, deviceID, kind string) store.SensorAlert {
	t.Helper()
	var rows []store.SensorAlert
	if err := e.DB.NewSelect().Model(&rows).Where("device_id = ?", deviceID).Where("kind = ?", kind).Where("status = 'open'").Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("open %s alerts of %s: %d", kind, deviceID, len(rows))
	}
	return rows[0]
}

func (e *wenv) countAlerts(t testing.TB, where string, args ...any) int {
	t.Helper()
	n, err := e.DB.NewSelect().Table("sensor_alerts").Where("marina_id = ?", e.MarinaID).Where(where, args...).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// invariants checks Q42 and Q43.
func (e *wenv) invariants(t testing.TB) {
	t.Helper()
	v, err := store.CheckSensorAlertInvariants(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) > 0 {
		t.Fatalf("sensor invariants: %+v", v)
	}
}

// occupancyHash hashes the rows that sensors must never change (INV-100).
func (e *wenv) occupancyHash(t testing.TB) string {
	t.Helper()
	h := sha256.New()
	for _, q := range []string{
		"SELECT id, status, first_night, last_night, version FROM allocations ORDER BY id",
		"SELECT berth_id, night, allocation_id FROM berth_occupancies ORDER BY berth_id, night",
		"SELECT area_id, cell, night, allocation_id FROM cell_occupancies ORDER BY area_id, cell, night",
		"SELECT resource_id, night, allocation_id FROM land_occupancies ORDER BY resource_id, night",
		"SELECT id, status, from_night, to_night, version FROM releases ORDER BY id",
		"SELECT id, status, version FROM contracts ORDER BY id",
		"SELECT id, version FROM charges ORDER BY id",
	} {
		rows, err := e.DB.QueryContext(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(h, "%v|", vals)
		}
		_ = rows.Close()
		_, _ = fmt.Fprint(h, "\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}

func i64(v int64) *int64 { return &v }
