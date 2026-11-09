package slots

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
)

// Slot geometry.
const (
	PerDay  = 48 // slots per local date
	Minutes = 30 // minutes per slot
	// DSTFrom and DSTTo bound the slots (02:00-03:00) that the open window and the bands must exclude.
	DSTFrom = 4
	DSTTo   = 6
)

// Point is a (date, slot) point; as an end it is exclusive and Slot may be 48.
type Point struct {
	Date calendar.Date
	Slot int
}

// Range is [Start, End) in slots.
type Range struct {
	Start Point
	End   Point
}

// Key is one slot row key (date, slot 0..47).
type Key struct {
	Date calendar.Date
	Slot int
}

// Band is a half-day band [From, To).
type Band struct {
	Code string
	From int
	To   int
}

// Settings are the time settings of the marina (rental_settings).
type Settings struct {
	OpenFrom int
	OpenTo   int
	Bands    []Band
	Buffer   int
}

// Formula is the time part of a rental formula.
type Formula struct {
	Kind     string // hourly, half_day, day, multi_day, week
	Band     string // half_day only
	MinSlots int
	MaxSlots int // 0 = the open window
	MinDays  int
	MaxDays  int
}

// Quantity is the priced quantity of a valid range: Slots (hourly), Days (dates touched; 1 for hourly, half day
// and day) and the band of a half day.
type Quantity struct {
	Slots int
	Days  int
	Band  string
}

// Range error codes (problem slugs).
const (
	CodeFormulaRangeMismatch = "formula_range_mismatch"
	CodeOutsideRentalHours   = "outside_rental_hours"
	CodeBandGapLtBuffer      = "band_gap_lt_buffer"
)

// RangeError is a range that does not fit the formula or the open window (422 with Code, Kind and Expected).
type RangeError struct {
	Code     string
	Kind     string
	Expected string
}

func (e *RangeError) Error() string { return "slots: " + e.Code }

// ErrNotImplemented is returned by the CONTRACT skeleton until BOOKING lands the bodies.
var ErrNotImplemented = errors.New("slots: not implemented")
