package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Seed codes of the rental fleet, read by the BOOKING and HANDOVER seeds.
const (
	SeedRentalUnitGOM58   = "GOM58"
	SeedRentalUnitGOM65   = "GOM65"
	SeedRentalUnitOPEN    = "OPEN55"
	SeedRentalUnitSAIL    = "VELA26"
	SeedRentalUnitSUP     = "SUP1"
	SeedRentalUnitGOM50   = "GOM50"
	SeedRentalUnitGOM70   = "GOM70"
	SeedRentalUnitOPEN60  = "OPEN60"
	SeedRentalUnitSAIL32  = "VELA32"
	SeedRentalUnitSUP2    = "SUP2"
	SeedRentalHourly      = "ORARIA"
	SeedRentalMorning     = "MEZZA-MATTINA"
	SeedRentalAfternoon   = "MEZZA-POMERIGGIO"
	SeedRentalDay         = "GIORNATA"
	SeedRentalMultiDay    = "PIU-GIORNI"
	SeedRentalWeek        = "SETTIMANA"
	seedRentalListCodeFmt = "NOL-%d"
)

// seedRentalFleet adds the M11 demo of FLEET (m11-plan §16.2): 10 units (4 rib, 2 open, 2 sail, 2 sup), 3 skippers, the
// formulas ORARIA, MEZZA-MATTINA, MEZZA-POMERIGGIO, GIORNATA, PIU-GIORNI, SETTIMANA, the price list NOL-<year> of
// m11-plan §5.3 (period alta = July and August, prices VAT included), extras, fuel, late and damage items, and the
// settings with the skipper and damage items. Everything goes through the services as the demo owner; a no-op when
// the marina already has rental units.
//
// Like CANT-<year> (M10), the list is created for charter and saved with no contract type: it serves no mooring
// quote, and RentalQuote ignores contract-type scopes.
func (a *App) seedRentalFleet(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.RentalFleet == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if units, err := store.AllRentalUnits(ctx, a.DB.DB, scope); err != nil || len(units) > 0 {
		return err
	}
	m := res.MarinaID
	en := func(s string) *string { return &s }
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	y := calendar.In(a.Clock.Now(), loc).Year()

	// Items: rental units and skipper (revenue class rental), extras, fuel, late fee and damages.
	classes, err := a.Catalog.ListRevenueClasses(ctx, p, m, true)
	if err != nil {
		return err
	}
	classID := map[string]string{}
	for _, c := range classes {
		classID[c.Code] = c.ID
	}
	services := "services"
	if classID[services] == "" {
		services = "rental"
	}
	items := map[string]string{}
	vat, incl := int64(2200), true
	for _, it := range []struct{ code, unit, class, it, en string }{
		{"NOL-GOM58", "hour", "rental", "Noleggio gommone 5,8 m", "Rental RIB 5.8 m"},
		{"NOL-GOM65", "hour", "rental", "Noleggio gommone 6,5 m", "Rental RIB 6.5 m"},
		{"NOL-OPEN", "hour", "rental", "Noleggio open 5,5 m", "Rental open boat 5.5 m"},
		{"NOL-VELA", "hour", "rental", "Noleggio barca a vela 26'", "Rental sailing boat 26'"},
		{"NOL-SUP", "hour", "rental", "Noleggio SUP", "Rental SUP"},
		{"SKIPPER", "hour", "rental", "Skipper", "Skipper"},
		{"SNORKEL", "lump", "rental", "Kit snorkeling", "Snorkelling kit"},
		{"FRIGO", "lump", "rental", "Borsa frigo", "Ice box"},
		{"RADA", "lump", services, "Ormeggio in rada", "Roadstead mooring"},
		{"FUEL-P", "piece", "rental", "Carburante (litro)", "Fuel (litre)"},
		{"LATE-GOM", "hour", "rental", "Ritardo riconsegna", "Late return"},
		{"DANNI", "lump", "rental", "Danni al mezzo", "Damage to the boat"},
	} {
		created, err := a.Catalog.CreateItem(ctx, p, m, catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.it, En: en(it.en)}, ItemType: string(pricing.ItemService), Unit: it.unit,
			VATRateBP: &vat, PriceIncludesVAT: &incl, RevenueClassID: classID[it.class],
		})
		if err != nil {
			return fmt.Errorf("rental item %s: %w", it.code, err)
		}
		items[it.code] = created.ID
		res.Items++
	}

	// Price list NOL-<year> (m11-plan §5.3), valid two years like the yard list.
	if err := a.seedRentalPriceList(ctx, p, m, y, items, res); err != nil {
		return err
	}

	// Settings: skipper and damage items (the defaults otherwise).
	set, err := a.RentalFleet.GetSettings(ctx, p, m)
	if err != nil {
		return err
	}
	in := fleet.SettingsInput{
		OpenFromSlot: set.Row.OpenFromSlot, OpenToSlot: set.Row.OpenToSlot, Bands: set.Bands, BufferSlots: set.Row.BufferSlots,
		DeskHoldMinutes: set.Row.DeskHoldMinutes, NoShowAfterMinutes: set.Row.NoShowAfterMinutes,
		LicenceThresholdKWMilli: set.Row.LicenceThresholdKWMilli, SkipperItemID: items["SKIPPER"], DamageItemID: items["DANNI"],
		Checklist: set.Checklist, CheckoutMinPhotos: set.Row.CheckoutMinPhotos, CheckinMinPhotos: set.Row.CheckinMinPhotos,
		DepositModes: set.DepositModes, IdentityRetentionDays: set.Row.IdentityRetentionDays,
	}
	if _, err := a.RentalFleet.PutSettings(ctx, p, m, set.Row.Version, in); err != nil {
		return fmt.Errorf("rental settings: %w", err)
	}

	// Units: registry vessels of the marina, then the fleet rows.
	for i, u := range []struct {
		code, name, hull, category, item, fuel, licence string
		loa, beam, kw                                   int64
		seats                                           int
		tank, deposit                                   int64
	}{
		{SeedRentalUnitGOM58, "Gabbiano", "rib", store.RentalCategoryRIB, "NOL-GOM58", store.RentalFuelPetrol, store.RentalLicenceThreshold, 580, 240, 40, 8, 60_000, 50_000},
		{SeedRentalUnitGOM65, "Cormorano", "rib", store.RentalCategoryRIB, "NOL-GOM65", store.RentalFuelPetrol, store.RentalLicenceThreshold, 650, 260, 110, 10, 120_000, 80_000},
		{SeedRentalUnitOPEN, "Sirena", "monohull_motor", store.RentalCategoryOpen, "NOL-OPEN", store.RentalFuelPetrol, store.RentalLicenceThreshold, 550, 220, 29, 6, 40_000, 30_000},
		{SeedRentalUnitSAIL, "Maestrale", "monohull_sail", store.RentalCategorySail, "NOL-VELA", store.RentalFuelDiesel, store.RentalLicenceAlways, 790, 280, 15, 6, 30_000, 100_000},
		{SeedRentalUnitSUP, "Tavola 1", "other", store.RentalCategorySUP, "NOL-SUP", store.RentalFuelNone, store.RentalLicenceNever, 320, 80, 0, 1, 0, 5_000},
		{SeedRentalUnitGOM50, "Delfino", "rib", store.RentalCategoryRIB, "NOL-GOM58", store.RentalFuelPetrol, store.RentalLicenceThreshold, 500, 220, 30, 6, 50_000, 40_000},
		{SeedRentalUnitGOM70, "Albatros", "rib", store.RentalCategoryRIB, "NOL-GOM65", store.RentalFuelPetrol, store.RentalLicenceThreshold, 700, 280, 150, 12, 150_000, 100_000},
		{SeedRentalUnitOPEN60, "Ondina", "monohull_motor", store.RentalCategoryOpen, "NOL-OPEN", store.RentalFuelPetrol, store.RentalLicenceThreshold, 600, 240, 40, 7, 60_000, 40_000},
		{SeedRentalUnitSAIL32, "Libeccio", "monohull_sail", store.RentalCategorySail, "NOL-VELA", store.RentalFuelDiesel, store.RentalLicenceAlways, 980, 330, 20, 8, 60_000, 150_000},
		{SeedRentalUnitSUP2, "Tavola 2", "other", store.RentalCategorySUP, "NOL-SUP", store.RentalFuelNone, store.RentalLicenceNever, 320, 80, 0, 1, 0, 5_000},
	} {
		name, hull, loa, beam, fuel := "Noleggio "+u.name, u.hull, u.loa, u.beam, u.fuel
		patch := vessel.Patch{Name: &name, HullType: &hull, LOACm: &loa, BeamCm: &beam, Flag: ptrOf("IT")}
		if u.kw > 0 {
			kw := u.kw
			patch.Engines = &[]vessel.Engine{{Make: "Demo", PowerKW: &kw}}
			patch.Fuel = &fuel
		}
		v, err := a.Vessels.Create(ctx, p, m, patch, nil)
		if err != nil {
			return fmt.Errorf("rental vessel %s: %w", u.code, err)
		}
		if _, err := a.RentalFleet.CreateUnit(ctx, p, m, fleet.UnitInput{
			VesselID: v.Row.ID, Code: u.code, Category: u.category, Seats: u.seats, Licence: u.licence, FuelType: u.fuel,
			Tank: qty.Milli(u.tank), Deposit: money.Cents(u.deposit), ItemID: items[u.item], Sort: i,
		}); err != nil {
			return fmt.Errorf("rental unit %s: %w", u.code, err)
		}
	}

	// Skippers: Monday to Saturday 08:00-19:00.
	for i, name := range []string{"Skipper Demo Uno", "Skipper Demo Due", "Skipper Demo Tre"} {
		k, err := a.RentalFleet.CreateSkipper(ctx, p, m, fleet.SkipperInput{
			Name: name, Phone: "+39 000 000000" + strconv.Itoa(i), LicenceExpiresOn: calendar.New(y+3, 12, 31), Sort: i,
		})
		if err != nil {
			return fmt.Errorf("rental skipper: %w", err)
		}
		var rows []fleet.HoursRow
		for wd := 1; wd <= 6; wd++ {
			rows = append(rows, fleet.HoursRow{Weekday: wd, FromSlot: 16, ToSlot: 38})
		}
		if _, err := a.RentalFleet.PutSkipperHours(ctx, p, m, k.ID, k.Version, rows); err != nil {
			return fmt.Errorf("rental skipper hours: %w", err)
		}
	}

	// Formulas.
	metered, included := store.RentalFuelMetered, store.RentalFuelIncluded
	grace := 15
	for i, f := range []struct {
		code, it, en, kind, band, skipper, fuel string
		minDays                                 int
	}{
		{SeedRentalHourly, "Oraria", "Hourly", store.RentalKindHourly, "", store.RentalSkipperOptional, metered, 0},
		{SeedRentalMorning, "Mezza giornata mattina", "Half day morning", store.RentalKindHalfDay, "morning", store.RentalSkipperOptional, metered, 0},
		{SeedRentalAfternoon, "Mezza giornata pomeriggio", "Half day afternoon", store.RentalKindHalfDay, "afternoon", store.RentalSkipperOptional, metered, 0},
		{SeedRentalDay, "Giornata", "Full day", store.RentalKindDay, "", store.RentalSkipperOptional, metered, 0},
		{SeedRentalMultiDay, "Più giorni", "Several days", store.RentalKindMultiDay, "", store.RentalSkipperOptional, included, 2},
		{SeedRentalWeek, "Settimana", "Week", store.RentalKindWeek, "", store.RentalSkipperOptional, included, 7},
	} {
		in := fleet.FormulaInput{
			Code: f.code, Name: map[string]string{"it": f.it, "en": f.en}, Kind: f.kind, BandCode: f.band, Skipper: f.skipper,
			Fuel: f.fuel, LateItemID: items["LATE-GOM"], GraceMinutes: &grace, LateStepMinutes: 30, MinDays: f.minDays, Sort: i,
		}
		if f.fuel == metered {
			in.FuelItemID = items["FUEL-P"]
		}
		if f.kind == store.RentalKindHourly {
			in.Categories = []string{store.RentalCategoryRIB, store.RentalCategoryOpen, store.RentalCategorySUP}
		}
		if _, err := a.RentalFleet.CreateFormula(ctx, p, m, in); err != nil {
			return fmt.Errorf("rental formula %s: %w", f.code, err)
		}
	}

	// Extras.
	for i, x := range []struct {
		code, mode string
		cats       []string
	}{
		{"SNORKEL", store.RentalExtraPerRental, nil},
		{"FRIGO", store.RentalExtraPerDay, nil},
		{"RADA", store.RentalExtraPerRental, []string{store.RentalCategoryRIB, store.RentalCategoryOpen, store.RentalCategorySail}},
	} {
		if _, err := a.RentalFleet.CreateExtra(ctx, p, m, fleet.ExtraInput{
			ItemID: items[x.code], ChargeMode: x.mode, DefaultQtyMilli: qty.One, MaxQtyMilli: qty.Milli(10_000), Categories: x.cats, Sort: i,
		}); err != nil {
			return fmt.Errorf("rental extra %s: %w", x.code, err)
		}
	}
	return nil
}

// seedRentalPriceList saves and activates NOL-<year> with the §5.3 prices (and plausible ones for the other units).
func (a *App) seedRentalPriceList(ctx context.Context, p *auth.Principal, m string, y int, items map[string]string, res *SeedResult) error {
	en := func(s string) *string { return &s }
	cents := func(c int64) *money.Cents { v := money.Cents(c); return &v }
	code := fmt.Sprintf(seedRentalListCodeFmt, y)
	doc := pricelist.SaveRequest{
		Code: code, Name: catalog.I18nText{It: "Noleggio " + strconv.Itoa(y), En: en("Rental " + strconv.Itoa(y))},
		ValidFrom: calendar.New(y, 1, 1), ValidTo: calendar.New(y+1, 12, 31), ContractTypes: []pricing.ContractType{},
		Settings: pricing.DefaultSettings(),
		Periods: []pricelist.Period{{Key: "alta", Code: "alta", Name: catalog.I18nText{It: "Alta"}, Kind: pricing.PeriodSeason, Ranges: []pricing.DateRange{
			{From: calendar.New(y, 7, 1), To: calendar.New(y, 8, 31)}, {From: calendar.New(y+1, 7, 1), To: calendar.New(y+1, 8, 31)},
		}}},
	}
	alta := pricing.Scope{Periods: []string{"alta"}}
	n := 0
	add := func(item string, unit pricing.TimeUnit, c int64, scope pricing.Scope) {
		n++
		doc.Rules = append(doc.Rules, pricelist.Rule{Key: "nol-" + strconv.Itoa(n), Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: item + " " + string(unit), ItemID: items[item], Basis: pricing.BasisFlat, Unit: unit,
			AmountCents: cents(c), Scope: scope,
		}})
	}
	band := func(code string, periods ...string) pricing.Scope {
		return pricing.Scope{TimeBands: []string{code}, Periods: periods}
	}
	// item: hour, hour alta, morning, morning alta, afternoon, afternoon alta, day, day alta, week (cents, VAT incl.)
	for _, r := range []struct {
		item string
		p    [9]int64
	}{
		{"NOL-GOM58", [9]int64{4500, 5500, 15000, 18000, 13000, 16000, 24000, 29000, 140000}},
		{"NOL-GOM65", [9]int64{7000, 8500, 22000, 26000, 20000, 24000, 36000, 42000, 210000}},
		{"NOL-OPEN", [9]int64{3500, 4500, 12000, 14000, 10000, 12000, 19000, 23000, 110000}},
		{"NOL-VELA", [9]int64{5000, 6000, 17000, 20000, 15000, 18000, 28000, 34000, 165000}},
		{"NOL-SUP", [9]int64{1000, 1500, 3000, 3500, 3000, 3500, 4500, 5500, 25000}},
	} {
		add(r.item, pricing.UnitHour, r.p[0], pricing.Scope{})
		add(r.item, pricing.UnitHour, r.p[1], alta)
		add(r.item, pricing.UnitHalfDay, r.p[2], band("morning"))
		add(r.item, pricing.UnitHalfDay, r.p[3], band("morning", "alta"))
		add(r.item, pricing.UnitHalfDay, r.p[4], band("afternoon"))
		add(r.item, pricing.UnitHalfDay, r.p[5], band("afternoon", "alta"))
		add(r.item, pricing.UnitDay, r.p[6], pricing.Scope{})
		add(r.item, pricing.UnitDay, r.p[7], alta)
		add(r.item, pricing.UnitWeek, r.p[8], pricing.Scope{})
	}
	add("SKIPPER", pricing.UnitHour, 2500, pricing.Scope{})
	add("SKIPPER", pricing.UnitHalfDay, 10000, pricing.Scope{})
	add("SKIPPER", pricing.UnitDay, 18000, pricing.Scope{})
	add("SKIPPER", pricing.UnitWeek, 100000, pricing.Scope{})
	for _, r := range []struct {
		item string
		c    int64
	}{{"SNORKEL", 1000}, {"FRIGO", 500}, {"RADA", 1500}, {"FUEL-P", 210}, {"LATE-GOM", 6000}, {"DANNI", 100}} {
		add(r.item, pricing.UnitStay, r.c, pricing.Scope{})
	}
	l, err := a.PriceLists.Create(ctx, p, m, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo,
		ContractTypes: []pricing.ContractType{pricing.ContractCharter},
	})
	if err != nil {
		return fmt.Errorf("rental list: %w", err)
	}
	saved, err := a.PriceLists.Save(ctx, p, m, l.ID, l.Version, doc)
	if err != nil {
		return fmt.Errorf("rental list save: %w", err)
	}
	if _, err := a.PriceLists.Activate(ctx, p, m, l.ID, saved.Document.Version, calendar.Date{}); err != nil {
		return fmt.Errorf("rental list activate: %w", err)
	}
	res.PriceLists++
	res.PriceRules += len(doc.Rules)
	return nil
}
