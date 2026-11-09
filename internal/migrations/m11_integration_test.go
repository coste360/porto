//go:build integration

package migrations_test

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMigrations_M11Constraints: the M11 fleet and handover guarantees that are database constraints hold on both
// dialects (m11-plan §15-§16.4; the booking and slot ones are TestMigrations_M11BookingConstraints): rental_units
// (unique code and vessel per marina, third-party ownership iff an owner, a tank for fuelled units); rental_formulas
// (band iff half_day, fuel item iff metered, late step 15/30/60); INV-99 rental_booking_charges (one main and one
// return link per booking, a replaced link beside a new main accepted, a charge linked once); rental_handovers (one
// per booking and phase, client_ref unique per marina, identity only at check-out); and the CHECKs 00025 widened:
// payment_intents.purpose rental_deposit, signature_requests subject_type rental and driver screen,
// generated_documents.subject_type rental_booking. Up/Down/Up of 00023-00025 is TestMigrations_UpDownUp.
func TestMigrations_M11Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		m := f.marinaID
		day := calendar.New(2027, 6, 12)

		rc := &store.RevenueClass{ID: store.NewID(), MarinaID: m, Code: "rental", Name: store.JSON(`{"it":"Noleggio"}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		item := &store.Item{
			ID: store.NewID(), MarinaID: m, Code: "NOL", Name: store.JSON(`{"it":"x"}`), Description: store.JSON(`{}`), ItemType: "service",
			Unit: "hour", VATRateBP: 2200, RevenueClassID: rc.ID, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, rc, item)

		// rental_units.
		unit := func(code, vesselID string) *store.RentalUnit {
			return &store.RentalUnit{
				ID: store.NewID(), MarinaID: m, VesselID: vesselID, Code: code, Category: store.RentalCategoryRIB, Seats: 6,
				Licence: store.RentalLicenceThreshold, FuelType: store.RentalFuelPetrol, TankMilli: 60_000, Status: store.RentalUnitAvailable,
				ItemID: item.ID, Ownership: store.RentalOwnershipMarina, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		u := unit("GOM1", f.vesselID)
		mustInsert(t, db, u)
		wantUnique(t, "second unit of a vessel", insertRow(t, db, unit("GOM2", f.vesselID)))
		v2 := &store.Vessel{
			ID: store.NewID(), MarinaID: m, Name: "Due", HullType: "rib", LOACm: 500, BeamCm: 200, DimensionsSource: "declared",
			Engines: store.JSON(`[]`), SearchText: "due", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, v2)
		wantUnique(t, "unit code reused", insertRow(t, db, unit("GOM1", v2.ID)))
		third := unit("GOM3", v2.ID)
		third.Ownership = store.RentalOwnershipThirdParty
		if err := insertRow(t, db, third); err == nil {
			t.Fatal("third-party unit without an owner accepted")
		}
		owned := unit("GOM4", v2.ID)
		owned.OwnerCustomerID = f.customerID
		if err := insertRow(t, db, owned); err == nil {
			t.Fatal("marina unit with an owner accepted")
		}
		noTank := unit("GOM5", v2.ID)
		noTank.TankMilli = 0
		if err := insertRow(t, db, noTank); err == nil {
			t.Fatal("petrol unit without a tank accepted")
		}

		// rental_formulas.
		formula := func(code, kind string) *store.RentalFormula {
			return &store.RentalFormula{
				ID: store.NewID(), MarinaID: m, Code: code, Name: store.JSON(`{"it":"x"}`), Kind: kind, MinSlots: 2, MinDays: 1, MaxDays: 30,
				Skipper: store.RentalSkipperOptional, Fuel: store.RentalFuelIncluded, GraceMinutes: 15, LateStepMinutes: 30,
				Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		hourly := formula("ORARIA", store.RentalKindHourly)
		mustInsert(t, db, hourly)
		if err := insertRow(t, db, formula("MEZZA", store.RentalKindHalfDay)); err == nil {
			t.Fatal("half_day formula without a band accepted")
		}
		banded := formula("ORA-B", store.RentalKindHourly)
		banded.BandCode = "morning"
		if err := insertRow(t, db, banded); err == nil {
			t.Fatal("hourly formula with a band accepted")
		}
		metered := formula("MET", store.RentalKindHourly)
		metered.Fuel = store.RentalFuelMetered
		if err := insertRow(t, db, metered); err == nil {
			t.Fatal("metered formula without a fuel item accepted")
		}
		step := formula("STEP", store.RentalKindHourly)
		step.LateStepMinutes = 20
		if err := insertRow(t, db, step); err == nil {
			t.Fatal("late step 20 accepted")
		}
		wantUnique(t, "formula code reused", insertRow(t, db, formula("ORARIA", store.RentalKindDay)))

		// INV-99: rental_booking_charges.
		b := &store.RentalBooking{
			ID: store.NewID(), MarinaID: m, Number: 1, Year: 2027, UnitID: u.ID, FormulaID: hourly.ID, StartsOn: day, StartSlot: 20,
			EndsOn: day, EndSlot: 24, Status: store.RentalStatusConfirmed, Channel: store.RentalChannelDesk, PriceSnapshot: store.JSON(`{}`),
			EngineVersion: 1, DepositStatus: store.RentalDepositStatusNone, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, b)
		n := 0
		charge := func() *store.Charge {
			n++
			return &store.Charge{
				ID: store.NewID(), MarinaID: m, Number: "R2027-" + string(rune('A'+n)), OriginType: store.ChargeOriginRental, OriginID: b.ID,
				IssuedOn: day, DueOn: day, TotalCents: 1000, NetCents: 1000, PaymentStatus: store.ChargeOpen,
				InvoicingStatus: store.ChargeToInvoice, FiscalMode: "receipt", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		c1, c2, c3, c4 := charge(), charge(), charge(), charge()
		mustInsert(t, db, c1, c2, c3, c4)
		link := func(c *store.Charge, kind string) *store.RentalBookingCharge {
			return store.NewRentalBookingCharge(store.MarinaScope{}, b.ID, c.ID, kind, now)
		}
		fix := func(l *store.RentalBookingCharge) *store.RentalBookingCharge {
			l.MarinaID = m
			return l
		}
		mustInsert(t, db, fix(link(c1, store.RentalChargeMain)), fix(link(c3, store.RentalChargeReturn)))
		wantUnique(t, "second main link", insertRow(t, db, fix(link(c2, store.RentalChargeMain))))
		wantUnique(t, "second return link", insertRow(t, db, fix(link(c4, store.RentalChargeReturn))))
		if _, err := db.NewUpdate().Table("rental_booking_charges").Set("kind = ?", store.RentalChargeReplaced).
			Where("charge_id = ?", c1.ID).Exec(t.Context()); err != nil {
			t.Fatalf("main -> replaced: %v", err)
		}
		mustInsert(t, db, fix(link(c2, store.RentalChargeMain)))
		wantUnique(t, "a charge linked twice", insertRow(t, db, fix(link(c2, store.RentalChargeAdjustment))))
		if err := insertRow(t, db, fix(link(c4, "deposit"))); err == nil {
			t.Fatal("unknown link kind accepted")
		}

		// rental_handovers.
		ho := func(phase, ref string) *store.RentalHandover {
			h := &store.RentalHandover{
				ID: store.NewID(), MarinaID: m, BookingID: b.ID, Phase: phase, Checklist: store.JSON(`[]`), ClientRef: ref,
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			return h
		}
		mustInsert(t, db, ho(store.RentalPhaseCheckout, "ref-00000001"))
		wantUnique(t, "second check-out handover", insertRow(t, db, ho(store.RentalPhaseCheckout, "")))
		wantUnique(t, "client_ref reused", insertRow(t, db, ho(store.RentalPhaseCheckin, "ref-00000001")))
		withIdentity := ho(store.RentalPhaseCheckin, "")
		withIdentity.IdentityEnc = "x"
		if err := insertRow(t, db, withIdentity); err == nil {
			t.Fatal("identity on a check-in handover accepted")
		}
		mustInsert(t, db, ho(store.RentalPhaseCheckin, ""))

		// Widened CHECKs of 00025.
		intent := &store.PaymentIntent{
			ID: store.NewID(), MarinaID: m, Purpose: "rental_deposit", ProviderCode: "stripe", CustomerID: f.customerID,
			AmountCents: 50000, Currency: "EUR", Status: "open", Attempt: 1, IdempotencyKey: "rental-dep-1", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, intent)
		file := &store.StoredFile{
			ID: store.NewID(), MarinaID: m, Purpose: "document", ContentType: "application/pdf", SizeBytes: 10,
			SHA256: "abc", StorageKey: m + "/r.bin", CreatedAt: now, UpdatedAt: now,
		}
		doc := &store.GeneratedDocument{
			ID: store.NewID(), MarinaID: m, Kind: "rental_contract", BuiltinRef: "it/rental_contract", Locale: "it",
			SubjectType: "rental_booking", SubjectID: b.ID, Status: "ready", FileID: file.ID, SHA256: "abc", Pages: 1,
			PDFA: true, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, file, doc)
		sig := &store.SignatureRequest{
			ID: store.NewID(), MarinaID: m, SubjectType: "rental", SubjectID: b.ID, DocumentID: doc.ID, Driver: "screen",
			OrderMode: "parallel", Status: "sent", ExpiresAt: now.Add(time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, sig)
		bad := *sig
		bad.ID, bad.Driver = store.NewID(), "carrier_pigeon"
		if err := insertRow(t, db, &bad); err == nil {
			t.Fatal("unknown signature driver accepted")
		}
	})
}
