//go:build integration

package collect

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/collect/levels"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func TestPaymentRequest_PublicPage(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Bianchi")
		ch1 := e.charge(t, cust, 120_00, e.today().AddDays(10))
		ch2 := e.charge(t, cust, 30_00, e.today().AddDays(20))
		v, err := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch2.ID, ch1.ID}, Send: true})
		if err != nil {
			t.Fatal(err)
		}
		if v.Request.AmountCents != 150_00 || v.Request.SentCount != 1 || !strings.HasPrefix(v.URL, "https://porto.test/b/marina-test/pay/") {
			t.Fatalf("request %+v %s", v.Request, v.URL)
		}
		tok := tokenOf(v.URL)
		// TestPayToken_HashOnly: only the SHA-256 of the token is stored.
		if v.Request.TokenHash != HashToken(tok) || e.count(t, "payment_requests", "token_hash = ?", tok) != 0 {
			t.Fatal("token stored in clear")
		}
		page, err := e.svc.GetPayPage(ctx(), "marina-test", tok)
		if err != nil {
			t.Fatal(err)
		}
		if page.Online || len(page.Methods) != 0 || page.Bank == nil || page.Bank.IBAN == "" || page.Amount != 150_00 ||
			len(page.Charges) != 2 || page.Charges[0].Number != ch1.Number || !strings.Contains(page.Bank.Reference, ch2.Number) {
			t.Fatalf("manual-only page %+v %+v", page, page.Bank)
		}
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card"); !errors.Is(err, ErrProviderDisabled) {
			t.Fatalf("checkout without provider: %v", err)
		}
		// Re-send keeps the link; rotate changes it.
		v2, err := e.svc.SendRequest(ctx(), e.Owner, e.MarinaID, v.Request.ID, v.Request.Version, false)
		if err != nil || v2.URL != v.URL {
			t.Fatalf("resend %v %s", err, v2.URL)
		}
		v3, err := e.svc.SendRequest(ctx(), e.Owner, e.MarinaID, v.Request.ID, v2.Request.Version, true)
		if err != nil || v3.URL == v.URL {
			t.Fatalf("rotate %v", err)
		}
		if _, err := e.svc.GetPayPage(ctx(), "marina-test", tok); !errors.Is(err, ErrLinkInvalid) {
			t.Fatalf("old link: %v", err)
		}
		if n := e.count(t, "message_log", "marina_id = ? AND template = ?", e.MarinaID, "payment_request"); n != 3 {
			t.Fatalf("payment request emails %d", n)
		}
		e.invariants(t)
	})
}

func TestPayLink_CrossMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.AddMarina(t, "altra-marina")
		cust := e.customer(t, "Verdi")
		ch := e.charge(t, cust, 50_00, e.today().AddDays(5))
		v, err := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch.ID}})
		if err != nil {
			t.Fatal(err)
		}
		tok := tokenOf(v.URL)
		for _, c := range []struct{ slug, token string }{{"altra-marina", tok}, {"marina-test", flip(tok)}, {"marina-test", "short"}} {
			if _, err := e.svc.GetPayPage(ctx(), c.slug, c.token); !errors.Is(err, ErrLinkInvalid) {
				t.Fatalf("%s/%s: %v", c.slug, c.token, err)
			}
		}
		if _, err := e.svc.CancelRequest(ctx(), e.Owner, e.MarinaID, v.Request.ID, v.Request.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.GetPayPage(ctx(), "marina-test", tok); !errors.Is(err, ErrLinkInvalid) {
			t.Fatalf("cancelled: %v", err)
		}
	})
}

// transitLink creates a held transit and its payment link.
func (e *env) transitLink(t testing.TB) (*contract.Detail, string) {
	t.Helper()
	e.enableFake(t)
	tr := e.Transit(t, e.Berths[0], "2027-06-10", "2027-06-13", false)
	v, err := e.svc.CreateTransitLink(ctx(), e.Owner, e.MarinaID, tr.Row.ID, tr.Row.Version, TransitLinkInput{ExpiresInHours: 24, Send: false})
	if err != nil {
		t.Fatalf("transit link: %v", err)
	}
	c, _ := store.GetContract(ctx(), e.DB.DB, e.scope, tr.Row.ID)
	if c.Status != string(contract.StatusPendingPayment) {
		t.Fatalf("status %s", c.Status)
	}
	return tr, tokenOf(v.URL)
}

// TestCheckout_BrowserReturnDoesNotConfirm and TestPaymentWebhook_ConfirmsTransit (INV-09).
func TestPaymentWebhook_ConfirmsTransit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		tr, tok := e.transitLink(t)
		url, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card")
		if err != nil || !strings.HasPrefix(url, "https://pay.fake.test/") {
			t.Fatalf("checkout %s %v", url, err)
		}
		// A second click reuses the open session.
		if again, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card"); err != nil || again != url {
			t.Fatalf("reuse %s %v", again, err)
		}
		// The browser return only polls: verifying, transit still pending.
		st, verifying, err := e.svc.PayStatus(ctx(), "marina-test", tok)
		if err != nil || st != store.RequestOpen || !verifying {
			t.Fatalf("status %s %v %v", st, verifying, err)
		}
		c, _ := store.GetContract(ctx(), e.DB.DB, e.scope, tr.Row.ID)
		if c.Status != string(contract.StatusPendingPayment) {
			t.Fatalf("confirmed by the browser: %s", c.Status)
		}
		req := e.fake.LastCheckout()
		ev := ext.PaymentEvent{
			EventID: "evt_1", Kind: ext.PaymentEventSucceeded, SessionRef: "cs_0001", PaymentRef: "pi_paid_1",
			AmountCents: req.AmountCents, Currency: "EUR", FeeCents: 35, Method: "card_online",
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}
		if err := e.webhook(t, ev); err != nil {
			t.Fatalf("webhook: %v", err)
		}
		c, _ = store.GetContract(ctx(), e.DB.DB, e.scope, tr.Row.ID)
		if c.Status != string(contract.StatusConfirmed) {
			t.Fatalf("not confirmed: %s", c.Status)
		}
		ch, err := store.LivePrimaryCharge(ctx(), e.DB.DB, e.scope, store.ChargeOriginTransit, tr.Row.ID)
		if err != nil || ch.PaymentStatus != store.ChargePaid {
			t.Fatalf("transit charge %+v %v", ch, err)
		}
		p, err := store.PaymentByProviderRef(ctx(), e.DB.DB, e.scope, fixture.FakeProviderCode, "pi_paid_1")
		if err != nil || p.Source != store.PaymentSourceProvider || p.FeeCents != 35 || p.AllocatedCents != p.AmountCents {
			t.Fatalf("payment %+v %v", p, err)
		}
		if st, _, _ := e.svc.PayStatus(ctx(), "marina-test", tok); st != store.RequestPaid {
			t.Fatalf("request %s", st)
		}
		if e.AuditCount(t, "intent.apply") == 0 {
			t.Fatal("no audit")
		}
		e.invariants(t)
	})
}

func TestPaymentWebhook_Deduplicated(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		_, tok := e.transitLink(t)
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		ev := ext.PaymentEvent{
			EventID: "evt_dup", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_dup", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}
		for range 3 {
			if err := e.webhook(t, ev); err != nil {
				t.Fatal(err)
			}
		}
		// A different event of the same payment (payment_intent.succeeded after session.completed) is a no-op too.
		ev.EventID = "evt_dup_2"
		if err := e.webhook(t, ev); err != nil {
			t.Fatal(err)
		}
		if n := e.count(t, "payments", "marina_id = ? AND source = 'provider'", e.MarinaID); n != 1 {
			t.Fatalf("payments %d", n)
		}
		if n := e.count(t, "payment_provider_events", "marina_id = ?", e.MarinaID); n != 2 {
			t.Fatalf("events %d", n)
		}
		e.invariants(t)
	})
}

func TestConcurrency_WebhookDuplicate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		_, tok := e.transitLink(t)
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		ev := &ext.PaymentEvent{
			EventID: "evt_par", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_par", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cp := *ev
				if err := e.svc.ApplyProviderEvent(ctx(), fixture.FakeProviderCode, e.MarinaID, &cp); err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("delivery: %v", err)
		}
		if n := e.count(t, "payments", "marina_id = ? AND intent_id = ?", e.MarinaID, req.IntentID); n != 1 {
			t.Fatalf("payments %d", n)
		}
		e.invariants(t)
	})
}

func TestPaymentWebhook_OutOfOrder(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Neri")
		e.enableFake(t)
		ch := e.charge(t, cust, 80_00, e.today().AddDays(3))
		v, err := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tokenOf(v.URL), "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		meta := map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID}
		// expired then succeeded: money arrived, the payment is applied.
		if err := e.webhook(t, ext.PaymentEvent{EventID: "e1", Kind: ext.PaymentEventExpired, Metadata: meta}); err != nil {
			t.Fatal(err)
		}
		if err := e.webhook(t, ext.PaymentEvent{EventID: "e2", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_late", AmountCents: 80_00, Metadata: meta}); err != nil {
			t.Fatal(err)
		}
		if e.reload(t, ch.ID).PaymentStatus != store.ChargePaid {
			t.Fatal("succeeded after expired not applied")
		}
		// expired after succeeded changes nothing.
		if err := e.webhook(t, ext.PaymentEvent{EventID: "e3", Kind: ext.PaymentEventExpired, Metadata: meta}); err != nil {
			t.Fatal(err)
		}
		in, _ := store.GetPaymentIntent(ctx(), e.DB.DB, e.scope, req.IntentID)
		if in.Status != store.IntentSucceeded {
			t.Fatalf("intent %s", in.Status)
		}
		e.invariants(t)
	})
}

func TestPaymentWebhook_LatePayment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		tr, tok := e.transitLink(t)
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tok, "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		cur, _ := store.GetContract(ctx(), e.DB.DB, e.scope, tr.Row.ID)
		if _, err := e.Contracts.CancelTransit(ctx(), e.Owner, e.MarinaID, tr.Row.ID, cur.Version, "test"); err != nil {
			t.Fatalf("cancel transit: %v", err)
		}
		if err := e.webhook(t, ext.PaymentEvent{
			EventID: "late", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_late2", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}); err != nil {
			t.Fatal(err)
		}
		p, err := store.PaymentByProviderRef(ctx(), e.DB.DB, e.scope, fixture.FakeProviderCode, "pi_late2")
		if err != nil || p.AllocatedCents != 0 || p.UnallocatedCents() != req.AmountCents {
			t.Fatalf("late payment %+v %v", p, err)
		}
		if e.count(t, "message_log", "marina_id = ? AND template = ?", e.MarinaID, "late_payment_office") != 1 {
			t.Fatal("office email not queued")
		}
	})
}

func TestReconcile_FixesMissedWebhook(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Gialli")
		e.enableFake(t)
		ch := e.charge(t, cust, 40_00, e.today().AddDays(3))
		v, _ := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch.ID}})
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tokenOf(v.URL), "card"); err != nil {
			t.Fatal(err)
		}
		in, _ := store.GetPaymentIntent(ctx(), e.DB.DB, e.scope, e.fake.LastCheckout().IntentID)
		e.fake.StatusOf[in.ProviderSessionRef] = &ext.PaymentEvent{Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_rec", AmountCents: 40_00}
		e.Clock.Advance(11 * time.Minute)
		for range 2 {
			if _, err := e.svc.ReconcileMarina(ctx(), e.MarinaID); err != nil {
				t.Fatal(err)
			}
		}
		if e.reload(t, ch.ID).PaymentStatus != store.ChargePaid || e.count(t, "payments", "provider_ref = ?", "pi_rec") != 1 {
			t.Fatal("reconcile did not apply once")
		}
		e.invariants(t)
	})
}

func TestRefund_ViaProviderPendingThenSucceeded(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Rosa")
		e.enableFake(t)
		ch := e.charge(t, cust, 60_00, e.today().AddDays(3))
		v, _ := e.svc.CreateRequest(ctx(), e.Owner, e.MarinaID, CreateRequestInput{CustomerID: cust, ChargeIDs: []string{ch.ID}})
		if _, err := e.svc.StartPayCheckout(ctx(), "marina-test", tokenOf(v.URL), "card"); err != nil {
			t.Fatal(err)
		}
		req := e.fake.LastCheckout()
		if err := e.webhook(t, ext.PaymentEvent{
			EventID: "p", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_r", AmountCents: 70_00,
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": req.IntentID},
		}); err != nil {
			t.Fatal(err)
		}
		p, _ := store.PaymentByProviderRef(ctx(), e.DB.DB, e.scope, fixture.FakeProviderCode, "pi_r")
		if p.UnallocatedCents() != 10_00 {
			t.Fatalf("overpayment credit %d", p.UnallocatedCents())
		}
		r, err := e.payments.Refund(ctx(), e.Owner, e.MarinaID, p.ID, payment.RefundInput{Amount: 10_00, ViaProvider: true, Reason: "eccedenza"})
		if err != nil {
			t.Fatal(err)
		}
		cur, _ := store.GetRefund(ctx(), e.DB.DB, e.scope, r.ID)
		if cur.Status != store.RefundPending || len(e.fake.Refunds) != 1 {
			t.Fatalf("refund %s calls %d", cur.Status, len(e.fake.Refunds))
		}
		if err := e.webhook(t, ext.PaymentEvent{EventID: "rf", Kind: ext.PaymentEventRefundSucceeded, PaymentRef: "pi_r", RefundRef: "re_" + r.ID, AmountCents: 10_00}); err != nil {
			t.Fatal(err)
		}
		cur, _ = store.GetRefund(ctx(), e.DB.DB, e.scope, r.ID)
		if cur.Status != store.RefundSucceeded {
			t.Fatalf("refund %s", cur.Status)
		}
		e.invariants(t)
	})
}

func TestSEPA_MandateAndCollect(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.enableFake(t)
		cust := e.customer(t, "Sepa")
		m, err := e.svc.RequestMandate(ctx(), e.Owner, e.MarinaID, cust, "", true)
		if err != nil || m.Mandate.Status != store.MandatePending || m.SetupURL == "" {
			t.Fatalf("mandate %+v %v", m, err)
		}
		if err := e.webhook(t, ext.PaymentEvent{
			EventID: "seti", Kind: ext.PaymentEventMandateActive, CustomerRef: "cus_1", PaymentMethodRef: "pm_1", MandateRef: "md_1", IBANLast4: "3000",
			Metadata: map[string]string{"marina_id": e.MarinaID, "intent_id": e.fake.Setups[0].IntentID},
		}); err != nil {
			t.Fatal(err)
		}
		cur, _ := store.GetSEPAMandate(ctx(), e.DB.DB, e.scope, m.Mandate.ID)
		if cur.Status != store.MandateActive || cur.IBANLast4 != "3000" || strings.Contains(cur.PaymentMethodRefEnc, "pm_1") {
			t.Fatalf("active mandate %+v", cur)
		}
		// Revocation from the provider.
		if err := e.webhook(t, ext.PaymentEvent{EventID: "mrev", Kind: ext.PaymentEventMandateRevoked, MandateRef: "md_1"}); err != nil {
			t.Fatal(err)
		}
		cur, _ = store.GetSEPAMandate(ctx(), e.DB.DB, e.scope, m.Mandate.ID)
		if cur.Status != store.MandateRevoked {
			t.Fatalf("revoked %s", cur.Status)
		}
	})
}

func TestProviderSettings_SecretsWriteOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		s, err := e.svc.GetProviderSettings(ctx(), e.Owner, e.MarinaID, fixture.FakeProviderCode)
		if err != nil || s.Row.Enabled || s.Row.Version != 1 || s.Row.ID != "" {
			t.Fatalf("disabled by default %+v %v", s, err)
		}
		e.enableFake(t)
		s, _ = e.svc.GetProviderSettings(ctx(), e.Owner, e.MarinaID, fixture.FakeProviderCode)
		if !s.HasSecretKey || !s.HasWebhookSecret || s.Row.PublishableKeyLast4 != "ABCD" ||
			strings.Contains(s.Row.CredentialsEnc, "sk_test_fake") || strings.Contains(s.Row.WebhookSecretEnc, webhookSecret) {
			t.Fatalf("settings %+v", s)
		}
		if ok, code, err := e.svc.TestProvider(ctx(), e.Owner, e.MarinaID, fixture.FakeProviderCode); err != nil || !ok {
			t.Fatalf("test %v %s %v", ok, code, err)
		}
		if _, err := e.svc.GetProviderSettings(ctx(), e.Owner, e.MarinaID, "manual"); !errors.Is(err, ErrProviderUnknown) {
			t.Fatalf("manual: %v", err)
		}
		list, err := e.svc.ListProviders(ctx(), e.Owner, e.MarinaID)
		if err != nil || len(list) != 2 {
			t.Fatalf("providers %+v %v", list, err)
		}
	})
}

func TestReminders_LevelsOncePerCharge(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		cust := e.customer(t, "Lenti")
		ch := e.charge(t, cust, 100_00, e.today().AddDays(5))
		other := e.charge(t, cust, 50_00, e.today().AddDays(5))
		run, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders"))
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); again.ID != run.ID {
			t.Fatal("second run of the day")
		}
		if n := e.count(t, "reminder_items", "charge_id = ? AND level = ?", ch.ID, levels.PreDue); n != 1 {
			t.Fatalf("pre_due items %d (run %s counts %s, today %s, due %s)", n, run.Status, run.Counts, e.today(), ch.DueOn)
		}
		// One email per customer and level for both charges.
		if n := e.count(t, "message_log", "marina_id = ? AND template = ?", e.MarinaID, "reminder_pre_due"); n != 1 {
			t.Fatalf("emails %d", n)
		}
		// Days later: due, then courteous, formal; final notice waits for approval.
		for _, d := range []int{5, 12, 26} {
			e.Clock.Set(dayAt(e, d))
			if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
				t.Fatal(err)
			}
		}
		e.Clock.Set(dayAt(e, 50))
		if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
			t.Fatal(err)
		}
		var items []store.ReminderItem
		if err := e.DB.NewSelect().Model(&items).Where("charge_id = ?", ch.ID).OrderExpr("id").Scan(ctx()); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, it := range items {
			got = append(got, it.Level)
		}
		if strings.Join(got, ",") != "pre_due,due,courteous,formal,final_notice" || items[4].Status != ItemAwaitingApproval {
			t.Fatalf("levels %v (%s)", got, items[len(items)-1].Status)
		}
		// Approve the final notice of one charge; dismiss the other.
		it, err := e.svc.ApproveItem(ctx(), e.Owner, e.MarinaID, items[4].ID)
		if err != nil || it.Item.Status != ItemSent {
			t.Fatalf("approve %+v %v", it, err)
		}
		if _, err := e.svc.ApproveItem(ctx(), e.Owner, e.MarinaID, items[4].ID); !errors.Is(err, ErrItemDecided) {
			t.Fatalf("approve twice: %v", err)
		}
		var o store.ReminderItem
		_ = e.DB.NewSelect().Model(&o).Where("charge_id = ? AND level = ?", other.ID, levels.FinalNotice).Scan(ctx())
		if d, err := e.svc.DismissItem(ctx(), e.Owner, e.MarinaID, o.ID); err != nil || d.Item.Status != ItemDismissed {
			t.Fatalf("dismiss %v", err)
		}
		e.invariants(t)
	})
}

// time_ is the env start shifted by days (10:00 Rome).
func dayAt(_ *env, days int) time.Time {
	return time.Date(2027, 6, 2, 8, 0, 0, 0, time.UTC).AddDate(0, 0, days)
}

func TestReminders_StopWhenPaidAndExclusions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		paid := e.customer(t, "Pagato")
		excl := e.customer(t, "Escluso")
		ch1 := e.charge(t, paid, 30_00, e.today())
		ch2 := e.charge(t, excl, 30_00, e.today())
		ch3 := e.charge(t, excl, 20_00, e.today())
		if _, err := e.payments.Record(ctx(), e.Owner, e.MarinaID, payment.RecordInput{
			CustomerID: paid, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: 30_00,
			Matches: []payment.MatchInput{{ChargeID: ch1.ID, Amount: 30_00}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.svc.CreateExclusion(ctx(), e.Owner, e.MarinaID, ExclusionInput{ChargeID: ch2.ID, Kind: "disputed"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
			t.Fatal(err)
		}
		if n := e.count(t, "reminder_items", "charge_id = ?", ch1.ID); n != 0 {
			t.Fatalf("paid charge reminded %d", n)
		}
		if n := e.count(t, "reminder_items", "charge_id = ? AND status = ?", ch2.ID, ItemSkippedExcluded); n != 1 {
			t.Fatalf("excluded %d", n)
		}
		if n := e.count(t, "reminder_items", "charge_id = ? AND status = ?", ch3.ID, ItemSent); n != 1 {
			t.Fatalf("sent %d", n)
		}
		e.invariants(t)
	})
}

func TestConcurrency_ReminderRunOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		for i := range 3 {
			c := e.customer(t, "Par"+string(rune('A'+i)))
			e.charge(t, c, 10_00, e.today())
		}
		var wg sync.WaitGroup
		ids := make(chan string, 100)
		errs := make(chan error, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders"))
				if err != nil {
					errs <- err
					return
				}
				ids <- r.ID
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for id := range ids {
			seen[id] = true
		}
		if len(seen) != 1 || e.count(t, "reminder_runs", "marina_id = ?", e.MarinaID) != 1 {
			t.Fatalf("runs %d", len(seen))
		}
		if n := e.count(t, "message_log", "marina_id = ? AND template = ?", e.MarinaID, "reminder_due"); n != 3 {
			t.Fatalf("emails %d", n)
		}
		e.invariants(t)
	})
}

func TestReminders_ManualSend(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c := e.customer(t, "Manuale")
		ch := e.charge(t, c, 10_00, e.today().AddDays(30))
		res, err := e.svc.SendReminders(ctx(), e.Owner, e.MarinaID, []string{ch.ID}, "")
		if err != nil || res.Queued != 1 {
			t.Fatalf("manual %+v %v", res, err)
		}
		res, err = e.svc.SendReminders(ctx(), e.Owner, e.MarinaID, []string{ch.ID}, levels.PreDue)
		if err != nil || res.Queued != 1 {
			t.Fatalf("second manual (next level) %+v %v", res, err)
		}
		if n := e.count(t, "reminder_items", "charge_id = ?", ch.ID); n != 2 {
			t.Fatalf("items %d", n)
		}
	})
}

func TestArrears_SuspendsContract(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		// A transit with an overdue charge is never suspended; the policy is disabled by default.
		tr := e.Transit(t, e.Berths[1], "2027-06-03", "2027-06-05", true)
		e.Clock.Set(dayAt(e, 90))
		if _, err := e.svc.RunDayTx(ctx(), e.scope, jobActor("payments.reminders")); err != nil {
			t.Fatal(err)
		}
		c, _ := store.GetContract(ctx(), e.DB.DB, e.scope, tr.Row.ID)
		if c.Status == string(contract.StatusSuspended) {
			t.Fatal("transit suspended")
		}
	})
}

func flip(tok string) string {
	last := tok[len(tok)-1]
	if last == '2' {
		return tok[:len(tok)-1] + "3"
	}
	return tok[:len(tok)-1] + "2"
}
