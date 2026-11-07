//go:build integration

package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"
	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestPayment_RecordMatchReverseRefund(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a := e.addCharge(t, chargeOpts{due: d("2027-06-05")}, line{60_00, 2200}, line{40_00, 1000})
		b := e.addCharge(t, chargeOpts{due: d("2027-06-20")}, line{80_00, 0})

		p := e.record(t, RecordInput{Amount: 150_00, AutoMatch: AutoMatchOldestDueFirst, Reference: "CRO 1234"})
		if p.AllocatedCents != 150_00 || len(p.Matches) != 2 || !p.HasReference {
			t.Fatalf("payment %+v", p.Payment)
		}
		if ca := e.charge(t, a.ID); ca.PaymentStatus != store.ChargePaid || ca.PaidAmountCents != 100_00 {
			t.Fatalf("charge a %s %d", ca.PaymentStatus, ca.PaidAmountCents)
		}
		if cb := e.charge(t, b.ID); cb.PaymentStatus != store.ChargePartiallyPaid || cb.PaidAmountCents != 50_00 {
			t.Fatalf("charge b %s %d", cb.PaymentStatus, cb.PaidAmountCents)
		}
		// The VAT split: all of a's buckets, half of b's (N1).
		var gross money.Cents
		for _, bk := range p.Split {
			gross += bk.Gross
			if bk.Net+bk.VAT != bk.Gross {
				t.Fatalf("bucket %+v", bk)
			}
		}
		if gross != 150_00 || len(p.Split) != 3 {
			t.Fatalf("split %+v", p.Split)
		}
		if len(e.Events(t, event.ChargePaid)) != 1 || len(e.Events(t, event.PaymentSucceeded)) != 1 {
			t.Fatal("events")
		}

		// Nothing unallocated: a refund needs a reversal first.
		if _, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, p.ID, RefundInput{Amount: 30_00, Reason: "errore"}); !errors.Is(err, ErrRefundExceeds) {
			t.Fatalf("refund without reversal: %v", err)
		}
		var bMatch string
		for _, m := range p.Matches {
			if m.ChargeID == b.ID {
				bMatch = m.ID
			}
		}
		r, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, p.ID, RefundInput{
			Amount: 30_00, Reason: "doppio pagamento", Unmatch: []UnmatchInput{{MatchID: bMatch, Amount: 30_00}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != store.RefundSucceeded || r.Method != store.PaymentMethodBankTransfer || r.Number == "" {
			t.Fatalf("refund %+v", r)
		}
		if cb := e.charge(t, b.ID); cb.PaidAmountCents != 20_00 || cb.PaymentStatus != store.ChargePartiallyPaid {
			t.Fatalf("charge b after reversal %+v", cb)
		}
		pp := e.payment(t, p.ID)
		if pp.AllocatedCents != 120_00 || pp.RefundedCents != 30_00 {
			t.Fatalf("payment after refund %+v", pp)
		}
		// A match is reversed at most once.
		if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, p.ID, bMatch); !errors.Is(err, ErrAlreadyReversed) {
			t.Fatalf("second reversal: %v", err)
		}
		// Reversing a's match reopens it.
		var aMatch string
		for _, m := range p.Matches {
			if m.ChargeID == a.ID {
				aMatch = m.ID
			}
		}
		if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, p.ID, aMatch); err != nil {
			t.Fatal(err)
		}
		if ca := e.charge(t, a.ID); ca.PaymentStatus != store.ChargeOpen || ca.PaidAmountCents != 0 {
			t.Fatalf("charge a reopened %+v", ca)
		}
		if got := e.payment(t, p.ID).UnallocatedCents(); got != 100_00 {
			t.Fatalf("unallocated %d", got)
		}
		// Matching above the charge residual or the payment fails with the limit.
		_, err = e.svc.Match(ctx, e.Owner, e.MarinaID, p.ID, []MatchInput{{ChargeID: b.ID, Amount: 70_00}})
		var le *LimitError
		if !errors.As(err, &le) || le.Limit != LimitCharge || le.ChargeID != b.ID {
			t.Fatalf("match above charge: %v", err)
		}
		_, err = e.svc.Match(ctx, e.Owner, e.MarinaID, p.ID, []MatchInput{{ChargeID: a.ID, Amount: 100_00}, {ChargeID: b.ID, Amount: 10_00}})
		if !errors.As(err, &le) || le.Limit != LimitPayment {
			t.Fatalf("match above payment: %v", err)
		}
		e.invariants(t)
		if e.AuditCount(t, "payment.record") != 1 || e.AuditCount(t, "payment.unmatch") != 2 || e.AuditCount(t, "refund.create") != 1 {
			t.Fatal("audit")
		}
	})
}

func TestPayment_RecordValidationAndPermissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		if _, err := e.svc.Record(ctx, e.Owner, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "payment", Method: "card_online", Amount: 1}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("online method by hand: %v", err)
		}
		if _, err := e.svc.Record(ctx, e.Owner, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "credit", Method: "credit_note", Amount: 1}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("credit by hand: %v", err)
		}
		if _, err := e.svc.Record(ctx, e.Owner, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "payment", Method: "cash", Amount: 0}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("zero amount: %v", err)
		}
		// Cash without an open session.
		e.cash.err = errNoSession
		if _, err := e.svc.Record(ctx, e.Owner, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "payment", Method: "cash", Amount: 10_00}); !errors.Is(err, errNoSession) {
			t.Fatalf("cash without session: %v", err)
		}
		e.cash.err = nil
		p := e.record(t, RecordInput{Method: "cash", Amount: 10_00})
		if p.CashSessionID != e.cash.sessionID {
			t.Fatalf("cash session %q", p.CashSessionID)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.svc.Record(ctx, dock, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "payment", Method: "card_pos", Amount: 10_00}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock card: %v", err)
		}
		if _, err := e.svc.Record(ctx, dock, e.MarinaID, RecordInput{CustomerID: e.Customer, Kind: "payment", Method: "cash", Amount: 10_00}); err != nil {
			t.Fatalf("dock cash: %v", err)
		}
		if _, err := e.svc.Refund(ctx, dock, e.MarinaID, p.ID, RefundInput{Amount: 1, Reason: "x"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock refund: %v", err)
		}
		_, other, owner2 := e.OtherOrganization(t)
		if _, err := e.svc.Get(ctx, owner2, other, p.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("cross marina get: %v", err)
		}
		if _, err := e.svc.Get(ctx, e.Owner, other, p.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		e.invariants(t)
	})
}

func TestPayment_IdempotentRecord(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.addCharge(t, chargeOpts{})
		in := RecordInput{
			CustomerID: e.Customer, Kind: "payment", Method: "bank_transfer", Amount: 40_00, IdempotencyKey: "pay-key-0001",
			Matches: []MatchInput{{ChargeID: c.ID, Amount: 40_00}},
		}
		p1, err := e.svc.Record(ctx, e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		p2, err := e.svc.Record(ctx, e.Owner, e.MarinaID, in)
		if err != nil || p2.ID != p1.ID {
			t.Fatalf("replay %v %v", p2, err)
		}
		in.Amount = 41_00
		in.Matches[0].Amount = 41_00
		if _, err := e.svc.Record(ctx, e.Owner, e.MarinaID, in); !errors.Is(err, ErrIdempotencyMismatch) {
			t.Fatalf("different body: %v", err)
		}
		if n := e.count(t, "payments", "marina_id = ?", e.MarinaID); n != 1 {
			t.Fatalf("payments %d", n)
		}
		if got := e.charge(t, c.ID).PaidAmountCents; got != 40_00 {
			t.Fatalf("paid %d", got)
		}
		e.invariants(t)
	})
}

func TestCredits_UnallocatedIsCredit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a := e.addCharge(t, chargeOpts{}, line{70_00, 2200})
		e.record(t, RecordInput{Amount: 100_00, AutoMatch: AutoMatchOldestDueFirst})
		row, pays, err := e.svc.CustomerCredit(ctx, e.Owner, e.MarinaID, e.Customer)
		if err != nil {
			t.Fatal(err)
		}
		if row.Credit != 30_00 || len(pays) != 1 {
			t.Fatalf("credit %+v %d", row, len(pays))
		}
		if e.charge(t, a.ID).PaymentStatus != store.ChargePaid {
			t.Fatal("charge a not paid")
		}
		// A goodwill credit adds to it; deposits are listed apart and are not credit.
		if _, err := e.svc.GrantCredit(ctx, e.Owner, e.MarinaID, e.Customer, CreditInput{Amount: 5_00, Reason: "disagio", Source: "goodwill"}, ""); err != nil {
			t.Fatal(err)
		}
		e.record(t, RecordInput{Kind: "deposit", Method: "cash", Amount: 50_00})
		row, _, _ = e.svc.CustomerCredit(ctx, e.Owner, e.MarinaID, e.Customer)
		if row.Credit != 35_00 || row.Deposits != 50_00 {
			t.Fatalf("credit %+v", row)
		}
		// Applying credit pays new charges oldest first; the deposit is never used.
		b := e.addCharge(t, chargeOpts{due: d("2027-07-01")}, line{20_00, 2200})
		c := e.addCharge(t, chargeOpts{due: d("2027-08-01")}, line{40_00, 2200})
		got, err := e.svc.ApplyCredit(ctx, e.Owner, e.MarinaID, e.Customer, nil)
		if err != nil || got != 35_00 {
			t.Fatalf("apply %d %v", got, err)
		}
		if e.charge(t, b.ID).PaymentStatus != store.ChargePaid || e.charge(t, c.ID).PaidAmountCents != 15_00 {
			t.Fatal("credit application order")
		}
		if _, err := e.svc.ApplyCredit(ctx, e.Owner, e.MarinaID, e.Customer, nil); !errors.Is(err, ErrCreditInsufficient) {
			t.Fatalf("no credit left: %v", err)
		}
		bal, credit, err := e.svc.Balance(ctx, e.Owner, e.MarinaID, e.Customer)
		if err != nil || credit != 0 || bal != 25_00 {
			t.Fatalf("balance %d credit %d %v", bal, credit, err)
		}
		// A release credit needs a resold release of the customer.
		if _, err := e.svc.GrantCredit(ctx, e.Owner, e.MarinaID, e.Customer, CreditInput{Amount: 5_00, Reason: "rilascio", Source: "release", ReleaseID: store.NewID()}, ""); !errors.Is(err, ErrReleaseNotResold) {
			t.Fatalf("release credit: %v", err)
		}
		e.invariants(t)
	})
}

func TestStatement_ClosingEqualsBalance(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a := e.addCharge(t, chargeOpts{due: d("2027-05-01")}, line{100_00, 2200})
		e.addCharge(t, chargeOpts{due: d("2027-06-30")}, line{55_55, 1000})
		p := e.record(t, RecordInput{Amount: 120_00, AutoMatch: AutoMatchOldestDueFirst})
		if _, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, p.ID, RefundInput{
			Amount: 1_00, Reason: "arrotondamento",
			Unmatch: []UnmatchInput{{MatchID: p.Matches[1].ID, Amount: 1_00}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.GrantCredit(ctx, e.Owner, e.MarinaID, e.Customer, CreditInput{Amount: 3_00, Reason: "omaggio", Source: "goodwill"}, ""); err != nil {
			t.Fatal(err)
		}
		e.record(t, RecordInput{Kind: "deposit", Method: "cash", Amount: 10_00})
		// Opening balances: a debt and a credit (the negative charge grants an opening_balance credit tied to it,
		// which must not be a second row of the statement).
		e.charges.SetCreditGranter(e.svc)
		var itemID string
		if err := e.DB.NewSelect().TableExpr("items").Column("id").Where("marina_id = ?", e.MarinaID).OrderExpr("id").Limit(1).Scan(context.Background(), &itemID); err != nil {
			t.Fatal(err)
		}
		if err := e.DB.RunInTx(context.Background(), func(c context.Context, tx bun.Tx) error {
			return store.ReplaceOriginItems(c, tx, e.scope, map[string]string{"opening_balance": itemID}, e.Clock.Now())
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.DB.NewUpdate().Model((*store.Customer)(nil)).Set("fiscal_code = 'BNCMRA80A01H501Z'").Where("id = ?", e.Customer).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.charges.ImportOpeningBalances(ctx, e.Owner, e.MarinaID, charge.OpeningInput{
			Format: "csv", File: []byte("cf;importo;scadenza\nBNCMRA80A01H501Z;20,00;31/07/2027\nBNCMRA80A01H501Z;-7,00;31/07/2027\n"), HasHeader: true,
			Mapping: charge.OpeningMapping{CustomerMatch: "fiscal_code", CustomerColumn: 0, AmountColumn: 1, DueOnColumn: 2, DescriptionColumn: -1, ReferenceColumn: -1},
		}); err != nil {
			t.Fatal(err)
		}
		st, err := e.svc.CustomerStatement(ctx, e.Owner, e.MarinaID, e.Customer, d("2027-01-01"), calendarZero())
		if err != nil {
			t.Fatal(err)
		}
		bal, _, err := e.svc.Balance(ctx, e.Owner, e.MarinaID, e.Customer)
		if err != nil {
			t.Fatal(err)
		}
		if st.Closing != bal {
			t.Fatalf("closing %d ≠ balance %d (%+v)", st.Closing, bal, st.Rows)
		}
		if bal != 155_55-120_00+1_00-3_00+20_00-7_00 {
			t.Fatalf("balance %d", bal)
		}
		if len(StatementCSV(st)) == 0 {
			t.Fatal("csv")
		}
		_ = a
	})
}

// TestCharge_PaidAmountMatchesLedger (INV-50): random sequences of record, match, reverse, refund, credit grant
// and application keep B50/B52 empty after each step.
func TestCharge_PaidAmountMatchesLedger(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		rapid.Check(t, func(rt *rapid.T) {
			var charges []string
			var pays []string
			for range rapid.IntRange(1, 4).Draw(rt, "charges") {
				charges = append(charges, e.addCharge(t, chargeOpts{due: d("2027-06-01").AddDays(rapid.IntRange(-30, 30).Draw(rt, "due"))},
					line{money.Cents(rapid.Int64Range(1, 50_000).Draw(rt, "gross")), rapid.SampledFrom([]int64{0, 1000, 2200}).Draw(rt, "rate")}).ID)
			}
			for range rapid.IntRange(1, 12).Draw(rt, "steps") {
				switch rapid.IntRange(0, 5).Draw(rt, "op") {
				case 0:
					p, err := e.svc.Record(ctx, e.Owner, e.MarinaID, RecordInput{
						CustomerID: e.Customer, Kind: "payment", Method: "bank_transfer",
						Amount: money.Cents(rapid.Int64Range(1, 60_000).Draw(rt, "amount")), AutoMatch: AutoMatchOldestDueFirst,
					})
					if err != nil {
						rt.Fatalf("record: %v", err)
					}
					pays = append(pays, p.ID)
				case 1:
					if len(pays) == 0 {
						continue
					}
					pid := rapid.SampledFrom(pays).Draw(rt, "pay")
					cid := rapid.SampledFrom(charges).Draw(rt, "charge")
					_, err := e.svc.Match(ctx, e.Owner, e.MarinaID, pid, []MatchInput{{ChargeID: cid, Amount: money.Cents(rapid.Int64Range(1, 30_000).Draw(rt, "m"))}})
					if err != nil && !errors.Is(err, ErrMatchExceeds) {
						rt.Fatalf("match: %v", err)
					}
				case 2:
					if len(pays) == 0 {
						continue
					}
					pid := rapid.SampledFrom(pays).Draw(rt, "pay")
					det, err := e.svc.Get(ctx, e.Owner, e.MarinaID, pid)
					if err != nil {
						rt.Fatal(err)
					}
					if len(det.Matches) == 0 {
						continue
					}
					m := rapid.SampledFrom(det.Matches).Draw(rt, "match")
					if _, err := e.svc.ReverseMatch(ctx, e.Owner, e.MarinaID, pid, m.ID); err != nil && !errors.Is(err, ErrAlreadyReversed) {
						rt.Fatalf("reverse: %v", err)
					}
				case 3:
					if len(pays) == 0 {
						continue
					}
					pid := rapid.SampledFrom(pays).Draw(rt, "pay")
					_, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, pid, RefundInput{Amount: money.Cents(rapid.Int64Range(1, 20_000).Draw(rt, "r")), Reason: "test"})
					if err != nil && !errors.Is(err, ErrRefundExceeds) {
						rt.Fatalf("refund: %v", err)
					}
				case 4:
					if _, err := e.svc.GrantCredit(ctx, e.Owner, e.MarinaID, e.Customer, CreditInput{Amount: money.Cents(rapid.Int64Range(1, 5_000).Draw(rt, "c")), Reason: "test", Source: "goodwill"}, ""); err != nil {
						rt.Fatal(err)
					}
				case 5:
					if _, err := e.svc.ApplyCredit(ctx, e.Owner, e.MarinaID, e.Customer, nil); err != nil && !errors.Is(err, ErrCreditInsufficient) {
						rt.Fatalf("apply: %v", err)
					}
				}
				vs, err := store.CheckPaymentsInvariants(context.Background(), e.DB.DB, e.scope)
				if err != nil {
					rt.Fatal(err)
				}
				for _, v := range vs {
					if v.Query == "B50" || v.Query == "B52" || v.Query == "B52c" {
						rt.Fatalf("violation %+v", v)
					}
				}
			}
			st, err := e.svc.CustomerStatement(ctx, e.Owner, e.MarinaID, e.Customer, d("2020-01-01"), calendarZero())
			if err != nil {
				rt.Fatal(err)
			}
			bal, _, err := e.svc.Balance(ctx, e.Owner, e.MarinaID, e.Customer)
			if err != nil {
				rt.Fatal(err)
			}
			if st.Closing != bal {
				rt.Fatalf("statement closing %d ≠ balance %d", st.Closing, bal)
			}
		})
	})
}

func TestWorklist_Assign(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		p := e.record(t, RecordInput{Amount: 10_00})
		if err := store.RaiseWorkItem(context.Background(), e.DB.DB, e.scope, WorkLatePayment, p.ID, "", e.Clock.Now()); err != nil {
			t.Fatal(err)
		}
		page, err := e.svc.Worklist(ctx, e.Owner, e.MarinaID, WorkFilter{}, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, it := range page.Items {
			if it.Kind == WorkLatePayment && it.SourceID == p.ID && it.Status == store.WorkItemOpen {
				found = true
			}
		}
		if !found {
			t.Fatalf("late payment item missing: %+v", page.Items)
		}
		owner := e.OwnerID
		it, err := e.svc.UpdateWorkItem(ctx, e.Owner, e.MarinaID, WorkLatePayment, p.ID, WorkPatch{AssigneeUserID: &owner, Status: store.WorkItemInProgress})
		if err != nil || it.AssigneeUserID != owner || it.Status != store.WorkItemInProgress {
			t.Fatalf("assign %+v %v", it, err)
		}
		bogus := store.NewID()
		if _, err := e.svc.UpdateWorkItem(ctx, e.Owner, e.MarinaID, WorkLatePayment, p.ID, WorkPatch{AssigneeUserID: &bogus, Status: "open"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("unknown assignee: %v", err)
		}
		if _, err := e.svc.UpdateWorkItem(ctx, e.Owner, e.MarinaID, WorkLatePayment, p.ID, WorkPatch{Status: store.WorkItemDone}); err != nil {
			t.Fatal(err)
		}
		page, _ = e.svc.Worklist(ctx, e.Owner, e.MarinaID, WorkFilter{}, "", 0)
		for _, it := range page.Items {
			if it.SourceID == p.ID {
				t.Fatal("done items are hidden by default")
			}
		}
		page, _ = e.svc.Worklist(ctx, e.Owner, e.MarinaID, WorkFilter{Status: store.WorkItemDone, AssigneeUserID: owner}, "", 0)
		if len(page.Items) != 1 {
			t.Fatalf("done filter %+v", page.Items)
		}
		if _, err := e.svc.UpdateWorkItem(ctx, e.Owner, e.MarinaID, "charge", store.NewID(), WorkPatch{Status: "done"}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown source: %v", err)
		}
		if e.AuditCount(t, "work_item.update") != 2 {
			t.Fatal("audit")
		}
	})
}
