package summary

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/money"
)

var methods = []string{"cash", "card_pos", "bank_transfer", "cheque", "other"}

func TestCashSummary_Table(t *testing.T) {
	cases := []struct {
		name     string
		in       Input
		wantCash money.Cents
		wantDiff money.Cents
	}{
		{"empty", Input{}, 0, 0},
		{"float only counted", Input{Float: 10000, Counted: map[string]money.Cents{"cash": 10000}}, 10000, 0},
		{
			"payments refunds movements, short by 2 euro",
			Input{
				Float: 5000, Payments: map[string]money.Cents{"cash": 12000, "card_pos": 3000},
				Refunds: map[string]money.Cents{"cash": 1000}, MovementsIn: 500, MovementsOut: 2000,
				Counted: map[string]money.Cents{"cash": 14300, "card_pos": 3000},
			},
			14500, -200,
		},
		{"counted card only", Input{Payments: map[string]money.Cents{"card_pos": 100}, Counted: map[string]money.Cents{"card_pos": 100}}, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Compute(c.in)
			if r.Expected[0].Method != "cash" || r.Expected[0].Amount != c.wantCash || r.Difference != c.wantDiff {
				t.Fatalf("result %+v", r)
			}
			if ExpectedCash(c.in) != c.wantCash {
				t.Fatalf("expected cash %d", ExpectedCash(c.in))
			}
		})
	}
}

func genAmounts(t *rapid.T, label string) map[string]money.Cents {
	out := map[string]money.Cents{}
	for _, m := range methods {
		if rapid.Bool().Draw(t, label+"_has_"+m) {
			out[m] = money.Cents(rapid.Int64Range(0, 10_000_000).Draw(t, label+"_"+m))
		}
	}
	return out
}

// TestCashSummary_Reconciles: expected = float + payments − refunds ± movements per method; difference = Σ counted −
// Σ expected (INV-55).
func TestCashSummary_Reconciles(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := Input{
			Float:        money.Cents(rapid.Int64Range(0, 1_000_000).Draw(t, "float")),
			Payments:     genAmounts(t, "pay"),
			Refunds:      genAmounts(t, "ref"),
			MovementsIn:  money.Cents(rapid.Int64Range(0, 1_000_000).Draw(t, "in")),
			MovementsOut: money.Cents(rapid.Int64Range(0, 1_000_000).Draw(t, "out")),
			Counted:      genAmounts(t, "cnt"),
		}
		r := Compute(in)
		var expSum, cntSum, paySum, refSum money.Cents
		for i, e := range r.Expected {
			want := in.Payments[e.Method] - in.Refunds[e.Method]
			if e.Method == MethodCash {
				want += in.Float + in.MovementsIn - in.MovementsOut
				if i != 0 {
					t.Fatalf("cash not first: %v", r.Expected)
				}
			}
			if e.Amount != want {
				t.Fatalf("expected %s = %d, want %d", e.Method, e.Amount, want)
			}
			if r.Counted[i].Method != e.Method || r.Counted[i].Amount != in.Counted[e.Method] {
				t.Fatalf("counted %v", r.Counted[i])
			}
			expSum += e.Amount
			cntSum += r.Counted[i].Amount
		}
		for _, v := range in.Payments {
			paySum += v
		}
		for _, v := range in.Refunds {
			refSum += v
		}
		if r.ExpectedTotal != expSum || r.CountedTotal != cntSum || r.Difference != cntSum-expSum {
			t.Fatalf("totals %+v", r)
		}
		if r.Payments != paySum || r.Refunds != refSum {
			t.Fatalf("payments %d/%d refunds %d/%d", r.Payments, paySum, r.Refunds, refSum)
		}
		if expSum != in.Float+paySum-refSum+in.MovementsIn-in.MovementsOut {
			t.Fatal("expected total does not reconcile")
		}
	})
}

func TestVATTotals_SumsAndSplits(t *testing.T) {
	rows := []VATRow{
		{RateBP: 2200, Gross: 12200},
		{RateBP: 1000, Gross: 1100, Net: 1000, VAT: 100},
		{RateBP: 2200, Gross: 122, Net: 100, VAT: 22},
		{RateBP: 0, Nature: "N4", Gross: 500},
	}
	got := VATTotals(rows)
	if len(got) != 3 || got[0].RateBP != 2200 || got[0].Gross != 12322 || got[0].Net != 10100 || got[0].VAT != 2222 ||
		got[2].Nature != "N4" || got[2].Net != 500 {
		t.Fatalf("totals %+v", got)
	}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		var rows []VATRow
		var gross money.Cents
		for i := range n {
			g := money.Cents(rapid.Int64Range(-100_000, 1_000_000).Draw(t, "g"))
			rate := rapid.SampledFrom([]int64{2200, 1000, 400, 0}).Draw(t, "r")
			rows = append(rows, VATRow{RateBP: rate, Gross: g})
			gross += g
			_ = i
		}
		var sum money.Cents
		for _, b := range VATTotals(rows) {
			if b.Net+b.VAT != b.Gross {
				t.Fatalf("bucket %+v", b)
			}
			sum += b.Gross
		}
		if sum != gross {
			t.Fatalf("sum %d != %d", sum, gross)
		}
	})
}
