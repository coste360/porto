package policy

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// Block reasons of a whole search or of a stay (BookingBlockReason in the API).
const (
	BlockClosed    = "closed"
	BlockMinNights = "min_nights"
	BlockMaxNights = "max_nights"
	BlockAdvance   = "advance"
	BlockWeekday   = "weekday"
	BlockTooLarge  = "too_large"
)

// Approval reasons (online_bookings.approval_reason).
const (
	ApprovalLOA        = "loa"
	ApprovalGroup      = "group"
	ApprovalAlways     = "always"
	ApprovalNoProvider = "no_provider"
)

// StayRules are the online stay rules of a marina (a copy of the onlineconf fields the pure checks need).
type StayRules struct {
	MinNights       int
	MaxNights       int
	MinAdvanceHours int
	MaxAdvanceDays  int
	ClosedWeekdays  []time.Weekday
	// ArrivalToMinutes is the latest arrival time of the day in minutes from local midnight (0 = end of day): the
	// minimum advance is counted to that instant of the arrival day.
	ArrivalToMinutes int
}

// Stay is a requested stay. Now is the current instant and Loc the marina time zone (passed in: no clock here).
type Stay struct {
	Arrival, Departure calendar.Date // departure exclusive
	Now                time.Time
	Loc                *time.Location
}

// StayCheck is the outcome of a stay check: OK, or the first blocking reason.
type StayCheck struct {
	OK     bool
	Reason string // one of the Block* constants when !OK
}

// ApprovalRules are the rules that put a booking in awaiting_approval.
type ApprovalRules struct {
	LOAOverCm *geo.Cm
	GroupIDs  []string
	Always    bool
}

// ApprovalInput is what the approval rules look at: the vessel and the groups of the chosen solution.
type ApprovalInput struct {
	LOACm    geo.Cm
	GroupIDs []string
}

// ApprovalDecision says whether approval is required and why.
type ApprovalDecision struct {
	Required bool
	Reason   string // one of the Approval* constants when Required
}

// Tier is a cancellation tier of a policy snapshot (see onlineconf.Tier).
type Tier struct {
	MinDaysBefore int
	RefundBP      int
}

// RefundInput computes a cancellation refund from the tiers of the contract's policy snapshot.
type RefundInput struct {
	Tiers   []Tier
	Paid    money.Cents
	Arrival calendar.Date
	Today   calendar.Date // marina time zone
}

// RefundResult is the refund and the tier that produced it (Tier nil when no tier applies: refund 0).
type RefundResult struct {
	Refund  money.Cents
	Tier    *Tier
	Allowed bool // false on or after the arrival day
}

// Quota kinds (berth_groups.online_quota_kind).
const (
	QuotaNone    = "none"
	QuotaCount   = "count"
	QuotaPercent = "percent"
)

// DepositInput computes the amount due now: the price-list deposit wins when the policy defines one, otherwise
// total × bp / 10000 rounded half-even to the cent, at least 1 cent (payment mode deposit), or the total (full).
type DepositInput struct {
	Mode             string // deposit, full
	BP               int
	Total            money.Cents
	PriceListDeposit money.Cents // 0 = none
}
