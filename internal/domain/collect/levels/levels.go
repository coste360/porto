package levels

import (
	"encoding/json"

	"github.com/coste360/porto/internal/calendar"
)

// Level names, in their strictly increasing order.
const (
	PreDue      = "pre_due"
	Due         = "due"
	Courteous   = "courteous"
	Formal      = "formal"
	FinalNotice = "final_notice"
)

// Order lists the levels from the first to the last.
var Order = []string{PreDue, Due, Courteous, Formal, FinalNotice}

// Rank is the position of a level in Order (−1 for none or unknown).
func Rank(level string) int {
	for i, l := range Order {
		if l == level {
			return i
		}
	}
	return -1
}

// Step is the trigger of one level: days before the due date (pre_due) or after it (the others; due is day 0).
type Step struct {
	Level   string `json:"level"`
	Days    int    `json:"days"`
	Enabled bool   `json:"enabled"`
}

// Policy is the reminder policy of a marina (billing_settings.reminder_policy).
type Policy struct {
	PreDueDays int    `json:"pre_due_days"`
	Steps      []Step `json:"levels"`
}

// DefaultPolicy is §16 D13: pre-due 7 days before, due, courteous +7, formal +21, final notice +45.
func DefaultPolicy() Policy {
	return Policy{PreDueDays: 7, Steps: []Step{
		{Level: PreDue, Days: 7, Enabled: true},
		{Level: Due, Days: 0, Enabled: true},
		{Level: Courteous, Days: 7, Enabled: true},
		{Level: Formal, Days: 21, Enabled: true},
		{Level: FinalNotice, Days: 45, Enabled: true},
	}}
}

// ParsePolicy reads a stored policy (pre_due days may be stored negative, relative to the due date); an empty or invalid document, and missing levels, take the defaults. Days
// are clamped to 0..365 and the after-due days are made non-decreasing in level order.
func ParsePolicy(raw []byte) Policy {
	def := DefaultPolicy()
	var in struct {
		PreDueDays *int   `json:"pre_due_days"`
		Steps      []Step `json:"levels"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &in) != nil {
		return def
	}
	byLevel := map[string]Step{}
	for _, s := range in.Steps {
		if Rank(s.Level) >= 0 {
			byLevel[s.Level] = s
		}
	}
	out := Policy{PreDueDays: def.PreDueDays}
	if in.PreDueDays != nil {
		out.PreDueDays = clamp(abs(*in.PreDueDays))
	}
	prev := 0
	for _, d := range def.Steps {
		s, ok := byLevel[d.Level]
		if !ok {
			s = d
		}
		if s.Level == PreDue && s.Days < 0 {
			s.Days = -s.Days // stored as days relative to the due date (negative = before)
		}
		s.Days = clamp(s.Days)
		switch s.Level {
		case PreDue:
			if in.PreDueDays == nil && ok {
				out.PreDueDays = s.Days
			}
			s.Days = out.PreDueDays
		case Due:
			s.Days = 0
		default:
			if s.Days < prev {
				s.Days = prev
			}
			prev = s.Days
		}
		out.Steps = append(out.Steps, s)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func clamp(n int) int {
	return max(0, min(n, 365))
}

func (p Policy) step(level string) (Step, bool) {
	for _, s := range p.Steps {
		if s.Level == level {
			return s, true
		}
	}
	return Step{}, false
}

// TriggerOn is the date from which a level applies to a charge due on due.
func (p Policy) TriggerOn(level string, due calendar.Date) calendar.Date {
	s, _ := p.step(level)
	if level == PreDue {
		return due.AddDays(-s.Days)
	}
	return due.AddDays(s.Days)
}

// Next returns the level to send today for a charge due on due whose last sent level is last ("" for none): the
// highest enabled level already reached (trigger ≤ today) that ranks above last. pre_due applies only before the
// due date. ok is false when nothing is due. Levels sent for a charge are therefore strictly increasing and never
// repeated.
func Next(due, today calendar.Date, p Policy, last string) (string, bool) {
	lastRank := Rank(last)
	best := ""
	for i, l := range Order {
		s, ok := p.step(l)
		if !ok || !s.Enabled || i <= lastRank {
			continue
		}
		if l == PreDue && !today.Before(due) {
			continue
		}
		if p.TriggerOn(l, due).After(today) {
			continue
		}
		best = l
	}
	return best, best != ""
}

// NextAfter is the next enabled level after last regardless of dates (manual reminders before a level is
// reached); pre_due is skipped once the charge is due.
func NextAfter(due, today calendar.Date, p Policy, last string) (string, bool) {
	if l, ok := Next(due, today, p, last); ok {
		return l, true
	}
	lastRank := Rank(last)
	for i, l := range Order {
		s, ok := p.step(l)
		if !ok || !s.Enabled || i <= lastRank {
			continue
		}
		if l == PreDue && !today.Before(due) {
			continue
		}
		return l, true
	}
	return "", false
}

// NeedsApproval reports whether a level waits for the office before it is sent (final notice).
func NeedsApproval(level string) bool { return level == FinalNotice }
