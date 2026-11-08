//go:build integration

package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// senv is a backend with a shed (20 m × 10 m, cells of 1 m, margin 0, a 4 m door on the left side, rows 3..6),
// a storage price list for 2027, a customer and boats.
type senv struct {
	*fixture.Env
	occ    *occupancy.Service
	ctr    *contract.Service
	svc    *Service
	shed   string // area id
	shedMp string
	trail  string // trailer spot
	cradle string // marina cradle
	cust   string
}

var t0 = time.Date(2027, 10, 1, 8, 0, 0, 0, time.UTC)

func d(s string) calendar.Date { return calendar.MustParse(s) }

func newSenv(t *testing.T, db *store.DB) *senv {
	t.Helper()
	e := fixture.New(t, db)
	e.Clock.Set(t0)
	occ := occupancy.NewService(occupancy.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	prices := pricelist.NewService(pricelist.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	ctr := contract.NewService(contract.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Occupancy: occ, Prices: prices})
	se := &senv{Env: e, occ: occ, ctr: ctr}
	se.svc = NewService(Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Occupancy: occ, Contracts: ctr, Prices: prices,
		OptimizeBudget: 5 * time.Second,
	})
	se.harbour(t)
	se.priceList(t, prices)
	se.cust = se.addCustomer(t)
	return se
}

func (e *senv) scope(t testing.TB) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *senv) insert(t testing.TB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if _, err := e.DB.NewInsert().Model(r).Exec(context.Background()); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
}

// addShed inserts a cols × rows shed area (cells of 1 m) with a door on the left side, rows r0..r1.
func (e *senv) addShed(t testing.TB, mapID, code string, cols, rows, r0, r1 int, supports store.Strings, margin int64) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	w, h := int64(cols)*100, int64(rows)*100
	outline := fmt.Sprintf(`[{"x":0,"y":0},{"x":%d,"y":0},{"x":%d,"y":%d},{"x":0,"y":%d}]`, w, w, h, h)
	var exits []int
	for r := r0; r <= r1; r++ {
		exits = append(exits, r*cols)
	}
	a := &store.DynamicArea{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: mapID, Kind: "surface", Code: code, Outline: store.JSON(outline),
		CellCm: 100, MarginCm: margin, Supports: supports, DepthProfile: store.JSON(`[]`), BlockedCells: store.JSON(`[]`),
		Exits: store.MustJSON(exits), CellCount: int64(cols * rows), PriceClass: "shed", Active: true, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, a)
	return a.ID
}

func (e *senv) harbour(t testing.TB) {
	now := store.UTC(e.Clock.Now())
	mp := &store.Map{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "shed", Name: store.JSON(`{"it":"Capannone"}`), Icon: "warehouse",
		WidthCm: 5000, HeightCm: 5000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, mp)
	e.shedMp = mp.ID
	e.shed = e.addShed(t, mp.ID, "C1", 20, 10, 3, 6, store.Strings{"cradle", "trailer"}, 0)
	for i, kind := range []string{"trailer_spot", "cradle"} {
		l := &store.LandResource{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: mp.ID, Kind: kind, Code: fmt.Sprintf("L%d", i+1), XCm: 3000, YCm: int64(100 + 600*i),
			WidthCm: 400, LengthCm: 1500, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(t, l)
		if kind == "cradle" {
			e.cradle = l.ID
		} else {
			e.trail = l.ID
		}
	}
}

var vesselN int

func (e *senv) addVessel(t testing.TB, loa, beam int64) string {
	t.Helper()
	vesselN++
	now := store.UTC(e.Clock.Now())
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: e.MarinaID, Name: fmt.Sprintf("Barca %d", vesselN), HullType: "monohull_motor", LOACm: loa, BeamCm: beam,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), SearchText: "barca", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, v)
	return v.ID
}

func (e *senv) addCustomer(t testing.TB) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: "Rossi", FirstName: "Mario",
		BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
		SearchText: "rossi", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(t, c)
	return c.ID
}

func (e *senv) priceList(t testing.TB, prices *pricelist.Service) {
	t.Helper()
	ctx := fixture.Ctx()
	cat := catalog.NewService(catalog.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	classes, err := cat.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil {
		t.Fatal(err)
	}
	incl := true
	it, err := cat.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: "RIM-MESE", Name: catalog.I18nText{It: "Rimessaggio"}, ItemType: "berth_fee", Unit: "month", PriceIncludesVAT: &incl,
		RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractStorage: it.ID}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundNone}
	set.SeasonWindow = &pricing.DateRange{From: d("2027-10-01"), To: d("2028-05-31")}
	amount, yardExtra := money.Cents(20000), money.Cents(10000)
	types := []pricing.ContractType{pricing.ContractStorage}
	doc := pricelist.SaveRequest{
		Code: "R2027", Name: catalog.I18nText{It: "Rimessaggio"}, ValidFrom: d("2027-01-01"), ValidTo: d("2028-12-31"),
		ContractTypes: types, Settings: set,
		Rules: []pricelist.Rule{{Key: "r-rim", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Rimessaggio", ItemID: it.ID, Basis: pricing.BasisFlat, Unit: pricing.UnitMonth,
			AmountCents: &amount, Scope: pricing.Scope{ContractTypes: types},
		}}, {Key: "r-yard", Rule: pricing.Rule{
			// An uncovered yard (price class "yard") costs 100 € a month: the most specific base rule wins over the shed one.
			Stage: pricing.StageBase, Name: "Piazzale", ItemID: it.ID, Basis: pricing.BasisFlat, Unit: pricing.UnitMonth,
			AmountCents: &yardExtra, Scope: pricing.Scope{ContractTypes: types, Categories: []string{"yard"}},
		}}},
	}
	l, err := prices.Create(ctx, e.Owner, e.MarinaID, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := prices.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save list: %v", err)
	}
	if _, err := prices.Activate(ctx, e.Owner, e.MarinaID, l.ID, res.Document.Version, calendar.Date{}); err != nil {
		if ie, ok := errors.AsType[*pricelist.InvalidError](err); ok {
			t.Fatalf("activate list: %+v", ie.Issues)
		}
		t.Fatalf("activate list: %v", err)
	}
}

// contract creates a storage contract in the shed (nil placement = automatic).
func (e *senv) contract(t testing.TB, p *auth.Principal, vessel string, start, end string, at *optimizer.Placement, launch string) *ContractView {
	t.Helper()
	v, err := e.create(p, vessel, start, end, at, launch)
	if err != nil {
		t.Fatalf("create storage contract: %v", err)
	}
	return v
}

func (e *senv) create(p *auth.Principal, vessel string, start, end string, at *optimizer.Placement, launch string) (*ContractView, error) {
	ends := d(end)
	in := ContractInput{
		Term: contract.TermPeriod, CustomerID: e.cust, VesselID: vessel, StartsOn: d(start), EndsOn: &ends, AreaID: e.shed,
		Placement: at, Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
	}
	if launch != "" {
		l := d(launch)
		in.Details.LaunchOn = &l
	}
	return e.svc.CreateContract(fixture.Ctx(), p, e.MarinaID, in)
}

func pl(x, y int64, rot int64) *optimizer.Placement {
	return &optimizer.Placement{X: geo.Cm(x * 100), Y: geo.Cm(y * 100), Rotation: geo.Angle(rot)}
}

// state is a fingerprint of the real state: allocations and occupancy rows.
func (e *senv) state(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	var allocs []struct {
		ID      string `bun:"id"`
		Status  string `bun:"status"`
		Version int    `bun:"version"`
		X       *int64 `bun:"x_cm"`
		Y       *int64 `bun:"y_cm"`
		Last    string `bun:"last_night"`
	}
	if err := e.DB.NewSelect().TableExpr("allocations").ColumnExpr("id, status, version, x_cm, y_cm, CAST(last_night AS TEXT) AS last_night").
		Where("marina_id = ?", e.MarinaID).OrderExpr("id").Scan(ctx, &allocs); err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, a := range allocs {
		out += fmt.Sprintf("%s %s %d %v %v %s;", a.ID, a.Status, a.Version, deref(a.X), deref(a.Y), a.Last)
	}
	var cells []struct {
		Alloc string `bun:"allocation_id"`
		N     int    `bun:"n"`
	}
	if err := e.DB.NewSelect().TableExpr("cell_occupancies").ColumnExpr("allocation_id, COUNT(*) AS n").Where("marina_id = ?", e.MarinaID).
		GroupExpr("allocation_id").OrderExpr("allocation_id").Scan(ctx, &cells); err != nil {
		t.Fatal(err)
	}
	for _, c := range cells {
		out += fmt.Sprintf("%s=%d;", c.Alloc, c.N)
	}
	return out
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (e *senv) checkInvariants(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	vs, err := store.CheckStorageInvariants(ctx, e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	ov, err := store.CheckOccupancyInvariants(ctx, e.DB.DB, e.MarinaID, d("2027-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(vs)+len(ov) > 0 {
		t.Fatalf("invariants: %+v %+v", vs, ov)
	}
}
