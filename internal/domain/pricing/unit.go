package pricing

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Unit prices of yard items (M10, m10-plan §3.7, D1). The base rule selection of the engine is reused: among the
// base rules of an item in the given active catalogues whose list validity covers the date, whose rate period
// scope matches the date and whose customer category scope matches (empty = any; contract type, berth, group and
// channel scopes are ignored), the winner is the first by ruleCmp (priority, specificity, recency). EngineVersion
// does not change: Quote output is untouched.

// UnitPriceResult is the catalogue unit price of an item: UnitCents of the winning rule, else its AmountCents.
type UnitPriceResult struct {
	UnitCents        money.Cents `json:"unit_cents"`
	PriceListID      string      `json:"price_list_id"`
	RuleID           string      `json:"rule_id"`
	PriceIncludesVAT bool        `json:"price_includes_vat"`
}

// unitScope keeps the scope lists UnitPrice honours: customer categories and rate periods.
func unitScope(s Scope) Scope {
	return Scope{CustomerCategories: s.CustomerCategories, Periods: s.Periods}
}

// UnitPrice returns the unit price of itemID on the date for the customer category (pure). No applicable rule is
// a *NoPriceError with Reason ReasonNoRule (yard quote line issue no_price).
func UnitPrice(cats []Catalog, itemID string, on calendar.Date, customerCategory string) (UnitPriceResult, error) {
	var (
		best    *Rule
		bestCat *Catalog
	)
	for ci := range cats {
		cat := &cats[ci]
		pl := cat.PriceList
		if on.Before(pl.ValidFrom) || on.After(pl.ValidTo) {
			continue
		}
		ctx := matchCtx{customerCategory: customerCategory, period: newPeriodIndex(pl.Periods).of(on)}
		for ri := range cat.Rules {
			r := &cat.Rules[ri]
			if !r.Active || r.Stage != StageBase || r.ItemID != itemID {
				continue
			}
			if r.UnitCents == nil && r.AmountCents == nil {
				continue
			}
			sc := unitScope(r.Scope)
			if !sc.matches(ctx) {
				continue
			}
			cand := *r
			cand.Scope = sc
			if best == nil || ruleCmp(&cand, best) < 0 {
				c := cand
				best, bestCat = &c, cat
			}
		}
	}
	if best == nil {
		return UnitPriceResult{}, &NoPriceError{ItemID: itemID, Night: on, Reason: ReasonNoRule}
	}
	out := UnitPriceResult{PriceListID: bestCat.PriceList.ID, RuleID: best.ID}
	if best.UnitCents != nil {
		out.UnitCents = *best.UnitCents
	} else {
		out.UnitCents = *best.AmountCents
	}
	for _, it := range bestCat.Items {
		if it.ID == itemID {
			out.PriceIncludesVAT = it.PriceIncludesVAT
			break
		}
	}
	return out, nil
}
