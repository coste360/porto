package pricing

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// rentalRule is a flat base rule of a rental item.
func rentalRule(id string, item string, unit TimeUnit, cents int64, scope Scope) Rule {
	a := money.Cents(cents)
	return Rule{ID: id, Seq: 1, Stage: StageBase, Active: true, ItemID: item, Basis: BasisFlat, Unit: unit, AmountCents: &a, Scope: scope}
}

func unitRule(id, item string, cents int64) Rule {
	u := money.Cents(cents)
	return Rule{ID: id, Seq: 1, Stage: StageBase, Active: true, ItemID: item, Basis: BasisMetered, Unit: UnitStay, UnitCents: &u}
}

// nol2027 is the seed price list of m11-plan §5.3 (prices VAT 22% included; period alta = 07-01..08-31).
func nol2027() Catalog {
	alta := Scope{Periods: []string{"alta"}}
	morning := Scope{TimeBands: []string{"morning"}}
	afternoon := Scope{TimeBands: []string{"afternoon"}}
	altaMorning := Scope{Periods: []string{"alta"}, TimeBands: []string{"morning"}}
	altaAfternoon := Scope{Periods: []string{"alta"}, TimeBands: []string{"afternoon"}}
	item := func(id, code string) Item {
		return Item{ID: id, Code: code, Type: ItemService, VATRateBP: 2200, PriceIncludesVAT: true, RevenueClass: "rental", Active: true}
	}
	return Catalog{
		PriceList: PriceList{
			ID: "nol", Code: "NOL-2027", ValidFrom: calendar.MustParse("2027-01-01"), ValidTo: calendar.MustParse("2027-12-31"),
			Periods: []RatePeriod{
				{Code: "bassa", Kind: PeriodSeason, Ranges: []DateRange{{From: calendar.MustParse("2027-01-01"), To: calendar.MustParse("2027-06-30")}, {From: calendar.MustParse("2027-09-01"), To: calendar.MustParse("2027-12-31")}}},
				{Code: "alta", Kind: PeriodSeason, Ranges: []DateRange{{From: calendar.MustParse("2027-07-01"), To: calendar.MustParse("2027-08-31")}}},
			},
		},
		Items: []Item{
			item("gom", "NOL-GOM58"), item("skp", "SKIPPER"),
			item("snk", "SNORKEL"), item("frg", "FRIGO"), item("rada", "RADA"),
			item("fuel", "FUEL-P"), item("late", "LATE-GOM"),
		},
		Rules: []Rule{
			rentalRule("g-h", "gom", UnitHour, 4500, Scope{}),
			rentalRule("g-h-a", "gom", UnitHour, 5500, alta),
			rentalRule("g-m", "gom", UnitHalfDay, 15000, morning),
			rentalRule("g-m-a", "gom", UnitHalfDay, 18000, altaMorning),
			rentalRule("g-p", "gom", UnitHalfDay, 13000, afternoon),
			rentalRule("g-p-a", "gom", UnitHalfDay, 16000, altaAfternoon),
			rentalRule("g-d", "gom", UnitDay, 24000, Scope{}),
			rentalRule("g-d-a", "gom", UnitDay, 29000, alta),
			rentalRule("g-w", "gom", UnitWeek, 140000, Scope{}),
			rentalRule("s-h", "skp", UnitHour, 2500, Scope{}),
			rentalRule("s-hd", "skp", UnitHalfDay, 10000, Scope{}),
			rentalRule("s-d", "skp", UnitDay, 18000, Scope{}),
			rentalRule("s-w", "skp", UnitWeek, 100000, Scope{}),
			unitRule("x-snk", "snk", 1000), unitRule("x-frg", "frg", 500), unitRule("x-rada", "rada", 1500),
			unitRule("x-fuel", "fuel", 210), unitRule("x-late", "late", 6000),
		},
	}
}

func rd(s string) calendar.Date { return calendar.MustParse(s) }

func TestRentalQuote_Table(t *testing.T) {
	cat := nol2027()
	noWeek := nol2027()
	noWeek.Rules = withoutRule(noWeek.Rules, "g-w")
	odd := nol2027()
	odd.Rules = append(odd.Rules, Rule{ID: "g-h-odd", Seq: 9, Stage: StageBase, Active: true, Priority: 10, ItemID: "gom", Basis: BasisFlat, Unit: UnitHour, AmountCents: new(money.Cents(4501))})
	cases := []struct {
		name  string
		cats  []Catalog
		in    RentalInput
		total money.Cents
		lines []money.Cents
		err   string
	}{
		{"E1_hourly", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12"), Slots: 6}, 13500, []money.Cents{13500}, ""},
		{"E2_hourly_alta", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-07-10"), Slots: 5}, 13750, []money.Cents{13750}, ""},
		{"E3_half_day_skipper_snorkel", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "morning", Start: rd("2027-06-12"), SkipperItemID: "skp", Extras: []RentalExtra{{ItemID: "snk", Qty: qty.One}}}, 26000, []money.Cents{15000, 10000, 1000}, ""},
		{"E4_half_day_afternoon_alta", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "afternoon", Start: rd("2027-08-15")}, 16000, []money.Cents{16000}, ""},
		{"E5_multi_day_period_change", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalMultiDay, Start: rd("2027-06-29"), Days: 3, Extras: []RentalExtra{{ItemID: "frg", Qty: qty.One, PerDay: true}}}, 78500, []money.Cents{77000, 1500}, ""},
		{"E6_week_9_days", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-05"), Days: 9}, 188000, []money.Cents{188000}, ""},
		{"E7_week_13_days_cap", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-05"), Days: 13}, 280000, []money.Cents{280000}, ""},
		{"E8_half_even", []Catalog{odd}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12"), Slots: 5}, 11252, []money.Cents{11252}, ""},
		{"E9_no_week_rule", []Catalog{noWeek}, RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-05"), Days: 9}, 0, nil, "no_price"},
		{"E10_band_without_rule", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "evening", Start: rd("2027-06-12")}, 0, nil, "no_price"},
		{"day_alta", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-07-02")}, 29000, []money.Cents{29000}, ""},
		{"skipper_hourly", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12"), Slots: 4, SkipperItemID: "skp"}, 9000 + 5000, []money.Cents{9000, 5000}, ""},
		{"skipper_week", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-05"), Days: 7, SkipperItemID: "skp"}, 240000, []money.Cents{140000, 100000}, ""},
		{"extra_per_day_on_hourly_is_one_day", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12"), Slots: 2, Extras: []RentalExtra{{ItemID: "frg", Qty: qty.Milli(2000), PerDay: true}}}, 4500 + 1000, []money.Cents{4500, 1000}, ""},
		{"week_remainder_days_cheaper", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-05"), Days: 8}, 140000 + 24000, nil, ""},
		{"outside_validity", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2028-01-02")}, 0, nil, "no_price"},
		{"unknown_kind", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: "fortnight", Start: rd("2027-06-12")}, 0, nil, "invalid_input"},
		{"hourly_zero_slots", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12")}, 0, nil, "invalid_input"},
		{"extra_without_price", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12"), Extras: []RentalExtra{{ItemID: "nope", Qty: qty.One}}}, 0, nil, "no_price"},
		{"skipper_without_rule", []Catalog{cat}, RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12"), SkipperItemID: "snk"}, 0, nil, "no_price"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := RentalQuote(c.cats, c.in)
			switch c.err {
			case "no_price":
				var np *NoPriceError
				if !errors.As(err, &np) || np.Reason != ReasonNoRule {
					t.Fatalf("err = %v, want no_price no_rule", err)
				}
				return
			case "invalid_input":
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("err = %v, want invalid input", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if b.Total != c.total {
				t.Fatalf("total = %d, want %d (%+v)", b.Total, c.total, b.Lines)
			}
			if b.EngineVersion != RentalEngineVersion {
				t.Fatalf("engine version %d", b.EngineVersion)
			}
			if c.lines != nil {
				if len(b.Lines) != len(c.lines) {
					t.Fatalf("lines = %+v", b.Lines)
				}
				for i, l := range b.Lines {
					if l.Amount != c.lines[i] || l.Seq != i+1 {
						t.Fatalf("line %d = %+v, want %d", i, l, c.lines[i])
					}
				}
			}
		})
	}
}

func withoutRule(rules []Rule, id string) []Rule {
	out := []Rule{}
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}

func TestRentalQuote_LineDetails(t *testing.T) {
	b, err := RentalQuote([]Catalog{nol2027()}, RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "morning", Start: rd("2027-06-12"), SkipperItemID: "skp", Extras: []RentalExtra{{ItemID: "snk", Qty: qty.One}}})
	if err != nil {
		t.Fatal(err)
	}
	u := b.Lines[0]
	if u.Kind != RentalLineUnit || u.ItemCode != "NOL-GOM58" || u.Qty != qty.One || u.UnitCents != 15000 || !u.PriceIncludesVAT || u.RevenueClass != "rental" || !reflect.DeepEqual(u.RuleIDs, []string{"g-m"}) {
		t.Fatalf("unit line %+v", u)
	}
	// E3: the 150.00 line splits into net 122.95 and VAT 27.05 (charge.CreateTx does it per line).
	if net, vat := money.VATSplit(u.Amount, 2200); net != 12295 || vat != 2705 {
		t.Fatalf("vat split %d %d", net, vat)
	}
	if b.Lines[1].Kind != RentalLineSkipper || b.Lines[1].ItemCode != "SKIPPER" || b.Lines[2].Kind != RentalLineExtra || b.Lines[2].ItemCode != "SNORKEL" {
		t.Fatalf("lines %+v", b.Lines)
	}
	h, _ := RentalQuote([]Catalog{nol2027()}, RentalInput{ItemID: "gom", Kind: RentalHourly, Start: rd("2027-06-12"), Slots: 5})
	if h.Lines[0].Qty != 2500 || h.Lines[0].UnitCents != 4500 {
		t.Fatalf("hourly line %+v", h.Lines[0])
	}
	m, _ := RentalQuote([]Catalog{nol2027()}, RentalInput{ItemID: "gom", Kind: RentalMultiDay, Start: rd("2027-06-29"), Days: 3})
	if m.Lines[0].Qty != 3000 || !reflect.DeepEqual(m.Lines[0].RuleIDs, []string{"g-d", "g-d-a"}) {
		t.Fatalf("multi-day line %+v", m.Lines[0])
	}
}

func TestRentalQuote_Scopes(t *testing.T) {
	base := func(extra ...Rule) Catalog {
		c := nol2027()
		c.Rules = append(c.Rules, extra...)
		return c
	}
	vip := rentalRule("g-d-vip", "gom", UnitDay, 20000, Scope{CustomerCategories: []string{"vip"}})
	online := rentalRule("g-d-web", "gom", UnitDay, 22000, Scope{Channels: []Channel{ChannelOnline}})
	broad := rentalRule("g-m-any", "gom", UnitHalfDay, 9900, Scope{})
	ct := rentalRule("g-d-ct", "gom", UnitDay, 100, Scope{ContractTypes: []ContractType{ContractTransit}})
	ct.Priority = 1
	cases := []struct {
		name string
		cat  Catalog
		in   RentalInput
		want money.Cents
	}{
		{"customer_category_matches", base(vip), RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12"), CustomerCategory: "vip"}, 20000},
		{"customer_category_other", base(vip), RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12"), CustomerCategory: "std"}, 24000},
		{"channel_online", base(online), RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12"), Channel: ChannelOnline}, 22000},
		{"channel_default_desk", base(online), RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12")}, 24000},
		{"time_band_more_specific", base(broad), RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "morning", Start: rd("2027-06-12")}, 15000},
		{"unscoped_half_day_for_other_band", base(broad), RentalInput{ItemID: "gom", Kind: RentalHalfDay, Band: "evening", Start: rd("2027-06-12")}, 9900},
		{"contract_type_scope_ignored", base(ct), RentalInput{ItemID: "gom", Kind: RentalDay, Start: rd("2027-06-12")}, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := RentalQuote([]Catalog{c.cat}, c.in)
			if err != nil {
				t.Fatal(err)
			}
			if b.Total != c.want {
				t.Fatalf("total %d, want %d", b.Total, c.want)
			}
		})
	}
}

func TestRentalQuote_Recompute(t *testing.T) {
	in := RentalInput{ItemID: "gom", Kind: RentalMultiDay, Start: rd("2027-06-29"), Days: 3, SkipperItemID: "skp", Extras: []RentalExtra{{ItemID: "frg", Qty: qty.One, PerDay: true}}}
	cat := nol2027()
	first, err := RentalQuote([]Catalog{cat}, in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first)
	var stored RentalBreakdown
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	again, _ := RentalQuote([]Catalog{cat}, in)
	if !reflect.DeepEqual(stored, again) || stored.Total != again.Total {
		t.Fatalf("recompute differs:\n%+v\n%+v", stored, again)
	}
}

func TestRentalQuote_Purity(t *testing.T) {
	in := RentalInput{ItemID: "gom", Kind: RentalWeek, Start: rd("2027-06-27"), Days: 10, SkipperItemID: "skp", Extras: []RentalExtra{{ItemID: "snk", Qty: qty.One}}}
	want, err := RentalQuote([]Catalog{nol2027()}, in)
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic permutations (no randomness in a pure package): every rotation of the rules, reversed or not.
	base := nol2027()
	for k := range 2 * len(base.Rules) {
		cat := nol2027()
		n := len(cat.Rules)
		rules := make([]Rule, n)
		for i := range n {
			rules[i] = cat.Rules[(i+k)%n]
		}
		if k >= n {
			for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
				rules[i], rules[j] = rules[j], rules[i]
			}
			for i, j := 0, len(cat.Items)-1; i < j; i, j = i+1, j-1 {
				cat.Items[i], cat.Items[j] = cat.Items[j], cat.Items[i]
			}
		}
		cat.Rules = rules
		got, err := RentalQuote([]Catalog{cat}, in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != want.Total || len(got.Lines) != len(want.Lines) {
			t.Fatalf("order dependent: %+v vs %+v", got, want)
		}
	}
}

func TestRentalQuote_QuoteIgnoresRentalRules(t *testing.T) {
	// A catalogue JSON without time bands stays byte-identical (INV-26) and Scope.specificity of mooring rules is
	// unchanged.
	raw, _ := json.Marshal(Scope{Periods: []string{"alta"}})
	if string(raw) != `{"periods":["alta"]}` {
		t.Fatalf("scope json %s", raw)
	}
	if (Scope{TimeBands: []string{"morning"}, Periods: []string{"x"}}).specificity() != 2 {
		t.Fatal("time bands add one to specificity")
	}
}

func TestValidate_RentalRules(t *testing.T) {
	cat := nol2027()
	cat.Items = append(cat.Items, Item{ID: "berth", Code: "B", Type: ItemBerthFee, VATRateBP: 2200, Active: true})
	if issues := Validate(cat); HasErrors(issues) {
		t.Fatalf("seed list has errors: %+v", issues)
	}
	codes := func(r Rule) []string {
		c := nol2027()
		c.Items = append(c.Items, Item{ID: "berth", Code: "B", Type: ItemBerthFee, VATRateBP: 2200, Active: true})
		c.Rules = append(c.Rules, r)
		var out []string
		for _, i := range Validate(c) {
			if i.Severity == SeverityError && i.Ref.ID == r.ID {
				out = append(out, i.Code+":"+i.Ref.Field)
			}
		}
		return out
	}
	u := money.Cents(100)
	cases := []struct {
		name string
		rule Rule
		want []string
	}{
		{"hour_on_non_rental_item", rentalRule("x1", "berth", UnitHour, 100, Scope{}), []string{"rule_unit_invalid:unit"}},
		{"hour_not_flat", Rule{ID: "x2", Stage: StageBase, Active: true, ItemID: "gom", Basis: BasisMetered, Unit: UnitHour, UnitCents: &u}, []string{"rule_basis_invalid:basis"}},
		{"time_bands_on_day", rentalRule("x3", "gom", UnitDay, 100, Scope{TimeBands: []string{"morning"}}), []string{"rule_unit_invalid:scope.time_bands"}},
		{"week_rental_not_flat", Rule{ID: "x4", Stage: StageBase, Active: true, ItemID: "gom", Basis: BasisMetered, Unit: UnitWeek, UnitCents: &u}, []string{"rule_basis_invalid:basis"}},
		{"half_day_with_bands_ok", rentalRule("x5", "gom", UnitHalfDay, 100, Scope{TimeBands: []string{"evening"}}), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codes(c.rule); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("issues %v, want %v", got, c.want)
			}
		})
	}
}
