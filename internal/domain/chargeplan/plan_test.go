package chargeplan

import (
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

var testRates = []struct {
	bp     int64
	nature string
}{{2200, ""}, {1000, ""}, {400, ""}, {0, "N1"}, {0, "N4"}}

// genBreakdown draws a breakdown of 1..30 lines with mixed rates, natures and net/gross pricing.
func genBreakdown(t *rapid.T) pricing.Breakdown {
	n := rapid.IntRange(1, 30).Draw(t, "lines")
	b := pricing.Breakdown{}
	d := calendar.MustParse("2027-01-01")
	for i := range n {
		r := testRates[rapid.IntRange(0, len(testRates)-1).Draw(t, "rate")]
		gross := money.Cents(rapid.Int64Range(1, 5_000_000).Draw(t, "gross"))
		net, vat := money.VATSplit(gross, r.bp)
		kind := pricing.LineService
		if i == 0 || rapid.Bool().Draw(t, "berth") {
			kind = pricing.LineBerth
		}
		b.Lines = append(b.Lines, pricing.Line{
			Seq: i + 1, Kind: kind, ItemID: "item", ItemCode: "I", Qty: qty.Milli(rapid.Int64Range(1, 400_000).Draw(t, "qty")),
			Gross: gross, Net: net, VAT: vat, VATRateBP: r.bp, VATNature: r.nature,
			PriceIncludesVAT: rapid.Bool().Draw(t, "incl"), ServiceFrom: d, ServiceTo: d.AddDays(30),
		})
		b.Total += gross
	}
	b.VATSummary = Totals(linesOf(b))
	return b
}

func linesOf(b pricing.Breakdown) []Line {
	out := make([]Line, 0, len(b.Lines))
	for _, l := range b.Lines {
		out = append(out, FromBreakdownLine(l))
	}
	return out
}

func berthKeys(b pricing.Breakdown) []string {
	var keys []string
	for _, l := range b.Lines {
		if l.Kind == pricing.LineBerth {
			keys = append(keys, SnapKey(l.Seq))
		}
	}
	return keys
}

func charge(parts []Part) []ChargedLine {
	out := make([]ChargedLine, 0, len(parts))
	for _, p := range parts {
		l := p.Line
		out = append(out, ChargedLine{Key: l.Key, GrossCents: p.Gross, VATCents: p.VAT, Line: &l})
	}
	return out
}

// assertMatches checks Σ charged = target per key and per (rate, nature).
func assertMatches(t interface {
	Fatalf(string, ...any)
}, target Target, charged []ChargedLine,
) {
	byKey := map[string][2]money.Cents{}
	var lines []Line
	for _, c := range charged {
		v := byKey[c.Key]
		v[0] += c.GrossCents
		v[1] += c.VATCents
		byKey[c.Key] = v
		l := *c.Line
		l.Gross, l.VAT, l.Net = c.GrossCents, c.VATCents, c.GrossCents-c.VATCents
		lines = append(lines, l)
	}
	for _, l := range target.Lines {
		if got := byKey[l.Key]; got[0] != l.Gross || got[1] != l.VAT {
			t.Fatalf("line %s: charged gross %d vat %d, target %d %d", l.Key, got[0], got[1], l.Gross, l.VAT)
		}
		delete(byKey, l.Key)
	}
	for k, v := range byKey {
		if v[0] != 0 || v[1] != 0 {
			t.Fatalf("key %s not in target but charged %v", k, v)
		}
	}
	if got, want := nonZero(Totals(lines)), nonZero(Totals(target.Lines)); !reflect.DeepEqual(got, want) {
		t.Fatalf("totals %+v, want %+v", got, want)
	}
}

func nonZero(b []VATBucket) []VATBucket {
	out := b[:0:0]
	for _, x := range b {
		if x.Gross != 0 || x.VAT != 0 || x.Net != 0 {
			out = append(out, x)
		}
	}
	return out
}

func TestChargePlan_TargetEqualsSnapshotWithoutAdjustments(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := genBreakdown(t)
		tg, err := ContractTarget(b, nil, b.Total)
		if err != nil {
			t.Fatal(err)
		}
		if got := Totals(tg.Lines); !reflect.DeepEqual(got, b.VATSummary) {
			t.Fatalf("totals %+v, want %+v", got, b.VATSummary)
		}
		if _, err := ContractTarget(b, nil, b.Total+1); !errors.Is(err, ErrTargetMismatch) {
			t.Fatalf("mismatch not detected: %v", err)
		}
	})
}

func TestChargePlan_SplitSumsToTarget(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := genBreakdown(t)
		total := b.Total
		var adj []Adjustment
		target, err := ContractTarget(b, adj, total)
		if err != nil {
			t.Fatal(err)
		}
		n := rapid.IntRange(1, 24).Draw(t, "instalments")
		weights := make([]int64, n)
		for i := range weights {
			weights[i] = rapid.Int64Range(1, 100).Draw(t, "w")
		}
		plan, err := money.Allocate(total, weights)
		if err != nil {
			t.Fatal(err)
		}
		var charged []ChargedLine
		var issued money.Cents
		for k := range n {
			if k > 0 && rapid.IntRange(0, 4).Draw(t, "adjust?") == 0 {
				a := genAdjustment(t, b, total)
				if total+a.DeltaCents >= 0 {
					adj = append(adj, a)
					total += a.DeltaCents
					target, err = ContractTarget(b, adj, total)
					if err != nil {
						t.Fatal(err)
					}
					left := total - issued
					if left < 0 {
						left = 0
					}
					rest, err := money.Allocate(left, weights[k:])
					if err != nil {
						t.Fatal(err)
					}
					copy(plan[k:], rest)
				}
			}
			rem := Remaining(target, charged)
			pos := money.Cents(0)
			for _, r := range rem {
				if r.Gross > 0 {
					pos += r.Gross
				}
			}
			parts, err := Split(rem, plan[k])
			if err != nil {
				t.Fatal(err)
			}
			var sum money.Cents
			for _, p := range parts {
				if p.Net+p.VAT != p.Gross {
					t.Fatalf("part net+vat != gross: %+v", p)
				}
				sum += p.Gross
				if plan[k] <= pos {
					for _, r := range rem {
						if r.Line.Key == p.Line.Key && p.Gross > r.Gross {
							t.Fatalf("part %d above remainder %d", p.Gross, r.Gross)
						}
					}
				}
			}
			if sum != plan[k] {
				t.Fatalf("parts sum %d, instalment %d", sum, plan[k])
			}
			charged = append(charged, charge(parts)...)
			issued += plan[k]
		}
		// True-up at the end.
		charged = append(charged, charge(All(Remaining(target, charged)))...)
		assertMatches(t, target, charged)
	})
}

func genAdjustment(t *rapid.T, b pricing.Breakdown, total money.Cents) Adjustment {
	kinds := []string{AdjustMove, AdjustMoveUndo, AdjustVesselChange, AdjustEarlyPayment, AdjustAddendum, AdjustServiceAdd}
	k := kinds[rapid.IntRange(0, len(kinds)-1).Draw(t, "kind")]
	delta := money.Cents(rapid.Int64Range(-int64(total)/4, 1_000_000).Draw(t, "delta"))
	a := Adjustment{Kind: k, DeltaCents: delta, AddendumID: rapid.StringMatching(`[a-z]{6}`).Draw(t, "add")}
	switch k {
	case AdjustMove, AdjustMoveUndo, AdjustVesselChange:
		a.LineKeys = berthKeys(b)
	case AdjustServiceAdd:
		if delta <= 0 {
			a.DeltaCents = -delta + 1
		}
		r := testRates[rapid.IntRange(0, len(testRates)-1).Draw(t, "addrate")]
		a.Added = &Line{ItemID: "svc", ItemCode: "S", Qty: 1000, VATRateBP: r.bp, VATNature: r.nature, PriceIncludesVAT: true}
		a.Added.Net, a.Added.VAT = money.VATSplit(a.DeltaCents, r.bp)
	}
	return a
}

func TestChargePlan_VATCumulativeNeverDrifts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := testRates[rapid.IntRange(0, len(testRates)-1).Draw(t, "rate")]
		gross := money.Cents(rapid.Int64Range(1, 10_000_000).Draw(t, "gross"))
		net, vat := money.VATSplit(gross, r.bp)
		target := Target{Lines: []Line{{Key: "snap:1", Gross: gross, Net: net, VAT: vat, VATRateBP: r.bp, VATNature: r.nature, Qty: 1000}}}
		var charged []ChargedLine
		var cum money.Cents
		for cum < gross {
			amount := min(money.Cents(rapid.Int64Range(1, int64(gross)).Draw(t, "amount")), gross-cum)
			parts, err := Split(Remaining(target, charged), amount)
			if err != nil {
				t.Fatal(err)
			}
			charged = append(charged, charge(parts)...)
			cum += amount
			// The VAT charged so far is the rounded share of the line VAT: it never drifts by more than one cent.
			var vcum money.Cents
			for _, c := range charged {
				vcum += c.VATCents
			}
			if want := vat.MulRat(int64(cum), int64(gross)); vcum != want {
				t.Fatalf("cumulative VAT %d, want %d", vcum, want)
			}
		}
		assertMatches(t, target, charged)
	})
}

// TestChargePlan_Scenario: an annual contract, deposit 30 % + 3 quarters, mixed 22 %/10 %/N4, an early payment
// discount, a repriced move after the second instalment and a service added by addendum.
func TestChargePlan_Scenario(t *testing.T) {
	mk := func(seq int, kind string, gross money.Cents, bp int64, nature string) pricing.Line {
		net, vat := money.VATSplit(gross, bp)
		return pricing.Line{Seq: seq, Kind: kind, ItemID: "i", ItemCode: "C", Qty: 1000, Gross: gross, Net: net, VAT: vat, VATRateBP: bp, VATNature: nature, PriceIncludesVAT: true}
	}
	b := pricing.Breakdown{Lines: []pricing.Line{
		mk(1, pricing.LineBerth, 610000, 2200, ""),
		mk(2, pricing.LineService, 55000, 1000, ""),
		mk(3, pricing.LineService, 12000, 0, "N4"),
		mk(4, pricing.LineIncluded, 0, 2200, ""),
	}}
	for _, l := range b.Lines {
		b.Total += l.Gross
	}
	b.VATSummary = Totals(linesOf(b))
	tests := []struct {
		name string
		adj  []Adjustment // adjustment applied after instalment 2
	}{
		{name: "plain"},
		{name: "early payment", adj: []Adjustment{{Kind: AdjustEarlyPayment, DeltaCents: -33850}}},
		{name: "move reprice", adj: []Adjustment{{Kind: AdjustMove, DeltaCents: 41234, LineKeys: []string{"snap:1"}}}},
		{name: "service added", adj: []Adjustment{{Kind: AdjustServiceAdd, AddendumID: "a1", DeltaCents: 9999, Added: &Line{ItemID: "s", ItemCode: "S", Qty: 1000, VATRateBP: 1000, PriceIncludesVAT: true}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total := b.Total
			target, err := ContractTarget(b, nil, total)
			if err != nil {
				t.Fatal(err)
			}
			if len(target.Lines) != 3 {
				t.Fatalf("included line must not be a target line: %d", len(target.Lines))
			}
			plan, _ := money.Allocate(total, []int64{30, 70, 0 + 70, 70}) // deposit 30 % then 3 equal quarters
			var charged []ChargedLine
			var issued money.Cents
			for k, amount := range plan {
				if k == 2 && tc.adj != nil {
					for _, a := range tc.adj {
						total += a.DeltaCents
					}
					target, err = ContractTarget(b, tc.adj, total)
					if err != nil {
						t.Fatal(err)
					}
					rest, _ := money.Allocate(total-issued, []int64{1, 1})
					copy(plan[2:], rest)
					amount = plan[k]
				}
				parts, err := Split(Remaining(target, charged), amount)
				if err != nil {
					t.Fatal(err)
				}
				charged = append(charged, charge(parts)...)
				issued += amount
			}
			if tu := All(Remaining(target, charged)); len(tu) != 0 {
				t.Fatalf("no true-up expected when instalments sum to the total: %+v", tu)
			}
			assertMatches(t, target, charged)
			if tc.adj == nil && !reflect.DeepEqual(Totals(target.Lines), b.VATSummary) {
				t.Fatal("unadjusted target differs from the VAT summary")
			}
		})
	}
}

func TestChargePlan_RemovedKeyBecomesNegative(t *testing.T) {
	l := Line{Key: "snap:9", ItemCode: "X", VATRateBP: 2200, Qty: 1000, PriceIncludesVAT: true}
	rem := Remaining(Target{}, []ChargedLine{{Key: "snap:9", GrossCents: 1220, VATCents: 220, Line: &l}})
	parts := All(rem)
	if len(parts) != 1 || parts[0].Gross != -1220 || parts[0].VAT != -220 || parts[0].Net != -1000 || parts[0].Qty != -1000 || parts[0].Unit != 1220 {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestChargePlan_SplitExcessOverAllLines(t *testing.T) {
	target := Target{Lines: []Line{{Key: "a", Gross: 100, Net: 82, VAT: 18, VATRateBP: 2200, Qty: 1000}, {Key: "b", Gross: 100, Net: 100, VATNature: "N1", Qty: 1000}}}
	charged := []ChargedLine{{Key: "a", GrossCents: 100, VATCents: 18, Line: &target.Lines[0]}, {Key: "b", GrossCents: 100, Line: &target.Lines[1]}}
	parts, err := Split(Remaining(target, charged), 50)
	if err != nil {
		t.Fatal(err)
	}
	var sum money.Cents
	for _, p := range parts {
		sum += p.Gross
	}
	if sum != 50 {
		t.Fatalf("excess must still be charged: %+v", parts)
	}
	// The true-up gives it back exactly.
	charged = append(charged, charge(parts)...)
	charged = append(charged, charge(All(Remaining(target, charged)))...)
	assertMatches(t, target, charged)
}
