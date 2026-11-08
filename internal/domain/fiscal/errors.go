package fiscal

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/domain/auth"

	"github.com/coste360/porto/internal/store"
)

// Errors.
var (
	// ErrDriverUnknown: billing_settings.fiscal_driver names no registered driver.
	ErrDriverUnknown = errors.New("fiscal: driver unknown")
	// ErrNotFound is the store sentinel (404).
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the contract-phase skeleton until CASH-DRIVERS lands M5.
	ErrNotImplemented = errors.New("fiscal: not implemented")
	// ErrClaimExpired: the bridge claim of a document is stale (lease expired or claimed again, M6b §8.4).
	// 409 claim_expired.
	ErrClaimExpired = errors.New("fiscal: bridge claim expired")
	// ErrBridgeDeviceMismatch: the calling device is not the bridge device of the marina's billing settings
	// (M6b §8.4). 403 forbidden.
	ErrBridgeDeviceMismatch = errors.New("fiscal: not the bridge device")
)

// More errors of the fiscal service.
var (
	// ErrNotRetryable: only pending or failed documents can be retried (409).
	ErrNotRetryable = fmt.Errorf("fiscal: document not retryable: %w", store.ErrConflict)
	// ErrNotVoidable: only issued documents can be voided (409).
	ErrNotVoidable = fmt.Errorf("fiscal: document not voidable: %w", store.ErrConflict)
	// ErrDriverFailed: the fiscal driver refused the operation (409).
	ErrDriverFailed = fmt.Errorf("fiscal: driver failed: %w", store.ErrConflict)
	// ErrInvalidRange: a corrispettivi range without from/to, reversed or longer than 400 days (validation_failed).
	ErrInvalidRange = fmt.Errorf("fiscal: invalid range: %w", auth.ErrInvalidInput)
)
