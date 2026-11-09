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

// TestMigrations_M11BookingConstraints: the M11 booking guarantees that are database constraints hold on both dialects
// (m11-plan §4.3, §15, INV-18): the series 'rental'; one booking per (marina, year, number); a held booking has a hold
// expiry and only a held one; invoice requests need a customer; the primary keys of rental_slots and
// rental_skipper_slots refuse a second owner of a slot (ON CONFLICT DO NOTHING inserts nothing); a slot row is owned
// by exactly one booking or block; skipper rows have no buffer kind; a block targets exactly one unit or skipper.
func TestMigrations_M11BookingConstraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		m := f.marinaID
		day := calendar.New(2027, 6, 12)

		if _, err := db.NewInsert().Model(&map[string]any{"marina_id": m, "series": "rental", "year": 2027, "next_number": 1}).
			TableExpr("number_sequences").Exec(ctx); err != nil {
			t.Fatalf("series rental: %v", err)
		}
		rc := &store.RevenueClass{ID: store.NewID(), MarinaID: m, Code: "rental", Name: store.JSON(`{"it":"Noleggio"}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		item := &store.Item{
			ID: store.NewID(), MarinaID: m, Code: "NOL", Name: store.JSON(`{"it":"x"}`), Description: store.JSON(`{}`), ItemType: "service",
			Unit: "hour", VATRateBP: 2200, RevenueClassID: rc.ID, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		unit := &store.RentalUnit{
			ID: store.NewID(), MarinaID: m, VesselID: f.vesselID, Code: "GOM1", Category: store.RentalCategoryRIB, Seats: 6,
			Licence: store.RentalLicenceThreshold, FuelType: store.RentalFuelPetrol, TankMilli: 60_000, Status: store.RentalUnitAvailable,
			ItemID: item.ID, Ownership: store.RentalOwnershipMarina, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		sk := &store.RentalSkipper{ID: store.NewID(), MarinaID: m, Name: "Skipper", Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		formula := &store.RentalFormula{
			ID: store.NewID(), MarinaID: m, Code: "ORARIA", Name: store.JSON(`{"it":"Oraria"}`), Kind: store.RentalKindHourly, MinSlots: 2,
			MinDays: 1, MaxDays: 30, Skipper: store.RentalSkipperOptional, Fuel: store.RentalFuelIncluded, GraceMinutes: 15,
			LateStepMinutes: 30, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, rc, item, unit, sk, formula)
		booking := func(n int, status string) *store.RentalBooking {
			b := &store.RentalBooking{
				ID: store.NewID(), MarinaID: m, Number: n, Year: 2027, UnitID: unit.ID, FormulaID: formula.ID, StartsOn: day, StartSlot: 20,
				EndsOn: day, EndSlot: 24, Status: status, Channel: store.RentalChannelDesk, PriceSnapshot: store.JSON(`{}`), EngineVersion: 1,
				DepositStatus: store.RentalDepositStatusNone, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if status == store.RentalStatusHeld {
				b.HoldExpiresAt = now.Add(time.Hour)
			}
			return b
		}
		a, b := booking(1, store.RentalStatusConfirmed), booking(2, store.RentalStatusHeld)
		mustInsert(t, db, a, b)
		dup := booking(1, store.RentalStatusConfirmed)
		wantUnique(t, "booking number", insertRow(t, db, dup))
		heldNoExpiry := booking(3, store.RentalStatusHeld)
		heldNoExpiry.HoldExpiresAt = time.Time{}
		if err := insertRow(t, db, heldNoExpiry); err == nil {
			t.Fatal("held booking without expiry accepted")
		}
		confirmedWithExpiry := booking(4, store.RentalStatusConfirmed)
		confirmedWithExpiry.HoldExpiresAt = now
		if err := insertRow(t, db, confirmedWithExpiry); err == nil {
			t.Fatal("confirmed booking with a hold expiry accepted")
		}
		invoice := booking(5, store.RentalStatusConfirmed)
		invoice.InvoiceRequested = true
		if err := insertRow(t, db, invoice); err == nil {
			t.Fatal("invoice request without a customer accepted")
		}
		reversed := booking(6, store.RentalStatusConfirmed)
		reversed.EndSlot = 20
		if err := insertRow(t, db, reversed); err == nil {
			t.Fatal("empty range accepted")
		}

		scope, err := store.ResolveMarinaScope(ctx, db.DB, m)
		if err != nil {
			t.Fatal(err)
		}
		// INV-18: the slot primary keys.
		row := func(bk *store.RentalBooking, slot int, kind string) store.RentalSlot {
			return store.RentalSlot{UnitID: unit.ID, Date: day, Slot: slot, MarinaID: m, BookingID: bk.ID, Kind: kind}
		}
		first := []store.RentalSlot{row(a, 20, "booked"), row(a, 21, "booked"), row(a, 22, "buffer")}
		mustInsert(t, db, &first)
		taken := row(b, 21, "hold")
		taken.ExpiresAt = now.Add(time.Hour)
		wantUnique(t, "second owner of a unit slot", insertRow(t, db, &taken))
		n, err := store.InsertRentalSlots(ctx, db.DB, scope, []store.RentalSlot{taken, func() store.RentalSlot {
			r := row(b, 23, "hold")
			r.ExpiresAt = now.Add(time.Hour)
			return r
		}()})
		if err != nil || n != 1 {
			t.Fatalf("ON CONFLICT DO NOTHING inserted %d, %v", n, err)
		}
		holdNoExpiry := row(b, 30, "hold")
		if err := insertRow(t, db, &holdNoExpiry); err == nil {
			t.Fatal("hold row without expiry accepted")
		}
		badSlot := row(a, 48, "booked")
		if err := insertRow(t, db, &badSlot); err == nil {
			t.Fatal("slot 48 accepted")
		}
		blk := &store.RentalBlock{
			ID: store.NewID(), MarinaID: m, UnitID: unit.ID, FromOn: day.AddDays(1), FromSlot: 0, ToOn: day.AddDays(1), ToSlot: 48,
			Reason: store.RentalBlockMaintenance, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, blk)
		both := row(a, 40, "block")
		both.BlockID = blk.ID
		if err := insertRow(t, db, &both); err == nil {
			t.Fatal("slot row with both booking and block accepted")
		}
		blockKindNoBlock := row(a, 41, "block")
		if err := insertRow(t, db, &blockKindNoBlock); err == nil {
			t.Fatal("block kind without a block accepted")
		}
		twoTargets := *blk
		twoTargets.ID, twoTargets.SkipperID = store.NewID(), sk.ID
		if err := insertRow(t, db, &twoTargets); err == nil {
			t.Fatal("block with two targets accepted")
		}

		// Skipper slots: same guarantee, no buffer kind.
		skRow := store.RentalSkipperSlot{SkipperID: sk.ID, Date: day, Slot: 20, MarinaID: m, BookingID: a.ID, Kind: "booked"}
		mustInsert(t, db, &skRow)
		skTaken := skRow
		skTaken.BookingID, skTaken.Kind, skTaken.ExpiresAt = b.ID, "hold", now.Add(time.Hour)
		wantUnique(t, "second owner of a skipper slot", insertRow(t, db, &skTaken))
		skBuffer := store.RentalSkipperSlot{SkipperID: sk.ID, Date: day, Slot: 25, MarinaID: m, BookingID: a.ID, Kind: "buffer"}
		if err := insertRow(t, db, &skBuffer); err == nil {
			t.Fatal("skipper buffer row accepted")
		}
	})
}
