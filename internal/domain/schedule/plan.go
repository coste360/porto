package schedule

import (
	"sort"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Limits of a payment policy.
const (
	MaxCount       = 24  // instalments of an interval schedule, fixed dates of a fixed_dates schedule
	MaxEveryMonths = 12  // months between two instalments
	MaxOffsetDays  = 366 // deposit_due_days, due_offset_days, issue_days_before_due, pay_by_offset_days
	MaxOffsetMonth = 24  // first_due_offset_months
	WeightTotal    = 10_000
)

// Validate checks a payment policy without planning it (used when a policy is saved).
func Validate(p PaymentPolicy) error {
	if p.RoundingUnit < 0 || (p.RoundingUnit != 0 && p.RoundingUnit != 1 && p.RoundingUnit != 100 && p.RoundingUnit != 1000) {
		return ErrInvalidPolicy
	}
	if p.DepositDueDays < 0 || p.DepositDueDays > MaxOffsetDays || p.IssueDaysBeforeDue < 0 || p.IssueDaysBeforeDue > MaxOffsetDays {
		return ErrInvalidPolicy
	}
	if d := p.Deposit; d != nil {
		if d.BP < 0 || d.BP > WeightTotal || d.Cents < 0 || (d.BP != 0 && d.Cents != 0) {
			return ErrInvalidPolicy
		}
	}
	if e := p.EarlyPayment; e != nil {
		if e.DiscountBP < 0 || e.DiscountBP > WeightTotal || e.PayByOffsetDays < 0 || e.PayByOffsetDays > MaxOffsetDays {
			return ErrInvalidPolicy
		}
	}
	sc := p.Schedule
	switch sc.Mode {
	case ModeSingle:
		if sc.DueOffsetDays < 0 || sc.DueOffsetDays > MaxOffsetDays {
			return ErrInvalidPolicy
		}
	case ModeInterval:
		if sc.Count < 1 || sc.Count > MaxCount || sc.EveryMonths < 1 || sc.EveryMonths > MaxEveryMonths ||
			sc.FirstDueOffsetMonths < 0 || sc.FirstDueOffsetMonths > MaxOffsetMonth {
			return ErrInvalidPolicy
		}
	case ModeFixedDates:
		if len(sc.Dates) == 0 || len(sc.Dates) > MaxCount {
			return ErrInvalidPolicy
		}
		var sum int64
		for _, d := range sc.Dates {
			if d.BP <= 0 || d.BP > WeightTotal {
				return ErrInvalidPolicy
			}
			if d.Date.IsZero() {
				if _, _, ok := parseMonthDay(d.MonthDay); !ok {
					return ErrInvalidPolicy
				}
			}
			sum += d.BP
		}
		if sum != WeightTotal {
			return ErrInvalidPolicy
		}
	default:
		return ErrInvalidPolicy
	}
	return nil
}

// part is an instalment before numbering.
type part struct {
	due    calendar.Date
	weight int64
	amount money.Cents
}

// Plan returns the instalments of in (empty for a zero total). Σ Amount = Total (INV-31): every amount is ≥ 0,
// the parts after the deposit are floored to the rounding unit and the last one absorbs the remainder. Every
// due date is on or after Today and every issue date is in [Today, DueOn].
func Plan(in Input) ([]Item, error) {
	if in.Total < 0 {
		return nil, ErrNegativeTotal
	}
	if err := Validate(in.Payment); err != nil {
		return nil, err
	}
	if in.StartsOn.IsZero() || in.EndsOn.IsZero() || in.Today.IsZero() || !in.EndsOn.After(in.StartsOn) {
		return nil, ErrInvalidPolicy
	}
	if in.Total == 0 {
		return nil, nil
	}
	p := in.Payment
	unit := p.RoundingUnit
	if unit == 0 {
		unit = 1
	}
	var deposit money.Cents
	if d := p.Deposit; d != nil {
		switch {
		case d.Cents > 0:
			if d.Cents > in.Total {
				return nil, ErrInvalidPolicy
			}
			deposit = d.Cents
		case d.BP > 0:
			deposit = in.Total.PercentBP(d.BP)
		}
	}
	rest := in.Total - deposit

	parts, err := dueParts(in)
	if err != nil {
		return nil, err
	}
	split(parts, rest, unit)
	parts = mergePast(parts, in.Today)

	items := make([]Item, 0, len(parts)+1)
	if deposit > 0 {
		due := in.Today.AddDays(p.DepositDueDays)
		items = append(items, Item{Kind: KindDeposit, IssueOn: in.Today, DueOn: due, Amount: deposit})
	}
	kind := KindInstallment
	if p.Schedule.Mode == ModeSingle && deposit == 0 {
		kind = KindSingle
	}
	for i, pt := range parts {
		// Zero parts are dropped (the sum is unchanged); the last part is kept only when it carries money.
		if pt.amount == 0 && (i < len(parts)-1 || len(items) > 0) {
			continue
		}
		items = append(items, Item{Kind: kind, IssueOn: issueOn(pt.due, p.IssueDaysBeforeDue, in.Today), DueOn: pt.due, Amount: pt.amount})
	}
	for i := range items {
		items[i].Seq = i + 1
	}
	return items, nil
}

// EarlyPaymentPlan returns the early-payment alternative of in, or nil when the policy has none: one single
// instalment due PayByOffsetDays after Today, with the discount (half-even on the total) applied.
func EarlyPaymentPlan(in Input) (*Alternative, error) {
	if in.Total < 0 {
		return nil, ErrNegativeTotal
	}
	if err := Validate(in.Payment); err != nil {
		return nil, err
	}
	e := in.Payment.EarlyPayment
	if e == nil || in.Today.IsZero() {
		return nil, nil
	}
	discount := in.Total.PercentBP(e.DiscountBP)
	alt := &Alternative{Discount: discount, Total: in.Total - discount}
	if alt.Total > 0 {
		alt.Items = []Item{{Seq: 1, Kind: KindSingle, IssueOn: in.Today, DueOn: in.Today.AddDays(e.PayByOffsetDays), Amount: alt.Total}}
	}
	return alt, nil
}

// dueParts returns the due dates and weights of the schedule, sorted by date.
func dueParts(in Input) ([]part, error) {
	sc := in.Payment.Schedule
	switch sc.Mode {
	case ModeSingle:
		return []part{{due: maxDate(in.StartsOn.AddDays(sc.DueOffsetDays), in.Today), weight: 1}}, nil
	case ModeInterval:
		out := make([]part, 0, sc.Count)
		for i := range sc.Count {
			out = append(out, part{due: AddMonths(in.StartsOn, sc.FirstDueOffsetMonths+i*sc.EveryMonths), weight: 1})
		}
		return out, nil
	default: // fixed_dates (Validate checked the mode)
		out := make([]part, 0, len(sc.Dates))
		for _, d := range sc.Dates {
			due := d.Date
			if due.IsZero() {
				m, day, _ := parseMonthDay(d.MonthDay)
				due = rollInto(in.StartsOn, m, day)
			}
			if due.Before(in.StartsOn) || !due.Before(in.EndsOn) {
				return nil, ErrInvalidPolicy
			}
			out = append(out, part{due: due, weight: d.BP})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].due.Before(out[j].due) })
		return out, nil
	}
}

// split assigns rest to the parts by weight: each part floor(rest·w/Σw / unit)·unit, the last the remainder.
func split(parts []part, rest, unit money.Cents) {
	var sumW int64
	for _, p := range parts {
		sumW += p.weight
	}
	var assigned money.Cents
	for i := range parts {
		if i == len(parts)-1 {
			parts[i].amount = rest - assigned
			break
		}
		// rest ≤ 10^15 and weights ≤ 10^4 keep the product inside int64; MulRatChecked would round half-even,
		// the plan floors (§15 D2) so the last part is never negative.
		share := money.Cents(int64(rest) / sumW * parts[i].weight)
		share += money.Cents(int64(rest) % sumW * parts[i].weight / sumW)
		share = share / unit * unit
		parts[i].amount = share
		assigned += share
	}
}

// mergePast moves the amounts of the parts due before today into the first part due on or after today (a part
// due today when every part is past).
func mergePast(parts []part, today calendar.Date) []part {
	var carry money.Cents
	out := make([]part, 0, len(parts))
	for _, p := range parts {
		if p.due.Before(today) {
			carry += p.amount
			continue
		}
		p.amount += carry
		carry = 0
		out = append(out, p)
	}
	if len(out) == 0 {
		return []part{{due: today, amount: carry}}
	}
	return out
}

func issueOn(due calendar.Date, daysBefore int, today calendar.Date) calendar.Date {
	return maxDate(due.AddDays(-daysBefore), today)
}

func maxDate(a, b calendar.Date) calendar.Date {
	if a.Before(b) {
		return b
	}
	return a
}

// rollInto returns the first date with month m and day d on or after from (Feb 29 becomes Feb 28 in common years).
func rollInto(from calendar.Date, m time.Month, d int) calendar.Date {
	for y := from.Year(); ; y++ {
		day := min(d, daysIn(y, m))
		c := calendar.New(y, m, day)
		if !c.Before(from) {
			return c
		}
	}
}

// AddMonths adds n months to d, clamping the day to the end of the target month.
func AddMonths(d calendar.Date, n int) calendar.Date {
	idx := int(d.Month()) - 1 + n
	y := d.Year() + idx/12
	m := time.Month(idx%12 + 1)
	return calendar.New(y, m, min(d.Day(), daysIn(y, m)))
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// parseMonthDay parses "MM-DD".
func parseMonthDay(s string) (time.Month, int, bool) {
	if len(s) != 5 || s[2] != '-' {
		return 0, 0, false
	}
	m := atoi2(s[0:2])
	d := atoi2(s[3:5])
	if m < 1 || m > 12 || d < 1 || d > daysIn(2024, time.Month(m)) {
		return 0, 0, false
	}
	return time.Month(m), d, true
}

func atoi2(s string) int {
	if s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return -1
	}
	return int(s[0]-'0')*10 + int(s[1]-'0')
}
