package pricing

import (
	"strings"

	"github.com/coste360/porto/internal/money"
)

// discountGroups is the application order of m2-plan §2.4 step 8.
var discountGroups = []DiscountKind{DiscountCustomer, DiscountPromo, DiscountAutomatic}

// discountMatches tells whether a discount rule of the group applies to the quote (its lines are checked
// separately).
func (e *engine) discountMatches(r *Rule, kind DiscountKind, promo string) bool {
	if !r.Active || r.Stage != StageDiscount || r.DiscountKind != kind {
		return false
	}
	if !r.Scope.matches(e.ctxAt(e.in.Arrival)) || !e.conditionsHold(r.Conditions) {
		return false
	}
	switch kind {
	case DiscountCustomer:
		return len(r.Scope.CustomerCategories) > 0
	case DiscountPromo:
		return promo != "" && strings.ToUpper(r.PromoCode) == promo
	}
	return true
}

// applyDiscounts applies the three groups in order. Inside a group every reduction is computed on the line
// amounts at the start of the group; the rules then apply in winner order, each capped at what is left on its
// lines and spread over them by largest remainder weighted by the amounts left, so no line goes negative.
func (e *engine) applyDiscounts(lines []Line, items []*Item) (money.Cents, error) {
	promo := strings.ToUpper(strings.TrimSpace(e.in.PromoCode))
	promoApplied := false
	var total money.Cents
	for _, kind := range discountGroups {
		start := make([]money.Cents, len(lines))
		for i := range lines {
			start[i] = lines[i].Amount
		}
		type cand struct {
			r         *Rule
			lines     []int
			reduction money.Cents
		}
		var stack []cand
		var choice *cand
		var discarded []cand
		for _, r := range e.rules {
			if !e.discountMatches(r, kind, promo) {
				continue
			}
			var idx []int
			var eligible money.Cents
			for i := range lines {
				if r.targets(items[i]) && start[i] > 0 {
					idx = append(idx, i)
					v, err := addCents(eligible, start[i])
					if err != nil {
						return 0, err
					}
					eligible = v
				}
			}
			if eligible <= 0 {
				continue
			}
			var red money.Cents
			switch {
			case r.Effect.PercentBP != nil:
				v, err := eligible.MulRatChecked(*r.Effect.PercentBP, 10_000)
				if err != nil {
					return 0, ErrOverflow
				}
				red = v
			case r.Effect.AmountCents != nil:
				red = min(*r.Effect.AmountCents, eligible)
			}
			red = max(0, min(red, eligible))
			if kind == DiscountPromo {
				promoApplied = true
			}
			c := cand{r: r, lines: idx, reduction: red}
			if r.Stackable {
				stack = append(stack, c)
				continue
			}
			if choice == nil || c.reduction > choice.reduction {
				if choice != nil {
					discarded = append(discarded, *choice)
				}
				cc := c
				choice = &cc
			} else {
				discarded = append(discarded, c)
			}
		}
		apply := stack
		if choice != nil {
			apply = append(apply, *choice)
			// Keep the winner order of the rules among the applied ones.
			for i := len(apply) - 1; i > 0 && ruleBefore(apply[i].r, apply[i-1].r); i-- {
				apply[i], apply[i-1] = apply[i-1], apply[i]
			}
		}
		for _, d := range discarded {
			e.addTrace(TraceDiscountDiscarded, -1, -1, d.r.ID, map[string]string{"kind": string(kind), "reduction_cents": itoa(int64(d.reduction))})
		}
		for _, c := range apply {
			var left money.Cents
			weights := make([]int64, 0, len(c.lines))
			for _, i := range c.lines {
				v, err := addCents(left, lines[i].Amount)
				if err != nil {
					return 0, err
				}
				left = v
				weights = append(weights, int64(lines[i].Amount))
			}
			red := c.reduction
			if red > left {
				red = left
				e.addTrace(TraceDiscountCapped, -1, -1, c.r.ID, map[string]string{"kind": string(kind), "cap_cents": itoa(int64(left))})
			}
			if red <= 0 {
				e.addTrace(TraceDiscountApplied, -1, -1, c.r.ID, map[string]string{"kind": string(kind), "reduction_cents": "0"})
				continue
			}
			shares, err := money.Allocate(red, weights)
			if err != nil {
				return 0, ErrOverflow
			}
			for k, i := range c.lines {
				if shares[k] == 0 {
					continue
				}
				lines[i].Amount -= shares[k]
				lines[i].Discounts = append(lines[i].Discounts, Applied{RuleID: c.r.ID, Delta: -shares[k]})
			}
			v, err := addCents(total, red)
			if err != nil {
				return 0, err
			}
			total = v
			e.addTrace(TraceDiscountApplied, -1, -1, c.r.ID, map[string]string{"kind": string(kind), "reduction_cents": itoa(int64(red))})
		}
	}
	if promo != "" && !promoApplied {
		return 0, ErrPromoNotApplicable
	}
	return total, nil
}
