package price

import (
	"encoding/json"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

func num(s string) VariableValue { return VariableValue{Kind: KindNumber, Num: qty.MustParse(s)} }

func centsP(c int64) *money.Cents {
	v := money.Cents(c)
	return &v
}

func milliP(s string) *qty.Milli {
	v := qty.MustParse(s)
	return &v
}

func bpP(v int64) *int64 { return &v }

var vat22 = ItemVAT{RateBP: 2200}

// antifouling is the worked example of m10-plan §4.4: package ANTIVEG v1 on a 12 m sailing boat, layers = 2.
func antifouling() Snapshot {
	items := map[string]ItemData{
		"alag":  {ItemID: "alag", Code: "SRV-ALAG", ItemType: "service", Unit: "m", VAT: vat22},
		"lav":   {ItemID: "lav", Code: "SRV-LAV", ItemType: "service", Unit: "m2", VAT: vat22, CostCents: 100},
		"hand":  {ItemID: "hand", Code: "LAB-HAND", ItemType: "labour", Unit: "hour", VAT: vat22, CostCents: 2200, QualificationID: "q-hand"},
		"paint": {ItemID: "paint", Code: "LAB-PAINT", ItemType: "labour", Unit: "hour", VAT: vat22, CostCents: 2600, QualificationID: "q-painter"},
		"aveg":  {ItemID: "aveg", Code: "MAT-AVEG", ItemType: "material", Unit: "piece", VAT: vat22, CostCents: 5800},
		"varo":  {ItemID: "varo", Code: "SRV-VARO", ItemType: "service", Unit: "m", VAT: vat22},
	}
	prices := map[string]UnitPrice{}
	for id, c := range map[string]int64{"alag": 1800, "lav": 400, "hand": 3800, "paint": 4500, "aveg": 9500, "varo": 1800} {
		prices[id] = UnitPrice{ItemID: id, UnitCents: money.Cents(c), PriceListID: "pl", RuleID: "r-" + id, Found: true}
	}
	pkg := func(seq int, item, qf, pf string) LineSpec {
		return LineSpec{
			Seq: seq, Source: SourcePackage, PackageID: "antiveg", PackageVersion: 1, PackageLineSeq: seq, ItemID: item,
			WorkTypeID: "wt-af", QtyFormula: qf, PriceFormula: pf,
		}
	}
	return Snapshot{
		LanguageVersion: 1, QuotedOn: "2027-03-01",
		Vessel: map[string]VariableValue{
			"loa": num("12"), "beam": num("4"), "draft": num("1.5"), "hull_type": {Kind: KindEnum, Enum: "monohull_sail"},
		},
		JobInputs: map[string]VariableValue{"layers": num("2")},
		Derived:   []DerivedSnapshot{{Name: "hull_area", Src: "loa * (beam + draft) * 0.85"}},
		Lines: []LineSpec{
			pkg(1, "alag", "loa", `if(hull_type == "catamaran", list_price * 1.5, list_price)`),
			pkg(2, "lav", "round(hull_area, 1)", ""),
			pkg(3, "hand", "round(hull_area * 0.15, 1)", ""),
			pkg(4, "paint", "round(hull_area * layers * 0.05, 1)", ""),
			pkg(5, "aveg", "ceil(hull_area * layers / 25)", ""),
			pkg(6, "varo", "loa", ""),
		},
		Prices: prices, Items: items, DepositBP: bpP(3000),
	}
}

func TestQuoteLines_Antifouling12m(t *testing.T) {
	lines, tot, err := Compute(antifouling())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		qty           string
		unit, net, vt int64
	}{
		{"12", 1800, 21600, 4752},
		{"56.1", 400, 22440, 4937},
		{"8.4", 3800, 31920, 7022},
		{"5.6", 4500, 25200, 5544},
		{"5", 9500, 47500, 10450},
		{"12", 1800, 21600, 4752},
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines", len(lines))
	}
	for i, w := range want {
		l := lines[i]
		if l.Qty != qty.MustParse(w.qty) || int64(l.Unit) != w.unit || int64(l.Net) != w.net || int64(l.VAT) != w.vt {
			t.Fatalf("line %d: %+v", i+1, l)
		}
		if l.QtyOverridden || l.PriceOverridden || len(l.Issues) > 0 {
			t.Fatalf("line %d marked or with issues: %+v", i+1, l)
		}
	}
	if tot.Net != 170260 || tot.VAT != 37457 || tot.Gross != 207717 || tot.Deposit != 62315 || tot.Blocking {
		t.Fatalf("totals %+v", tot)
	}
	if tot.EstimatedHours["q-hand"] != qty.MustParse("8.4") || tot.EstimatedHours["q-painter"] != qty.MustParse("5.6") {
		t.Fatalf("hours %+v", tot.EstimatedHours)
	}
	if len(tot.Buckets) != 1 || tot.Buckets[0].Net != 170260 || tot.Buckets[0].VAT != 37457 {
		t.Fatalf("buckets %+v", tot.Buckets)
	}
	if lines[2].EstimatedHours != qty.MustParse("8.4") || lines[0].EstimatedHours != 0 || lines[2].UnitCost != 2200 {
		t.Fatalf("labour line %+v", lines[2])
	}

	// layers = 3: painter 8.4 h = 378.00, tins ceil(6.732) = 7 = 665.00.
	s := antifouling()
	s.JobInputs["layers"] = num("3")
	lines, _, err = Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if lines[3].Qty != qty.MustParse("8.4") || lines[3].Net != 37800 || lines[4].Qty != qty.MustParse("7") || lines[4].Net != 66500 {
		t.Fatalf("layers 3: %+v %+v", lines[3], lines[4])
	}

	// Catamaran: haul-out unit 27.00, line 324.00.
	s = antifouling()
	s.Vessel["hull_type"] = VariableValue{Kind: KindEnum, Enum: "catamaran"}
	lines, _, _ = Compute(s)
	if lines[0].Unit != 2700 || lines[0].Net != 32400 || lines[0].PriceOverridden {
		t.Fatalf("catamaran: %+v", lines[0])
	}
}

func TestQuoteLines_OverridesMarked(t *testing.T) {
	s := antifouling()
	s.Overrides = []Override{
		{Seq: 2, Qty: milliP("60")},
		{Seq: 3, Unit: centsP(4000)},
		{Seq: 4, Discount: centsP(1000)},
		{Seq: 6, Remove: true},
		{Seq: 1, Description: func() *string { d := "Alaggio con gru"; return &d }()},
	}
	lines, tot, err := Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("%d lines", len(lines))
	}
	if !lines[1].QtyOverridden || lines[1].PriceOverridden || lines[1].Net != 24000 {
		t.Fatalf("qty override %+v", lines[1])
	}
	if lines[2].QtyOverridden || !lines[2].PriceOverridden || lines[2].Net != 33600 {
		t.Fatalf("unit override %+v", lines[2])
	}
	if !lines[3].PriceOverridden || lines[3].Discount != 1000 || lines[3].Net != 24200 {
		t.Fatalf("discount %+v", lines[3])
	}
	if lines[0].QtyOverridden || lines[0].PriceOverridden || lines[0].Spec.Description != "Alaggio con gru" {
		t.Fatalf("description %+v", lines[0])
	}
	var sum money.Cents
	for _, l := range lines {
		sum += l.Gross
	}
	if sum != tot.Gross {
		t.Fatal("totals are not the sum of lines")
	}
}

func TestQuoteLines_IssuesAndManual(t *testing.T) {
	s := antifouling()
	delete(s.Vessel, "draft") // missing vessel data: hull_area cannot be computed
	delete(s.Prices, "varo")
	s.Lines = append(s.Lines, LineSpec{Seq: 7, Source: SourceManual, ItemID: "lav", WorkTypeID: "wt-af", ManualQty: milliP("2"), ManualUnit: centsP(1500)})
	lines, tot, err := Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if !tot.Blocking {
		t.Fatal("not blocking")
	}
	if len(lines[1].Issues) != 1 || lines[1].Issues[0].Code != IssueMissingVariable || lines[1].Issues[0].Ref != "hull_area" {
		t.Fatalf("missing variable %+v", lines[1].Issues)
	}
	if len(lines[5].Issues) != 1 || lines[5].Issues[0].Code != IssueNoPrice {
		t.Fatalf("no price %+v", lines[5].Issues)
	}
	if lines[6].Net != 3000 || len(lines[6].Issues) > 0 {
		t.Fatalf("manual %+v", lines[6])
	}
	// The override of the unit price clears no_price; the vessel override clears missing_variable.
	s.Overrides = []Override{{Seq: 6, Unit: centsP(1800)}}
	s.VesselOverrides = map[string]VariableValue{"draft": num("1.5")}
	_, tot, _ = Compute(s)
	if tot.Blocking {
		t.Fatal("still blocking")
	}
}

func TestQuoteLines_GrossPricesAndDeposit(t *testing.T) {
	s := Snapshot{
		Lines:  []LineSpec{{Seq: 1, Source: SourceItem, ItemID: "g"}},
		Items:  map[string]ItemData{"g": {ItemID: "g", ItemType: "product", VAT: ItemVAT{RateBP: 2200, PriceIncludesVAT: true}}},
		Prices: map[string]UnitPrice{"g": {ItemID: "g", UnitCents: 12200, PriceIncludesVAT: true, Found: true}},
	}
	lines, tot, err := Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Qty != qty.One || lines[0].Gross != 12200 || lines[0].Net != 10000 || lines[0].VAT != 2200 {
		t.Fatalf("gross line %+v", lines[0])
	}
	if tot.Deposit != 0 {
		t.Fatal("deposit without policy")
	}
	if Deposit(10000, nil, centsP(20000)) != 10000 || Deposit(10000, bpP(3333), nil) != 3333 || Deposit(0, bpP(3000), nil) != 0 {
		t.Fatal("deposit")
	}
	// Nature-coded zero rate in its own bucket.
	s.Lines = append(s.Lines, LineSpec{Seq: 2, Source: SourceItem, ItemID: "e"})
	s.Items["e"] = ItemData{ItemID: "e", ItemType: "fee", VAT: ItemVAT{RateBP: 0, Nature: "N4"}}
	s.Prices["e"] = UnitPrice{ItemID: "e", UnitCents: 500, Found: true}
	_, tot, _ = Compute(s)
	if len(tot.Buckets) != 2 || tot.Buckets[1].Nature != "N4" || tot.Buckets[1].Net != 500 {
		t.Fatalf("buckets %+v", tot.Buckets)
	}
}

// TestRapid_QuoteRecompute: random packages and vessels; recompute of the JSON round trip of the snapshot equals
// the computed lines, and totals are the sum of the lines (INV-28).
func TestRapid_QuoteRecompute(t *testing.T) {
	formulas := []string{
		"loa", "round(hull_area, 1)", "round(hull_area * 0.15, 1)", "ceil(hull_area * layers / 25)", "beam * 2",
		"if(hull_type == \"catamaran\", 2, 1)", "max(loa - 10, 0)", "1 / 3 * 3", "draft * 0.333", "",
		"band(loa, fees)",
	}
	priceFormulas := []string{"", "list_price * 1.1", "if(qty > 10, list_price * 0.9, list_price)", "loa * 2.5"}
	rapid.Check(t, func(rt *rapid.T) {
		s := antifouling()
		s.Tables = []TableSnapshot{{Name: "fees", Rows: []TableRowSnapshot{{UpTo: milliP("10"), Value: qty.MustParse("1")}, {Value: qty.MustParse("2")}}}}
		s.Vessel["loa"] = VariableValue{Kind: KindNumber, Num: qty.Milli(rapid.Int64Range(300, 40000).Draw(rt, "loa") * 10)}
		s.Vessel["beam"] = VariableValue{Kind: KindNumber, Num: qty.Milli(rapid.Int64Range(100, 1200).Draw(rt, "beam") * 10)}
		if rapid.Bool().Draw(rt, "has_draft") {
			s.Vessel["draft"] = VariableValue{Kind: KindNumber, Num: qty.Milli(rapid.Int64Range(30, 500).Draw(rt, "draft") * 10)}
		} else {
			delete(s.Vessel, "draft")
		}
		s.Vessel["hull_type"] = VariableValue{Kind: KindEnum, Enum: rapid.SampledFrom([]string{"monohull_sail", "catamaran"}).Draw(rt, "hull")}
		s.JobInputs["layers"] = num(rapid.SampledFrom([]string{"1", "2", "3", "4"}).Draw(rt, "layers"))
		ids := []string{"alag", "lav", "hand", "paint", "aveg", "varo"}
		n := rapid.IntRange(1, 12).Draw(rt, "n")
		s.Lines = nil
		for i := range n {
			s.Lines = append(s.Lines, LineSpec{
				Seq: i + 1, Source: SourcePackage, PackageID: "p", PackageVersion: 1, PackageLineSeq: i + 1,
				ItemID: rapid.SampledFrom(ids).Draw(rt, "item"), WorkTypeID: "wt",
				QtyFormula: rapid.SampledFrom(formulas).Draw(rt, "qf"), PriceFormula: rapid.SampledFrom(priceFormulas).Draw(rt, "pf"),
			})
			if rapid.IntRange(0, 4).Draw(rt, "ov") == 0 {
				s.Overrides = append(s.Overrides, Override{Seq: i + 1, Discount: centsP(rapid.Int64Range(0, 5000).Draw(rt, "disc"))})
			}
		}
		if rapid.Bool().Draw(rt, "fixed") {
			s.DepositBP, s.DepositCents = nil, centsP(rapid.Int64Range(0, 500000).Draw(rt, "dep"))
		}
		lines, tot, err := Compute(s)
		if err != nil {
			rt.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			rt.Fatal(err)
		}
		var back Snapshot
		if err := json.Unmarshal(raw, &back); err != nil {
			rt.Fatal(err)
		}
		lines2, tot2, err := Recompute(back)
		if err != nil {
			rt.Fatal(err)
		}
		if !reflect.DeepEqual(lines, lines2) || !reflect.DeepEqual(tot, tot2) {
			rt.Fatalf("recompute differs:\n%+v\n%+v", lines, lines2)
		}
		var net, vt, gross money.Cents
		for _, l := range lines {
			net, vt, gross = net+l.Net, vt+l.VAT, gross+l.Gross
			if l.Net+l.VAT != l.Gross || l.Net < 0 {
				rt.Fatalf("line %+v", l)
			}
		}
		if net != tot.Net || vt != tot.VAT || gross != tot.Gross || tot.Deposit > tot.Gross || tot.Deposit < 0 {
			rt.Fatalf("totals %+v", tot)
		}
	})
}
