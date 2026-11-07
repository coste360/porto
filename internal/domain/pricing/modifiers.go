package pricing

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// targets reports whether a modifier or discount rule applies to item: TargetItemIDs contains it, or
// TargetItemTypes contains its type; both empty means [berth_fee].
func (r *Rule) targets(item *Item) bool {
	if len(r.TargetItemIDs) == 0 && len(r.TargetItemTypes) == 0 {
		return item.Type == ItemBerthFee
	}
	return slices.Contains(r.TargetItemIDs, item.ID) || slices.Contains(r.TargetItemTypes, item.Type)
}

// conditionsHold checks the conditions of a modifier or discount (m2-plan §2.4 step 6).
func (e *engine) conditionsHold(c Conditions) bool {
	v := e.in.Vessel
	if len(c.HullTypes) > 0 && (v == nil || !slices.Contains(c.HullTypes, v.HullType)) {
		return false
	}
	if c.OverhangMinCm != nil {
		if v == nil || e.in.Resource.Length <= 0 || v.LOA-e.in.Resource.Length < *c.OverhangMinCm {
			return false
		}
	}
	if c.MinNights != nil && e.nights < *c.MinNights {
		return false
	}
	if c.MaxNights != nil && e.nights > *c.MaxNights {
		return false
	}
	if c.LeadMinDays != nil || c.LeadMaxDays != nil {
		lead := e.in.QuotedOn.DaysUntil(e.in.Arrival)
		if e.in.QuotedOn.IsZero() {
			return false
		}
		if c.LeadMinDays != nil && lead < *c.LeadMinDays {
			return false
		}
		if c.LeadMaxDays != nil && lead > *c.LeadMaxDays {
			return false
		}
	}
	return true
}

// effectDelta is half_even(base × bp / 10 000) or the fixed amount.
func effectDelta(eff Effect, base money.Cents) (money.Cents, error) {
	switch {
	case eff.PercentBP != nil:
		d, err := base.MulRatChecked(*eff.PercentBP, 10_000)
		if err != nil {
			return 0, ErrOverflow
		}
		return d, nil
	case eff.AmountCents != nil:
		return *eff.AmountCents, nil
	}
	return 0, nil
}

// applyModifiers applies the matching modifiers to a block: every stackable one plus the non-stackable one with
// the lowest delta (the most favourable to the customer). Deltas are computed on the block base and summed; the
// amount never goes below zero.
func (e *engine) applyModifiers(pb *pricedBlock, item *Item) error {
	ctx := e.ctxAt(pb.b.From)
	base := pb.b.Base
	var applied []Applied
	var choice *Rule
	var choiceDelta money.Cents
	var others []Applied
	for _, r := range e.rules {
		if !r.Active || r.Stage != StageModifier || !r.targets(item) || !r.Scope.matches(ctx) || !e.conditionsHold(r.Conditions) {
			continue
		}
		d, err := effectDelta(r.Effect, base)
		if err != nil {
			return err
		}
		if r.Stackable {
			applied = append(applied, Applied{RuleID: r.ID, Delta: d})
			continue
		}
		// Rules are in winner order, so a strictly lower delta is needed to replace an earlier choice.
		if choice == nil || d < choiceDelta {
			if choice != nil {
				others = append(others, Applied{RuleID: choice.ID, Delta: choiceDelta})
			}
			choice, choiceDelta = r, d
		} else {
			others = append(others, Applied{RuleID: r.ID, Delta: d})
		}
	}
	if choice != nil {
		applied = append(applied, Applied{RuleID: choice.ID, Delta: choiceDelta})
	}
	amount := base
	for _, a := range applied {
		s, err := addCents(amount, a.Delta)
		if err != nil {
			return err
		}
		amount = s
	}
	if amount < 0 {
		amount = 0
	}
	if applied == nil {
		applied = []Applied{}
	}
	pb.b.Modifiers = applied
	pb.b.Amount = amount
	pb.discarded = others
	return nil
}

// stayLastNight is the last night of the stay.
func (e *engine) stayLastNight() calendar.Date { return e.in.Departure.AddDays(-1) }
