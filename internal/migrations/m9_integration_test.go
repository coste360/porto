//go:build integration

package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMigrations_M9Constraints: the M9 guarantees that are database constraints hold on both dialects (m9-plan
// §13): storage details dates and enums, a scenario item is placed or unplaced as a whole and unique per
// allocation, applied scenarios carry their instant, a done movement its instant; plan tasks are unique per
// (plan, element, due date) and a done task has an outcome; meter readings are unique per (meter, instant) and per
// client_ref (INV-83), never negative; a consumption is charged if and only if it has a charge and is unique per
// (interval, share) (INV-90, INV-91); one live primary charge per utility billing (charges_origin_primary_uidx).
func TestMigrations_M9Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		d := calendar.New

		// Storage details.
		det := &store.StorageContractDetails{
			ContractID: f.contractID, MarinaID: f.marinaID, Support: store.StorageSupportCradle, SupportOwner: store.StorageSupportOwn,
			HaulOutOn: d(2027, 10, 1), LaunchOn: d(2027, 9, 1), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, det); err == nil {
			t.Fatal("launch before haul-out accepted")
		}
		det.LaunchOn = d(2028, 4, 1)
		det.Support = "boat_stand"
		if err := insertRow(t, db, det); err == nil {
			t.Fatal("unknown support accepted")
		}
		det.Support = store.StorageSupportCradle
		mustInsert(t, db, det)
		wantUnique(t, "second details row", insertRow(t, db, det))

		// Scenarios: a surface area of the fixture map.
		area := &store.DynamicArea{
			ID: store.NewID(), MarinaID: f.marinaID, MapID: f.mapID, Kind: "surface", Code: "S1",
			Outline: store.JSON(`[{"x":0,"y":0},{"x":1000,"y":0},{"x":1000,"y":1000},{"x":0,"y":1000}]`), CellCm: 100,
			Supports: store.Strings{}, DepthProfile: store.JSON(`[]`), BlockedCells: store.JSON(`[]`), Exits: store.JSON(`[]`),
			CellCount: 100, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, area)
		sc := &store.StorageScenario{
			ID: store.NewID(), MarinaID: f.marinaID, AreaID: area.ID, Name: "A", FromOn: d(2027, 10, 1), ToOn: d(2027, 9, 1),
			Status: store.ScenarioDraft, Score: store.JSON(`{}`), Base: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, sc); err == nil {
			t.Fatal("reversed scenario window accepted")
		}
		sc.ToOn = d(2028, 3, 31)
		sc.Status = store.ScenarioApplied
		if err := insertRow(t, db, sc); err == nil {
			t.Fatal("applied scenario without applied_at accepted")
		}
		sc.Status = store.ScenarioDraft
		mustInsert(t, db, sc)
		alloc := allocation(t, db, f.occupancyFixture, f.landID, d(2027, 10, 1), d(2027, 10, 5))
		x, y, rot := int64(100), int64(200), int64(900)
		half := &store.ScenarioItem{ScenarioID: sc.ID, MarinaID: f.marinaID, AllocationID: alloc, XCm: &x, Origin: store.ScenarioOriginArea}
		if err := insertRow(t, db, half); err == nil {
			t.Fatal("half placed scenario item accepted")
		}
		item := &store.ScenarioItem{
			ScenarioID: sc.ID, MarinaID: f.marinaID, AllocationID: alloc, XCm: &x, YCm: &y, RotationDd: &rot, Origin: store.ScenarioOriginTray,
		}
		mustInsert(t, db, item)
		wantUnique(t, "second item of an allocation", insertRow(t, db, item))
		bad := int64(450)
		other := allocation(t, db, f.occupancyFixture, f.landID, d(2027, 11, 1), d(2027, 11, 5))
		if err := insertRow(t, db, &store.ScenarioItem{
			ScenarioID: sc.ID, MarinaID: f.marinaID, AllocationID: other, XCm: &x, YCm: &y, RotationDd: &bad, Origin: store.ScenarioOriginArea,
		}); err == nil {
			t.Fatal("rotation 45° accepted on a surface")
		}
		mv := &store.StorageMovement{
			ID: store.NewID(), MarinaID: f.marinaID, AreaID: area.ID, ScenarioID: sc.ID, AllocationID: alloc, Seq: 1,
			MoveOn: d(2027, 10, 1), ToResourceType: store.ResourceArea, ToResourceID: area.ID, EquipmentHint: store.EquipmentHintNone,
			Status: store.MovementDone, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, mv); err == nil {
			t.Fatal("done movement without done_at accepted")
		}
		mv.Status = store.MovementPending
		mustInsert(t, db, mv)
		mv2 := *mv
		mv2.ID = store.NewID()
		wantUnique(t, "same scenario sequence", insertRow(t, db, &mv2))

		// Maintenance tasks.
		plan := &store.MaintenancePlan{
			ID: store.NewID(), MarinaID: f.marinaID, Name: "P", Intervention: "custom", PeriodKind: store.PeriodMonths,
			Checklist: store.JSON(`[]`), Nights: 1, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, plan); err == nil {
			t.Fatal("custom intervention without label accepted")
		}
		plan.Intervention = "general"
		if err := insertRow(t, db, plan); err == nil {
			t.Fatal("months periodicity without every accepted")
		}
		every := int64(12)
		plan.PeriodEvery = &every
		mustInsert(t, db, plan)
		task := func(due calendar.Date) *store.MaintenanceTask {
			return &store.MaintenanceTask{
				ID: store.NewID(), MarinaID: f.marinaID, ElementType: store.ElementBerth, ElementID: f.berthID, PlanID: plan.ID,
				Source: store.TaskSourcePlan, Title: "T", Priority: "normal", Status: store.TaskOpen, DueOn: due,
				Checklist: store.JSON(`[]`), ChecklistResults: store.JSON(`[]`), AllocationIDs: store.JSON(`[]`),
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, task(d(2027, 6, 1)))
		wantUnique(t, "same plan, element and due date", insertRow(t, db, task(d(2027, 6, 1))))
		done := task(d(2027, 7, 1))
		done.Status = store.TaskDone
		if err := insertRow(t, db, done); err == nil {
			t.Fatal("done task without outcome accepted")
		}
		report := task(calendar.Date{})
		report.PlanID, report.Source, report.ClientRef = "", store.TaskSourceReport, "client-ref-1"
		mustInsert(t, db, report)
		report2 := task(calendar.Date{})
		report2.PlanID, report2.Source, report2.ClientRef = "", store.TaskSourceReport, "client-ref-1"
		wantUnique(t, "same report client_ref", insertRow(t, db, report2))
		nowhere := task(calendar.Date{})
		nowhere.PlanID, nowhere.Source, nowhere.ElementType, nowhere.ElementID = "", store.TaskSourceOffice, "", ""
		if err := insertRow(t, db, nowhere); err == nil {
			t.Fatal("task without element or map accepted")
		}

		// Meters and readings.
		meter := &store.UtilityMeter{
			ID: store.NewID(), MarinaID: f.marinaID, Code: "M1", Kind: store.MeterPower, Unit: "m3", MultiplierMilli: 1000,
			Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, meter); err == nil {
			t.Fatal("power meter in m3 accepted")
		}
		meter.Unit = "kwh"
		mustInsert(t, db, meter)
		dupMeter := *meter
		dupMeter.ID = store.NewID()
		wantUnique(t, "same meter code", insertRow(t, db, &dupMeter))
		reading := func(at time.Time, value int64, ref string) *store.MeterReading {
			return &store.MeterReading{
				ID: store.NewID(), MarinaID: f.marinaID, MeterID: meter.ID, ReadAt: at, ReadOn: d(2027, 6, at.Day()),
				ValueMilli: value, Kind: store.ReadingRegular, Source: "staff", Status: store.ReadingValid,
				AnomalyCodes: store.JSON(`[]`), ClientRef: ref, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		t1, t2 := time.Date(2027, 6, 1, 9, 0, 0, 0, time.UTC), time.Date(2027, 6, 10, 9, 0, 0, 0, time.UTC)
		if err := insertRow(t, db, reading(t1, -1, "")); err == nil {
			t.Fatal("negative reading accepted")
		}
		r1, r2 := reading(t1, 1000, "reading-ref-1"), reading(t2, 5000, "")
		mustInsert(t, db, r1, r2)
		wantUnique(t, "same meter instant", insertRow(t, db, reading(t1, 2000, "")))
		wantUnique(t, "same reading client_ref", insertRow(t, db, reading(time.Date(2027, 6, 5, 9, 0, 0, 0, time.UTC), 2000, "reading-ref-1")))

		// Consumptions and the utility charge.
		billing := &store.UtilityBilling{
			ID: store.NewID(), MarinaID: f.marinaID, ContractID: f.contractID, Kind: "primary", RunOn: d(2027, 6, 10), CreatedAt: now,
		}
		mustInsert(t, db, billing)
		cons := func(share, status, chargeID string) *store.UtilityConsumption {
			return &store.UtilityConsumption{
				ID: store.NewID(), MarinaID: f.marinaID, MeterID: meter.ID, FromReadingID: r1.ID, ToReadingID: r2.ID,
				ShareKey: share, FirstNight: d(2027, 6, 1), LastNight: d(2027, 6, 9), QtyMilli: 4000, Status: status,
				ChargeID: chargeID, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		if err := insertRow(t, db, cons(store.ConsumptionShareUnassigned, store.ConsumptionCharged, "")); err == nil {
			t.Fatal("charged consumption without charge accepted")
		}
		mustInsert(t, db, cons(store.ConsumptionShareUnassigned, store.ConsumptionUnassigned, ""))
		wantUnique(t, "same interval share", insertRow(t, db, cons(store.ConsumptionShareUnassigned, store.ConsumptionPending, "")))
		charge := func() *store.Charge {
			id := store.NewID()
			return &store.Charge{
				ID: id, MarinaID: f.marinaID, Number: "U-" + id[len(id)-6:], CustomerID: f.customerID,
				OriginType: store.ChargeOriginUtility, OriginID: billing.ID, IssuedOn: d(2027, 6, 10), DueOn: d(2027, 7, 10),
				TotalCents: 100, NetCents: 100, PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice,
				FiscalMode: "invoice", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		first := charge()
		mustInsert(t, db, first)
		wantUnique(t, "second live utility charge of a billing", insertRow(t, db, charge()))
		if _, err := db.NewRaw("UPDATE utility_consumptions SET status = 'charged', charge_id = ? WHERE share_key = 'unassigned'", first.ID).Exec(ctx); err != nil {
			t.Fatalf("charging a consumption: %v", err)
		}
		if _, err := db.NewRaw("UPDATE utility_consumptions SET charge_id = NULL WHERE share_key = 'unassigned'").Exec(ctx); err == nil {
			t.Fatal("charged consumption lost its charge")
		}
	})
}

// allocation inserts an active transit allocation of the fixture vessel on a land resource and returns its id.
func allocation(t *testing.T, db *store.DB, f occupancyFixture, landID string, first, last calendar.Date) string {
	t.Helper()
	now := store.UTC(time.Now())
	a := &store.Allocation{
		ID: store.NewID(), MarinaID: f.marinaID, VesselID: f.vesselID, ResourceType: store.ResourceLand, ResourceID: landID,
		MapID: f.mapID, FirstNight: first, LastNight: last, Kind: store.AllocKindTransit, Status: store.AllocActive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mustInsert(t, db, a)
	return a.ID
}
