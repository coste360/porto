package signature

import (
	"context"

	"github.com/coste360/porto/internal/store"
)

// Drivers (signature_requests.driver).
const (
	DriverPaper    = "paper"
	DriverOTP      = "otp"
	DriverProvider = "provider"
)

// Request statuses (signature_requests.status).
const (
	StatusPendingDocument = "pending_document"
	StatusSent            = "sent"
	StatusPartiallySigned = "partially_signed"
	StatusSigned          = "signed"
	StatusDeclined        = "declined"
	StatusExpired         = "expired"
	StatusCancelled       = "cancelled"
)

// Driver opens, polls and cancels requests of one kind (m4-plan §6.5): paperDriver, otpDriver and providerDriver
// (an adapter over ext.SignatureProvider, credentials decrypted from signature_settings only in memory).
type Driver interface {
	Code() string
	// Open is called once the document is ready: it invites the signers (email with a signature link for otp,
	// nothing for paper, the provider envelope for provider).
	Open(ctx context.Context, scope store.MarinaScope, requestID string) error
	// Status reconciles the request with the driver (polling of providers; no-op for paper and otp).
	Status(ctx context.Context, scope store.MarinaScope, requestID string) error
	Cancel(ctx context.Context, scope store.MarinaScope, requestID string) error
}
