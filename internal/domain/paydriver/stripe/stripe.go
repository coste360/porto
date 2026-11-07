package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// APIVersion is the pinned Stripe API version sent with every request (Stripe-Version header).
const APIVersion = "2024-06-20"

// DefaultBaseURL is the production API endpoint. Tests pass an httptest server; PORTO_STRIPE_API_BASE overrides it
// only in dev.
const DefaultBaseURL = "https://api.stripe.com"

// CredentialSecretKey is the key of the secret API key in the per-call credentials (ext.WithProviderCredentials).
const CredentialSecretKey = "secret_key"

// maxResponseBytes bounds a provider response read into memory.
const maxResponseBytes = 1 << 20

// MaxWebhookBytes bounds a webhook body (x-porto-max-body-bytes of the contract).
const MaxWebhookBytes = 256 << 10

// SignatureTolerance is the accepted distance between the signed timestamp and the host clock, both ways.
const SignatureTolerance = 5 * time.Minute

// Doer sends HTTP requests: the egress client (anti-SSRF) in production, a test server client in tests.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Driver is the Stripe payment driver.
type Driver struct {
	base   string
	client Doer
}

// New returns the driver. client must be the egress client of the host; base "" means DefaultBaseURL.
func New(client Doer, base string) *Driver {
	if base == "" {
		base = DefaultBaseURL
	}
	return &Driver{base: strings.TrimRight(base, "/"), client: client}
}

// Code implements ext.PaymentProvider.
func (*Driver) Code() string { return Code }

// Capabilities implements ext.PaymentProvider.
func (*Driver) Capabilities() ext.PaymentCapabilities {
	return ext.PaymentCapabilities{
		Checkout: true, MandateSetup: true, MandateCharge: true, Refund: true, StatusCheck: true,
		Methods: []string{"card", "wallet", "sepa_debit"},
	}
}

// Error is a Stripe API error reduced to its type, code and HTTP status: the message and the parameters are
// dropped because they may echo request data.
type Error struct {
	Status int
	Type   string
	Code   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("stripe: %d %s %s", e.Status, e.Type, e.Code)
}

// ProviderCode is the code reported to the office (details.code of provider_error).
func (e *Error) ProviderCode() string {
	if e.Code != "" {
		return e.Code
	}
	if e.Type != "" {
		return e.Type
	}
	return "http_" + strconv.Itoa(e.Status)
}

// ErrNoCredentials means the context carries no secret key.
var ErrNoCredentials = errors.New("stripe: no credentials")

// CheckoutForm returns the form of a Checkout Session in payment mode (exported for the golden test).
func CheckoutForm(req ext.CheckoutRequest) url.Values {
	f := url.Values{}
	f.Set("mode", "payment")
	f.Set("client_reference_id", req.IntentID)
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", strings.ToLower(currency(req.Currency)))
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(req.AmountCents, 10))
	desc := req.Description
	if desc == "" {
		desc = "Payment"
	}
	f.Set("line_items[0][price_data][product_data][name]", desc)
	i := 0
	seen := map[string]bool{}
	for _, m := range req.Methods {
		t := "card" // card and wallets
		if m == "sepa_debit" {
			t = "sepa_debit"
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		f.Set("payment_method_types["+strconv.Itoa(i)+"]", t)
		i++
	}
	if i == 0 {
		f.Set("payment_method_types[0]", "card")
	}
	f.Set("success_url", req.SuccessURL)
	f.Set("cancel_url", req.CancelURL)
	if !req.ExpiresAt.IsZero() {
		f.Set("expires_at", strconv.FormatInt(req.ExpiresAt.Unix(), 10))
	}
	if req.CustomerRef != "" {
		f.Set("customer", req.CustomerRef)
	}
	if l := locale(req.Locale); l != "" {
		f.Set("locale", l)
	}
	setMetadata(f, "metadata", req.MarinaID, req.IntentID)
	setMetadata(f, "payment_intent_data[metadata]", req.MarinaID, req.IntentID)
	return f
}

func setMetadata(f url.Values, prefix, marinaID, intentID string) {
	f.Set(prefix+"[marina_id]", marinaID)
	f.Set(prefix+"[intent_id]", intentID)
}

func currency(c string) string {
	if c == "" {
		return "EUR"
	}
	return c
}

func locale(l string) string {
	l = strings.ToLower(l)
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	switch l {
	case "it", "en", "de", "fr", "es":
		return l
	}
	return ""
}

type sessionObject struct {
	ID            string            `json:"id"`
	Object        string            `json:"object"`
	Mode          string            `json:"mode"`
	Status        string            `json:"status"`
	PaymentStatus string            `json:"payment_status"`
	URL           string            `json:"url"`
	ExpiresAt     int64             `json:"expires_at"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	Customer      json.RawMessage   `json:"customer"`
	PaymentIntent json.RawMessage   `json:"payment_intent"`
	SetupIntent   json.RawMessage   `json:"setup_intent"`
	Metadata      map[string]string `json:"metadata"`
	Created       int64             `json:"created"`
}

// CreateCheckout implements ext.PaymentProvider.
func (d *Driver) CreateCheckout(ctx context.Context, req ext.CheckoutRequest) (*ext.CheckoutSession, error) {
	var s sessionObject
	if err := d.call(ctx, http.MethodPost, "/v1/checkout/sessions", CheckoutForm(req), req.IdempotencyKey, &s); err != nil {
		return nil, err
	}
	out := &ext.CheckoutSession{SessionRef: s.ID, PaymentRef: refID(s.PaymentIntent), URL: s.URL}
	if s.ExpiresAt > 0 {
		out.ExpiresAt = time.Unix(s.ExpiresAt, 0).UTC()
	}
	return out, nil
}

// CreateMandateSetup implements ext.PaymentProvider: a Stripe customer (no personal data, metadata only) when the
// request has none, then a Checkout Session in setup mode for sepa_debit.
func (d *Driver) CreateMandateSetup(ctx context.Context, req ext.MandateSetupRequest) (*ext.CheckoutSession, error) {
	customer := req.CustomerRef
	if customer == "" {
		f := url.Values{}
		f.Set("metadata[marina_id]", req.MarinaID)
		var c struct {
			ID string `json:"id"`
		}
		if err := d.call(ctx, http.MethodPost, "/v1/customers", f, req.IdempotencyKey+":customer", &c); err != nil {
			return nil, err
		}
		customer = c.ID
	}
	f := url.Values{}
	f.Set("mode", "setup")
	f.Set("client_reference_id", req.IntentID)
	f.Set("currency", strings.ToLower(currency(req.Currency)))
	f.Set("customer", customer)
	f.Set("payment_method_types[0]", "sepa_debit")
	f.Set("success_url", req.SuccessURL)
	f.Set("cancel_url", req.CancelURL)
	if l := locale(req.Locale); l != "" {
		f.Set("locale", l)
	}
	setMetadata(f, "metadata", req.MarinaID, req.IntentID)
	setMetadata(f, "setup_intent_data[metadata]", req.MarinaID, req.IntentID)
	var s sessionObject
	if err := d.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, req.IdempotencyKey, &s); err != nil {
		return nil, err
	}
	out := &ext.CheckoutSession{SessionRef: s.ID, URL: s.URL}
	if s.ExpiresAt > 0 {
		out.ExpiresAt = time.Unix(s.ExpiresAt, 0).UTC()
	}
	return out, nil
}

type paymentIntentObject struct {
	ID                 string            `json:"id"`
	Object             string            `json:"object"`
	Status             string            `json:"status"`
	Amount             int64             `json:"amount"`
	AmountReceived     int64             `json:"amount_received"`
	Currency           string            `json:"currency"`
	PaymentMethodTypes []string          `json:"payment_method_types"`
	Metadata           map[string]string `json:"metadata"`
	LatestCharge       json.RawMessage   `json:"latest_charge"`
	LastPaymentError   *struct {
		Code        string `json:"code"`
		DeclineCode string `json:"decline_code"`
	} `json:"last_payment_error"`
	Created int64 `json:"created"`
}

// ChargeMandate implements ext.PaymentProvider: an off-session SEPA PaymentIntent confirmed immediately; the
// outcome usually arrives days later by webhook.
func (d *Driver) ChargeMandate(ctx context.Context, req ext.MandateChargeRequest) (*ext.ProviderPayment, error) {
	f := url.Values{}
	f.Set("amount", strconv.FormatInt(req.AmountCents, 10))
	f.Set("currency", strings.ToLower(currency(req.Currency)))
	f.Set("customer", req.CustomerRef)
	f.Set("payment_method", req.PaymentMethodRef)
	f.Set("payment_method_types[0]", "sepa_debit")
	f.Set("confirm", "true")
	f.Set("off_session", "true")
	if req.Description != "" {
		f.Set("description", req.Description)
	}
	setMetadata(f, "metadata", req.MarinaID, req.IntentID)
	var pi paymentIntentObject
	if err := d.call(ctx, http.MethodPost, "/v1/payment_intents", f, req.IdempotencyKey, &pi); err != nil {
		return nil, err
	}
	return &ext.ProviderPayment{PaymentRef: pi.ID, Status: piStatus(pi.Status)}, nil
}

func piStatus(s string) string {
	switch s {
	case "succeeded":
		return ext.PaymentEventSucceeded
	case "processing":
		return ext.PaymentEventProcessing
	case "canceled", "requires_payment_method":
		return ext.PaymentEventFailed
	}
	return ""
}

type refundObject struct {
	ID            string            `json:"id"`
	Object        string            `json:"object"`
	Status        string            `json:"status"`
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	PaymentIntent json.RawMessage   `json:"payment_intent"`
	FailureReason string            `json:"failure_reason"`
	Metadata      map[string]string `json:"metadata"`
	Created       int64             `json:"created"`
}

// Refund implements ext.PaymentProvider.
func (d *Driver) Refund(ctx context.Context, req ext.RefundRequest) (*ext.RefundResult, error) {
	f := url.Values{}
	f.Set("payment_intent", req.PaymentRef)
	f.Set("amount", strconv.FormatInt(req.AmountCents, 10))
	f.Set("metadata[marina_id]", req.MarinaID)
	var r refundObject
	if err := d.call(ctx, http.MethodPost, "/v1/refunds", f, req.IdempotencyKey, &r); err != nil {
		return nil, err
	}
	return &ext.RefundResult{RefundRef: r.ID, Status: refundStatus(r.Status)}, nil
}

func refundStatus(s string) string {
	switch s {
	case "succeeded":
		return "succeeded"
	case "failed", "canceled":
		return "failed"
	}
	return "pending"
}

// Status implements ext.PaymentProvider: the current state of a refund, a payment intent or a checkout session.
// Kind is empty when nothing changed (session still open, payment still requiring action).
func (d *Driver) Status(ctx context.Context, ref ext.ProviderRef) (*ext.PaymentEvent, error) {
	switch {
	case ref.RefundRef != "":
		var r refundObject
		if err := d.call(ctx, http.MethodGet, "/v1/refunds/"+url.PathEscape(ref.RefundRef), nil, "", &r); err != nil {
			return nil, err
		}
		ev := refundEvent(&r)
		return ev, nil
	case ref.PaymentRef != "":
		q := url.Values{}
		q.Set("expand[0]", "latest_charge.balance_transaction")
		var pi paymentIntentObject
		if err := d.call(ctx, http.MethodGet, "/v1/payment_intents/"+url.PathEscape(ref.PaymentRef), q, "", &pi); err != nil {
			return nil, err
		}
		ev := piEvent(&pi, piStatus(pi.Status))
		return ev, nil
	case ref.SessionRef != "":
		q := url.Values{}
		q.Set("expand[0]", "payment_intent.latest_charge.balance_transaction")
		var s sessionObject
		if err := d.call(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(ref.SessionRef), q, "", &s); err != nil {
			return nil, err
		}
		ev := sessionEvent(&s, sessionKind(&s))
		if pi := expandedPI(s.PaymentIntent); pi != nil {
			ev.FeeCents = fee(pi.LatestCharge)
			if ev.Kind == ext.PaymentEventProcessing || ev.Kind == "" {
				if k := piStatus(pi.Status); k != "" {
					ev.Kind = k
				}
			}
		}
		return ev, nil
	}
	return nil, errors.New("stripe: empty reference")
}

func sessionKind(s *sessionObject) string {
	switch {
	case s.Status == "expired":
		return ext.PaymentEventExpired
	case s.Status == "complete" && (s.PaymentStatus == "paid" || s.PaymentStatus == "no_payment_required"):
		return ext.PaymentEventSucceeded
	case s.Status == "complete":
		return ext.PaymentEventProcessing
	}
	return ""
}

// CloseCheckout implements ext.PaymentProvider.
func (d *Driver) CloseCheckout(ctx context.Context, sessionRef string) error {
	return d.call(ctx, http.MethodPost, "/v1/checkout/sessions/"+url.PathEscape(sessionRef)+"/expire", url.Values{}, "", nil)
}

// TestCredentials reads the account balance: a cheap authenticated call (POST …/test of the settings page).
func (d *Driver) TestCredentials(ctx context.Context) error {
	return d.call(ctx, http.MethodGet, "/v1/balance", nil, "", nil)
}

// call sends one form-encoded request and decodes the JSON answer into out (nil = discard).
func (d *Driver) call(ctx context.Context, method, path string, form url.Values, idem string, out any) error {
	key := ext.ProviderCredentials(ctx)[CredentialSecretKey]
	if key == "" {
		return ErrNoCredentials
	}
	target := d.base + path
	var body io.Reader
	if method == http.MethodGet {
		if len(form) > 0 {
			target += "?" + form.Encode()
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Stripe-Version", APIVersion)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: request failed: %w", stripURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("stripe: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Type string `json:"type"`
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return &Error{Status: resp.StatusCode, Type: token(e.Error.Type), Code: token(e.Error.Code)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("stripe: invalid response")
	}
	return nil
}

// stripURL drops the URL from transport errors (it may carry query parameters).
func stripURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}

// token keeps only a short slug of [a-z0-9_] so that provider strings never carry free text.
func token(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// refID returns the id of a field that is either an id string or an expanded object.
func refID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.ID
}

func expandedPI(raw json.RawMessage) *paymentIntentObject {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var pi paymentIntentObject
	if json.Unmarshal(raw, &pi) != nil {
		return nil
	}
	return &pi
}

// fee reads latest_charge.balance_transaction.fee when expanded.
func fee(latestCharge json.RawMessage) int64 {
	if len(latestCharge) == 0 || latestCharge[0] != '{' {
		return 0
	}
	var c struct {
		BalanceTransaction json.RawMessage `json:"balance_transaction"`
	}
	if json.Unmarshal(latestCharge, &c) != nil || len(c.BalanceTransaction) == 0 || c.BalanceTransaction[0] != '{' {
		return 0
	}
	var bt struct {
		Fee int64 `json:"fee"`
	}
	_ = json.Unmarshal(c.BalanceTransaction, &bt)
	return bt.Fee
}

// cleanMetadata keeps only marina_id and intent_id.
func cleanMetadata(m map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"marina_id", "intent_id"} {
		if v := m[k]; v != "" {
			out[k] = v
		}
	}
	return out
}
