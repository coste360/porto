package lifting

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/lifting/slots"
	"github.com/coste360/porto/internal/store"
)

// SinkName is the name of the in-process outbox sink that turns occupancy.moved (water <-> yard/shed) into
// requested bookings when yard_settings.lift_request_on_move is on (m10-plan §11, D30).
const SinkName = "lifting.move_requests"

// Booking is a booking with the vessel name.
type Booking = store.BookingView

// BookingInput creates a request (RequestOnly, or no equipment/date/window) or a booking.
type BookingInput struct {
	EquipmentID        string
	Date               calendar.Date
	FromSlot, ToSlot   *int
	Purpose            string
	VesselID           string
	WorkOrderID        string
	ContractID         string
	StorageMovementID  string
	OperatorEmployeeID string
	RequestOnly        bool
	Note               string
}

// BookingPatch books a request or reschedules a booking; nil fields are kept.
type BookingPatch struct {
	EquipmentID        *string
	Date               *calendar.Date
	FromSlot, ToSlot   *int
	OperatorEmployeeID *string
	ClearOperator      bool
	Note               *string
}

// DayEquipment is a bookable machine on the day calendar.
type DayEquipment struct {
	Row          store.Equipment
	OutOfService bool
}

// DayView is the lifting day calendar (or the bookings of a work order when filtered by order).
type DayView struct {
	Date      calendar.Date
	Window    slots.Window
	Equipment []DayEquipment
	Bookings  []store.BookingView
	Requests  []store.BookingView
}

// DayFilter filters the day calendar.
type DayFilter struct {
	Date        calendar.Date // zero = today
	EquipmentID string
	Status      string
	WorkOrderID string
}
