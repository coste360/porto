//go:build integration

package pricelist

import (
	"context"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// env is a backend with the catalogue and price list services, and the items of the tests.
type env struct {
	*fixture.Env
	cat *catalog.Service
	pl  *Service
	trn string // TRN-NTT, berth fee per night, VAT 22 % included
	orm string // ORM-ANN, berth fee per year, VAT 22 % excluded
	srv string // SRV-AUTO, service per night
}

func newEnv(t testing.TB, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	d := Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log}
	en := &env{
		Env: e, pl: NewService(d),
		cat: catalog.NewService(catalog.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log}),
	}
	en.trn = en.item(t, "TRN-NTT", "berth_fee", "night", true, "transit")
	en.orm = en.item(t, "ORM-ANN", "berth_fee", "year", false, "mooring_annual")
	en.srv = en.item(t, "SRV-AUTO", "service", "night", true, "services")
	return en
}

func (e *env) item(t testing.TB, code, typ, unit string, incl bool, class string) string {
	t.Helper()
	ctx := fixture.Ctx()
	classes, err := e.cat.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil {
		t.Fatal(err)
	}
	rc := ""
	for _, c := range classes {
		if c.Code == class {
			rc = c.ID
		}
	}
	it, err := e.cat.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: typ, Unit: unit, PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}

func (e *env) scope(t testing.TB) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func d(s string) calendar.Date { return calendar.MustParse(s) }

func cents(v int64) *money.Cents { c := money.Cents(v); return &c }

func cm(v int64) *geo.Cm { c := geo.Cm(v); return &c }

func ptr[T any](v T) *T { return &v }

// transitDoc is a valid transit + annual document for year y: BASSA/ALTA seasons, bands L10/L12/L14 (open),
// per-metre night rules for transits and a length-band annual rule. Rules carry keys.
func (e *env) transitDoc(code string, y int) SaveRequest {
	from, to := calendar.New(y, 1, 1), calendar.New(y, 12, 31)
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractTransit: e.trn, pricing.ContractAnnual: e.orm}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundNone, pricing.ContractAnnual: pricing.RoundEuro}
	return SaveRequest{
		Code: code, Name: catalog.I18nText{It: "Listino " + code}, ValidFrom: from, ValidTo: to,
		ContractTypes: []pricing.ContractType{pricing.ContractTransit, pricing.ContractAnnual}, Settings: set,
		Periods: []Period{
			{Key: "p-bassa", Code: "BASSA", Name: catalog.I18nText{It: "Bassa"}, Kind: pricing.PeriodSeason, Ranges: []pricing.DateRange{
				{From: from, To: calendar.New(y, 5, 31)}, {From: calendar.New(y, 10, 1), To: to},
			}},
			{Key: "p-alta", Code: "ALTA", Name: catalog.I18nText{It: "Alta"}, Kind: pricing.PeriodSeason, Ranges: []pricing.DateRange{
				{From: calendar.New(y, 6, 1), To: calendar.New(y, 9, 30)},
			}},
		},
		Bands: []Band{
			{Key: "b10", Code: "L10", MaxCm: cm(1000), Category: "L10"},
			{Key: "b12", Code: "L12", MaxCm: cm(1200), Category: "L12"},
			{Key: "b14", Code: "L14"},
		},
		Rules: []Rule{
			{Key: "r-bassa", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Passaggio bassa", ItemID: e.trn, Basis: pricing.BasisPerMeter,
				Unit: pricing.UnitNight, UnitCents: cents(300), Scope: pricing.Scope{Periods: []string{"BASSA"}},
			}},
			{Key: "r-alta", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Passaggio alta", ItemID: e.trn, Basis: pricing.BasisPerMeter,
				Unit: pricing.UnitNight, UnitCents: cents(750), Scope: pricing.Scope{Periods: []string{"ALTA"}},
			}},
			{Key: "r-ann", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Annuale", ItemID: e.orm, Basis: pricing.BasisLengthBand,
				Unit: pricing.UnitYear, Amounts: map[string]money.Cents{"L10": 380000, "L12": 490000, "L14": 620000},
			}},
			{Key: "r-auto", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Auto", ItemID: e.srv, Basis: pricing.BasisFlat,
				Unit: pricing.UnitNight, AmountCents: cents(800),
			}},
		},
	}
}

// createList creates a draft from doc and saves it; it returns the saved document.
func (e *env) createList(t testing.TB, doc SaveRequest) *Document {
	t.Helper()
	ctx := fixture.Ctx()
	l, err := e.pl.Create(ctx, e.Owner, e.MarinaID, CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.pl.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save %s: %v", doc.Code, err)
	}
	return res.Document
}

// activate activates a list from eff (zero = valid_from).
func (e *env) activate(t testing.TB, doc *Document, eff calendar.Date) *ActivateResult {
	t.Helper()
	res, err := e.pl.Activate(fixture.Ctx(), e.Owner, e.MarinaID, doc.ID, doc.Version, eff)
	if err != nil {
		t.Fatalf("activate %s: %v", doc.Code, err)
	}
	return res
}

// docToSave turns a stored document back into a save request (rows by id).
func docToSave(doc *Document) SaveRequest {
	return SaveRequest{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
		Settings: doc.Settings, Periods: append([]Period(nil), doc.Periods...), Bands: append([]Band(nil), doc.Bands...),
		Rules: append([]Rule(nil), doc.Rules...),
	}
}

// addBerth inserts a map (once) and a berth with a price class and dimensions through the store; it returns
// the berth id.
func (e *env) addBerth(t testing.TB, mapID *string, code, class string, length, width, maxLOA, maxBeam int64) string {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	if *mapID == "" {
		m := &store.Map{
			ID: store.NewID(), Kind: "water", Name: store.MustJSON(catalog.I18nText{It: "Acqua"}), Icon: "map", WidthCm: 100000,
			HeightCm: 100000, GridCm: 50, WaterPolygons: store.MustJSON([]any{}), LandPolygons: store.MustJSON([]any{}), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertMap(ctx, e.DB.DB, e.scope(t), m); err != nil {
			t.Fatal(err)
		}
		*mapID = m.ID
	}
	b := &store.Berth{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: *mapID, Code: code, Side: "none", WidthCm: width, LengthCm: length,
		MaxLOACm: maxLOA, MaxBeamCm: maxBeam, MooringType: "mooring_line", PriceClass: class, Attributes: store.Strings{}, Active: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(b).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return b.ID
}

// addCategory registers a berth category.
func (e *env) addCategory(t testing.TB, code string) {
	t.Helper()
	if _, err := e.cat.CreateCategory(fixture.Ctx(), e.Owner, e.MarinaID, catalog.CategoryInput{
		Code: code, Name: catalog.I18nText{It: code}, AppliesTo: []string{"berth"},
	}); err != nil {
		t.Fatal(err)
	}
}
