package reservation

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
)

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until BOOKING lands the bodies (501).
	ErrNotImplemented = errors.New("rental reservation: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("rental reservation: version mismatch")
	// ErrInvalidTransition means the action is not allowed from the booking status (409 invalid_transition).
	ErrInvalidTransition = errors.New("rental reservation: invalid transition")
	// ErrSlotTaken is the sentinel of a unit SlotConflict (409 rental_slot_taken).
	ErrSlotTaken = errors.New("rental reservation: rental slot taken")
	// ErrSkipperBusy is the sentinel of a skipper SlotConflict (409 skipper_busy).
	ErrSkipperBusy = errors.New("rental reservation: skipper busy")
	// ErrHoldExpired is a held booking whose rows were reclaimed and cannot be re-allocated (409
	// rental_hold_expired).
	ErrHoldExpired = errors.New("rental reservation: hold expired")
	// ErrUnitNotBookable is a unit in maintenance or retired for a new booking or a reschedule (422
	// unit_not_bookable; an application rule, not the occupancy guarantee).
	ErrUnitNotBookable = errors.New("rental reservation: unit not bookable")
	// ErrSkipperRequired is a formula with skipper required and no skipper (422 skipper_required).
	ErrSkipperRequired = errors.New("rental reservation: skipper required")
	// ErrSkipperUnavailable is a skipper outside his weekly hours without allow_outside_hours (422
	// skipper_unavailable).
	ErrSkipperUnavailable = errors.New("rental reservation: skipper unavailable")
	// ErrSkipperLicenceExpired is a skipper whose licence expires before the end date (422 skipper_licence_expired).
	ErrSkipperLicenceExpired = errors.New("rental reservation: skipper licence expired")
	// ErrCustomerRequired is an invoice request without a customer (422 customer_required).
	ErrCustomerRequired = errors.New("rental reservation: customer required")
)

// Conflict targets.
const (
	TargetUnit    = "unit"
	TargetSkipper = "skipper"
)

// SlotConflict is 409 rental_slot_taken (target unit) or skipper_busy (target skipper) {target, date, slots,
// booking_ids, block_ids}: the insert-and-count of the slot rows came short (INV-18); the caller's transaction
// rolls back everything. Details carry ids, dates and slots only (INV-70).
type SlotConflict struct {
	Target     string
	ResourceID string
	Date       calendar.Date
	Slots      []int
	BookingIDs []string
	BlockIDs   []string
}

func (e *SlotConflict) Error() string {
	if e.Target == TargetSkipper {
		return "rental reservation: skipper busy"
	}
	return "rental reservation: rental slot taken"
}

// Unwrap lets errors.Is match ErrSlotTaken or ErrSkipperBusy.
func (e *SlotConflict) Unwrap() error {
	if e.Target == TargetSkipper {
		return ErrSkipperBusy
	}
	return ErrSlotTaken
}
