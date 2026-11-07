//go:build integration

package contract

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// quarterlyPolicy is the M4 demo payment policy: deposit 30 % due in 7 days, then 3 instalments every 3 months.
func quarterlyPolicy() *PolicyInput {
	bp := int64(3000)
	return &PolicyInput{Payment: &PaymentPolicy{
		Deposit: &DepositPolicy{BP: &bp}, DepositDueDays: 7, IssueDaysBeforeDue: 15, RoundingUnitCents: 1,
		Schedule:     SchedulePolicy{Mode: "interval", EveryMonths: 3, Count: 3, FirstDueOffsetMonths: 3},
		EarlyPayment: &EarlyPaymentPolicy{DiscountBP: 300, PayByOffsetDays: 15},
	}}
}

// setPolicy stores the marina default policy (and a stored settings row: require_paper_scan is false so the
// M3 paper activation keeps working).
func (e *m4env) setPolicy(t *testing.T, driver string) {
	t.Helper()
	noScan := false
	if _, err := e.svc.UpdateContractSettings(fixture.Ctx(), e.Owner, e.MarinaID, 0, SettingsPatch{
		DefaultPolicy: quarterlyPolicy(), SignatureDriver: &driver, RequirePaperScan: &noScan,
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}
}

func (e *m4env) withEmail(t *testing.T, customerID string) {
	t.Helper()
	if _, err := e.DB.NewUpdate().TableExpr("customers").Set("emails = ?", `[{"label":"main","value":"cliente@porto.test"}]`).
		Where("id = ?", customerID).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (e *m4env) plan(t *testing.T, id string) *PlanView {
	t.Helper()
	p, err := e.svc.ContractInstallments(fixture.Ctx(), e.Owner, e.MarinaID, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *m4env) contractInvariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckContractInvariants(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("contract invariant violations: %+v", vs)
	}
	e.invariants(t)
}

func (e *m4env) monthly(t *testing.T, berth, starts string, activate bool) *Detail {
	t.Helper()
	s := d(starts)
	ends, _ := DefaultEndsOn(TermMonthly, s)
	c, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, MooringInput{
		Term: TermMonthly, CustomerID: e.cust, VesselID: e.vessel, StartsOn: s,
		Placements: []Segment{berthSeg(berth, starts, ends.AddDays(-1).String())}, Activate: activate,
	})
	if err != nil {
		t.Fatalf("create monthly: %v", err)
	}
	return c
}

func TestInstallment_PlanIssueSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		p := e.plan(t, c.Row.ID)
		if p.Sum != p.Total || len(p.Items) != 4 || p.Items[0].Kind != "deposit" || p.Items[0].Status != store.InstallmentIssued {
			t.Fatalf("plan %+v", p)
		}
		if p.EarlyPayment != nil {
			t.Fatal("alternative offered after the deposit was issued")
		}
		if e.mailer.count(MailInstallmentPaymentRequest) != 1 {
			t.Fatalf("payment requests %d", e.mailer.count(MailInstallmentPaymentRequest))
		}
		// The job issues the next instalment at its issue date, once.
		e.Clock.Set(time.Date(2027, 9, 1, 8, 0, 0, 0, time.UTC))
		r1, err := e.svc.RunInstallmentsIssueCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := e.svc.RunInstallmentsIssueCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Issued != 1 || r2.Issued != 0 || r1.Overdue != 1 || r2.Overdue != 0 {
			t.Fatalf("issue runs %+v %+v", r1, r2)
		}
		// Manual settlement: one wins, version mismatch and settled rows refuse.
		dep := p.Items[0]
		row, err := e.svc.SettleInstallment(ctx, e.Owner, e.MarinaID, dep.ID, dep.Version, SettleInput{Method: "bank_transfer", Reference: "CRO 123"})
		if err != nil || row.Status != store.InstallmentPaid || row.SettlementSource != store.SettlementManual || row.Reference != "CRO 123" {
			t.Fatalf("settle %+v %v", row, err)
		}
		if _, err := e.svc.SettleInstallment(ctx, e.Owner, e.MarinaID, dep.ID, 0, SettleInput{Method: "cash"}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("second settle %v", err)
		}
		// Bulk settlement is all or nothing.
		p = e.plan(t, c.Row.ID)
		items := []SettleItem{{ID: p.Items[1].ID, Version: p.Items[1].Version}, {ID: dep.ID}}
		if _, err := e.svc.SettleInstallments(ctx, e.Owner, e.MarinaID, items, SettleInput{Method: "cash"}); !errors.Is(err, ErrSettleConflict) {
			t.Fatalf("bulk with a paid row: %v", err)
		}
		if got := e.plan(t, c.Row.ID).Items[1].Status; got != store.InstallmentIssued {
			t.Fatalf("bulk rolled back? %s", got)
		}
		// The dock role cannot settle.
		dock := e.AddUser(t, "dock@porto.test", "dock", e.OrgID, e.MarinaID)
		if _, err := e.svc.SettleInstallment(ctx, dock, e.MarinaID, p.Items[1].ID, 0, SettleInput{Method: "cash"}); err == nil {
			t.Fatal("dock settled")
		}
		csv, err := e.svc.InstallmentsCSV(ctx, e.Owner, e.MarinaID, InstallmentFilter{ContractID: c.Row.ID})
		if err != nil || !strings.Contains(string(csv), ";") || strings.Count(string(csv), "\n") != 5 {
			t.Fatalf("csv %q %v", csv, err)
		}
		e.contractInvariants(t)
	})
}

func TestConcurrency_InstallmentSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		p := e.plan(t, c.Row.ID)
		var ok atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = e.svc.SettleInstallment(ctx, e.Owner, e.MarinaID, p.Items[0].ID, 0, SettleInput{Method: "cash"})
				} else {
					_, err = e.svc.SettleInstallments(ctx, e.Owner, e.MarinaID, []SettleItem{{ID: p.Items[0].ID}, {ID: p.Items[1].ID}}, SettleInput{Method: "cash"})
				}
				if err == nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		n := e.count(t, "audit_log", "action", "installment.settle")
		if ok.Load() < 1 || n > 2 {
			t.Fatalf("successes %d, settle audits %d", ok.Load(), n)
		}
		for _, id := range []string{p.Items[0].ID, p.Items[1].ID} {
			if c := e.countWhere(t, "audit_log", "action = 'installment.settle' AND entity_id = ?", id); c > 1 {
				t.Fatalf("%s settled %d times", id, c)
			}
		}
		e.contractInvariants(t)
	})
}

func (e *m4env) countWhere(t *testing.T, table, where string, args ...any) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr(table).Where(where, args...).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestContract_SuspendTerminateAmounts(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		b := e.tally(t, c.Row.ID)
		s, err := e.svc.SuspendContract(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, "morosità")
		if err != nil || s.Row.Status != string(StatusSuspended) {
			t.Fatalf("suspend %v", err)
		}
		expectTransition(t, "suspend", b, e.tally(t, c.Row.ID), next(t, MooringTransitions, StatusActive, ActionSuspend))
		if _, err := e.svc.SuspendContract(ctx, e.Owner, e.MarinaID, c.Row.ID, s.Row.Version, "x"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("double suspend %v", err)
		}
		r, err := e.svc.ResumeContract(ctx, e.Owner, e.MarinaID, c.Row.ID, s.Row.Version)
		if err != nil || r.Row.Status != string(StatusActive) {
			t.Fatalf("resume %v", err)
		}
		p := e.plan(t, c.Row.ID)
		if _, err := e.svc.SettleInstallment(ctx, e.Owner, e.MarinaID, p.Items[0].ID, 0, SettleInput{Method: "cash"}); err != nil {
			t.Fatal(err)
		}
		tr, err := e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, c.Row.ID, r.Row.Version, d("2027-12-10"), "vendita barca")
		if err != nil {
			t.Fatal(err)
		}
		// 30 % paid: the refund is the unused nights' share of it (half-even), no penalty by default.
		paid := money.Cents(p.Items[0].AmountCents)
		nights := c.Row.StartsOn.DaysUntil(c.Row.EndsOn)
		unused := d("2027-12-10").DaysUntil(c.Row.EndsOn)
		want := int64(TerminationAmount(money.Cents(c.Row.TotalCents), paid, nights, unused, BuiltinCancellation()))
		if tr.Row.TerminationAmountCents == nil || *tr.Row.TerminationAmountCents != want || want >= 0 {
			t.Fatalf("termination amount %v, want %d", tr.Row.TerminationAmountCents, want)
		}
		live := e.plan(t, c.Row.ID)
		if len(live.Items) != 1 {
			t.Fatalf("scheduled instalments left after termination: %+v", live.Items)
		}
		e.invariants(t)
	})
}

func TestRenewal_SimpleHoldsBerthAndExpiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		src := e.monthly(t, e.berths[1], "2027-07-01", true)
		dl := d("2027-07-20")
		pr, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, src.Row.Version, RenewInput{DeadlineOn: &dl, Send: true})
		if err != nil {
			t.Fatal(err)
		}
		if pr.Row.Status != string(ProposalSent) || pr.Successor == nil || pr.Successor.StartsOn.String() != "2027-08-01" {
			t.Fatalf("proposal %+v", pr.Row)
		}
		if e.mailer.count(MailRenewalProposal) != 1 || e.links.tokenOf(pr.Row.ID) == "" {
			t.Fatal("proposal not sent with a link")
		}
		succ, err := e.svc.GetContract(ctx, e.Owner, e.MarinaID, pr.Successor.ID)
		if err != nil || succ.Allocations[0].Kind != store.AllocKindHold || succ.HoldExpiresAt.Before(time.Date(2027, 7, 20, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("successor hold %+v %v", succ, err)
		}
		if _, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, RenewInput{}); !errors.Is(err, ErrRenewalExists) {
			t.Fatalf("second renewal %v", err)
		}
		// The customer accepts through the link; the office signs on paper; the deposit is settled.
		token := e.links.tokenOf(pr.Row.ID)
		if _, err := e.svc.AcceptPublicRenewal(ctx, "", token, PlanStandard); err != nil {
			t.Fatalf("public accept %v", err)
		}
		succ, err = e.svc.ActivateContract(ctx, e.Owner, e.MarinaID, succ.Row.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.GetRenewalProposal(ctx, e.Owner, e.MarinaID, pr.Row.ID)
		if got.Row.Status != string(ProposalSigned) {
			t.Fatalf("after activation %s", got.Row.Status)
		}
		dep := e.plan(t, succ.Row.ID).Items[0]
		if _, err := e.svc.SettleInstallment(ctx, e.Owner, e.MarinaID, dep.ID, 0, SettleInput{Method: "bank_transfer"}); err != nil {
			t.Fatal(err)
		}
		got, _ = e.svc.GetRenewalProposal(ctx, e.Owner, e.MarinaID, pr.Row.ID)
		if got.Row.Status != string(ProposalCompleted) {
			t.Fatalf("after deposit %s", got.Row.Status)
		}
		// At the end the source is renewed (INV-35).
		e.Clock.Set(time.Date(2027, 8, 1, 0, 30, 0, 0, time.UTC))
		r, err := e.svc.RunExpiryCount(ctx)
		if err != nil || r.Renewed != 1 {
			t.Fatalf("expiry %+v %v", r, err)
		}
		s2, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, src.Row.ID)
		if s2.Row.Status != string(StatusRenewed) || e.count(t, "outbox_events", "type", event.ContractRenewed) != 1 {
			t.Fatalf("source %s", s2.Row.Status)
		}
		e.contractInvariants(t)
	})
}

func TestRenewal_ExpiredReleasesHold(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		src := e.monthly(t, e.berths[1], "2027-07-01", true)
		dl := d("2027-06-20")
		pr, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, RenewInput{DeadlineOn: &dl})
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(time.Date(2027, 6, 21, 1, 0, 0, 0, time.UTC))
		for range 2 { // idempotent
			if _, err := e.svc.RunRenewalsSyncCount(ctx); err != nil {
				t.Fatal(err)
			}
		}
		got, _ := e.svc.GetRenewalProposal(ctx, e.Owner, e.MarinaID, pr.Row.ID)
		if got.Row.Status != string(ProposalExpired) || got.Successor.Status != string(StatusCancelled) {
			t.Fatalf("proposal %s successor %s", got.Row.Status, got.Successor.Status)
		}
		if e.countWhere(t, "audit_log", "action = 'renewal_proposal.expire'") != 1 {
			t.Fatal("expired twice")
		}
		// The berth is free: a transit can take it.
		e.transit(t, e.Owner, e.berths[1], "2027-08-02", "2027-08-04", false)
		e.contractInvariants(t)
	})
}

func TestConcurrency_RenewSameContract(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		src := e.monthly(t, e.berths[2], "2027-07-01", true)
		var ok, exists atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, RenewInput{})
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrRenewalExists):
					exists.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 || ok.Load()+exists.Load() != 100 {
			t.Fatalf("ok %d exists %d", ok.Load(), exists.Load())
		}
		if n := e.countWhere(t, "contracts", "status = 'draft' AND marina_id = ?", e.MarinaID); n != 1 {
			t.Fatalf("%d draft successors", n)
		}
		e.contractInvariants(t)
	})
}

func TestRenewal_CampaignPreviewLaunchSync(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		a := e.monthly(t, e.berths[0], "2027-07-01", true)
		b := e.monthly(t, e.berths[1], "2027-07-01", true)
		c, err := e.svc.CreateRenewalCampaign(ctx, e.Owner, e.MarinaID, CampaignInput{
			Name: "Rinnovi agosto", Filter: CampaignFilter{ContractIDs: []string{a.Row.ID, b.Row.ID}}, DeadlineOn: d("2027-07-25"),
		})
		if err != nil {
			t.Fatal(err)
		}
		c, err = e.svc.PreviewRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version)
		if err != nil || c.Counts[string(ProposalPreviewed)] != 2 {
			t.Fatalf("preview %+v %v", c, err)
		}
		c, err = e.svc.PreviewRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version)
		if err != nil || c.Counts[string(ProposalPreviewed)] != 2 || len(c.Counts) != 1 {
			t.Fatalf("preview again %+v %v", c.Counts, err)
		}
		if _, err := e.svc.LaunchRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = e.svc.RunRenewalsSyncCount(ctx)
			}()
		}
		wg.Wait()
		if _, err := e.svc.RunRenewalsSyncCount(ctx); err != nil {
			t.Fatal(err)
		}
		c, _ = e.svc.GetRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if c.Row.Status != string(CampaignOpen) || c.Counts[string(ProposalSent)] != 2 || e.mailer.count(MailRenewalProposal) != 2 {
			t.Fatalf("campaign %s %+v mails %d", c.Row.Status, c.Counts, e.mailer.count(MailRenewalProposal))
		}
		e.contractInvariants(t)
	})
}

func TestQuote_AcceptRunsAllSteps(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverOTP)
		e.withEmail(t, e.cust)
		q := e.quote(t, e.Owner, KindMooring, TermAnnual, "2027-07-01", "2028-07-01", true, e.berths[3])
		sent, err := e.svc.SendQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, false)
		if err != nil || sent.Quote.Row.Status != string(QuoteSent) || e.mailer.count(MailQuote) != 1 {
			t.Fatalf("send %v", err)
		}
		first := e.links.tokenOf(q.Row.ID)
		sent, err = e.svc.SendQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, sent.Quote.Row.Version, false)
		if err != nil || e.links.tokenOf(q.Row.ID) == first || sent.Quote.Row.SentCount != 2 {
			t.Fatalf("resend did not rotate the link: %v", err)
		}
		// A failure after the document row leaves nothing.
		boom := errors.New("boom")
		e.svc.afterDocument = func() error { return boom }
		if _, err := e.svc.AcceptQuoteM4(ctx, e.Owner, e.MarinaID, q.Row.ID, 0, AcceptInput{OptionID: q.Options[0].Row.ID}); !errors.Is(err, boom) {
			t.Fatalf("forced failure %v", err)
		}
		e.svc.afterDocument = nil
		if n := e.countWhere(t, "contracts", "quote_id = ?", q.Row.ID); n != 0 {
			t.Fatalf("%d contracts after rollback", n)
		}
		res, err := e.svc.AcceptPublicQuote(ctx, "", e.links.tokenOf(q.Row.ID), q.Options[0].Row.ID, PlanEarlyPayment, PublicData{})
		if err != nil {
			t.Fatal(err)
		}
		qd, _ := e.svc.GetQuote(ctx, e.Owner, e.MarinaID, q.Row.ID)
		c, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, qd.Row.ContractID)
		if c.Row.Status != string(StatusAwaitingSignature) || res.SignatureReqID == "" || len(e.sig.opened) != 1 || qd.Row.AcceptedVia != ViaLink {
			t.Fatalf("contract %s request %q", c.Row.Status, res.SignatureReqID)
		}
		p := e.plan(t, c.Row.ID)
		if len(p.Items) != 1 || p.Items[0].Kind != "single" || p.Sum != p.Total || !hasEarlyPayment(&c.Row) {
			t.Fatalf("early payment plan %+v", p)
		}
		// The signature completes: active, first instalment issued, contract.signed.
		b := e.tally(t, c.Row.ID)
		scope := e.scope(t)
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			return contractSubject{e.svc}.OnSignedTx(ctx, tx, scope, c.Row.ID, store.NewID(), e.Clock.Now())
		})
		if err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "sign", b, e.tally(t, c.Row.ID), next(t, MooringTransitions, StatusAwaitingSignature, ActionSign))
		c, _ = e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if c.Row.Status != string(StatusActive) || c.Row.SignedDocumentID == "" || e.plan(t, c.Row.ID).Items[0].Status != store.InstallmentIssued {
			t.Fatalf("after signature %s", c.Row.Status)
		}
		e.invariants(t)
	})
}

func TestConcurrency_SignVsCancelContract(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverOTP)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", false)
		c, err := e.svc.RequestContractSignature(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, SignatureInput{})
		if err != nil {
			t.Fatal(err)
		}
		scope := e.scope(t)
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%2 == 0 {
					_ = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
						return contractSubject{e.svc}.OnSignedTx(ctx, tx, scope, c.Row.ID, store.NewID(), e.Clock.Now())
					})
				} else {
					_, _ = e.svc.CancelContract(ctx, e.Owner, e.MarinaID, c.Row.ID, 0, "")
				}
			}()
		}
		wg.Wait()
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		switch got.Row.Status {
		case string(StatusActive):
			if got.Row.SignedDocumentID == "" {
				t.Fatal("active without signed document")
			}
		case string(StatusCancelled):
		default:
			t.Fatalf("status %s", got.Row.Status)
		}
		if n := e.countWhere(t, "audit_log", "entity_id = ? AND action IN ('contract.sign','contract.cancel')", c.Row.ID); n != 1 {
			t.Fatalf("%d terminal audit rows", n)
		}
		e.invariants(t)
	})
}

func TestAddendum_PriceChangeReplans(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		before := e.plan(t, c.Row.ID)
		nt := int64(500_001)
		ad, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, AddendumInput{
			Kind: AddPriceChange, EffectiveOn: d("2027-07-01"), Payload: AddendumPayload{NewTotalCents: &nt, Reason: "accordo"},
		})
		if err != nil || ad.Row.AmountDeltaCents != nt-c.Row.TotalCents {
			t.Fatalf("create %+v %v", ad, err)
		}
		if _, err := e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, ad.Row.ID, ad.Row.Version, ActivateInput{}); err != nil {
			t.Fatal(err)
		}
		after := e.plan(t, c.Row.ID)
		if after.Total != 500_001 || after.Sum != after.Total || after.PlanVersion != before.PlanVersion+1 {
			t.Fatalf("replan %+v", after)
		}
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		snap := got.Snapshot
		var adj int64
		for _, a := range snap.Adjustments {
			adj += a.NewCents - a.OldCents
		}
		if int64(snap.Breakdown.Total)+adj != got.Row.TotalCents { // INV-27
			t.Fatalf("snapshot %d + %d ≠ %d", snap.Breakdown.Total, adj, got.Row.TotalCents)
		}
		// A change of vessel truncates and allocates again from its date.
		v2 := e.addVessel(t, "Grecale", 1100, 370, "monohull_sail")
		vc, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, AddendumInput{Kind: AddVesselChange, EffectiveOn: d("2027-08-01"), Payload: AddendumPayload{VesselID: v2}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, vc.Row.ID, vc.Row.Version, ActivateInput{}); err != nil {
			t.Fatal(err)
		}
		got, _ = e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if got.Row.VesselID != v2 {
			t.Fatal("vessel not changed")
		}
		e.contractInvariants(t)
	})
}

func TestCharter_RotationNoReprice(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		company := e.addCustomer(t, "Charter Srl")
		v1, v2, v3 := e.addVessel(t, "C1", 1100, 360, "monohull_sail"), e.addVessel(t, "C2", 1100, 360, "monohull_sail"), e.addVessel(t, "C3", 1100, 360, "monohull_sail")
		fl, err := e.svc.CreateCharterFleet(ctx, e.Owner, e.MarinaID, FleetInput{CustomerID: company, Name: "Flotta", VesselIDs: []string{v1, v2, v3}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateCharterFleet(ctx, e.Owner, e.MarinaID, FleetInput{CustomerID: company, Name: "Altra", VesselIDs: []string{v1}}); !errors.Is(err, ErrVesselInOtherFleet) {
			t.Fatalf("vessel in two fleets: %v", err)
		}
		c, err := e.svc.CreateCharter(ctx, e.Owner, e.MarinaID, CharterInput{
			FleetID: fl.Row.ID, StartsOn: d("2027-06-05"), EndsOn: d("2027-09-01"), ChangeoverWeekday: 6, PricingMode: CharterFlat,
			FlatCents: ptr64(900_000), FlatReason: "accordo stagionale", Activate: true,
			Placements: []CharterPlacementInput{
				{Target: Segment{ResourceType: store.ResourceBerth, ResourceID: e.berths[0]}, VesselID: v1},
				{Target: Segment{ResourceType: store.ResourceBerth, ResourceID: e.berths[1]}, VesselID: v2},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.Kind != KindCharter || c.Row.TotalCents != 900_000 || len(c.Allocations) != 2 {
			t.Fatalf("charter %+v", c.Row)
		}
		line := c.Allocations[0].ContractLineID
		r, err := e.svc.RotateCharter(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, d("2027-07-03"), []RotationAssignment{{LineID: line, VesselID: v3}})
		if err != nil {
			t.Fatal(err)
		}
		if r.Row.TotalCents != 900_000 {
			t.Fatal("rotation repriced")
		}
		if _, err := e.svc.RotateCharter(ctx, e.Owner, e.MarinaID, c.Row.ID, 0, d("2027-07-10"), []RotationAssignment{{LineID: line, VesselID: e.vessel}}); !errors.Is(err, ErrVesselNotInFleet) {
			t.Fatalf("outside fleet: %v", err)
		}
		e.contractInvariants(t)
	})
}

func ptr64(v int64) *int64 { return &v }

func TestWaitlist_OfferOnTermination(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		other := e.addCustomer(t, "Bianchi")
		w, err := e.svc.CreateWaitlistEntry(ctx, e.Owner, e.MarinaID, WaitlistInput{CustomerID: other, LOA: 1000, Beam: 350, Term: TermAnnual, RequestedFrom: d("2027-07-01")})
		if err != nil {
			t.Fatal(err)
		}
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		if _, err := e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, d("2027-09-01"), "vendita"); err != nil {
			t.Fatal(err)
		}
		if n := e.countWhere(t, "deadlines", "source_id = ? AND kind = 'waitlist_offer'", w.ID); n != 1 {
			t.Fatalf("%d offers", n)
		}
	})
}

func TestImport_ContractsResidualPlanAndUndo(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		if _, err := e.DB.NewUpdate().TableExpr("vessels").Set("registration = 'RM1234'").Where("id = ?", e.vessel).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		tbl, err := csvimport.Parse("ref;customer_id;vessel_registration;berth_code;term;starts_on;ends_on;total;due_on;amount;status\n"+
			"C1;"+e.cust+";rm1234;A3;annual;2027-01-01;2028-01-01;4800,00;2027-01-15;2400,00;paid\n"+
			"C1;"+e.cust+";rm1234;A3;annual;2027-01-01;2028-01-01;4800,00;2027-07-15;2400,00;scheduled\n"+
			"C2;"+e.cust+";rm1234;A9;annual;2027-01-01;2028-01-01;100,00;2027-07-15;99,00;scheduled\n", ';', true)
		if err != nil {
			t.Fatal(err)
		}
		m := csvimport.Mapping{}
		for i, h := range tbl.Header {
			m[h] = i
		}
		rows, err := tbl.Mapped(m)
		if err != nil {
			t.Fatal(err)
		}
		h := e.svc.ImportHandler()
		scope := e.scope(t)
		rep := &csvimport.Report{}
		if err := h.DryRun(ctx, db.DB, scope, rows, rep); err != nil || rep.Valid != 2 || len(rep.Rejected) != 1 {
			t.Fatalf("dry run %+v %v", rep, err)
		}
		var created []csvimport.Created
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			var err error
			created, err = h.Apply(ctx, tx, scope, rows, &csvimport.Report{})
			return err
		})
		if err != nil || len(created) != 1 {
			t.Fatalf("apply %v", err)
		}
		p := e.plan(t, created[0].EntityID)
		if p.Sum != p.Total || p.Items[0].Status != store.InstallmentPaid {
			t.Fatalf("imported plan %+v", p)
		}
		e.contractInvariants(t)
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error { return h.Undo(ctx, tx, scope, created) })
		if err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, created[0].EntityID)
		if got.Row.Status != string(StatusTerminated) {
			t.Fatalf("undo %s", got.Row.Status)
		}
		e.invariants(t)
	})
}
