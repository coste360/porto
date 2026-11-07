package pricelist

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Quote resolution (m2-plan §4.5): from a request with ids (vessel, resource, customer) or explicit values to the
// engine input. Explicit fields override resolved ones. Names, contact data and promo codes never leave this
// file: Resolved carries ids, codes and dimensions only, and nothing here logs.

// quoter is the engine entry point (pricing.Quote); tests may replace it.
type quoter func(pricing.Input, pricing.Catalog) (pricing.Breakdown, error)

// SimVessel are explicit vessel dimensions.
type SimVessel struct {
	LOA      geo.Cm `json:"loa_cm"`
	Beam     geo.Cm `json:"beam_cm"`
	HullType string `json:"hull_type,omitempty"`
}

// SimResource names a berth, dynamic area or land resource.
type SimResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// QuoteRequest is a stay to price (PricingSimulateRequest of the contract). Matrix probes use the same shape with
// explicit dimensions, price list id and quoted_on.
type QuoteRequest struct {
	PriceListID      string                   `json:"price_list_id,omitempty"`
	ContractType     pricing.ContractType     `json:"contract_type"`
	Arrival          calendar.Date            `json:"arrival"`
	Departure        calendar.Date            `json:"departure"`
	ItemID           string                   `json:"item_id,omitempty"`
	VesselID         string                   `json:"vessel_id,omitempty"`
	Vessel           *SimVessel               `json:"vessel,omitempty"`
	Resource         *SimResource             `json:"resource,omitempty"`
	Category         string                   `json:"category,omitempty"`
	BerthLengthCm    geo.Cm                   `json:"berth_length_cm,omitempty"`
	BerthWidthCm     geo.Cm                   `json:"berth_width_cm,omitempty"`
	GroupIDs         []string                 `json:"group_ids,omitempty"`
	CustomerID       string                   `json:"customer_id,omitempty"`
	CustomerCategory string                   `json:"customer_category,omitempty"`
	Channel          pricing.Channel          `json:"channel,omitempty"`
	PromoCode        string                   `json:"promo_code,omitempty"`
	QuotedOn         *calendar.Date           `json:"quoted_on,omitempty"`
	Services         []pricing.ServiceRequest `json:"services,omitempty"`
	Deposit          *pricing.DepositPolicy   `json:"deposit,omitempty"`
}

// Dimension origins of Resolved.
const (
	OriginDeclared = "declared"
	OriginMeasured = "measured"
	OriginInput    = "input"
	OriginNone     = "none"
)

// Resolved is what a quote used after resolving ids (PriceResolved of the contract).
type Resolved struct {
	PriceListID      string          `json:"price_list_id"`
	PriceListCode    string          `json:"price_list_code"`
	ItemID           string          `json:"item_id"`
	LOA              geo.Cm          `json:"loa_cm,omitempty"`
	Beam             geo.Cm          `json:"beam_cm,omitempty"`
	HullType         string          `json:"hull_type,omitempty"`
	Category         string          `json:"category,omitempty"`
	BerthLength      geo.Cm          `json:"berth_length_cm,omitempty"`
	BerthWidth       geo.Cm          `json:"berth_width_cm,omitempty"`
	GroupIDs         []string        `json:"group_ids,omitempty"`
	CustomerCategory string          `json:"customer_category,omitempty"`
	Channel          pricing.Channel `json:"channel"`
	QuotedOn         calendar.Date   `json:"quoted_on"`
	DimensionOrigin  string          `json:"dimension_origin"`
}

var customerCategories = []string{"member", "resident", "affiliated", "charter", "yard"}

// checkRequest validates the shape of a request.
func checkRequest(req *QuoteRequest) error {
	v := &auth.ValidationError{}
	if !slices.Contains(pricing.ContractTypes(), req.ContractType) {
		v.Add("contract_type", "invalid")
	}
	if req.Arrival.IsZero() {
		v.Add("arrival", "required")
	}
	if req.Departure.IsZero() {
		v.Add("departure", "required")
	}
	if req.Channel != "" && !slices.Contains(pricing.Channels(), req.Channel) {
		v.Add("channel", "invalid")
	}
	if req.CustomerCategory != "" && !slices.Contains(customerCategories, req.CustomerCategory) {
		v.Add("customer_category", "invalid")
	}
	if req.Vessel != nil && (req.Vessel.LOA < 1 || req.Vessel.LOA > 20000 || req.Vessel.Beam < 1 || req.Vessel.Beam > 20000) {
		v.Add("vessel", "out_of_range")
	}
	if req.BerthLengthCm < 0 || req.BerthLengthCm > 20000 {
		v.Add("berth_length_cm", "out_of_range")
	}
	if req.BerthWidthCm < 0 || req.BerthWidthCm > 20000 {
		v.Add("berth_width_cm", "out_of_range")
	}
	if len(req.Services) > 50 {
		v.Add("services", "too_many")
	}
	for i, sv := range req.Services {
		if sv.Qty < 0 || sv.Qty > 1_000_000_000 {
			v.Add(fmt.Sprintf("services[%d].qty_milli", i), "out_of_range")
		}
	}
	if len(req.GroupIDs) > 50 {
		v.Add("group_ids", "too_many")
	}
	if len(req.PromoCode) > 32 {
		v.Add("promo_code", "invalid")
	}
	return v.Err()
}

// resolve fills a copy of req with the values behind its ids (explicit fields win) and the defaults, so that the
// result maps to the engine input without database access. A vessel, resource or customer of another marina is
// ErrNotFound.
func (s *Service) resolve(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req QuoteRequest) (QuoteRequest, string, error) {
	origin := OriginNone
	if req.Vessel != nil {
		origin = OriginInput
	} else if req.VesselID != "" {
		v, err := store.PricingVesselByID(ctx, tx, scope, req.VesselID)
		if err != nil {
			return req, "", err
		}
		sv := &SimVessel{LOA: geo.Cm(v.LOACm), Beam: geo.Cm(v.BeamCm), HullType: v.HullType}
		origin = OriginDeclared
		if v.DimensionsSource == OriginMeasured && v.MeasuredLOACm > 0 {
			sv.LOA, origin = geo.Cm(v.MeasuredLOACm), OriginMeasured
			if v.MeasuredBeamCm > 0 {
				sv.Beam = geo.Cm(v.MeasuredBeamCm)
			}
		}
		req.Vessel = sv
	}
	if req.Resource != nil {
		r, err := store.PricingResourceByID(ctx, tx, scope, req.Resource.Type, req.Resource.ID)
		if err != nil {
			return req, "", err
		}
		if req.Category == "" {
			req.Category = r.PriceClass
		}
		if req.BerthLengthCm == 0 {
			req.BerthLengthCm = geo.Cm(r.LengthCm)
		}
		if req.BerthWidthCm == 0 {
			req.BerthWidthCm = geo.Cm(r.WidthCm)
		}
		if req.GroupIDs == nil && len(r.GroupIDs) > 0 {
			req.GroupIDs = r.GroupIDs
		}
	}
	if req.CustomerID != "" && req.CustomerCategory == "" {
		cat, err := store.CustomerCategoryByID(ctx, tx, scope, req.CustomerID)
		if err != nil {
			return req, "", err
		}
		req.CustomerCategory = cat
	} else if req.CustomerID != "" {
		if _, err := store.CustomerCategoryByID(ctx, tx, scope, req.CustomerID); err != nil {
			return req, "", err
		}
	}
	if req.Channel == "" {
		req.Channel = pricing.ChannelDesk
	}
	if req.QuotedOn == nil {
		today, err := s.today(ctx, tx, scope)
		if err != nil {
			return req, "", err
		}
		req.QuotedOn = &today
	}
	return req, origin, nil
}

// inputFromRequest maps a resolved request to the engine input. Matrix probes go through probeRequest and back
// through this function, so simulating a probe reaches the same Quote call as the matrix cell (INV-23).
func inputFromRequest(req QuoteRequest) pricing.Input {
	in := pricing.Input{
		ContractType: req.ContractType, Arrival: req.Arrival, Departure: req.Departure, ItemID: req.ItemID,
		Resource:         pricing.Resource{Category: req.Category, Length: req.BerthLengthCm, Width: req.BerthWidthCm, GroupIDs: slices.Clone(req.GroupIDs)},
		Channel:          req.Channel,
		CustomerCategory: req.CustomerCategory,
		PromoCode:        strings.TrimSpace(req.PromoCode),
		Services:         slices.Clone(req.Services),
		Deposit:          req.Deposit,
	}
	if req.Vessel != nil {
		in.Vessel = &pricing.Vessel{LOA: req.Vessel.LOA, Beam: req.Vessel.Beam, HullType: req.Vessel.HullType}
	}
	if req.QuotedOn != nil {
		in.QuotedOn = *req.QuotedOn
	}
	return in
}

// probeRequest is the request form of an engine input (a matrix probe) for the list listID.
func probeRequest(listID string, in pricing.Input) QuoteRequest {
	req := QuoteRequest{
		PriceListID: listID, ContractType: in.ContractType, Arrival: in.Arrival, Departure: in.Departure, ItemID: in.ItemID,
		Category: in.Resource.Category, BerthLengthCm: in.Resource.Length, BerthWidthCm: in.Resource.Width,
		GroupIDs: slices.Clone(in.Resource.GroupIDs), CustomerCategory: in.CustomerCategory, Channel: in.Channel,
		PromoCode: in.PromoCode, Services: slices.Clone(in.Services), Deposit: in.Deposit,
	}
	if in.Vessel != nil {
		req.Vessel = &SimVessel{LOA: in.Vessel.LOA, Beam: in.Vessel.Beam, HullType: in.Vessel.HullType}
	}
	if !in.QuotedOn.IsZero() {
		q := in.QuotedOn
		req.QuotedOn = &q
	}
	return req
}

// findList returns the list a request names, or the active list serving the contract type on the arrival night.
func findList(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req *QuoteRequest) (*store.PriceList, error) {
	if req.PriceListID != "" {
		return store.GetPriceList(ctx, tx, scope, req.PriceListID)
	}
	actives, err := store.ActivePriceLists(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	for i := range actives {
		a := &actives[i]
		if slices.Contains(a.ContractTypes, string(req.ContractType)) && !req.Arrival.Before(a.ValidFrom) && !req.Arrival.After(a.ValidTo) {
			return a, nil
		}
	}
	return nil, ErrNoActiveList
}

// Quote prices a stay (pricing.read): the single entry point of later milestones (quotes, transits, reports)
// and of the simulator. Without a price list id it uses the active list serving the contract type on the
// arrival night (ErrNoActiveList when none); with an id it works on drafts and archived lists too (D27).
func (s *Service) Quote(ctx context.Context, p *auth.Principal, marinaID string, req QuoteRequest) (pricing.Breakdown, Resolved, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return pricing.Breakdown{}, Resolved{}, err
	}
	if err := checkRequest(&req); err != nil {
		return pricing.Breakdown{}, Resolved{}, err
	}
	var (
		in  pricing.Input
		cat pricing.Catalog
		res Resolved
	)
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := findList(ctx, tx, scope, &req)
		if err != nil {
			return err
		}
		resolved, origin, err := s.resolve(ctx, tx, scope, req)
		if err != nil {
			return err
		}
		extra := make([]string, 0, len(resolved.Services)+1)
		for _, sv := range resolved.Services {
			extra = append(extra, sv.ItemID)
		}
		if resolved.ItemID != "" {
			extra = append(extra, resolved.ItemID)
		}
		children, err := store.LoadPriceListChildren(ctx, tx, scope, l.ID)
		if err != nil {
			return err
		}
		if cat, err = catalogOf(ctx, tx, scope, l, children, extra...); err != nil {
			return err
		}
		in = inputFromRequest(resolved)
		res = Resolved{
			PriceListID: l.ID, PriceListCode: l.Code, ItemID: resolved.ItemID, Category: resolved.Category,
			BerthLength: resolved.BerthLengthCm, BerthWidth: resolved.BerthWidthCm, GroupIDs: resolved.GroupIDs,
			CustomerCategory: resolved.CustomerCategory, Channel: resolved.Channel, QuotedOn: *resolved.QuotedOn, DimensionOrigin: origin,
		}
		if res.ItemID == "" {
			res.ItemID = cat.PriceList.Settings.PrimaryItems[req.ContractType]
		}
		if resolved.Vessel != nil {
			res.LOA, res.Beam, res.HullType = resolved.Vessel.LOA, resolved.Vessel.Beam, resolved.Vessel.HullType
		}
		return nil
	})
	if err != nil {
		return pricing.Breakdown{}, Resolved{}, err
	}
	b, err := s.quote(in, cat)
	if err != nil {
		return pricing.Breakdown{}, Resolved{}, err
	}
	return b, res, nil
}
