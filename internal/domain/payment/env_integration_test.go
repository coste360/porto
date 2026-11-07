//go:build integration

package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// env is a backend with the contract engine (contracttest), the payment service, a charge service and a cash
// register with an open session (the cash service is replaced by fakeCash: CASH-DRIVERS owns it).
type env struct {
	*contracttest.Env
	svc     *Service
	charges *charge.Service
	cash    *fakeCash
	scope   store.MarinaScope
	seq     int
}

// fakeCash resolves the session of a payment: the open session of the test register, or the configured error.
type fakeCash struct {
	sessionID string
	err       error
}

func (f *fakeCash) SessionForPaymentTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _ string, required bool) (string, error) {
	if f.err != nil && required {
		return "", f.err
	}
	return f.sessionID, nil
}

var errNoSession = errors.New("test: no open session")

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	ce := contracttest.New(t, db)
	e := &env{Env: ce}
	e.charges = charge.NewService(charge.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Prices: ce.Prices, Log: ce.Log,
	})
	// The contract service with the real billing hooks (charges of instalments and transits).
	ce.Contracts = contract.NewService(contract.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Log: ce.Log, Occupancy: ce.Occupancy,
		Prices: ce.Prices, Billing: e.charges.ContractHooks(),
	})
	e.svc = NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Charges: e.charges, Contracts: ce.Contracts,
		Notify: ce.Notify, Log: ce.Log,
	})
	e.scope = ce.Scope(t)
	now := store.UTC(ce.Clock.Now())
	reg := &store.CashRegister{ID: store.NewID(), MarinaID: ce.MarinaID, Code: "main", Name: "Cassa", Active: true, IsDefault: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	sess := &store.CashSession{
		ID: store.NewID(), MarinaID: ce.MarinaID, RegisterID: reg.ID, Status: "open", OpenedAt: now,
		BusinessDate: calendar.In(now, nil), Expected: store.JSON(`{}`), Counted: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	ce.Insert(t, reg, sess)
	e.cash = &fakeCash{sessionID: sess.ID}
	e.svc.SetCash(e.cash)
	return e
}

// line is a charge line for the test charges (gross split with money.VATSplit).
type line struct {
	gross  money.Cents
	rateBP int64
}

// chargeOpts are the optional fields of a test charge.
type chargeOpts struct {
	customerID, originType, originID, contractID string
	due                                          calendar.Date
	fiscalMode                                   string
}

// addCharge inserts a positive charge with its lines through the store (the charge service belongs to
// CHARGES-EXPORT; the ledger tests only need consistent rows).
func (e *env) addCharge(t testing.TB, o chargeOpts, lines ...line) *store.Charge {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	if o.customerID == "" {
		o.customerID = e.Customer
	}
	if o.originType == "" {
		o.originType = store.ChargeOriginManual
	}
	if o.originID == "" {
		o.originID = store.NewID()
	}
	if o.due.IsZero() {
		o.due = calendar.In(now, nil).AddDays(30)
	}
	if o.fiscalMode == "" {
		o.fiscalMode = "invoice"
	}
	if len(lines) == 0 {
		lines = []line{{100_00, 2200}}
	}
	e.seq++
	c := &store.Charge{
		ID: store.NewID(), MarinaID: e.MarinaID, Number: fmt.Sprintf("T2027-%06d", e.seq), CustomerID: o.customerID,
		OriginType: o.originType, OriginID: o.originID, ContractID: o.contractID, IssuedOn: calendar.In(now, nil), DueOn: o.due,
		PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: o.fiscalMode, CreatedByActor: "test",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	var ls []any
	for i, l := range lines {
		net, vat := money.VATSplit(l.gross, l.rateBP)
		cl := &store.ChargeLine{
			ID: store.NewID(), MarinaID: e.MarinaID, ChargeID: c.ID, Seq: i + 1, ItemCode: "TEST", QtyMilli: 1000,
			UnitCents: int64(l.gross), NetCents: int64(net), VATCents: int64(vat), GrossCents: int64(l.gross), VATRateBP: l.rateBP,
			PriceIncludesVAT: true, CreatedAt: now, UpdatedAt: now,
		}
		if l.rateBP == 0 {
			cl.VATNature = "N1"
		}
		c.TotalCents += int64(l.gross)
		c.NetCents += int64(net)
		c.VATCents += int64(vat)
		ls = append(ls, cl)
	}
	if _, err := e.DB.NewInsert().Model(c).Exec(ctx); err != nil {
		t.Fatalf("insert charge: %v", err)
	}
	for _, l := range ls {
		if _, err := e.DB.NewInsert().Model(l).Exec(ctx); err != nil {
			t.Fatalf("insert line: %v", err)
		}
	}
	return c
}

func (e *env) charge(t testing.TB, id string) *store.Charge {
	t.Helper()
	c, err := store.LedgerCharge(context.Background(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) payment(t testing.TB, id string) *store.Payment {
	t.Helper()
	p, err := store.GetPayment(context.Background(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// record records a manual payment as the owner.
func (e *env) record(t testing.TB, in RecordInput) *Detail {
	t.Helper()
	if in.Kind == "" {
		in.Kind = store.PaymentKindPayment
	}
	if in.Method == "" {
		in.Method = store.PaymentMethodBankTransfer
	}
	if in.CustomerID == "" {
		in.CustomerID = e.Customer
	}
	p, err := e.svc.Record(fixture.Ctx(), e.Owner, e.MarinaID, in)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return p
}

// invariants asserts that the ledger queries find nothing.
func (e *env) invariants(t testing.TB) {
	t.Helper()
	vs, err := store.CheckPaymentsInvariants(context.Background(), e.DB.DB, e.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("ledger invariant violations: %+v", vs)
	}
}

func (e *env) count(t testing.TB, table, where string, args ...any) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr(table).Where(where, args...).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// mooring creates an active annual mooring contract with the quarterly demo policy and returns its plan.
func (e *env) mooring(t testing.TB) (*contract.Detail, *contract.PlanView) {
	t.Helper()
	ctx := fixture.Ctx()
	bp := int64(3000)
	driver := contract.DriverPaper
	noScan := false
	if _, err := e.Contracts.UpdateContractSettings(ctx, e.Owner, e.MarinaID, 0, contract.SettingsPatch{
		DefaultPolicy: &contract.PolicyInput{Payment: &contract.PaymentPolicy{
			Deposit: &contract.DepositPolicy{BP: &bp}, DepositDueDays: 7, IssueDaysBeforeDue: 15, RoundingUnitCents: 1,
			Schedule: contract.SchedulePolicy{Mode: "interval", EveryMonths: 3, Count: 3, FirstDueOffsetMonths: 3},
		}},
		SignatureDriver: &driver, RequirePaperScan: &noScan,
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	starts := contracttest.D("2027-06-10")
	ends, _ := contract.DefaultEndsOn(contract.TermAnnual, starts)
	c, err := e.Contracts.CreateContract(ctx, e.Owner, e.MarinaID, contract.MooringInput{
		Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: e.Vessel, StartsOn: starts,
		Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[0], "2027-06-10", ends.AddDays(-1).String())}, Activate: true,
	})
	if err != nil {
		t.Fatalf("create mooring: %v", err)
	}
	p, err := e.Contracts.ContractInstallments(ctx, e.Owner, e.MarinaID, c.Row.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c, p
}

// chargeInstallment returns the charge of an instalment, issuing it first (the billing hook creates the charge);
// without hooks it inserts one as the hook would and sets installments.charge_id.
func (e *env) chargeInstallment(t testing.TB, id string) *store.Charge {
	t.Helper()
	ctx := context.Background()
	r, err := store.GetInstallment(ctx, e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status == store.InstallmentScheduled {
		if _, err := e.Contracts.IssueInstallments(fixture.Ctx(), e.Owner, e.MarinaID, []string{id}); err != nil {
			t.Fatalf("issue: %v", err)
		}
		if r, err = store.GetInstallment(ctx, e.DB.DB, e.scope, id); err != nil {
			t.Fatal(err)
		}
	}
	if r.ChargeID != "" {
		return e.charge(t, r.ChargeID)
	}
	c := e.addCharge(t, chargeOpts{originType: store.ChargeOriginInstallment, originID: r.ID, contractID: r.ContractID, due: r.DueOn},
		line{money.Cents(r.AmountCents), 2200})
	if _, err := e.DB.NewUpdate().TableExpr("installments").Set("charge_id = ?", c.ID).
		Set("status = CASE WHEN status = 'scheduled' THEN 'issued' ELSE status END").Where("id = ?", r.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func calendarZero() calendar.Date { return calendar.Date{} }
