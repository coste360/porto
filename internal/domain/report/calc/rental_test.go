package calc

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

func rentalDay(s string) calendar.Date { return calendar.MustParse(s) }

func TestBuildRental_Utilisation(t *testing.T) {
	in := RentalInput{
		From: rentalDay("2027-06-12"), To: rentalDay("2027-06-13"), View: RentalViewUtilisation, WindowDay: 22,
		Units: []RentalUnitInfo{
			{ID: "u1", Code: "GOM58", Category: "rib", CreatedOn: rentalDay("2027-01-01")},
			{ID: "u2", Code: "SUP1", Category: "sup", CreatedOn: rentalDay("2027-06-13")},
			{ID: "u3", Code: "OLD", Category: "rib", Retired: true, CreatedOn: rentalDay("2027-01-01")},
		},
		SlotDays: []RentalSlotDay{
			{UnitID: "u1", Date: rentalDay("2027-06-12"), Rented: 10, Blocked: 2},
			{UnitID: "u1", Date: rentalDay("2027-06-13"), Rented: 22},
			{UnitID: "u2", Date: rentalDay("2027-06-13"), Rented: 4},
		},
	}
	res, facts := BuildRental(in)
	// u1: 10/20 and 22/22; u2: created on the 13th, 4/22; u3 retired: no records.
	if len(res.Records) != 3 {
		t.Fatalf("records %+v", res.Records)
	}
	tot := res.Totals
	if tot.AvailableSlots.Value != 64 || tot.RentedSlots.Value != 36 || tot.UtilisationBP.Value != 5625 {
		t.Fatalf("totals %+v", tot)
	}
	v := TileValueOf(IndRentalUtilisation, tot)
	if v.Value.Value != 5625 || v.Numerator != 36 || v.Denominator != 64 {
		t.Fatalf("tile %+v", v)
	}
	if s := facts[IndRentalUtilisation].Sum(); s.Num != 36 || s.Den != 64 {
		t.Fatalf("facts %+v", s)
	}
	in.Category, in.GroupBy = "rib", "unit"
	res, _ = BuildRental(in)
	if len(res.Records) != 2 || len(res.Groups) != 1 || res.Groups[0].Totals.UtilisationBP.Value != Ratio(32, 42).Value {
		t.Fatalf("rib %+v", res)
	}
}

func TestBuildRental_RevenueExcludesNothingButFilters(t *testing.T) {
	line := func(id, day, kind, formula string, net money.Cents) RentalChargeLine {
		return RentalChargeLine{
			ChargeID: id, IssuedOn: rentalDay(day), BookingNumber: "NL2027-000001", UnitID: "u1", UnitCode: "GOM58",
			Category: "rib", FormulaID: formula, FormulaCode: formula, LineKind: kind, Net: net, VAT: net / 5, Gross: net + net/5,
		}
	}
	in := RentalInput{
		From: rentalDay("2027-06-01"), To: rentalDay("2027-06-30"), View: RentalViewRevenue, GroupBy: "line_kind",
		ChargeLine: []RentalChargeLine{
			line("c1", "2027-06-12", RentalLineUnit, "f1", 12295),
			line("c1", "2027-06-12", RentalLineExtra, "f1", 410),
			line("c2", "2027-06-12", RentalLineLate, "f1", 4918),
			line("c3", "2027-07-01", RentalLineUnit, "f1", 9999),
		},
	}
	res, facts := BuildRental(in)
	if len(res.Records) != 3 || res.Totals.Net.Value != 17623 || len(res.Groups) != 3 {
		t.Fatalf("revenue %+v", res)
	}
	if TileValueOf(IndRentalRevenue, res.Totals).Value.Value != 17623 || facts[IndRentalRevenue].Sum().Num != 17623 {
		t.Fatal("tile is not the sum of the drill")
	}
	in.FormulaID = "f2"
	if res, _ = BuildRental(in); len(res.Records) != 0 || res.Totals.Net.Value != 0 {
		t.Fatalf("formula filter %+v", res)
	}
}

func TestBuildRental_DamagesAndDeposits(t *testing.T) {
	in := RentalInput{
		From: rentalDay("2027-06-01"), To: rentalDay("2027-06-30"), View: RentalViewDamages,
		Damages: []RentalDamageRow{
			{ID: "d1", Date: rentalDay("2027-06-12"), Status: "charged", Estimate: 18000, Retained: 18000},
			{ID: "d2", Date: rentalDay("2027-06-20"), Status: "open", Estimate: 5000},
		},
		Deposits: []RentalDepositRow{
			{BookingID: "b1", Date: rentalDay("2027-06-12"), Mode: "cash", Status: "partially_retained", Deposit: 50000, Retained: 18000},
			{BookingID: "b2", Date: rentalDay("2027-06-13"), Mode: "preauth", Status: "authorized", Deposit: 30000},
			{BookingID: "b3", Date: rentalDay("2027-06-14"), Mode: "waived", Status: "waived", Deposit: 0},
			{BookingID: "b4", Date: rentalDay("2027-06-15"), Mode: "card_pos", Status: "released", Deposit: 20000},
		},
	}
	res, _ := BuildRental(in)
	if res.Totals.Estimate.Value != 23000 || res.Totals.Retained.Value != 18000 || res.Totals.Records != 2 {
		t.Fatalf("damages %+v", res.Totals)
	}
	in.View = RentalViewDeposits
	res, _ = BuildRental(in)
	tt := res.Totals
	if tt.Records != 4 || tt.Deposit.Value != 100000 || tt.Retained.Value != 18000 || tt.Released.Value != 52000 || tt.Outstanding.Value != 30000 {
		t.Fatalf("deposits %+v", tt)
	}
}

// TestRapid_RentalDepositsBalance: every deposit record splits into retained + released + outstanding = deposit.
func TestRapid_RentalDepositsBalance(t *testing.T) {
	statuses := []string{"pending", "held", "authorized", "capture_pending", "released", "retained", "partially_retained", "waived"}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 20).Draw(t, "n")
		in := RentalInput{From: rentalDay("2027-06-01"), To: rentalDay("2027-06-30"), View: RentalViewDeposits}
		for i := range n {
			dep := money.Cents(rapid.Int64Range(0, 200_000).Draw(t, "dep"))
			in.Deposits = append(in.Deposits, RentalDepositRow{
				BookingID: string(rune('a' + i)), Date: rentalDay("2027-06-10"), Mode: "cash",
				Status: rapid.SampledFrom(statuses).Draw(t, "status"), Deposit: dep,
				Retained: money.Cents(rapid.Int64Range(0, int64(dep)).Draw(t, "ret")),
			})
		}
		res, _ := BuildRental(in)
		for _, r := range res.Records {
			if r.Retained.Value+r.Released.Value+r.Outstanding.Value != r.Deposit.Value && r.DepositStatus != "waived" {
				t.Fatalf("record %+v", r)
			}
		}
		tt := res.Totals
		if tt.Retained.Value+tt.Released.Value+tt.Outstanding.Value > tt.Deposit.Value {
			t.Fatalf("totals %+v", tt)
		}
	})
}

func TestRentalGroupAllowed(t *testing.T) {
	if !RentalGroupAllowed(RentalViewRevenue, "formula") || RentalGroupAllowed(RentalViewUtilisation, "formula") || !RentalGroupAllowed(RentalViewDeposits, "") {
		t.Fatal("group_by rules")
	}
}
