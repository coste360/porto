package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M10 lifting tables (internal/migrations/*/00022_equipment_slots.sql, m10-plan §11, §15). Owned by
// the PLANNER builder after the contract phase. INV-17: the primary keys of equipment_slots and operator_slots are
// the only guarantee against double booking (ADR 0003).

// Lifting booking enums (m10-plan §11).
const (
	LiftPurposeHaulOut = "haul_out"
	LiftPurposeLaunch  = "launch"
	LiftPurposeMove    = "move"
	LiftPurposeOther   = "other"

	LiftBookingRequested = "requested"
	LiftBookingBooked    = "booked"
	LiftBookingDone      = "done"
	LiftBookingCancelled = "cancelled"

	LiftSourceOffice    = "office"
	LiftSourceWorkOrder = "work_order"
	LiftSourceStorage   = "storage"
	LiftSourceMove      = "move"
)

// EquipmentOperator is a row of equipment_operators.
type EquipmentOperator struct {
	bun.BaseModel `bun:"table:equipment_operators"`

	EquipmentID string `bun:"equipment_id,pk"`
	EmployeeID  string `bun:"employee_id,pk"`
	MarinaID    string `bun:"marina_id"`
}

// EquipmentBooking is a row of equipment_bookings (from_slot..to_slot exclusive, 15-minute slots of the day).
type EquipmentBooking struct {
	bun.BaseModel `bun:"table:equipment_bookings"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	EquipmentID        string        `bun:"equipment_id,nullzero"`
	Date               calendar.Date `bun:"date,nullzero"`
	FromSlot           *int          `bun:"from_slot"`
	ToSlot             *int          `bun:"to_slot"`
	Purpose            string        `bun:"purpose"`
	VesselID           string        `bun:"vessel_id,nullzero"`
	WorkOrderID        string        `bun:"work_order_id,nullzero"`
	ContractID         string        `bun:"contract_id,nullzero"`
	StorageMovementID  string        `bun:"storage_movement_id,nullzero"`
	AllocationID       string        `bun:"allocation_id,nullzero"`
	OperatorEmployeeID string        `bun:"operator_employee_id,nullzero"`
	Status             string        `bun:"status"`
	Source             string        `bun:"source"`
	Note               string        `bun:"note"`
	DoneAt             time.Time     `bun:"done_at,nullzero"`
	CancelledAt        time.Time     `bun:"cancelled_at,nullzero"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// EquipmentSlot is a row of equipment_slots (one booked 15-minute slot of a machine).
type EquipmentSlot struct {
	bun.BaseModel `bun:"table:equipment_slots"`

	EquipmentID string        `bun:"equipment_id,pk"`
	Date        calendar.Date `bun:"date,pk"`
	Slot        int           `bun:"slot,pk"`
	MarinaID    string        `bun:"marina_id"`
	BookingID   string        `bun:"booking_id"`
}

// OperatorSlot is a row of operator_slots (one booked 15-minute slot of an operator).
type OperatorSlot struct {
	bun.BaseModel `bun:"table:operator_slots"`

	EmployeeID string        `bun:"employee_id,pk"`
	Date       calendar.Date `bun:"date,pk"`
	Slot       int           `bun:"slot,pk"`
	MarinaID   string        `bun:"marina_id"`
	BookingID  string        `bun:"booking_id"`
}
