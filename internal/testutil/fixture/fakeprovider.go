package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/coste360/porto/internal/domain/paydriver/stripe"
	"github.com/coste360/porto/pkg/ext"
)

// FakeProviderCode is the code of the fake payment provider of the tests.
const FakeProviderCode = "fakepay"

// FakeSignatureHeader is the signature header of the fake provider (Stripe's scheme: t=…,v1=hmac(t.body)).
const FakeSignatureHeader = "Fake-Signature"

// FakeProvider is an in-memory ext.PaymentProvider for the M5 tests and acceptance scenarios: it records the calls,
// hands out deterministic references, and its webhooks are signed JSON ext.PaymentEvent bodies. It does no I/O.
// Shared read-only by the CHARGES-EXPORT and PAYMENTS acceptance tests (m5-plan §14.3).
type FakeProvider struct {
	mu        sync.Mutex
	seq       int
	Checkouts []ext.CheckoutRequest
	Setups    []ext.MandateSetupRequest
	Charges   []ext.MandateChargeRequest
	Refunds   []ext.RefundRequest
	Closed    []string
	// StatusOf answers Status by reference (session or payment ref); missing = no change.
	StatusOf map[string]*ext.PaymentEvent
	// FailCheckout makes CreateCheckout fail with ext.ErrNotSupported.
	FailCheckout bool
	// RefundStatus is the status Refund answers (default pending).
	RefundStatus string
	// M8 manual capture (ext.PaymentAuthorizer): recorded captures and voids; FailCapture makes Capture fail with
	// ext.ErrNotSupported (an expired authorisation).
	Captures    []ext.CaptureRequest
	Voids       []ext.VoidRequest
	FailCapture bool
}

// NewFakeProvider returns a fake provider.
func NewFakeProvider() *FakeProvider { return &FakeProvider{StatusOf: map[string]*ext.PaymentEvent{}} }

func (f *FakeProvider) next(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s_%04d", prefix, f.seq)
}

// Code implements ext.PaymentProvider.
func (*FakeProvider) Code() string { return FakeProviderCode }

// Capabilities implements ext.PaymentProvider.
func (*FakeProvider) Capabilities() ext.PaymentCapabilities {
	return ext.PaymentCapabilities{
		Checkout: true, MandateSetup: true, MandateCharge: true, Refund: true, StatusCheck: true,
		Methods: []string{"card", "wallet", "sepa_debit"},
	}
}

// CreateCheckout implements ext.PaymentProvider.
func (f *FakeProvider) CreateCheckout(_ context.Context, req ext.CheckoutRequest) (*ext.CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailCheckout {
		return nil, ext.ErrNotSupported
	}
	f.Checkouts = append(f.Checkouts, req)
	ref := f.next("cs")
	return &ext.CheckoutSession{SessionRef: ref, URL: "https://pay.fake.test/" + ref, ExpiresAt: req.ExpiresAt}, nil
}

// CreateMandateSetup implements ext.PaymentProvider.
func (f *FakeProvider) CreateMandateSetup(_ context.Context, req ext.MandateSetupRequest) (*ext.CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Setups = append(f.Setups, req)
	ref := f.next("cs_setup")
	return &ext.CheckoutSession{SessionRef: ref, URL: "https://pay.fake.test/" + ref}, nil
}

// ChargeMandate implements ext.PaymentProvider.
func (f *FakeProvider) ChargeMandate(_ context.Context, req ext.MandateChargeRequest) (*ext.ProviderPayment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Charges = append(f.Charges, req)
	return &ext.ProviderPayment{PaymentRef: f.next("pi"), Status: ext.PaymentEventProcessing}, nil
}

// Refund implements ext.PaymentProvider.
func (f *FakeProvider) Refund(_ context.Context, req ext.RefundRequest) (*ext.RefundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Refunds = append(f.Refunds, req)
	st := f.RefundStatus
	if st == "" {
		st = "pending"
	}
	return &ext.RefundResult{RefundRef: "re_" + req.RefundID, Status: st}, nil
}

// Status implements ext.PaymentProvider.
func (f *FakeProvider) Status(_ context.Context, ref ext.ProviderRef) (*ext.PaymentEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range []string{ref.PaymentRef, ref.SessionRef, ref.RefundRef} {
		if ev, ok := f.StatusOf[k]; ok && k != "" {
			cp := *ev
			return &cp, nil
		}
	}
	return &ext.PaymentEvent{}, nil
}

// ParseWebhook implements ext.PaymentProvider: verifies FakeSignatureHeader and decodes a JSON ext.PaymentEvent.
func (f *FakeProvider) ParseWebhook(r *http.Request, secret []byte, now time.Time) (*ext.PaymentEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, stripe.MaxWebhookBytes+1))
	if err != nil {
		return nil, ext.ErrInvalidSignature
	}
	if err := stripe.Verify(r.Header.Get(FakeSignatureHeader), body, secret, now); err != nil {
		return nil, err
	}
	var ev ext.PaymentEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.EventID == "" {
		return nil, ext.ErrInvalidSignature
	}
	if ev.Kind == "" {
		return &ev, ext.ErrIgnoredEvent
	}
	return &ev, nil
}

// CloseCheckout implements ext.PaymentProvider.
func (f *FakeProvider) CloseCheckout(_ context.Context, sessionRef string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Closed = append(f.Closed, sessionRef)
	return nil
}

// TestCredentials lets the settings page test pass.
func (*FakeProvider) TestCredentials(context.Context) error { return nil }

// LastCheckout returns the last checkout request (zero value when none).
func (f *FakeProvider) LastCheckout() ext.CheckoutRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Checkouts) == 0 {
		return ext.CheckoutRequest{}
	}
	return f.Checkouts[len(f.Checkouts)-1]
}

// WebhookRequest builds a signed webhook request of an event for a marina, signed at t with secret.
func WebhookRequest(ev ext.PaymentEvent, marinaID, secret string, t time.Time) *http.Request {
	body, _ := json.Marshal(ev)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/payments/"+FakeProviderCode+"/"+marinaID, bytes.NewReader(body))
	r.Header.Set(FakeSignatureHeader, stripe.Sign(body, t, secret))
	return r
}

// SignedBody returns the body and signature header of an event (HTTP-level acceptance tests).
func SignedBody(ev ext.PaymentEvent, secret string, t time.Time) ([]byte, string) {
	body, _ := json.Marshal(ev)
	return body, stripe.Sign(body, t, secret)
}

// Capture implements ext.PaymentAuthorizer (M8): records the capture; the outcome arrives by webhook
// (PaymentEventSucceeded built by the test).
func (f *FakeProvider) Capture(_ context.Context, req ext.CaptureRequest) (*ext.ProviderPayment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailCapture {
		return nil, ext.ErrNotSupported
	}
	f.Captures = append(f.Captures, req)
	return &ext.ProviderPayment{PaymentRef: req.PaymentRef, Status: ext.PaymentEventProcessing}, nil
}

// VoidAuthorization implements ext.PaymentAuthorizer (M8): records the void.
func (f *FakeProvider) VoidAuthorization(_ context.Context, req ext.VoidRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Voids = append(f.Voids, req)
	return nil
}

var (
	_ ext.PaymentProvider   = (*FakeProvider)(nil)
	_ ext.PaymentAuthorizer = (*FakeProvider)(nil)
)
