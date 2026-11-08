package pricelist

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Batch list-price quoting for the M7 reports (m7-plan §3.3, §3.4, D22): read-only, inside the caller's read
// transaction, one catalog load per price list, no permission re-check (the report permission covers it) and
// nothing logged.

// ReportQuote is a stay to price at list price: explicit dimensions, no customer, desk channel, no services.
type ReportQuote struct {
	ContractType       pricing.ContractType
	Arrival, Departure calendar.Date // departure exclusive
	Category           string
	BerthLength        geo.Cm
	BerthWidth         geo.Cm
	GroupIDs           []string
	LOA, Beam          geo.Cm
	QuotedOn           calendar.Date
}

// ReportQuoter prices many stays with the active lists of one marina.
type ReportQuoter struct {
	s       *Service
	tx      bun.IDB
	scope   store.MarinaScope
	actives []store.PriceList
	cats    map[string]*pricing.Catalog
	failed  map[string]error
}

// NewReportQuoter loads the active lists of the marina (catalogs are loaded on first use).
func (s *Service) NewReportQuoter(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (*ReportQuoter, error) {
	actives, err := store.ActivePriceLists(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	return &ReportQuoter{s: s, tx: tx, scope: scope, actives: actives, cats: map[string]*pricing.Catalog{}, failed: map[string]error{}}, nil
}

// Total returns the list total (VAT included) of a stay with the active list serving its contract type on the
// arrival night; ErrNoActiveList when none, or the engine error (no price).
func (q *ReportQuoter) Total(ctx context.Context, r ReportQuote) (money.Cents, error) {
	var list *store.PriceList
	for i := range q.actives {
		a := &q.actives[i]
		if slices.Contains(a.ContractTypes, string(r.ContractType)) && !r.Arrival.Before(a.ValidFrom) && !r.Arrival.After(a.ValidTo) {
			list = a
			break
		}
	}
	if list == nil {
		return 0, ErrNoActiveList
	}
	if err := q.failed[list.ID]; err != nil {
		return 0, err
	}
	cat := q.cats[list.ID]
	if cat == nil {
		children, err := store.LoadPriceListChildren(ctx, q.tx, q.scope, list.ID)
		if err == nil {
			var c pricing.Catalog
			if c, err = catalogOf(ctx, q.tx, q.scope, list, children); err == nil {
				cat = &c
			}
		}
		if err != nil {
			q.failed[list.ID] = err
			return 0, err
		}
		q.cats[list.ID] = cat
	}
	in := pricing.Input{
		ContractType: r.ContractType, Arrival: r.Arrival, Departure: r.Departure, Channel: pricing.ChannelDesk,
		Resource: pricing.Resource{Category: r.Category, Length: r.BerthLength, Width: r.BerthWidth, GroupIDs: slices.Clone(r.GroupIDs)},
		Vessel:   &pricing.Vessel{LOA: r.LOA, Beam: r.Beam}, QuotedOn: r.QuotedOn,
	}
	b, err := q.s.quote(in, *cat)
	if err != nil {
		return 0, err
	}
	return b.Total, nil
}
