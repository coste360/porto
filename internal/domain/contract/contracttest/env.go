// Package contracttest builds a backend with the occupancy engine, an active price list and a small harbour for
// the integration tests of the presence service and the M3 handlers (m3-plan §9.3). Test support only.
package contracttest

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// T0 is the clock start of the environment (a Wednesday in the transit season, 10:00 in Rome).
var T0 = time.Date(2027, 6, 2, 8, 0, 0, 0, time.UTC)

// Env is the backend: services, one map with berths A1..A4 (12 × 4 m), a 60 m linear area L1, parkings P1 and
// P2, a vessel (11.34 × 3.80 m), a customer and an active 2027 list (transit 3 €/m per night, annual, seasonal,
// monthly).
type Env struct {
	*fixture.Env
	Occupancy *occupancy.Service
	Prices    *pricelist.Service
	Catalog   *catalog.Service
	Contracts *contract.Service
	Presence  *presence.Service
	Files     *filestore.Store

	MapID    string
	Berths   []string
	Area     string
	Lands    []string
	Vessel   string
	Customer string
	ListID   string
}

// D parses a date.
func D(s string) calendar.Date { return calendar.MustParse(s) }

// New builds the environment on db.
func New(t testing.TB, db *store.DB) *Env {
	t.Helper()
	fe := fixture.New(t, db)
	fe.Clock.Set(T0)
	e := &Env{Env: fe}
	files := filestore.New(t.TempDir(), fe.Keys)
	e.Files = files
	e.Occupancy = occupancy.NewService(occupancy.Deps{DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log})
	e.Prices = pricelist.NewService(pricelist.Deps{DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log})
	e.Catalog = catalog.NewService(catalog.Deps{DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log})
	e.Contracts = contract.NewService(contract.Deps{
		DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log, Occupancy: e.Occupancy, Prices: e.Prices,
	})
	e.Presence = presence.NewService(presence.Deps{
		DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Keys: fe.Keys, Files: files, Log: fe.Log,
		Contracts: e.Contracts, Occupancy: e.Occupancy,
	})
	e.harbour(t)
	e.priceList(t)
	e.Vessel = e.AddVessel(t, "Libeccio", 1134, 380, "monohull_sail")
	e.Customer = e.AddCustomer(t, "Rossi")
	return e
}

// Scope resolves the marina scope.
func (e *Env) Scope(t testing.TB) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Insert inserts rows through Bun.
func (e *Env) Insert(t testing.TB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if _, err := e.DB.NewInsert().Model(r).Exec(context.Background()); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
}

func (e *Env) harbour(t testing.TB) {
	h := InsertHarbour(t, e.DB, e.MarinaID, store.UTC(e.Clock.Now()), 4)
	e.MapID, e.Berths, e.Area, e.Lands = h.MapID, h.Berths, h.Area, h.Lands
}

// Harbour is what InsertHarbour wrote.
type Harbour struct {
	MapID  string
	Berths []string // A1..An, 12 × 4 m, price class L12
	Area   string   // L1: linear, 60 m, cells of 50 cm, stern-to
	Lands  []string // parkings P1, P2
}

// InsertHarbour inserts a water map with n berths, a linear area and two parkings into a marina through the
// store (test support for packages that may not import internal/store, like the HTTP handler tests).
func InsertHarbour(t testing.TB, db *store.DB, marinaID string, now time.Time, n int) Harbour {
	t.Helper()
	ins := func(rows ...any) {
		for _, r := range rows {
			if _, err := db.NewInsert().Model(r).Exec(context.Background()); err != nil {
				t.Fatalf("insert %T: %v", r, err)
			}
		}
	}
	var h Harbour
	mp := &store.Map{
		ID: store.NewID(), MarinaID: marinaID, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map",
		WidthCm: 100000, HeightCm: 100000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	ins(mp)
	h.MapID = mp.ID
	for i := range n {
		b := &store.Berth{
			ID: store.NewID(), MarinaID: marinaID, MapID: mp.ID, Code: "A" + strconv.Itoa(i+1), Side: "none", AnchorXCm: int64(1000 + 500*i),
			AnchorYCm: 1000, WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 400, MooringType: "mooring_line",
			PriceClass: "L12", Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		ins(b)
		h.Berths = append(h.Berths, b.ID)
	}
	from, to := int64(0), int64(6000)
	a := &store.DynamicArea{
		ID: store.NewID(), MarinaID: marinaID, MapID: mp.ID, Kind: "linear", Code: "L1", Side: "left", FromCm: &from, ToCm: &to,
		MooringMode: "stern_to", CellCm: 50, MarginCm: 50, Supports: store.Strings{}, DepthProfile: store.JSON(`[]`),
		BlockedCells: store.JSON(`[]`), Exits: store.JSON(`[]`), CellCount: 120, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	ins(a)
	h.Area = a.ID
	for i, code := range []string{"P1", "P2"} {
		l := &store.LandResource{
			ID: store.NewID(), MarinaID: marinaID, MapID: mp.ID, Kind: "parking", Code: code, XCm: int64(5000 + 400*i), YCm: 5000,
			WidthCm: 250, LengthCm: 500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		ins(l)
		h.Lands = append(h.Lands, l.ID)
	}
	return h
}

// AddVessel inserts a vessel with declared dimensions.
func (e *Env) AddVessel(t testing.TB, name string, loa, beam int64, hull string) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: e.MarinaID, Name: name, HullType: hull, LOACm: loa, BeamCm: beam,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), SearchText: name, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, v)
	return v.ID
}

// AddCustomer inserts a person.
func (e *Env) AddCustomer(t testing.TB, name string) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: name, FirstName: "Mario",
		BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
		SearchText: name, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, c)
	return c.ID
}

func (e *Env) item(t testing.TB, code, typ, unit, class string) string {
	t.Helper()
	ctx := fixture.Ctx()
	classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil {
		t.Fatal(err)
	}
	rc := ""
	for _, c := range classes {
		if c.Code == class {
			rc = c.ID
		}
	}
	incl := true
	it, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: typ, Unit: unit, PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}

func cents(v int64) *money.Cents { c := money.Cents(v); return &c }

func (e *Env) priceList(t testing.TB) {
	t.Helper()
	trn := e.item(t, "TRN-NTT", "berth_fee", "night", "transit")
	orm := e.item(t, "ORM-ANN", "berth_fee", "year", "mooring_annual")
	ctx := fixture.Ctx()
	types := []pricing.ContractType{pricing.ContractTransit, pricing.ContractAnnual, pricing.ContractSeasonal, pricing.ContractMonthly}
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{
		pricing.ContractTransit: trn, pricing.ContractAnnual: orm, pricing.ContractSeasonal: orm, pricing.ContractMonthly: orm,
	}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundNone}
	set.SeasonWindow = &pricing.DateRange{From: D("2027-05-01"), To: D("2027-09-30")}
	rule := func(key, name, item string, basis pricing.Basis, unit pricing.TimeUnit, ct pricing.ContractType, unitCents, amount *money.Cents) pricelist.Rule {
		return pricelist.Rule{Key: key, Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: name, ItemID: item, Basis: basis, Unit: unit, UnitCents: unitCents, AmountCents: amount,
			Scope: pricing.Scope{ContractTypes: []pricing.ContractType{ct}},
		}}
	}
	doc := pricelist.SaveRequest{
		Code: "L2027", Name: catalog.I18nText{It: "Listino 2027"}, ValidFrom: D("2027-01-01"), ValidTo: D("2027-12-31"),
		ContractTypes: types, Settings: set,
		Rules: []pricelist.Rule{
			rule("r-trn", "Transito", trn, pricing.BasisPerMeter, pricing.UnitNight, pricing.ContractTransit, cents(300), nil),
			rule("r-ann", "Annuale", orm, pricing.BasisFlat, pricing.UnitYear, pricing.ContractAnnual, nil, cents(480000)),
			rule("r-sea", "Stagionale", orm, pricing.BasisFlat, pricing.UnitSeason, pricing.ContractSeasonal, nil, cents(300000)),
			rule("r-mon", "Mensile", orm, pricing.BasisFlat, pricing.UnitMonth, pricing.ContractMonthly, nil, cents(60000)),
		},
	}
	l, err := e.Prices.Create(ctx, e.Owner, e.MarinaID, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Prices.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save list: %v", err)
	}
	if _, err := e.Prices.Activate(ctx, e.Owner, e.MarinaID, l.ID, res.Document.Version, calendar.Date{}); err != nil {
		if ie, ok := errors.AsType[*pricelist.InvalidError](err); ok {
			t.Fatalf("activate list: %+v", ie.Issues)
		}
		t.Fatalf("activate list: %v", err)
	}
	e.ListID = l.ID
}

// BerthSeg is a berth segment for [first, last].
func BerthSeg(id, first, last string) contract.Segment {
	return contract.Segment{ResourceType: store.ResourceBerth, ResourceID: id, First: D(first), Last: D(last)}
}

// Transit creates a transit of the environment's vessel and customer on a berth for [arrival, departure).
func (e *Env) Transit(t testing.TB, berth, arrival, departure string, confirm bool) *contract.Detail {
	t.Helper()
	in := contract.TransitInput{
		VesselID: e.Vessel, CustomerID: e.Customer, Arrival: D(arrival), Departure: D(departure),
		Placements: []contract.Segment{BerthSeg(berth, arrival, D(departure).AddDays(-1).String())},
	}
	if confirm {
		in.Confirm = &contract.ManualConfirmation{Reference: "cassa"}
	}
	c, err := e.Contracts.CreateTransit(fixture.Ctx(), e.Owner, e.MarinaID, in)
	if err != nil {
		t.Fatalf("create transit: %v", err)
	}
	return c
}

// Rows counts the occupancy rows of an allocation.
func (e *Env) Rows(t testing.TB, allocID string) int {
	t.Helper()
	total := 0
	for _, table := range []string{"berth_occupancies", "cell_occupancies", "land_occupancies"} {
		n, err := e.DB.NewSelect().TableExpr(table).Where("allocation_id = ?", allocID).Count(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		total += int(n)
	}
	return total
}
