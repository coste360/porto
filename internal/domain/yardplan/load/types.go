// Package load is the pure workload model of the yard planner (m10-plan §6.3): capacity from the weekly calendar
// minus absences, assigned and actual hours, load in basis points, aggregates as ratios of sums (never means of
// ratios, D21), bay load without capacity (D22) and the at-risk rule. No store, no clock, no randomness, no float.
package load

import (
	"errors"
	"sort"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

// ErrNotImplemented is kept for the contract skeleton callers; nothing returns it any more.
var ErrNotImplemented = errors.New("yardplan/load: not implemented")

// ErrInvalid reports an input outside its domain (negative hours, calendar minutes out of range).
var ErrInvalid = errors.New("yardplan/load: invalid input")

// MaxDayMinutes is the longest working day of a calendar (16 h).
const MaxDayMinutes = 960

// Absence is a whole-day (Minutes nil) or partial absence, To inclusive.
type Absence struct {
	From, To calendar.Date
	Minutes  *int
}

// Employee is what the load model needs of an employee.
type Employee struct {
	ID             string
	Department     string
	Qualifications []string
	WeekMinutes    [7]int // ISO weekday 1..7 at index 0..6
	Absences       []Absence
}

// Holds reports whether e holds qualification q ("" = any employee qualifies).
func (e Employee) Holds(q string) bool {
	if q == "" {
		return true
	}
	for _, x := range e.Qualifications {
		if x == q {
			return true
		}
	}
	return false
}

// Assignment is planned hours of an employee on a day for a task of an order.
type Assignment struct {
	EmployeeID  string
	TaskID      string
	WorkOrderID string
	Bay         string // allocation resource of the order, "" = no position ("—")
	Date        calendar.Date
	Hours       qty.Milli
}

// Cell is the load of one employee (or group) on one day or period.
type Cell struct {
	Capacity qty.Milli
	Assigned qty.Milli
	Actual   qty.Milli
	LoadBP   *int64 // nil when capacity is 0
	Overload bool   // assigned > capacity
}

// divHalfEven returns n/d rounded half-even for n >= 0, d > 0.
func divHalfEven(n, d int64) int64 {
	q, r := n/d, n%d
	if 2*r > d || (2*r == d && q%2 == 1) {
		q++
	}
	return q
}

// AbsenceMinutes returns the minutes of absence of e on d (calendar minutes for a whole-day absence).
func AbsenceMinutes(e Employee, d calendar.Date) int {
	cal := e.WeekMinutes[d.ISOWeekday()-1]
	total := 0
	for _, a := range e.Absences {
		if d.Before(a.From) || d.After(a.To) {
			continue
		}
		if a.Minutes == nil {
			return cal
		}
		total += *a.Minutes
	}
	if total > cal {
		return cal
	}
	return total
}

// CapacityMinutes returns calendar minutes minus absence minutes of e on d, >= 0.
func CapacityMinutes(e Employee, d calendar.Date) int {
	cal := e.WeekMinutes[d.ISOWeekday()-1]
	if cal < 0 {
		return 0
	}
	if cal > MaxDayMinutes {
		cal = MaxDayMinutes
	}
	c := cal - AbsenceMinutes(e, d)
	if c < 0 {
		return 0
	}
	return c
}

// Capacity returns the capacity of e on d in hours milli: calendar minutes minus absence minutes, >= 0.
func Capacity(e Employee, d calendar.Date) (qty.Milli, error) {
	return qty.FromFraction(int64(CapacityMinutes(e, d)), 60)
}

// BP returns assigned × 10000 / capacity half-even, nil when capacity is 0.
func BP(assigned, capacity qty.Milli) *int64 {
	if capacity <= 0 {
		return nil
	}
	if assigned < 0 {
		assigned = 0
	}
	v := divHalfEven(int64(assigned)*10000, int64(capacity))
	return &v
}

// NewCell builds a cell from capacity, assigned and actual hours.
func NewCell(capacity, assigned, actual qty.Milli) Cell {
	return Cell{
		Capacity: capacity, Assigned: assigned, Actual: actual, LoadBP: BP(assigned, capacity), Overload: assigned > capacity,
	}
}

// Aggregate sums cells as a ratio of sums (never a mean of ratios).
func Aggregate(cells []Cell) (Cell, error) {
	var c, a, x qty.Milli
	var err error
	for _, cell := range cells {
		if cell.Capacity < 0 || cell.Assigned < 0 || cell.Actual < 0 {
			return Cell{}, ErrInvalid
		}
		if c, err = c.Add(cell.Capacity); err != nil {
			return Cell{}, err
		}
		if a, err = a.Add(cell.Assigned); err != nil {
			return Cell{}, err
		}
		if x, err = x.Add(cell.Actual); err != nil {
			return Cell{}, err
		}
	}
	return NewCell(c, a, x), nil
}

// Period groupings of the load view.
const (
	PeriodDay   = "day"
	PeriodWeek  = "week"
	PeriodMonth = "month"
)

// PeriodStart returns the first day of the period of d: d itself, the ISO Monday of its week or the 1st of its month.
func PeriodStart(d calendar.Date, period string) calendar.Date {
	switch period {
	case PeriodWeek:
		return d.AddDays(1 - d.ISOWeekday())
	case PeriodMonth:
		return calendar.New(d.Year(), d.Month(), 1)
	default:
		return d
	}
}

// WeekOf returns the Monday..Sunday week containing d.
func WeekOf(d calendar.Date) (from, to calendar.Date) {
	from = PeriodStart(d, PeriodWeek)
	return from, from.AddDays(6)
}

// MonthOf returns the first and last day of the month of d.
func MonthOf(d calendar.Date) (from, to calendar.Date) {
	from = calendar.New(d.Year(), d.Month(), 1)
	next := calendar.New(d.Year(), d.Month()+1, 1)
	if d.Month() == time.December {
		next = calendar.New(d.Year()+1, time.January, 1)
	}
	return from, next.AddDays(-1)
}

// AssignedBy sums the hours of assignments per key and date (key from keyOf, e.g. employee or bay).
func AssignedBy(as []Assignment, keyOf func(Assignment) string) map[string]map[calendar.Date]qty.Milli {
	out := map[string]map[calendar.Date]qty.Milli{}
	for _, a := range as {
		k := keyOf(a)
		m := out[k]
		if m == nil {
			m = map[calendar.Date]qty.Milli{}
			out[k] = m
		}
		m[a.Date] += a.Hours
	}
	return out
}

// RiskTask is an open task of an order for the at-risk rule.
type RiskTask struct {
	QualificationID string
	EstHours        qty.Milli
	ProgressBP      int64
}

// Remaining is est × (10000 − progress) / 10000 half-even (0 for a finished task).
func (t RiskTask) Remaining() qty.Milli {
	p := t.ProgressBP
	if p < 0 {
		p = 0
	}
	if p >= 10000 || t.EstHours <= 0 {
		return 0
	}
	return qty.Milli(divHalfEven(int64(t.EstHours)*(10000-p), 10000))
}

// Risk is the at-risk verdict of an order for one qualification.
type Risk struct {
	QualificationID string
	Remaining       qty.Milli
	Free            qty.Milli
}

// RiskInput is one order for the at-risk rule.
type RiskInput struct {
	WorkOrderID string
	Today       calendar.Date
	Delivery    calendar.Date // zero = no planned delivery (never late, no capacity check)
	Completed   bool          // completed, delivered or invoiced
	Tasks       []RiskTask    // open tasks (todo, in_progress)
	Employees   []Employee
	Assignments []Assignment // every assignment of the period today..delivery (all orders)
}

// Verdict of AtRisk.
type Verdict struct {
	Late  bool
	Risks []Risk // qualifications whose remaining hours exceed the free capacity, by qualification id
}

// AtRisk reports whether an order is late (delivery before today and not completed) and, per qualification of its
// open tasks, whether the remaining hours exceed the free capacity of the qualified employees from today to the
// delivery: free = Σ days Σ qualified employees max(capacity − assigned to other orders, 0).
func AtRisk(in RiskInput) (Verdict, error) {
	var v Verdict
	if in.Delivery.IsZero() || in.Completed {
		return v, nil
	}
	v.Late = in.Delivery.Before(in.Today)
	remaining := map[string]qty.Milli{}
	for _, t := range in.Tasks {
		if t.EstHours < 0 {
			return Verdict{}, ErrInvalid
		}
		if r := t.Remaining(); r > 0 {
			remaining[t.QualificationID] += r
		}
	}
	if len(remaining) == 0 {
		return v, nil
	}
	other := AssignedBy(in.Assignments, func(a Assignment) string {
		if a.WorkOrderID == in.WorkOrderID {
			return ""
		}
		return a.EmployeeID
	})
	days := calendar.Range(in.Today, in.Delivery)
	quals := make([]string, 0, len(remaining))
	for q := range remaining {
		quals = append(quals, q)
	}
	sort.Strings(quals)
	for _, q := range quals {
		var free qty.Milli
		for _, e := range in.Employees {
			if !e.Holds(q) {
				continue
			}
			for _, d := range days {
				c, err := Capacity(e, d)
				if err != nil {
					return Verdict{}, err
				}
				if f := c - other[e.ID][d]; f > 0 {
					free += f
				}
			}
		}
		if remaining[q] > free {
			v.Risks = append(v.Risks, Risk{QualificationID: q, Remaining: remaining[q], Free: free})
		}
	}
	return v, nil
}
