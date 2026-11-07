package pricing

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// daysIn returns the number of days of month m of year y.
func daysIn(y int, m time.Month) int {
	// Day 0 of the next month is the last day of m.
	return calendar.New(y, m+1, 0).Day()
}

// AddMonths returns d shifted by k calendar months with end-of-month clamping (m2-plan §12 D6):
// Jan 31 + 1 month = Feb 28 (Feb 29 in leap years), Mar 31 + 1 month = Apr 30. k may be negative.
func AddMonths(d calendar.Date, k int) calendar.Date {
	total := d.Year()*12 + int(d.Month()) - 1 + k
	y, m := total/12, time.Month(total%12+1)
	if total%12 < 0 {
		y, m = (total-11)/12, time.Month((total%12+12)%12+1)
	}
	day := d.Day()
	if last := daysIn(y, m); day > last {
		day = last
	}
	return calendar.New(y, m, day)
}

// AddYears returns d shifted by k years; Feb 29 becomes Feb 28 in a non-leap target year.
func AddYears(d calendar.Date, k int) calendar.Date { return AddMonths(d, 12*k) }

// dateMax returns the later of two dates.
func dateMax(a, b calendar.Date) calendar.Date {
	if a.After(b) {
		return a
	}
	return b
}

// inRange reports whether night d lies in the inclusive range r.
func inRange(d calendar.Date, r DateRange) bool { return !d.Before(r.From) && !d.After(r.To) }
