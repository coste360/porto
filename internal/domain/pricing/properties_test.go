package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

func mustJSON(t interface{ Fatalf(string, ...any) }, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// documentedError reports whether err is one of the documented engine errors.
func documentedError(err error) bool {
	for _, s := range []error{ErrNoPrice, ErrInvalidInput, ErrPromoNotApplicable, ErrOverflow} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// quoteOK quotes and skips the draw when there is no price; any undocumented error fails.
func quoteOK(t *rapid.T, in Input, cat Catalog) (Breakdown, bool) {
	bd, err := Quote(in, cat)
	if err != nil {
		if !documentedError(err) {
			t.Fatalf("undocumented error: %v", err)
		}
		return Breakdown{}, false
	}
	return bd, true
}

// INV-20: the snapshot reproduces the breakdown byte for byte after a JSON round trip.
func TestQuote_SnapshotReproducible(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		in := genInput(t, cat, 120)
		snap, err := NewSnapshot(in, cat)
		if err != nil {
			if !documentedError(err) {
				t.Fatalf("undocumented error: %v", err)
			}
			return
		}
		if snap.EngineVersion != EngineVersion || snap.Breakdown.EngineVersion != EngineVersion {
			t.Fatalf("engine version not recorded")
		}
		raw := mustJSON(t, snap)
		var back Snapshot
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		again, err := Recompute(back)
		if err != nil {
			t.Fatalf("recompute: %v", err)
		}
		if a, b := mustJSON(t, again), mustJSON(t, snap.Breakdown); !bytes.Equal(a, b) {
			t.Fatalf("recompute differs:\n%s\n%s", a, b)
		}
		if snap.Breakdown.CatalogHash != CatalogHash(back.Catalog) {
			t.Fatalf("catalog hash changed by the round trip")
		}
	})
}

func TestRapid_SplitStayAdditive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{contractTypes: []ContractType{ContractTransit}, nightOnly: true, noStayConds: true, noDiscounts: true, vatIncluded: true})
		cat.PriceList.Settings.Rounding = map[ContractType]TotalRounding{AnyContractType: RoundNone}
		cat.PriceList.Settings.Split = map[ContractType]SplitRule{AnyContractType: SplitProRataDaily}
		in := genInput(t, cat, 40)
		in.PromoCode = ""
		for i := range in.Services {
			// stay services are charged once per quote; keep only items priced per night
			_ = i
		}
		in.Services = nil
		n := in.Arrival.DaysUntil(in.Departure)
		if n < 2 {
			return
		}
		mid := in.Arrival.AddDays(rapid.IntRange(1, n-1).Draw(t, "mid"))
		if mid.After(cat.PriceList.ValidTo) {
			return
		}
		whole, ok := quoteOK(t, in, cat)
		first, second := in, in
		first.Departure, second.Arrival = mid, mid
		a, okA := quoteOK(t, first, cat)
		b, okB := quoteOK(t, second, cat)
		if ok != (okA && okB) {
			t.Fatalf("priceable whole %v, parts %v %v", ok, okA, okB)
		}
		if ok && whole.Total != a.Total+b.Total {
			t.Fatalf("whole %d != %d + %d", whole.Total, a.Total, b.Total)
		}
	})
}

func TestRapid_NoNegativeAmounts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		bd, ok := quoteOK(t, genInput(t, cat, 400), cat)
		if !ok {
			return
		}
		for _, l := range bd.Lines {
			for _, b := range l.Blocks {
				if b.Base < 0 || b.Amount < 0 {
					t.Fatalf("negative block %+v", b)
				}
			}
			if l.Amount < 0 || l.Gross < 0 || l.Net < 0 || l.VAT < 0 || l.Base < 0 {
				t.Fatalf("negative line %+v", l)
			}
		}
		if bd.Total < 0 || bd.Net < 0 || bd.VAT < 0 || bd.Deposit < 0 || bd.DiscountTotal < 0 || bd.Subtotal < 0 {
			t.Fatalf("negative totals %+v", bd)
		}
		if bd.Subtotal-bd.DiscountTotal != sumAmounts(bd) {
			t.Fatalf("subtotal %d - discounts %d != amounts %d", bd.Subtotal, bd.DiscountTotal, sumAmounts(bd))
		}
	})
}

func sumAmounts(bd Breakdown) money.Cents {
	var s money.Cents
	for _, l := range bd.Lines {
		s += l.Amount
	}
	return s
}

// INV-25: net + VAT sums to the total per line, per bucket and overall.
func TestRapid_VATSumsExactly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		bd, ok := quoteOK(t, genInput(t, cat, 400), cat)
		if !ok {
			return
		}
		var net, vat, gross, bn, bv, bg money.Cents
		for _, l := range bd.Lines {
			if l.Net+l.VAT != l.Gross {
				t.Fatalf("line net %d + vat %d != gross %d", l.Net, l.VAT, l.Gross)
			}
			net, vat, gross = net+l.Net, vat+l.VAT, gross+l.Gross
		}
		for _, b := range bd.VATSummary {
			if b.Net+b.VAT != b.Gross {
				t.Fatalf("bucket %+v", b)
			}
			bn, bv, bg = bn+b.Net, bv+b.VAT, bg+b.Gross
		}
		if gross != bd.Total || net+vat != bd.Total || bd.Net != net || bd.VAT != vat || bg != bd.Total || bn != net || bv != vat {
			t.Fatalf("sums: lines %d/%d/%d buckets %d/%d/%d total %d", net, vat, gross, bn, bv, bg, bd.Total)
		}
		if !slices.IsSortedFunc(bd.VATSummary, func(a, b VATBucket) int {
			if a.RateBP != b.RateBP {
				return int(b.RateBP - a.RateBP)
			}
			if a.Nature < b.Nature {
				return -1
			}
			if a.Nature > b.Nature {
				return 1
			}
			return 0
		}) {
			t.Fatalf("summary not sorted: %+v", bd.VATSummary)
		}
	})
}

func TestRapid_ExcludedVATNetEqualsAmount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		cat.PriceList.Settings.Rounding = map[ContractType]TotalRounding{AnyContractType: RoundNone}
		bd, ok := quoteOK(t, genInput(t, cat, 200), cat)
		if !ok {
			return
		}
		for _, l := range bd.Lines {
			if !l.PriceIncludesVAT && l.Net != l.Amount {
				t.Fatalf("excluded line net %d != amount %d (rate %d)", l.Net, l.Amount, l.VATRateBP)
			}
		}
	})
}

func shuffled[T any](t *rapid.T, label string, xs []T) []T {
	if len(xs) == 0 {
		return xs
	}
	perm := rapid.Permutation(rangeInts(len(xs))).Draw(t, label)
	out := make([]T, len(xs))
	for i, p := range perm {
		out[i] = xs[p]
	}
	return out
}

func TestRapid_RuleOrderIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		in := genInput(t, cat, 120)
		other := cat
		other.Rules = shuffled(t, "rules", cat.Rules)
		other.Items = shuffled(t, "items", cat.Items)
		other.PriceList.Periods = shuffled(t, "periods", cat.PriceList.Periods)
		a, errA := Quote(in, cat)
		b, errB := Quote(in, other)
		if ErrorCode(errA) != ErrorCode(errB) {
			t.Fatalf("errors differ: %v / %v", errA, errB)
		}
		if x, y := mustJSON(t, a), mustJSON(t, b); !bytes.Equal(x, y) {
			t.Fatalf("breakdown depends on the input order:\n%s\n%s", x, y)
		}
	})
}

func TestRapid_RoundingWithinHalfStep(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		mode := rapid.SampledFrom([]TotalRounding{RoundEuro, RoundFiveEuro}).Draw(t, "round")
		cat.PriceList.Settings.Rounding = map[ContractType]TotalRounding{AnyContractType: mode}
		bd, ok := quoteOK(t, genInput(t, cat, 200), cat)
		if !ok {
			return
		}
		step := money.Cents(roundingStep(mode))
		if bd.Total%step != 0 {
			t.Fatalf("total %d not a multiple of %d", bd.Total, step)
		}
		if r := bd.RoundingTotal; r > step/2 || r < -step/2 {
			t.Fatalf("rounding %d beyond half step %d", r, step/2)
		}
		var shares money.Cents
		for _, l := range bd.Lines {
			shares += l.RoundingShare
		}
		if shares != bd.RoundingTotal {
			t.Fatalf("shares %d != rounding total %d", shares, bd.RoundingTotal)
		}
	})
}

func TestRapid_BestCombinationNotWorse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{
			contractTypes: []ContractType{ContractTransit, ContractMonthly}, noStayConds: true, noDiscounts: true,
			basis: []Basis{BasisFlat, BasisPerMeter, BasisLengthBand}, noServices: true,
		})
		// Both contract types use the same berth item, so their rules are shared.
		for i := range cat.Rules {
			if cat.Rules[i].ItemID == berthItemID(ContractMonthly) {
				cat.Rules[i].ItemID = berthItemID(ContractTransit)
			}
		}
		cat.PriceList.Settings.PrimaryItems[ContractMonthly] = berthItemID(ContractTransit)
		cat.PriceList.Settings.Rounding = map[ContractType]TotalRounding{AnyContractType: RoundNone}
		in := genInput(t, cat, 70)
		if in.Arrival.Day() > 28 {
			return // month blocks from the arrival and from a block start only agree without end-of-month clamping
		}
		with := func(ct ContractType, split SplitRule) (Breakdown, bool) {
			c := cat
			c.PriceList.Settings.Split = map[ContractType]SplitRule{ct: split}
			i := in
			i.ContractType = ct
			return quoteOK(t, i, c)
		}
		best, ok := with(ContractTransit, SplitBestCombination)
		if nights, okN := with(ContractTransit, SplitProRataDaily); okN {
			if !ok || best.Total > nights.Total {
				t.Fatalf("transit best %d (ok %v) > all nights %d", best.Total, ok, nights.Total)
			}
		}
		bestM, okM := with(ContractMonthly, SplitBestCombination)
		if months, okW := with(ContractMonthly, SplitWholeMonths); okW {
			if !okM || bestM.Total > months.Total {
				t.Fatalf("monthly best %d (ok %v) > whole months %d", bestM.Total, okM, months.Total)
			}
		}
	})
}

func TestRapid_LengthMonotone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		basis := rapid.SampledFrom([]Basis{BasisPerMeter, BasisLengthBand}).Draw(t, "basis")
		cat := genCatalog(t, genOpts{contractTypes: []ContractType{ContractTransit}, noDiscounts: true, noModifiers: true, basis: []Basis{basis}, noServices: true})
		cat.PriceList.Settings.DimensionSource = DimensionVessel
		cat.PriceList.Settings.Rounding = map[ContractType]TotalRounding{AnyContractType: RoundNone}
		// Non-decreasing band amounts, a default above every band, no scope: one rule per unit.
		var rules []Rule
		for _, r := range cat.Rules {
			if r.Stage != StageBase || r.Unit != UnitNight {
				continue
			}
			r.Scope = Scope{}
			r.Active = true
			r.Priority = 0
			if basis == BasisLengthBand {
				var last money.Cents
				r.Amounts = map[string]money.Cents{}
				for i, b := range cat.PriceList.Bands {
					last += money.Cents(rapid.Int64Range(0, 10_000).Draw(t, "inc"+b.Code+r.ID+string(rune('a'+i))))
					r.Amounts[b.Code] = last
				}
				r.AmountCents = ptr(last + money.Cents(rapid.Int64Range(0, 10_000).Draw(t, "def"+r.ID)))
			}
			rules = append(rules, r)
			break
		}
		if len(rules) == 0 {
			return
		}
		cat.Rules = rules
		in := genInput(t, cat, 10)
		in.Services, in.PromoCode = nil, ""
		short := geo.Cm(rapid.Int64Range(100, 5000).Draw(t, "short"))
		long := short + geo.Cm(rapid.Int64Range(0, 3000).Draw(t, "delta"))
		in.Vessel = &Vessel{LOA: short, Beam: 300, HullType: "monohull_sail"}
		a, okA := quoteOK(t, in, cat)
		in.Vessel = &Vessel{LOA: long, Beam: 300, HullType: "monohull_sail"}
		b, okB := quoteOK(t, in, cat)
		if okA && okB && b.Total < a.Total {
			t.Fatalf("LOA %d pays %d, LOA %d pays %d", short, a.Total, long, b.Total)
		}
	})
}

func TestRapid_DepositAtMostTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cat := genCatalog(t, genOpts{})
		in := genInput(t, cat, 200)
		if rapid.Bool().Draw(t, "override") {
			in.Deposit = &DepositPolicy{AmountCents: cents(t, "dep", 0, 10_000_000)}
		}
		bd, ok := quoteOK(t, in, cat)
		if ok && (bd.Deposit < 0 || bd.Deposit > bd.Total) {
			t.Fatalf("deposit %d, total %d", bd.Deposit, bd.Total)
		}
	})
}
