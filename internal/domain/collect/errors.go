package collect

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/store"
)

// Errors (m5-plan §7.4).
var (
	// ErrProviderDisabled: no enabled provider with the capability for the marina (422 provider_disabled).
	ErrProviderDisabled = errors.New("collect: provider disabled")
	// ErrProviderRefundUnsupported: the payment's provider cannot refund (422 provider_refund_unsupported).
	ErrProviderRefundUnsupported = errors.New("collect: provider refund unsupported")
	// ErrProviderError: the provider refused or failed (502 provider_error, details.code); see *ProviderError.
	ErrProviderError = errors.New("collect: provider error")
	// ErrProviderUnknown: no driver with that code (422 provider_unknown).
	ErrProviderUnknown = errors.New("collect: provider unknown")
	// ErrChargesNotOpen: a payment request for charges that are not open (422 charges_not_open).
	ErrChargesNotOpen = errors.New("collect: charges not open")
	// ErrInvalidWebhookSignature: the callback failed verification (401 invalid_signature).
	ErrInvalidWebhookSignature = errors.New("collect: invalid webhook signature")
	// ErrWebhookSecretMissing: the provider is configured for the marina but has no webhook secret, so no callback
	// can be verified (401 invalid_signature, m5-plan §5.4).
	ErrWebhookSecretMissing = errors.New("collect: webhook secret missing")
	// ErrLinkInvalid: unknown, expired or cancelled payment link (404 link_invalid).
	ErrLinkInvalid = errors.New("collect: link invalid")
	// ErrRetryLater: an event for an intent not committed yet (503, the provider retries).
	ErrRetryLater = errors.New("collect: retry later")
	// ErrVersionMismatch: If-Match differs (412).
	ErrVersionMismatch = errors.New("collect: version mismatch")
	// ErrNotFound is the store sentinel (404).
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the contract-phase skeleton until CASH-DRIVERS lands M5.
	ErrNotImplemented = errors.New("collect: not implemented")
)

// ProviderError carries the provider's error code (type and code only, never a request echo).
type ProviderError struct {
	Code string
}

func (e *ProviderError) Error() string { return ErrProviderError.Error() + ": " + e.Code }

// Is makes errors.Is(err, ErrProviderError) true.
func (e *ProviderError) Is(target error) bool { return target == ErrProviderError }

// More errors of the collection service, mapped through the sentinels they wrap.
var (
	// ErrCustomerEmailMissing: the customer has no email address (422 customer_email_missing).
	ErrCustomerEmailMissing = contract.ErrCustomerEmailMissing
	// ErrRequestClosed: the request is paid, cancelled or expired (409).
	ErrRequestClosed = fmt.Errorf("collect: payment request closed: %w", store.ErrConflict)
	// ErrItemDecided: the reminder item is not awaiting approval (409).
	ErrItemDecided = fmt.Errorf("collect: reminder item already decided: %w", store.ErrConflict)
	// ErrMandateClosed: the mandate is revoked or failed already (409).
	ErrMandateClosed = fmt.Errorf("collect: mandate closed: %w", store.ErrConflict)
	// ErrExclusionRemoved: the exclusion was removed already (409).
	ErrExclusionRemoved = fmt.Errorf("collect: exclusion removed: %w", store.ErrConflict)
	// ErrInvalidTransition is the contract sentinel (409 invalid_transition), e.g. a payment link for a transit
	// that is not held.
	ErrInvalidTransition = contract.ErrInvalidTransition
)
