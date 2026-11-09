package slots

import (
	"fmt"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// Formula kinds (rental_formulas.kind).
const (
	KindHourly   = "hourly"
	KindHalfDay  = "half_day"
	KindDay      = "day"
	KindMultiDay = "multi_day"
	KindWeek     = "week"
)

// Defaults of a formula when a field is zero.
const (
	DefaultMinSlots = 2
	DefaultMinDays  = 2
	WeekDays        = 7
	DefaultMaxDays  = 30
	MaxBuffer       = 8
	MaxBands        = 4
)

// FromClock returns the slot of a wall-clock time, rounding the minute down. Out-of-range input is clamped to
// 0..47.
func FromClock(h, m int) int {
	s := h*2 + m/Minutes
	return min(max(s, 0), PerDay-1)
}

// Clock returns the wall-clock time of slot s (48 = 24:00).
func Clock(s int) (h, m int) {
	s = min(max(s, 0), PerDay)
	return s / 2, (s % 2) * Minutes
}

// Label returns "HH:MM" of slot s (48 = "24:00").
func Label(s int) string {
	h, m := Clock(s)
	return fmt.Sprintf("%02d:%02d", h, m)
}

// Instant converts a local (date, slot) to an instant with time.Date in loc (slot 48 is 00:00 of the next date).
func Instant(d calendar.Date, slot int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	h, m := Clock(slot)
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
}

// Up returns the first slot point at or after instant t in loc (slot_up of m11-plan §5.4). A point at the end of a
// date is returned as (date, 48).
func Up(t time.Time, loc *time.Location) Point {
	if loc == nil {
		loc = time.UTC
	}
	l := t.In(loc)
	d := calendar.New(l.Year(), l.Month(), l.Day())
	mins := l.Hour()*60 + l.Minute()
	s := mins / Minutes
	if mins%Minutes != 0 || l.Second() != 0 || l.Nanosecond() != 0 {
		s++
	}
	return Point{Date: d, Slot: s}
}

// Index returns the absolute slot index of a point relative to base (base slot 0 = 0); it orders points.
func Index(base calendar.Date, p Point) int {
	return base.DaysUntil(p.Date)*PerDay + p.Slot
}

// Less reports whether point a is before point b.
func Less(a, b Point) bool {
	if c := a.Date.Compare(b.Date); c != 0 {
		return c < 0
	}
	return a.Slot < b.Slot
}

// Norm returns the canonical form of a point: slot 48 stays on its date (an end point); a negative or larger slot
// is carried to the neighbouring dates.
func Norm(p Point) Point {
	for p.Slot > PerDay {
		p.Slot -= PerDay
		p.Date = p.Date.AddDays(1)
	}
	for p.Slot < 0 {
		p.Slot += PerDay
		p.Date = p.Date.AddDays(-1)
	}
	return p
}

// touchesDST reports whether [from, to) of one date intersects the DST slots 4-5.
func touchesDST(from, to int) bool { return from < DSTTo && to > DSTFrom }

// ValidateSettings checks the open window and the bands (inside the window, excluding DSTFrom..DSTTo, pairwise
// gap >= Buffer: *RangeError CodeBandGapLtBuffer).
func ValidateSettings(s Settings) error {
	if s.OpenFrom < 0 || s.OpenTo > PerDay || s.OpenFrom >= s.OpenTo || touchesDST(s.OpenFrom, s.OpenTo) {
		return &RangeError{Code: CodeOutsideRentalHours, Expected: "open_window"}
	}
	if s.Buffer < 0 || s.Buffer > MaxBuffer {
		return &RangeError{Code: CodeOutsideRentalHours, Expected: "buffer_slots"}
	}
	if len(s.Bands) < 1 || len(s.Bands) > MaxBands {
		return &RangeError{Code: CodeOutsideRentalHours, Expected: "bands"}
	}
	seen := map[string]bool{}
	bands := slices.Clone(s.Bands)
	for _, b := range bands {
		if b.Code == "" || seen[b.Code] {
			return &RangeError{Code: CodeOutsideRentalHours, Expected: "band_code"}
		}
		seen[b.Code] = true
		if b.From >= b.To || b.From < s.OpenFrom || b.To > s.OpenTo || touchesDST(b.From, b.To) {
			return &RangeError{Code: CodeOutsideRentalHours, Kind: KindHalfDay, Expected: b.Code}
		}
	}
	slices.SortFunc(bands, func(a, b Band) int { return a.From - b.From })
	for i := 1; i < len(bands); i++ {
		if bands[i].From-bands[i-1].To < s.Buffer {
			return &RangeError{Code: CodeBandGapLtBuffer, Kind: KindHalfDay, Expected: bands[i-1].Code + "," + bands[i].Code}
		}
	}
	return nil
}

func windowLabel(from, to int) string { return Label(from) + "-" + Label(to) }

func mismatch(kind, expected string) error {
	return &RangeError{Code: CodeFormulaRangeMismatch, Kind: kind, Expected: expected}
}

func outside(kind string, s Settings) error {
	return &RangeError{Code: CodeOutsideRentalHours, Kind: kind, Expected: windowLabel(s.OpenFrom, s.OpenTo)}
}

// WellFormed reports whether r has valid slots and a positive length.
func WellFormed(r Range) bool {
	if r.Start.Date.IsZero() || r.End.Date.IsZero() {
		return false
	}
	if r.Start.Slot < 0 || r.Start.Slot >= PerDay || r.End.Slot < 1 || r.End.Slot > PerDay {
		return false
	}
	return Count(r) > 0
}

// ValidateRange checks a range against a formula and the settings (m11-plan §4.2) and returns its quantity, or a
// *RangeError.
func ValidateRange(f Formula, s Settings, r Range) (Quantity, error) {
	if !WellFormed(r) {
		return Quantity{}, mismatch(f.Kind, "end_after_start")
	}
	sameDate := r.Start.Date.Equal(r.End.Date)
	switch f.Kind {
	case KindHourly:
		if !sameDate {
			return Quantity{}, mismatch(f.Kind, "same_date")
		}
		if r.Start.Slot < s.OpenFrom || r.End.Slot > s.OpenTo {
			return Quantity{}, outside(f.Kind, s)
		}
		n := r.End.Slot - r.Start.Slot
		lo, hi := f.MinSlots, f.MaxSlots
		if lo <= 0 {
			lo = DefaultMinSlots
		}
		if hi <= 0 {
			hi = s.OpenTo - s.OpenFrom
		}
		if n < lo || n > hi {
			return Quantity{}, mismatch(f.Kind, fmt.Sprintf("slots %d..%d", lo, hi))
		}
		return Quantity{Slots: n, Days: 1}, nil
	case KindHalfDay:
		var band *Band
		for i := range s.Bands {
			if s.Bands[i].Code == f.Band {
				band = &s.Bands[i]
			}
		}
		if band == nil {
			return Quantity{}, mismatch(f.Kind, "band "+f.Band)
		}
		if !sameDate || r.Start.Slot != band.From || r.End.Slot != band.To {
			return Quantity{}, mismatch(f.Kind, windowLabel(band.From, band.To))
		}
		return Quantity{Slots: band.To - band.From, Days: 1, Band: band.Code}, nil
	case KindDay:
		if !sameDate || r.Start.Slot != s.OpenFrom || r.End.Slot != s.OpenTo {
			return Quantity{}, mismatch(f.Kind, windowLabel(s.OpenFrom, s.OpenTo))
		}
		return Quantity{Slots: s.OpenTo - s.OpenFrom, Days: 1}, nil
	case KindMultiDay, KindWeek:
		if r.Start.Slot < s.OpenFrom || r.Start.Slot >= s.OpenTo || r.End.Slot <= s.OpenFrom || r.End.Slot > s.OpenTo {
			return Quantity{}, outside(f.Kind, s)
		}
		days := Days(r)
		lo, hi := f.MinDays, f.MaxDays
		lo = max(lo, DefaultMinDays) // a multi-day rental touches at least two dates
		if f.Kind == KindWeek {
			lo = max(lo, WeekDays)
		}
		if hi <= 0 {
			hi = DefaultMaxDays
		}
		if days < lo || days > hi {
			return Quantity{}, mismatch(f.Kind, fmt.Sprintf("days %d..%d", lo, hi))
		}
		return Quantity{Slots: Count(r), Days: days}, nil
	}
	return Quantity{}, mismatch(f.Kind, "kind")
}

// Expand returns the ordered slot keys of r followed by buffer more slots, carrying over midnight. The first
// Count(r) keys are the rental rows, the rest the buffer rows.
func Expand(r Range, buffer int) []Key {
	n := Count(r)
	if n <= 0 {
		return nil
	}
	buffer = max(buffer, 0)
	out := make([]Key, 0, n+buffer)
	d, s := r.Start.Date, r.Start.Slot
	for range n + buffer {
		out = append(out, Key{Date: d, Slot: s})
		s++
		if s == PerDay {
			s, d = 0, d.AddDays(1)
		}
	}
	return out
}

// Count returns the number of slots of r.
func Count(r Range) int {
	if r.Start.Date.IsZero() || r.End.Date.IsZero() {
		return 0
	}
	return max(Index(r.Start.Date, r.End)-r.Start.Slot, 0)
}

// Days returns the calendar dates touched by r.
func Days(r Range) int {
	if Count(r) <= 0 {
		return 0
	}
	return r.Start.Date.DaysUntil(r.End.Date) + 1
}

// After returns the point n slots after p (n >= 0), as an end point (slot 48 stays on its date).
func After(p Point, n int) Point {
	abs := p.Slot + n
	d := p.Date
	for abs > PerDay {
		abs -= PerDay
		d = d.AddDays(1)
	}
	return Point{Date: d, Slot: abs}
}

// KeyEnd returns the exclusive end point of key k (slot 47 ends at (date, 48)).
func KeyEnd(k Key) Point { return Point{Date: k.Date, Slot: k.Slot + 1} }

// AsStart returns the start form of a point: an end point (date, 48) becomes (date+1, 0).
func AsStart(p Point) Point {
	if p.Slot >= PerDay {
		return Norm(Point{Date: p.Date.AddDays(1), Slot: p.Slot - PerDay})
	}
	return p
}

// KeyPoint returns the start point of key k.
func KeyPoint(k Key) Point { return Point(k) }
