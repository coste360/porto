package pricing

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

func TestAddMonths_Clamp(t *testing.T) {
	for _, tc := range []struct {
		from string
		k    int
		want string
	}{
		{"2027-01-31", 1, "2027-02-28"},
		{"2028-01-31", 1, "2028-02-29"},
		{"2027-01-31", 2, "2027-03-31"},
		{"2027-03-31", 1, "2027-04-30"},
		{"2027-12-31", 2, "2028-02-29"},
		{"2027-12-15", 1, "2028-01-15"},
		{"2027-05-10", 12, "2028-05-10"},
		{"2027-03-31", -1, "2027-02-28"},
		{"2027-01-15", -1, "2026-12-15"},
		{"2027-01-15", -13, "2025-12-15"},
		{"2027-08-31", 0, "2027-08-31"},
	} {
		if got := AddMonths(d(tc.from), tc.k); got != d(tc.want) {
			t.Errorf("AddMonths(%s, %d) = %s, want %s", tc.from, tc.k, got, tc.want)
		}
	}
	for _, tc := range []struct{ from, want string }{
		{"2028-02-29", "2029-02-28"}, {"2027-02-28", "2028-02-28"}, {"2027-07-01", "2028-07-01"},
	} {
		if got := AddYears(d(tc.from), 1); got != d(tc.want) {
			t.Errorf("AddYears(%s, 1) = %s, want %s", tc.from, got, tc.want)
		}
	}
}

func TestLengthRounding_Table(t *testing.T) {
	for _, tc := range []struct {
		l, step geo.Cm
		mode    string
		want    geo.Cm
	}{
		{1234, 10, LengthRoundUp, 1240},
		{1240, 10, LengthRoundUp, 1240},
		{1234, 10, LengthRoundHalfEven, 1230},
		{1235, 10, LengthRoundHalfEven, 1240},
		{1225, 10, LengthRoundHalfEven, 1220},
		{1234, 1, LengthRoundUp, 1234},
		{1201, 50, LengthRoundUp, 1250},
		{1225, 50, LengthRoundHalfEven, 1200},
		{1275, 50, LengthRoundHalfEven, 1300},
		{1201, 100, LengthRoundUp, 1300},
		{1250, 100, LengthRoundHalfEven, 1200},
		{1350, 100, LengthRoundHalfEven, 1400},
		{0, 10, LengthRoundUp, 0},
		{7, 0, LengthRoundUp, 7}, // step below 1 means 1 cm
	} {
		if got := roundLength(tc.l, LengthRounding{StepCm: tc.step, Mode: tc.mode}); got != tc.want {
			t.Errorf("round(%d, %d, %s) = %d, want %d", tc.l, tc.step, tc.mode, got, tc.want)
		}
	}
}

// TestBestCombination_TieBreak: equal cost → fewer blocks → longer first unit.
func TestBestCombination_TieBreak(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "best"})
	flat := func(id string, seq int64, u TimeUnit, a money.Cents) Rule {
		return Rule{ID: id, Seq: seq, Stage: StageBase, Active: true, ItemID: "trn", Basis: BasisFlat, Unit: u, AmountCents: &a}
	}
	units := func(bd Breakdown) string {
		s := ""
		for _, b := range bd.Lines[0].Blocks {
			s += string(b.Unit)[:1]
		}
		return s
	}
	for _, tc := range []struct {
		name   string
		rules  []Rule
		nights int
		want   string
		total  money.Cents
	}{
		// 9 nights: week + 2 nights = 2 nights + week = 21000 + 8000; 3 blocks either way; the week comes first.
		{"longer_unit_first", []Rule{flat("n", 1, UnitNight, 4000), flat("w", 2, UnitWeek, 21000)}, 9, "wnn", 29000},
		// 7 nights at 3000 = 21000 = one week: one block beats seven.
		{"fewer_blocks", []Rule{flat("n", 1, UnitNight, 3000), flat("w", 2, UnitWeek, 21000)}, 7, "w", 21000},
		// June: month 60000 = two weeks (overshoot) 2 x 30000: one block.
		{"month_over_weeks", []Rule{flat("n", 1, UnitNight, 9000), flat("w", 2, UnitWeek, 30000), flat("m", 3, UnitMonth, 60000)}, 14, "m", 60000},
		// cheaper always wins regardless of block count
		{"cost_first", []Rule{flat("n", 1, UnitNight, 1000), flat("w", 2, UnitWeek, 7001)}, 7, "nnnnnnn", 7000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cat
			c.Rules = tc.rules
			bd, err := Quote(Input{ContractType: ContractTransit, Arrival: d("2027-06-01"), Departure: d("2027-06-01").AddDays(tc.nights)}, c)
			if err != nil {
				t.Fatal(err)
			}
			if got := units(bd); got != tc.want || bd.Total != tc.total {
				t.Fatalf("blocks %s total %d, want %s %d", got, bd.Total, tc.want, tc.total)
			}
		})
	}
}

func TestQuote_Preconditions(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "per-berth"})
	ok := Input{ContractType: ContractTransit, Arrival: d("2027-05-01"), Departure: d("2027-05-02"), Resource: Resource{Category: "A"}}
	for _, tc := range []struct {
		name string
		edit func(*Input, *Catalog)
		want string
	}{
		{"departure_equal", func(in *Input, _ *Catalog) { in.Departure = in.Arrival }, "invalid_input:departure_before_arrival"},
		{"departure_before", func(in *Input, _ *Catalog) { in.Departure = in.Arrival.AddDays(-3) }, "invalid_input:departure_before_arrival"},
		{"too_many_nights", func(in *Input, _ *Catalog) { in.Departure = in.Arrival.AddDays(MaxNights + 1) }, "invalid_input:too_many_nights"},
		{"invalid_channel", func(in *Input, _ *Catalog) { in.Channel = "fax" }, "invalid_input:invalid_channel"},
		{"unknown_item", func(in *Input, _ *Catalog) { in.ItemID = "nope" }, "invalid_input:unknown_item"},
		{"no_primary_item", func(_ *Input, c *Catalog) { c.PriceList.Settings.PrimaryItems = nil }, "no_price:no_primary_item"},
		{"negative_length", func(in *Input, _ *Catalog) { in.Vessel = &Vessel{LOA: -1} }, "invalid_input:dimensions_required"},
		{"max_nights_ok", func(in *Input, _ *Catalog) { in.Departure = in.Arrival.AddDays(MaxNights) }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, c := ok, cat
			c.PriceList.Settings.PrimaryItems = map[ContractType]string{ContractTransit: "trn", ContractAnnual: "orm"}
			tc.edit(&in, &c)
			_, err := Quote(in, c)
			if got := ErrorCode(err); got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
	var np *NoPriceError
	_, err := Quote(Input{ContractType: ContractTransit, Arrival: d("2027-05-01"), Departure: d("2027-05-02"), Resource: Resource{Category: "Z"}}, cat)
	if !errors.As(err, &np) || np.ItemID != "trn" || np.Night != d("2027-05-01") || np.Unit != UnitNight || np.Category != "Z" {
		t.Fatalf("no price error %+v", err)
	}
}

func TestQuote_TraceAndHash(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "modifiers"})
	in := Input{ContractType: ContractTransit, Arrival: d("2027-07-10"), Departure: d("2027-07-12"), Vessel: &Vessel{LOA: 1200, Beam: 400, HullType: "catamaran"}, QuotedOn: d("2027-06-01")}
	bd, err := Quote(in, cat)
	if err != nil {
		t.Fatal(err)
	}
	if bd.CatalogHash != CatalogHash(cat) || len(bd.CatalogHash) != 64 || bd.EngineVersion != EngineVersion || bd.PriceListID != "pl-mod" {
		t.Fatalf("header %+v", bd)
	}
	for _, s := range bd.Trace {
		if !isTraceCode(s.Code) {
			t.Errorf("unknown trace code %q", s.Code)
		}
	}
	// Changing a rule changes the hash; reordering does not.
	other := cat
	other.Rules = append([]Rule(nil), cat.Rules...)
	other.Rules[0], other.Rules[1] = other.Rules[1], other.Rules[0]
	if CatalogHash(other) != bd.CatalogHash {
		t.Error("hash depends on rule order")
	}
	other.Rules[0].Priority++
	if CatalogHash(other) == bd.CatalogHash {
		t.Error("hash ignores a rule change")
	}
	b, _ := json.Marshal(bd)
	if len(b) == 0 {
		t.Fatal("empty")
	}
}

func isTraceCode(c string) bool {
	for _, x := range TraceCodes() {
		if x == c {
			return true
		}
	}
	return false
}

// FuzzQuote: no panic, and errors only from the documented set.
func FuzzQuote(f *testing.F) {
	f.Add(int64(1200), int64(400), int64(10), int64(750), int64(2200), int64(-1500), 3, 5, "A", "SUMMER")
	f.Add(int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), 0, 0, "", "")
	f.Add(int64(-5), int64(1<<62), int64(-7), int64(1<<62), int64(1<<40), int64(-1<<62), 3700, 40, "Z", "x")
	cat := buildCatalog(f, corpusCase{Catalog: "modifiers"})
	f.Fuzz(func(t *testing.T, loa, beam, step, unit, vat, pct int64, nights, offset int, category, promo string) {
		c := cat
		c.Items = append([]Item(nil), cat.Items...)
		c.Items[0].VATRateBP = vat
		c.Rules = append([]Rule(nil), cat.Rules...)
		u := money.Cents(unit)
		c.Rules = append(c.Rules,
			Rule{ID: "f-pm", Seq: 90, Stage: StageBase, Active: true, Priority: 3, ItemID: "trn", Basis: BasisPerM2, Unit: UnitNight, UnitCents: &u},
			Rule{ID: "f-mod", Seq: 91, Stage: StageModifier, Active: true, Stackable: pct%2 == 0, Effect: Effect{PercentBP: &pct}},
			Rule{ID: "f-dsc", Seq: 92, Stage: StageDiscount, Active: true, DiscountKind: DiscountPromo, PromoCode: "SUMMER", Effect: Effect{AmountCents: &u}},
		)
		c.PriceList.Settings.LengthRounding = LengthRounding{StepCm: geo.Cm(step), Mode: LengthRoundHalfEven}
		arrival := calendar.New(2027, 1, 1).AddDays(offset % 400)
		in := Input{
			ContractType: ContractTransit, Arrival: arrival, Departure: arrival.AddDays(nights % 4000),
			Vessel: &Vessel{LOA: geo.Cm(loa), Beam: geo.Cm(beam), HullType: "catamaran"}, Resource: Resource{Category: category, Length: geo.Cm(loa / 2)},
			PromoCode: promo, QuotedOn: d("2026-12-01"),
		}
		bd, err := Quote(in, c)
		if err != nil {
			if !documentedError(err) {
				t.Fatalf("undocumented error %v", err)
			}
			return
		}
		if bd.Net+bd.VAT != bd.Total {
			t.Fatalf("net + vat != total")
		}
	})
}

func BenchmarkQuote_Transit30Nights(b *testing.B) {
	cat := buildCatalog(b, corpusCase{Catalog: "modifiers"})
	in := Input{
		ContractType: ContractTransit, Arrival: d("2027-06-20"), Departure: d("2027-07-20"), Vessel: &Vessel{LOA: 1250, Beam: 420, HullType: "catamaran"},
		Resource: Resource{Length: 1200}, QuotedOn: d("2027-06-01"), Channel: ChannelOnline,
	}
	for b.Loop() {
		if _, err := Quote(in, cat); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMatrix_8x5(b *testing.B) {
	cat := buildCatalog(b, corpusCase{Catalog: "periods"})
	var bands []Band
	amounts := map[string]money.Cents{}
	for i := range 8 {
		code := "L" + string(rune('A'+i))
		bands = append(bands, Band{Code: code, MaxCm: ptr(geo.Cm(600 + 200*i))})
		amounts[code] = money.Cents(3000 + 500*i)
	}
	cat.PriceList.Bands = bands
	cat.Rules = append(cat.Rules, Rule{ID: "r-lb", Seq: 50, Stage: StageBase, Active: true, Priority: 2, ItemID: "trn", Basis: BasisLengthBand, Unit: UnitNight, Amounts: amounts})
	spec := MatrixSpec{ContractType: ContractTransit, QuotedOn: d("2026-12-01")}
	for b.Loop() {
		m, err := Matrix(cat, spec)
		if err != nil || len(m.Cells) != 40 {
			b.Fatalf("%v %d", err, len(m.Cells))
		}
	}
}
