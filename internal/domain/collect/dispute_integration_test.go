//go:build integration

package collect

import (
	"testing"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// TestPaymentWebhook_DisputeRaisesWorkItem (m5-plan §1.2): a dispute (chargeback) on a provider payment is
// recorded as ignored, moves no money and raises a dispute worklist item on the payment; a duplicate delivery and a
// dispute of a payment that is not ours change nothing.
func TestPaymentWebhook_DisputeRaisesWorkItem(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Contestato")
		e.enableFake(t)
		ch := e.charge(t, cust, 70_00, e.today().AddDays(3))
		v, err := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tokenOf(v.URL), "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		if err := e.webhook(t, ext.PaymentEvent{
			EventID: "paid", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_disp", AmountCents: 70_00,
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}); err != nil {
			t.Fatal(err)
		}
		pay, err := store.PaymentByProviderRef(ctx(), e.DB.DB, e.scope, fixture.FakeProviderCode, "pi_disp")
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := e.webhook(t, ext.PaymentEvent{EventID: "dispute", Kind: ext.PaymentEventDisputed, PaymentRef: "pi_disp", AmountCents: 70_00}); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.webhook(t, ext.PaymentEvent{EventID: "foreign", Kind: ext.PaymentEventDisputed, PaymentRef: "pi_other"}); err != nil {
			t.Fatal(err)
		}
		st, err := store.GetWorkItemState(ctx(), e.DB.DB, e.scope, payment.WorkDispute, pay.ID)
		if err != nil || st.Status != store.WorkItemOpen {
			t.Fatalf("dispute item %+v %v", st, err)
		}
		if n := e.count(t, "work_item_states", "marina_id = ? AND source_type = ?", e.MarinaID, payment.WorkDispute); n != 1 {
			t.Fatalf("dispute items %d", n)
		}
		if n := e.count(t, "payment_provider_events", "marina_id = ? AND event_id = ? AND status = ?", e.MarinaID, "dispute", EventIgnored); n != 1 {
			t.Fatalf("dispute event rows %d", n)
		}
		if after, _ := store.GetPayment(ctx(), e.DB.DB, e.scope, pay.ID); after.AmountCents != pay.AmountCents || after.AllocatedCents != pay.AllocatedCents {
			t.Fatalf("payment changed %+v", after)
		}
		if e.reload(t, ch.ID).PaymentStatus != store.ChargePaid {
			t.Fatal("charge changed")
		}
		// The office closes the item; a second dispute on the same payment reopens it.
		if _, err := e.payments.UpdateWorkItem(ctx(), e.Owner, e.MarinaID, payment.WorkDispute, pay.ID, payment.WorkPatch{Status: store.WorkItemDone}); err != nil {
			t.Fatal(err)
		}
		if err := e.webhook(t, ext.PaymentEvent{EventID: "dispute-2", Kind: ext.PaymentEventDisputed, PaymentRef: "pi_disp", AmountCents: 70_00}); err != nil {
			t.Fatal(err)
		}
		if st, err := store.GetWorkItemState(ctx(), e.DB.DB, e.scope, payment.WorkDispute, pay.ID); err != nil || st.Status != store.WorkItemOpen {
			t.Fatalf("second dispute item %+v %v", st, err)
		}
		e.invariants(t)
	})
}
