//go:build integration

package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// fenv is the backend of the fleet tests: contracttest's marina plus rental items, the §5.3 price list (2027), a
// 40 kW vessel and the fleet service. The clock is 2027-06-02.
type fenv struct {
	*contracttest.Env
	s     *Service
	scope store.MarinaScope
	items map[string]string
	boat  string
}

func newFenv(t *testing.T, db *store.DB) *fenv {
	t.Helper()
	e := contracttest.New(t, db)
	f := &fenv{Env: e, scope: e.Scope(t), items: map[string]string{}}
	f.s = NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Keys: e.Keys, Prices: e.Prices})
	ctx := fixture.Ctx()
	classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, true)
	if err != nil {
		t.Fatal(err)
	}
	class := map[string]string{}
	for _, c := range classes {
		class[c.Code] = c.ID
	}
	vat, incl := int64(2200), true
	for _, it := range []struct{ code, class string }{
		{"NOL-GOM58", "rental"}, {"SKIPPER", "rental"}, {"SNORKEL", "rental"}, {"FRIGO", "rental"}, {"SRV-X", "services"},
	} {
		row, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.code}, ItemType: "service", Unit: "hour", VATRateBP: &vat,
			PriceIncludesVAT: &incl, RevenueClassID: class[it.class],
		})
		if err != nil {
			t.Fatalf("item %s: %v", it.code, err)
		}
		f.items[it.code] = row.ID
	}
	cents := func(c int64) *money.Cents { v := money.Cents(c); return &v }
	doc := pricelist.SaveRequest{
		Code: "NOL-2027", Name: catalog.I18nText{It: "Noleggio"}, ValidFrom: calendar.MustParse("2027-01-01"),
		ValidTo: calendar.MustParse("2027-12-31"), ContractTypes: []pricing.ContractType{}, Settings: pricing.DefaultSettings(),
		Periods: []pricelist.Period{{Key: "alta", Code: "alta", Name: catalog.I18nText{It: "Alta"}, Kind: pricing.PeriodSeason, Ranges: []pricing.DateRange{{From: calendar.MustParse("2027-07-01"), To: calendar.MustParse("2027-08-31")}}}},
	}
	n := 0
	add := func(item string, unit pricing.TimeUnit, c int64, scope pricing.Scope) {
		n++
		doc.Rules = append(doc.Rules, pricelist.Rule{Key: "r" + string(rune('a'+n)), Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: string(unit), ItemID: f.items[item], Basis: pricing.BasisFlat, Unit: unit, AmountCents: cents(c), Scope: scope,
		}})
	}
	add("NOL-GOM58", pricing.UnitHour, 4500, pricing.Scope{})
	add("NOL-GOM58", pricing.UnitHour, 5500, pricing.Scope{Periods: []string{"alta"}})
	add("NOL-GOM58", pricing.UnitHalfDay, 15000, pricing.Scope{TimeBands: []string{"morning"}})
	add("NOL-GOM58", pricing.UnitDay, 24000, pricing.Scope{})
	add("NOL-GOM58", pricing.UnitDay, 29000, pricing.Scope{Periods: []string{"alta"}})
	add("SKIPPER", pricing.UnitHalfDay, 10000, pricing.Scope{})
	add("SNORKEL", pricing.UnitStay, 1000, pricing.Scope{})
	add("FRIGO", pricing.UnitStay, 500, pricing.Scope{})
	l, err := e.Prices.Create(ctx, e.Owner, e.MarinaID, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: []pricing.ContractType{pricing.ContractCharter},
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := e.Prices.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save list: %v", err)
	}
	if _, err := e.Prices.Activate(ctx, e.Owner, e.MarinaID, l.ID, saved.Document.Version, calendar.Date{}); err != nil {
		t.Fatalf("activate list: %v", err)
	}
	f.boat = e.AddVessel(t, "Gabbiano", 580, 240, "rib")
	if _, err := db.NewUpdate().Table("vessels").Set("engines = ?", `[{"power_kw":40}]`).Where("id = ?", f.boat).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fenv) unit(t *testing.T, code string) *UnitView {
	t.Helper()
	u, err := f.s.CreateUnit(fixture.Ctx(), f.Owner, f.MarinaID, UnitInput{
		VesselID: f.boat, Code: code, Category: store.RentalCategoryRIB, Seats: 8, FuelType: store.RentalFuelPetrol,
		Tank: 60_000, Deposit: 50_000, ItemID: f.items["NOL-GOM58"],
	})
	if err != nil {
		t.Fatalf("unit: %v", err)
	}
	return u
}

func isField(err error, field string) bool {
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		return false
	}
	for _, fe := range v.Fields {
		if fe.Field == field {
			return true
		}
	}
	return false
}

func TestFleet_UnitsAndPermissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := newFenv(t, db)
		ctx := fixture.Ctx()
		u := f.unit(t, "GOM58")
		if !u.LicenceRequired || u.EngineKWMilli != 40_000 || u.VesselName != "Gabbiano" || u.Version != 1 {
			t.Fatalf("unit %+v", u)
		}
		if _, err := f.s.CreateUnit(ctx, f.Owner, f.MarinaID, UnitInput{
			VesselID: f.boat, Code: "GOM59", Category: "rib", Seats: 8, FuelType: "petrol", Tank: 1, ItemID: f.items["NOL-GOM58"],
		}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("same vessel: %v", err)
		}
		_, err := f.s.CreateUnit(ctx, f.Owner, f.MarinaID, UnitInput{
			VesselID: f.Vessel, Code: "X1", Category: "rib", Seats: 2, FuelType: "petrol", ItemID: f.items["SRV-X"], Ownership: "third_party",
		})
		for _, field := range []string{"item_id", "tank_milli", "owner_customer_id"} {
			if !isField(err, field) {
				t.Fatalf("want %s in %v", field, err)
			}
		}
		rental := f.AddUser(t, "rental@porto.test", auth.RoleRental, f.OrgID, f.MarinaID)
		dock := f.AddUser(t, "dock@porto.test", auth.RoleDock, f.OrgID, f.MarinaID)
		if _, err := f.s.GetUnit(ctx, rental, f.MarinaID, u.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.ListUnits(ctx, dock, f.MarinaID, UnitFilter{}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock: %v", err)
		}
		if _, err := f.s.CreateFormula(ctx, rental, f.MarinaID, FormulaInput{Code: "X", Name: map[string]string{"it": "X"}, Kind: "day"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("rental configures formulas: %v", err)
		}
		_, otherM, other := f.OtherOrganization(t)
		if _, err := f.s.GetUnit(ctx, other, f.MarinaID, u.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other org: %v", err)
		}
		if _, err := f.s.GetUnit(ctx, other, otherM, u.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina id: %v", err)
		}
		st := store.RentalUnitMaintenance
		u2, err := f.s.UpdateUnit(ctx, rental, f.MarinaID, u.ID, 1, UnitPatch{Status: &st})
		if err != nil || u2.Status != st || u2.Version != 2 {
			t.Fatalf("update: %+v %v", u2, err)
		}
		if _, err := f.s.UpdateUnit(ctx, rental, f.MarinaID, u.ID, 1, UnitPatch{Status: &st}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		berth, empty := f.Berths[0], ""
		if u2, err = f.s.UpdateUnit(ctx, rental, f.MarinaID, u.ID, 2, UnitPatch{HomeBerthID: &berth}); err != nil || u2.HomeBerthCode != "A1" {
			t.Fatalf("home berth %+v %v", u2, err)
		}
		if u2, err = f.s.UpdateUnit(ctx, rental, f.MarinaID, u.ID, 3, UnitPatch{HomeBerthID: &empty}); err != nil || u2.HomeBerthID != "" {
			t.Fatalf("clear home berth %+v %v", u2, err)
		}
		if n := f.AuditCount(t, "rental_unit.update"); n != 3 {
			t.Fatalf("audit %d", n)
		}
	})
}

func TestFleet_SkipperContactEncrypted(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := newFenv(t, db)
		ctx := fixture.Ctx()
		k, err := f.s.CreateSkipper(ctx, f.Owner, f.MarinaID, SkipperInput{Name: "Marco Bianchi", Phone: "+39 333 7654321", Email: "marco@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		if k.Phone != "+39 333 7654321" || k.Email != "marco@example.test" || !k.Active {
			t.Fatalf("skipper %+v", k)
		}
		row, _ := store.GetRentalSkipper(ctx, db.DB, f.scope, k.ID)
		if row.ContactEnc == "" || strings.Contains(row.ContactEnc, "7654321") {
			t.Fatalf("contact not encrypted: %q", row.ContactEnc)
		}
		var diffs []string
		if err := db.NewSelect().Table("audit_log").Column("diff").Where("entity_type = ?", "rental_skipper").Scan(ctx, &diffs); err != nil {
			t.Fatal(err)
		}
		for _, d := range diffs {
			if strings.Contains(d, "Marco") || strings.Contains(d, "7654321") || strings.Contains(d, "example.test") {
				t.Fatalf("PII in audit: %s", d)
			}
		}
		k, err = f.s.PutSkipperHours(ctx, f.Owner, f.MarinaID, k.ID, 1, []HoursRow{{Weekday: 1, FromSlot: 16, ToSlot: 38}, {Weekday: 6, FromSlot: 16, ToSlot: 26}})
		if err != nil || len(k.Hours) != 2 || k.Version != 2 {
			t.Fatalf("hours %+v %v", k, err)
		}
		if _, err := f.s.PutSkipperHours(ctx, f.Owner, f.MarinaID, k.ID, 1, nil); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale hours: %v", err)
		}
		k, err = f.s.PutSkipperHours(ctx, f.Owner, f.MarinaID, k.ID, 2, nil)
		if err != nil || len(k.Hours) != 0 {
			t.Fatalf("clear hours %+v %v", k, err)
		}
	})
}

func TestFleet_SettingsAndFormulas(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := newFenv(t, db)
		ctx := fixture.Ctx()
		set, err := f.s.GetSettings(ctx, f.Owner, f.MarinaID)
		if err != nil || set.Row.Version != 0 || len(set.Bands) != 2 || len(set.Checklist) != 8 {
			t.Fatalf("defaults %+v %v", set, err)
		}
		in := SettingsInput{
			OpenFromSlot: 16, OpenToSlot: 38, Bands: set.Bands, BufferSlots: 1, DeskHoldMinutes: 60, NoShowAfterMinutes: 60,
			LicenceThresholdKWMilli: 30_000, SkipperItemID: f.items["SKIPPER"], Checklist: set.Checklist, CheckoutMinPhotos: 2,
			CheckinMinPhotos: 1, DepositModes: []string{"cash", "card_pos"}, IdentityRetentionDays: 365,
		}
		gap := in
		gap.BufferSlots = 3
		if _, err := f.s.PutSettings(ctx, f.Owner, f.MarinaID, 0, gap); !errors.Is(err, ErrBandGapLtBuffer) {
			t.Fatalf("gap: %v", err)
		}
		dst := in
		dst.OpenFromSlot = 4
		if _, err := f.s.PutSettings(ctx, f.Owner, f.MarinaID, 0, dst); err == nil {
			t.Fatal("DST slots accepted")
		}
		set, err = f.s.PutSettings(ctx, f.Owner, f.MarinaID, 0, in)
		if err != nil || set.Row.Version != 1 || set.Row.SkipperItemID != f.items["SKIPPER"] || len(set.DepositModes) != 2 {
			t.Fatalf("saved %+v %v", set, err)
		}
		if _, err := f.s.PutSettings(ctx, f.Owner, f.MarinaID, 0, in); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("second first save: %v", err)
		}
		hd, err := f.s.CreateFormula(ctx, f.Owner, f.MarinaID, FormulaInput{Code: "MEZZA-MATTINA", Name: map[string]string{"it": "Mattina"}, Kind: "half_day", BandCode: "morning"})
		if err != nil || hd.MinDays != 1 || hd.MaxDays != 30 || hd.LateStepMinutes != 30 {
			t.Fatalf("formula %+v %v", hd, err)
		}
		if _, err := f.s.CreateFormula(ctx, f.Owner, f.MarinaID, FormulaInput{Code: "MULTI", Name: map[string]string{"it": "M"}, Kind: "multi_day", MinDays: 1}); !isField(err, "min_days") {
			t.Fatalf("multi day min: %v", err)
		}
		noMorning := in
		noMorning.Bands = set.Bands[1:]
		if _, err := f.s.PutSettings(ctx, f.Owner, f.MarinaID, 1, noMorning); !isField(err, "bands") {
			t.Fatalf("band in use: %v", err)
		}
		cats := []string{"sup"}
		upd, err := f.s.UpdateFormula(ctx, f.Owner, f.MarinaID, hd.ID, 1, FormulaPatch{Categories: cats})
		if err != nil || len(FormulaCategories(upd)) != 1 {
			t.Fatalf("patch formula %+v %v", upd, err)
		}
		x, err := f.s.CreateExtra(ctx, f.Owner, f.MarinaID, ExtraInput{ItemID: f.items["FRIGO"], ChargeMode: "per_day", DefaultQtyMilli: qty.One})
		if err != nil || x.ItemCode != "FRIGO" || x.MaxQtyMilli != 10_000 {
			t.Fatalf("extra %+v %v", x, err)
		}
	})
}

func TestFleet_QuoteAndSnapshot(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		f := newFenv(t, db)
		ctx := fixture.Ctx()
		u := f.unit(t, "GOM58")
		set, _ := f.s.GetSettings(ctx, f.Owner, f.MarinaID)
		if _, err := f.s.PutSettings(ctx, f.Owner, f.MarinaID, 0, SettingsInput{
			OpenFromSlot: 16, OpenToSlot: 38, Bands: set.Bands, BufferSlots: 1, DeskHoldMinutes: 60, NoShowAfterMinutes: 60,
			LicenceThresholdKWMilli: 30_000, SkipperItemID: f.items["SKIPPER"], Checklist: set.Checklist, CheckoutMinPhotos: 2,
			CheckinMinPhotos: 1, DepositModes: []string{"cash"}, IdentityRetentionDays: 365,
		}); err != nil {
			t.Fatal(err)
		}
		mk := func(code, kind, band, skipper string) *store.RentalFormula {
			fo, err := f.s.CreateFormula(ctx, f.Owner, f.MarinaID, FormulaInput{Code: code, Name: map[string]string{"it": code}, Kind: kind, BandCode: band, Skipper: skipper})
			if err != nil {
				t.Fatalf("formula %s: %v", code, err)
			}
			return fo
		}
		hourly := mk("ORARIA", "hourly", "", "none")
		morning := mk("MEZZA-MATTINA", "half_day", "morning", "optional")
		day := mk("GIORNATA", "day", "", "required")
		snk, _ := f.s.CreateExtra(ctx, f.Owner, f.MarinaID, ExtraInput{ItemID: f.items["SNORKEL"], DefaultQtyMilli: qty.One})
		frg, _ := f.s.CreateExtra(ctx, f.Owner, f.MarinaID, ExtraInput{ItemID: f.items["FRIGO"], ChargeMode: "per_day", DefaultQtyMilli: qty.One})
		pt := func(d string, s int) slots.Point { return slots.Point{Date: calendar.MustParse(d), Slot: s} }
		rng := func(a, b slots.Point) slots.Range { return slots.Range{Start: a, End: b} }

		// E1 through SnapshotTx.
		snap, err := f.s.SnapshotTx(ctx, db.DB, f.scope, SnapshotRequest{UnitID: u.ID, FormulaID: hourly.ID, Range: rng(pt("2027-06-12", 20), pt("2027-06-12", 26))})
		if err != nil || snap.Total() != 13500 || snap.Slots != 6 || !snap.LicenceRequired || snap.EngineVersion != pricing.RentalEngineVersion {
			t.Fatalf("E1 %+v %v", snap, err)
		}
		// §8.2 snapshot: half day morning + FRIGO + SNORKEL = 165.00.
		snap, err = f.s.SnapshotTx(ctx, db.DB, f.scope, SnapshotRequest{
			UnitID: u.ID, FormulaID: morning.ID, Range: rng(pt("2027-06-12", 16), pt("2027-06-12", 26)),
			Extras: []ExtraQty{{ExtraID: frg.ID, Qty: qty.One}, {ExtraID: snk.ID, Qty: qty.One}},
		})
		if err != nil || snap.Total() != 16500 || len(snap.Breakdown.Lines) != 3 {
			t.Fatalf("8.2 snapshot %+v %v", snap, err)
		}
		// INV-20 extended: the stored snapshot recomputes to the same total.
		raw, _ := json.Marshal(snap)
		var stored price.Snapshot
		_ = json.Unmarshal(raw, &stored)
		again, _ := f.s.SnapshotTx(ctx, db.DB, f.scope, SnapshotRequest{
			UnitID: u.ID, FormulaID: morning.ID, Range: rng(pt("2027-06-12", 16), pt("2027-06-12", 26)),
			Extras: []ExtraQty{{ExtraID: frg.ID, Qty: qty.One}, {ExtraID: snk.ID, Qty: qty.One}},
		})
		if again.Total() != stored.Total() || len(again.Breakdown.Lines) != len(stored.Breakdown.Lines) {
			t.Fatalf("recompute %d vs %d", again.Total(), stored.Total())
		}
		// E3 with a skipper: no licence needed.
		snap, err = f.s.SnapshotTx(ctx, db.DB, f.scope, SnapshotRequest{
			UnitID: u.ID, FormulaID: morning.ID, SkipperID: "x", Range: rng(pt("2027-06-12", 16), pt("2027-06-12", 26)),
			Extras: []ExtraQty{{ExtraID: snk.ID}},
		})
		if err != nil || snap.Total() != 26000 || snap.LicenceRequired || !snap.WithSkipper {
			t.Fatalf("E3 %+v %v", snap, err)
		}
		if _, err := f.s.SnapshotTx(ctx, db.DB, f.scope, SnapshotRequest{UnitID: u.ID, FormulaID: morning.ID, Range: rng(pt("2027-06-12", 17), pt("2027-06-12", 26))}); err == nil {
			t.Fatal("range mismatch accepted")
		} else if _, ok := errors.AsType[*slots.RangeError](err); !ok {
			t.Fatalf("range error: %v", err)
		}

		// Quote of every formula for the open window: day refused (skipper required), hourly 22 slots, half day mismatch.
		opts, err := f.s.Quote(ctx, f.Owner, f.MarinaID, QuoteInput{UnitID: u.ID, Range: rng(pt("2027-06-12", 16), pt("2027-06-12", 38))})
		if err != nil || len(opts) != 3 {
			t.Fatalf("quote %+v %v", opts, err)
		}
		if !opts[0].OK || opts[0].FormulaCode != "ORARIA" || opts[0].Snapshot.Total() != 49500 {
			t.Fatalf("first option %+v", opts[0])
		}
		problems := map[string]string{}
		for _, o := range opts[1:] {
			problems[o.FormulaCode] = o.Problem
		}
		if problems["GIORNATA"] != ProblemSkipperRequired || problems["MEZZA-MATTINA"] != slots.CodeFormulaRangeMismatch {
			t.Fatalf("problems %v", problems)
		}
		opts, _ = f.s.Quote(ctx, f.Owner, f.MarinaID, QuoteInput{UnitID: u.ID, FormulaID: day.ID, WithSkipper: true, Range: rng(pt("2027-07-02", 16), pt("2027-07-02", 38))})
		if len(opts) != 1 || opts[0].OK || opts[0].Problem != ProblemNoPrice {
			t.Fatalf("day alta without a skipper day rule: %+v", opts)
		}
		// A unit in maintenance is not bookable.
		st := store.RentalUnitMaintenance
		if _, err := f.s.UpdateUnit(ctx, f.Owner, f.MarinaID, u.ID, 1, UnitPatch{Status: &st}); err != nil {
			t.Fatal(err)
		}
		opts, _ = f.s.Quote(ctx, f.Owner, f.MarinaID, QuoteInput{UnitID: u.ID, FormulaID: hourly.ID, Range: rng(pt("2027-06-12", 20), pt("2027-06-12", 26))})
		if opts[0].OK || opts[0].Problem != ProblemUnitNotBookable {
			t.Fatalf("maintenance %+v", opts)
		}
		_, otherM, other := f.OtherOrganization(t)
		if _, err := f.s.Quote(context.Background(), other, otherM, QuoteInput{UnitID: u.ID, Range: rng(pt("2027-06-12", 20), pt("2027-06-12", 26))}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina unit: %v", err)
		}
	})
}
