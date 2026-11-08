package policy

import (
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

func rome(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestPolicy_CheckStay(t *testing.T) {
	loc := rome(t)
	d := calendar.MustParse
	base := StayRules{MinNights: 2, MaxNights: 10, MinAdvanceHours: 2, MaxAdvanceDays: 30, ClosedWeekdays: []time.Weekday{time.Monday}}
	// 2026-06-10 is a Wednesday; now = 2026-06-10 10:00 in Rome.
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, loc)
	cases := []struct {
		name   string
		rules  StayRules
		arr    string
		dep    string
		now    time.Time
		reason string
	}{
		{"ok", base, "2026-06-12", "2026-06-14", now, ""},
		{"one night under min", base, "2026-06-12", "2026-06-13", now, BlockMinNights},
		{"departure before arrival", base, "2026-06-12", "2026-06-12", now, BlockMinNights},
		{"over max", base, "2026-06-12", "2026-06-23", now, BlockMaxNights},
		{"max exactly", base, "2026-06-12", "2026-06-22", now, ""},
		{"past arrival", base, "2026-06-09", "2026-06-12", now, BlockAdvance},
		{"today ok (end of day minus 2h)", base, "2026-06-10", "2026-06-12", now, ""},
		{"today too late", base, "2026-06-10", "2026-06-12", time.Date(2026, 6, 10, 22, 30, 0, 0, loc), BlockAdvance},
		{
			"arrival_to bounds today",
			StayRules{MinNights: 1, MinAdvanceHours: 2, ArrivalToMinutes: 20 * 60},
			"2026-06-10", "2026-06-11",
			time.Date(2026, 6, 10, 18, 30, 0, 0, loc), BlockAdvance,
		},
		{
			"arrival_to bounds today ok",
			StayRules{MinNights: 1, MinAdvanceHours: 2, ArrivalToMinutes: 20 * 60},
			"2026-06-10", "2026-06-11",
			time.Date(2026, 6, 10, 17, 30, 0, 0, loc), "",
		},
		{"too far", base, "2026-07-11", "2026-07-13", now, BlockAdvance},
		{"far exactly", base, "2026-07-10", "2026-07-12", now, ""},
		{"closed weekday", base, "2026-06-15", "2026-06-17", now, BlockWeekday},
		// Now is 23:30 UTC on 2026-06-10, which is already 2026-06-11 in Rome: arriving on the 10th is in the past.
		{"timezone edge", base, "2026-06-10", "2026-06-12", time.Date(2026, 6, 10, 23, 30, 0, 0, time.UTC), BlockAdvance},
		// DST change night (2026-10-25, 25-hour day in Rome): two nights stay two nights.
		{"dst", base, "2026-10-24", "2026-10-26", time.Date(2026, 10, 1, 9, 0, 0, 0, loc), ""},
		{"no max", StayRules{MinNights: 1}, "2026-06-12", "2027-06-12", now, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckStay(c.rules, Stay{Arrival: d(c.arr), Departure: d(c.dep), Now: c.now, Loc: loc})
			if got.OK != (c.reason == "") || got.Reason != c.reason {
				t.Fatalf("got %+v, want reason %q", got, c.reason)
			}
		})
	}
}

func TestPolicy_Approval(t *testing.T) {
	loa := geo.Cm(1500)
	cases := []struct {
		name   string
		rules  ApprovalRules
		in     ApprovalInput
		reason string
	}{
		{"none", ApprovalRules{}, ApprovalInput{LOACm: 3000}, ""},
		{"always wins", ApprovalRules{Always: true, LOAOverCm: &loa}, ApprovalInput{LOACm: 2000}, ApprovalAlways},
		{"loa over", ApprovalRules{LOAOverCm: &loa}, ApprovalInput{LOACm: 1501}, ApprovalLOA},
		{"loa equal", ApprovalRules{LOAOverCm: &loa}, ApprovalInput{LOACm: 1500}, ""},
		{"group", ApprovalRules{GroupIDs: []string{"g2"}}, ApprovalInput{LOACm: 900, GroupIDs: []string{"g1", "g2"}}, ApprovalGroup},
		{"other group", ApprovalRules{GroupIDs: []string{"g3"}}, ApprovalInput{GroupIDs: []string{"g1"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Approval(c.rules, c.in)
			if got.Required != (c.reason != "") || got.Reason != c.reason {
				t.Fatalf("got %+v, want %q", got, c.reason)
			}
		})
	}
}

func TestPolicy_Deposit(t *testing.T) {
	cases := []struct {
		name string
		in   DepositInput
		want money.Cents
	}{
		{"full", DepositInput{Mode: "full", BP: 3000, Total: 10_000}, 10_000},
		{"deposit 30%", DepositInput{Mode: "deposit", BP: 3000, Total: 10_000}, 3000},
		{"half even down", DepositInput{Mode: "deposit", BP: 5000, Total: 5}, 2},
		{"half even up", DepositInput{Mode: "deposit", BP: 5000, Total: 7}, 4},
		{"minimum one cent", DepositInput{Mode: "deposit", BP: 1, Total: 10}, 1},
		{"price list wins", DepositInput{Mode: "deposit", BP: 3000, Total: 10_000, PriceListDeposit: 2500}, 2500},
		{"capped at total", DepositInput{Mode: "deposit", BP: 3000, Total: 1000, PriceListDeposit: 5000}, 1000},
		{"zero total", DepositInput{Mode: "deposit", BP: 3000, Total: 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Deposit(c.in); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestPolicy_Refund(t *testing.T) {
	tiers := []Tier{{MinDaysBefore: 0, RefundBP: 0}, {MinDaysBefore: 7, RefundBP: 10_000}, {MinDaysBefore: 2, RefundBP: 5000}}
	arr := calendar.MustParse("2026-08-10")
	cases := []struct {
		today   string
		paid    money.Cents
		refund  money.Cents
		allowed bool
	}{
		{"2026-07-01", 10_000, 10_000, true},
		{"2026-08-03", 10_000, 10_000, true}, // exactly 7 days
		{"2026-08-04", 10_000, 5000, true},   // 6 days
		{"2026-08-08", 10_001, 5000, true},   // 2 days, 5000.5 → half even 5000
		{"2026-08-08", 10_003, 5002, true},   // 5001.5 → 5002
		{"2026-08-09", 10_000, 0, true},      // 1 day
		{"2026-08-10", 10_000, 0, false},     // arrival day
		{"2026-08-12", 10_000, 0, false},
	}
	for _, c := range cases {
		got := Refund(RefundInput{Tiers: tiers, Paid: c.paid, Arrival: arr, Today: calendar.MustParse(c.today)})
		if got.Refund != c.refund || got.Allowed != c.allowed {
			t.Fatalf("today %s paid %d: got %+v, want %d allowed %v", c.today, c.paid, got, c.refund, c.allowed)
		}
	}
	if got := Refund(RefundInput{
		Tiers: []Tier{{MinDaysBefore: 30, RefundBP: 10_000}}, Paid: 500, Arrival: arr,
		Today: calendar.MustParse("2026-08-01"),
	}); got.Refund != 0 || got.Tier != nil || !got.Allowed {
		t.Fatalf("no matching tier: %+v", got)
	}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(rt, "n")
		ts := make([]Tier, n)
		for i := range ts {
			ts[i] = Tier{MinDaysBefore: rapid.IntRange(0, 60).Draw(rt, "days"), RefundBP: rapid.IntRange(0, 10_000).Draw(rt, "bp")}
		}
		paid := money.Cents(rapid.Int64Range(0, 10_000_000).Draw(rt, "paid"))
		today := arr.AddDays(-rapid.IntRange(-5, 90).Draw(rt, "before"))
		r := Refund(RefundInput{Tiers: ts, Paid: paid, Arrival: arr, Today: today})
		if r.Refund < 0 || r.Refund > paid {
			rt.Fatalf("refund %d out of [0, %d]", r.Refund, paid)
		}
		if !today.Before(arr) && (r.Allowed || r.Refund != 0) {
			rt.Fatalf("allowed on or after arrival: %+v", r)
		}
	})
}

func TestPolicy_QuotaCap(t *testing.T) {
	cases := []struct {
		kind    string
		value   int64
		members int
		want    int
	}{
		{QuotaNone, 5, 10, -1},
		{QuotaCount, 3, 10, 3},
		{QuotaCount, 0, 10, 0},
		{QuotaPercent, 50, 7, 3},
		{QuotaPercent, 100, 7, 7},
		{QuotaPercent, 150, 7, 7},
		{QuotaPercent, 10, 5, 0},
		{QuotaCount, -1, 1, 0},
	}
	for _, c := range cases {
		if got := QuotaCap(c.kind, c.value, c.members); got != c.want {
			t.Fatalf("%s %d/%d: got %d, want %d", c.kind, c.value, c.members, got, c.want)
		}
	}
	rapid.Check(t, func(rt *rapid.T) {
		members := rapid.IntRange(0, 500).Draw(rt, "members")
		a := rapid.Int64Range(0, 100).Draw(rt, "a")
		b := rapid.Int64Range(a, 100).Draw(rt, "b")
		ca, cb := QuotaCap(QuotaPercent, a, members), QuotaCap(QuotaPercent, b, members)
		if ca > cb || cb > members || ca < 0 {
			rt.Fatalf("not monotone: %d%%=%d %d%%=%d of %d", a, ca, b, cb, members)
		}
	})
}

func TestPolicy_ParseHHMM(t *testing.T) {
	for in, want := range map[string]int{"00:00": 0, "12:30": 750, "23:59": 1439, "24:00": 0, "9:00": 0, "12:60": 0, "ab:cd": 0} {
		if got := ParseHHMM(in); got != want {
			t.Fatalf("%s: got %d, want %d", in, got, want)
		}
	}
}
