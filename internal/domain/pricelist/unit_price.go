package pricelist

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// Unit prices of yard items (M10, m10-plan §3.7; owned by QUOTE): the active catalogues of the marina are loaded
// once and every item is priced by the pure pricing.UnitPrice. Read-only, inside the caller's transaction, no
// permission check (the caller authorised the marina) and nothing logged.

// UnitPriceLookup is the catalogue price of one item: Found false when no base rule applies (line issue no_price).
type UnitPriceLookup struct {
	pricing.UnitPriceResult
	Found bool
}

// UnitPricesTx returns the unit price of each item on the date for the customer category, from the active price
// lists of the marina.
func (s *Service) UnitPricesTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, itemIDs []string, on calendar.Date,
	customerCategory string,
) (map[string]UnitPriceLookup, error) {
	out := make(map[string]UnitPriceLookup, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	actives, err := store.ActivePriceLists(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	cats := make([]pricing.Catalog, 0, len(actives))
	for i := range actives {
		l := &actives[i]
		if on.Before(l.ValidFrom) || on.After(l.ValidTo) {
			continue
		}
		children, err := store.LoadPriceListChildren(ctx, tx, scope, l.ID)
		if err != nil {
			return nil, err
		}
		cat, err := catalogOf(ctx, tx, scope, l, children, itemIDs...)
		if err != nil {
			return nil, err
		}
		cats = append(cats, cat)
	}
	for _, id := range itemIDs {
		if _, done := out[id]; done {
			continue
		}
		r, err := pricing.UnitPrice(cats, id, on, customerCategory)
		switch {
		case err == nil:
			out[id] = UnitPriceLookup{UnitPriceResult: r, Found: true}
		case errors.Is(err, pricing.ErrNoPrice):
			out[id] = UnitPriceLookup{}
		default:
			return nil, err
		}
	}
	return out, nil
}
