//go:build integration

package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// mandateSetup inserts a berth right with two holders (600 and 400 millis), a mandate with a guaranteed minimum
// and two revenue rows on transit allocations.
func (e *env) mandateSetup(t *testing.T, minGuaranteed int64) (*store.ManagementMandate, string) {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	second := e.AddCustomer(t, "Bianchi")
	right := &store.BerthRight{
		ID: store.NewID(), MarinaID: e.MarinaID, BerthID: e.Berths[3], Kind: "ownership_share", FromOn: d("2020-01-01"),
		ToOn: d("2040-12-31"), Title: store.JSON(`{}`), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	m := &store.ManagementMandate{
		ID: store.NewID(), MarinaID: e.MarinaID, RightID: right.ID, FromOn: d("2027-06-01"), ToOn: d("2027-06-30"),
		OwnerShareBP: 6000, MinGuaranteedCents: minGuaranteed, Channels: store.Strings{"transit"}, Settlement: "credit",
		Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, right,
		&store.BerthRightHolder{ID: store.NewID(), MarinaID: e.MarinaID, RightID: right.ID, CustomerID: e.Customer, ShareMillis: 600, CreatedAt: now, UpdatedAt: now},
		&store.BerthRightHolder{ID: store.NewID(), MarinaID: e.MarinaID, RightID: right.ID, CustomerID: second, ShareMillis: 400, CreatedAt: now, UpdatedAt: now},
		m)
	for i, through := range []string{"2027-06-05", "2027-06-12"} {
		tr := e.Transit(t, e.Berths[i], "2027-06-0"+string(rune('2'+i)), "2027-06-0"+string(rune('4'+i)), true)
		allocs, err := store.AllocationsOfContracts(context.Background(), e.DB.DB, e.scope, []string{tr.Row.ID})
		if err != nil || len(allocs) == 0 {
			t.Fatalf("allocations %v", err)
		}
		e.Insert(t, &store.MandateRevenue{
			ID: store.NewID(), MarinaID: e.MarinaID, MandateID: m.ID, AllocationID: allocs[0].ID, ContractID: tr.Row.ID,
			Nights: 2, RevenueCents: 10_000, OwnerShareCents: 6_001, ThroughOn: d(through), CreatedAt: now, UpdatedAt: now,
		})
	}
	return m, second
}

func TestMandate_SettleCredit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		m, second := e.mandateSetup(t, 20_000)
		// Period open: only the shares through the date, split 600/400 by largest remainder.
		res, err := e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-06-10"), SettleCredit)
		if err != nil {
			t.Fatal(err)
		}
		if res.Credited != 6_001 || res.PaidOut != 0 {
			t.Fatalf("first settlement %+v", res)
		}
		c1, _ := store.CreditOf(context.Background(), e.DB.DB, e.scope, e.Customer)
		c2, _ := store.CreditOf(context.Background(), e.DB.DB, e.scope, second)
		if c1.CreditCents+c2.CreditCents != 6_001 || c1.CreditCents != 3_601 {
			t.Fatalf("split %d %d", c1.CreditCents, c2.CreditCents)
		}
		if _, err := e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-06-10"), SettleCredit); !errors.Is(err, ErrNothingToSettle) {
			t.Fatalf("again: %v", err)
		}
		// Closing the period pays the rest up to the guaranteed minimum, once.
		res, err = e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-06-30"), SettleCredit)
		if err != nil || res.Credited != 20_000-6_001 {
			t.Fatalf("closing %+v %v", res, err)
		}
		if _, err := e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-07-31"), SettleCredit); !errors.Is(err, ErrNothingToSettle) {
			t.Fatalf("after closing: %v", err)
		}
		if n := e.count(t, "mandate_revenues", "mandate_id = ? AND settled_at IS NULL", m.ID); n != 0 {
			t.Fatalf("unsettled revenues %d", n)
		}
		e.invariants(t)
	})
}

func TestMandate_SettlePayout(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		m, _ := e.mandateSetup(t, 0)
		res, err := e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-06-30"), SettlePayout)
		if err != nil {
			t.Fatal(err)
		}
		if res.PaidOut != 12_002 || res.Credited != 0 {
			t.Fatalf("payout %+v", res)
		}
		if n := e.count(t, "refunds", "mandate_id = ? AND kind = 'payout' AND status = 'succeeded'", m.ID); n != 2 {
			t.Fatalf("payouts %d", n)
		}
		if c, _ := store.CreditOf(context.Background(), e.DB.DB, e.scope, e.Customer); c.CreditCents != 0 {
			t.Fatalf("a payout is not credit: %d", c.CreditCents)
		}
		if _, err := e.svc.SettleMandate(ctx, e.Owner, e.MarinaID, m.ID, d("2027-06-30"), "wire"); err == nil {
			t.Fatal("invalid mode accepted")
		}
		office := e.AddUser(t, "office@porto.test", "office", e.OrgID, e.MarinaID)
		if _, err := e.svc.SettleMandate(ctx, office, e.MarinaID, m.ID, d("2027-06-30"), SettlePayout); err == nil {
			t.Fatal("office may not settle mandates (refunds.issue)")
		}
		e.invariants(t)
	})
}

func TestDeposit_RetainAndRelease(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		now := store.UTC(e.Clock.Now())
		item := &store.Item{
			ID: store.NewID(), MarinaID: e.MarinaID, Code: "CAUZIONE", Name: store.JSON(`{"it":"Cauzione trattenuta"}`), Description: store.JSON(`{}`),
			ItemType: "service", Unit: "piece", VATRateBP: 0, VATNature: "N1", PriceIncludesVAT: true, Active: true, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if rc, err := store.ListRevenueClasses(context.Background(), e.DB.DB, e.scope, false); err == nil && len(rc) > 0 {
			item.RevenueClassID = rc[0].ID
		}
		e.Insert(t, item)
		dep := e.record(t, RecordInput{Kind: "deposit", Method: "cash", Amount: 50_00})
		// A deposit is never matched to ordinary charges.
		c := e.addCharge(t, chargeOpts{})
		if _, err := e.svc.Match(ctx, e.Owner, e.MarinaID, dep.ID, []MatchInput{{ChargeID: c.ID, Amount: 10_00}}); !errors.Is(err, ErrNotDeposit) {
			t.Fatalf("deposit matched: %v", err)
		}
		if _, err := e.svc.Retain(ctx, e.Owner, e.MarinaID, dep.ID, RetainInput{Amount: 60_00, ItemID: item.ID, Reason: "chiavi"}); !errors.Is(err, ErrRefundExceeds) {
			t.Fatalf("retain above deposit: %v", err)
		}
		ch, err := e.svc.Retain(ctx, e.Owner, e.MarinaID, dep.ID, RetainInput{Amount: 20_00, ItemID: item.ID, Reason: "chiavi perse"})
		if err != nil {
			t.Fatal(err)
		}
		if ch.OriginType != store.ChargeOriginDepositRetention || ch.PaymentStatus != store.ChargePaid || ch.TotalCents != 20_00 {
			t.Fatalf("retention %+v", ch)
		}
		other := e.record(t, RecordInput{Amount: 1_00})
		if _, err := e.svc.Retain(ctx, e.Owner, e.MarinaID, other.ID, RetainInput{Amount: 1_00, ItemID: item.ID, Reason: "x"}); !errors.Is(err, ErrNotDeposit) {
			t.Fatalf("retain of a payment: %v", err)
		}
		// Release = refund of the rest.
		r, err := e.svc.Refund(ctx, e.Owner, e.MarinaID, dep.ID, RefundInput{Amount: 30_00, Reason: "restituzione cauzione"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Method != store.PaymentMethodCash || r.CashSessionID != e.cash.sessionID {
			t.Fatalf("release %+v", r)
		}
		if p := e.payment(t, dep.ID); p.UnallocatedCents() != 0 {
			t.Fatalf("deposit left %d", p.UnallocatedCents())
		}
		// The statement counts only what was retained.
		bal, _, _ := e.svc.Balance(ctx, e.Owner, e.MarinaID, e.Customer)
		st, err := e.svc.CustomerStatement(ctx, e.Owner, e.MarinaID, e.Customer, d("2027-01-01"), calendar.Date{})
		if err != nil || st.Closing != bal {
			t.Fatalf("statement %d balance %d %v", st.Closing, bal, err)
		}
		e.invariants(t)
	})
}
