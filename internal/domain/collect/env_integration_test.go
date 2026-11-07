//go:build integration

package collect

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

const webhookSecret = "whsec_fake_secret"

// env is the whole M5 backend wired as in app.go (charge hooks, payments, cash, fiscal, collect) on the contract
// test environment, with the fake provider registered (and enabled only when a test asks for it).
type env struct {
	*contracttest.Env
	charges  *charge.Service
	payments *payment.Service
	cash     *cash.Service
	fiscal   *fiscal.Service
	svc      *Service
	fake     *fixture.FakeProvider
	scope    store.MarinaScope
	seq      int
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	ce := contracttest.New(t, db)
	e := &env{Env: ce, fake: fixture.NewFakeProvider()}
	e.charges = charge.NewService(charge.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Prices: ce.Prices, Log: ce.Log,
	})
	ce.Contracts = contract.NewService(contract.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Log: ce.Log, Occupancy: ce.Occupancy,
		Prices: ce.Prices, Billing: e.charges.ContractHooks(),
	})
	e.payments = payment.NewService(payment.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Charges: e.charges, Contracts: ce.Contracts,
		Notify: ce.Notify, Log: ce.Log,
	})
	e.fiscal = fiscal.NewService(fiscal.Deps{DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log})
	e.cash = cash.NewService(cash.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Charges: e.charges, Payments: e.payments, Fiscal: e.fiscal,
		Prices: ce.Prices, Notify: ce.Notify, Log: ce.Log,
	})
	e.svc = NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Payments: e.payments, Charges: e.charges,
		Contracts: ce.Contracts, Drivers: paydriver.NewRegistry(e.fake), Notify: ce.Notify, BaseURL: "https://porto.test",
		Log: ce.Log,
	})
	e.charges.SetPaymentRequester(e.svc)
	e.charges.SetCreditGranter(e.payments)
	e.payments.SetCash(e.cash)
	e.payments.SetFiscal(e.fiscal)
	e.payments.SetProviderRefunder(e.svc)
	e.scope = ce.Scope(t)
	iban := "IT60X0542811101000000123456"
	if _, err := ce.Contracts.UpdateContractSettings(fixture.Ctx(), ce.Owner, ce.MarinaID, 0, contract.SettingsPatch{
		BankDetails: &contract.BankDetails{IBAN: iban, Holder: "Marina Test Srl", Bank: "Banca Test"},
		OfficeEmail: ptr("office@porto.test"),
	}); err != nil {
		t.Fatalf("contract settings: %v", err)
	}
	return e
}

func ptr[T any](v T) *T { return &v }

func ctx() context.Context { return fixture.Ctx() }

// enableFake enables the fake provider for the marina with card and SEPA.
func (e *env) enableFake(t testing.TB) {
	t.Helper()
	if _, err := e.svc.UpdateProviderSettings(ctx(), e.Owner, e.MarinaID, fixture.FakeProviderCode, 1, ProviderSettingsInput{
		Enabled: true, Mode: "test", Methods: []string{"card", "sepa_debit"}, SecretKey: ptr("sk_test_fake"),
		WebhookSecret: ptr(webhookSecret), PublishableKey: ptr("pk_test_ABCD"), SEPAEnabled: true,
	}); err != nil {
		t.Fatalf("enable provider: %v", err)
	}
}

// customer inserts a customer with an email.
func (e *env) customer(t testing.TB, name string) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: name, FirstName: "Mario",
		BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[{"label":"main","value":"` + strings.ToLower(name) + `@example.test"}]`),
		Phones: store.JSON(`[]`), Tags: store.Strings{}, SearchText: name, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, c)
	return c.ID
}

func (e *env) today() calendar.Date {
	return calendar.In(e.Clock.Now(), mustLoc("Europe/Rome"))
}

func mustLoc(name string) *time.Location {
	l, err := calendar.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return l
}

// charge creates a manual charge of gross (22 %) due on due through the charge service.
func (e *env) charge(t testing.TB, customerID string, gross money.Cents, due calendar.Date) *store.Charge {
	t.Helper()
	var ch *store.Charge
	err := e.DB.RunInTx(ctx(), func(c context.Context, tx bun.Tx) error {
		net, vat := money.VATSplit(gross, 2200)
		e.seq++
		var err error
		ch, _, err = e.charges.CreateTx(c, tx, e.scope, e.Owner.Actor(), charge.CreateInput{
			OriginType: store.ChargeOriginManual, CustomerID: customerID, IssuedOn: e.today(), DueOn: due,
			Description: fmt.Sprintf("Servizio %d", e.seq),
			Lines: []charge.LineInput{{
				ItemCode: "SRV", Description: "Servizio", Qty: 1000, Unit: gross, Net: net, VAT: vat, Gross: gross,
				VATRateBP: 2200, PriceIncludesVAT: true, ServiceFrom: e.today(), ServiceTo: e.today(),
			}},
		})
		return err
	})
	if err != nil {
		t.Fatalf("charge: %v", err)
	}
	return ch
}

func (e *env) reload(t testing.TB, id string) *store.Charge {
	t.Helper()
	c, err := store.GetCharge(context.Background(), e.DB.DB, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// webhook delivers a signed fake event through the provider's ParseWebhook and ApplyProviderEvent (as the HTTP
// handler does).
func (e *env) webhook(t testing.TB, ev ext.PaymentEvent) error {
	t.Helper()
	prov, secret, err := e.svc.WebhookProvider(ctx(), fixture.FakeProviderCode, e.MarinaID)
	if err != nil {
		t.Fatalf("webhook provider: %v", err)
	}
	parsed, err := prov.ParseWebhook(fixture.WebhookRequest(ev, e.MarinaID, webhookSecret, e.Clock.Now()), secret, e.Clock.Now())
	if err != nil {
		return err
	}
	return e.svc.ApplyProviderEvent(ctx(), fixture.FakeProviderCode, e.MarinaID, parsed)
}

func (e *env) count(t testing.TB, table, where string, args ...any) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr(table).Where(where, args...).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
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

// tokenOf extracts the token of a pay URL.
func tokenOf(url string) string { return url[strings.LastIndex(url, "/")+1:] }
