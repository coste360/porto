package account

import (
	"errors"
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

const (
	wtAF   = "wt-antifouling"
	wtProp = "wt-propulsion"
)

var vat22 = VAT{RateBP: 2200}

// worked is the order of m10-plan §4.4 (budget) with the actuals of §9.4.
func worked(mode string) Input {
	line := func(id, item, typ, qual string, q qty.Milli, unit, cost money.Cents) BudgetLine {
		net, _ := q.MulCents(unit)
		return BudgetLine{
			LineID: id, ItemID: item, ItemType: typ, WorkTypeID: wtAF, QualificationID: qual, Qty: q, Unit: unit, Net: net,
			VAT: net.PercentBP(2200), Gross: net + net.PercentBP(2200), UnitCost: cost,
		}
	}
	budget := []BudgetLine{
		line("l1", "SRV-ALAG", "service", "", 12000, 1800, 0),
		line("l2", "SRV-LAV", "service", "", 56100, 400, 100),
		line("l3", "LAB-HAND", "labour", "hand", 8400, 3800, 0),
		line("l4", "LAB-PAINT", "labour", "painter", 5600, 4500, 0),
		line("l5", "MAT-AVEG", "material", "", 5000, 9500, 5800),
		line("l6", "SRV-VARO", "service", "", 12000, 1800, 0),
	}
	vat := map[string]VAT{}
	for _, b := range budget {
		vat[b.ItemID] = vat22
	}
	vat["EXT"] = vat22
	return Input{
		Mode:   mode,
		Budget: budget,
		Rates: RateSnapshot{
			Units:         map[string]money.Cents{"SRV-ALAG": 1800, "SRV-LAV": 400, "LAB-HAND": 3800, "LAB-PAINT": 4500, "MAT-AVEG": 9500, "SRV-VARO": 1800},
			LabourItems:   map[string]string{"hand": "LAB-HAND", "painter": "LAB-PAINT"},
			OrderWorkType: wtAF,
		},
		Entries: []ApprovedEntry{
			{ID: "e1", QualificationID: "hand", WorkTypeID: wtAF, Hours: 5000, CostCents: 11000},
			{ID: "e2", QualificationID: "hand", WorkTypeID: wtAF, Hours: 4000, CostCents: 9600},
			{ID: "e3", QualificationID: "painter", WorkTypeID: wtAF, Hours: 5500, CostCents: 14300},
		},
		Movements: []Movement{
			{ID: "m1", ItemID: "MAT-AVEG", WorkTypeID: wtAF, Qty: 6000, UnitCost: 5800, UnitPrice: 9500},
			{ID: "m2", ItemID: "MAT-AVEG", WorkTypeID: wtAF, Return: true, Qty: 1000, UnitCost: 5800, UnitPrice: 9500},
		},
		External: []ExternalCost{{ID: "x1", ItemID: "EXT", WorkTypeID: wtProp, Cost: 15000, Billable: true, Price: 18000}},
		ItemVAT:  vat,
	}
}

// TestAccount_Worked: the final account of m10-plan §9.4, every number.
func TestAccount_Worked(t *testing.T) {
	a, err := Build(worked(ModeActual))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][5]int64{ // qty, revenue, cost, margin, vat
		Key("SRV-ALAG", wtAF):  {12000, 21600, 0, 21600, 4752},
		Key("SRV-LAV", wtAF):   {56100, 22440, 5610, 16830, 4937},
		Key("LAB-HAND", wtAF):  {9000, 34200, 20600, 13600, 7524},
		Key("LAB-PAINT", wtAF): {5500, 24750, 14300, 10450, 5445},
		Key("MAT-AVEG", wtAF):  {5000, 47500, 29000, 18500, 10450},
		Key("SRV-VARO", wtAF):  {12000, 21600, 0, 21600, 4752},
		ExternalKey("x1"):      {1000, 18000, 15000, 3000, 3960},
	}
	if len(a.Lines) != len(want) {
		t.Fatalf("lines %d", len(a.Lines))
	}
	for _, l := range a.Lines {
		w, ok := want[l.Key]
		if !ok {
			t.Fatalf("unexpected line %s", l.Key)
		}
		got := [5]int64{int64(l.Qty), int64(l.Revenue), int64(l.Cost), int64(l.Margin), int64(l.VATCents)}
		if got != w {
			t.Errorf("%s = %v, want %v", l.Key, got, w)
		}
	}
	tt := a.Totals
	if tt.Revenue != 190090 || tt.Cost != 84510 || tt.Margin != 105580 || tt.MarginBP == nil || *tt.MarginBP != 5554 {
		t.Fatalf("totals %+v", tt)
	}
	if tt.VATCents != 41820 || tt.Gross != 231910 || tt.HoursLabour != 14500 {
		t.Fatalf("vat %d gross %d hours %d", tt.VATCents, tt.Gross, tt.HoursLabour)
	}
	if a.QuoteNet != 170260 || a.DeltaNet != 19830 {
		t.Fatalf("quote %d delta %d", a.QuoteNet, a.DeltaNet)
	}
	if len(a.ByWorkType) != 2 {
		t.Fatalf("by work type %+v", a.ByWorkType)
	}
	af, pr := a.ByWorkType[0], a.ByWorkType[1]
	if af.WorkTypeID != wtAF || af.Revenue != 172090 || af.Cost != 69510 || af.Margin != 102580 {
		t.Fatalf("antifouling %+v", af)
	}
	if pr.WorkTypeID != wtProp || pr.Revenue != 18000 || pr.Cost != 15000 || pr.Margin != 3000 {
		t.Fatalf("propulsion %+v", pr)
	}
	for _, l := range a.Lines {
		switch l.Key {
		case Key("LAB-HAND", wtAF):
			if l.DeltaNet != 2280 {
				t.Errorf("hand delta %d", l.DeltaNet)
			}
		case Key("LAB-PAINT", wtAF):
			if l.DeltaNet != -450 {
				t.Errorf("painter delta %d", l.DeltaNet)
			}
		}
	}
	// Billing: advance 30 % of the quote gross at acceptance, then the final remainder.
	budgetTarget := BudgetTarget(worked(ModeActual).Budget, worked(ModeActual).ItemVAT, wtAF)
	if TargetGross(budgetTarget) != 207717 {
		t.Fatalf("budget gross %d", TargetGross(budgetTarget))
	}
	adv, err := Partial(budgetTarget, nil, 62315)
	if err != nil || SumParts(adv) != 62315 {
		t.Fatalf("advance %v %d", err, SumParts(adv))
	}
	final := Final(a.Target, charged(adv))
	if SumParts(final) != 169595 {
		t.Fatalf("final %d", SumParts(final))
	}
	if SumParts(adv)+SumParts(final) != 231910 {
		t.Fatal("Σ billings")
	}
}

func charged(parts ...[]chargeplan.Part) []chargeplan.ChargedLine {
	var out []chargeplan.ChargedLine
	for _, ps := range parts {
		for _, p := range ps {
			l := p.Line
			out = append(out, chargeplan.ChargedLine{Key: p.Line.Key, GrossCents: p.Gross, VATCents: p.VAT, Line: &l})
		}
	}
	return out
}

// TestAccount_Modes: fixed and progress revenue is the budget plus billable external costs (D16), unless
// fixed_includes_external.
func TestAccount_Modes(t *testing.T) {
	in := worked(ModeFixed)
	a, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Totals.Revenue != 170260+18000 || a.Totals.Cost != 84510 {
		t.Fatalf("fixed %+v", a.Totals)
	}
	in.FixedIncludesExternal = true
	if a, _ = Build(in); a.Totals.Revenue != 170260 || a.DeltaNet != 0 {
		t.Fatalf("fixed incl. external %+v delta %d", a.Totals, a.DeltaNet)
	}
	in.Mode = "other"
	if _, err := Build(in); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("mode %v", err)
	}
}

func TestAccount_Completion(t *testing.T) {
	cases := []struct {
		p    []int64
		h    []qty.Milli
		want int64
	}{
		{nil, nil, 0},
		{[]int64{10000, 0}, []qty.Milli{8400, 5600}, 6000},
		{[]int64{5000, 10000}, []qty.Milli{0, 0}, 7500},
		{[]int64{3333, 3333, 3334}, []qty.Milli{1000, 1000, 1000}, 3333},
		{[]int64{1}, []qty.Milli{2000}, 1},
		{[]int64{5000, 0}, []qty.Milli{1, 2}, 1667},
	}
	for _, c := range cases {
		got, err := Completion(c.p, c.h)
		if err != nil || got != c.want {
			t.Errorf("Completion(%v, %v) = %d %v, want %d", c.p, c.h, got, err, c.want)
		}
	}
	if _, err := Completion([]int64{1}, nil); err == nil {
		t.Error("length mismatch")
	}
	if _, err := Completion([]int64{10001}, []qty.Milli{1}); err == nil {
		t.Error("range")
	}
}

// genInput draws a random order: budget lines, entries, movements (returns never above issues) and external costs.
func genInput(t *rapid.T) Input {
	mode := rapid.SampledFrom([]string{ModeActual, ModeFixed, ModeProgress}).Draw(t, "mode")
	wts := []string{"wa", "wb", "wc"}
	items := []struct{ id, typ string }{{"s1", "service"}, {"s2", "service"}, {"l1", "labour"}, {"l2", "labour"}, {"m1", "material"}, {"m2", "product"}}
	in := Input{
		Mode: mode, FixedIncludesExternal: rapid.Bool().Draw(t, "incl"),
		Rates:   RateSnapshot{Units: map[string]money.Cents{}, LabourItems: map[string]string{"q1": "l1", "q2": "l2"}, OrderWorkType: "wa"},
		ItemVAT: map[string]VAT{}, ItemCost: map[string]money.Cents{},
	}
	for _, it := range items {
		in.Rates.Units[it.id] = money.Cents(rapid.Int64Range(0, 20000).Draw(t, "unit"+it.id))
		in.ItemVAT[it.id] = VAT{RateBP: rapid.SampledFrom([]int64{0, 1000, 2200}).Draw(t, "vat"+it.id), PriceIncludesVAT: rapid.Bool().Draw(t, "inc"+it.id)}
		in.ItemCost[it.id] = money.Cents(rapid.Int64Range(0, 10000).Draw(t, "cost"+it.id))
	}
	in.ItemVAT["x"] = VAT{RateBP: 2200}
	nb := rapid.IntRange(0, 6).Draw(t, "nb")
	for i := range nb {
		it := rapid.SampledFrom(items).Draw(t, "bitem")
		q := qty.Milli(rapid.Int64Range(0, 50000).Draw(t, "bq"))
		unit := in.Rates.Units[it.id]
		amount, _ := q.MulCents(unit)
		disc := money.Cents(rapid.Int64Range(0, int64(amount)).Draw(t, "disc"))
		net, vat := vatOf(amount-disc, in.ItemVAT[it.id])
		b := BudgetLine{
			LineID: fmt.Sprint(i), ItemID: it.id, ItemType: it.typ, WorkTypeID: rapid.SampledFrom(wts).Draw(t, "bwt"),
			Qty: q, Unit: unit, Discount: disc, Net: net, VAT: vat, Gross: net + vat, UnitCost: in.ItemCost[it.id],
		}
		if it.typ == "service" && rapid.Bool().Draw(t, "consumed") {
			c := qty.Milli(rapid.Int64Range(0, 50000).Draw(t, "cq"))
			b.ConsumedQty = &c
		}
		in.Budget = append(in.Budget, b)
	}
	ne := rapid.IntRange(0, 8).Draw(t, "ne")
	for i := range ne {
		in.Entries = append(in.Entries, ApprovedEntry{
			ID: fmt.Sprint(i), QualificationID: rapid.SampledFrom([]string{"q1", "q2", "q3"}).Draw(t, "eq"),
			WorkTypeID: rapid.SampledFrom(append(wts, "")).Draw(t, "ewt"), Hours: qty.Milli(rapid.Int64Range(0, 16000).Draw(t, "eh")),
			CostCents: money.Cents(rapid.Int64Range(0, 50000).Draw(t, "ec")),
		})
	}
	issued := map[string]qty.Milli{}
	nm := rapid.IntRange(0, 8).Draw(t, "nm")
	for i := range nm {
		item := rapid.SampledFrom([]string{"m1", "m2"}).Draw(t, "mitem")
		wt := rapid.SampledFrom(wts).Draw(t, "mwt")
		ret := rapid.Bool().Draw(t, "ret") && issued[item+wt] > 0
		q := qty.Milli(rapid.Int64Range(1, 20000).Draw(t, "mq"))
		if ret {
			q = min(q, issued[item+wt])
			issued[item+wt] -= q
		} else {
			issued[item+wt] += q
		}
		in.Movements = append(in.Movements, Movement{
			ID: fmt.Sprint(i), ItemID: item, WorkTypeID: wt, Return: ret, Qty: q,
			UnitCost: money.Cents(rapid.Int64Range(0, 10000).Draw(t, "muc")), UnitPrice: money.Cents(rapid.Int64Range(0, 20000).Draw(t, "mup")),
		})
	}
	nx := rapid.IntRange(0, 3).Draw(t, "nx")
	for i := range nx {
		in.External = append(in.External, ExternalCost{
			ID: fmt.Sprint(i), ItemID: "x", WorkTypeID: rapid.SampledFrom(append(wts, "")).Draw(t, "xwt"),
			Cost: money.Cents(rapid.Int64Range(1, 100000).Draw(t, "xc")), Billable: rapid.Bool().Draw(t, "xb"),
			Price: money.Cents(rapid.Int64Range(0, 120000).Draw(t, "xp")),
		})
	}
	return in
}

// TestRapid_AccountReconciles: the reconciliation equations 1-5 and 7 of m10-plan §9.3 (INV-93).
func TestRapid_AccountReconciles(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t)
		a, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		tt := a.Totals
		var rev, cost, margin, vat, delta money.Cents
		for _, l := range a.Lines {
			rev += l.Revenue
			cost += l.Cost
			margin += l.Margin
			vat += l.VATCents
			delta += l.DeltaNet
			if l.Margin != l.Revenue-l.Cost || l.Gross != l.Revenue+l.VATCents {
				t.Fatalf("line %+v", l)
			}
		}
		// 1, 2: Σ lines = Σ sources = Σ work types.
		if rev != tt.Revenue || tt.Revenue != tt.RevenueLabour+tt.RevenueMaterial+tt.RevenueService+tt.RevenueExt {
			t.Fatalf("revenue %d %+v", rev, tt)
		}
		if cost != tt.Cost || tt.Cost != tt.CostLabour+tt.CostMaterial+tt.CostService+tt.CostExt {
			t.Fatalf("cost %d %+v", cost, tt)
		}
		if margin != tt.Margin || vat != tt.VATCents || tt.Gross != tt.Revenue+tt.VATCents {
			t.Fatal("margin or vat")
		}
		var wr, wc, wm money.Cents
		for _, w := range a.ByWorkType {
			wr += w.Revenue
			wc += w.Cost
			wm += w.Margin
		}
		if wr != tt.Revenue || wc != tt.Cost || wm != tt.Margin {
			t.Fatal("work types")
		}
		// 3: hours and labour cost are the exact sums of the approved entries.
		var hours qty.Milli
		var lc money.Cents
		for _, e := range in.Entries {
			hours += e.Hours
			lc += e.CostCents
		}
		if tt.HoursLabour != hours || tt.CostLabour != lc {
			t.Fatalf("labour %d %d", tt.HoursLabour, tt.CostLabour)
		}
		// 4: material quantities and costs from the signed movements.
		mq := map[string]qty.Milli{}
		var mc money.Cents
		for _, m := range in.Movements {
			q := m.Qty
			if m.Return {
				q = -q
			}
			mq[Key(m.ItemID, m.WorkTypeID)] += q
			c, _ := q.MulCents(m.UnitCost)
			mc += c
		}
		for _, l := range a.Lines {
			if l.Source == SourceMaterial && l.Qty != mq[l.Key] {
				t.Fatalf("material qty %s %d %d", l.Key, l.Qty, mq[l.Key])
			}
		}
		if tt.CostMaterial != mc {
			t.Fatalf("material cost %d %d", tt.CostMaterial, mc)
		}
		// 5: external cost and revenue.
		var xc, xr money.Cents
		for _, x := range in.External {
			xc += x.Cost
			if x.Billable && (in.Mode == ModeActual || !in.FixedIncludesExternal) {
				p, _ := vatOf(x.Price, in.ItemVAT["x"])
				xr += p
			}
		}
		if tt.CostExt != xc || tt.RevenueExt != xr {
			t.Fatalf("external %d %d / %d %d", tt.CostExt, xc, tt.RevenueExt, xr)
		}
		// 7: Σ deltas = revenue − quote net.
		if delta != tt.Revenue-a.QuoteNet || a.DeltaNet != delta {
			t.Fatalf("delta %d %d", delta, tt.Revenue-a.QuoteNet)
		}
	})
}

// TestRapid_BillingSequenceSums: random advance and progress amounts then the final billing: Σ gross = target gross,
// per VAT bucket net and VAT equal to the target, no partial part above its remainder (INV-94).
func TestRapid_BillingSequenceSums(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput(t)
		a, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		budget := BudgetTarget(in.Budget, in.ItemVAT, in.Rates.OrderWorkType)
		var done []chargeplan.ChargedLine
		steps := rapid.IntRange(0, 4).Draw(t, "steps")
		for i := range steps {
			target := budget
			if i > 0 && rapid.Bool().Draw(t, "account") {
				target = a.Target
			}
			rem := chargeplan.Remaining(PlanTarget(target), done)
			pos := PositiveRemaining(rem)
			if pos <= 0 {
				continue
			}
			amount := money.Cents(rapid.Int64Range(1, int64(pos)).Draw(t, "amount"))
			parts, err := Partial(target, done, amount)
			if err != nil {
				t.Fatal(err)
			}
			if SumParts(parts) != amount {
				t.Fatalf("parts %d amount %d", SumParts(parts), amount)
			}
			byKey := map[string]money.Cents{}
			for _, r := range rem {
				byKey[r.Line.Key] = r.Gross
			}
			for _, p := range parts {
				if p.Gross < 0 || p.Gross > byKey[p.Line.Key] {
					t.Fatalf("part %s %d above remainder %d", p.Line.Key, p.Gross, byKey[p.Line.Key])
				}
			}
			done = append(done, charged(parts)...)
			if _, err := Partial(target, done, PositiveRemaining(chargeplan.Remaining(PlanTarget(target), done))+1); err == nil {
				t.Fatal("amount above remaining accepted")
			}
		}
		final := Final(a.Target, done)
		done = append(done, charged(final)...)
		var gross money.Cents
		for _, c := range done {
			gross += c.GrossCents
		}
		if gross != TargetGross(a.Target) {
			t.Fatalf("Σ gross %d target %d", gross, TargetGross(a.Target))
		}
		// Per VAT bucket: Σ charged net and VAT = target.
		type b struct{ rate int64 }
		want, got := map[b][2]money.Cents{}, map[b][2]money.Cents{}
		for _, l := range a.Target {
			w := want[b{l.VAT.RateBP}]
			want[b{l.VAT.RateBP}] = [2]money.Cents{w[0] + l.Net, w[1] + l.VATCents}
		}
		for _, c := range done {
			k := b{c.Line.VATRateBP}
			g := got[k]
			got[k] = [2]money.Cents{g[0] + c.GrossCents - c.VATCents, g[1] + c.VATCents}
		}
		for k, w := range want {
			if got[k] != w {
				t.Fatalf("bucket %v got %v want %v", k, got[k], w)
			}
		}
		for k, g := range got {
			if _, ok := want[k]; !ok && g != ([2]money.Cents{}) {
				t.Fatalf("extra bucket %v %v", k, g)
			}
		}
		// A second final has nothing left.
		if len(Final(a.Target, done)) != 0 {
			t.Fatal("second final not empty")
		}
	})
}
