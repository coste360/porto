package fleet

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until FLEET lands the bodies (501).
	ErrNotImplemented = errors.New("rental fleet: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("rental fleet: version mismatch")
	// ErrCodeTaken is a duplicate unit or formula code, or a vessel already in the fleet (409 code_taken).
	ErrCodeTaken = errors.New("rental fleet: code taken")
	// ErrNotRentalItem is an item without revenue class rental where one is required (422 validation_failed on the
	// item field).
	ErrNotRentalItem = errors.New("rental fleet: not a rental item")
	// ErrBandGapLtBuffer is two bands closer than the buffer (422 band_gap_lt_buffer).
	ErrBandGapLtBuffer = errors.New("rental fleet: band gap lower than the buffer")
	// ErrNoPrice is a formula with no applicable price rule (422 no_price; quotes list it per formula).
	ErrNoPrice = errors.New("rental fleet: no price")
)
