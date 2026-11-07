package stripe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

const testSecret = "whsec_test_secret"

var now = time.Date(2027, 6, 1, 10, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "stripe", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func webhookRequest(body []byte, header string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/payments/stripe/m", bytes.NewReader(body))
	r.Header.Set(SignatureHeader, header)
	return r
}

func TestStripe_SignatureValid(t *testing.T) {
	body := fixture(t, "checkout_session_completed_paid.json")
	ev, err := New(nil, "").ParseWebhook(webhookRequest(body, Sign(body, now, testSecret)), []byte(testSecret), now)
	if err != nil || ev.Kind != ext.PaymentEventSucceeded || ev.EventID != "evt_t_session_paid" {
		t.Fatalf("ev %+v err %v", ev, err)
	}
}

func TestStripe_SignatureInvalid(t *testing.T) {
	body := fixture(t, "checkout_session_completed_paid.json")
	d := New(nil, "")
	cases := map[string]string{
		"wrong secret": Sign(body, now, "whsec_other"),
		"empty":        "",
		"garbage":      "t=abc,v1=zz",
		"no v1":        "t=" + strings.Split(Sign(body, now, testSecret), ",")[0][2:],
	}
	for name, h := range cases {
		if _, err := d.ParseWebhook(webhookRequest(body, h), []byte(testSecret), now); !errors.Is(err, ext.ErrInvalidSignature) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	tampered := bytes.Replace(body, []byte("12345"), []byte("99999"), 1)
	if _, err := d.ParseWebhook(webhookRequest(tampered, Sign(body, now, testSecret)), []byte(testSecret), now); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("tampered: %v", err)
	}
	if _, err := d.ParseWebhook(webhookRequest(body, Sign(body, now, testSecret)), nil, now); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("no secret: %v", err)
	}
}

func TestStripe_SignatureReplay(t *testing.T) {
	body := fixture(t, "checkout_session_completed_paid.json")
	h := Sign(body, now.Add(-6*time.Minute), testSecret)
	if _, err := New(nil, "").ParseWebhook(webhookRequest(body, h), []byte(testSecret), now); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("replay accepted: %v", err)
	}
	h = Sign(body, now.Add(-4*time.Minute), testSecret)
	if _, err := New(nil, "").ParseWebhook(webhookRequest(body, h), []byte(testSecret), now); err != nil {
		t.Fatalf("within tolerance: %v", err)
	}
}

func TestStripe_SignatureRotated(t *testing.T) {
	body := fixture(t, "payment_intent_succeeded.json")
	h := Sign(body, now, "whsec_old", testSecret)
	if _, err := New(nil, "").ParseWebhook(webhookRequest(body, h), []byte(testSecret), now); err != nil {
		t.Fatalf("rotated: %v", err)
	}
}

func TestStripe_SignatureFutureTimestamp(t *testing.T) {
	body := fixture(t, "payment_intent_succeeded.json")
	h := Sign(body, now.Add(6*time.Minute), testSecret)
	if _, err := New(nil, "").ParseWebhook(webhookRequest(body, h), []byte(testSecret), now); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("future accepted: %v", err)
	}
}

func TestStripe_EventMapping(t *testing.T) {
	cases := []struct {
		file                          string
		kind                          string
		session, payment, refund      string
		amount, fee                   int64
		method, failure, mandate, pm4 string
		ignored                       bool
	}{
		{file: "checkout_session_completed_paid.json", kind: ext.PaymentEventSucceeded, session: "cs_test_paid", payment: "pi_test_paid", amount: 12345},
		{file: "checkout_session_completed_unpaid.json", kind: ext.PaymentEventProcessing, session: "cs_test_sepa", payment: "pi_test_sepa", amount: 50000},
		{file: "checkout_session_async_payment_succeeded.json", kind: ext.PaymentEventSucceeded, session: "cs_test_sepa", payment: "pi_test_sepa", amount: 50000},
		{file: "checkout_session_async_payment_failed.json", kind: ext.PaymentEventFailed, session: "cs_test_sepa", payment: "pi_test_sepa", amount: 50000},
		{file: "checkout_session_expired.json", kind: ext.PaymentEventExpired, session: "cs_test_old", amount: 9000},
		{file: "payment_intent_succeeded.json", kind: ext.PaymentEventSucceeded, payment: "pi_test_paid", amount: 12345, fee: 205},
		{file: "payment_intent_processing.json", kind: ext.PaymentEventProcessing, payment: "pi_test_mandate", amount: 30000, method: "sepa_debit"},
		{file: "payment_intent_payment_failed.json", kind: ext.PaymentEventFailed, payment: "pi_test_mandate", amount: 30000, method: "sepa_debit", failure: "insufficient_funds"},
		{file: "charge_refunded.json", kind: ext.PaymentEventRefundSucceeded, payment: "pi_test_paid", refund: "re_test_1", amount: 2000},
		{file: "refund_updated_failed.json", kind: ext.PaymentEventRefundFailed, payment: "pi_test_paid", refund: "re_test_2", amount: 1500, failure: "expired_or_canceled_card"},
		{file: "setup_intent_succeeded.json", kind: ext.PaymentEventMandateActive, session: "seti_test_1", mandate: "mandate_test_1", pm4: "3000"},
		{file: "mandate_updated_inactive.json", kind: ext.PaymentEventMandateRevoked, mandate: "mandate_test_1"},
		{file: "checkout_session_setup_completed.json", ignored: true},
		{file: "charge_dispute_created.json", kind: ext.PaymentEventDisputed, payment: "pi_test_paid", amount: 12345},
		{file: "charge_dispute_no_payment.json", ignored: true},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			ev, err := MapEvent(fixture(t, c.file))
			if c.ignored {
				if !errors.Is(err, ext.ErrIgnoredEvent) || ev == nil || ev.EventID == "" || ev.Kind != "" {
					t.Fatalf("ignored: %+v %v", ev, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ev.Kind != c.kind || ev.SessionRef != c.session || ev.PaymentRef != c.payment || ev.RefundRef != c.refund ||
				ev.AmountCents != c.amount || ev.FeeCents != c.fee || ev.Method != c.method || ev.FailureCode != c.failure ||
				ev.MandateRef != c.mandate || ev.IBANLast4 != c.pm4 || ev.EventID == "" || ev.OccurredAt.IsZero() {
				t.Fatalf("event %+v", ev)
			}
			for k := range ev.Metadata {
				if k != "marina_id" && k != "intent_id" {
					t.Fatalf("metadata key %s", k)
				}
			}
			if strings.Contains(strings.ToLower(ev.FailureCode), "rossi") {
				t.Fatal("failure code carries free text")
			}
		})
	}
	ev, _ := MapEvent(fixture(t, "setup_intent_succeeded.json"))
	if ev.CustomerRef != "cus_test_1" || ev.PaymentMethodRef != "pm_test_1" || ev.Metadata["intent_id"] == "" {
		t.Fatalf("mandate event %+v", ev)
	}
}

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
	resp func(path string) (int, string)
}

type recorded struct {
	method, path, idem, version, auth string
	form                              url.Values
	query                             url.Values
}

func (rc *recorder) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(raw))
		rc.mu.Lock()
		rc.reqs = append(rc.reqs, recorded{
			method: r.Method, path: r.URL.Path, idem: r.Header.Get("Idempotency-Key"), version: r.Header.Get("Stripe-Version"),
			auth: r.Header.Get("Authorization"), form: f, query: r.URL.Query(),
		})
		rc.mu.Unlock()
		status, body := rc.resp(r.URL.Path)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func creds() context.Context {
	return ext.WithProviderCredentials(context.Background(), map[string]string{CredentialSecretKey: "sk_test_abc"})
}

func TestStripe_CheckoutRequest(t *testing.T) {
	req := ext.CheckoutRequest{
		MarinaID: "m-1", IntentID: "i-1", IdempotencyKey: "payment_request:r-1:1", AmountCents: 12345, Currency: "EUR",
		Description: "A2027-000001 A2027-000002", Methods: []string{"card", "wallet", "sepa_debit"},
		SuccessURL: "https://porto.test/b/x/pay/t/return", CancelURL: "https://porto.test/b/x/pay/t",
		ExpiresAt: time.Unix(1814000000, 0), Locale: "it-IT",
	}
	golden := "cancel_url=https%3A%2F%2Fporto.test%2Fb%2Fx%2Fpay%2Ft&client_reference_id=i-1&expires_at=1814000000" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bcurrency%5D=eur" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bproduct_data%5D%5Bname%5D=A2027-000001+A2027-000002" +
		"&line_items%5B0%5D%5Bprice_data%5D%5Bunit_amount%5D=12345&line_items%5B0%5D%5Bquantity%5D=1&locale=it" +
		"&metadata%5Bintent_id%5D=i-1&metadata%5Bmarina_id%5D=m-1&mode=payment" +
		"&payment_intent_data%5Bmetadata%5D%5Bintent_id%5D=i-1&payment_intent_data%5Bmetadata%5D%5Bmarina_id%5D=m-1" +
		"&payment_method_types%5B0%5D=card&payment_method_types%5B1%5D=sepa_debit" +
		"&success_url=https%3A%2F%2Fporto.test%2Fb%2Fx%2Fpay%2Ft%2Freturn"
	if got := CheckoutForm(req).Encode(); got != golden {
		t.Fatalf("form\n got %s\nwant %s", got, golden)
	}
	rc := &recorder{resp: func(string) (int, string) {
		return 200, `{"id":"cs_1","url":"https://checkout.stripe.test/cs_1","payment_intent":null,"expires_at":1814000000}`
	}}
	srv := rc.server(t)
	s, err := New(srv.Client(), srv.URL).CreateCheckout(creds(), req)
	if err != nil || s.SessionRef != "cs_1" || s.URL == "" || s.ExpiresAt.Unix() != 1814000000 {
		t.Fatalf("session %+v %v", s, err)
	}
	r := rc.reqs[0]
	if r.method != http.MethodPost || r.path != "/v1/checkout/sessions" || r.idem != req.IdempotencyKey ||
		r.version != APIVersion || r.auth != "Bearer sk_test_abc" || r.form.Encode() != golden {
		t.Fatalf("request %+v", r)
	}
	if _, err := New(srv.Client(), srv.URL).CreateCheckout(context.Background(), req); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("no credentials: %v", err)
	}
}

func TestStripe_MandateRefundStatus(t *testing.T) {
	rc := &recorder{resp: func(p string) (int, string) {
		switch {
		case p == "/v1/customers":
			return 200, `{"id":"cus_9"}`
		case p == "/v1/checkout/sessions":
			return 200, `{"id":"cs_setup","url":"https://checkout.stripe.test/cs_setup"}`
		case p == "/v1/payment_intents":
			return 200, `{"id":"pi_9","status":"processing"}`
		case p == "/v1/refunds":
			return 200, `{"id":"re_9","status":"pending"}`
		case strings.HasPrefix(p, "/v1/payment_intents/"):
			return 200, `{"id":"pi_9","status":"succeeded","amount":3000,"amount_received":3000,"currency":"eur",` +
				`"latest_charge":{"id":"ch","balance_transaction":{"fee":35}},"metadata":{"intent_id":"i-2","marina_id":"m-1","other":"x"}}`
		case strings.HasPrefix(p, "/v1/checkout/sessions/"):
			return 200, `{"id":"cs_open","status":"open","payment_status":"unpaid"}`
		case strings.HasPrefix(p, "/v1/refunds/"):
			return 200, `{"id":"re_9","status":"succeeded","amount":100,"currency":"eur","payment_intent":"pi_9"}`
		}
		return 404, `{"error":{"type":"invalid_request_error","code":"resource_missing"}}`
	}}
	srv := rc.server(t)
	d := New(srv.Client(), srv.URL)
	ctx := creds()
	s, err := d.CreateMandateSetup(ctx, ext.MandateSetupRequest{MarinaID: "m-1", IntentID: "i-1", IdempotencyKey: "mandate_setup:c:1"})
	if err != nil || s.SessionRef != "cs_setup" {
		t.Fatalf("setup %+v %v", s, err)
	}
	if rc.reqs[1].form.Get("customer") != "cus_9" || rc.reqs[1].form.Get("mode") != "setup" ||
		rc.reqs[1].form.Get("payment_method_types[0]") != "sepa_debit" || rc.reqs[0].idem != "mandate_setup:c:1:customer" {
		t.Fatalf("setup requests %+v", rc.reqs)
	}
	p, err := d.ChargeMandate(ctx, ext.MandateChargeRequest{MarinaID: "m-1", IntentID: "i-2", AmountCents: 3000, CustomerRef: "cus_9", PaymentMethodRef: "pm_9", IdempotencyKey: "sepa:c:1"})
	if err != nil || p.PaymentRef != "pi_9" || p.Status != ext.PaymentEventProcessing {
		t.Fatalf("charge %+v %v", p, err)
	}
	if f := rc.reqs[2].form; f.Get("off_session") != "true" || f.Get("confirm") != "true" || f.Get("payment_method") != "pm_9" {
		t.Fatalf("charge form %v", f)
	}
	rf, err := d.Refund(ctx, ext.RefundRequest{MarinaID: "m-1", PaymentRef: "pi_9", AmountCents: 100, IdempotencyKey: "refund:r"})
	if err != nil || rf.RefundRef != "re_9" || rf.Status != "pending" {
		t.Fatalf("refund %+v %v", rf, err)
	}
	ev, err := d.Status(ctx, ext.ProviderRef{PaymentRef: "pi_9"})
	if err != nil || ev.Kind != ext.PaymentEventSucceeded || ev.FeeCents != 35 || len(ev.Metadata) != 2 {
		t.Fatalf("status %+v %v", ev, err)
	}
	if ev, err := d.Status(ctx, ext.ProviderRef{SessionRef: "cs_open"}); err != nil || ev.Kind != "" {
		t.Fatalf("open session %+v %v", ev, err)
	}
	if ev, err := d.Status(ctx, ext.ProviderRef{RefundRef: "re_9"}); err != nil || ev.Kind != ext.PaymentEventRefundSucceeded {
		t.Fatalf("refund status %+v %v", ev, err)
	}
	if err := d.TestCredentials(ctx); err == nil {
		t.Fatal("balance should 404 in this fake")
	}
}

func TestStripe_ErrorsCarryNoPII(t *testing.T) {
	body := string(fixture(t, "error_card_declined.json"))
	rc := &recorder{resp: func(string) (int, string) { return 402, body }}
	srv := rc.server(t)
	_, err := New(srv.Client(), srv.URL).CreateCheckout(creds(), ext.CheckoutRequest{IntentID: "i", AmountCents: 100})
	var se *Error
	if !errors.As(err, &se) || se.Code != "card_declined" || se.Type != "card_error" || se.Status != 402 || se.ProviderCode() != "card_declined" {
		t.Fatalf("error %v", err)
	}
	msg := err.Error()
	for _, leak := range []string{"mario", "example.test", "declined for", "customer_email", "sk_test"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaks %q: %s", leak, msg)
		}
	}
	// Transport errors drop the URL.
	_, err = New(&http.Client{Transport: failing{}}, "https://api.stripe.test").Status(creds(), ext.ProviderRef{PaymentRef: "pi_secret?x=1"})
	if err == nil || strings.Contains(err.Error(), "pi_secret") {
		t.Fatalf("transport error %v", err)
	}
}

type failing struct{}

func (failing) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial refused")
}
