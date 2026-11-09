package price

import (
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

func TestLateFee_Table(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	planned := time.Date(2027, 6, 12, 13, 0, 0, 0, rome)
	at := func(h, m int) time.Time { return time.Date(2027, 6, 12, h, m, 0, 0, rome) }
	cases := []struct {
		name    string
		planned time.Time
		actual  time.Time
		late    int
		blocks  int
		fee     money.Cents
	}{
		{"13:14_within_grace", planned, at(13, 14), 0, 0, 0},
		{"13:16", planned, at(13, 16), 1, 1, 3000},
		{"13:45", planned, at(13, 45), 30, 1, 3000},
		{"13:50", planned, at(13, 50), 35, 2, 6000},
		{"15:20", planned, at(15, 20), 125, 5, 15000},
		{"early_return", planned, at(11, 0), 0, 0, 0},
	}
	// Autumn DST in Europe/Rome on 2027-10-31: 03:00 CEST goes back to 02:00 CET. Planned end 02:00 (first
	// occurrence, CEST = 00:00 UTC), returned at the second 02:30 (CET = 01:30 UTC): 90 real minutes.
	dstPlanned := time.Date(2027, 10, 31, 0, 0, 0, 0, time.UTC).In(rome)
	dstActual := time.Date(2027, 10, 31, 1, 30, 0, 0, time.UTC).In(rome)
	cases = append(cases, struct {
		name    string
		planned time.Time
		actual  time.Time
		late    int
		blocks  int
		fee     money.Cents
	}{"dst_autumn_second_0230", dstPlanned, dstActual, 75, 3, 9000})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := LateFee(c.planned, c.actual, 15, 30, 6000)
			if got.LateMinutes != c.late || got.Blocks != c.blocks || got.Fee != c.fee {
				t.Fatalf("got %+v, want late %d blocks %d fee %d", got, c.late, c.blocks, c.fee)
			}
		})
	}
	if LateFee(planned, at(15, 0), 15, 30, 0).Fee != 0 {
		t.Fatal("no late item means no fee")
	}
	if got := LateFee(planned, at(13, 31), 0, 15, 4501); got.Fee != 3*1125 {
		t.Fatalf("step 15 half-even block: %+v", got)
	}
}

func TestFuel_Table(t *testing.T) {
	refuel := qty.Milli(10_000)
	neg := qty.Milli(-5)
	cases := []struct {
		name   string
		in     FuelInput
		litres qty.Milli
		amount money.Cents
	}{
		{"tank_60_8_to_5", FuelInput{OutEighths: 8, InEighths: 5, Tank: 60_000, UnitCents: 210}, 22_500, 4725},
		{"full_return", FuelInput{OutEighths: 8, InEighths: 8, Tank: 60_000, UnitCents: 210}, 0, 0},
		{"returned_fuller_clamped", FuelInput{OutEighths: 6, InEighths: 8, Tank: 60_000, UnitCents: 210}, 0, 0},
		{"explicit_refuel_wins", FuelInput{OutEighths: 8, InEighths: 2, Tank: 60_000, Refuel: &refuel, UnitCents: 210}, 10_000, 2100},
		{"negative_refuel_clamped", FuelInput{OutEighths: 8, InEighths: 2, Tank: 60_000, Refuel: &neg, UnitCents: 210}, 0, 0},
		{"half_even_milli", FuelInput{OutEighths: 1, InEighths: 0, Tank: 25_003, UnitCents: 199}, 3125, 622},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Fuel(c.in)
			if got.Litres != c.litres || got.Amount != c.amount {
				t.Fatalf("got %+v, want %d l %d", got, c.litres, c.amount)
			}
		})
	}
}

func TestDepositProposal_Table(t *testing.T) {
	cases := []struct{ deposit, estimates, want money.Cents }{
		{50000, 18000, 18000}, {50000, 0, 0}, {50000, 70000, 50000}, {0, 1000, 0},
	}
	for _, c := range cases {
		if got := DepositProposal(c.deposit, c.estimates); got != c.want {
			t.Fatalf("proposal(%d,%d) = %d, want %d", c.deposit, c.estimates, got, c.want)
		}
	}
}

func TestReturn_WorkedExample(t *testing.T) {
	// m11-plan §8.2: late 60.00 + fuel 47.25, damage 180.00 fully covered by a 500.00 deposit.
	got, err := Return(ReturnInput{LateFee: 6000, Fuel: 4725, Deposit: 50000, Estimates: 18000, Retained: 18000})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 10725 || got.DamageExcess != 0 || got.Proposal != 18000 {
		t.Fatalf("return %+v", got)
	}
	got, _ = Return(ReturnInput{Deposit: 10000, Estimates: 25000, Retained: 10000, Extras: 500})
	if got.DamageExcess != 15000 || got.Total != 15500 {
		t.Fatalf("excess %+v", got)
	}
	if _, err := Return(ReturnInput{Deposit: 100, Retained: 101}); !errors.Is(err, ErrDepositExceeds) {
		t.Fatalf("err %v", err)
	}
}

func TestRapid_DepositSettlement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deposit := money.Cents(rapid.Int64Range(0, 1_000_000).Draw(t, "deposit"))
		n := rapid.IntRange(0, 5).Draw(t, "n")
		var estimates money.Cents
		for i := range n {
			estimates += money.Cents(rapid.Int64Range(0, 500_000).Draw(t, "estimate"+string(rune('a'+i))))
		}
		retained := money.Cents(rapid.Int64Range(-10, int64(deposit)+10).Draw(t, "retained"))
		st, err := Settle(deposit, retained, estimates)
		if retained < 0 || retained > deposit {
			if !errors.Is(err, ErrDepositExceeds) {
				t.Fatalf("want deposit_exceeds, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if st.Retained+st.Released != deposit || st.Retained > deposit || st.Released < 0 {
			t.Fatalf("split %+v of %d", st, deposit)
		}
		if want := max(estimates-retained, 0); st.Excess != want {
			t.Fatalf("excess %d, want %d", st.Excess, want)
		}
		if p := DepositProposal(deposit, estimates); p > deposit || p > estimates || p < 0 {
			t.Fatalf("proposal %d", p)
		}
	})
}

func TestLicenceRequired_Table(t *testing.T) {
	cases := []struct {
		mode    string
		kw      int64
		skipper bool
		want    bool
	}{
		{LicenceNever, 100_000, false, false},
		{LicenceAlways, 1_000, false, true},
		{LicenceAlways, 1_000, true, false},
		{LicenceThreshold, 40_000, false, true},
		{LicenceThreshold, 30_000, false, false},
		{LicenceThreshold, 40_000, true, false},
	}
	for _, c := range cases {
		if got := LicenceRequired(c.mode, c.kw, 30_000, c.skipper); got != c.want {
			t.Fatalf("%+v: got %v", c, got)
		}
	}
}

func TestBuild_Snapshot(t *testing.T) {
	a := money.Cents(15000)
	s := money.Cents(500)
	cat := pricing.Catalog{
		PriceList: pricing.PriceList{ID: "l", ValidFrom: calendar.MustParse("2027-01-01"), ValidTo: calendar.MustParse("2027-12-31")},
		Items:     []pricing.Item{{ID: "gom", Code: "G", RevenueClass: "rental", PriceIncludesVAT: true, Active: true}, {ID: "frg", Code: "F", Active: true}},
		Rules: []pricing.Rule{
			{ID: "r", Stage: pricing.StageBase, Active: true, ItemID: "gom", Basis: pricing.BasisFlat, Unit: pricing.UnitHalfDay, AmountCents: &a, Scope: pricing.Scope{TimeBands: []string{"morning"}}},
			{ID: "f", Stage: pricing.StageBase, Active: true, ItemID: "frg", Basis: pricing.BasisMetered, Unit: pricing.UnitStay, UnitCents: &s},
		},
	}
	snap, err := Build([]pricing.Catalog{cat}, SnapshotInput{
		UnitID: "u", UnitItemID: "gom", FormulaID: "f1", FormulaCode: "MEZZA-MATTINA", Kind: pricing.RentalHalfDay,
		Start: calendar.MustParse("2027-06-12"), Band: "morning", LicenceRequired: true,
		Extras: []pricing.RentalExtra{{ItemID: "frg", Qty: qty.One, PerDay: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Total() != 15500 || snap.Days != 1 || snap.EngineVersion != pricing.RentalEngineVersion || snap.WithSkipper || !snap.LicenceRequired {
		t.Fatalf("snapshot %+v", snap)
	}
	if Days(pricing.RentalMultiDay, 3) != 3 || Days(pricing.RentalHourly, 1) != 1 || Days(pricing.RentalWeek, 9) != 9 {
		t.Fatal("days")
	}
}
