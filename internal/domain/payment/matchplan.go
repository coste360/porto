package payment

import "github.com/coste360/porto/internal/money"

// Open is a charge that can receive money: its id and residual (total − paid − credited), already in the
// matching order (due date, then number).
type Open struct {
	ChargeID string
	Residual money.Cents
}

// PlanOldestFirst fills the open charges in order with amount (pure, m5-plan §4.2 auto-match oldest_due_first):
// every match is at most the charge residual, the matches sum to at most amount, and the remainder (amount − Σ)
// stays unallocated as customer credit (TestMatchPlan_OldestFirst).
func PlanOldestFirst(open []Open, amount money.Cents) (matches []MatchInput, remainder money.Cents) {
	left := amount
	for _, o := range open {
		if left <= 0 {
			break
		}
		if o.Residual <= 0 {
			continue
		}
		a := min(o.Residual, left)
		matches = append(matches, MatchInput{ChargeID: o.ChargeID, Amount: a})
		left -= a
	}
	return matches, left
}
