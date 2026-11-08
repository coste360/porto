package align

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// Alignments of a previous period (m7-plan §5).
const (
	ByDate    = "date"
	ByWeekday = "weekday"
)

// Granularities of a series.
const (
	Day   = "day"
	Week  = "week"
	Month = "month"
)

// AddYears moves d by k years keeping month and day; a Feb 29 that does not exist in the target year becomes
// Feb 28 (never Mar 1).
func AddYears(d calendar.Date, k int) calendar.Date {
	y := d.Year() + k
	day := d.Day()
	if d.Month() == time.February && day == 29 && !isLeap(y) {
		day = 28
	}
	return calendar.New(y, d.Month(), day)
}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// WeekdayShift is the number of days a weekday alignment shifts k years back: 7 × round(365.2425 k / 7) (D19),
// 364 for one year. The rounding is half-even on the exact rational 3652425 k / 70000.
func WeekdayShift(k int) int {
	num := int64(3652425) * int64(k)
	const den = 70000
	q, r := num/den, num%den
	if 2*r > den || (2*r == den && q%2 == 1) {
		q++
	}
	return int(q) * 7
}

// Previous returns the period k years before [from, to]: same calendar dates (date) or shifted by whole weeks
// (weekday). Any other alignment is treated as date.
func Previous(from, to calendar.Date, k int, alignment string) (calendar.Date, calendar.Date) {
	if alignment == ByWeekday {
		s := WeekdayShift(k)
		return from.AddDays(-s), to.AddDays(-s)
	}
	return AddYears(from, -k), AddYears(to, -k)
}

// MapToCurrent maps a day p of the previous period (k years back) onto the current period: AddYears(p, k) for
// date alignment (a previous Feb 29 lands on Feb 28), p + shift for weekday alignment. Never by index.
func MapToCurrent(p calendar.Date, k int, alignment string) calendar.Date {
	if alignment == ByWeekday {
		return p.AddDays(WeekdayShift(k))
	}
	return AddYears(p, k)
}

// DefaultGranularity is day up to 31 days, week up to 120, else month.
func DefaultGranularity(from, to calendar.Date) string {
	n := from.DaysUntil(to) + 1
	switch {
	case n <= 31:
		return Day
	case n <= 120:
		return Week
	default:
		return Month
	}
}

// Bucket returns the first day of the bucket of d: d itself, the Monday of its ISO week, or the first of its
// month.
func Bucket(d calendar.Date, granularity string) calendar.Date {
	switch granularity {
	case Week:
		return d.AddDays(1 - d.ISOWeekday())
	case Month:
		return calendar.New(d.Year(), d.Month(), 1)
	default:
		return d
	}
}

// Buckets returns the buckets of [from, to] in order (each bucket's first day; the first bucket may start
// before from).
func Buckets(from, to calendar.Date, granularity string) []calendar.Date {
	if to.Before(from) {
		return nil
	}
	var out []calendar.Date
	for b := Bucket(from, granularity); !b.After(to); {
		out = append(out, b)
		switch granularity {
		case Week:
			b = b.AddDays(7)
		case Month:
			b = calendar.New(b.Year(), b.Month()+1, 1)
		default:
			b = b.AddDays(1)
		}
	}
	return out
}

// Clamp keeps d inside [from, to].
func Clamp(d, from, to calendar.Date) calendar.Date {
	if d.Before(from) {
		return from
	}
	if d.After(to) {
		return to
	}
	return d
}
