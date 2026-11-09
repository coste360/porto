package reservation

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Renter is the walk-in renter of a booking without a customer (encrypted at rest: renter_enc, AAD
// rental-renter:<id>). Never logged, never in events (INV-70).
type Renter struct {
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`
}

// ChargeRef is a charge of a booking.
type ChargeRef struct {
	ChargeID   string
	Kind       string // main, return, adjustment, replaced, deposit_retention
	Status     string
	Total      money.Cents
	Paid       money.Cents
	FiscalMode string
}

// BookingView is a booking with its unit and formula codes, instants, decrypted renter and decoded snapshot.
type BookingView struct {
	store.RentalBooking
	UnitCode        string
	FormulaCode     string
	Kind            string
	StartsAt        time.Time
	EndsAt          time.Time
	Renter          *Renter
	Snapshot        price.Snapshot
	Extras          []store.RentalBookingExtra
	ExtraItems      map[string]string // extra id -> item id of the extras above
	Charges         []ChargeRef
	FiscalMode      string
	LicenceRequired bool
	CheckoutURL     string // the pre-authorisation page while the deposit is pending
	// DepositExpiresAt is the expiry of the pending pre-authorisation intent.
	DepositExpiresAt time.Time
}

// Range returns the booking range.
func (b *BookingView) Range() slots.Range {
	return slots.Range{
		Start: slots.Point{Date: b.StartsOn, Slot: b.StartSlot}, End: slots.Point{Date: b.EndsOn, Slot: b.EndSlot},
	}
}

// ListFilter filters List.
type ListFilter struct {
	From       calendar.Date
	To         calendar.Date
	Status     string
	UnitID     string
	CustomerID string
	Cursor     string
	Limit      int
}

// CreateInput creates a booking: Hold true = held (desk_hold_minutes), false = confirmed.
type CreateInput struct {
	UnitID            string
	FormulaID         string
	SkipperID         string
	Range             slots.Range
	Hold              bool
	CustomerID        string
	Renter            *Renter
	PartySize         *int
	Extras            []fleet.ExtraQty
	InvoiceRequested  bool
	AllowOutsideHours bool
	Note              string
	Channel           string // desk (default) or api (API keys); online is reserved (deferred, §1.2)
}

// Patch reschedules or edits a held or confirmed booking; nil = unchanged. ClearSkipper / ClearCustomer remove the
// reference. KeepPrice keeps the snapshot (marina.configure).
type Patch struct {
	UnitID            *string
	FormulaID         *string
	Range             *slots.Range
	SkipperID         *string
	ClearSkipper      bool
	CustomerID        *string
	ClearCustomer     bool
	Renter            *Renter
	PartySize         *int
	ClearPartySize    bool
	Extras            []fleet.ExtraQty
	ExtrasSet         bool
	InvoiceRequested  *bool
	AllowOutsideHours *bool
	KeepPrice         bool
	Note              *string
}

// Transition actions of transitionRentalBooking.
const (
	ActionConfirm    = "confirm"
	ActionCancel     = "cancel"
	ActionNoShow     = "no_show"
	ActionExtendHold = "extend_hold"
	ActionExtend     = "extend"
)

// TransitionInput is an action on a booking.
type TransitionInput struct {
	Action string
	Reason string
	Until  time.Time    // extend_hold
	End    *slots.Point // extend (checked_out only)
	// FormulaID (extend only, optional) switches the formula the extended range is priced with: a half_day or day
	// booking cannot grow within its own formula (its range is fixed by the band or the open window).
	FormulaID string
}

// Block is a rental block row (handlers name it without importing the store).
type Block = store.RentalBlock

// BlockInput creates a block of a unit or a skipper.
type BlockInput struct {
	UnitID    string
	SkipperID string
	Range     slots.Range
	Reason    string
	Note      string
}

// BlockPatch changes a block; nil = unchanged.
type BlockPatch struct {
	Range  *slots.Range
	Reason *string
	Note   *string
}

// BlockFilter filters ListBlocks.
type BlockFilter struct {
	From      calendar.Date
	To        calendar.Date
	UnitID    string
	SkipperID string
}

// Planner views.
const (
	ViewHours    = "hours"
	ViewHalfDays = "half_days"
	ViewDays     = "days"
)

// PlannerQuery is the window of the planner.
type PlannerQuery struct {
	From     calendar.Date
	To       calendar.Date
	View     string
	Category string
}

// Segment is a run of slots of one booking (or block, or buffer) on a row.
type Segment struct {
	BookingID string
	BlockID   string
	Number    string
	Status    string
	Kind      string // hold, booked, buffer, block
	Reason    string
	From      slots.Point
	To        slots.Point
}

// PlannerRow is a unit or skipper row.
type PlannerRow struct {
	Kind     string // unit, skipper
	ID       string
	Code     string // unit code, or skipper name (rental.manage)
	Category string
	Status   string
	Seats    int
	Segments []Segment
}

// Planner is the planner of a window.
type Planner struct {
	From     calendar.Date
	To       calendar.Date
	View     string
	Settings *fleet.Settings
	Rows     []PlannerRow
}

// AvailabilityQuery searches free units (and skippers) for a range.
type AvailabilityQuery struct {
	Range     slots.Range
	FormulaID string
	Category  string
	Skippers  bool
}

// AvailableUnit is a free unit with the formulas that fit the range.
type AvailableUnit struct {
	UnitID          string
	Code            string
	Category        string
	Seats           int
	LicenceRequired bool
	FormulaIDs      []string
}

// AvailableSkipper is a free skipper.
type AvailableSkipper struct {
	SkipperID   string
	Name        string
	WithinHours bool
}

// Availability is the result of an availability search (a pre-check: the slot keys decide).
type Availability struct {
	Units    []AvailableUnit
	Skippers []AvailableSkipper
}

// DayEntry is a booking of the day view.
type DayEntry struct {
	BookingID     string
	Number        string
	Status        string
	UnitID        string
	UnitCode      string
	SkipperID     string
	RenterName    string
	Range         slots.Range
	StartsAt      time.Time
	EndsAt        time.Time
	Late          bool
	LateMinutes   int
	DepositStatus string
	Unpaid        money.Cents
}

// DayQuery is the date of the day view (zero = today in the marina time zone).
type DayQuery struct {
	Date calendar.Date
}

// Day is the day view of the rental desk and phone.
type Day struct {
	Date       calendar.Date
	Pickups    []DayEntry
	Out        []DayEntry
	ReturnsDue []DayEntry
	Late       []DayEntry
}

// RepriceResult is the new snapshot of a booking and the delta against the previous one.
type RepriceResult struct {
	Booking  *store.RentalBooking
	Snapshot price.Snapshot
	Previous price.Snapshot
	Delta    money.Cents
}
