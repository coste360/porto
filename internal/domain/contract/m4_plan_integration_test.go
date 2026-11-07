//go:build integration

package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Tests of the fixed list of m4-plan §9.3 not covered by m4_integration_test.go.

// conflictErr reports the errors a losing parallel writer may get (412 or 409).
func conflictErr(err error) bool {
	var se *SettleError
	return errors.Is(err, ErrVersionMismatch) || errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrRenewalExists) ||
		errors.As(err, &se) || isTaken(err) || store.IsSerializationFailure(err)
}

// TestQuote_SendRotatesLink: every send creates a new link and revokes the previous one; the old token no longer
// accepts the quote.
func TestQuote_SendRotatesLink(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		q := e.quote(t, e.Owner, KindMooring, TermAnnual, "2027-07-01", "2028-07-01", true, e.berths[3])
		sent, err := e.svc.SendQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, false)
		if err != nil {
			t.Fatal(err)
		}
		first := e.links.tokenOf(q.Row.ID)
		sent, err = e.svc.SendQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, sent.Quote.Row.Version, false)
		if err != nil {
			t.Fatal(err)
		}
		second := e.links.tokenOf(q.Row.ID)
		if first == "" || second == "" || first == second || sent.Quote.Row.SentCount != 2 || e.mailer.count(MailQuote) != 2 {
			t.Fatalf("links %q %q sent %d mails %d", first, second, sent.Quote.Row.SentCount, e.mailer.count(MailQuote))
		}
		if _, err := e.svc.AcceptPublicQuote(ctx, "", first, q.Options[0].Row.ID, PlanStandard, PublicData{}); err == nil {
			t.Fatal("the revoked link accepted the quote")
		} else if !errors.Is(err, magiclink.ErrInvalid) && !errors.Is(err, auth.ErrNotFound) {
			t.Logf("revoked link: %v", err)
		}
		if _, err := e.svc.AcceptPublicQuote(ctx, "", second, q.Options[0].Row.ID, PlanStandard, PublicData{}); err != nil {
			t.Fatalf("current link: %v", err)
		}
	})
}

// TestInstallment_IssueJobIdempotent: parallel and repeated runs of installments.issue issue each due instalment
// once, with one audit row and one payment request each.
func TestInstallment_IssueJobIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		a := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		b := e.mooring(t, e.Owner, e.berths[1], "2027-06-10", true)
		requests := e.mailer.count(MailInstallmentPaymentRequest)
		e.Clock.Set(time.Date(2027, 9, 1, 8, 0, 0, 0, time.UTC))
		var issued atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := e.svc.RunInstallmentsIssueCount(ctx)
				if err == nil {
					issued.Add(int32(r.Issued))
				}
			}()
		}
		wg.Wait()
		if r, err := e.svc.RunInstallmentsIssueCount(ctx); err != nil || r.Issued != 0 {
			t.Fatalf("run after the parallel ones %+v %v", r, err)
		}
		if issued.Load() != 2 {
			t.Fatalf("issued %d, want 2 (one per contract)", issued.Load())
		}
		for _, c := range []string{a.Row.ID, b.Row.ID} {
			n := e.countWhere(t, "installments", "contract_id = ? AND status = 'issued' AND seq = 2", c)
			if n != 1 {
				t.Fatalf("contract %s: %d second instalments issued", c, n)
			}
		}
		var dup int
		if err := db.NewSelect().TableExpr("audit_log").ColumnExpr("count(*) - count(DISTINCT entity_id)").
			Where("action = 'installment.issue'").Scan(context.Background(), &dup); err != nil {
			t.Fatal(err)
		}
		if dup != 0 || e.mailer.count(MailInstallmentPaymentRequest) != requests+2 {
			t.Fatalf("duplicate issue audits %d, payment requests %d", dup, e.mailer.count(MailInstallmentPaymentRequest)-requests)
		}
		e.contractInvariants(t)
	})
}

// renewalCampaign creates a campaign over the contracts and previews it.
func (e *m4env) renewalCampaign(t *testing.T, ids ...string) *Campaign {
	t.Helper()
	ctx := fixture.Ctx()
	c, err := e.svc.CreateRenewalCampaign(ctx, e.Owner, e.MarinaID, CampaignInput{
		Name: "Rinnovi", Filter: CampaignFilter{ContractIDs: ids}, DeadlineOn: d("2027-07-25"),
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err = e.svc.PreviewRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRenewal_CampaignPreviewIdempotent: previewing again changes nothing: the same proposals, one draft
// successor and one hold per source.
func TestRenewal_CampaignPreviewIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		a := e.monthly(t, e.berths[0], "2027-07-01", true)
		b := e.monthly(t, e.berths[1], "2027-07-01", true)
		c := e.renewalCampaign(t, a.Row.ID, b.Row.ID)
		props := e.countWhere(t, "renewal_proposals", "campaign_id = ?", c.Row.ID)
		drafts := e.countWhere(t, "contracts", "status = 'draft' AND marina_id = ?", e.MarinaID)
		allocs := e.countWhere(t, "allocations", "marina_id = ?", e.MarinaID)
		audits := e.countWhere(t, "audit_log", "action = 'renewal_proposal.create'")
		for range 3 {
			got, err := e.svc.PreviewRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, 0)
			if err != nil || got.Counts[string(ProposalPreviewed)] != 2 {
				t.Fatalf("preview again %+v %v", got, err)
			}
		}
		if props != 2 || drafts != 2 ||
			e.countWhere(t, "renewal_proposals", "campaign_id = ?", c.Row.ID) != props ||
			e.countWhere(t, "contracts", "status = 'draft' AND marina_id = ?", e.MarinaID) != drafts ||
			e.countWhere(t, "allocations", "marina_id = ?", e.MarinaID) != allocs ||
			e.countWhere(t, "audit_log", "action = 'renewal_proposal.create'") != audits {
			t.Fatal("a repeated preview wrote something")
		}
		e.contractInvariants(t)
	})
}

// TestRenewal_SyncJobIdempotent: repeated renewals.sync runs send each due proposal once and expire past-deadline
// proposals once.
func TestRenewal_SyncJobIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		a := e.monthly(t, e.berths[0], "2027-07-01", true)
		b := e.monthly(t, e.berths[1], "2027-07-01", true)
		c := e.renewalCampaign(t, a.Row.ID, b.Row.ID)
		if _, err := e.svc.LaunchRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if _, err := e.svc.RunRenewalsSyncCount(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if e.mailer.count(MailRenewalProposal) != 2 || e.countWhere(t, "renewal_proposals", "campaign_id = ? AND sent_count = 1", c.Row.ID) != 2 {
			t.Fatalf("mails %d", e.mailer.count(MailRenewalProposal))
		}
		e.Clock.Set(time.Date(2027, 7, 26, 1, 0, 0, 0, time.UTC))
		for range 3 {
			if _, err := e.svc.RunRenewalsSyncCount(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if n := e.countWhere(t, "audit_log", "action = 'renewal_proposal.expire'"); n != 2 {
			t.Fatalf("%d expiries, want 2", n)
		}
		if n := e.countWhere(t, "renewal_proposals", "campaign_id = ? AND status = 'expired'", c.Row.ID); n != 2 {
			t.Fatalf("%d expired proposals", n)
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_CampaignLaunchAndSync: two launches and four concurrent renewals.sync runs email each proposal
// once (sent_count = 1, one mail per dedupe key).
func TestConcurrency_CampaignLaunchAndSync(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		var ids []string
		for i := range 3 {
			ids = append(ids, e.monthly(t, e.berths[i], "2027-07-01", true).Row.ID)
		}
		c := e.renewalCampaign(t, ids...)
		var launched atomic.Int32
		var wg sync.WaitGroup
		for i := range 102 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i < 2 {
					if _, err := e.svc.LaunchRenewalCampaign(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version); err == nil {
						launched.Add(1)
					}
					return
				}
				_, _ = e.svc.RunRenewalsSyncCount(ctx)
			}()
		}
		wg.Wait()
		if _, err := e.svc.RunRenewalsSyncCount(ctx); err != nil {
			t.Fatal(err)
		}
		if launched.Load() != 1 {
			t.Fatalf("%d launches succeeded", launched.Load())
		}
		if n := e.countWhere(t, "renewal_proposals", "campaign_id = ? AND sent_count = 1", c.Row.ID); n != 3 {
			t.Fatalf("%d proposals sent once", n)
		}
		if e.mailer.count(MailRenewalProposal) != 3 {
			t.Fatalf("%d proposal mails", e.mailer.count(MailRenewalProposal))
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_ContractTwoOperators: office and director act on the same contract version (activate, cancel,
// request signature, update): exactly one wins, the others get 412/409; one audit row and one event set.
func TestConcurrency_ContractTwoOperators(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverOTP)
		e.withEmail(t, e.cust)
		office := e.AddUser(t, "office@porto.test", "office", e.OrgID, e.MarinaID)
		director := e.AddUser(t, "director@porto.test", "director", e.OrgID, e.MarinaID)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", false)
		v := c.Row.Version
		before := e.tally(t, c.Row.ID)
		notes := "nota"
		var ok, lost atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p := office
				if i%2 == 1 {
					p = director
				}
				var err error
				switch i % 8 / 2 {
				case 0:
					_, err = e.svc.ActivateContract(ctx, p, e.MarinaID, c.Row.ID, v)
				case 1:
					_, err = e.svc.CancelContract(ctx, p, e.MarinaID, c.Row.ID, v, "")
				case 2:
					_, err = e.svc.RequestContractSignature(ctx, p, e.MarinaID, c.Row.ID, v, SignatureInput{})
				default:
					_, err = e.svc.UpdateContract(ctx, p, e.MarinaID, c.Row.ID, v, MooringPatch{Notes: &notes})
				}
				switch {
				case err == nil:
					ok.Add(1)
				case conflictErr(err):
					lost.Add(1)
				default:
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 || lost.Load() != 99 {
			t.Fatalf("ok %d lost %d", ok.Load(), lost.Load())
		}
		after := e.tally(t, c.Row.ID)
		if after.audit-before.audit != 1 {
			t.Fatalf("%d audit rows", after.audit-before.audit)
		}
		st := e.status(t, c.Row.ID)
		switch Status(st) {
		case StatusActive, StatusCancelled, StatusAwaitingSignature, StatusDraft:
		default:
			t.Fatalf("final status %s", st)
		}
		e.contractInvariants(t)
	})
}

// TestConcurrency_AddendumVsTerminate: activations of a price addendum race the termination of the contract; the
// contract ends terminated once, no scheduled instalment survives and an addendum is never activated after it.
func TestConcurrency_AddendumVsTerminate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		nt := int64(500_001)
		ad, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, AddendumInput{
			Kind: AddPriceChange, EffectiveOn: d("2027-07-01"), Payload: AddendumPayload{NewTotalCents: &nt, Reason: "accordo"},
		})
		if err != nil {
			t.Fatal(err)
		}
		var terms, acts atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					if _, err = e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, c.Row.ID, 0, d("2027-12-10"), "vendita"); err == nil {
						terms.Add(1)
					}
				} else if _, err = e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, ad.Row.ID, 0, ActivateInput{}); err == nil {
					acts.Add(1)
				}
				if err != nil && !conflictErr(err) {
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		if terms.Load() != 1 || acts.Load() > 1 || e.status(t, c.Row.ID) != string(StatusTerminated) {
			t.Fatalf("terminations %d activations %d status %s", terms.Load(), acts.Load(), e.status(t, c.Row.ID))
		}
		if n := e.countWhere(t, "installments", "contract_id = ? AND status = 'scheduled'", c.Row.ID); n != 0 {
			t.Fatalf("%d scheduled instalments after the termination", n)
		}
		if acts.Load() == 1 {
			var actAt, termAt time.Time
			_ = db.NewSelect().TableExpr("audit_log").Column("created_at").Where("entity_id = ?", ad.Row.ID).
				Where("action LIKE 'addendum.activ%'").Limit(1).Scan(context.Background(), &actAt)
			_ = db.NewSelect().TableExpr("audit_log").Column("created_at").Where("entity_id = ?", c.Row.ID).
				Where("action = 'contract.terminate'").Scan(context.Background(), &termAt)
			if !actAt.IsZero() && actAt.After(termAt) {
				t.Fatal("addendum activated after the termination")
			}
		}
		e.invariants(t)
	})
}

// TestConcurrency_QuotePublicAcceptVsPanelAccept: the customer accepting through the link races the office
// accepting from the panel: exactly one contract is created.
func TestConcurrency_QuotePublicAcceptVsPanelAccept(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		e.withEmail(t, e.cust)
		q := e.quote(t, e.Owner, KindMooring, TermAnnual, "2027-07-01", "2028-07-01", true, e.berths[3])
		if _, err := e.svc.SendQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, false); err != nil {
			t.Fatal(err)
		}
		token := e.links.tokenOf(q.Row.ID)
		opt := q.Options[0].Row.ID
		var ok atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = e.svc.AcceptPublicQuote(ctx, "", token, opt, PlanStandard, PublicData{})
				} else {
					_, err = e.svc.AcceptQuoteM4(ctx, e.Owner, e.MarinaID, q.Row.ID, 0, AcceptInput{OptionID: opt})
				}
				if err == nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 {
			t.Fatalf("%d acceptances succeeded", ok.Load())
		}
		if n := e.countWhere(t, "contracts", "quote_id = ?", q.Row.ID); n != 1 {
			t.Fatalf("%d contracts for the quote", n)
		}
		if n := e.countWhere(t, "audit_log", "entity_id = ? AND action LIKE 'quote.accept%'", q.Row.ID); n != 1 {
			t.Fatalf("%d acceptance audits", n)
		}
		e.contractInvariants(t)
	})
}

// TestContract_ChangeVesselProRata: a change of vessel prices the remaining nights on both vessels and records the
// delta new_remaining − old_remaining; the allocations are truncated at the effective date and allocated again
// with the new vessel; the plan keeps summing to the total (INV-27, INV-31).
func TestContract_ChangeVesselProRata(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		e.setPolicy(t, DriverPaper)
		c := e.mooring(t, e.Owner, e.berths[0], "2027-06-10", true)
		v2 := e.addVessel(t, "Grecale", 1100, 370, "monohull_sail")
		e.insure(v2)
		eff := d("2027-09-01")
		ad, err := e.svc.CreateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, AddendumInput{Kind: AddVesselChange, EffectiveOn: eff, Payload: AddendumPayload{VesselID: v2}})
		if err != nil {
			t.Fatal(err)
		}
		if ad.Row.AmountDeltaCents != ad.Payload.NewRemainingCents-ad.Payload.OldRemainingCents || ad.Payload.OldRemainingCents <= 0 {
			t.Fatalf("delta %d, remaining %d → %d", ad.Row.AmountDeltaCents, ad.Payload.OldRemainingCents, ad.Payload.NewRemainingCents)
		}
		if _, err := e.svc.ActivateAddendum(ctx, e.Owner, e.MarinaID, c.Row.ID, ad.Row.ID, ad.Row.Version, ActivateInput{}); err != nil {
			t.Fatal(err)
		}
		got, err := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Row.VesselID != v2 || got.Row.TotalCents != c.Row.TotalCents+ad.Row.AmountDeltaCents {
			t.Fatalf("contract vessel %s total %d", got.Row.VesselID, got.Row.TotalCents)
		}
		var oldLeg, newLeg bool
		for _, al := range got.Allocations {
			if al.Status != store.AllocActive {
				continue
			}
			switch {
			case al.VesselID == e.vessel && al.LastNight.Equal(eff.AddDays(-1)):
				oldLeg = true
			case al.VesselID == v2 && al.FirstNight.Equal(eff):
				newLeg = true
			case al.VesselID == e.vessel && !al.LastNight.Before(eff):
				t.Fatalf("old vessel still allocated from the effective date: %+v", al)
			}
		}
		if !oldLeg || !newLeg {
			t.Fatalf("allocations %+v", got.Allocations)
		}
		if p := e.plan(t, c.Row.ID); p.Sum != p.Total {
			t.Fatalf("plan %+v", p)
		}
		e.contractInvariants(t)
	})
}

// TestImport_UndoGuard: an import cannot be undone once a created contract changed (version > 1); nothing is
// undone and the contract stays as it is.
func TestImport_UndoGuard(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := withFakes(newTenv(t, db))
		ctx := fixture.Ctx()
		if _, err := e.DB.NewUpdate().TableExpr("vessels").Set("registration = 'RM1234'").Where("id = ?", e.vessel).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		tbl, err := csvimport.Parse("ref;customer_id;vessel_registration;berth_code;term;starts_on;ends_on;total;due_on;amount;status\n"+
			"C1;"+e.cust+";rm1234;A3;annual;2027-01-01;2028-01-01;4800,00;2027-07-15;4800,00;scheduled\n", ';', true)
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
		var created []csvimport.Created
		if err := db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			var err error
			created, err = h.Apply(ctx, tx, scope, rows, &csvimport.Report{})
			return err
		}); err != nil || len(created) != 1 {
			t.Fatalf("apply %v", err)
		}
		id := created[0].EntityID
		notes := "nota aggiunta dopo l'import"
		c, err := e.svc.UpdateContract(ctx, e.Owner, e.MarinaID, id, 0, MooringPatch{Notes: &notes})
		if err != nil {
			t.Fatal(err)
		}
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error { return h.Undo(ctx, tx, scope, created) })
		if !errors.Is(err, csvimport.ErrNotUndoable) {
			t.Fatalf("undo of a changed contract: %v", err)
		}
		if st := e.status(t, id); st != c.Row.Status {
			t.Fatalf("status after a refused undo %s", st)
		}
		e.contractInvariants(t)
	})
}
