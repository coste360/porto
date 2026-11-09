package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M11 rental booking tables (internal/migrations/*/00024_rental_bookings.sql, m11-plan §4, §6,
// §9, §15). BOOKING owns the queries of bookings, slots and blocks (rental_bookings.go, rental_slots.go,
// rental_blocks.go); HANDOVER owns rental_booking_charges (rental_charges.go). INV-18: the primary keys of
// rental_slots (unit_id, date, slot) and rental_skipper_slots (skipper_id, date, slot) are the only guarantee
// against double booking (ADR 0003). INV-97: only internal/domain/rental/reservation writes rental_bookings.status,
// the slot rows and hold_expires_at.

// Rental booking enums (m11-plan §4, §6, §9).
const (
	RentalStatusHeld       = "held"
	RentalStatusConfirmed  = "confirmed"
	RentalStatusCheckedOut = "checked_out"
	RentalStatusReturned   = "returned"
	RentalStatusClosed     = "closed"
	RentalStatusCancelled  = "cancelled"
	RentalStatusNoShow     = "no_show"

	RentalChannelDesk   = "desk"
	RentalChannelAPI    = "api"
	RentalChannelOnline = "online"

	RentalSlotHold   = "hold"
	RentalSlotBooked = "booked"
	RentalSlotBuffer = "buffer"
	RentalSlotBlock  = "block"

	RentalBlockMaintenance = "maintenance"
	RentalBlockClosure     = "closure"
	RentalBlockDayOff      = "day_off"
	RentalBlockOther       = "other"

	RentalDepositCash    = "cash"
	RentalDepositCardPOS = "card_pos"
	RentalDepositPreauth = "preauth"
	RentalDepositWaived  = "waived"

	RentalDepositStatusNone              = "none"
	RentalDepositStatusPending           = "pending"
	RentalDepositStatusHeld              = "held"
	RentalDepositStatusAuthorized        = "authorized"
	RentalDepositStatusCapturePending    = "capture_pending"
	RentalDepositStatusReleased          = "released"
	RentalDepositStatusRetained          = "retained"
	RentalDepositStatusPartiallyRetained = "partially_retained"
	RentalDepositStatusWaived            = "waived"

	RentalExtraSourceBooking = "booking"
	RentalExtraSourceReturn  = "return"

	RentalChargeMain       = "main"
	RentalChargeReturn     = "return"
	RentalChargeAdjustment = "adjustment"
	// RentalChargeReplaced is the link of a cancelled main or return charge (replaced by a reprice, cancelled with the
	// booking or by the office); INV-99 keeps one live main and one return link per booking.
	RentalChargeReplaced = "replaced"

	// SeriesRental is the number_sequences series of rental bookings (NL<year>-NNNNNN, D23).
	SeriesRental = "rental"
)

// RentalBooking is a row of rental_bookings: from (starts_on, start_slot) inclusive to (ends_on, end_slot)
// exclusive, 30-minute slots of the local wall-clock day in the marina time zone.
type RentalBooking struct {
	bun.BaseModel `bun:"table:rental_bookings"`

	ID                   string        `bun:"id,pk"`
	MarinaID             string        `bun:"marina_id"`
	Number               int           `bun:"number"`
	Year                 int           `bun:"year"`
	UnitID               string        `bun:"unit_id"`
	FormulaID            string        `bun:"formula_id"`
	SkipperID            string        `bun:"skipper_id,nullzero"`
	StartsOn             calendar.Date `bun:"starts_on"`
	StartSlot            int           `bun:"start_slot"`
	EndsOn               calendar.Date `bun:"ends_on"`
	EndSlot              int           `bun:"end_slot"`
	Status               string        `bun:"status"`
	Channel              string        `bun:"channel"`
	HoldExpiresAt        time.Time     `bun:"hold_expires_at,nullzero"`
	SlotsLostAt          time.Time     `bun:"slots_lost_at,nullzero"`
	CustomerID           string        `bun:"customer_id,nullzero"`
	RenterEnc            string        `bun:"renter_enc,nullzero"` // KeyRing, AAD rental-renter:<id>
	PartySize            *int          `bun:"party_size"`
	InvoiceRequested     bool          `bun:"invoice_requested"`
	AllowOutsideHours    bool          `bun:"allow_outside_hours"`
	PriceSnapshot        JSON          `bun:"price_snapshot"`
	EngineVersion        int           `bun:"engine_version"`
	TotalCents           int64         `bun:"total_cents"`
	DepositMode          string        `bun:"deposit_mode,nullzero"`
	DepositCents         int64         `bun:"deposit_cents"`
	DepositStatus        string        `bun:"deposit_status"`
	DepositPaymentID     string        `bun:"deposit_payment_id,nullzero"`
	DepositIntentID      string        `bun:"deposit_intent_id,nullzero"`
	DepositDecisionCents *int64        `bun:"deposit_decision_cents"`
	DepositRetainedCents int64         `bun:"deposit_retained_cents"`
	DepositWaiveReason   string        `bun:"deposit_waive_reason"`
	CheckedOutAt         time.Time     `bun:"checked_out_at,nullzero"`
	ReturnedAt           time.Time     `bun:"returned_at,nullzero"`
	ClosedAt             time.Time     `bun:"closed_at,nullzero"`
	CancelledAt          time.Time     `bun:"cancelled_at,nullzero"`
	CancelReason         string        `bun:"cancel_reason"`
	NoShowAt             time.Time     `bun:"no_show_at,nullzero"`
	Note                 string        `bun:"note"`
	CreatedByUserID      string        `bun:"created_by_user_id,nullzero"`
	Version              int           `bun:"version"`
	CreatedAt            time.Time     `bun:"created_at"`
	UpdatedAt            time.Time     `bun:"updated_at"`
}

// RentalBookingExtra is a row of rental_booking_extras.
type RentalBookingExtra struct {
	bun.BaseModel `bun:"table:rental_booking_extras"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	BookingID string    `bun:"booking_id"`
	ExtraID   string    `bun:"extra_id"`
	QtyMilli  int64     `bun:"qty_milli"`
	Source    string    `bun:"source"`
	CreatedAt time.Time `bun:"created_at"`
}

// RentalBlock is a row of rental_blocks (exactly one of UnitID and SkipperID is set).
type RentalBlock struct {
	bun.BaseModel `bun:"table:rental_blocks"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	UnitID          string        `bun:"unit_id,nullzero"`
	SkipperID       string        `bun:"skipper_id,nullzero"`
	FromOn          calendar.Date `bun:"from_on"`
	FromSlot        int           `bun:"from_slot"`
	ToOn            calendar.Date `bun:"to_on"`
	ToSlot          int           `bun:"to_slot"`
	Reason          string        `bun:"reason"`
	Note            string        `bun:"note"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// RentalSlot is a row of rental_slots (one occupied 30-minute slot of a unit).
type RentalSlot struct {
	bun.BaseModel `bun:"table:rental_slots"`

	UnitID    string        `bun:"unit_id,pk"`
	Date      calendar.Date `bun:"date,pk"`
	Slot      int           `bun:"slot,pk"`
	MarinaID  string        `bun:"marina_id"`
	BookingID string        `bun:"booking_id,nullzero"`
	BlockID   string        `bun:"block_id,nullzero"`
	Kind      string        `bun:"kind"`
	ExpiresAt time.Time     `bun:"expires_at,nullzero"`
}

// RentalSkipperSlot is a row of rental_skipper_slots (one occupied 30-minute slot of a skipper, no buffer).
type RentalSkipperSlot struct {
	bun.BaseModel `bun:"table:rental_skipper_slots"`

	SkipperID string        `bun:"skipper_id,pk"`
	Date      calendar.Date `bun:"date,pk"`
	Slot      int           `bun:"slot,pk"`
	MarinaID  string        `bun:"marina_id"`
	BookingID string        `bun:"booking_id,nullzero"`
	BlockID   string        `bun:"block_id,nullzero"`
	Kind      string        `bun:"kind"`
	ExpiresAt time.Time     `bun:"expires_at,nullzero"`
}

// RentalBookingCharge is a row of rental_booking_charges.
type RentalBookingCharge struct {
	bun.BaseModel `bun:"table:rental_booking_charges"`

	BookingID string    `bun:"booking_id,pk"`
	ChargeID  string    `bun:"charge_id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Kind      string    `bun:"kind"`
	CreatedAt time.Time `bun:"created_at"`
}
