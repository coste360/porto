package handover

import (
	"errors"

	"github.com/coste360/porto/internal/domain/document"
)

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until HANDOVER lands the bodies (501).
	ErrNotImplemented = errors.New("rental handover: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("rental handover: version mismatch")
	// ErrInvalidTransition means the booking status does not allow the step (409 invalid_transition).
	ErrInvalidTransition = errors.New("rental handover: invalid transition")
	// ErrHandoverIncomplete is the sentinel of IncompleteError (422 handover_incomplete {missing}).
	ErrHandoverIncomplete = errors.New("rental handover: handover incomplete")
	// ErrHandoverLocked is a save on a completed handover, e.g. a late offline replay (409 handover_locked).
	ErrHandoverLocked = errors.New("rental handover: handover locked")
	// ErrEngineHoursDecreasing is engine hours below the unit's or the check-out's (422 engine_hours_decreasing).
	ErrEngineHoursDecreasing = errors.New("rental handover: engine hours decreasing")
	// ErrLicenceRequired is a check-out without the licence the unit needs (422 licence_required).
	ErrLicenceRequired = errors.New("rental handover: licence required")
	// ErrLicenceExpired is a licence expired on the rental dates (422 licence_expired).
	ErrLicenceExpired = errors.New("rental handover: licence expired")
	// ErrPreauthUnavailable is a pre-authorisation without an enabled provider implementing ext.PaymentAuthorizer
	// (422 preauth_unavailable).
	ErrPreauthUnavailable = errors.New("rental handover: pre-authorisation unavailable")
	// ErrDepositExceeds is a retention above the deposit (422 deposit_exceeds).
	ErrDepositExceeds = errors.New("rental handover: retention exceeds the deposit")
	// ErrNothingToCollect is a quick cash with no residual on the target charges (409 nothing_to_collect).
	ErrNothingToCollect = errors.New("rental handover: nothing to collect")
	// ErrCustomerRequired is an invoice-mode step without a booking customer (422 customer_required).
	ErrCustomerRequired = errors.New("rental handover: customer required")
	// ErrOfflineTimeOutOfRange is an offline save whose device time is outside the window (422
	// offline_time_out_of_range).
	ErrOfflineTimeOutOfRange = errors.New("rental handover: offline time out of range")
	// ErrUnsupportedMedia is a photo that is not a JPEG or PNG (415 unsupported_media_type).
	ErrUnsupportedMedia = document.ErrUnsupportedMedia
	// ErrFileTooLarge is a photo above MaxPhotoBytes (413 payload_too_large).
	ErrFileTooLarge = document.ErrFileTooLarge
)

// IncompleteError is 422 handover_incomplete with the missing steps (identity, licence, contract, deposit,
// checklist, photos, engine_hours, fuel).
type IncompleteError struct {
	Missing []string
}

func (e *IncompleteError) Error() string { return "rental handover: handover incomplete" }

// Unwrap lets errors.Is(err, ErrHandoverIncomplete) match.
func (e *IncompleteError) Unwrap() error { return ErrHandoverIncomplete }
