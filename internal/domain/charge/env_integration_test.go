//go:build integration

package charge

import (
	"context"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// cenv is the backend of the charge integration tests: contracttest's harbour, price list, vessel and customer,
// a contract service whose billing hooks are this charge service, fake credit granter, payment requester and
// backfill payer (the payment and collect services are other builders' packages, m5-plan §14.3).
type cenv struct {
	*contracttest.Env
	svc       *Service
	contracts *contract.Service
	credits   *fakeCredits
	requests  *fakeRequester
	scope     store.MarinaScope
	items     map[string]string // code → id
}

type fakeCredits struct {
	mu       sync.Mutex
	grants   map[string]money.Cents // charge id → credit
	backfill map[string]BackfillPayment
}

func (f *fakeCredits) GrantAdjustmentCreditTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _ audit.Actor, ch *store.Charge, amount money.Cents) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[ch.ID] += amount
	return nil
}

func (f *fakeCredits) RecordBackfillPaymentTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _ audit.Actor, ch *store.Charge, in BackfillPayment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backfill[ch.ID] = in
	return nil
}

type fakeRequester struct {
	mu   sync.Mutex
	keys map[string]int
}

func (f *fakeRequester) RequestForChargeTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _ audit.Actor, _ *store.Charge, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[key]++
	return nil
}

func newCenv(t *testing.T, db *store.DB) *cenv {
	t.Helper()
	e := contracttest.New(t, db)
	d := docs.NewService(docs.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: e.Files, Log: e.Log})
	svc := NewService(Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: e.Files, Prices: e.Prices, Docs: d, Log: e.Log,
	})
	ce := &cenv{
		Env: e, svc: svc, credits: &fakeCredits{grants: map[string]money.Cents{}, backfill: map[string]BackfillPayment{}},
		requests: &fakeRequester{keys: map[string]int{}}, items: map[string]string{},
	}
	svc.SetCreditGranter(ce.credits)
	svc.SetPaymentRequester(ce.requests)
	ce.contracts = contract.NewService(contract.Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Occupancy: e.Occupancy, Prices: e.Prices,
		Keys: e.Keys, Files: e.Files, Billing: svc.ContractHooks(),
	})
	ce.scope = e.Scope(t)
	for _, it := range []struct {
		code string
		bp   int64
		nat  string
	}{{"ONERI", 2200, ""}, {"PENALE", 0, "N1"}, {"RIMBORSO", 2200, ""}, {"SALDO", 0, "N1"}, {"ACQUA", 1000, ""}, {"GRU", 2200, ""}} {
		ce.items[it.code] = ce.item(t, it.code, it.bp, it.nat)
	}
	ctx := fixture.Ctx()
	if _, err := svc.UpdateSettings(ctx, e.Owner, e.MarinaID, 0, SettingsInput{
		DefaultDueDays: 30, AutoPaymentRequest: true, ReminderHour: 9, ExportReminderDays: 30, FiscalDriver: "none",
		Arrears: ArrearsPolicy{DaysOverdue: 60}, FiscalDrivers: []string{"none", "export"},
		OriginItems: map[string]string{
			"management_fee": ce.items["ONERI"], "common_advance": ce.items["ONERI"], "common_balance": ce.items["ONERI"],
			"transfer_fee": ce.items["ONERI"], "termination_penalty": ce.items["PENALE"], "termination_refund": ce.items["RIMBORSO"],
			"opening_balance": ce.items["SALDO"], "contract_trueup_fallback": ce.items["ONERI"], "deposit_retention": ce.items["PENALE"],
		},
	}); err != nil {
		t.Fatal(err)
	}
	return ce
}

// item creates a catalogue item with a VAT rate (or nature).
func (e *cenv) item(t *testing.T, code string, bp int64, nature string) string {
	t.Helper()
	incl := true
	classes, err := e.Catalog.ListRevenueClasses(fixture.Ctx(), e.Owner, e.MarinaID, false)
	if err != nil || len(classes) == 0 {
		t.Fatalf("revenue classes %d %v", len(classes), err)
	}
	it, err := e.Catalog.CreateItem(fixture.Ctx(), e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: "service", Unit: "piece", PriceIncludesVAT: &incl,
		VATRateBP: &bp, VATNature: nature, RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatalf("item %s: %v %+v", code, err, err)
	}
	return it.ID
}

// mooring creates an annual mooring contract (active when activate) on a berth from starts.
func (e *cenv) mooring(t *testing.T, berth, starts string, activate bool) *contract.Detail {
	t.Helper()
	s := contracttest.D(starts)
	ends, _ := contract.DefaultEndsOn(contract.TermAnnual, s)
	c, err := e.contracts.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, contract.MooringInput{
		Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: e.Vessel, StartsOn: s,
		Placements: []contract.Segment{contracttest.BerthSeg(berth, starts, ends.AddDays(-1).String())}, Activate: activate,
	})
	if err != nil {
		t.Fatalf("create mooring: %v", err)
	}
	return c
}

// transit creates a transit on a berth for [arrival, departure), confirmed when confirm.
func (e *cenv) transit(t *testing.T, berth, arrival, departure string, confirm bool) *contract.Detail {
	t.Helper()
	in := contract.TransitInput{
		VesselID: e.Vessel, CustomerID: e.Customer, Arrival: contracttest.D(arrival), Departure: contracttest.D(departure),
		Placements: []contract.Segment{contracttest.BerthSeg(berth, arrival, contracttest.D(departure).AddDays(-1).String())},
	}
	if confirm {
		in.Confirm = &contract.ManualConfirmation{Reference: "cassa"}
	}
	c, err := e.contracts.CreateTransit(fixture.Ctx(), e.Owner, e.MarinaID, in)
	if err != nil {
		t.Fatalf("create transit: %v", err)
	}
	return c
}

// manual creates a manual charge of qty × unit of an item for the customer.
func (e *cenv) manual(t *testing.T, code string, qtyMilli int64, unit money.Cents) *Detail {
	t.Helper()
	d, err := e.svc.Create(fixture.Ctx(), e.Owner, e.MarinaID, ManualInput{
		OriginType: store.ChargeOriginManual, CustomerID: e.Customer,
		Lines: []ItemLine{{ItemID: e.items[code], Qty: qtyOf(qtyMilli), Unit: &unit}},
	})
	if err != nil {
		t.Fatalf("manual charge: %v", err)
	}
	return d
}

func (e *cenv) charges(t *testing.T, origin, originID string) []store.Charge {
	t.Helper()
	cs, err := store.ChargesOfOrigin(context.Background(), e.DB.DB, e.scope, origin, originID, false)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func (e *cenv) charge(t *testing.T, id string) *store.Charge {
	t.Helper()
	c, err := store.GetCharge(context.Background(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *cenv) lines(t *testing.T, ids ...string) []store.ChargeLine {
	t.Helper()
	ls, err := store.ChargeLinesOf(context.Background(), e.DB.DB, e.scope, ids)
	if err != nil {
		t.Fatal(err)
	}
	return ls
}

// invariants asserts the charge invariant queries (B51, B53, B54, B57) are empty.
func (e *cenv) invariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckBillingInvariants(context.Background(), e.DB.DB, e.scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		switch v.Query {
		case "B51", "B53", "B54", "B57":
			t.Errorf("invariant %s %v: %s", v.Query, v.IDs, v.Detail)
		}
	}
}

func (e *cenv) inTx(t *testing.T, fn func(ctx context.Context, tx bun.Tx) error) {
	t.Helper()
	if err := e.DB.RunInTx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}
