//go:build integration

package handover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

const webhookSecret = "whsec_fake_secret"

// fakeReservations implements the frozen Reservations interface for the handover tests: plain status moves under the
// booking lock, as reservation/tx.go does (rows untouched: the slot model is BOOKING's).
type fakeReservations struct{ clk clock.Clock }

func (f fakeReservations) LockBookingTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (*store.RentalBooking, error) {
	return store.LockRentalBooking(ctx, tx, scope, id)
}

func (f fakeReservations) move(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, expected int, from, to, col string, at time.Time) (*store.RentalBooking, error) {
	b, err := store.LockRentalBooking(ctx, tx, scope, id)
	if err != nil {
		return nil, err
	}
	if expected > 0 && b.Version != expected {
		return nil, reservation.ErrVersionMismatch
	}
	if b.Status != from {
		return nil, reservation.ErrInvalidTransition
	}
	if _, err := tx.NewUpdate().Table("rental_bookings").Set("status = ?", to).Set("? = ?", bun.Ident(col), store.UTC(at)).
		Set("version = version + 1").Where("id = ?", id).Exec(ctx); err != nil {
		return nil, err
	}
	return store.GetRentalBooking(ctx, tx, scope, id)
}

func (f fakeReservations) CheckOutTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, _ audit.Actor, id string, expected int, at time.Time) (*store.RentalBooking, error) {
	return f.move(ctx, tx, scope, id, expected, store.RentalStatusConfirmed, store.RentalStatusCheckedOut, "checked_out_at", at)
}

func (f fakeReservations) ReturnTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, _ audit.Actor, id string, expected int, at time.Time) (*store.RentalBooking, error) {
	return f.move(ctx, tx, scope, id, expected, store.RentalStatusCheckedOut, store.RentalStatusReturned, "returned_at", at)
}

func (f fakeReservations) CloseTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, _ audit.Actor, id string) (*store.RentalBooking, error) {
	return f.move(ctx, tx, scope, id, 0, store.RentalStatusReturned, store.RentalStatusClosed, "closed_at", f.clk.Now())
}

func (f fakeReservations) RepriceTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, string) (*reservation.RepriceResult, error) {
	return nil, errors.New("not used")
}

func (f fakeReservations) CancelTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, string, string) (*store.RentalBooking, error) {
	return nil, errors.New("not used")
}

// env is the M5 billing backend plus docs and signatures (as in app.go) and a rental fleet inserted directly (unit
// GOM58: rib 40 kW, tank 60 l, deposit 500.00; formula MEZZA-MATTINA: half day morning, fuel metered, late item
// LATE-GOM 60.00/h, grace 15, step 30), the items of m11-plan §5.3 priced by an active list.
type env struct {
	*contracttest.Env
	charges  *charge.Service
	payments *payment.Service
	cash     *cash.Service
	fiscal   *fiscal.Service
	collect  *collect.Service
	docs     *docs.Service
	sigs     *signature.Service
	svc      *Service
	fake     *fixture.FakeProvider
	scope    store.MarinaScope
	items    map[string]string
	unit     *store.RentalUnit
	formula  *store.RentalFormula
	extras   map[string]string
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	ce := contracttest.New(t, db)
	e := &env{Env: ce, fake: fixture.NewFakeProvider(), items: map[string]string{}, extras: map[string]string{}}
	e.charges = charge.NewService(charge.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Prices: ce.Prices, Log: ce.Log,
	})
	ce.Contracts = contract.NewService(contract.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Log: ce.Log, Occupancy: ce.Occupancy,
		Prices: ce.Prices, Billing: e.charges.ContractHooks(),
	})
	e.docs = docs.NewService(docs.Deps{DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Notify: ce.Notify, Log: ce.Log})
	e.payments = payment.NewService(payment.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Charges: e.charges, Contracts: ce.Contracts,
		Docs: e.docs, Notify: ce.Notify, Log: ce.Log,
	})
	e.fiscal = fiscal.NewService(fiscal.Deps{DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log})
	e.cash = cash.NewService(cash.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Charges: e.charges, Payments: e.payments, Fiscal: e.fiscal,
		Prices: ce.Prices, Notify: ce.Notify, Log: ce.Log,
	})
	e.collect = collect.NewService(collect.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Payments: e.payments, Charges: e.charges,
		Contracts: ce.Contracts, Drivers: paydriver.NewRegistry(e.fake), Notify: ce.Notify, BaseURL: "https://porto.test", Log: ce.Log,
	})
	e.charges.SetPaymentRequester(e.collect)
	e.charges.SetCreditGranter(e.payments)
	e.payments.SetCash(e.cash)
	e.payments.SetFiscal(e.fiscal)
	e.payments.SetProviderRefunder(e.collect)
	links := magiclink.NewService(magiclink.Deps{DB: db, Clock: ce.Clock, BaseURL: "https://porto.test"})
	e.sigs = signature.NewService(signature.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Docs: e.docs, Links: links,
		Notify: ce.Notify, Log: ce.Log,
	})
	e.svc = NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log, Keys: ce.Keys, Files: ce.Files,
		Reservations: fakeReservations{clk: ce.Clock}, Charges: e.charges, Payments: e.payments, Cash: e.cash, Fiscal: e.fiscal,
		Collect: e.collect, Docs: e.docs, Signatures: e.sigs,
	})
	e.collect.SetRentalDepositHooks(e.svc)
	e.sigs.RegisterSubject(e.svc)
	e.scope = ce.Scope(t)
	e.fleet(t)
	return e
}

func ctx() context.Context { return fixture.Ctx() }

func ptr[T any](v T) *T { return &v }

// rome is the marina time zone of the fixture.
func rome(t testing.TB) *time.Location {
	t.Helper()
	l, err := calendar.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// at sets the clock to a local Rome time.
func (e *env) at(t testing.TB, d string, h, m int) time.Time {
	t.Helper()
	dd := calendar.MustParse(d)
	ts := time.Date(dd.Year(), dd.Month(), dd.Day(), h, m, 0, 0, rome(t)).UTC()
	e.Clock.Set(ts)
	return ts
}

func (e *env) item(t testing.TB, code, unit, class string) string {
	t.Helper()
	classes, err := e.Catalog.ListRevenueClasses(ctx(), e.Owner, e.MarinaID, false)
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
	it, err := e.Catalog.CreateItem(ctx(), e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: "service", Unit: unit, PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		t.Fatalf("item %s: %v", code, err)
	}
	e.items[code] = it.ID
	return it.ID
}

// fleet inserts the items, an active list with their unit prices, the rental settings, unit, formula and extras.
func (e *env) fleet(t testing.TB) {
	t.Helper()
	prices := map[string]int64{"NOL-GOM58": 15000, "LATE-GOM": 6000, "FUEL-P": 210, "FRIGO": 500, "SNORKEL": 1000, "DANNI": 100, "CAU-TRAT": 100}
	for _, code := range []string{"NOL-GOM58", "LATE-GOM", "FUEL-P", "FRIGO", "SNORKEL", "DANNI", "CAU-TRAT"} {
		e.item(t, code, "lump", "services")
	}
	rules := []pricelist.Rule{}
	for code, c := range prices {
		cc := money.Cents(c)
		rules = append(rules, pricelist.Rule{Key: "r-" + strings.ToLower(code), Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: code, ItemID: e.items[code], Basis: pricing.BasisMetered, Unit: pricing.UnitStay,
			UnitCents: &cc, Scope: pricing.Scope{ContractTypes: []pricing.ContractType{pricing.ContractStorage}},
		}})
	}
	set := pricing.DefaultSettings()
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundNone}
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractStorage: e.items["NOL-GOM58"]}
	doc := pricelist.SaveRequest{
		Code: "NOL", Name: catalog.I18nText{It: "Noleggio"}, ValidFrom: contracttest.D("2027-01-01"), ValidTo: contracttest.D("2027-12-31"),
		ContractTypes: []pricing.ContractType{pricing.ContractStorage}, Settings: set, Rules: rules,
	}
	l, err := e.Prices.Create(ctx(), e.Owner, e.MarinaID, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Prices.Save(ctx(), e.Owner, e.MarinaID, l.ID, l.Version, doc)
	if err != nil {
		t.Fatalf("save list: %v", err)
	}
	if _, err := e.Prices.Activate(ctx(), e.Owner, e.MarinaID, l.ID, res.Document.Version, calendar.Date{}); err != nil {
		if ie, ok := errors.AsType[*pricelist.InvalidError](err); ok {
			t.Fatalf("activate list: %+v", ie.Issues)
		}
		t.Fatalf("activate: %v", err)
	}
	now := store.UTC(e.Clock.Now())
	e.Insert(t, &store.ChargeOriginItem{ID: store.NewID(), MarinaID: e.MarinaID, OriginKey: "deposit_retention", ItemID: e.items["CAU-TRAT"], CreatedAt: now, UpdatedAt: now})
	rs := store.DefaultRentalSettings(e.MarinaID)
	rs.DamageItemID, rs.Version, rs.CreatedAt, rs.UpdatedAt = e.items["DANNI"], 1, now, now
	e.Insert(t, rs)
	vessel := e.AddVessel(t, "Gommone 5,8", 580, 240, "rib")
	e.unit = &store.RentalUnit{
		ID: store.NewID(), MarinaID: e.MarinaID, VesselID: vessel, Code: "GOM58", Category: store.RentalCategoryRIB, Seats: 8,
		Licence: store.RentalLicenceThreshold, FuelType: store.RentalFuelPetrol, TankMilli: 60_000, DepositCents: 50_000,
		Status: store.RentalUnitAvailable, ItemID: e.items["NOL-GOM58"], Ownership: store.RentalOwnershipMarina,
		EngineHoursMilli: 120_000, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.formula = &store.RentalFormula{
		ID: store.NewID(), MarinaID: e.MarinaID, Code: "MEZZA-MATTINA", Name: store.JSON(`{"it":"Mezza giornata mattina"}`),
		Kind: store.RentalKindHalfDay, BandCode: "morning", MinSlots: 2, MinDays: 1, MaxDays: 1, Skipper: store.RentalSkipperNone, Fuel: store.RentalFuelMetered,
		FuelItemID: e.items["FUEL-P"], LateItemID: e.items["LATE-GOM"], GraceMinutes: 15, LateStepMinutes: 30,
		Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, e.unit, e.formula)
	for _, code := range []string{"FRIGO", "SNORKEL"} {
		mode := store.RentalExtraPerRental
		if code == "FRIGO" {
			mode = store.RentalExtraPerDay
		}
		x := &store.RentalExtra{
			ID: store.NewID(), MarinaID: e.MarinaID, ItemID: e.items[code], ChargeMode: mode, DefaultQtyMilli: 1000,
			MaxQtyMilli: 10_000, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, x)
		e.extras[code] = x.ID
	}
}

var bookingSeq int

// booking inserts a confirmed half-day morning booking of GOM58 on 2027-06-12 (slots 16-26) with the snapshot of
// m11-plan §8.2 (165.00: unit 150.00, FRIGO 5.00, SNORKEL 10.00).
func (e *env) booking(t testing.TB, customerID string, invoice bool) *store.RentalBooking {
	t.Helper()
	snap := price.Snapshot{
		UnitID: e.unit.ID, FormulaID: e.formula.ID, FormulaCode: e.formula.Code, Kind: pricing.RentalKind(store.RentalKindHalfDay),
		Start: contracttest.D("2027-06-12"), Days: 1, Band: "morning", LicenceRequired: true, EngineVersion: 1,
		Breakdown: pricing.RentalBreakdown{Total: 16500, EngineVersion: 1, Lines: []pricing.RentalLine{
			{Seq: 1, Kind: "unit", ItemID: e.items["NOL-GOM58"], ItemCode: "NOL-GOM58", Qty: qty.Milli(1000), UnitCents: 15000, Amount: 15000, PriceIncludesVAT: true},
			{Seq: 2, Kind: "extra", ItemID: e.items["FRIGO"], ItemCode: "FRIGO", Qty: qty.Milli(1000), UnitCents: 500, Amount: 500, PriceIncludesVAT: true},
			{Seq: 3, Kind: "extra", ItemID: e.items["SNORKEL"], ItemCode: "SNORKEL", Qty: qty.Milli(1000), UnitCents: 1000, Amount: 1000, PriceIncludesVAT: true},
		}},
	}
	raw, _ := json.Marshal(snap)
	bookingSeq++
	now := store.UTC(e.Clock.Now())
	b := &store.RentalBooking{
		ID: store.NewID(), Number: bookingSeq, Year: 2027, UnitID: e.unit.ID, FormulaID: e.formula.ID,
		StartsOn: contracttest.D("2027-06-12"), StartSlot: 16, EndsOn: contracttest.D("2027-06-12"), EndSlot: 26,
		Status: store.RentalStatusConfirmed, Channel: store.RentalChannelDesk, CustomerID: customerID, InvoiceRequested: invoice,
		PriceSnapshot: store.JSON(raw), EngineVersion: 1, TotalCents: 16500, DepositCents: e.unit.DepositCents,
		CreatedAt: now, UpdatedAt: now, // the deposit snapshot reservation takes at creation
	}
	if customerID == "" {
		enc, err := e.Keys.EncryptString(`{"name":"Walk In"}`, "rental-renter:"+b.ID)
		if err != nil {
			t.Fatal(err)
		}
		b.RenterEnc = enc
	}
	if err := e.DB.RunInTx(ctx(), func(c context.Context, tx bun.Tx) error {
		return store.InsertRentalBooking(c, tx, e.scope, b)
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) reload(t testing.TB, id string) *store.RentalBooking {
	t.Helper()
	b, err := store.GetRentalBooking(context.Background(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) openSession(t testing.TB) {
	t.Helper()
	if _, err := e.cash.OpenSession(ctx(), e.Owner, e.MarinaID, "", 0); err != nil {
		t.Fatalf("open cash session: %v", err)
	}
}

func (e *env) enableFake(t testing.TB) {
	t.Helper()
	if _, err := e.collect.UpdateProviderSettings(ctx(), e.Owner, e.MarinaID, fixture.FakeProviderCode, 1, collect.ProviderSettingsInput{
		Enabled: true, Mode: "test", Methods: []string{"card"}, SecretKey: ptr("sk_test_fake"),
		WebhookSecret: ptr(webhookSecret), PublishableKey: ptr("pk_test_ABCD"),
	}); err != nil {
		t.Fatalf("enable provider: %v", err)
	}
}

func (e *env) webhook(t testing.TB, ev ext.PaymentEvent) {
	t.Helper()
	prov, secret, err := e.collect.WebhookProvider(ctx(), fixture.FakeProviderCode, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := prov.ParseWebhook(fixture.WebhookRequest(ev, e.MarinaID, webhookSecret, e.Clock.Now()), secret, e.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.collect.ApplyProviderEvent(ctx(), fixture.FakeProviderCode, e.MarinaID, parsed); err != nil {
		t.Fatalf("apply event: %v", err)
	}
}

// jpeg is a small valid JPEG (or PNG when png is true) of a given shade, so that photos differ by bytes.
func photo(t testing.TB, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, color.RGBA{R: shade, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// prepareCheckOut fills every check-out step: identity and licence with photos, contract signed on screen, cash
// deposit, checklist all ok with fuel 8/8 and engine hours 120.5, two boat photos.
func (e *env) prepareCheckOut(t testing.TB, b *store.RentalBooking) {
	t.Helper()
	p := e.Owner
	if _, err := e.svc.PutIdentity(ctx(), p, e.MarinaID, b.ID, IdentityInput{
		DocumentType: "identity_card", DocumentNumber: "CA12345XY", DocumentExpiresOn: contracttest.D("2030-01-01"),
		LicenceNumber: "LT-998877", LicenceCategory: "entro 12 miglia", LicenceExpiresOn: contracttest.D("2031-05-01"),
	}); err != nil {
		t.Fatalf("identity: %v", err)
	}
	for i, kind := range []string{store.RentalFileIDDocument, store.RentalFileLicence, store.RentalFileBoat, store.RentalFileBoat} {
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckout, PhotoInput{
			Kind: kind, ContentType: "image/png", Body: photo(t, uint8(10+i)),
		}); err != nil {
			t.Fatalf("photo %s: %v", kind, err)
		}
	}
	if _, err := e.svc.SignContract(ctx(), p, e.MarinaID, b.ID, SignInput{Driver: SignScreen, PNG: photo(t, 200), DeviceLabel: "Telefono pontile"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := e.svc.StartDeposit(ctx(), p, e.MarinaID, b.ID, DepositInput{Mode: store.RentalDepositCash}); err != nil {
		t.Fatalf("deposit: %v", err)
	}
	e.checklist(t, b.ID, store.RentalPhaseCheckout, 8, 120_500)
}

func (e *env) checklist(t testing.TB, bookingID, phase string, fuel int, hours int64) {
	t.Helper()
	keys := checklistItems(store.DefaultRentalSettings(e.MarinaID), store.RentalCategoryRIB)
	items := make([]ChecklistEntry, 0, len(keys))
	for _, k := range keys {
		items = append(items, ChecklistEntry{Key: k, OK: true})
	}
	h := qty.Milli(hours)
	if _, err := e.svc.SaveChecklist(ctx(), e.Owner, e.MarinaID, bookingID, phase, ChecklistInput{
		ClientRef: "ref-" + phase + "-" + bookingID[len(bookingID)-12:], Items: items, FuelEighths: &fuel, EngineHours: &h,
	}); err != nil {
		t.Fatalf("checklist %s: %v", phase, err)
	}
}

func (e *env) sum(t testing.TB, query string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := e.DB.NewRaw(query, args...).Scan(context.Background(), &v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func (e *env) invariants(t testing.TB) {
	t.Helper()
	vs, err := store.CheckBillingInvariants(context.Background(), e.DB.DB, e.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("billing invariants: %+v", vs)
	}
}
