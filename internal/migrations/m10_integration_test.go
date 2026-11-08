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

// TestMigrations_M10Constraints: the M10 guarantees that are database constraints hold on both dialects (m10-plan
// §15): widened series and document subject CHECKs; insert-only package versions (INV-28) that still cascade with
// their package; a variation quote carries its order and a quote has one deposit kind; one order per quote (INV-96);
// one running timer per employee (INV-95) and approved entries carry their cost; offline client_ref dedupe (INV-83);
// a stock movement is an issue or a return; one live primary charge per work order billing (INV-94); no double
// booking of a machine or an operator (INV-17).
func TestMigrations_M10Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		d := calendar.New
		m := f.marinaID

		// Widened CHECKs (D12).
		if _, err := db.NewInsert().Model(&map[string]any{"marina_id": m, "series": "yard_quote", "year": 2027, "next_number": 1}).
			TableExpr("number_sequences").Exec(ctx); err != nil {
			t.Fatalf("series yard_quote: %v", err)
		}
		if _, err := db.NewInsert().Model(&map[string]any{"marina_id": m, "series": "bogus", "year": 2027, "next_number": 1}).
			TableExpr("number_sequences").Exec(ctx); err == nil {
			t.Fatal("unknown series accepted")
		}

		// Catalogue.
		rc := &store.RevenueClass{ID: store.NewID(), MarinaID: m, Code: "yard", Name: store.JSON(`{"it":"Cantiere"}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		item := func(code, typ, unit string) *store.Item {
			return &store.Item{
				ID: store.NewID(), MarinaID: m, Code: code, Name: store.JSON(`{"it":"x"}`), Description: store.JSON(`{}`), ItemType: typ,
				Unit: unit, VATRateBP: 2200, RevenueClassID: rc.ID, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		hand, paint, tin := item("LAB-HAND", "labour", "hour"), item("LAB-PAINT", "labour", "hour"), item("MAT-AVEG", "material", "piece")
		mustInsert(t, db, rc, hand, paint, tin)
		wt := &store.WorkType{ID: store.NewID(), MarinaID: m, Code: "ANTIFOULING", Name: store.JSON(`{"it":"Antivegetativa"}`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		q1 := &store.YardQualification{ID: store.NewID(), MarinaID: m, Code: "hand", Name: store.JSON(`{}`), LabourItemID: hand.ID, Active: true, LiftOperator: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, wt, q1)
		dupLabour := *q1
		dupLabour.ID, dupLabour.Code = store.NewID(), "other"
		wantUnique(t, "second qualification on one labour item", insertRow(t, db, &dupLabour))
		dupCode := *wt
		dupCode.ID = store.NewID()
		wantUnique(t, "work type code", insertRow(t, db, &dupCode))

		v := &store.FormulaVariable{ID: store.NewID(), MarinaID: m, Name: "hull_area", Source: "derived", Kind: "number", Unit: "m2", Expression: "", EnumValues: store.Strings{}, Label: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := insertRow(t, db, v); err == nil {
			t.Fatal("derived variable without expression accepted")
		}
		v.Expression = "loa * (beam + draft) * 0.85"
		mustInsert(t, db, v)

		pkg := &store.WorkPackage{ID: store.NewID(), MarinaID: m, Code: "ANTIVEG", Name: store.JSON(`{}`), WorkTypeID: wt.ID, Active: true, CurrentVersion: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
		ver := &store.WorkPackageVersion{ID: store.NewID(), MarinaID: m, PackageID: pkg.ID, Version: 1, LanguageVersion: 1, Lines: store.JSON(`[]`), JobVars: store.Strings{"layers"}, CreatedAt: now}
		mustInsert(t, db, pkg, ver)
		dupVer := *ver
		dupVer.ID = store.NewID()
		wantUnique(t, "package version", insertRow(t, db, &dupVer))
		_, err := db.NewUpdate().TableExpr("work_package_versions").Set("lines = ?", `[{"seq":1}]`).Where("id = ?", ver.ID).Exec(ctx)
		wantTrigger(t, "update of a package version", "work_package_version_immutable", err)
		_, err = db.NewDelete().TableExpr("work_package_versions").Where("id = ?", ver.ID).Exec(ctx)
		wantTrigger(t, "delete of a package version", "work_package_version_immutable", err)

		// Quotes.
		q := &store.YardQuote{
			ID: store.NewID(), MarinaID: m, Number: 1, Year: 2027, Kind: "variation", CustomerID: f.customerID, VesselID: f.vesselID,
			Status: "draft", QuotedOn: d(2027, 3, 1), ValidUntil: d(2027, 3, 31), ExpiresAt: now, BillingMode: "actual",
			JobInputs: store.JSON(`{}`), VesselOverrides: store.JSON(`{}`), Snapshot: store.JSON(`{}`), EstimatedHours: store.JSON(`{}`),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, q); err == nil {
			t.Fatal("variation without work order accepted")
		}
		q.Kind = "job"
		bp, cents := 3000, int64(10000)
		q.DepositBP, q.DepositCents = &bp, &cents
		if err := insertRow(t, db, q); err == nil {
			t.Fatal("two deposit kinds accepted")
		}
		q.DepositCents = nil
		mustInsert(t, db, q)
		pv := 1
		line := &store.YardQuoteLine{
			ID: store.NewID(), MarinaID: m, QuoteID: q.ID, Seq: 1, Source: "package", ItemID: tin.ID, WorkTypeID: wt.ID,
			QtyMilli: 5000, UnitCents: 9500, VATRateBP: 2200, Issues: store.JSON(`[]`), CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, line); err == nil {
			t.Fatal("package line without package reference accepted")
		}
		line.PackageID, line.PackageVersion, line.PackageLineSeq = pkg.ID, &pv, &pv
		mustInsert(t, db, line)

		// Work orders: one order per quote (INV-96).
		wo := &store.WorkOrder{
			ID: store.NewID(), MarinaID: m, Number: 1, Year: 2027, QuoteID: q.ID, CustomerID: f.customerID, VesselID: f.vesselID,
			Status: "planned", BillingMode: "actual", RateSnapshot: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, wo)
		wo2 := *wo
		wo2.ID, wo2.Number = store.NewID(), 2
		wantUnique(t, "second order of one quote", insertRow(t, db, &wo2))
		wo2.QuoteID, wo2.Status = "", "cancelled"
		if err := insertRow(t, db, &wo2); err == nil {
			t.Fatal("cancelled order without cancelled_at accepted")
		}

		task := &store.WorkTask{
			ID: store.NewID(), MarinaID: m, WorkOrderID: wo.ID, Seq: 1, Title: "Carteggiatura", QualificationID: q1.ID,
			EstimatedHoursMilli: 8400, Status: "done", ProgressBP: 5000, Checklist: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, task); err == nil {
			t.Fatal("done task below 100 % accepted")
		}
		task.Status, task.ClientRef = "todo", "task-ref-0001"
		mustInsert(t, db, task)
		if err := insertRow(t, db, &store.WorkTaskDep{TaskID: task.ID, DependsOnTaskID: task.ID, MarinaID: m}); err == nil {
			t.Fatal("self dependency accepted")
		}

		// Employees, timers (INV-95), approved cost.
		emp := &store.Employee{ID: store.NewID(), MarinaID: m, Code: "E1", FirstName: "Luca", LastName: "B", HourlyCostCents: 2200, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, emp)
		running := func(ref string) *store.Timesheet {
			return &store.Timesheet{
				ID: store.NewID(), MarinaID: m, EmployeeID: emp.ID, WorkOrderID: wo.ID, TaskID: task.ID, WorkDate: d(2027, 3, 2),
				StartedAt: now, Source: "timer", Status: "running", ClientRef: ref, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, running("timer-ref-0001"))
		wantUnique(t, "second running timer", insertRow(t, db, running("timer-ref-0002")))
		manual := &store.Timesheet{
			ID: store.NewID(), MarinaID: m, EmployeeID: emp.ID, WorkOrderID: wo.ID, WorkDate: d(2027, 3, 2), HoursMilli: 4000,
			Source: "manual", Status: "approved", ClientRef: "manual-ref-1", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, manual); err == nil {
			t.Fatal("approved entry without cost accepted")
		}
		rate, cost := int64(2200), int64(8800)
		manual.CostRateCents, manual.CostCents = &rate, &cost
		mustInsert(t, db, manual)
		replay := *manual
		replay.ID = store.NewID()
		wantUnique(t, "offline replay of a time entry", insertRow(t, db, &replay))

		// Materials.
		mv := &store.StockMovement{
			ID: store.NewID(), MarinaID: m, WorkOrderID: wo.ID, ItemID: tin.ID, Kind: "load", QtyMilli: 6000, UnitCostCents: 5800,
			UnitPriceCents: 9500, MovedOn: d(2027, 3, 2), CreatedAt: now,
		}
		if err := insertRow(t, db, mv); err == nil {
			t.Fatal("stock load accepted (no stock management in M10)")
		}
		mv.Kind = "issue"
		mustInsert(t, db, mv)

		// One live primary charge per billing (INV-94).
		billingID := store.NewID()
		charge := func() *store.Charge {
			id := store.NewID()
			return &store.Charge{
				ID: id, MarinaID: m, Number: "W-" + id[len(id)-6:], CustomerID: f.customerID, OriginType: store.ChargeOriginWorkOrder,
				OriginID: billingID, IssuedOn: d(2027, 3, 1), DueOn: d(2027, 3, 31), TotalCents: 100, NetCents: 100,
				PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice", Version: 1,
				CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, charge())
		wantUnique(t, "second primary charge of a billing", insertRow(t, db, charge()))

		// Lifting slots (INV-17).
		eq := &store.Equipment{ID: store.NewID(), MarinaID: m, Code: "TL-1", Name: "Travel lift", Kind: "travel_lift", Active: true, Bookable: true, DefaultMinutes: 30, Version: 1, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, eq)
		from, to := 32, 34
		bk := func() *store.EquipmentBooking {
			return &store.EquipmentBooking{
				ID: store.NewID(), MarinaID: m, EquipmentID: eq.ID, Date: d(2027, 3, 3), FromSlot: &from, ToSlot: &to,
				Purpose: "haul_out", VesselID: f.vesselID, OperatorEmployeeID: emp.ID, Status: "booked", Source: "office",
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		b1, b2 := bk(), bk()
		mustInsert(t, db, b1, b2)
		mustInsert(t, db, &store.EquipmentSlot{EquipmentID: eq.ID, Date: d(2027, 3, 3), Slot: 32, MarinaID: m, BookingID: b1.ID})
		wantUnique(t, "machine slot booked twice", insertRow(t, db, &store.EquipmentSlot{EquipmentID: eq.ID, Date: d(2027, 3, 3), Slot: 32, MarinaID: m, BookingID: b2.ID}))
		mustInsert(t, db, &store.OperatorSlot{EmployeeID: emp.ID, Date: d(2027, 3, 3), Slot: 32, MarinaID: m, BookingID: b1.ID})
		wantUnique(t, "operator slot booked twice", insertRow(t, db, &store.OperatorSlot{EmployeeID: emp.ID, Date: d(2027, 3, 3), Slot: 32, MarinaID: m, BookingID: b2.ID}))
		if err := insertRow(t, db, &store.EquipmentSlot{EquipmentID: eq.ID, Date: d(2027, 3, 3), Slot: 96, MarinaID: m, BookingID: b2.ID}); err == nil {
			t.Fatal("slot 96 accepted")
		}
		bad := bk()
		bad.FromSlot, bad.ToSlot = &to, &from
		if err := insertRow(t, db, bad); err == nil {
			t.Fatal("reversed window accepted")
		}
		req := bk()
		req.FromSlot, req.ToSlot, req.Status, req.EquipmentID = nil, nil, "requested", ""
		mustInsert(t, db, req)
		req2 := bk()
		req2.FromSlot, req2.ToSlot, req2.Status = nil, nil, "booked"
		if err := insertRow(t, db, req2); err == nil {
			t.Fatal("booked booking without slots accepted")
		}

		// A package delete cascades through its insert-only versions (the trigger finds no package row).
		if _, err := db.NewDelete().TableExpr("work_packages").Where("id = ?", pkg.ID).Exec(ctx); err != nil {
			t.Fatalf("delete package: %v", err)
		}
		if n, err := db.NewSelect().TableExpr("work_package_versions").Where("package_id = ?", pkg.ID).Count(ctx); err != nil || n != 0 {
			t.Fatalf("versions after the package delete: %d %v", n, err)
		}
	})
}
