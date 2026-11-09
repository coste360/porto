//go:build integration

package report_test

import (
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestReport_Rental: the four rental views on rows written as the rental services write them (§8.2 flow: one
// half-day booking with slots 16-25 booked + 26 buffer, a block, a main and a return charge, a deposit retention
// charge, one damage and the deposit), the tiles equal to the totals of their drill views (INV-62), deposits never
// revenue (INV-98) and the permissions (reports.rental or reports.all; another marina 404).
func TestReport_Rental(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		now := store.UTC(e.Clock.Now())
		day := calendar.MustParse("2027-06-12")
		var itemID, revClass string
		if err := db.NewSelect().Table("revenue_classes").Column("id").Where("marina_id = ?", e.MarinaID).Limit(1).Scan(ctx, &revClass); err != nil {
			// The defaults are created on first use: list them through the service.
			cls, lerr := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, true)
			if lerr != nil || len(cls) == 0 {
				t.Fatalf("revenue classes: %v %v", err, lerr)
			}
			revClass = cls[0].ID
		}
		it := &store.Item{
			ID: store.NewID(), MarinaID: e.MarinaID, Code: "NOL-GOM58", Name: store.JSON(`{"it":"Gommone"}`), Description: store.JSON(`{}`),
			ItemType: "service", Unit: "hour", VATRateBP: 2200, PriceIncludesVAT: true, RevenueClassID: revClass, Active: true,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, it)
		itemID = it.ID
		unit := &store.RentalUnit{
			ID: store.NewID(), MarinaID: e.MarinaID, VesselID: e.Vessel, Code: "GOM58", Category: "rib", Seats: 8, Licence: "threshold",
			FuelType: "petrol", TankMilli: 60000, DepositCents: 50000, Status: "available", ItemID: itemID, Ownership: "marina",
			Version: 1, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
		}
		formula := &store.RentalFormula{
			ID: store.NewID(), MarinaID: e.MarinaID, Code: "MEZZA-MATTINA", Name: store.JSON(`{"it":"Mattina"}`), Kind: "half_day",
			BandCode: "morning", MinSlots: 2, MinDays: 1, MaxDays: 30, Skipper: "none", Fuel: "included", GraceMinutes: 15,
			LateStepMinutes: 30, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		booking := &store.RentalBooking{
			ID: store.NewID(), MarinaID: e.MarinaID, Number: 1, Year: 2027, UnitID: unit.ID, FormulaID: formula.ID, StartsOn: day,
			StartSlot: 16, EndsOn: day, EndSlot: 26, Status: store.RentalStatusClosed, Channel: "desk", PriceSnapshot: store.JSON(`{}`),
			EngineVersion: 1, TotalCents: 16500, DepositMode: "cash", DepositCents: 50000, DepositStatus: "partially_retained",
			DepositRetainedCents: 18000, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		cancelled := &store.RentalBooking{
			ID: store.NewID(), MarinaID: e.MarinaID, Number: 2, Year: 2027, UnitID: unit.ID, FormulaID: formula.ID, StartsOn: day.AddDays(1),
			StartSlot: 16, EndsOn: day.AddDays(1), EndSlot: 26, Status: store.RentalStatusCancelled, Channel: "desk",
			PriceSnapshot: store.JSON(`{}`), EngineVersion: 1, DepositStatus: "none", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		block := &store.RentalBlock{
			ID: store.NewID(), MarinaID: e.MarinaID, UnitID: unit.ID, FromOn: day, FromSlot: 30, ToOn: day, ToSlot: 34, Reason: "maintenance",
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, unit, formula, booking, cancelled, block)
		var rows []any
		for s := 16; s <= 26; s++ {
			kind := store.RentalSlotBooked
			if s == 26 {
				kind = store.RentalSlotBuffer
			}
			rows = append(rows, &store.RentalSlot{UnitID: unit.ID, Date: day, Slot: s, MarinaID: e.MarinaID, BookingID: booking.ID, Kind: kind})
		}
		for s := 30; s < 34; s++ {
			rows = append(rows, &store.RentalSlot{UnitID: unit.ID, Date: day, Slot: s, MarinaID: e.MarinaID, BlockID: block.ID, Kind: store.RentalSlotBlock})
		}
		e.Insert(t, rows...)
		charge := func(n, origin, originID string, gross, net int64, lines ...[3]int64) *store.Charge {
			c := &store.Charge{
				ID: store.NewID(), MarinaID: e.MarinaID, Number: n, OriginType: origin, OriginID: originID, IssuedOn: day, DueOn: day,
				TotalCents: gross, NetCents: net, VATCents: gross - net, PaymentStatus: "open", InvoicingStatus: "to_invoice",
				FiscalMode: "receipt", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			e.Insert(t, c)
			for i, l := range lines {
				e.Insert(t, &store.ChargeLine{
					ID: store.NewID(), MarinaID: e.MarinaID, ChargeID: c.ID, Seq: i + 1, ItemID: itemID, ItemCode: "NOL-GOM58",
					QtyMilli: 1000, UnitCents: l[0], NetCents: l[1], VATCents: l[0] - l[1], GrossCents: l[0], VATRateBP: 2200,
					PriceIncludesVAT: true, RevenueClassCode: "rental", PlanKey: []string{"rental:unit", "rental:late", "rental:fuel"}[l[2]],
					CreatedAt: now, UpdatedAt: now,
				})
			}
			return c
		}
		main := charge("A2027-000001", store.ChargeOriginRental, booking.ID, 15000, 12295, [3]int64{15000, 12295, 0})
		ret := charge("A2027-000002", store.ChargeOriginRental, booking.ID, 10725, 8791, [3]int64{6000, 4918, 1}, [3]int64{4725, 3873, 2})
		charge("A2027-000003", store.ChargeOriginDepositRetention, booking.ID, 18000, 14754, [3]int64{18000, 14754, 0})
		e.Insert(t,
			&store.RentalBookingCharge{BookingID: booking.ID, ChargeID: main.ID, MarinaID: e.MarinaID, Kind: "main", CreatedAt: now},
			&store.RentalBookingCharge{BookingID: booking.ID, ChargeID: ret.ID, MarinaID: e.MarinaID, Kind: "return", CreatedAt: now},
			&store.RentalDamage{
				ID: store.NewID(), MarinaID: e.MarinaID, BookingID: booking.ID, Description: "Graffio", EstimateCents: 18000,
				Status: "charged", RetainedCents: 18000, Version: 1, CreatedAt: time.Date(2027, 6, 12, 11, 50, 0, 0, time.UTC), UpdatedAt: now,
			})

		svc := report.NewService(report.Deps{DB: db, Clock: e.Clock, Log: e.Log})
		q := report.Params{From: day, To: day.AddDays(1)}
		u, err := svc.Rental(ctx, e.Owner, e.MarinaID, q)
		// Window 16-38 = 22 slots; the block takes 4 on the 12th; 10 booked rows (the buffer row is not rented).
		if err != nil || u.Totals.AvailableSlots.Value != 18+22 || u.Totals.RentedSlots.Value != 10 || len(u.Records) != 2 {
			t.Fatalf("utilisation %+v %v", u, err)
		}
		q.View = calc.RentalViewRevenue
		rv, err := svc.Rental(ctx, e.Owner, e.MarinaID, q)
		if err != nil || len(rv.Records) != 3 || rv.Totals.Net.Value != 12295+4918+3873 || rv.Totals.Gross.Value != 15000+10725 {
			t.Fatalf("revenue %+v %v", rv, err)
		}
		kinds := map[string]bool{}
		for _, r := range rv.Records {
			kinds[r.LineKind] = true
			if r.BookingNumber != "NL2027-000001" || r.FormulaCode != "MEZZA-MATTINA" {
				t.Fatalf("revenue record %+v", r)
			}
		}
		if !kinds["unit"] || !kinds["late"] || !kinds["fuel"] {
			t.Fatalf("line kinds %v", kinds)
		}
		q.View = calc.RentalViewDamages
		dm, err := svc.Rental(ctx, e.Owner, e.MarinaID, q)
		if err != nil || len(dm.Records) != 1 || dm.Totals.Estimate.Value != 18000 || dm.Totals.Retained.Value != 18000 {
			t.Fatalf("damages %+v %v", dm, err)
		}
		q.View = calc.RentalViewDeposits
		dp, err := svc.Rental(ctx, e.Owner, e.MarinaID, q)
		if err != nil || len(dp.Records) != 1 || dp.Totals.Deposit.Value != 50000 || dp.Totals.Retained.Value != 18000 || dp.Totals.Released.Value != 32000 {
			t.Fatalf("deposits %+v %v", dp, err)
		}

		// Tiles = totals of the drill views; the deposit retention is not revenue.
		dash, err := svc.Dashboard(ctx, e.Owner, e.MarinaID, report.DashboardParams{From: day, To: day.AddDays(1), Compare: "none"})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, tile := range dash.Tiles {
			switch string(tile.Key) {
			case calc.IndRentalUtilisation:
				found++
				if !tile.Available || tile.Current.Numerator != 10 || tile.Current.Denominator != 40 || tile.Current.Value.Value != 2500 {
					t.Fatalf("utilisation tile %+v", tile)
				}
			case calc.IndRentalRevenue:
				found++
				if !tile.Available || tile.Current.Value.Value != int64(rv.Totals.Net.Value) {
					t.Fatalf("revenue tile %+v", tile)
				}
			}
		}
		if found != 2 {
			t.Fatalf("rental tiles %d", found)
		}

		// Permissions.
		rental := e.AddUser(t, "rental@porto.test", auth.RoleRental, e.OrgID, e.MarinaID)
		if _, err := svc.Rental(ctx, rental, e.MarinaID, q); err != nil {
			t.Fatalf("rental role: %v", err)
		}
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		if _, err := svc.Rental(ctx, office, e.MarinaID, q); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office: %v", err)
		}
		od, err := svc.Dashboard(ctx, office, e.MarinaID, report.DashboardParams{From: day, To: day})
		if err != nil {
			t.Fatal(err)
		}
		for _, tile := range od.Tiles {
			if string(tile.Key) == calc.IndRentalRevenue || string(tile.Key) == calc.IndRentalUtilisation {
				t.Fatalf("office sees %s", tile.Key)
			}
		}
		_, otherM, other := e.OtherOrganization(t)
		if _, err := svc.Rental(ctx, other, e.MarinaID, q); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other organization: %v", err)
		}
		if r, err := svc.Rental(ctx, other, otherM, q); err != nil || len(r.Records) != 0 {
			t.Fatalf("other marina sees rows: %+v %v", r, err)
		}
		q.GroupBy = "formula"
		q.View = calc.RentalViewUtilisation
		if _, err := svc.Rental(ctx, e.Owner, e.MarinaID, q); err == nil {
			t.Fatal("group_by formula accepted on utilisation")
		}
	})
}
