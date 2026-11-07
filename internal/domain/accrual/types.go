package accrual

import (
	"errors"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// ErrInvalidPeriod means a line's period (or the reporting period) is empty or reversed.
var ErrInvalidPeriod = errors.New("accrual: invalid period")

// MaxDays bounds the period of one line (ten years of nights), so that a typo cannot allocate millions of quotas.
const MaxDays = 3660

// Line is an amount earned over the inclusive days [From, To].
type Line struct {
	ID     string
	Amount money.Cents
	From   calendar.Date
	To     calendar.Date // inclusive
}

// Quota is the share of a line on one day.
type Quota struct {
	LineID string
	Day    calendar.Date
	Amount money.Cents
}

// Result is the accrual of the lines inside a reporting period: the quotas of the days inside it and, per line,
// their sum.
type Result struct {
	Quotas  []Quota
	PerLine map[string]money.Cents
	Total   money.Cents
}

// Days returns the number of days of l (0 for an invalid period).
func (l Line) Days() int {
	if l.From.IsZero() || l.To.IsZero() || l.To.Before(l.From) {
		return 0
	}
	return l.From.DaysUntil(l.To) + 1
}

// DayQuota returns the quota of a line on its i-th day (0-based): every day gets amount / days truncated toward
// zero and the last day the remainder, so the quotas of a line sum to its amount exactly (INV-60).
func DayQuota(amount money.Cents, days, i int) money.Cents {
	if days <= 0 || i < 0 || i >= days {
		return 0
	}
	q := int64(amount) / int64(days)
	if i == days-1 {
		return money.Cents(int64(amount) - q*int64(days-1))
	}
	return money.Cents(q)
}

// Within returns the sum of the quotas of l on the days inside [from, to] (inclusive) without building the
// quota list. A period that does not intersect gives 0.
func Within(l Line, from, to calendar.Date) (money.Cents, error) {
	days := l.Days()
	if days == 0 || days > MaxDays {
		return 0, ErrInvalidPeriod
	}
	if to.Before(from) {
		return 0, ErrInvalidPeriod
	}
	lo, hi := l.From, l.To
	if from.After(lo) {
		lo = from
	}
	if to.Before(hi) {
		hi = to
	}
	if hi.Before(lo) {
		return 0, nil
	}
	first := l.From.DaysUntil(lo)
	last := l.From.DaysUntil(hi)
	q := int64(l.Amount) / int64(days)
	n := int64(last - first + 1)
	sum := q * n
	if last == days-1 {
		sum += int64(l.Amount) - q*int64(days) // the remainder on the last day
	}
	return money.Cents(sum), nil
}

// Allocate spreads every line evenly by day (truncated toward zero, remainder on the last day of the line: Σ
// quotas of a line = its amount, INV-60) and keeps the quotas of the days inside [from, to]. Quotas are ordered
// by day, then by line order.
func Allocate(lines []Line, from, to calendar.Date) (Result, error) {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return Result{}, ErrInvalidPeriod
	}
	res := Result{PerLine: make(map[string]money.Cents, len(lines))}
	for _, l := range lines {
		days := l.Days()
		if days == 0 || days > MaxDays {
			return Result{}, ErrInvalidPeriod
		}
		lo, hi := l.From, l.To
		if from.After(lo) {
			lo = from
		}
		if to.Before(hi) {
			hi = to
		}
		if _, ok := res.PerLine[l.ID]; !ok {
			res.PerLine[l.ID] = 0
		}
		if hi.Before(lo) {
			continue
		}
		for i := l.From.DaysUntil(lo); i <= l.From.DaysUntil(hi); i++ {
			a := DayQuota(l.Amount, days, i)
			res.Quotas = append(res.Quotas, Quota{LineID: l.ID, Day: l.From.AddDays(i), Amount: a})
			res.PerLine[l.ID] += a
			res.Total += a
		}
	}
	slices.SortStableFunc(res.Quotas, func(a, b Quota) int { return a.Day.Compare(b.Day) })
	return res, nil
}
