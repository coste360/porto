//go:build integration

package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// setTacitPolicy stores the quarterly policy with the tacit renewal clause and the insurance policy.
func (e *m4env) setTacitPolicy(t *testing.T, insurance string) {
	t.Helper()
	pol := quarterlyPolicy()
	pol.Renewal = &RenewalPolicy{Tacit: true, NoticeDays: 30}
	driver, noScan := DriverPaper, false
	if _, err := e.svc.UpdateContractSettings(fixture.Ctx(), e.Owner, e.MarinaID, 0, SettingsPatch{
		DefaultPolicy: pol, SignatureDriver: &driver, RequirePaperScan: &noScan, InsurancePolicy: &insurance,
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}
}

// monthlyFor creates an active monthly contract of a customer and vessel on a berth.
func (e *m4env) monthlyFor(t *testing.T, berth, starts, cust, vessel string) *Detail {
	t.Helper()
	s := d(starts)
	ends, _ := DefaultEndsOn(TermMonthly, s)
	c, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, MooringInput{
		Term: TermMonthly, CustomerID: cust, VesselID: vessel, StartsOn: s,
		Placements: []Segment{berthSeg(berth, starts, ends.AddDays(-1).String())}, Activate: true,
	})
	if err != nil {
		t.Fatalf("create monthly: %v", err)
	}
	return c
}

// settleAll settles every live instalment of a contract (no arrears).
func (e *m4env) settleAll(t *testing.T, contractID string) {
	t.Helper()
	var items []SettleItem
	for _, it := range e.plan(t, contractID).Items {
		if it.Status == store.InstallmentIssued || it.Status == store.InstallmentScheduled {
			items = append(items, SettleItem{ID: it.ID})
		}
	}
	if _, err := e.svc.SettleInstallments(fixture.Ctx(), e.Owner, e.MarinaID, items, SettleInput{Method: "cash"}); err != nil {
		t.Fatalf("settle all: %v", err)
	}
}

func (e *m4env) status(t *testing.T, id string) string {
	t.Helper()
	c, err := e.svc.GetContract(fixture.Ctx(), e.Owner, e.MarinaID, id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Row.Status
}

// successorOf returns the active contract of the customer starting on starts ("" = none).
func (e *m4env) successorOf(t *testing.T, cust, starts string) string {
	t.Helper()
	var ids []string
	if err := e.DB.NewSelect().TableExpr("contracts").Column("id").Where("marina_id = ?", e.MarinaID).
		Where("customer_id = ?", cust).Where("starts_on = ?", d(starts)).Where("status = 'active'").
		Scan(context.Background(), &ids); err != nil {
		t.Fatal(err)
	}
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	}
	t.Fatalf("%d successors of %s", len(ids), cust)
	return ""
}

// TestContract_TacitRenewal (spec 13.8, m4-plan §3.1 tacit_renew, §3.4, INV-35): at the end of a contract with
// the tacit clause the expiry job renews it (active successor holding a contract allocation, source renewed,
// audit and outbox, the notice queued once); arrears and an expired insurance block it and leave a
// tacit_renewal_blocked deadline for the office. A second run changes nothing.
func TestContract_TacitRenewal(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setTacitPolicy(t, InsuranceWarn)
		e.withEmail(t, e.cust)
		ok := e.monthlyFor(t, e.berths[0], "2027-07-01", e.cust, e.vessel)
		e.settleAll(t, ok.Row.ID)

		late := e.addCustomer(t, "Bianchi")
		lateVessel := e.addVessel(t, "Grecale", 1134, 380, "monohull_sail")
		e.insure(lateVessel)
		arrears := e.monthlyFor(t, e.berths[1], "2027-07-01", late, lateVessel) // the deposit stays unpaid

		uninsured := e.addCustomer(t, "Verdi")
		bare := e.addVessel(t, "Tramontana", 1134, 380, "monohull_sail")
		noInsurance := e.monthlyFor(t, e.berths[2], "2027-07-01", uninsured, bare) // warn at creation
		e.settleAll(t, noInsurance.Row.ID)
		e.setTacitPolicy(t, InsuranceBlock)

		before := e.tally(t, ok.Row.ID)
		e.Clock.Set(time.Date(2027, 8, 1, 0, 30, 0, 0, time.UTC))
		r, err := e.svc.RunExpiryCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Tacit != 1 || r.Contracts != 2 {
			t.Fatalf("first run %+v", r)
		}
		// The renewed contract: successor active on the same berth with a contract allocation and a plan.
		if st := e.status(t, ok.Row.ID); st != string(StatusRenewed) {
			t.Fatalf("source %s", st)
		}
		after := e.tally(t, ok.Row.ID)
		expectTransition(t, "tacit_renew", before, after, next(t, MooringTransitions, StatusActive, ActionTacitRenew))
		sid := e.successorOf(t, e.cust, "2027-08-01")
		if sid == "" {
			t.Fatal("no active successor")
		}
		succ, err := e.svc.GetContract(ctx, e.Owner, e.MarinaID, sid)
		if err != nil {
			t.Fatal(err)
		}
		if len(succ.Allocations) == 0 || succ.Allocations[0].Kind != store.AllocKindContract ||
			succ.Allocations[0].ResourceID != e.berths[0] || succ.Row.TotalCents <= 0 {
			t.Fatalf("successor %+v allocations %+v", succ.Row, succ.Allocations)
		}
		if p := e.plan(t, sid); p.Sum != p.Total || len(p.Items) == 0 {
			t.Fatalf("successor plan %+v", p)
		}
		if n := e.mailer.count(MailRenewalTacitNotice); n != 1 {
			t.Fatalf("tacit notices %d", n)
		}
		// The blocked contracts: not renewed, expired, one deadline each with the reason.
		for id, reason := range map[string]string{arrears.Row.ID: ExcludeArrears, noInsurance.Row.ID: ExcludeInsuranceExpired} {
			if st := e.status(t, id); st != string(StatusExpired) {
				t.Fatalf("blocked %s is %s", reason, st)
			}
			var rows []store.Deadline
			if err := db.NewSelect().Model(&rows).Where("marina_id = ?", e.MarinaID).Where("kind = 'tacit_renewal_blocked'").
				Where("source_type = 'contract'").Where("source_id = ?", id).Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Status != "open" || rows[0].Note != reason || rows[0].DueOn.String() != "2027-08-01" {
				t.Fatalf("deadline of %s: %+v", reason, rows)
			}
		}
		if e.successorOf(t, late, "2027-08-01") != "" || e.successorOf(t, uninsured, "2027-08-01") != "" {
			t.Fatal("a blocked contract was renewed")
		}
		blockedAudits := e.countWhere(t, "audit_log", "action = 'contract.tacit_renew_blocked' AND entity_id IN (?, ?)", arrears.Row.ID, noInsurance.Row.ID)
		if blockedAudits != 2 {
			t.Fatalf("blocked audits %d", blockedAudits)
		}
		// Second run: a no-op.
		events := e.count(t, "outbox_events", "type", event.ContractRenewed)
		r2, err := e.svc.RunExpiryCount(ctx)
		if err != nil || r2.Tacit != 0 || r2.Contracts != 0 {
			t.Fatalf("second run %+v %v", r2, err)
		}
		if e.mailer.count(MailRenewalTacitNotice) != 1 || e.count(t, "outbox_events", "type", event.ContractRenewed) != events ||
			e.countWhere(t, "deadlines", "kind = 'tacit_renewal_blocked'") != 2 {
			t.Fatal("second run changed something")
		}
		e.contractInvariants(t)
	})
}

// TestContract_ExpireMarksRenewed (INV-35): at the end of a contract the expiry job marks it renewed only when the
// successor of a signed proposal is active; an open proposal or no proposal expires it.
func TestContract_ExpireMarksRenewed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		signed := e.monthly(t, e.berths[0], "2027-07-01", true)
		open := e.monthly(t, e.berths[1], "2027-07-01", true)
		none := e.monthly(t, e.berths[2], "2027-07-01", true)
		dl := d("2027-07-25")
		pr, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, signed.Row.ID, 0, RenewInput{DeadlineOn: &dl})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ActivateContract(ctx, e.Owner, e.MarinaID, pr.Successor.ID, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, open.Row.ID, 0, RenewInput{DeadlineOn: &dl}); err != nil {
			t.Fatal(err)
		}
		bs, bo, bn := e.tally(t, signed.Row.ID), e.tally(t, open.Row.ID), e.tally(t, none.Row.ID)
		e.Clock.Set(time.Date(2027, 8, 1, 0, 30, 0, 0, time.UTC))
		r, err := e.svc.RunExpiryCount(ctx)
		if err != nil || r.Renewed != 1 {
			t.Fatalf("expiry %+v %v", r, err)
		}
		want := map[string]Status{signed.Row.ID: StatusRenewed, open.Row.ID: StatusExpired, none.Row.ID: StatusExpired}
		for id, st := range want {
			if got := e.status(t, id); got != string(st) {
				t.Fatalf("%s: %s, want %s", id, got, st)
			}
		}
		expectTransition(t, "expire_renewed", bs, e.tally(t, signed.Row.ID), next(t, MooringTransitions, StatusActive, ActionExpireRenewed))
		expectTransition(t, "expire (open proposal)", bo, e.tally(t, open.Row.ID), next(t, MooringTransitions, StatusActive, ActionExpire))
		expectTransition(t, "expire", bn, e.tally(t, none.Row.ID), next(t, MooringTransitions, StatusActive, ActionExpire))
		if r2, err := e.svc.RunExpiryCount(ctx); err != nil || r2.Contracts != 0 || r2.Renewed != 0 {
			t.Fatalf("second run %+v %v", r2, err)
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_ReplanVsSettle (INV-31): replans (explicit and early payment) race settlements and the issue
// job on the same contract; the live plan always sums to the total and each instalment is settled once.
func TestConcurrency_ReplanVsSettle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		e.Clock.Set(time.Date(2027, 9, 1, 8, 0, 0, 0, time.UTC)) // the second instalment is due to issue
		var unexpected atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				switch i % 4 {
				case 0, 1:
					_, err = e.svc.ReplanInstallments(ctx, e.Owner, e.MarinaID, c.Row.ID, 0, ReplanInput{Plan: PlanStandard})
				case 2:
					p, perr := e.svc.ContractInstallments(ctx, e.Owner, e.MarinaID, c.Row.ID)
					if perr != nil {
						err = perr
						break
					}
					var items []SettleItem
					for _, it := range p.Items {
						if it.Status == store.InstallmentScheduled || it.Status == store.InstallmentIssued {
							items = append(items, SettleItem{ID: it.ID})
						}
						if len(items) == 2 {
							break
						}
					}
					if len(items) > 0 {
						_, err = e.svc.SettleInstallments(ctx, e.Owner, e.MarinaID, items, SettleInput{Method: "cash"})
					}
				default:
					_, err = e.svc.RunInstallmentsIssueCount(ctx)
				}
				var se *SettleError
				switch {
				case err == nil, errors.Is(err, ErrVersionMismatch), errors.Is(err, ErrInvalidTransition),
					errors.As(err, &se), errors.Is(err, ErrRepriceRefused), store.IsSerializationFailure(err):
				default:
					t.Logf("unexpected: %v", err)
					unexpected.Add(1)
				}
			}()
		}
		wg.Wait()
		if unexpected.Load() != 0 {
			t.Fatalf("%d unexpected errors", unexpected.Load())
		}
		if p := e.plan(t, c.Row.ID); p.Sum != p.Total {
			t.Fatalf("live plan sums to %d, total %d", p.Sum, p.Total)
		}
		var dup int
		if err := db.NewSelect().TableExpr("audit_log").ColumnExpr("count(*) - count(DISTINCT entity_id)").
			Where("action = 'installment.settle'").Scan(context.Background(), &dup); err != nil {
			t.Fatal(err)
		}
		if dup != 0 {
			t.Fatalf("%d instalments settled twice", dup)
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_RenewVsTerminate: renewals racing a termination of the source never leave an open proposal or a
// successor on a terminated source.
func TestConcurrency_RenewVsTerminate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		src := e.monthly(t, e.berths[2], "2027-07-01", true)
		var terminated atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%10 == 0 {
					if _, err := e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, d("2027-07-20"), "vendita"); err == nil {
						terminated.Add(1)
					}
					return
				}
				_, _ = e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, RenewInput{})
			}()
		}
		wg.Wait()
		if terminated.Load() != 1 || e.status(t, src.Row.ID) != string(StatusTerminated) {
			t.Fatalf("terminations %d, status %s", terminated.Load(), e.status(t, src.Row.ID))
		}
		// A proposal created before the termination may stay open (it is the office's to decline); one created
		// after it must not exist: at most one proposal and one draft successor in total.
		props := e.countWhere(t, "renewal_proposals", "source_contract_id = ?", src.Row.ID)
		drafts := e.countWhere(t, "contracts", "status = 'draft' AND marina_id = ?", e.MarinaID)
		if props > 1 || drafts != props {
			t.Fatalf("proposals %d drafts %d", props, drafts)
		}
		if props == 1 {
			var created, termAt time.Time
			if err := db.NewSelect().TableExpr("renewal_proposals").Column("created_at").Where("source_contract_id = ?", src.Row.ID).
				Scan(context.Background(), &created); err != nil {
				t.Fatal(err)
			}
			if err := db.NewSelect().TableExpr("audit_log").Column("created_at").Where("action = 'contract.terminate'").
				Where("entity_id = ?", src.Row.ID).Scan(context.Background(), &termAt); err != nil {
				t.Fatal(err)
			}
			if created.After(termAt) {
				t.Fatal("proposal created after the termination")
			}
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_RenewVsTacit: desk renewals racing the tacit renewal at the end of the source leave at most one
// successor (an active tacit one or a draft of a proposal), never both.
func TestConcurrency_RenewVsTacit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setTacitPolicy(t, InsuranceWarn)
		src := e.monthly(t, e.berths[1], "2027-07-01", true)
		e.settleAll(t, src.Row.ID)
		e.Clock.Set(time.Date(2027, 8, 1, 0, 30, 0, 0, time.UTC))
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%10 == 0 {
					_, _ = e.svc.RunExpiryCount(ctx)
					return
				}
				_, _ = e.svc.RenewContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, RenewInput{})
			}()
		}
		wg.Wait()
		succ := e.countWhere(t, "contracts", "marina_id = ? AND id <> ? AND status IN ('draft','active')", e.MarinaID, src.Row.ID)
		if succ > 1 {
			t.Fatalf("%d live successors", succ)
		}
		st := e.status(t, src.Row.ID)
		open := e.countWhere(t, "renewal_proposals", "source_contract_id = ? AND status IN ('previewed','proposed','sent','accepted')", src.Row.ID)
		if st == string(StatusRenewed) && open != 0 {
			t.Fatalf("tacitly renewed with %d open proposals", open)
		}
		e.contractInvariants(t)
	})
}

// TestRenewal_StaleSourceRefused makes the races of RenewVsTerminate and RenewVsTacit deterministic: a renewal
// priced before a termination commits is refused inside its transaction, and a tacit renewal priced before a desk
// proposal is refused under the source lock.
func TestRenewal_StaleSourceRefused(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		scope := e.scope(t)
		src := e.monthly(t, e.berths[2], "2027-07-01", true)
		srcRow, err := store.GetContract(ctx, db.DB, scope, src.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		rd, err := e.svc.prepareRenewal(ctx, e.Owner, e.MarinaID, scope, srcRow, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, src.Row.ID, 0, d("2027-07-20"), "vendita"); err != nil {
			t.Fatal(err)
		}
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.proposeTx(ctx, tx, scope, actor{p: e.Owner}, rd, "", d("2027-07-25"), ProposalActionPropose)
			return err
		})
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("proposal on a terminated source: %v", err)
		}
		if n := e.countWhere(t, "renewal_proposals", "source_contract_id = ?", src.Row.ID); n != 0 {
			t.Fatalf("%d proposals written", n)
		}

		e.setTacitPolicy(t, InsuranceWarn)
		other := e.monthly(t, e.berths[1], "2027-07-01", true)
		e.settleAll(t, other.Row.ID)
		otherRow, err := store.GetContract(ctx, db.DB, scope, other.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := e.svc.settingsTx(ctx, db.DB, scope)
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(time.Date(2027, 7, 31, 8, 0, 0, 0, time.UTC))
		reason, trd, err := e.svc.tacitCheck(ctx, jobPrincipal(scope), scope, cs, otherRow)
		if err != nil || reason != "" {
			t.Fatalf("tacit check %q %v", reason, err)
		}
		if _, err := e.svc.RenewContract(ctx, e.Owner, e.MarinaID, other.Row.ID, 0, RenewInput{}); err != nil {
			t.Fatal(err)
		}
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			return e.svc.tacitRenewTx(ctx, tx, scope, other.Row.ID, trd)
		})
		if !errors.Is(err, ErrRenewalExists) {
			t.Fatalf("tacit renewal over an open proposal: %v", err)
		}
		if st := e.status(t, other.Row.ID); st != string(StatusActive) {
			t.Fatalf("source %s", st)
		}
		e.contractInvariants(t)
	})
}

// TestContract_M4StateChangesWriteAuditAndOutbox extends INV-30 to the M4 rows of MooringTransitions not covered
// elsewhere (suspend and sign: m4_integration_test.go; expire, expire_renewed, tacit_renew: above): resume,
// request_signature, signature_failed, addendum and change_vessel each write their audit row and events in the
// transaction of the change.
func TestContract_M4StateChangesWriteAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverOTP)
		scope := e.scope(t)

		// resume
		a := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		s, err := e.svc.SuspendContract(ctx, e.Owner, e.MarinaID, a.Row.ID, 0, "morosità")
		if err != nil {
			t.Fatal(err)
		}
		b := e.tally(t, a.Row.ID)
		if _, err := e.svc.ResumeContract(ctx, e.Owner, e.MarinaID, a.Row.ID, s.Row.Version); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "resume", b, e.tally(t, a.Row.ID), next(t, MooringTransitions, StatusSuspended, ActionResume))

		// request_signature, signature_failed
		dr := e.mooring(t, e.Owner, e.berths[1], "2027-06-10", false)
		b = e.tally(t, dr.Row.ID)
		if _, err := e.svc.RequestContractSignature(ctx, e.Owner, e.MarinaID, dr.Row.ID, dr.Row.Version, SignatureInput{}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "request_signature", b, e.tally(t, dr.Row.ID), next(t, MooringTransitions, StatusDraft, ActionRequestSignature))
		b = e.tally(t, dr.Row.ID)
		if err := db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			return contractSubject{e.svc}.OnFailedTx(ctx, tx, scope, dr.Row.ID, "expired")
		}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "signature_failed", b, e.tally(t, dr.Row.ID), next(t, MooringTransitions, StatusAwaitingSignature, ActionSignatureFailed))
		if st := e.status(t, dr.Row.ID); st != string(StatusDraft) {
			t.Fatalf("after signature_failed %s", st)
		}

		// addendum (price change) and change_vessel on an active contract: the audit row of the addendum and
		// contract.changed are written in the activation transaction.
		nt := int64(500_001)
		ad, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, a.Row.ID, AddendumInput{
			Kind: AddPriceChange, EffectiveOn: d("2027-07-01"), Payload: AddendumPayload{NewTotalCents: &nt, Reason: "accordo"},
		})
		if err != nil {
			t.Fatal(err)
		}
		b = e.tally(t, ad.Row.ID)
		if _, err := e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, a.Row.ID, ad.Row.ID, ad.Row.Version, ActivateInput{}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "addendum", b, e.tally(t, ad.Row.ID), next(t, MooringTransitions, StatusActive, ActionAddendum))
		v2 := e.addVessel(t, "Grecale", 1100, 370, "monohull_sail")
		e.insure(v2)
		vc, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, a.Row.ID, AddendumInput{Kind: AddVesselChange, EffectiveOn: d("2027-08-01"), Payload: AddendumPayload{VesselID: v2}})
		if err != nil {
			t.Fatal(err)
		}
		b = e.tally(t, vc.Row.ID)
		if _, err := e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, a.Row.ID, vc.Row.ID, vc.Row.Version, ActivateInput{}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "change_vessel", b, e.tally(t, vc.Row.ID), next(t, MooringTransitions, StatusActive, ActionChangeVessel))
		e.contractInvariants(t)
	})
}
