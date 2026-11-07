//go:build integration

package cash

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

type env struct {
	*contracttest.Env
	charges  *charge.Service
	payments *payment.Service
	fiscal   *fiscal.Service
	svc      *Service
	scope    store.MarinaScope
	water    string // item 10 % gross-priced
	fuel     string // item 22 % gross-priced
}

func newEnv(t *testing.T, db *store.DB, drivers ...ext.FiscalDriver) *env {
	t.Helper()
	ce := contracttest.New(t, db)
	e := &env{Env: ce}
	e.charges = charge.NewService(charge.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Files: ce.Files, Prices: ce.Prices, Log: ce.Log,
	})
	e.payments = payment.NewService(payment.Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Charges: e.charges, Contracts: ce.Contracts,
		Notify: ce.Notify, Log: ce.Log,
	})
	e.fiscal = fiscal.NewService(fiscal.Deps{DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Log: ce.Log, Drivers: drivers})
	e.svc = NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Charges: e.charges, Payments: e.payments, Fiscal: e.fiscal,
		Prices: ce.Prices, Notify: ce.Notify, Log: ce.Log,
	})
	e.charges.SetCreditGranter(e.payments)
	e.payments.SetCash(e.svc)
	e.payments.SetFiscal(e.fiscal)
	e.scope = ce.Scope(t)
	e.water = e.item(t, "ACQUA", 1000)
	e.fuel = e.item(t, "GASOLIO", 2200)
	return e
}

func ctx() context.Context { return fixture.Ctx() }

func (e *env) item(t testing.TB, code string, rate int64) string {
	t.Helper()
	classes, err := e.Catalog.ListRevenueClasses(ctx(), e.Owner, e.MarinaID, false)
	if err != nil || len(classes) == 0 {
		t.Fatalf("classes %v", err)
	}
	incl := true
	it, err := e.Catalog.CreateItem(ctx(), e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: "product", Unit: "piece", VATRateBP: &rate,
		PriceIncludesVAT: &incl, RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	return it.ID
}

func unit(c money.Cents) *money.Cents { return &c }

func (e *env) sale(t testing.TB, in CounterSaleInput) *CounterSaleResult {
	t.Helper()
	res, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, in)
	if err != nil {
		t.Fatalf("sale: %v", err)
	}
	return res
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

func TestCash_OneOpenSessionPerRegister(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		s, err := e.svc.OpenSession(ctx(), e.Owner, e.MarinaID, "", 100_00)
		if err != nil || s.RegisterCode != "main" {
			t.Fatalf("open %+v %v", s, err)
		}
		if _, err := e.svc.OpenSession(ctx(), e.Owner, e.MarinaID, "", 0); !errors.Is(err, ErrSessionOpen) {
			t.Fatalf("second open: %v", err)
		}
		reg, err := e.svc.CreateRegister(ctx(), e.Owner, e.MarinaID, RegisterInput{Code: "banchina", Name: "Banchina", Active: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateRegister(ctx(), e.Owner, e.MarinaID, RegisterInput{Code: "banchina", Active: true}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("code taken: %v", err)
		}
		if _, err := e.svc.OpenSession(ctx(), e.Owner, e.MarinaID, reg.ID, 0); err != nil {
			t.Fatalf("second register: %v", err)
		}
		// Movements: in, out, out above the drawer refused.
		if _, err := e.svc.AddMovement(ctx(), e.Owner, e.MarinaID, s.ID, MovementIn, 20_00, "fondo"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.AddMovement(ctx(), e.Owner, e.MarinaID, s.ID, MovementOut, 500_00, "prelievo"); !errors.Is(err, ErrInsufficient) {
			t.Fatalf("withdrawal: %v", err)
		}
		e.sale(t, CounterSaleInput{Lines: []CounterSaleLine{{ItemID: e.fuel, QtyMilli: 2000, Unit: unit(15_00)}}, Method: "cash", FiscalDocument: true})
		e.sale(t, CounterSaleInput{CustomerID: e.Customer, Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(5_00)}}, Method: "card_pos"})
		cur, _ := e.svc.GetSession(ctx(), e.Owner, e.MarinaID, s.ID)
		closed, err := e.svc.CloseSession(ctx(), e.Owner, e.MarinaID, s.ID, cur.Version, []CountedInput{{Method: "cash", Amount: 148_00}, {Method: "card_pos", Amount: 5_00}}, "")
		if err != nil {
			t.Fatal(err)
		}
		if closed.Summary.ExpectedTotal != 155_00 || closed.Summary.Difference != -2_00 || closed.Status != store.CashSessionClosed {
			t.Fatalf("close %+v", closed.Summary)
		}
		if _, err := e.svc.AddMovement(ctx(), e.Owner, e.MarinaID, s.ID, MovementIn, 1_00, "x"); !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("movement after close: %v", err)
		}
		// Cash without an open session is refused.
		if _, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, CounterSaleInput{
			Lines: []CounterSaleLine{{ItemID: e.fuel, QtyMilli: 1000, Unit: unit(1_00)}}, Method: "cash", FiscalDocument: true,
		}); !errors.Is(err, ErrSessionRequired) {
			t.Fatalf("cash without session: %v", err)
		}
		z, err := e.svc.ZReportOf(ctx(), e.Owner, e.MarinaID, s.ID)
		if err != nil || z.FiscalCount != 1 || len(z.VAT) == 0 {
			t.Fatalf("z %+v %v", z, err)
		}
		if _, err := z.HTML(); err != nil {
			t.Fatal(err)
		}
		e.invariants(t)
	})
}

func TestConcurrency_OpenSession(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		if _, err := e.svc.ListRegisters(ctx(), e.Owner, e.MarinaID); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, open := 0, 0
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.OpenSession(ctx(), e.Owner, e.MarinaID, "", 0)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, ErrSessionOpen):
					open++
				default:
					t.Errorf("open: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok != 1 || open != 99 {
			t.Fatalf("ok %d open %d", ok, open)
		}
	})
}

func TestConcurrency_CashCloseVsPayment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		s, err := e.svc.OpenSession(ctx(), e.Owner, e.MarinaID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		var paid []string
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == 50 {
					cur, err := e.svc.GetSession(ctx(), e.Owner, e.MarinaID, s.ID)
					if err != nil {
						t.Errorf("get: %v", err)
						return
					}
					for {
						_, err := e.svc.CloseSession(ctx(), e.Owner, e.MarinaID, s.ID, cur.Version, nil, "")
						if !errors.Is(err, ErrVersionMismatch) {
							if err != nil {
								t.Errorf("close: %v", err)
							}
							return
						}
						cur, _ = e.svc.GetSession(ctx(), e.Owner, e.MarinaID, s.ID)
					}
				}
				res, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, CounterSaleInput{
					Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(1_00)}}, Method: "cash", FiscalDocument: true,
				})
				if err != nil {
					if !errors.Is(err, ErrSessionClosed) && !errors.Is(err, ErrSessionRequired) {
						t.Errorf("sale: %v", err)
					}
					return
				}
				mu.Lock()
				paid = append(paid, res.Payment.ID)
				mu.Unlock()
			}()
		}
		wg.Wait()
		closed, err := e.svc.GetSession(ctx(), e.Owner, e.MarinaID, s.ID)
		if err != nil || closed.Status != store.CashSessionClosed {
			t.Fatalf("session %v", err)
		}
		// Every successful cash payment is in the closed totals.
		if closed.Summary.Payments != money.Cents(len(paid))*1_00 {
			t.Fatalf("payments %d in totals, %d recorded", closed.Summary.Payments, len(paid))
		}
		var expected map[string]int64
		_ = jsonUnmarshal(closed.Expected, &expected)
		if expected["cash"] != int64(len(paid))*1_00 {
			t.Fatalf("frozen expected %d, recorded %d", expected["cash"], len(paid))
		}
		e.invariants(t)
	})
}

func TestCounterSale_AllInOneTx(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		before := countRows(t, e, "charges")
		// A walk-in sale without a commercial document is refused before anything is written.
		if _, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, CounterSaleInput{
			Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(1_00)}}, Method: "card_pos",
		}); !errors.Is(err, ErrCustomerRequired) {
			t.Fatalf("walk-in without document: %v", err)
		}
		// A failure in the payment step (cash without session) rolls back the charge.
		if _, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, CounterSaleInput{
			Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(1_00)}}, Method: "cash", FiscalDocument: true,
		}); !errors.Is(err, ErrSessionRequired) {
			t.Fatalf("no session: %v", err)
		}
		if countRows(t, e, "charges") != before || countRows(t, e, "fiscal_documents") != 0 {
			t.Fatal("partial sale left rows")
		}
		res := e.sale(t, CounterSaleInput{
			CustomerID: e.Customer, Method: "card_pos", FiscalDocument: true, IdempotencyKey: "sale-key-0001",
			Lines: []CounterSaleLine{{ItemID: e.fuel, QtyMilli: 20000, Unit: unit(1_83)}, {ItemID: e.water, QtyMilli: 1000, Unit: unit(11_00)}},
		})
		if res.Charge.TotalCents != 47_60 || res.Charge.PaymentStatus != store.ChargePaid || res.Charge.FiscalMode != "receipt" ||
			res.FiscalDocument == nil || res.FiscalDocument.TotalCents != 47_60 || res.FiscalDocument.Status != store.FiscalIssued {
			t.Fatalf("sale %+v %+v", res.Charge, res.FiscalDocument)
		}
		again := e.sale(t, CounterSaleInput{
			CustomerID: e.Customer, Method: "card_pos", FiscalDocument: true, IdempotencyKey: "sale-key-0001",
			Lines: []CounterSaleLine{{ItemID: e.fuel, QtyMilli: 20000, Unit: unit(1_83)}, {ItemID: e.water, QtyMilli: 1000, Unit: unit(11_00)}},
		})
		if again.Payment.ID != res.Payment.ID || again.Charge.ID != res.Charge.ID {
			t.Fatal("idempotent replay created a new sale")
		}
		b, ct, err := e.svc.Receipt(ctx(), e.Owner, e.MarinaID, res.Payment.ID, Receipt58)
		if err != nil || ct != "text/html; charset=utf-8" || !containsAll(string(b), "Non valido ai fini fiscali", res.FiscalDocument.Number) {
			t.Fatalf("receipt %v", err)
		}
		e.invariants(t)
	})
}

func TestConcurrency_FiscalSequence(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var numbers []string
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := e.svc.CounterSale(ctx(), e.Owner, e.MarinaID, CounterSaleInput{
					CustomerID: e.Customer, Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(1_00)}},
					Method: "card_pos", FiscalDocument: true,
				})
				if err != nil {
					t.Errorf("sale: %v", err)
					return
				}
				mu.Lock()
				numbers = append(numbers, res.FiscalDocument.Number)
				mu.Unlock()
			}()
		}
		wg.Wait()
		sort.Strings(numbers)
		if len(numbers) != 100 || numbers[0] != "20270602-0001" || numbers[99] != "20270602-0100" {
			t.Fatalf("numbers %d first %v last %v", len(numbers), first(numbers), last(numbers))
		}
		for i := 1; i < len(numbers); i++ {
			if numbers[i] == numbers[i-1] {
				t.Fatalf("duplicate %s", numbers[i])
			}
		}
		e.invariants(t)
	})
}

// deferredDriver is a device that is unreachable until up is set.
type deferredDriver struct {
	mu sync.Mutex
	up bool
	n  int
}

type deferErr struct{}

func (deferErr) Error() string  { return "device offline" }
func (deferErr) Deferred() bool { return true }

func (d *deferredDriver) Code() string { return "device" }
func (d *deferredDriver) Issue(context.Context, ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	if !d.up {
		return nil, deferErr{}
	}
	return &ext.FiscalDocumentResult{ExternalRef: "dev-1"}, nil
}
func (d *deferredDriver) Void(context.Context, string) error { return nil }
func (d *deferredDriver) Status(context.Context) (ext.DeviceStatus, error) {
	return ext.DeviceStatus{Online: d.up}, nil
}

func TestFiscal_DeferredDriver(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		drv := &deferredDriver{}
		e := newEnv(t, db, drv)
		if _, err := db.NewInsert().Model(&store.BillingSettings{
			MarinaID: e.MarinaID, DefaultDueDays: 30, AutoPaymentRequest: true, ReminderPolicy: store.JSON(`{}`), ReminderHour: 9,
			ArrearsPolicy: store.JSON(`{}`), ExportReminderDays: 30, FiscalDriver: "device", Version: 1,
			CreatedAt: store.UTC(e.Clock.Now()), UpdatedAt: store.UTC(e.Clock.Now()),
		}).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		res := e.sale(t, CounterSaleInput{CustomerID: e.Customer, Lines: []CounterSaleLine{{ItemID: e.water, QtyMilli: 1000, Unit: unit(3_00)}}, Method: "card_pos", FiscalDocument: true})
		if res.FiscalDocument.Status != store.FiscalPending {
			t.Fatalf("status %s", res.FiscalDocument.Status)
		}
		if err := e.fiscal.RunIssue(context.Background()); err != nil {
			t.Fatal(err)
		}
		d, _ := store.GetFiscalDocument(context.Background(), db.DB, e.scope, res.FiscalDocument.ID)
		if d.Status != store.FiscalPending || drv.n != 1 {
			t.Fatalf("deferred: %s after %d calls", d.Status, drv.n)
		}
		drv.up = true
		if err := e.fiscal.RunIssue(context.Background()); err != nil {
			t.Fatal(err)
		}
		d, _ = store.GetFiscalDocument(context.Background(), db.DB, e.scope, res.FiscalDocument.ID)
		if d.Status != store.FiscalIssued || d.ExternalRef != "dev-1" {
			t.Fatalf("issued: %+v", d)
		}
		// Void through the driver.
		if v, err := e.fiscal.Void(ctx(), e.Owner, e.MarinaID, d.ID); err != nil || v.Status != store.FiscalVoid {
			t.Fatalf("void %v", err)
		}
		e.invariants(t)
	})
}

func countRows(t testing.TB, e *env, table string) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr(table).Where("marina_id = ?", e.MarinaID).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func jsonUnmarshal(raw store.JSON, v any) error { return json.Unmarshal(raw, v) }
