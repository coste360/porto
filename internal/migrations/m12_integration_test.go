//go:build integration

package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMigrations_M12Constraints: the M12 sensor and profile guarantees that are database constraints hold on both
// dialects (m12-plan §15): sensor_devices (unique external id and code per marina, segment iff dynamic_area with
// from < to, socket only on pedestals, at most one binding, one active device per berth); sensor_readings
// idempotent on (device_id, observed_at) (INV-101); sensor_state_changes from <> to; sensor_alerts one open alert
// per (device, kind) (INV-104); marina_profiles depth min <= max and published only with a position; the CHECKs
// 00026 widened: maintenance_tasks element_type and source sensor. Up/Down/Up is TestMigrations_UpDownUp.
func TestMigrations_M12Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := occupancyRows(t, db)
		now := store.UTC(time.Now())
		m := f.marinaID
		ctx := context.Background()

		gw := &store.SensorGateway{ID: store.NewID(), MarinaID: m, Code: "GW1", Name: "G", Kind: "lorawan", Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		src := &store.SensorSource{ID: store.NewID(), MarinaID: m, Code: "fake", Name: "F", PollSeconds: 60, Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, gw, src)
		wantUnique(t, "gateway code reused", insertRow(t, db, &store.SensorGateway{
			ID: store.NewID(), MarinaID: m, Code: "GW1", Name: "H", Kind: "other", Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}))

		device := func(ext, code, typ, res string) *store.SensorDevice {
			return &store.SensorDevice{
				ID: store.NewID(), MarinaID: m, ExternalID: ext, Code: code, Kind: "ultrasonic", ResourceType: typ, ResourceID: res,
				MapID: f.mapID, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		d := device("EXT1", "S1", "berth", f.berthID)
		mustInsert(t, db, d)
		wantUnique(t, "second active device on a berth", insertRow(t, db, device("EXT2", "S2", "berth", f.berthID)))
		wantUnique(t, "external id reused", insertRow(t, db, device("EXT1", "S3", "pedestal", store.NewID())))
		wantUnique(t, "code reused", insertRow(t, db, device("EXT4", "S1", "pedestal", store.NewID())))
		inactive := device("EXT5", "S5", "berth", f.berthID)
		inactive.Active = false
		mustInsert(t, db, inactive)
		if err := insertRow(t, db, device("EXT6", "S6", "dynamic_area", f.areaID)); err == nil {
			t.Fatal("dynamic_area device without a segment accepted")
		}
		from, to := int64(500), int64(100)
		bad := device("EXT7", "S7", "dynamic_area", f.areaID)
		bad.FromCm, bad.ToCm = &from, &to
		if err := insertRow(t, db, bad); err == nil {
			t.Fatal("segment with from >= to accepted")
		}
		to = 900
		seg := device("EXT8", "S8", "dynamic_area", f.areaID)
		seg.FromCm, seg.ToCm = &from, &to
		mustInsert(t, db, seg)
		berthSeg := device("EXT9", "S9", "berth", store.NewID())
		berthSeg.FromCm, berthSeg.ToCm = &from, &to
		if err := insertRow(t, db, berthSeg); err == nil {
			t.Fatal("segment on a berth device accepted")
		}
		sock := device("EXT10", "S10", "berth", store.NewID())
		sock.PedestalSocket = "P1"
		if err := insertRow(t, db, sock); err == nil {
			t.Fatal("socket on a berth device accepted")
		}
		both := device("EXT11", "S11", "pedestal", store.NewID())
		both.GatewayID, both.SourceID = gw.ID, src.ID
		if err := insertRow(t, db, both); err == nil {
			t.Fatal("device bound to a gateway and a source accepted")
		}

		// Readings: idempotent on (device_id, observed_at).
		at := now.Truncate(time.Millisecond)
		r := &store.SensorReading{DeviceID: d.ID, ObservedAt: at, MarinaID: m, ReceivedAt: now, State: "occupied", Origin: "device"}
		mustInsert(t, db, r)
		res, err := db.NewInsert().Model(&store.SensorReading{DeviceID: d.ID, ObservedAt: at, MarinaID: m, ReceivedAt: now, State: "free", Origin: "device"}).
			On("CONFLICT (device_id, observed_at) DO NOTHING").Exec(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := res.RowsAffected(); n != 0 {
			t.Fatalf("duplicate reading stored (%d rows)", n)
		}

		// State changes and device state helpers.
		if err := store.InsertSensorStateChange(ctx, db.DB, &store.SensorStateChange{
			MarinaID: m, DeviceID: d.ID, ResourceType: "berth", ResourceID: f.berthID, MapID: f.mapID, FromState: "free", ToState: "free",
			At: now, Reason: "readings", CreatedAt: now,
		}); err == nil {
			t.Fatal("state change without a change accepted")
		}
		if err := store.InsertSensorStateChange(ctx, db.DB, &store.SensorStateChange{
			MarinaID: m, DeviceID: d.ID, ResourceType: "berth", ResourceID: f.berthID, MapID: f.mapID, FromState: "unknown", ToState: "occupied",
			At: now, Reason: "readings", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		since := now
		for _, phys := range []string{"free", "occupied"} {
			if err := store.UpsertSensorDeviceState(ctx, db.DB, &store.SensorDeviceState{
				DeviceID: d.ID, MarinaID: m, Physical: phys, PhysicalSince: &since, UpdatedAt: now,
			}); err != nil {
				t.Fatal(err)
			}
		}
		scope, err := store.ResolveMarinaScope(ctx, db.DB, m)
		if err != nil {
			t.Fatal(err)
		}
		byBerth, err := store.SensorPhysicalByBerth(ctx, db.DB, scope)
		if err != nil || byBerth[f.berthID] != "occupied" {
			t.Fatalf("SensorPhysicalByBerth: %v %v", byBerth, err)
		}

		// Alerts: one open alert per (device, kind).
		alert := func(key string) *store.SensorAlert {
			return &store.SensorAlert{
				ID: store.NewID(), MarinaID: m, Kind: "battery_low", DeviceID: d.ID, ResourceType: "berth", ResourceID: f.berthID,
				MapID: f.mapID, ConditionKey: key, Refs: store.JSON(`{}`), Status: "open", OpenedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, alert("battery"))
		wantUnique(t, "second open alert of a kind", insertRow(t, db, alert("battery2")))
		resolved := alert("battery3")
		resolved.Status = "resolved"
		mustInsert(t, db, resolved)
		dismissed := alert("battery4")
		dismissed.Status = "dismissed"
		if err := insertRow(t, db, dismissed); err == nil {
			t.Fatal("dismissed alert without decided_at accepted")
		}

		// maintenance_tasks accept the sensor element and source.
		task := &store.MaintenanceTask{
			ID: store.NewID(), MarinaID: m, ElementType: store.ElementSensor, ElementID: d.ID, Source: store.TaskSourceSensor,
			Title: "Batteria", Priority: "normal", Status: "open", MapID: f.mapID, Checklist: store.JSON(`[]`),
			ChecklistResults: store.JSON(`[]`), AllocationIDs: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, task)

		// marina_profiles.
		profile := func() *store.MarinaProfile {
			return &store.MarinaProfile{
				MarinaID: m, Locales: store.JSON(`["it"]`), Address: store.JSON(`{}`), VHFChannels: store.JSON(`[]`),
				OfficeHours: store.JSON(`[]`), DockHours: store.JSON(`[]`), HoursNote: store.JSON(`{}`), Shelter: store.JSON(`[]`),
				AccessNotes: store.JSON(`{}`), AccessImages: store.JSON(`[]`), Lights: store.JSON(`{}`), Services: store.JSON(`{}`),
				Photos: store.JSON(`[]`), OccupancyCategories: store.JSON(`[]`), POI: store.JSON(`[]`), Description: store.JSON(`{}`),
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		p := profile()
		minD, maxD := int64(500), int64(300)
		p.DepthMinCm, p.DepthMaxCm = &minD, &maxD
		if err := insertRow(t, db, p); err == nil {
			t.Fatal("depth min > max accepted")
		}
		p = profile()
		p.Published, p.PublishedAt = true, &now
		if err := insertRow(t, db, p); err == nil {
			t.Fatal("published profile without a position accepted")
		}
		lat, lon := int64(437101234), int64(104012345)
		p.EntranceLatE7, p.EntranceLonE7 = &lat, &lon
		mustInsert(t, db, p)
	})
}
