//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

const m5WebhookSecret = "whsec_acceptance"

// newM5World is the desk world with extensions (the fake payment provider of the fixture).
func newM5World(t *testing.T, db *store.DB, x ext.Extensions) *deskWorld {
	t.Helper()
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true", "PORTO_LOG_LEVEL": "debug",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	mail := &fixture.Mailbox{}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", x, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), app.Options{
		DB: db, Clock: c, Argon2: auth.TestArgon2Params, Transport: mail,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	h := &harness{t: t, app: a, srv: srv, logs: logs, mail: mail}
	own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Online SRL", MarinaName: "Marina Online", MarinaSlug: "online",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &deskWorld{h: h, clock: c, marina: own.MarinaID, M: A + "/marinas/" + own.MarinaID, owner: h.cookieClient(), clients: map[string]*client{}}
	w.owner.login(t, "owner@porto.test")
	w.harbour = contracttest.InsertHarbour(t, db, own.MarinaID, store.UTC(c.Now()), 3)
	w.prices(t, false)
	w.contractSettings(t, map[string]any{"bank_details": map[string]any{"iban": "IT60X0542811101000000123456", "holder": "Marina Online Srl"}})
	return w
}

// postWebhook delivers a signed fake provider event over HTTP.
func (w *deskWorld) postWebhook(t *testing.T, ev ext.PaymentEvent, at time.Time) int {
	t.Helper()
	body, sig := fixture.SignedBody(ev, m5WebhookSecret, at)
	req, _ := http.NewRequest(http.MethodPost, w.h.srv.URL+"/webhooks/payments/"+fixture.FakeProviderCode+"/"+w.marina, bytes.NewReader(body))
	req.Header.Set(fixture.FakeSignatureHeader, sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestAcceptance_M5_OnlinePayments (m5-plan §11.4, fake provider with real signatures): payment link on a held
// transit → pending_payment → browser return (no confirmation) → signed webhook → confirmed and paid; duplicate,
// replayed and badly signed webhooks; a payment request paid online; reconciliation of a missed webhook; then the
// provider disabled ⇒ the public page shows the bank details only.
func TestAcceptance_M5_OnlinePayments(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fake := fixture.NewFakeProvider()
		w := newM5World(t, db, ext.Extensions{PaymentProviders: []ext.PaymentProvider{fake}})
		o, M := w.owner, w.M
		list := o.expect(t, 200, "GET", M+"/payment-providers", nil)
		codes := map[string]bool{}
		for _, it := range jsonList(list, "items") {
			codes[str(it.(map[string]any), "code")] = true
		}
		if !codes["manual"] || !codes[fixture.FakeProviderCode] { // stripe too once app.go registers it
			t.Fatalf("providers %v", list)
		}
		set := o.expect(t, 200, "GET", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", nil)
		if set["enabled"] != false || set["has_secret_key"] != false {
			t.Fatalf("disabled by default %v", set)
		}
		set = o.expect(t, 200, "PUT", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", map[string]any{
			"enabled": true, "mode": "test", "methods": []string{"card"}, "secret_key": "sk_test_x", "webhook_secret": m5WebhookSecret,
			"sepa_enabled": false,
		}, "If-Match", etagOf(set["version"]))
		if bytes.Contains(mustJSON(t, set), []byte("sk_test_x")) || bytes.Contains(mustJSON(t, set), []byte(m5WebhookSecret)) || set["has_secret_key"] != true {
			t.Fatalf("secrets leaked %v", set)
		}
		rossi := w.mailCustomer(t, "Rossi", "rossi@example.test", "it")
		vessel := w.vessel(t, "Libeccio", 1134, 380)
		tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": vessel, "customer_id": rossi, "arrival": "2027-06-05", "departure": "2027-06-08",
			"placements": []any{berthPlacement(w.harbour.Berths[0], "2027-06-05", "2027-06-07")},
		})
		link := o.expect(t, 200, "POST", M+"/transits/"+str(tr, "id")+"/payment-link", map[string]any{"expires_in_hours": 24, "send": true}, "If-Match", etagOf(tr["version"]))
		url := str(link, "url")
		tok := url[strings.LastIndex(url, "/")+1:]
		pub := w.h.cookieClient()
		page := pub.expect(t, 200, "GET", "/api/public/v1/online/pay/"+tok, nil)
		if page["online"] != true || page["bank"] == nil {
			t.Fatalf("pay page %v", page)
		}
		co := pub.expect(t, 200, "POST", "/api/public/v1/online/pay/"+tok+"/checkout", map[string]any{"method": "card"})
		if !strings.HasPrefix(str(co, "redirect_url"), "https://pay.fake.test/") {
			t.Fatalf("checkout %v", co)
		}
		// The browser comes back: still verifying, the transit is not confirmed (INV-09).
		st := pub.expect(t, 200, "GET", "/api/public/v1/online/pay/"+tok+"/status", nil)
		if st["verifying"] != true {
			t.Fatalf("status %v", st)
		}
		if cur := o.expect(t, 200, "GET", M+"/contracts/"+str(tr, "id"), nil); str(cur, "status") != "pending_payment" {
			t.Fatalf("confirmed by the browser: %v", str(cur, "status"))
		}
		req := fake.LastCheckout()
		ev := ext.PaymentEvent{
			EventID: "evt_tr", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_tr", AmountCents: req.AmountCents, Method: "card_online",
			Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
		}
		if code := w.postWebhook(t, ev, w.clock.Now().Add(-10*time.Minute)); code != 401 {
			t.Fatalf("replayed signature %d", code)
		}
		for range 2 {
			if code := w.postWebhook(t, ev, w.clock.Now()); code != 204 {
				t.Fatalf("webhook %d", code)
			}
		}
		if cur := o.expect(t, 200, "GET", M+"/contracts/"+str(tr, "id"), nil); str(cur, "status") != "confirmed" {
			t.Fatalf("not confirmed: %v", str(cur, "status"))
		}
		if st := pub.expect(t, 200, "GET", "/api/public/v1/online/pay/"+tok+"/status", nil); str(st, "status") != "paid" {
			t.Fatalf("request %v", st)
		}
		n, _ := db.NewSelect().TableExpr("payments").Where("marina_id = ? AND provider_ref = ?", w.marina, "pi_tr").Count(t.Context())
		if n != 1 {
			t.Fatalf("payments %d", n)
		}

		// A payment request of a manual charge paid online; the webhook is missed and reconciliation applies it.
		items := o.expect(t, 200, "GET", M+"/items", nil)
		item := str(jsonList(items, "items")[0].(map[string]any), "id")
		ch := o.expect(t, 201, "POST", M+"/charges", map[string]any{
			"origin_type": "manual", "customer_id": rossi, "lines": []map[string]any{{"item_id": item, "qty_milli": 1000, "unit_cents": 90_00}},
		})
		pr := o.expect(t, 201, "POST", M+"/payment-requests", map[string]any{"customer_id": rossi, "charge_ids": []string{str(ch, "id")}, "send": true})
		tok2 := str(pr, "url")[strings.LastIndex(str(pr, "url"), "/")+1:]
		pub.expect(t, 200, "POST", "/api/public/v1/online/pay/"+tok2+"/checkout", map[string]any{"method": "card"})
		in := fake.LastCheckout()
		scope, _ := store.SystemMarinaScope(t.Context(), db.DB, w.marina)
		intent, err := store.GetPaymentIntent(t.Context(), db.DB, scope, in.IntentID)
		if err != nil {
			t.Fatal(err)
		}
		fake.StatusOf[intent.ProviderSessionRef] = &ext.PaymentEvent{Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_missed", AmountCents: in.AmountCents}
		w.clock.Advance(11 * time.Minute)
		w.h.runJobs("payments.reconcile")
		if got := o.expect(t, 200, "GET", M+"/charges/"+str(ch, "id"), nil); str(got, "payment_status") != "paid" {
			t.Fatalf("reconciled charge %v", str(got, "payment_status"))
		}

		// Provider disabled: the public page shows the bank details only.
		cur := o.expect(t, 200, "GET", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", nil)
		o.expect(t, 200, "PUT", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", map[string]any{
			"enabled": false, "mode": "test", "methods": []string{"card"}, "sepa_enabled": false,
		}, "If-Match", etagOf(cur["version"]))
		ch3 := o.expect(t, 201, "POST", M+"/charges", map[string]any{
			"origin_type": "manual", "customer_id": rossi, "lines": []map[string]any{{"item_id": item, "qty_milli": 1000, "unit_cents": 10_00}},
		})
		pr3 := o.expect(t, 201, "POST", M+"/payment-requests", map[string]any{"customer_id": rossi, "charge_ids": []string{str(ch3, "id")}})
		tok3 := str(pr3, "url")[strings.LastIndex(str(pr3, "url"), "/")+1:]
		if p := pub.expect(t, 200, "GET", "/api/public/v1/online/pay/"+tok3, nil); p["online"] != false || p["bank"] == nil {
			t.Fatalf("offline page %v", p)
		}
		if vs, err := store.CheckBillingInvariants(t.Context(), db.DB, scope); err != nil || len(vs) != 0 {
			t.Fatalf("invariants %+v %v", vs, err)
		}
	})
}
