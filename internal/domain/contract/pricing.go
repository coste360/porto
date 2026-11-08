package contract

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Prices (m3-plan §4.6, §13 D10/D11): every price comes from pricelist.Service.Quote, called before the write
// transaction. The stored snapshot pins the price list and the quoting date of the request, so quoting the
// stored request again reproduces the stored breakdown (INV-20); it is never recomputed silently.

// SnapshotVersion is the version of PriceSnapshot.
const SnapshotVersion = 1

// PriceSnapshot is the JSON stored in price_snapshot of quote options, contracts and transits: the pinned
// request, what the quote resolved and the breakdown. Ids, codes and dimensions only (no names, no promo codes:
// the M3 desk never sends one).
type PriceSnapshot struct {
	Version       int                    `json:"version"`
	EngineVersion int                    `json:"engine_version"`
	Request       pricelist.QuoteRequest `json:"request"`
	Resolved      pricelist.Resolved     `json:"resolved"`
	Breakdown     pricing.Breakdown      `json:"breakdown"`
	// Adjustments are the repriced moves applied after the quote (total = breakdown total + their deltas).
	Adjustments []PriceAdjustment `json:"adjustments,omitempty"`
}

// priced is a price ready to be stored.
type priced struct {
	snapshot PriceSnapshot
	raw      store.JSON
}

func (p *priced) listID() string {
	if p == nil {
		return ""
	}
	return p.snapshot.Resolved.PriceListID
}

func (p *priced) total() int64 {
	if p == nil {
		return 0
	}
	return int64(p.snapshot.Breakdown.Total)
}

func (p *priced) deposit() int64 {
	if p == nil {
		return 0
	}
	return int64(p.snapshot.Breakdown.Deposit)
}

func (p *priced) engineVersion() int {
	if p == nil {
		return 0
	}
	return p.snapshot.EngineVersion
}

// DecodeSnapshot reads a stored snapshot (nil when absent or unreadable).
func DecodeSnapshot(raw store.JSON) *PriceSnapshot {
	if len(raw) == 0 {
		return nil
	}
	var s PriceSnapshot
	if json.Unmarshal(raw, &s) != nil || s.Version == 0 {
		return nil
	}
	return &s
}

// PricingType maps a contract kind and term to the price list contract type (m3-plan §13 D10).
func PricingType(kind, term string) pricing.ContractType {
	switch kind {
	case KindTransit:
		return pricing.ContractTransit
	case KindStorage:
		return pricing.ContractStorage // every storage term (m9-plan D2)
	}
	switch term {
	case TermAnnual:
		return pricing.ContractAnnual
	case TermSeasonal:
		return pricing.ContractSeasonal
	default:
		return pricing.ContractMonthly // monthly and period
	}
}

// priceRequest is what a stay is priced with.
type priceRequest struct {
	contractType pricing.ContractType
	arrival      calendar.Date
	departure    calendar.Date
	first        Segment // the first segment's resource prices the whole stay
	vesselID     string
	vessel       *pricelist.SimVessel // explicit dimensions (new vessels)
	customerID   string
	channel      string
	services     []ServiceRequest
}

// price quotes a stay with the principal's rights (pricing.read).
func (s *Service) price(ctx context.Context, p *auth.Principal, marinaID string, r priceRequest) (*priced, error) {
	req := pricelist.QuoteRequest{
		ContractType: r.contractType, Arrival: r.arrival, Departure: r.departure, VesselID: r.vesselID, Vessel: r.vessel,
		Resource:   &pricelist.SimResource{Type: r.first.ResourceType, ID: r.first.ResourceID},
		CustomerID: r.customerID, Channel: pricing.Channel(r.channel),
	}
	if req.Vessel != nil {
		req.VesselID = ""
	}
	for _, sv := range r.services {
		req.Services = append(req.Services, pricing.ServiceRequest{ItemID: sv.ItemID, Qty: qty.Milli(sv.QtyMilli)})
	}
	b, res, err := s.d.Prices.Quote(ctx, p, marinaID, req)
	if err != nil {
		return nil, err
	}
	// Pin the list, the date and the resolved inputs so the stored request prices the same way later.
	req.PriceListID = res.PriceListID
	q := res.QuotedOn
	req.QuotedOn = &q
	if req.Vessel == nil && res.LOA > 0 {
		req.Vessel = &pricelist.SimVessel{LOA: geo.Cm(res.LOA), Beam: geo.Cm(res.Beam), HullType: res.HullType}
	}
	snap := PriceSnapshot{Version: SnapshotVersion, EngineVersion: b.EngineVersion, Request: req, Resolved: res, Breakdown: b}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	return &priced{snapshot: snap, raw: raw}, nil
}

// checkListStillActive fails with ErrPriceListChanged when the list of a price is no longer the active one for
// the arrival of its contract type (m3-plan §4.6): the price was computed in its own read transaction.
func checkListStillActive(ctx context.Context, tx bun.IDB, scope store.MarinaScope, pr *priced) error {
	if pr == nil {
		return nil
	}
	actives, err := store.ActivePriceLists(ctx, tx, scope)
	if err != nil {
		return err
	}
	req := pr.snapshot.Request
	for i := range actives {
		a := &actives[i]
		if slices.Contains(a.ContractTypes, string(req.ContractType)) && !req.Arrival.Before(a.ValidFrom) && !req.Arrival.After(a.ValidTo) {
			if a.ID == pr.snapshot.Resolved.PriceListID {
				return nil
			}
			return ErrPriceListChanged
		}
	}
	return ErrPriceListChanged
}

// Recompute quotes a stored snapshot's request again (pricing.read) and returns the new breakdown: equal to the
// stored one while the pinned list and its catalogue are unchanged (INV-20, TestContract_SnapshotStored).
func (s *Service) Recompute(ctx context.Context, p *auth.Principal, marinaID string, snap *PriceSnapshot) (pricing.Breakdown, error) {
	b, _, err := s.d.Prices.Quote(ctx, p, marinaID, snap.Request)
	return b, err
}
