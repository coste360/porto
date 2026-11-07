//go:build integration

package webhooks

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: testdb hands the database to the test
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func enable(t *testing.T, svc *collect.Service, fe *fixture.Env, marinaID, secret string) {
	t.Helper()
	sk, ws := "sk_test", secret
	if _, err := svc.UpdateProviderSettings(fixture.Ctx(), fe.Owner, marinaID, fixture.FakeProviderCode, 1, collect.ProviderSettingsInput{
		Enabled: true, Mode: "test", Methods: []string{"card"}, SecretKey: &sk, WebhookSecret: &ws,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPaymentWebhook_CrossTenant: an event signed for marina A posted to marina B is refused (B without the
// provider: 404; B with its own secret: 401) and nothing is applied; unknown providers are 404; an ignored event
// is acknowledged and recorded once.
func TestPaymentWebhook_CrossTenant(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fe := fixture.New(t, db)
		svc := collect.NewService(collect.Deps{
			DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Keys: fe.Keys,
			Drivers: paydriver.NewRegistry(fixture.NewFakeProvider()), Notify: fe.Notify, BaseURL: "https://porto.test",
		})
		h := &Handler{Collect: svc, Clock: fe.Clock}
		b := fe.AddMarina(t, "marina-b")
		enable(t, svc, fe, fe.MarinaID, "whsec_a")
		post := func(marinaID, provider string, ev ext.PaymentEvent, secret string) *httptest.ResponseRecorder {
			body, sig := fixture.SignedBody(ev, secret, fe.Clock.Now())
			r := httptest.NewRequest(http.MethodPost, "/webhooks/payments/"+provider+"/"+marinaID, bytes.NewReader(body))
			r.Header.Set(fixture.FakeSignatureHeader, sig)
			w := httptest.NewRecorder()
			h.PaymentWebhook(w, r, provider, marinaID)
			return w
		}
		ev := ext.PaymentEvent{
			EventID: "evt_x", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_x", AmountCents: 100,
			Metadata: map[string]string{"marina_id": fe.MarinaID},
		}
		if w := post(b, fixture.FakeProviderCode, ev, "whsec_a"); w.Code != http.StatusNotFound {
			t.Fatalf("B without provider: %d", w.Code)
		}
		enable(t, svc, fe, b, "whsec_b")
		if w := post(b, fixture.FakeProviderCode, ev, "whsec_a"); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_signature") {
			t.Fatalf("B with own secret: %d %s", w.Code, w.Body.String())
		}
		// A provider configured without a webhook secret cannot verify anything: 401 (m5-plan §5.4).
		c := fe.AddMarina(t, "marina-c")
		sk := "sk_test"
		if _, err := svc.UpdateProviderSettings(fixture.Ctx(), fe.Owner, c, fixture.FakeProviderCode, 1, collect.ProviderSettingsInput{
			Enabled: false, Mode: "test", Methods: []string{"card"}, SecretKey: &sk,
		}); err != nil {
			t.Fatal(err)
		}
		if w := post(c, fixture.FakeProviderCode, ev, "whsec_a"); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_signature") {
			t.Fatalf("provider without webhook secret: %d %s", w.Code, w.Body.String())
		}
		if w := post(fe.MarinaID, "stripe", ev, "whsec_a"); w.Code != http.StatusNotFound {
			t.Fatalf("unknown provider: %d", w.Code)
		}
		// Event of A for an intent A does not know: plausibly ours without an intent id → ignored (204).
		if w := post(fe.MarinaID, fixture.FakeProviderCode, ev, "whsec_a"); w.Code != http.StatusNoContent {
			t.Fatalf("unknown intent: %d %s", w.Code, w.Body.String())
		}
		ig := ext.PaymentEvent{EventID: "evt_ignored"}
		for range 2 {
			if w := post(fe.MarinaID, fixture.FakeProviderCode, ig, "whsec_a"); w.Code != http.StatusNoContent {
				t.Fatalf("ignored: %d", w.Code)
			}
		}
		n, _ := db.NewSelect().TableExpr("payment_provider_events").Where("event_id = ?", "evt_ignored").Count(context.Background())
		m, _ := db.NewSelect().TableExpr("payments").Count(context.Background())
		if n != 1 || m != 0 {
			t.Fatalf("ignored rows %d, payments %d", n, m)
		}
	})
}
