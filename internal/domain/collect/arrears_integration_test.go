//go:build integration

package collect

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// activeMooring creates a mooring contract on berth for vessel and marks it active without issuing instalments
// (the arrears tests control every charge of the contract).
func (e *env) activeMooring(t *testing.T, berth, vessel string) *contract.Detail {
	t.Helper()
	ends, _ := contract.DefaultEndsOn(contract.TermAnnual, contracttest.D("2027-06-10"))
	c, err := e.Contracts.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, contract.MooringInput{
		Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: vessel, StartsOn: contracttest.D("2027-06-10"),
		Placements: []contract.Segment{contracttest.BerthSeg(berth, "2027-06-10", ends.AddDays(-1).String())},
	})
	if err != nil {
		t.Fatalf("create mooring: %v", err)
	}
	if _, err := e.DB.NewUpdate().Model((*store.Contract)(nil)).Set("status = 'active'").Where("id = ?", c.Row.ID).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

// contractCharge creates a manual charge of gross on a contract of the test customer, due on due.
func (e *env) contractCharge(t *testing.T, contractID string, gross money.Cents, due calendar.Date) *store.Charge {
	t.Helper()
	var ch *store.Charge
	err := e.DB.RunInTx(ctx(), func(c context.Context, tx bun.Tx) error {
		net, vat := money.VATSplit(gross, 2200)
		var err error
		ch, _, err = e.charges.CreateTx(c, tx, e.scope, e.Owner.Actor(), charge.CreateInput{
			OriginType: store.ChargeOriginManual, CustomerID: e.Customer, ContractID: contractID, IssuedOn: e.today(), DueOn: due,
			Description: "Servizio", Lines: []charge.LineInput{{
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

// TestArrears_DisputedChargeAndArrearsPaid (spec 16.4; m5-plan §5.5, §16 D15): an overdue charge with an active
// charge-level exclusion (disputed) does not count towards the arrears threshold; a contract with an overdue charge
// is suspended; once that charge is paid the next run raises an arrears_paid worklist item for the contract.
func TestArrears_DisputedChargeAndArrearsPaid(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		bs, err := store.GetBillingSettings(ctx(), e.DB.DB, e.scope, e.Clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		bs.ArrearsPolicy = store.JSON(`{"enabled":true,"days_overdue":30,"min_amount_cents":0}`)
		if _, err := e.DB.NewInsert().Model(bs).On("CONFLICT (marina_id) DO UPDATE").Set("arrears_policy = EXCLUDED.arrears_policy").
			Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		disputed := e.activeMooring(t, e.Berths[2], e.Vessel)
		late := e.activeMooring(t, e.Berths[3], e.AddVessel(t, "Seconda", 1000, 350, "monohull_sail"))
		dch := e.contractCharge(t, disputed.Row.ID, 80_00, e.today())
		lch := e.contractCharge(t, late.Row.ID, 50_00, e.today())
		if _, _, err := e.svc.CreateExclusion(ctx(), e.Owner, e.MarinaID, ExclusionInput{ChargeID: dch.ID, Kind: "disputed"}); err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(dayAt(e, 60))
		if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
			t.Fatal(err)
		}
		status := func(id string) string {
			c, _ := store.GetContract(ctx(), e.DB.DB, e.scope, id)
			return c.Status
		}
		if s := status(disputed.Row.ID); s != "active" {
			t.Fatalf("contract with only a disputed overdue charge: %s", s)
		}
		if s := status(late.Row.ID); s != string(contract.StatusSuspended) {
			t.Fatalf("contract in arrears: %s", s)
		}
		if _, err := e.payments.Record(ctx(), e.Owner, e.MarinaID, payment.RecordInput{
			CustomerID: e.Customer, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: 50_00,
			Matches: []payment.MatchInput{{ChargeID: lch.ID, Amount: 50_00}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetWorkItemState(ctx(), e.DB.DB, e.scope, payment.WorkArrearsPaid, late.Row.ID); err == nil {
			t.Fatal("arrears_paid before the run")
		}
		e.Clock.Set(dayAt(e, 61))
		if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
			t.Fatal(err)
		}
		if st, err := store.GetWorkItemState(ctx(), e.DB.DB, e.scope, payment.WorkArrearsPaid, late.Row.ID); err != nil || st.Status != store.WorkItemOpen {
			t.Fatalf("arrears_paid item %+v %v", st, err)
		}
		page, err := e.payments.Worklist(ctx(), e.Owner, e.MarinaID, payment.WorkFilter{Kind: payment.WorkArrearsPaid}, "", 50)
		if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != late.Row.ID {
			t.Fatalf("worklist %+v %v", page.Items, err)
		}
		if s := status(late.Row.ID); s != string(contract.StatusSuspended) {
			t.Fatalf("resume must stay manual: %s", s)
		}
	})
}

// TestArrears_SecondCycleReopensArrearsPaid (m5-plan §5.5): a contract suspended for arrears a second time, after
// the office closed the arrears_paid item of the first cycle and resumed it, raises an open arrears_paid item again
// once the second arrears is paid (a recurring signal is never lost).
func TestArrears_SecondCycleReopensArrearsPaid(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		bs, err := store.GetBillingSettings(ctx(), e.DB.DB, e.scope, e.Clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		bs.ArrearsPolicy = store.JSON(`{"enabled":true,"days_overdue":30,"min_amount_cents":0}`)
		if _, err := e.DB.NewInsert().Model(bs).On("CONFLICT (marina_id) DO UPDATE").Set("arrears_policy = EXCLUDED.arrears_policy").
			Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		c := e.activeMooring(t, e.Berths[3], e.Vessel)
		run := func(day int) {
			t.Helper()
			e.Clock.Set(dayAt(e, day))
			if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
				t.Fatal(err)
			}
		}
		pay := func(ch *store.Charge) {
			t.Helper()
			if _, err := e.payments.Record(ctx(), e.Owner, e.MarinaID, payment.RecordInput{
				CustomerID: e.Customer, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: money.Cents(ch.TotalCents),
				Matches: []payment.MatchInput{{ChargeID: ch.ID, Amount: money.Cents(ch.TotalCents)}},
			}); err != nil {
				t.Fatal(err)
			}
		}
		openItems := func() int {
			t.Helper()
			page, err := e.payments.Worklist(ctx(), e.Owner, e.MarinaID, payment.WorkFilter{Kind: payment.WorkArrearsPaid, Status: store.WorkItemOpen}, "", 50)
			if err != nil {
				t.Fatal(err)
			}
			return len(page.Items)
		}
		for cycle, day := range []int{0, 100} {
			e.Clock.Set(dayAt(e, day))
			ch := e.contractCharge(t, c.Row.ID, 50_00, e.today())
			run(day + 60)
			if got, _ := store.GetContract(ctx(), e.DB.DB, e.scope, c.Row.ID); got.Status != string(contract.StatusSuspended) {
				t.Fatalf("cycle %d: contract in arrears is %s", cycle, got.Status)
			}
			pay(ch)
			run(day + 61)
			if n := openItems(); n != 1 {
				t.Fatalf("cycle %d: %d open arrears_paid items, want 1", cycle, n)
			}
			// The office closes the item and resumes the contract.
			if _, err := e.payments.UpdateWorkItem(ctx(), e.Owner, e.MarinaID, payment.WorkArrearsPaid, c.Row.ID, payment.WorkPatch{Status: store.WorkItemDone}); err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetContract(ctx(), e.DB.DB, e.scope, c.Row.ID)
			if _, err := e.Contracts.ResumeContract(ctx(), e.Owner, e.MarinaID, c.Row.ID, got.Version); err != nil {
				t.Fatal(err)
			}
			if n := openItems(); n != 0 {
				t.Fatalf("cycle %d: %d open arrears_paid items after done", cycle, n)
			}
		}
	})
}

// TestConcurrency_WebhookVsDeskSettle (INV-31, INV-52): 100 goroutines mix provider succeeded events of a
// payment request on an instalment charge with desk settlements of that instalment. The webhook locks the contract
// before the charges, as the desk does, so no deadlock reaches the caller; the charge is paid once and the ledger
// invariants hold (money that arrives after the desk settlement stays as customer credit).
func TestConcurrency_WebhookVsDeskSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.enableFake(t)
		ends, _ := contract.DefaultEndsOn(contract.TermAnnual, contracttest.D("2027-06-10"))
		c, err := e.Contracts.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, contract.MooringInput{
			Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: e.Vessel, StartsOn: contracttest.D("2027-06-10"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[2], "2027-06-10", ends.AddDays(-1).String())}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := store.InstallmentsOfContracts(ctx(), db.DB, e.scope, []string{c.Row.ID}, true)
		var inst *store.Installment
		for i := range rows {
			if rows[i].Status == store.InstallmentIssued && rows[i].ChargeID != "" {
				inst = &rows[i]
				break
			}
		}
		if inst == nil {
			t.Fatal("no issued instalment with a charge")
		}
		ch := e.reload(t, inst.ChargeID)
		v, err := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: e.Customer, ChargeIDs: []string{ch.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tokenOf(v.URL), "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%2 == 0 {
					ev := &ext.PaymentEvent{
						EventID: fmt.Sprintf("evt_%d", i), Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_race", AmountCents: ch.TotalCents,
						Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
					}
					if err := e.svc.ApplyProviderEvent(ctx(), fixture.FakeProviderCode, e.MarinaID, ev); err != nil {
						errs <- fmt.Errorf("webhook: %w", err)
					}
					return
				}
				_, err := e.Contracts.SettleInstallment(ctx(), e.Owner, e.MarinaID, inst.ID, 0, contract.SettleInput{Method: "bank_transfer"})
				if err != nil && !errors.Is(err, contract.ErrInvalidTransition) && !errors.Is(err, contract.ErrSettleConflict) {
					errs <- fmt.Errorf("settle: %w", err)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if got := e.reload(t, ch.ID); got.PaymentStatus != store.ChargePaid || got.PaidAmountCents != ch.TotalCents {
			t.Fatalf("charge %s paid %d of %d", got.PaymentStatus, got.PaidAmountCents, ch.TotalCents)
		}
		if r, _ := store.GetInstallment(ctx(), db.DB, e.scope, inst.ID); r.Status != store.InstallmentPaid {
			t.Fatalf("instalment %s", r.Status)
		}
		vs, err := store.CheckPaymentsInvariants(context.Background(), db.DB, e.scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("ledger invariants %+v %v", vs, err)
		}
		e.invariants(t)
	})
}

// enableArrears turns the arrears policy on for the marina (30 days overdue, the given minimum).
func (e *env) enableArrears(t *testing.T, minimum money.Cents) ArrearsPolicy {
	t.Helper()
	bs, err := store.GetBillingSettings(ctx(), e.DB.DB, e.scope, e.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	bs.ArrearsPolicy = store.JSON(fmt.Sprintf(`{"enabled":true,"days_overdue":30,"min_amount_cents":%d}`, minimum))
	if _, err := e.DB.NewInsert().Model(bs).On("CONFLICT (marina_id) DO UPDATE").Set("arrears_policy = EXCLUDED.arrears_policy").
		Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return parseArrears(bs.ArrearsPolicy)
}

// TestArrears_RecheckedUnderLock (m5-plan §5.5, §0.8): SuspendForArrearsTx re-reads the overdue amount and the
// customer exclusion after locking the contract, so that a payment committed after the job's unlocked scan, an
// amount below the minimum or a customer exclusion added meanwhile never suspend the contract.
func TestArrears_RecheckedUnderLock(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		pol := e.enableArrears(t, 60_00)
		c := e.activeMooring(t, e.Berths[2], e.Vessel)
		ch := e.contractCharge(t, c.Row.ID, 50_00, e.today())
		c2 := e.activeMooring(t, e.Berths[3], e.AddVessel(t, "Seconda", 1000, 350, "monohull_sail"))
		e.contractCharge(t, c2.Row.ID, 50_00, e.today())
		e.Clock.Set(dayAt(e, 60))
		today := e.today()
		chk := contract.ArrearsCheck{Cutoff: today.AddDays(-pol.DaysOverdue), Today: today, MinAmountCents: pol.MinAmountCents}
		suspend := func(chk contract.ArrearsCheck) error {
			return e.DB.RunInTx(ctx(), func(c2 context.Context, tx bun.Tx) error {
				_, err := e.Contracts.SuspendForArrearsTx(c2, tx, e.scope, c.Row.ID, chk)
				return err
			})
		}
		status := func() string {
			got, _ := store.GetContract(ctx(), e.DB.DB, e.scope, c.Row.ID)
			return got.Status
		}
		// Below the minimum.
		if err := suspend(chk); !errors.Is(err, contract.ErrNotInArrears) || status() != "active" {
			t.Fatalf("below minimum: %v, %s", err, status())
		}
		chk.MinAmountCents = 0
		// Customer excluded after the scan.
		ex, _, err := e.svc.CreateExclusion(ctx(), e.Owner, e.MarinaID, ExclusionInput{CustomerID: e.Customer, Kind: "payment_plan"})
		if err != nil {
			t.Fatal(err)
		}
		if err := suspend(chk); !errors.Is(err, contract.ErrNotInArrears) || status() != "active" {
			t.Fatalf("customer excluded: %v, %s", err, status())
		}
		if err := e.svc.RemoveExclusion(ctx(), e.Owner, e.MarinaID, ex.ID); err != nil {
			t.Fatal(err)
		}
		// Paid after the scan: the job saw the charge overdue, the payment committed before the suspension.
		if _, err := e.payments.Record(ctx(), e.Owner, e.MarinaID, payment.RecordInput{
			CustomerID: e.Customer, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: 50_00,
			Matches: []payment.MatchInput{{ChargeID: ch.ID, Amount: 50_00}},
		}); err != nil {
			t.Fatal(err)
		}
		if err := suspend(chk); !errors.Is(err, contract.ErrNotInArrears) || status() != "active" {
			t.Fatalf("paid after the scan: %v, %s", err, status())
		}
		// Still in arrears: suspended.
		if err := e.DB.RunInTx(ctx(), func(c3 context.Context, tx bun.Tx) error {
			_, err := e.Contracts.SuspendForArrearsTx(c3, tx, e.scope, c2.Row.ID, chk)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if got, _ := store.GetContract(ctx(), e.DB.DB, e.scope, c2.Row.ID); got.Status != string(contract.StatusSuspended) {
			t.Fatalf("contract in arrears: %s", got.Status)
		}
	})
}

// TestConcurrency_ArrearsVsSettle (m5-plan §0.8, PostgreSQL READ COMMITTED): 100 goroutines mix arrears runs
// (scan without locks, then suspend) with desk payments of the overdue charges. A suspension is valid only if it
// was decided while the charge was still unpaid: no contract.suspend audit row may be written after the payment
// match of its charge (UUIDv7 ids are monotonic in the process; the match is written under the charge lock).
func TestConcurrency_ArrearsVsSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		pol := e.enableArrears(t, 0)
		var contracts []string
		chargeOf := map[string]string{}
		for i, b := range e.Berths {
			v := e.Vessel
			if i > 0 {
				v = e.AddVessel(t, fmt.Sprintf("Barca%d", i), 1000, 350, "monohull_sail")
			}
			c := e.activeMooring(t, b, v)
			ch := e.contractCharge(t, c.Row.ID, 50_00, e.today())
			contracts = append(contracts, c.Row.ID)
			chargeOf[c.Row.ID] = ch.ID
		}
		e.Clock.Set(dayAt(e, 60))
		today := e.today()
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%2 == 0 {
					err := e.DB.RunInTx(ctx(), func(c context.Context, tx bun.Tx) error {
						_, err := e.svc.arrearsTx(c, tx, e.scope, today, pol)
						return err
					})
					if err != nil {
						errs <- fmt.Errorf("arrears: %w", err)
					}
					return
				}
				id := chargeOf[contracts[(i/2)%len(contracts)]]
				_, err := e.payments.Record(ctx(), e.Owner, e.MarinaID, payment.RecordInput{
					CustomerID: e.Customer, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: 50_00,
					Matches: []payment.MatchInput{{ChargeID: id, Amount: 50_00}},
				})
				if err != nil && !errors.Is(err, payment.ErrMatchExceeds) {
					errs <- fmt.Errorf("pay: %w", err)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		for _, cid := range contracts {
			var audits []store.AuditEntry
			if err := db.NewSelect().Model(&audits).Where("marina_id = ?", e.MarinaID).Where("entity_id = ?", cid).
				Where("action = 'contract.suspend'").Scan(ctx()); err != nil {
				t.Fatal(err)
			}
			var matches []store.PaymentMatch
			if err := db.NewSelect().Model(&matches).Where("marina_id = ?", e.MarinaID).Where("charge_id = ?", chargeOf[cid]).
				OrderExpr("id").Scan(ctx()); err != nil {
				t.Fatal(err)
			}
			if len(matches) != 1 {
				t.Fatalf("contract %s: %d matches", cid, len(matches))
			}
			for _, a := range audits {
				if a.ID > matches[0].ID {
					t.Errorf("contract %s suspended for arrears after its overdue charge was paid", cid)
				}
			}
		}
		vs, err := store.CheckPaymentsInvariants(context.Background(), db.DB, e.scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("ledger invariants %+v %v", vs, err)
		}
	})
}
