package policy

import (
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// MaxVesselLOACm bounds the declared length of a vessel on the public page (m8-plan §4.3).
const MaxVesselLOACm geo.Cm = 10_000

// CheckStay applies the online stay rules to a stay (m8-plan §3.1). The checks run in a fixed order and the first
// failing one is reported: nights (min, max), advance (arrival in the past, too close, too far), arrival weekday.
// Times are compared in s.Loc: the arrival instant is the arrival day at ArrivalToMinutes (end of the day when 0),
// so a stay can be booked on its arrival day until MinAdvanceHours before the latest arrival time.
func CheckStay(r StayRules, s Stay) StayCheck {
	loc := s.Loc
	if loc == nil {
		loc = time.UTC
	}
	nights := s.Arrival.DaysUntil(s.Departure)
	switch {
	case nights < max(r.MinNights, 1):
		return StayCheck{Reason: BlockMinNights}
	case r.MaxNights > 0 && nights > r.MaxNights:
		return StayCheck{Reason: BlockMaxNights}
	}
	today := calendar.In(s.Now, loc)
	if s.Arrival.Before(today) {
		return StayCheck{Reason: BlockAdvance}
	}
	latest := 24 * 60
	if r.ArrivalToMinutes > 0 && r.ArrivalToMinutes < latest {
		latest = r.ArrivalToMinutes
	}
	arrivalAt := s.Arrival.Time(loc).Add(time.Duration(latest) * time.Minute)
	if s.Now.Add(time.Duration(r.MinAdvanceHours) * time.Hour).After(arrivalAt) {
		return StayCheck{Reason: BlockAdvance}
	}
	if r.MaxAdvanceDays > 0 && today.DaysUntil(s.Arrival) > r.MaxAdvanceDays {
		return StayCheck{Reason: BlockAdvance}
	}
	if slices.Contains(r.ClosedWeekdays, s.Arrival.Time(loc).Weekday()) {
		return StayCheck{Reason: BlockWeekday}
	}
	return StayCheck{OK: true}
}

// Approval says whether a booking needs the approval of the office and why (m8-plan §6): "always" first, then a
// vessel longer than the LOA threshold, then a solution in one of the approval groups.
func Approval(r ApprovalRules, in ApprovalInput) ApprovalDecision {
	switch {
	case r.Always:
		return ApprovalDecision{Required: true, Reason: ApprovalAlways}
	case r.LOAOverCm != nil && in.LOACm > *r.LOAOverCm:
		return ApprovalDecision{Required: true, Reason: ApprovalLOA}
	}
	for _, g := range in.GroupIDs {
		if slices.Contains(r.GroupIDs, g) {
			return ApprovalDecision{Required: true, Reason: ApprovalGroup}
		}
	}
	return ApprovalDecision{}
}

// Deposit returns the amount due now (m8-plan §3.1): the total in mode full; in mode deposit the price-list
// deposit when the policy defines one, otherwise total × bp / 10000 rounded half-even to the cent, at least one
// cent and never more than the total. A zero or negative total is due as it is.
func Deposit(in DepositInput) money.Cents {
	if in.Total <= 0 || in.Mode != "deposit" {
		return max(in.Total, 0)
	}
	d := in.PriceListDeposit
	if d <= 0 {
		bp := min(max(in.BP, 0), 10_000)
		d = in.Total.MulRat(int64(bp), 10_000)
	}
	return min(max(d, 1), in.Total)
}

// SortTiers returns the tiers ordered by MinDaysBefore descending (the order Refund reads them in).
func SortTiers(tiers []Tier) []Tier {
	out := slices.Clone(tiers)
	slices.SortStableFunc(out, func(a, b Tier) int { return b.MinDaysBefore - a.MinDaysBefore })
	return out
}

// Refund computes the refund of a cancellation from the tiers of the contract's policy snapshot (m8-plan §7.2):
// cancelling d whole days before arrival (d = days from Today to Arrival, marina time zone) refunds the paid
// amount × refund_bp / 10000 (half-even) of the first tier, by MinDaysBefore descending, with MinDaysBefore ≤ d.
// No tier matching means no refund. On or after the arrival day a cancellation is not allowed (refund 0).
func Refund(in RefundInput) RefundResult {
	d := in.Today.DaysUntil(in.Arrival)
	if d <= 0 {
		return RefundResult{}
	}
	paid := max(in.Paid, 0)
	for _, t := range SortTiers(in.Tiers) {
		if d >= t.MinDaysBefore {
			tier := t
			bp := min(max(t.RefundBP, 0), 10_000)
			return RefundResult{Refund: min(paid.MulRat(int64(bp), 10_000), paid), Tier: &tier, Allowed: true}
		}
	}
	return RefundResult{Allowed: true}
}

// QuotaCap is the online cap of a group on a night (m8-plan §3.3, §16 D5): count = value, percent =
// floor(value × active member berths / 100), none = no cap (-1). Negative inputs count as zero.
func QuotaCap(kind string, value int64, memberBerths int) int {
	value = max(value, 0)
	switch kind {
	case QuotaCount:
		return int(min(value, 1_000_000))
	case QuotaPercent:
		return int(min(value, 100) * int64(max(memberBerths, 0)) / 100)
	}
	return -1
}

// ParseHHMM returns the minutes from midnight of an HH:MM time, or 0 when it is not valid.
func ParseHHMM(s string) int {
	if len(s) != 5 || s[2] != ':' {
		return 0
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if s[0] < '0' || s[0] > '2' || s[1] < '0' || s[1] > '9' || s[3] < '0' || s[3] > '5' || s[4] < '0' || s[4] > '9' || h > 23 {
		return 0
	}
	return h*60 + m
}
