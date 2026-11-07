package schedule

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Errors of the planner.
var (
	// ErrInvalidPolicy means the payment policy cannot produce a plan (weights not summing to 10 000, a fixed
	// deposit above the total, no dates in the period, counts out of range).
	ErrInvalidPolicy = errors.New("schedule: invalid payment policy")
	// ErrNegativeTotal means the total is below zero (adjustments are M5 charges).
	ErrNegativeTotal = errors.New("schedule: negative total")
)

// Schedule modes.
const (
	ModeFixedDates = "fixed_dates"
	ModeInterval   = "interval"
	ModeSingle     = "single"
)

// Item kinds.
const (
	KindDeposit     = "deposit"
	KindInstallment = "installment"
	KindSingle      = "single"
)

// Deposit is the deposit at signature: a share in basis points (half-even) or a fixed amount. Zero value = none.
type Deposit struct {
	BP    int64       // 0..10 000; used when Cents is 0
	Cents money.Cents // fixed amount, at most the total
}

// FixedDue is one due date of a fixed_dates schedule. MonthDay is "MM-DD" (rolled into the period) unless Date
// is set.
type FixedDue struct {
	MonthDay string
	Date     calendar.Date
	BP       int64 // weight of the instalment, the weights sum to 10 000
}

// Schedule is how the amount after the deposit is split.
type Schedule struct {
	Mode                 string // fixed_dates, interval, single
	Dates                []FixedDue
	EveryMonths          int
	Count                int
	FirstDueOffsetMonths int
	DueOffsetDays        int
}

// EarlyPayment is the single-payment alternative with a discount.
type EarlyPayment struct {
	DiscountBP      int64
	PayByOffsetDays int
}

// PaymentPolicy is the payment part of the contract policy v2 (m4-plan §3.3).
type PaymentPolicy struct {
	Deposit            *Deposit
	DepositDueDays     int
	Schedule           Schedule
	IssueDaysBeforeDue int
	RoundingUnit       money.Cents // 1, 100 or 1000 cents; 0 means 1
	EarlyPayment       *EarlyPayment
	SEPA               bool
}

// Input is what Plan needs. EndsOn is exclusive (the contract end date); Today is the marina's local date.
type Input struct {
	Total    money.Cents
	StartsOn calendar.Date
	EndsOn   calendar.Date
	Today    calendar.Date
	Payment  PaymentPolicy
}

// Item is one planned instalment. IssueOn ≤ DueOn.
type Item struct {
	Seq     int
	Kind    string // deposit, installment, single
	IssueOn calendar.Date
	DueOn   calendar.Date
	Amount  money.Cents
}

// Alternative is the early-payment plan: one single item with the discount applied.
type Alternative struct {
	Discount money.Cents
	Total    money.Cents
	Items    []Item
}
