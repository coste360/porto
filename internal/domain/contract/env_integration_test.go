//go:build integration

package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// tenv is a backend with the real occupancy engine, the price lists of the tests and a small harbour: one map,
// berths A1..A4 (12 m × 4 m), one 60 m linear area and two parkings.
type tenv struct {
	*fixture.Env
	occ    *occupancy.Service
	prices *pricelist.Service
	svc    *Service
	mapID  string
	berths []string
	area   string
	lands  []string
	vessel string // 11.34 m × 3.80 m sailing yacht
	cust   string
	trn    string // transit item
	orm    string // annual item
	list   string // active transit + annual + seasonal + monthly list
}

// t0 is the test clock start (a Wednesday in the transit season).
var t0 = time.Date(2027, 6, 2, 8, 0, 0, 0, time.UTC)

func d(s string) calendar.Date { return calendar.MustParse(s) }

type bunTx = bun.Tx

func newTenv(t *testing.T, db *store.DB) *tenv {
	t.Helper()
	e := fixture.New(t, db)
	e.Clock.Set(t0)
	occ := occupancy.NewService(occupancy.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	prices := pricelist.NewService(pricelist.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	te := &tenv{Env: e, occ: occ, prices: prices}
	te.svc = NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Occupancy: occ, Prices: prices})
	te.harbour(t)
	te.priceList(t)
	te.vessel = te.addVessel(t, "Libeccio", 1134, 380, "monohull_sail")
	te.cust = te.addCustomer(t, "Rossi")
	return te
}

func (e *tenv) scope(t testing.TB) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *tenv) insert(t testing.TB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if _, err := e.DB.NewInsert().Model(r).Exec(context.Background()); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
}

func (e *tenv) harbour(t testing.TB) {
	now := store.UTC(e.Clock.Now())
	mp := &store.Map{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map",
		WidthCm: 100000, HeightCm: 100000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, mp)
	e.mapID = mp.ID
	for i, code := range []string{"A1", "A2", "A3", "A4"} {
		b := &store.Berth{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: mp.ID, Code: code, Side: "none", AnchorXCm: int64(1000 + 500*i),
			AnchorYCm: 1000, WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 400, MooringType: "mooring_line",
			PriceClass: "L12", Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(t, b)
		e.berths = append(e.berths, b.ID)
	}
	from, to := int64(0), int64(6000)
	a := &store.DynamicArea{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: mp.ID, Kind: "linear", Code: "L1", Side: "left", FromCm: &from, ToCm: &to,
		MooringMode: "stern_to", CellCm: 50, MarginCm: 50, Supports: store.Strings{}, DepthProfile: store.JSON(`[]`),
		BlockedCells: store.JSON(`[]`), Exits: store.JSON(`[]`), CellCount: 120, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, a)
	e.area = a.ID
	for i, code := range []string{"P1", "P2"} {
		l := &store.LandResource{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: mp.ID, Kind: "parking", Code: code, XCm: int64(5000 + 400*i), YCm: 5000,
			WidthCm: 250, LengthCm: 500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(t, l)
		e.lands = append(e.lands, l.ID)
	}
}

func (e *tenv) addVessel(t testing.TB, name string, loa, beam int64, hull string) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: e.MarinaID, Name: name, HullType: hull, LOACm: loa, BeamCm: beam,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), SearchText: name, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, v)
	return v.ID
}

func (e *tenv) addCustomer(t testing.TB, name string) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: name, FirstName: "Mario",
		BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
		SearchText: name, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, c)
	return c.ID
}

func (e *tenv) item(t testing.TB, cat *catalog.Service, code, typ, unit, class string) string {
	t.Helper()
	ctx := fixture.Ctx()
	classes, err := cat.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
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
	it, err := cat.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: typ, Unit: unit, PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}

func cents(v int64) *money.Cents { c := money.Cents(v); return &c }

// priceList creates and activates a list for 2027 with transit (3 €/m per night), annual, seasonal and monthly
// (flat) rules.
func (e *tenv) priceList(t testing.TB) {
	t.Helper()
	cat := catalog.NewService(catalog.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	e.trn = e.item(t, cat, "TRN-NTT", "berth_fee", "night", "transit")
	e.orm = e.item(t, cat, "ORM-ANN", "berth_fee", "year", "mooring_annual")
	ctx := fixture.Ctx()
	from, to := d("2027-01-01"), d("2027-12-31")
	types := []pricing.ContractType{pricing.ContractTransit, pricing.ContractAnnual, pricing.ContractSeasonal, pricing.ContractMonthly}
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{
		pricing.ContractTransit: e.trn, pricing.ContractAnnual: e.orm, pricing.ContractSeasonal: e.orm, pricing.ContractMonthly: e.orm,
	}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundNone}
	set.SeasonWindow = &pricing.DateRange{From: d("2027-05-01"), To: d("2027-09-30")}
	doc := pricelist.SaveRequest{
		Code: "L2027", Name: catalog.I18nText{It: "Listino 2027"}, ValidFrom: from, ValidTo: to, ContractTypes: types, Settings: set,
		Rules: []pricelist.Rule{
			{Key: "r-trn", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Transito", ItemID: e.trn, Basis: pricing.BasisPerMeter, Unit: pricing.UnitNight,
				UnitCents: cents(300), Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractTransit}},
			}},
			// Berths of class L15 cost more in transit (move repricing tests).
			{Key: "r-trn-l15", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Transito L15", ItemID: e.trn, Basis: pricing.BasisPerMeter, Unit: pricing.UnitNight,
				UnitCents: cents(500), Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractTransit}, Categories: []string{"L15"}},
			}},
			{Key: "r-ann", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Annuale", ItemID: e.orm, Basis: pricing.BasisFlat, Unit: pricing.UnitYear,
				AmountCents: cents(480000), Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractAnnual}},
			}},
			{Key: "r-sea", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Stagionale", ItemID: e.orm, Basis: pricing.BasisFlat, Unit: pricing.UnitSeason,
				AmountCents: cents(300000), Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractSeasonal}},
			}},
			{Key: "r-mon", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Mensile", ItemID: e.orm, Basis: pricing.BasisFlat, Unit: pricing.UnitMonth,
				AmountCents: cents(60000), Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractMonthly}},
			}},
		},
	}
	l, err := e.prices.Create(ctx, e.Owner, e.MarinaID, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.prices.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save list: %v", err)
	}
	if _, err := e.prices.Activate(ctx, e.Owner, e.MarinaID, l.ID, res.Document.Version, calendar.Date{}); err != nil {
		if ie, ok := errors.AsType[*pricelist.InvalidError](err); ok {
			t.Fatalf("activate list: %+v", ie.Issues)
		}
		t.Fatalf("activate list: %v", err)
	}
	e.list = l.ID
}

func berthSeg(id string, first, last string) Segment {
	return Segment{ResourceType: store.ResourceBerth, ResourceID: id, First: d(first), Last: d(last)}
}

// transit creates a held (or confirmed) transit on a berth for [arrival, departure).
func (e *tenv) transit(t testing.TB, p *auth.Principal, berth, arrival, departure string, confirm bool) *Detail {
	t.Helper()
	in := TransitInput{
		VesselID: e.vessel, CustomerID: e.cust, Arrival: d(arrival), Departure: d(departure),
		Placements: []Segment{berthSeg(berth, arrival, d(departure).AddDays(-1).String())},
	}
	if confirm {
		in.Confirm = &ManualConfirmation{Reference: "cassa 12"}
	}
	c, err := e.svc.CreateTransit(fixture.Ctx(), p, e.MarinaID, in)
	if err != nil {
		t.Fatalf("create transit: %v", err)
	}
	return c
}

// rows counts the occupancy rows of an allocation (any family).
func (e *tenv) rows(t testing.TB, allocID string) int {
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

// counts returns (audit rows of action, outbox events of type).
func (e *tenv) count(t testing.TB, table, col, val string) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr(table).Where("? = ?", bun.Ident(col), val).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// invariants asserts that the invariant queries of m3-plan §6 find nothing.
func (e *tenv) invariants(t testing.TB) {
	t.Helper()
	today := occupancy.Today(e.Clock, time.UTC)
	vs, err := store.CheckOccupancyInvariants(context.Background(), e.DB.DB, e.MarinaID, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("invariant violations: %+v", vs)
	}
}
