// Package calendar implements calendar dates (YYYY-MM-DD) and nights, the occupancy unit of Porto (ADR 0002).
//
// A Date has no time of day and no timezone: it is the calendar date in the marina timezone. Converting an
// instant to a Date always requires the marina location (In).
package calendar

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

const layout = "2006-01-02"

var (
	// ErrInvalidDate reports a string that is not a strict YYYY-MM-DD date.
	ErrInvalidDate = errors.New("calendar: invalid date")
	// ErrInvalidRange reports a range whose end is not after its start.
	ErrInvalidRange = errors.New("calendar: invalid range")
	// ErrInvalidLocation reports an empty or unknown IANA timezone name.
	ErrInvalidLocation = errors.New("calendar: invalid location")
)

// Date is a calendar date. The zero value is the "no date" value (IsZero).
type Date struct {
	t time.Time // always 00:00:00 UTC
}

// New returns the date y-m-d, normalizing out-of-range values like time.Date (2026-02-30 → 2026-03-02).
// New(1, time.January, 1) is the zero Date.
func New(y int, m time.Month, d int) Date {
	return Date{t: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// Parse parses a strict YYYY-MM-DD date (exactly 10 characters, year 1000-9999, valid day of month).
// Years before 1000 are rejected: 0001-01-01 would collide with the zero Date.
func Parse(s string) (Date, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return Date{}, fmt.Errorf("%w: %q", ErrInvalidDate, s)
	}
	for i := 0; i < len(s); i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return Date{}, fmt.Errorf("%w: %q", ErrInvalidDate, s)
		}
	}
	t, err := time.Parse(layout, s)
	if err != nil || t.Year() < 1000 {
		return Date{}, fmt.Errorf("%w: %q", ErrInvalidDate, s)
	}
	return Date{t: t}, nil
}

// MustParse is Parse for constants and tests; it panics on error.
func MustParse(s string) Date {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// In returns the calendar date of instant t in loc (the marina timezone). A nil loc means UTC.
func In(t time.Time, loc *time.Location) Date {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return New(y, m, d)
}

// LoadLocation loads an IANA timezone ("Europe/Rome"). Unlike time.LoadLocation it rejects "" and "Local",
// which would silently depend on the host configuration.
func LoadLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLocation, name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLocation, name)
	}
	return loc, nil
}

// String returns YYYY-MM-DD, or "" for the zero Date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format(layout)
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d.t.IsZero() }

// Year returns the year.
func (d Date) Year() int { return d.t.Year() }

// Month returns the month.
func (d Date) Month() time.Month { return d.t.Month() }

// Day returns the day of the month.
func (d Date) Day() int { return d.t.Day() }

// AddDays returns d shifted by n days (n may be negative).
func (d Date) AddDays(n int) Date { return Date{t: d.t.AddDate(0, 0, n)} }

// Before reports whether d is before o.
func (d Date) Before(o Date) bool { return d.t.Before(o.t) }

// After reports whether d is after o.
func (d Date) After(o Date) bool { return d.t.After(o.t) }

// Equal reports whether d and o are the same date.
func (d Date) Equal(o Date) bool { return d.t.Equal(o.t) }

// Compare returns -1, 0 or +1 as d is before, equal to or after o.
func (d Date) Compare(o Date) int { return d.t.Compare(o.t) }

// DaysUntil returns the number of days from d to o (negative when o is before d).
func (d Date) DaysUntil(o Date) int { return int(o.t.Sub(d.t) / (24 * time.Hour)) }

// ISOWeekday returns 1 for Monday through 7 for Sunday.
func (d Date) ISOWeekday() int {
	if wd := d.t.Weekday(); wd != time.Sunday {
		return int(wd)
	}
	return 7
}

// Time returns the date at 00:00 in loc (nil means UTC). Use it to build instants such as check-in times.
func (d Date) Time(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// Range returns every date from from to to, both inclusive. It returns nil when to is before from.
func Range(from, to Date) []Date {
	if to.Before(from) {
		return nil
	}
	out := make([]Date, 0, from.DaysUntil(to)+1)
	for d := from; !d.After(to); d = d.AddDays(1) {
		out = append(out, d)
	}
	return out
}

// Nights is an inclusive range of nights. Night D is the night between D and D+1 (ADR 0002).
type Nights struct {
	First, Last Date
}

// NightsBetween returns the nights of a stay arriving on arrival and departing on departure: the first night
// is arrival and the last is departure-1. departure must be after arrival.
func NightsBetween(arrival, departure Date) (Nights, error) {
	if arrival.IsZero() || departure.IsZero() || !departure.After(arrival) {
		return Nights{}, fmt.Errorf("%w: departure %s must be after arrival %s", ErrInvalidRange, departure, arrival)
	}
	return Nights{First: arrival, Last: departure.AddDays(-1)}, nil
}

// Count returns the number of nights.
func (n Nights) Count() int { return n.First.DaysUntil(n.Last) + 1 }

// Contains reports whether night d belongs to n.
func (n Nights) Contains(d Date) bool { return !d.Before(n.First) && !d.After(n.Last) }

// Each returns every night in n.
func (n Nights) Each() []Date { return Range(n.First, n.Last) }

// Overlaps reports whether n and o share at least one night.
func (n Nights) Overlaps(o Nights) bool { return !n.Last.Before(o.First) && !o.Last.Before(n.First) }

// Departure returns the departure date (the day after the last night).
func (n Nights) Departure() Date { return n.Last.AddDays(1) }

// MarshalText implements encoding.TextMarshaler (empty for the zero Date).
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler; empty input yields the zero Date.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalJSON encodes the date as a "YYYY-MM-DD" string, or null for the zero Date.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON accepts a "YYYY-MM-DD" string or null.
func (d *Date) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*d = Date{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("%w: %s", ErrInvalidDate, s)
	}
	v, err := Parse(s[1 : len(s)-1])
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Value implements driver.Valuer: "YYYY-MM-DD" text (SQLite TEXT; PostgreSQL casts it to date), NULL for zero.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.String(), nil
}

// Scan implements sql.Scanner for TEXT (SQLite) and date/time.Time (PostgreSQL) columns.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		// Drivers return DATE columns as midnight in UTC (or in the session zone): keep the wall date.
		y, m, day := v.Date()
		*d = New(y, m, day)
		return nil
	case string:
		return d.scanString(v)
	case []byte:
		return d.scanString(string(v))
	default:
		return fmt.Errorf("%w: cannot scan %T", ErrInvalidDate, src)
	}
}

func (d *Date) scanString(s string) error {
	// Tolerate a timestamp rendering of a date ("2026-07-01T00:00:00Z", "2026-07-01 00:00:00").
	if len(s) > 10 && (s[10] == 'T' || s[10] == ' ') {
		s = s[:10]
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
