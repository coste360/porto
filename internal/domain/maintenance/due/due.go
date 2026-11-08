package due

import (
	"strconv"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

// Periodicity kinds.
const (
	KindMonths    = "months"
	KindCycles    = "cycles"
	KindFixedDate = "fixed_date"
)

// MaxEveryMonths bounds a months periodicity (ten years).
const MaxEveryMonths = 120

// Periodicity is the periodicity of a plan.
type Periodicity struct {
	Kind     string
	Every    int64          // months, or meter units in milli for cycles
	MonthDay string         // MM-DD for fixed_date
	StartsOn *calendar.Date // first reference date when never done (default: the plan creation date)
}

// State is what is known of a target: the last done date and the meter values (cycles only).
type State struct {
	LastDone      *calendar.Date
	LastCycles    qty.Milli // meter value at the last done
	CurrentCycles qty.Milli // latest meter value
}

// Valid reports whether the periodicity is well formed.
func (p Periodicity) Valid() bool {
	switch p.Kind {
	case KindMonths:
		return p.Every >= 1 && p.Every <= MaxEveryMonths
	case KindCycles:
		return p.Every > 0
	case KindFixedDate:
		_, _, ok := ParseMonthDay(p.MonthDay)
		return ok
	}
	return false
}

// ParseMonthDay parses MM-DD (29 February is accepted).
func ParseMonthDay(s string) (time.Month, int, bool) {
	if len(s) != 5 || s[2] != '-' {
		return 0, 0, false
	}
	m, err1 := strconv.Atoi(s[:2])
	d, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || m < 1 || m > 12 || d < 1 {
		return 0, 0, false
	}
	if d > daysIn(2024, time.Month(m)) { // a leap year: 02-29 is a valid month day
		return 0, 0, false
	}
	return time.Month(m), d, true
}

func daysIn(year int, m time.Month) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// AddMonths adds n months to d, clamping the day to the end of the target month (31 January + 1 = 28/29 February).
func AddMonths(d calendar.Date, n int) calendar.Date {
	total := int(d.Month()) - 1 + n
	y := d.Year() + total/12
	m := total % 12
	if m < 0 {
		m += 12
		y--
	}
	month := time.Month(m + 1)
	return calendar.New(y, month, min(d.Day(), daysIn(y, month)))
}

// occurrence returns MM-DD in year y (29 February falls back to 28 February in common years).
func occurrence(y int, m time.Month, day int) calendar.Date {
	return calendar.New(y, m, min(day, daysIn(y, m)))
}

// reference is the date the periodicity counts from: the day after the last done, else the plan start, else from.
func reference(p Periodicity, s State, from calendar.Date) calendar.Date {
	switch {
	case s.LastDone != nil:
		return *s.LastDone
	case p.StartsOn != nil:
		return *p.StartsOn
	default:
		return from
	}
}

// Next returns the due date of the target; ok is false when not computable (cycles not reached yet, or an invalid
// periodicity). The result never depends on from when the target was done or the plan has a start, so the job
// computes the same date every hour (tasks are unique per due date):
//   - months: last done (or start, or from) + every months, end-of-month clamp; it may be in the past (overdue);
//   - fixed_date: the first MM-DD after the last done (or on/after the start, or from);
//   - cycles: from, as soon as current − last ≥ every.
func Next(p Periodicity, s State, from calendar.Date) (calendar.Date, bool) {
	if !p.Valid() {
		return calendar.Date{}, false
	}
	switch p.Kind {
	case KindMonths:
		return AddMonths(reference(p, s, from), int(p.Every)), true
	case KindFixedDate:
		m, day, _ := ParseMonthDay(p.MonthDay)
		ref := reference(p, s, from)
		if s.LastDone != nil {
			ref = ref.AddDays(1)
		}
		d := occurrence(ref.Year(), m, day)
		if d.Before(ref) {
			d = occurrence(ref.Year()+1, m, day)
		}
		return d, true
	case KindCycles:
		advanced, err := s.CurrentCycles.Sub(s.LastCycles)
		if err != nil || int64(advanced) < p.Every {
			return calendar.Date{}, false
		}
		if s.LastDone != nil && from.Before(*s.LastDone) {
			return *s.LastDone, true
		}
		return from, true
	}
	return calendar.Date{}, false
}
