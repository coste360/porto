// Package states is the pure transition table of rental bookings (spec 19.3, m11-plan §6, INV-97). It is data:
// internal/domain/rental/reservation is the only writer of rental_bookings.status and consults Next under the
// booking lock. CONTRACT fills the table; BOOKING tests it (TestRentalStates_Table).
package states

// Status is a rental booking status (rental_bookings.status).
type Status string

// Statuses.
const (
	Held       Status = "held"
	Confirmed  Status = "confirmed"
	CheckedOut Status = "checked_out"
	Returned   Status = "returned"
	Closed     Status = "closed"
	Cancelled  Status = "cancelled"
	NoShow     Status = "no_show"
)

// Action is an operation on a booking.
type Action string

// Actions (m11-plan §6). CreateHold and CreateConfirm start from None.
const (
	CreateHold    Action = "create_hold"
	CreateConfirm Action = "create_confirm"
	Confirm       Action = "confirm"
	ExtendHold    Action = "extend_hold"
	Reschedule    Action = "reschedule"
	Extend        Action = "extend"
	Cancel        Action = "cancel"
	HoldExpire    Action = "hold_expire"
	MarkNoShow    Action = "no_show"
	CheckOut      Action = "check_out"
	Return        Action = "return"
	Close         Action = "close"
)

// None is the status before creation.
const None Status = ""

// Transition is a row of the table.
type Transition struct {
	From   Status
	Action Action
	To     Status
}

var table = []Transition{
	{None, CreateHold, Held},
	{None, CreateConfirm, Confirmed},
	{Held, Confirm, Confirmed},
	{Held, ExtendHold, Held},
	{Held, Reschedule, Held},
	{Confirmed, Reschedule, Confirmed},
	{CheckedOut, Extend, CheckedOut},
	{Held, Cancel, Cancelled},
	{Confirmed, Cancel, Cancelled},
	{Held, HoldExpire, Cancelled},
	{Confirmed, MarkNoShow, NoShow},
	{Confirmed, CheckOut, CheckedOut},
	{CheckedOut, Return, Returned},
	{Returned, Close, Closed},
}

// Table returns a copy of the transition table.
func Table() []Transition { return append([]Transition(nil), table...) }

// Next returns the status after action from, and false when the transition is not allowed (409
// invalid_transition).
func Next(from Status, a Action) (Status, bool) {
	for _, t := range table {
		if t.From == from && t.Action == a {
			return t.To, true
		}
	}
	return "", false
}

// Terminal reports whether no action leaves s (closed, cancelled, no_show).
func Terminal(s Status) bool { return s == Closed || s == Cancelled || s == NoShow }

// Occupies reports whether a booking in s holds slot rows (held, confirmed, checked_out; returned and closed keep
// their rows as history, D12).
func Occupies(s Status) bool {
	switch s {
	case Held, Confirmed, CheckedOut, Returned, Closed:
		return true
	}
	return false
}
