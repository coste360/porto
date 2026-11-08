package lifting

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
)

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until PLANNER lands the bodies (501).
	ErrNotImplemented = errors.New("lifting: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("lifting: version mismatch")
	// ErrInvalidTransition means the operation is not allowed from the booking status (409 invalid_transition).
	ErrInvalidTransition = errors.New("lifting: invalid transition")
	// ErrSlotTaken is the sentinel of SlotTakenError (409 equipment_slot_taken).
	ErrSlotTaken = errors.New("lifting: equipment slot taken")
	// ErrOperatorBusy is the sentinel of OperatorBusyError (409 operator_busy).
	ErrOperatorBusy = errors.New("lifting: operator busy")
	// ErrOutOfService means a maintenance task with out_of_service covers the date (409 equipment_out_of_service).
	ErrOutOfService = errors.New("lifting: equipment out of service")
	// ErrCapacityExceeded means the vessel displacement exceeds the equipment capacity (422
	// equipment_capacity_exceeded, never overridable).
	ErrCapacityExceeded = errors.New("lifting: equipment capacity exceeded")
	// ErrOperatorNotEnabled means the employee is not an operator of the equipment (422 operator_not_enabled).
	ErrOperatorNotEnabled = errors.New("lifting: operator not enabled")
	// ErrOutsideWindow means the slots are outside yard_settings.lift_from_slot..lift_to_slot (422
	// outside_lift_window).
	ErrOutsideWindow = errors.New("lifting: outside lift window")
	// ErrNotBookable means the equipment is not bookable or not active (422 validation_failed on equipment_id).
	ErrNotBookable = errors.New("lifting: equipment not bookable")
	// ErrWorkOrderClosed means the booking references a cancelled or invoiced work order (409 work_order_closed).
	ErrWorkOrderClosed = errors.New("lifting: work order closed")
)

// SlotTakenError is 409 equipment_slot_taken {equipment_id, date, slots, booking_ids}: a unique violation of
// equipment_slots rolled the transaction back (INV-17); nothing was written.
type SlotTakenError struct {
	EquipmentID string
	Date        calendar.Date
	Slots       []int
	BookingIDs  []string
}

func (e *SlotTakenError) Error() string { return "lifting: equipment slot taken" }

// Unwrap lets errors.Is(err, ErrSlotTaken) match.
func (e *SlotTakenError) Unwrap() error { return ErrSlotTaken }

// OperatorBusyError is 409 operator_busy {employee_id, date, slots, booking_ids} (unique violation of
// operator_slots).
type OperatorBusyError struct {
	EmployeeID string
	Date       calendar.Date
	Slots      []int
	BookingIDs []string
}

func (e *OperatorBusyError) Error() string { return "lifting: operator busy" }

// Unwrap lets errors.Is(err, ErrOperatorBusy) match.
func (e *OperatorBusyError) Unwrap() error { return ErrOperatorBusy }
