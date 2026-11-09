package pricing

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
)

// matchCtx is what a scope is matched against.
type matchCtx struct {
	contract         ContractType
	category         string
	groups           []string
	channel          Channel
	customerCategory string
	period           string
}

// specificity is the number of non-empty scope lists (0..7; TimeBands is set only on rental rules).
func (s Scope) specificity() int {
	n := 0
	for _, l := range []int{len(s.ContractTypes), len(s.Categories), len(s.GroupIDs), len(s.Channels), len(s.CustomerCategories), len(s.Periods), len(s.TimeBands)} {
		if l > 0 {
			n++
		}
	}
	return n
}

// matches reports whether every non-empty scope list contains the context value. An empty context value never
// matches a non-empty list.
func (s Scope) matches(c matchCtx) bool {
	if len(s.ContractTypes) > 0 && !slices.Contains(s.ContractTypes, c.contract) {
		return false
	}
	if len(s.Categories) > 0 && (c.category == "" || !slices.Contains(s.Categories, c.category)) {
		return false
	}
	if len(s.GroupIDs) > 0 && !intersects(s.GroupIDs, c.groups) {
		return false
	}
	if len(s.Channels) > 0 && !slices.Contains(s.Channels, c.channel) {
		return false
	}
	if len(s.CustomerCategories) > 0 && (c.customerCategory == "" || !slices.Contains(s.CustomerCategories, c.customerCategory)) {
		return false
	}
	if len(s.Periods) > 0 && (c.period == "" || !slices.Contains(s.Periods, c.period)) {
		return false
	}
	return true
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if x != "" && slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// ruleBefore is the winner order of m2-plan §2.4 step 2: higher priority, then higher specificity, then higher
// seq (more recent), then lower id.
func ruleBefore(a, b *Rule) bool { return ruleCmp(a, b) < 0 }

func ruleCmp(a, b *Rule) int {
	if a.Priority != b.Priority {
		if a.Priority > b.Priority {
			return -1
		}
		return 1
	}
	if sa, sb := a.Scope.specificity(), b.Scope.specificity(); sa != sb {
		if sa > sb {
			return -1
		}
		return 1
	}
	if a.Seq != b.Seq {
		if a.Seq > b.Seq {
			return -1
		}
		return 1
	}
	switch {
	case a.ID < b.ID:
		return -1
	case a.ID > b.ID:
		return 1
	}
	return 0
}

// periodIndex finds the rate period of a night: an event containing it, else a season containing it, else none.
// Periods are scanned in code order so the result never depends on the input order, even for overlapping
// periods that validation would reject.
type periodIndex struct {
	events, seasons []RatePeriod
}

func newPeriodIndex(periods []RatePeriod) periodIndex {
	var ix periodIndex
	for _, p := range periods {
		switch p.Kind {
		case PeriodEvent:
			ix.events = append(ix.events, p)
		case PeriodSeason:
			ix.seasons = append(ix.seasons, p)
		}
	}
	byCode := func(a, b RatePeriod) int {
		switch {
		case a.Code < b.Code:
			return -1
		case a.Code > b.Code:
			return 1
		}
		return 0
	}
	slices.SortStableFunc(ix.events, byCode)
	slices.SortStableFunc(ix.seasons, byCode)
	return ix
}

func (ix periodIndex) of(d calendar.Date) string {
	for _, group := range [][]RatePeriod{ix.events, ix.seasons} {
		for _, p := range group {
			for _, r := range p.Ranges {
				if inRange(d, r) {
					return p.Code
				}
			}
		}
	}
	return ""
}
