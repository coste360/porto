package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedPricing seeds the demo pricing catalogue and price lists (m2-plan §6) through the catalog and pricelist
// services with the demo owner principal, so audit rows, outbox events and the immutability triggers all hold.
// It runs only when the marina has no price lists. Y is the current year in the marina time zone.
func (a *App) seedPricing(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := store.CountPriceLists(ctx, a.DB.DB, scope); err != nil || n > 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	y := calendar.In(a.Clock.Now(), loc).Year()
	m := res.MarinaID

	// Revenue classes: the nine defaults plus a subclass.
	classes, err := a.Catalog.ListRevenueClasses(ctx, p, m, true)
	if err != nil {
		return fmt.Errorf("revenue classes: %w", err)
	}
	classID := map[string]string{}
	for _, c := range classes {
		classID[c.Code] = c.ID
	}
	if classID["mooring_seasonal"] == "" {
		en := "Seasonal moorings"
		c, err := a.Catalog.CreateRevenueClass(ctx, p, m, catalog.RevenueClassInput{
			Code: "mooring_seasonal", Name: catalog.I18nText{It: "Ormeggi stagionali", En: &en}, ParentID: classID["mooring_annual"],
		})
		if err != nil {
			return fmt.Errorf("revenue class: %w", err)
		}
		classID[c.Code] = c.ID
	}

	// Berth categories: sync from the harbour rule and the codes in use, then names and the extra ones.
	if err := a.seedCategories(ctx, p, m); err != nil {
		return err
	}

	// Items.
	items := map[string]string{}
	for _, it := range seedItems {
		in := catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.it, En: &it.en}, ItemType: it.typ, Unit: it.unit, VATRateBP: &it.vat,
			VATNature: it.nature, PriceIncludesVAT: &it.incl, RevenueClassID: classID[it.class],
		}
		created, err := a.Catalog.CreateItem(ctx, p, m, in)
		if err != nil {
			return fmt.Errorf("item %s: %w", it.code, err)
		}
		items[it.code] = created.ID
		res.Items++
	}

	// Price list LST-Y, active.
	doc := seedListDoc(y, items)
	l, err := a.PriceLists.Create(ctx, p, m, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		return fmt.Errorf("price list: %w", err)
	}
	saved, err := a.PriceLists.Save(ctx, p, m, l.ID, l.Version, doc)
	if err != nil {
		return fmt.Errorf("price list save: %w", err)
	}
	act, err := a.PriceLists.Activate(ctx, p, m, l.ID, saved.Document.Version, calendar.Date{})
	if err != nil {
		return fmt.Errorf("price list activate: %w", err)
	}
	res.PriceLists++
	res.PriceRules += len(act.Document.Rules)

	// Draft LST-(Y+1) renewed from LST-Y: +1 year, ISTAT 1,2 %, +3 % on transits, rounded to the euro.
	next := y + 1
	enNext := "Price list " + strconv.Itoa(next)
	_, draft, err := a.PriceLists.Renew(ctx, p, m, l.ID, pricelist.RenewalRequest{
		Code: "LST-" + strconv.Itoa(next), Name: catalog.I18nText{It: "Listino " + strconv.Itoa(next), En: &enNext},
		ShiftYears: 1, ISTATBP: 120, AmountRounding: pricing.AmountRoundEuro,
		Adjustments: []pricing.Adjustment{{Scope: pricing.AdjustItem, ItemID: items["TRN-NTT"], PercentBP: ptrOf(int64(300))}},
	})
	if err != nil {
		return fmt.Errorf("price list renewal: %w", err)
	}
	res.PriceLists++
	res.PriceRules += len(draft.Rules)
	return nil
}

func ptrOf[T any](v T) *T { return &v }

// seedCategories syncs the berth categories and names them; BOA, PARK and BOX are created when missing.
func (a *App) seedCategories(ctx context.Context, p *auth.Principal, m string) error {
	if _, err := a.Catalog.SyncCategories(ctx, p, m); err != nil {
		return fmt.Errorf("categories sync: %w", err)
	}
	list, err := a.Catalog.ListCategories(ctx, p, m, true)
	if err != nil {
		return err
	}
	byCode := map[string]catalog.BerthCategory{}
	for _, c := range list.Items {
		byCode[c.Code] = c
	}
	for i, sc := range seedCategoryNames {
		en := sc.en
		name := catalog.I18nText{It: sc.it, En: &en}
		if c, ok := byCode[sc.code]; ok {
			order := i
			if _, err := a.Catalog.UpdateCategory(ctx, p, m, c.ID, c.Version, catalog.CategoryPatch{Name: &name, SortOrder: &order}); err != nil {
				return fmt.Errorf("category %s: %w", sc.code, err)
			}
			continue
		}
		order := i
		if _, err := a.Catalog.CreateCategory(ctx, p, m, catalog.CategoryInput{Code: sc.code, Name: name, AppliesTo: sc.applies, SortOrder: &order}); err != nil {
			return fmt.Errorf("category %s: %w", sc.code, err)
		}
	}
	return nil
}

var seedCategoryNames = []struct {
	code, it, en string
	applies      []string
}{
	{"L8", "Fino a 8 m", "Up to 8 m", []string{"berth", "dynamic_area"}},
	{"L10", "Fino a 10 m", "Up to 10 m", []string{"berth", "dynamic_area"}},
	{"L12", "Fino a 12 m", "Up to 12 m", []string{"berth", "dynamic_area"}},
	{"L14", "Fino a 14 m", "Up to 14 m", []string{"berth", "dynamic_area"}},
	{"L16", "Fino a 16 m", "Up to 16 m", []string{"berth", "dynamic_area"}},
	{"L18", "Fino a 18 m", "Up to 18 m", []string{"berth", "dynamic_area"}},
	{"L20", "Fino a 20 m", "Up to 20 m", []string{"berth", "dynamic_area"}},
	{"L25", "Fino a 25 m", "Up to 25 m", []string{"berth", "dynamic_area"}},
	{"BOA", "Boe", "Buoys", []string{"berth"}},
	{"PARK", "Posti auto", "Parking spaces", []string{"land_resource"}},
	{"BOX", "Box", "Storage boxes", []string{"land_resource"}},
}

type seedItem struct {
	code, typ, unit string
	vat             int64
	nature          string
	incl            bool
	class, it, en   string
}

var seedItems = []seedItem{
	{"ORM-ANN", "berth_fee", "year", 2200, "", false, "mooring_annual", "Ormeggio annuale", "Annual mooring"},
	{"ORM-STG", "berth_fee", "season", 2200, "", false, "mooring_seasonal", "Ormeggio stagionale", "Seasonal mooring"},
	{"ORM-MES", "berth_fee", "month", 2200, "", false, "mooring_annual", "Ormeggio mensile", "Monthly mooring"},
	{"TRN-NTT", "berth_fee", "night", 2200, "", true, "transit", "Transiti per notte", "Transit per night"},
	{"SRV-UTF", "utility", "season", 2200, "", false, "utilities", "Utenze forfettarie", "Flat-rate utilities"},
	{"SRV-ALG", "service", "lump", 2200, "", false, "services", "Alaggio e varo", "Haul-out and launch"},
	{"SRV-AUTO", "service", "night", 2200, "", true, "services", "Posto auto", "Car park"},
	{"UTL-KWH", "utility", "kwh", 2200, "", true, "utilities", "Energia elettrica", "Electricity"},
	{"UTL-H2O", "utility", "m3", 1000, "", true, "utilities", "Acqua", "Water"},
	{"FEE-PRA", "fee", "lump", 2200, "", false, "fees", "Diritti di pratica", "Paperwork fee"},
	{"DEP-CAU", "deposit", "lump", 0, "N1", false, "fees", "Deposito cauzionale", "Security deposit"},
}

// seedListDoc is the document of LST-Y (m2-plan §6). Euro amounts are written in cents.
func seedListDoc(y int, items map[string]string) pricelist.SaveRequest {
	day := func(mo, d int) calendar.Date { return calendar.New(y, time.Month(mo), d) }
	c := func(v int64) *money.Cents { x := money.Cents(v); return &x }
	bp := func(v int64) *int64 { return &v }
	n := func(v int) *int { return &v }
	en := func(s string) *string { return &s }
	bandCodes := []string{"L8", "L10", "L12", "L14", "L16", "L18", "L20", "L25"}
	bandMax := []int64{800, 1000, 1200, 1400, 1600, 1800, 2000, 2500}
	amounts := func(euros ...int64) map[string]money.Cents {
		out := map[string]money.Cents{}
		for i, e := range euros {
			out[bandCodes[i]] = money.Cents(e * 100)
		}
		return out
	}
	set := pricing.DefaultSettings()
	set.Split = map[pricing.ContractType]pricing.SplitRule{pricing.AnyContractType: pricing.SplitProRataDaily, pricing.ContractTransit: pricing.SplitBestCombination}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{
		pricing.AnyContractType: pricing.RoundNone, pricing.ContractAnnual: pricing.RoundEuro, pricing.ContractSeasonal: pricing.RoundEuro,
	}
	set.Deposits = map[pricing.ContractType]pricing.DepositPolicy{
		pricing.ContractAnnual: {PercentBP: bp(3000)}, pricing.ContractSeasonal: {PercentBP: bp(5000)},
		pricing.ContractMonthly: {PercentBP: bp(10000)}, pricing.ContractTransit: {PercentBP: bp(3000)},
	}
	set.SeasonWindow = &pricing.DateRange{From: day(4, 1), To: day(9, 30)}
	// The public marina profile (M12) shows the transit matrix of this list.
	set.PublishMatrix = true
	set.PrimaryItems = map[pricing.ContractType]string{
		pricing.ContractAnnual: items["ORM-ANN"], pricing.ContractSeasonal: items["ORM-STG"],
		pricing.ContractMonthly: items["ORM-MES"], pricing.ContractTransit: items["TRN-NTT"],
	}
	doc := pricelist.SaveRequest{
		Code: "LST-" + strconv.Itoa(y), Name: catalog.I18nText{It: "Listino " + strconv.Itoa(y), En: en("Price list " + strconv.Itoa(y))},
		ValidFrom: day(1, 1), ValidTo: day(12, 31),
		ContractTypes: []pricing.ContractType{pricing.ContractAnnual, pricing.ContractSeasonal, pricing.ContractMonthly, pricing.ContractTransit},
		Settings:      set,
		Periods: []pricelist.Period{
			{
				Key: "bassa", Code: "BASSA", Name: catalog.I18nText{It: "Bassa stagione", En: en("Low season")}, Kind: pricing.PeriodSeason, Color: "chart-1",
				Ranges: []pricing.DateRange{{From: day(1, 1), To: day(3, 31)}, {From: day(10, 16), To: day(12, 31)}},
			},
			{
				Key: "media", Code: "MEDIA", Name: catalog.I18nText{It: "Media stagione", En: en("Mid season")}, Kind: pricing.PeriodSeason, Color: "chart-2",
				Ranges: []pricing.DateRange{{From: day(4, 1), To: day(6, 15)}, {From: day(9, 1), To: day(10, 15)}},
			},
			{
				Key: "alta", Code: "ALTA", Name: catalog.I18nText{It: "Alta stagione", En: en("High season")}, Kind: pricing.PeriodSeason, Color: "chart-3",
				Ranges: []pricing.DateRange{{From: day(6, 16), To: day(8, 31)}},
			},
			{
				Key: "salone", Code: "SALONE", Name: catalog.I18nText{It: "Salone nautico", En: en("Boat show")}, Kind: pricing.PeriodEvent, Color: "chart-4",
				Ranges: []pricing.DateRange{{From: day(9, 17), To: day(9, 22)}},
			},
		},
	}
	for i, code := range bandCodes {
		mx := geo.Cm(bandMax[i])
		doc.Bands = append(doc.Bands, pricelist.Band{Key: "band-" + code, Code: code, MaxCm: &mx, Category: code})
	}
	base := func(name, item string, basis pricing.Basis, unit pricing.TimeUnit) pricing.Rule {
		return pricing.Rule{Stage: pricing.StageBase, Name: name, ItemID: items[item], Basis: basis, Unit: unit}
	}
	var rules []pricelist.Rule
	add := func(key string, r pricing.Rule) { rules = append(rules, pricelist.Rule{Key: key, Rule: r}) }

	annual := base("Annuale", "ORM-ANN", pricing.BasisLengthBand, pricing.UnitYear)
	annual.Amounts = amounts(2900, 3800, 4900, 6200, 7800, 9600, 11800, 16500)
	annual.Includes = []pricing.Inclusion{{ItemID: items["SRV-UTF"], Qty: 1000}}
	add("ann", annual)
	seasonal := base("Stagionale", "ORM-STG", pricing.BasisLengthBand, pricing.UnitSeason)
	seasonal.Amounts = amounts(1900, 2450, 3200, 4050, 5050, 6250, 7650, 10700)
	add("stg", seasonal)
	for _, mr := range []struct {
		key, name string
		periods   []string
		euros     []int64
	}{
		{"mes-bassa", "Mensile bassa", []string{"BASSA"}, []int64{290, 380, 490, 620, 780, 960, 1180, 1650}},
		{"mes-media", "Mensile media", []string{"MEDIA"}, []int64{380, 490, 640, 810, 1010, 1250, 1530, 2150}},
		{"mes-alta", "Mensile alta e salone", []string{"ALTA", "SALONE"}, []int64{520, 680, 880, 1120, 1400, 1730, 2120, 2970}},
	} {
		r := base(mr.name, "ORM-MES", pricing.BasisLengthBand, pricing.UnitMonth)
		r.Amounts = amounts(mr.euros...)
		r.Scope = pricing.Scope{Periods: mr.periods}
		add(mr.key, r)
	}
	for _, tr := range []struct {
		period      string
		night, week int64
	}{{"BASSA", 300, 1800}, {"MEDIA", 450, 2700}, {"ALTA", 750, 4500}, {"SALONE", 900, 5400}} {
		r := base("Transiti notte "+tr.period, "TRN-NTT", pricing.BasisPerMeter, pricing.UnitNight)
		r.UnitCents, r.Scope = c(tr.night), pricing.Scope{Periods: []string{tr.period}}
		add("trn-n-"+tr.period, r)
		w := base("Transiti settimana "+tr.period, "TRN-NTT", pricing.BasisPerMeter, pricing.UnitWeek)
		w.UnitCents, w.Scope = c(tr.week), pricing.Scope{Periods: []string{tr.period}}
		add("trn-w-"+tr.period, w)
	}
	buoy := base("Transiti in boa", "TRN-NTT", pricing.BasisPerBerth, pricing.UnitNight)
	buoy.Priority, buoy.Amounts, buoy.Scope = 10, map[string]money.Cents{"BOA": 2500}, pricing.Scope{Categories: []string{"BOA"}}
	add("trn-boa", buoy)
	for _, sv := range []struct {
		item  string
		basis pricing.Basis
		unit  pricing.TimeUnit
		cents int64
	}{
		{"SRV-ALG", pricing.BasisFlat, pricing.UnitStay, 18000},
		{"SRV-AUTO", pricing.BasisFlat, pricing.UnitNight, 800},
		{"UTL-KWH", pricing.BasisMetered, pricing.UnitStay, 45},
		{"UTL-H2O", pricing.BasisMetered, pricing.UnitStay, 320},
		{"FEE-PRA", pricing.BasisFlat, pricing.UnitStay, 2500},
		{"DEP-CAU", pricing.BasisFlat, pricing.UnitStay, 20000},
		{"SRV-UTF", pricing.BasisFlat, pricing.UnitStay, 0},
	} {
		r := base("Servizio "+sv.item, sv.item, sv.basis, sv.unit)
		if sv.basis == pricing.BasisMetered {
			r.UnitCents = c(sv.cents)
		} else {
			r.AmountCents = c(sv.cents)
		}
		add("srv-"+sv.item, r)
	}
	transit := []pricing.ContractType{pricing.ContractTransit}
	mod := func(key, name string, pct int64, stackable bool, scope pricing.Scope, cond pricing.Conditions) {
		add(key, pricing.Rule{
			Stage: pricing.StageModifier, Name: name, Stackable: stackable, Scope: scope, Conditions: cond,
			TargetItemTypes: []pricing.ItemType{pricing.ItemBerthFee}, Effect: pricing.Effect{PercentBP: bp(pct)},
		})
	}
	overhang := geo.Cm(50)
	mod("multiscafo", "MULTISCAFO", 5000, false, pricing.Scope{}, pricing.Conditions{HullTypes: []string{"catamaran", "trimaran"}})
	mod("sbalzo", "SBALZO", 2000, false, pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractTransit, pricing.ContractMonthly}},
		pricing.Conditions{OverhangMinCm: &overhang})
	mod("lunga-sosta", "LUNGA-SOSTA", -1000, true, pricing.Scope{ContractTypes: transit}, pricing.Conditions{MinNights: n(14)})
	mod("anticipo", "ANTICIPO", -500, true, pricing.Scope{ContractTypes: transit}, pricing.Conditions{LeadMinDays: n(60)})
	mod("ultimo-minuto", "ULTIMO-MINUTO", -1000, false, pricing.Scope{ContractTypes: transit, Periods: []string{"BASSA"}}, pricing.Conditions{LeadMaxDays: n(2)})
	mod("online", "ONLINE", -300, true, pricing.Scope{Channels: []pricing.Channel{pricing.ChannelOnline}}, pricing.Conditions{})
	disc := func(key, name string, kind pricing.DiscountKind, pct int64, scope pricing.Scope, promo string) {
		add(key, pricing.Rule{
			Stage: pricing.StageDiscount, Name: name, DiscountKind: kind, PromoCode: promo, Scope: scope,
			TargetItemTypes: []pricing.ItemType{pricing.ItemBerthFee}, Effect: pricing.Effect{PercentBP: bp(pct)},
		})
	}
	disc("soci", "SOCI", pricing.DiscountCustomer, 1000, pricing.Scope{
		CustomerCategories: []string{"member"},
		ContractTypes:      []pricing.ContractType{pricing.ContractAnnual, pricing.ContractSeasonal},
	}, "")
	disc("residenti", "RESIDENTI", pricing.DiscountCustomer, 500, pricing.Scope{
		CustomerCategories: []string{"resident"},
		ContractTypes:      []pricing.ContractType{pricing.ContractAnnual},
	}, "")
	disc("benvenuto", "BENVENUTO", pricing.DiscountPromo, 1000, pricing.Scope{ContractTypes: transit}, "BENVENUTO"+strconv.Itoa(y))
	doc.Rules = rules
	return doc
}
