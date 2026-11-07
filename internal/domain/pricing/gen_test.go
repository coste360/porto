package pricing

import (
	"fmt"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Generators of random catalogs and inputs for the property tests.

var (
	genValidFrom = calendar.New(2027, 1, 1)
	genValidTo   = calendar.New(2027, 12, 31)
	genCats      = []string{"A", "B", "C"}
	genHulls     = []string{"monohull_sail", "monohull_motor", "catamaran", "trimaran"}
	genCustCats  = []string{"member", "resident", "affiliated", "charter", "yard"}
)

// genOpts restricts the random catalogs for properties that need it.
type genOpts struct {
	contractTypes []ContractType // nil = random subset
	nightOnly     bool           // berth rules use the night unit only
	noStayConds   bool           // no min/max nights and no lead conditions
	noDiscounts   bool
	noModifiers   bool
	vatIncluded   bool // every item has its VAT included (or rate 0)
	basis         []Basis
	noServices    bool
}

func ptr[T any](v T) *T { return &v }

func cents(t *rapid.T, label string, lo, hi int64) *money.Cents {
	return ptr(money.Cents(rapid.Int64Range(lo, hi).Draw(t, label)))
}

func subset[T any](t *rapid.T, label string, from []T, maxLen int) []T {
	if len(from) == 0 || !rapid.Bool().Draw(t, label+"_on") {
		return nil
	}
	n := rapid.IntRange(1, min(maxLen, len(from))).Draw(t, label+"_n")
	idx := rapid.Permutation(rangeInts(len(from))).Draw(t, label+"_perm")
	out := make([]T, 0, n)
	for _, i := range idx[:n] {
		out = append(out, from[i])
	}
	return out
}

func rangeInts(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func berthItemID(ct ContractType) string { return "b-" + string(ct) }

func genCatalog(t *rapid.T, o genOpts) Catalog {
	cts := o.contractTypes
	if cts == nil {
		cts = subset(t, "cts", ContractTypes(), 6)
		if len(cts) == 0 {
			cts = []ContractType{ContractTransit}
		}
	}
	vat := func(label string) (int64, string, bool) {
		rate := rapid.SampledFrom([]int64{0, 400, 1000, 2200}).Draw(t, label+"_rate")
		nature := ""
		if rate == 0 {
			nature = rapid.SampledFrom([]string{"N1", "N4", "N2.2"}).Draw(t, label+"_nature")
		}
		incl := o.vatIncluded || rapid.Bool().Draw(t, label+"_incl")
		return rate, nature, incl
	}
	cat := Catalog{PriceList: PriceList{ID: "pl", Code: "PL", ValidFrom: genValidFrom, ValidTo: genValidTo, ContractTypes: cts}}
	s := DefaultSettings()
	s.PrimaryItems = map[ContractType]string{}
	s.SeasonWindow = &DateRange{From: calendar.New(2027, 5, 1), To: calendar.New(2027, 9, 30)}
	s.DimensionSource = rapid.SampledFrom([]DimensionSource{DimensionVessel, DimensionBerth, DimensionMax}).Draw(t, "dimsrc")
	s.LengthRounding = LengthRounding{
		StepCm: geo.Cm(rapid.SampledFrom([]int64{1, 10, 50, 100}).Draw(t, "step")),
		Mode:   rapid.SampledFrom([]string{LengthRoundUp, LengthRoundHalfEven}).Draw(t, "mode"),
	}
	if rapid.Bool().Draw(t, "nominbeam") {
		s.MinBeamRatio = nil
	}
	s.Split[AnyContractType] = rapid.SampledFrom([]SplitRule{SplitProRataDaily, SplitWholeMonths, SplitBestCombination}).Draw(t, "split")
	s.Rounding[AnyContractType] = rapid.SampledFrom([]TotalRounding{RoundNone, RoundEuro, RoundFiveEuro}).Draw(t, "rounding")
	if rapid.Bool().Draw(t, "deposit") {
		if rapid.Bool().Draw(t, "deposit_pct") {
			s.Deposits[AnyContractType] = DepositPolicy{PercentBP: ptr(rapid.Int64Range(0, 10_000).Draw(t, "deposit_bp"))}
		} else {
			s.Deposits[AnyContractType] = DepositPolicy{AmountCents: cents(t, "deposit_amt", 0, 1_000_000)}
		}
	}
	for _, ct := range cts {
		rate, nature, incl := vat("item_" + string(ct))
		cat.Items = append(cat.Items, Item{ID: berthItemID(ct), Code: "B-" + string(ct), Type: ItemBerthFee, VATRateBP: rate, VATNature: nature, PriceIncludesVAT: incl, Active: true})
		s.PrimaryItems[ct] = berthItemID(ct)
	}
	nServices := 0
	if !o.noServices {
		nServices = rapid.IntRange(0, 3).Draw(t, "services")
	}
	for i := range nServices {
		rate, nature, incl := vat(fmt.Sprintf("svc%d", i))
		cat.Items = append(cat.Items, Item{
			ID: fmt.Sprintf("s%d", i), Code: fmt.Sprintf("S-%d", i),
			Type:      rapid.SampledFrom([]ItemType{ItemService, ItemUtility}).Draw(t, fmt.Sprintf("svc%d_type", i)),
			VATRateBP: rate, VATNature: nature, PriceIncludesVAT: incl, Active: true,
		})
	}
	cat.PriceList.Settings = s

	// Periods: LOW / HIGH seasons and an optional event.
	if rapid.Bool().Draw(t, "periods") {
		cat.PriceList.Periods = []RatePeriod{
			{Code: "LOW", Kind: PeriodSeason, Ranges: []DateRange{{From: calendar.New(2027, 1, 1), To: calendar.New(2027, 6, 30)}, {From: calendar.New(2027, 9, 1), To: calendar.New(2027, 12, 31)}}},
			{Code: "HIGH", Kind: PeriodSeason, Ranges: []DateRange{{From: calendar.New(2027, 7, 1), To: calendar.New(2027, 8, 31)}}},
		}
		if rapid.Bool().Draw(t, "event") {
			cat.PriceList.Periods = append(cat.PriceList.Periods, RatePeriod{
				Code: "EV", Kind: PeriodEvent,
				Ranges: []DateRange{{From: calendar.New(2027, 7, 20), To: calendar.New(2027, 7, 25)}},
			})
		}
	}
	periodCodes := []string{}
	for _, p := range cat.PriceList.Periods {
		periodCodes = append(periodCodes, p.Code)
	}
	// Bands.
	nb := rapid.IntRange(0, 4).Draw(t, "bands")
	var maxCm geo.Cm
	for i := range nb {
		b := Band{Code: fmt.Sprintf("L%d", i), Category: rapid.SampledFrom(append([]string{""}, genCats...)).Draw(t, fmt.Sprintf("band%d_cat", i))}
		if i < nb-1 || rapid.Bool().Draw(t, "band_closed") {
			maxCm += geo.Cm(rapid.Int64Range(100, 800).Draw(t, fmt.Sprintf("band%d_step", i)))
			b.MaxCm = ptr(maxCm)
		}
		cat.PriceList.Bands = append(cat.PriceList.Bands, b)
	}

	scope := func(label string) Scope {
		return Scope{
			Categories:         subset(t, label+"_cats", genCats, 2),
			Channels:           subset(t, label+"_ch", Channels(), 2),
			CustomerCategories: subset(t, label+"_cc", genCustCats, 2),
			Periods:            subset(t, label+"_per", periodCodes, 2),
		}
	}
	bases := o.basis
	if bases == nil {
		bases = []Basis{BasisFlat, BasisPerBerth, BasisLengthBand, BasisPerMeter, BasisPerM2, BasisPerM2Berth}
	}
	seq := int64(0)
	nextID := func(prefix string) (string, int64) {
		seq++
		return fmt.Sprintf("%s%03d", prefix, seq), seq
	}
	units := []TimeUnit{UnitNight, UnitWeek, UnitMonth, UnitSeason, UnitYear}
	if o.nightOnly {
		units = []TimeUnit{UnitNight}
	}
	for _, it := range cat.Items {
		// Most catalogs have a low-priority unscoped fallback so that many draws are priceable.
		if rapid.IntRange(0, 4).Draw(t, it.ID+"_fallback") > 0 {
			fu := []TimeUnit{UnitStay}
			if it.Type == ItemBerthFee {
				fu = []TimeUnit{UnitNight}
				for _, ct := range cts {
					if berthItemID(ct) == it.ID && !o.nightOnly && referenceUnit(ct) != UnitNight {
						fu = append(fu, referenceUnit(ct), UnitMonth)
					}
				}
			} else if o.nightOnly {
				fu = []TimeUnit{UnitNight}
			}
			for _, u := range fu {
				id, sq := nextID("f")
				cat.Rules = append(cat.Rules, Rule{
					ID: id, Seq: sq, Stage: StageBase, Name: id, Active: true, Priority: -5, ItemID: it.ID,
					Unit: u, Basis: BasisFlat, AmountCents: cents(t, id+"_amt", 0, 300_000),
				})
			}
		}
		n := rapid.IntRange(1, 4).Draw(t, it.ID+"_rules")
		for k := range n {
			label := fmt.Sprintf("%s_r%d", it.ID, k)
			id, sq := nextID("r")
			r := Rule{
				ID: id, Seq: sq, Stage: StageBase, Name: id, Active: rapid.IntRange(0, 9).Draw(t, label+"_active") > 0,
				Priority: rapid.Int64Range(-2, 2).Draw(t, label+"_prio"), ItemID: it.ID,
			}
			if it.Type == ItemBerthFee {
				r.Unit = rapid.SampledFrom(units).Draw(t, label+"_unit")
				r.Basis = rapid.SampledFrom(bases).Draw(t, label+"_basis")
				if !o.nightOnly && rapid.Bool().Draw(t, label+"_incl") && nServices > 0 {
					r.Includes = []Inclusion{{ItemID: "s0", Qty: qty.Milli(rapid.Int64Range(1, 5000).Draw(t, label+"_incq"))}}
				}
			} else {
				r.Unit = rapid.SampledFrom([]TimeUnit{UnitStay, UnitNight}).Draw(t, label+"_unit")
				if o.nightOnly {
					r.Unit = UnitNight
				}
				r.Basis = rapid.SampledFrom([]Basis{BasisFlat, BasisMetered}).Draw(t, label+"_basis")
			}
			switch r.Basis {
			case BasisFlat:
				r.AmountCents = cents(t, label+"_amt", 0, 500_000)
			case BasisPerBerth:
				r.Amounts = map[string]money.Cents{}
				for _, c := range subset(t, label+"_keys", genCats, 3) {
					r.Amounts[c] = *cents(t, label+"_"+c, 0, 500_000)
				}
				if rapid.Bool().Draw(t, label+"_def") {
					r.AmountCents = cents(t, label+"_amt", 0, 500_000)
				}
			case BasisLengthBand:
				r.Amounts = map[string]money.Cents{}
				for _, b := range cat.PriceList.Bands {
					if rapid.IntRange(0, 4).Draw(t, label+"_"+b.Code+"_on") > 0 {
						r.Amounts[b.Code] = *cents(t, label+"_"+b.Code, 0, 500_000)
					}
				}
				if rapid.Bool().Draw(t, label+"_def") {
					r.AmountCents = cents(t, label+"_amt", 0, 500_000)
				}
			default:
				r.UnitCents = cents(t, label+"_unit_cents", 0, 50_000)
			}
			if rapid.Bool().Draw(t, label+"_scoped") {
				r.Scope = scope(label)
			}
			cat.Rules = append(cat.Rules, r)
		}
	}
	allItems := make([]string, len(cat.Items))
	for i, it := range cat.Items {
		allItems[i] = it.ID
	}
	conds := func(label string) Conditions {
		var c Conditions
		c.HullTypes = subset(t, label+"_hull", genHulls, 2)
		if rapid.IntRange(0, 3).Draw(t, label+"_over_on") == 0 {
			c.OverhangMinCm = ptr(geo.Cm(rapid.Int64Range(0, 300).Draw(t, label+"_over")))
		}
		if !o.noStayConds {
			if rapid.IntRange(0, 3).Draw(t, label+"_minn_on") == 0 {
				c.MinNights = ptr(rapid.IntRange(1, 30).Draw(t, label+"_minn"))
			}
			if rapid.IntRange(0, 3).Draw(t, label+"_lead_on") == 0 {
				c.LeadMaxDays = ptr(rapid.IntRange(0, 60).Draw(t, label+"_lead"))
			}
		}
		return c
	}
	if !o.noModifiers {
		for k := range rapid.IntRange(0, 5).Draw(t, "modifiers") {
			label := fmt.Sprintf("m%d", k)
			id, sq := nextID("m")
			r := Rule{
				ID: id, Seq: sq, Stage: StageModifier, Name: id, Active: true, Stackable: rapid.Bool().Draw(t, label+"_stack"),
				Priority: rapid.Int64Range(-1, 1).Draw(t, label+"_prio"), Conditions: conds(label),
			}
			if rapid.Bool().Draw(t, label+"_pct") {
				r.Effect.PercentBP = ptr(rapid.Int64Range(-20_000, 20_000).Draw(t, label+"_bp"))
			} else {
				r.Effect.AmountCents = cents(t, label+"_amt", -200_000, 200_000)
			}
			if rapid.Bool().Draw(t, label+"_targets") {
				r.TargetItemIDs = subset(t, label+"_ti", allItems, 2)
			}
			if rapid.Bool().Draw(t, label+"_scoped") {
				r.Scope = scope(label)
			}
			cat.Rules = append(cat.Rules, r)
		}
	}
	if !o.noDiscounts {
		for k := range rapid.IntRange(0, 5).Draw(t, "discounts") {
			label := fmt.Sprintf("d%d", k)
			id, sq := nextID("d")
			r := Rule{
				ID: id, Seq: sq, Stage: StageDiscount, Name: id, Active: true, Stackable: rapid.Bool().Draw(t, label+"_stack"),
				DiscountKind: rapid.SampledFrom([]DiscountKind{DiscountCustomer, DiscountPromo, DiscountAutomatic}).Draw(t, label+"_kind"),
				Conditions:   conds(label),
			}
			if rapid.Bool().Draw(t, label+"_pct") {
				r.Effect.PercentBP = ptr(rapid.Int64Range(1, 10_000).Draw(t, label+"_bp"))
			} else {
				r.Effect.AmountCents = cents(t, label+"_amt", 1, 2_000_000)
			}
			if rapid.Bool().Draw(t, label+"_scoped") {
				r.Scope = scope(label)
			}
			switch r.DiscountKind {
			case DiscountCustomer:
				r.Scope.CustomerCategories = []string{rapid.SampledFrom(genCustCats).Draw(t, label+"_cust")}
			case DiscountPromo:
				r.PromoCode = "PROMO"
			}
			if rapid.Bool().Draw(t, label+"_targets") {
				r.TargetItemTypes = []ItemType{ItemBerthFee, ItemService, ItemUtility}
			}
			cat.Rules = append(cat.Rules, r)
		}
	}
	return cat
}

// genInput draws an input for cat; nights are bounded by maxNights.
func genInput(t *rapid.T, cat Catalog, maxNights int) Input {
	ct := rapid.SampledFrom(cat.PriceList.ContractTypes).Draw(t, "in_ct")
	arrival := genValidFrom.AddDays(rapid.IntRange(0, 364).Draw(t, "in_arrival"))
	n := rapid.IntRange(1, maxNights).Draw(t, "in_nights")
	if rapid.IntRange(0, 4).Draw(t, "in_exact") == 0 {
		switch ct {
		case ContractAnnual:
			arrival = calendar.New(2027, 1, 1)
			n = arrival.DaysUntil(AddYears(arrival, 1))
		case ContractSeasonal, ContractCharter:
			arrival = calendar.New(2027, 5, 1)
			n = 153
		case ContractMonthly, ContractStorage:
			n = arrival.DaysUntil(AddMonths(arrival, rapid.IntRange(1, 3).Draw(t, "in_months")))
		}
	}
	in := Input{ContractType: ct, Arrival: arrival, Departure: arrival.AddDays(n), QuotedOn: arrival.AddDays(-rapid.IntRange(0, 90).Draw(t, "in_lead"))}
	if rapid.IntRange(0, 5).Draw(t, "in_vessel") > 0 {
		loa := geo.Cm(rapid.Int64Range(300, 4000).Draw(t, "in_loa"))
		in.Vessel = &Vessel{LOA: loa, Beam: geo.Cm(rapid.Int64Range(0, int64(loa)/2).Draw(t, "in_beam")), HullType: rapid.SampledFrom(genHulls).Draw(t, "in_hull")}
	}
	in.Resource.Category = rapid.SampledFrom(append([]string{""}, genCats...)).Draw(t, "in_cat")
	if rapid.Bool().Draw(t, "in_berth") {
		in.Resource.Length = geo.Cm(rapid.Int64Range(300, 4000).Draw(t, "in_len"))
		in.Resource.Width = geo.Cm(rapid.Int64Range(100, 1200).Draw(t, "in_wid"))
	}
	in.Channel = rapid.SampledFrom(append([]Channel{""}, Channels()...)).Draw(t, "in_channel")
	in.CustomerCategory = rapid.SampledFrom(append([]string{""}, genCustCats...)).Draw(t, "in_cust")
	if rapid.IntRange(0, 6).Draw(t, "in_promo") == 0 {
		in.PromoCode = "promo"
	}
	for _, it := range cat.Items {
		if it.Type != ItemBerthFee && rapid.Bool().Draw(t, "in_svc_"+it.ID) {
			in.Services = append(in.Services, ServiceRequest{ItemID: it.ID, Qty: qty.Milli(rapid.Int64Range(0, 20_000).Draw(t, "in_q_"+it.ID))})
		}
	}
	return in
}
