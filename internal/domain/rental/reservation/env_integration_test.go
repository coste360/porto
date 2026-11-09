//go:build integration

package reservation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// The clock is 2027-06-10 08:00 UTC (10:00 in Rome); default settings: open 08:00-19:00 (16-38), bands morning
// 16-26 and afternoon 28-38, buffer 1, desk hold 60 minutes.
var t0 = time.Date(2027, 6, 10, 8, 0, 0, 0, time.UTC)

// fakePricer prices 10.00 per slot of the range, plus 5.00 per slot with a skipper, plus 3.00 per extra unit:
// enough to see reprices without the price lists (FLEET owns pricing.RentalQuote).
type fakePricer struct{}

func (fakePricer) SnapshotTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, in fleet.SnapshotRequest) (price.Snapshot, error) {
	n := int64(slots.Count(in.Range))
	total := n * 1000
	lines := []pricing.RentalLine{{Seq: 1, Kind: "unit", Qty: 0, Amount: money.Cents(n * 1000)}}
	if in.SkipperID != "" {
		total += n * 500
		lines = append(lines, pricing.RentalLine{Seq: 2, Kind: "skipper", Amount: money.Cents(n * 500)})
	}
	for _, x := range in.Extras {
		total += int64(x.Qty) / 1000 * 300
	}
	return price.Snapshot{
		UnitID: in.UnitID, FormulaID: in.FormulaID, Start: in.Range.Start.Date, Days: slots.Days(in.Range),
		Slots: int(n), WithSkipper: in.SkipperID != "", EngineVersion: 1,
		Breakdown: pricing.RentalBreakdown{Lines: lines, Total: money.Cents(total), EngineVersion: 1},
	}, nil
}

type renv struct {
	*fixture.Env
	t        testing.TB
	s        *Service
	scope    store.MarinaScope
	item     string
	units    []string
	skippers []string
	hourly   string
	morning  string
	day      string
	multi    string
	withSk   string // hourly, skipper required
	extra    string
	hooks    *recHooks
}

type recHooks struct {
	confirmed, ended, repriced int
}

func (h *recHooks) RentalConfirmedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.RentalBooking) error {
	h.confirmed++
	return nil
}

func (h *recHooks) RentalEndedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.RentalBooking) error {
	h.ended++
	return nil
}

func (h *recHooks) RentalRepricedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *RepriceResult) error {
	h.repriced++
	return nil
}

func d(s string) calendar.Date { return calendar.MustParse(s) }

func r(d0 string, s0 int, d1 string, s1 int) slots.Range {
	return slots.Range{Start: slots.Point{Date: d(d0), Slot: s0}, End: slots.Point{Date: d(d1), Slot: s1}}
}

func newRenv(t testing.TB, db *store.DB, nUnits, nSkippers int) *renv {
	t.Helper()
	fe := fixture.New(t, db)
	fe.Clock.Set(t0)
	scope, err := store.ResolveMarinaScope(context.Background(), db.DB, fe.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	e := &renv{Env: fe, t: t, scope: scope, hooks: &recHooks{}}
	e.s = NewService(Deps{DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log, Keys: fe.Keys, Notify: fe.Notify, Pricer: fakePricer{}})
	e.s.SetHooks(e.hooks)
	ctx := fixture.Ctx()
	cat := catalog.NewService(catalog.Deps{DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Log: fe.Log})
	classes, err := cat.ListRevenueClasses(ctx, fe.Owner, fe.MarinaID, false)
	if err != nil {
		t.Fatal(err)
	}
	rc := ""
	for _, c := range classes {
		if c.Code == "rental" {
			rc = c.ID
		}
	}
	incl := true
	it, err := cat.CreateItem(ctx, fe.Owner, fe.MarinaID, catalog.ItemInput{
		Code: "NOL-GOM", Name: catalog.I18nText{It: "Gommone"}, ItemType: "service", Unit: "hour", PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.item = it.ID
	now := store.UTC(fe.Clock.Now())
	for i := range nUnits {
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: fe.MarinaID, Name: fmt.Sprintf("Gommone %d", i), HullType: "rib", LOACm: 580, BeamCm: 240,
			DimensionsSource: "declared", Engines: store.JSON(`[{"power_kw":40}]`), SearchText: "gommone", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(v)
		u := &store.RentalUnit{
			ID: store.NewID(), MarinaID: fe.MarinaID, VesselID: v.ID, Code: fmt.Sprintf("GOM%d", i), Category: store.RentalCategoryRIB, Seats: 6,
			Licence: store.RentalLicenceThreshold, FuelType: store.RentalFuelPetrol, TankMilli: 60000, DepositCents: 30000,
			Status: store.RentalUnitAvailable, ItemID: it.ID, Ownership: store.RentalOwnershipMarina, Sort: i, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(u)
		e.units = append(e.units, u.ID)
	}
	for i := range nSkippers {
		sk := &store.RentalSkipper{
			ID: store.NewID(), MarinaID: fe.MarinaID, Name: fmt.Sprintf("Skipper %d", i), Active: true, Sort: i, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		e.insert(sk)
		e.skippers = append(e.skippers, sk.ID)
	}
	formula := func(code, kind, band, skipper string, minDays int) string {
		f := &store.RentalFormula{
			ID: store.NewID(), MarinaID: fe.MarinaID, Code: code, Name: store.JSON(`{"it":"` + code + `"}`), Kind: kind, BandCode: band,
			MinSlots: 2, MinDays: minDays, MaxDays: 30, Skipper: skipper, Fuel: store.RentalFuelIncluded, GraceMinutes: 15,
			LateStepMinutes: 30, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.insert(f)
		return f.ID
	}
	e.hourly = formula("ORARIA", store.RentalKindHourly, "", store.RentalSkipperOptional, 1)
	e.morning = formula("MEZZA-MATTINA", store.RentalKindHalfDay, "morning", store.RentalSkipperOptional, 1)
	e.day = formula("GIORNATA", store.RentalKindDay, "", store.RentalSkipperOptional, 1)
	e.multi = formula("PIU-GIORNI", store.RentalKindMultiDay, "", store.RentalSkipperOptional, 2)
	e.withSk = formula("ORARIA-SK", store.RentalKindHourly, "", store.RentalSkipperRequired, 1)
	x := &store.RentalExtra{
		ID: store.NewID(), MarinaID: fe.MarinaID, ItemID: it.ID, ChargeMode: store.RentalExtraPerRental, DefaultQtyMilli: 1000,
		MaxQtyMilli: 5000, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.insert(x)
	e.extra = x.ID
	return e
}

func (e *renv) insert(rows ...any) {
	e.t.Helper()
	for _, row := range rows {
		if _, err := e.DB.NewInsert().Model(row).Exec(context.Background()); err != nil {
			e.t.Fatalf("insert %T: %v", row, err)
		}
	}
}

func (e *renv) create(in CreateInput) (*BookingView, error) {
	if in.FormulaID == "" {
		in.FormulaID = e.hourly
	}
	if in.UnitID == "" {
		in.UnitID = e.units[0]
	}
	return e.s.Create(fixture.Ctx(), e.Owner, e.MarinaID, in)
}

func (e *renv) mustCreate(in CreateInput) *BookingView {
	e.t.Helper()
	b, err := e.create(in)
	if err != nil {
		e.t.Fatalf("create: %v", err)
	}
	return b
}

func (e *renv) get(id string) *BookingView {
	e.t.Helper()
	b, err := e.s.Get(fixture.Ctx(), e.Owner, e.MarinaID, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *renv) transition(id string, in TransitionInput) (*BookingView, error) {
	b, err := e.s.Get(fixture.Ctx(), e.Owner, e.MarinaID, id)
	if err != nil {
		return nil, err
	}
	return e.s.Transition(fixture.Ctx(), e.Owner, e.MarinaID, id, b.Version, in)
}

// q32 runs the rental store invariant queries (Q32 INV-19, Q33a, B60). Q33b (closed booking charged = snapshot) is
// skipped: this env records the handover hooks, so no charge is ever created; the handover and acceptance tests run
// it with real charges.
func (e *renv) q32() {
	e.t.Helper()
	vs, err := store.CheckRentalInvariants(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, v := range vs {
		if v.Query == "Q33" && strings.HasPrefix(v.Detail, "main+adjustments") {
			continue
		}
		e.t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func (e *renv) rows(bookingID string) (unit, skipper int) {
	e.t.Helper()
	u, s, err := store.CountRentalBookingSlots(context.Background(), e.DB.DB, e.scope, bookingID)
	if err != nil {
		e.t.Fatal(err)
	}
	return u, s
}

func (e *renv) withTx(fn func(ctx context.Context, tx bun.Tx) error) error {
	return e.DB.RunInTx(fixture.Ctx(), fn)
}

func (e *renv) eventChanges(typ string) []string {
	e.t.Helper()
	var out []string
	for _, ev := range e.Events(e.t, typ) {
		var data struct {
			Data struct {
				Change string `json:"change"`
			} `json:"data"`
			Change string `json:"change"`
		}
		_ = json.Unmarshal(ev.Payload, &data)
		c := data.Change
		if c == "" {
			c = data.Data.Change
		}
		out = append(out, c)
	}
	return out
}

func (e *renv) setUnitStatus(id, status string) {
	e.t.Helper()
	if _, err := e.DB.NewUpdate().TableExpr("rental_units").Set("status = ?", status).Where("id = ?", id).Exec(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *renv) rentalUser() *auth.Principal {
	return e.AddUser(e.t, "rental@porto.test", auth.RoleRental, e.OrgID, e.MarinaID)
}
