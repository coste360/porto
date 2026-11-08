package ext

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// PaymentProvider is a payment gateway or method driver (spec 16.2). Porto works with no provider at all: the
// manual driver is always present and online drivers are optional, enabled per marina. Credentials arrive per call
// in the context (WithProviderCredentials); outbound HTTP goes only through the client the host gives the driver
// at construction. Calls never run inside a database transaction. Amounts are integer cents with an explicit ISO
// 4217 currency; metadata carry only marina_id and intent_id.
type PaymentProvider interface {
	Code() string
	Capabilities() PaymentCapabilities
	// CreateCheckout opens a hosted payment page (ErrNotSupported when the driver has none).
	CreateCheckout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)
	// CreateMandateSetup opens a hosted page that collects a direct debit mandate (SEPA).
	CreateMandateSetup(ctx context.Context, req MandateSetupRequest) (*CheckoutSession, error)
	// ChargeMandate collects an amount off-session on an active mandate; the outcome usually arrives later by
	// webhook.
	ChargeMandate(ctx context.Context, req MandateChargeRequest) (*ProviderPayment, error)
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
	// Status reads the current state of a session or payment (reconciliation of missed webhooks).
	Status(ctx context.Context, ref ProviderRef) (*PaymentEvent, error)
	// ParseWebhook verifies a callback with the marina's webhook secret against now (the host's clock) and decodes
	// it: ErrInvalidSignature, or ErrIgnoredEvent for event types Porto does not use.
	ParseWebhook(r *http.Request, secret []byte, now time.Time) (*PaymentEvent, error)
	// CloseCheckout expires an open hosted session (best effort).
	CloseCheckout(ctx context.Context, sessionRef string) error
}

// Errors of payment drivers.
var (
	ErrNotSupported     = errors.New("ext: operation not supported by the provider")
	ErrInvalidSignature = errors.New("ext: invalid webhook signature")
	ErrIgnoredEvent     = errors.New("ext: event ignored")
)

// PaymentCapabilities says what a driver can do. Methods lists the online methods (card, wallet, sepa_debit).
type PaymentCapabilities struct {
	Checkout      bool
	MandateSetup  bool
	MandateCharge bool
	Refund        bool
	StatusCheck   bool
	Methods       []string
}

// CheckoutRequest asks for a hosted payment page for one intent.
type CheckoutRequest struct {
	MarinaID       string
	IntentID       string
	IdempotencyKey string
	AmountCents    int64
	Currency       string
	Description    string   // charge numbers only, never personal data
	Methods        []string // card, wallet, sepa_debit
	SuccessURL     string
	CancelURL      string
	ExpiresAt      time.Time
	CustomerRef    string // provider customer reference, when known
	Locale         string
	// CaptureManual asks for an authorisation only (card pre-authorisation, M8 approval flow); the host captures
	// or voids it later through PaymentAuthorizer. Drivers that do not implement PaymentAuthorizer must return
	// ErrNotSupported when it is set.
	CaptureManual bool
}

// MandateSetupRequest asks for a hosted page that collects a direct debit mandate.
type MandateSetupRequest struct {
	MarinaID       string
	IntentID       string
	IdempotencyKey string
	Currency       string
	SuccessURL     string
	CancelURL      string
	CustomerRef    string
	Locale         string
}

// MandateChargeRequest collects an amount on an active mandate.
type MandateChargeRequest struct {
	MarinaID         string
	IntentID         string
	IdempotencyKey   string
	AmountCents      int64
	Currency         string
	Description      string
	CustomerRef      string
	PaymentMethodRef string
}

// CheckoutSession is a hosted page opened by the provider.
type CheckoutSession struct {
	SessionRef string
	PaymentRef string
	URL        string
	ExpiresAt  time.Time
}

// ProviderPayment is a payment created at the provider (mandate collection).
type ProviderPayment struct {
	PaymentRef string
	Status     string // processing, succeeded, failed
}

// RefundRequest refunds (part of) a provider payment.
type RefundRequest struct {
	MarinaID       string
	RefundID       string
	IdempotencyKey string
	PaymentRef     string
	AmountCents    int64
	Currency       string
}

// RefundResult is the provider's answer to a refund.
type RefundResult struct {
	RefundRef string
	Status    string // pending, succeeded, failed
}

// ProviderRef names a session or a payment at the provider (Status).
type ProviderRef struct {
	SessionRef string
	PaymentRef string
	RefundRef  string
}

// PaymentAuthorizer is optionally implemented by a PaymentProvider that supports manual capture (m8-plan §6,
// §16 D6). The host checks it with a type assertion. Both calls are idempotent on IdempotencyKey, run outside any
// database transaction, and their outcome is confirmed by webhook (PaymentEventSucceeded after a capture,
// PaymentEventAuthorizationVoided after a void).
type PaymentAuthorizer interface {
	// Capture captures an authorised payment (the full authorised amount when AmountCents is 0).
	Capture(ctx context.Context, req CaptureRequest) (*ProviderPayment, error)
	// VoidAuthorization releases an authorisation that will not be captured.
	VoidAuthorization(ctx context.Context, req VoidRequest) error
}

// CaptureRequest captures an authorised payment.
type CaptureRequest struct {
	MarinaID       string
	IntentID       string
	IdempotencyKey string // capture:<intent>
	PaymentRef     string
	AmountCents    int64
	Currency       string
}

// VoidRequest releases an authorised payment.
type VoidRequest struct {
	MarinaID       string
	IntentID       string
	IdempotencyKey string // void:<intent>
	PaymentRef     string
}

// Payment event kinds.
const (
	// PaymentEventAuthorized is a manual-capture payment authorised by the customer (funds held, not captured).
	PaymentEventAuthorized = "authorized"
	// PaymentEventAuthorizationVoided is an authorisation released without capture (voided or expired).
	PaymentEventAuthorizationVoided = "authorization_voided"
	PaymentEventSucceeded           = "succeeded"
	PaymentEventProcessing          = "processing"
	PaymentEventFailed              = "failed"
	PaymentEventExpired             = "expired"
	PaymentEventRefundSucceeded     = "refund_succeeded"
	PaymentEventRefundFailed        = "refund_failed"
	PaymentEventMandateActive       = "mandate_active"
	PaymentEventMandateRevoked      = "mandate_revoked"
	// PaymentEventDisputed is a dispute (chargeback) opened, updated or closed on a payment (PaymentRef). The host
	// records it as ignored and raises a worklist item for the office; money is never moved automatically.
	PaymentEventDisputed = "disputed"
)

// PaymentEvent is a decoded provider event (webhook or status check). Metadata carries only marina_id and
// intent_id. MandateRef, CustomerRef and PaymentMethodRef are set by mandate events; IBANLast4 by mandate
// activation.
type PaymentEvent struct {
	EventID          string
	Kind             string
	SessionRef       string
	PaymentRef       string
	RefundRef        string
	MandateRef       string
	CustomerRef      string
	PaymentMethodRef string
	IBANLast4        string
	AmountCents      int64
	Currency         string
	FeeCents         int64
	Method           string // card_online, wallet, sepa_debit
	FailureCode      string
	Metadata         map[string]string
	OccurredAt       time.Time
}
