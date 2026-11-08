package utility

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until UTILITIES lands the bodies (501).
	ErrNotImplemented = errors.New("utility: not implemented")
	// ErrReadingCharged means the reading is used by a charged consumption (409 reading_charged).
	ErrReadingCharged = errors.New("utility: reading charged")
	// ErrConsumptionFrozen means the consumption is charged or excluded (409 consumption_frozen).
	ErrConsumptionFrozen = errors.New("utility: consumption frozen")
	// ErrCodeTaken means the meter code is used (409 code_taken).
	ErrCodeTaken = errors.New("utility: code taken")
	// ErrElementInvalid means the pedestal, resource, equipment or item does not exist in the marina or does not
	// suit the meter (422 element_invalid).
	ErrElementInvalid = errors.New("utility: element invalid")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("utility: version mismatch")
	// ErrInvalidTransition means a reading cannot be confirmed or voided from its status (409 invalid_transition).
	ErrInvalidTransition = errors.New("utility: invalid transition")
)
