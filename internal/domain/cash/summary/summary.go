package summary

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/money"
)

// MethodCash is the only method whose expected amount includes the float and the movements.
const MethodCash = "cash"

// Input is what a session collected: the float, payments and refunds summed per method, movements and (at close)
// the counted amounts per method. Methods missing from Counted count as zero.
type Input struct {
	Float        money.Cents
	Payments     map[string]money.Cents
	Refunds      map[string]money.Cents
	MovementsIn  money.Cents
	MovementsOut money.Cents
	Counted      map[string]money.Cents
}

// MethodAmount is an amount of one payment method.
type MethodAmount struct {
	Method string
	Amount money.Cents
}

// Result is the summary of a session (m5-plan §5.1, INV-55): expected per method = float (cash only) + payments −
// refunds (± movements for cash); difference = Σ counted − Σ expected.
type Result struct {
	Expected      []MethodAmount
	Counted       []MethodAmount
	ExpectedTotal money.Cents
	CountedTotal  money.Cents
	Difference    money.Cents
	Payments      money.Cents
	Refunds       money.Cents
	MovementsIn   money.Cents
	MovementsOut  money.Cents
}

// Compute returns the summary of in. Methods are listed cash first, then alphabetically; a method appears in both
// lists when it has a payment, a refund or a counted amount (cash always).
func Compute(in Input) Result {
	set := map[string]bool{MethodCash: true}
	for m := range in.Payments {
		set[m] = true
	}
	for m := range in.Refunds {
		set[m] = true
	}
	for m := range in.Counted {
		set[m] = true
	}
	methods := make([]string, 0, len(set))
	for m := range set {
		methods = append(methods, m)
	}
	slices.SortFunc(methods, compareMethods)
	r := Result{MovementsIn: in.MovementsIn, MovementsOut: in.MovementsOut}
	for _, m := range methods {
		exp := in.Payments[m] - in.Refunds[m]
		if m == MethodCash {
			exp += in.Float + in.MovementsIn - in.MovementsOut
		}
		r.Expected = append(r.Expected, MethodAmount{Method: m, Amount: exp})
		r.Counted = append(r.Counted, MethodAmount{Method: m, Amount: in.Counted[m]})
		r.ExpectedTotal += exp
		r.CountedTotal += in.Counted[m]
		r.Payments += in.Payments[m]
		r.Refunds += in.Refunds[m]
	}
	r.Difference = r.CountedTotal - r.ExpectedTotal
	return r
}

// ExpectedCash is the cash expected in the drawer: float + cash payments − cash refunds + movements in − out. A
// withdrawal above it is refused (cash_insufficient).
func ExpectedCash(in Input) money.Cents {
	return in.Float + in.Payments[MethodCash] - in.Refunds[MethodCash] + in.MovementsIn - in.MovementsOut
}

func compareMethods(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == MethodCash:
		return -1
	case b == MethodCash:
		return 1
	}
	return cmp.Compare(a, b)
}

// VATRow is an amount of one VAT rate or nature (from the payments' frozen vat_split).
type VATRow struct {
	RateBP int64
	Nature string
	Gross  money.Cents
	Net    money.Cents
	VAT    money.Cents
}

// VATTotals sums rows per (rate, nature), sorted by rate descending then nature (as the pricing VAT summary). A
// row with a gross amount and no net/VAT split is split with money.VATSplit, so net + vat = gross per bucket.
func VATTotals(rows []VATRow) []VATRow {
	idx := map[[2]any]int{}
	var out []VATRow
	for _, r := range rows {
		if r.Net == 0 && r.VAT == 0 && r.Gross != 0 {
			r.Net, r.VAT = money.VATSplit(r.Gross, r.RateBP)
		}
		k := [2]any{r.RateBP, r.Nature}
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			out = append(out, VATRow{RateBP: r.RateBP, Nature: r.Nature})
			i = len(out) - 1
		}
		out[i].Gross += r.Gross
		out[i].Net += r.Net
		out[i].VAT += r.VAT
	}
	slices.SortFunc(out, func(a, b VATRow) int {
		if c := cmp.Compare(b.RateBP, a.RateBP); c != 0 {
			return c
		}
		return cmp.Compare(a.Nature, b.Nature)
	})
	return out
}
