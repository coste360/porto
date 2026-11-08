package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// SignatureHeader is the header of a Stripe webhook signature.
const SignatureHeader = "Stripe-Signature"

// Sign returns a Stripe-Signature header for body at t with the given secrets (one v1 per secret). It is used by
// the tests and by the fake provider of the acceptance fixture; production only verifies.
func Sign(body []byte, t time.Time, secrets ...string) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	parts := []string{"t=" + ts}
	for _, s := range secrets {
		parts = append(parts, "v1="+mac(s, ts, body))
	}
	return strings.Join(parts, ",")
}

func mac(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(ts))
	h.Write([]byte("."))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Verify checks a Stripe-Signature header: t=…,v1=… (several v1 accepted during a secret rotation), HMAC-SHA256 of
// "t.body" with the secret, constant-time comparison, |now − t| ≤ SignatureTolerance.
func Verify(header string, body, secret []byte, now time.Time) error {
	if len(secret) == 0 || header == "" {
		return ext.ErrInvalidSignature
	}
	var ts string
	var sigs []string
	for _, p := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ext.ErrInvalidSignature
	}
	at := time.Unix(sec, 0)
	if d := now.Sub(at); d > SignatureTolerance || d < -SignatureTolerance {
		return ext.ErrInvalidSignature
	}
	want := mac(string(secret), ts, body)
	ok := false
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			ok = true
		}
	}
	if !ok {
		return ext.ErrInvalidSignature
	}
	return nil
}

type eventEnvelope struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Data    struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// ParseWebhook implements ext.PaymentProvider. Event types Porto does not use return ErrIgnoredEvent together with
// an event carrying only the id (so that the host records it as ignored).
func (d *Driver) ParseWebhook(r *http.Request, secret []byte, now time.Time) (*ext.PaymentEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBytes+1))
	if err != nil || len(body) > MaxWebhookBytes {
		return nil, ext.ErrInvalidSignature
	}
	if err := Verify(r.Header.Get(SignatureHeader), body, secret, now); err != nil {
		return nil, err
	}
	return MapEvent(body)
}

// MapEvent decodes a verified event body (exported for the event mapping tests).
func MapEvent(body []byte) (*ext.PaymentEvent, error) {
	var env eventEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.ID == "" {
		return nil, ext.ErrInvalidSignature
	}
	ignored := &ext.PaymentEvent{EventID: env.ID, OccurredAt: unix(env.Created)}
	obj := env.Data.Object
	var ev *ext.PaymentEvent
	switch env.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded",
		"checkout.session.async_payment_failed", "checkout.session.expired":
		var s sessionObject
		if json.Unmarshal(obj, &s) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		if s.Mode == "setup" {
			// The mandate is activated by setup_intent.succeeded, which carries the payment method.
			return ignored, ext.ErrIgnoredEvent
		}
		kind := map[string]string{
			"checkout.session.async_payment_succeeded": ext.PaymentEventSucceeded,
			"checkout.session.async_payment_failed":    ext.PaymentEventFailed,
			"checkout.session.expired":                 ext.PaymentEventExpired,
		}[env.Type]
		if env.Type == "checkout.session.completed" {
			kind = ext.PaymentEventProcessing
			if s.PaymentStatus == "paid" {
				kind = ext.PaymentEventSucceeded
			}
		}
		ev = sessionEvent(&s, kind)
	case "payment_intent.succeeded", "payment_intent.processing", "payment_intent.payment_failed":
		var pi paymentIntentObject
		if json.Unmarshal(obj, &pi) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		kind := map[string]string{
			"payment_intent.succeeded":      ext.PaymentEventSucceeded,
			"payment_intent.processing":     ext.PaymentEventProcessing,
			"payment_intent.payment_failed": ext.PaymentEventFailed,
		}[env.Type]
		ev = piEvent(&pi, kind)
	case "payment_intent.amount_capturable_updated", "payment_intent.canceled":
		// M8 manual capture (m8-plan §6): an authorisation, and its release.
		var pi paymentIntentObject
		if json.Unmarshal(obj, &pi) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		kind := ext.PaymentEventAuthorizationVoided
		if env.Type == "payment_intent.amount_capturable_updated" {
			if pi.Status != "requires_capture" {
				return ignored, ext.ErrIgnoredEvent
			}
			kind = ext.PaymentEventAuthorized
		}
		ev = piEvent(&pi, kind)
		if kind == ext.PaymentEventAuthorized && pi.AmountCapturable > 0 {
			ev.AmountCents = pi.AmountCapturable
		}
	case "charge.refunded":
		var c struct {
			PaymentIntent  json.RawMessage `json:"payment_intent"`
			AmountRefunded int64           `json:"amount_refunded"`
			Currency       string          `json:"currency"`
			Refunds        struct {
				Data []refundObject `json:"data"`
			} `json:"refunds"`
		}
		if json.Unmarshal(obj, &c) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		ev = &ext.PaymentEvent{
			Kind: ext.PaymentEventRefundSucceeded, PaymentRef: refID(c.PaymentIntent), AmountCents: c.AmountRefunded,
			Currency: strings.ToUpper(c.Currency), Metadata: map[string]string{},
		}
		if len(c.Refunds.Data) > 0 {
			ev.RefundRef = c.Refunds.Data[0].ID
			ev.AmountCents = c.Refunds.Data[0].Amount
		}
	case "refund.updated", "refund.created":
		var rf refundObject
		if json.Unmarshal(obj, &rf) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		ev = refundEvent(&rf)
		if ev.Kind == "" {
			return ignored, ext.ErrIgnoredEvent
		}
	case "setup_intent.succeeded":
		var si struct {
			ID            string            `json:"id"`
			Customer      json.RawMessage   `json:"customer"`
			PaymentMethod json.RawMessage   `json:"payment_method"`
			Mandate       json.RawMessage   `json:"mandate"`
			Metadata      map[string]string `json:"metadata"`
		}
		if json.Unmarshal(obj, &si) != nil {
			return ignored, ext.ErrIgnoredEvent
		}
		ev = &ext.PaymentEvent{
			Kind: ext.PaymentEventMandateActive, SessionRef: si.ID, CustomerRef: refID(si.Customer),
			PaymentMethodRef: refID(si.PaymentMethod), MandateRef: refID(si.Mandate), IBANLast4: ibanLast4(si.PaymentMethod),
			Metadata: cleanMetadata(si.Metadata),
		}
	case "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed",
		"charge.dispute.funds_withdrawn", "charge.dispute.funds_reinstated":
		var dp struct {
			PaymentIntent json.RawMessage `json:"payment_intent"`
			Amount        int64           `json:"amount"`
			Currency      string          `json:"currency"`
		}
		if json.Unmarshal(obj, &dp) != nil || refID(dp.PaymentIntent) == "" {
			return ignored, ext.ErrIgnoredEvent
		}
		ev = &ext.PaymentEvent{
			Kind: ext.PaymentEventDisputed, PaymentRef: refID(dp.PaymentIntent), AmountCents: dp.Amount,
			Currency: strings.ToUpper(dp.Currency), Metadata: map[string]string{},
		}
	case "mandate.updated":
		var m struct {
			ID            string          `json:"id"`
			Status        string          `json:"status"`
			PaymentMethod json.RawMessage `json:"payment_method"`
		}
		if json.Unmarshal(obj, &m) != nil || m.Status == "active" || m.Status == "pending" {
			return ignored, ext.ErrIgnoredEvent
		}
		ev = &ext.PaymentEvent{
			Kind: ext.PaymentEventMandateRevoked, MandateRef: m.ID, PaymentMethodRef: refID(m.PaymentMethod),
			Metadata: map[string]string{},
		}
	default:
		return ignored, ext.ErrIgnoredEvent
	}
	ev.EventID = env.ID
	ev.OccurredAt = unix(env.Created)
	return ev, nil
}

func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func sessionEvent(s *sessionObject, kind string) *ext.PaymentEvent {
	return &ext.PaymentEvent{
		Kind: kind, SessionRef: s.ID, PaymentRef: refID(s.PaymentIntent), CustomerRef: refID(s.Customer),
		AmountCents: s.AmountTotal, Currency: strings.ToUpper(s.Currency), Metadata: cleanMetadata(s.Metadata),
		OccurredAt: unix(s.Created),
	}
}

func piEvent(pi *paymentIntentObject, kind string) *ext.PaymentEvent {
	amount := pi.AmountReceived
	if amount == 0 {
		amount = pi.Amount
	}
	ev := &ext.PaymentEvent{
		Kind: kind, PaymentRef: pi.ID, AmountCents: amount, Currency: strings.ToUpper(pi.Currency),
		FeeCents: fee(pi.LatestCharge), Metadata: cleanMetadata(pi.Metadata), OccurredAt: unix(pi.Created),
	}
	if len(pi.PaymentMethodTypes) == 1 && pi.PaymentMethodTypes[0] == "sepa_debit" {
		ev.Method = "sepa_debit"
	}
	if pi.LastPaymentError != nil && kind == ext.PaymentEventFailed {
		ev.FailureCode = token(pi.LastPaymentError.Code)
		if ev.FailureCode == "" {
			ev.FailureCode = token(pi.LastPaymentError.DeclineCode)
		}
	}
	return ev
}

func refundEvent(r *refundObject) *ext.PaymentEvent {
	kind := ""
	switch refundStatus(r.Status) {
	case "succeeded":
		kind = ext.PaymentEventRefundSucceeded
	case "failed":
		kind = ext.PaymentEventRefundFailed
	}
	return &ext.PaymentEvent{
		Kind: kind, RefundRef: r.ID, PaymentRef: refID(r.PaymentIntent), AmountCents: r.Amount,
		Currency: strings.ToUpper(r.Currency), FailureCode: token(r.FailureReason), Metadata: cleanMetadata(r.Metadata),
		OccurredAt: unix(r.Created),
	}
}

// ibanLast4 reads payment_method.sepa_debit.last4 when the payment method is expanded.
func ibanLast4(raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '{' {
		return ""
	}
	var pm struct {
		SEPADebit struct {
			Last4 string `json:"last4"`
		} `json:"sepa_debit"`
	}
	_ = json.Unmarshal(raw, &pm)
	if len(pm.SEPADebit.Last4) > 4 {
		return ""
	}
	return pm.SEPADebit.Last4
}

var (
	_ ext.PaymentProvider   = (*Driver)(nil)
	_ ext.PaymentAuthorizer = (*Driver)(nil)
)
