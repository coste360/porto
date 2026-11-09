package booking

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Public configuration, availability and quotes (m8-plan §3, §11). Nothing here writes, except the anonymous
// lost-demand inquiry of an unanswered search (§9), in its own small transaction.

func nameOf(raw store.JSON) Name {
	var n Name
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		n.It, n.En = m["it"], m["en"]
	}
	return n
}

// catalogue is the online categories of a marina with the resource → category map.
type catalogue struct {
	categories []Category
	classOf    map[string]string // resource id → category code
	resources  []store.OnlineResource
}

func (s *Service) loadCatalogue(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*catalogue, error) {
	res, err := store.OnlineResources(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	cats, err := store.ListBerthCategories(ctx, db, scope, false)
	if err != nil {
		return nil, err
	}
	names := map[string]Name{}
	order := map[string]int{}
	for i, c := range cats {
		names[c.Code], order[c.Code] = nameOf(c.Name), i
	}
	out := &catalogue{classOf: map[string]string{}, resources: res}
	byCode := map[string]*Category{}
	for _, r := range res {
		code := r.PriceClass
		if code == "" {
			code = CategoryStandard
		}
		out.classOf[r.ID] = code
		c := byCode[code]
		if c == nil {
			n, ok := names[code]
			if !ok {
				n = Name{It: code, En: code}
				if code == CategoryStandard {
					n = Name{It: "Posto barca", En: "Berth"}
				}
			}
			c = &Category{Code: code, Name: n}
			byCode[code] = c
		}
		if r.Type == store.ResourceArea {
			c.MaxLOACm, c.MaxBeamCm = -1, -1 // an area takes any size its length allows
			continue
		}
		if c.MaxLOACm >= 0 && geo.Cm(r.MaxLOACm) > c.MaxLOACm {
			c.MaxLOACm = geo.Cm(r.MaxLOACm)
		}
		if c.MaxBeamCm >= 0 && geo.Cm(r.MaxBeamCm) > c.MaxBeamCm {
			c.MaxBeamCm = geo.Cm(r.MaxBeamCm)
		}
	}
	for _, c := range byCode {
		if c.MaxLOACm < 0 {
			c.MaxLOACm, c.MaxBeamCm = 0, 0
		}
		out.categories = append(out.categories, *c)
	}
	rank := func(code string) int {
		if i, ok := order[code]; ok {
			return i
		}
		if code == CategoryStandard {
			return 1 << 20
		}
		return 1 << 19
	}
	slices.SortStableFunc(out.categories, func(a, b Category) int {
		if d := rank(a.Code) - rank(b.Code); d != 0 {
			return d
		}
		if a.Code < b.Code {
			return -1
		}
		return 1
	})
	return out, nil
}

// fitsAny reports whether some online resource could ever take the vessel (dimension limits only, no dates).
func (c *catalogue) fitsAny(v Vessel) bool {
	for _, r := range c.resources {
		if r.Type == store.ResourceArea {
			return true
		}
		if (r.MaxLOACm == 0 || geo.Cm(r.MaxLOACm) >= v.LOACm) && (r.MaxBeamCm == 0 || geo.Cm(r.MaxBeamCm) >= v.BeamCm) {
			return true
		}
	}
	return false
}

// Config returns the public configuration of the booking page. A disabled marina returns Enabled false (not an
// error); an unknown slug is ErrLinkInvalid.
func (s *Service) Config(ctx context.Context, slug string) (*Config, error) {
	mc, err := s.resolve(ctx, slug, true)
	if err != nil {
		return nil, err
	}
	out := &Config{Currency: mc.m.Currency, Settings: onlineconf.Defaults(), Categories: []Category{}, Services: []ServiceOption{}, Texts: []TextRef{}}
	if out.Currency == "" {
		out.Currency = "EUR"
	}
	if !mc.m.BookingEnabled {
		return out, nil
	}
	db := s.d.DB.DB
	out.Enabled, out.Settings = true, mc.set
	if s.d.Collect != nil {
		_, perr := s.d.Collect.OnlineCheckoutProvider(ctx, db, mc.scope)
		out.Payable = perr == nil
	}
	// Without a checkout provider, allow_without_provider still takes bookings: /pay moves them to approval
	// (bank transfer and manual driver, m8-plan §5.6, D7), so the page must let the customer go on.
	out.Payable = out.Payable || mc.set.AllowWithoutProvider
	cat, err := s.loadCatalogue(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	out.Categories = cat.categories
	items, err := store.OnlineServiceItems(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out.Services = append(out.Services, ServiceOption{ItemID: it.ID, Name: nameOf(it.Name), Description: nameOf(it.Description), Unit: it.Unit})
	}
	texts, err := onlineconf.CurrentTexts(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	for _, t := range texts {
		out.Texts = append(out.Texts, TextRef{Kind: t.Kind, Version: t.Version, Text: t.Texts})
	}
	// Without published terms and privacy the details step refuses every booking (consentTexts).
	out.Payable = out.Payable && requiredTextsPublished(texts)
	return out, nil
}

// checkVessel validates declared dimensions (m8-plan §4.3).
func checkVessel(v Vessel) error {
	switch {
	case v.LOACm <= 0 || v.LOACm > policy.MaxVesselLOACm:
		return &RulesError{Field: "loa_cm", Code: policy.BlockTooLarge}
	case v.BeamCm <= 0 || v.BeamCm > policy.MaxVesselLOACm:
		return &RulesError{Field: "beam_cm", Code: "invalid"}
	case v.DraftCm < 0 || v.DraftCm > 5000:
		return &RulesError{Field: "draft_cm", Code: "invalid"}
	}
	return nil
}

// sessionVessel resolves a saved vessel of the session's customer (ErrLinkInvalid otherwise).
func (s *Service) sessionVessel(ctx context.Context, mc *marinaCtx, slug, vesselID, session string) (Vessel, string, error) {
	if s.resolveSession == nil || session == "" || !store.ValidID(vesselID) {
		return Vessel{}, "", ErrLinkInvalid
	}
	customerID, err := s.resolveSession(ctx, slug, session)
	if err != nil {
		return Vessel{}, "", ErrLinkInvalid
	}
	parties, err := store.PartiesOfVessels(ctx, s.d.DB.DB, mc.scope, []string{vesselID})
	if err != nil {
		return Vessel{}, "", err
	}
	if !slices.ContainsFunc(parties, func(p store.VesselParty) bool { return p.CustomerID == customerID }) {
		return Vessel{}, "", ErrLinkInvalid
	}
	v, err := store.GetVessel(ctx, s.d.DB.DB, mc.scope, vesselID)
	if err != nil {
		return Vessel{}, "", ErrLinkInvalid
	}
	return Vessel{LOACm: geo.Cm(v.LOACm), BeamCm: geo.Cm(v.BeamCm), DraftCm: geo.Cm(v.DraftCm), HullType: v.HullType}, customerID, nil
}

func fitVessel(v Vessel) *fit.Vessel {
	fv := &fit.Vessel{LOA: v.LOACm, Beam: v.BeamCm, Hull: v.HullType}
	if v.DraftCm > 0 {
		d := v.DraftCm
		fv.Draft = &d
	}
	return fv
}

// blockReason applies the stay rules and the marina-wide closures to a stay ("" = not blocked).
func (s *Service) blockReason(ctx context.Context, mc *marinaCtx, arrival, departure calendar.Date) (string, error) {
	if arrival.IsZero() || departure.IsZero() {
		return policy.BlockMinNights, nil
	}
	chk := policy.CheckStay(stayRules(mc.set), policy.Stay{Arrival: arrival, Departure: departure, Now: s.d.Clock.Now(), Loc: mc.loc})
	if !chk.OK {
		return chk.Reason, nil
	}
	cls, err := store.ListClosures(ctx, s.d.DB.DB, mc.scope, arrival, arrival)
	if err != nil {
		return "", err
	}
	for _, c := range cls {
		if c.ScopeType == store.ClosureMarina && !arrival.Before(c.FromDate) && !arrival.After(c.ToDate) {
			return policy.BlockClosed, nil
		}
	}
	return "", nil
}

// search runs the online search of a stay (channel online or external), best solutions first.
func (s *Service) search(ctx context.Context, scope store.MarinaScope, channel string, arrival, departure calendar.Date, v Vessel) ([]occupancy.Solution, error) {
	zero := 0
	res, err := s.d.Occupancy.SearchScoped(ctx, scope, occupancy.SearchInput{
		Purpose: engine.PurposeTransit, Vessel: fitVessel(v), Arrival: arrival, Departure: departure, MaxSegments: &zero,
		Limit: 200, Channel: channel,
	})
	if err != nil {
		return nil, err
	}
	return res.Solutions, nil
}

// byCategory groups solutions by category, keeping the search order inside each category.
func byCategory(cat *catalogue, sols []occupancy.Solution) map[string][]occupancy.Solution {
	out := map[string][]occupancy.Solution{}
	for _, sol := range sols {
		if len(sol.Segments) != 1 {
			continue
		}
		code, ok := cat.classOf[sol.Segments[0].ResourceID]
		if !ok {
			continue
		}
		out[code] = append(out[code], sol)
	}
	return out
}

// placement converts a search solution to the contract segment.
func placement(sol occupancy.Solution) contract.Segment {
	g := sol.Segments[0]
	seg := contract.Segment{ResourceType: string(g.ResourceType), ResourceID: g.ResourceID, First: g.First, Last: g.Last}
	if g.Placement != nil {
		x, y, r := int64(g.Placement.X), int64(g.Placement.Y), int64(g.Placement.Rotation)
		seg.X, seg.Y, seg.RotationDd = &x, &y, &r
	}
	return seg
}

func newVessel(v Vessel, name string) contract.NewVessel {
	if name == "" {
		name = "—"
	}
	return contract.NewVessel{Name: name, LOACm: int64(v.LOACm), BeamCm: int64(v.BeamCm), HullType: v.HullType, DraftCm: int64(v.DraftCm)}
}

// Availability runs one online search and groups the solutions by category (m8-plan §3.2); an anonymous search
// with no availability and no rule block records lost demand (§9) without failing the search.
func (s *Service) Availability(ctx context.Context, slug string, in AvailabilityInput) (*Availability, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	v := in.Vessel
	if in.VesselID != "" {
		if v, _, err = s.sessionVessel(ctx, mc, slug, in.VesselID, in.SessionToken); err != nil {
			return nil, err
		}
	}
	if err := checkVessel(v); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	cat, err := s.loadCatalogue(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	out := &Availability{Arrival: in.Arrival, Departure: in.Departure, Categories: []CategoryAvailability{}}
	if !in.Arrival.IsZero() && in.Departure.After(in.Arrival) {
		out.Nights = in.Arrival.DaysUntil(in.Departure)
	}
	unavailable := func() {
		for _, c := range cat.categories {
			out.Categories = append(out.Categories, CategoryAvailability{Code: c.Code, Name: c.Name})
		}
	}
	reason, err := s.blockReason(ctx, mc, in.Arrival, in.Departure)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		out.Reason = reason
		unavailable()
		return out, nil
	}
	if !cat.fitsAny(v) {
		out.Reason = policy.BlockTooLarge
		unavailable()
		s.recordLostDemand(ctx, mc, in, store.InquiryDeclinedSize)
		return out, nil
	}
	sols, err := s.search(ctx, mc.scope, occupancy.ChannelOnline, in.Arrival, in.Departure, v)
	if err != nil {
		return nil, err
	}
	groups, err := store.GroupsOfResources(ctx, db, mc.scope, "")
	if err != nil {
		return nil, err
	}
	grouped := byCategory(cat, sols)
	found := false
	for _, c := range cat.categories {
		ca := CategoryAvailability{Code: c.Code, Name: c.Name}
		if list := grouped[c.Code]; len(list) > 0 {
			best := list[0]
			ca.Available, found = true, true
			ca.ApprovalRequired = policy.Approval(approvalRules(mc.set), policy.ApprovalInput{
				LOACm: v.LOACm, GroupIDs: groups[best.Segments[0].ResourceID],
			}).Required
			if p, err := s.d.Contracts.PriceOnline(ctx, mc.scope, contract.OnlinePriceInput{
				Channel: contract.ChannelOnline, Arrival: in.Arrival, Departure: in.Departure, Placement: placement(best),
				Vessel: ptr(newVessel(v, "")),
			}); err == nil {
				ca.FromCents = money.Cents(p.Total())
			}
		}
		out.Categories = append(out.Categories, ca)
	}
	if !found {
		s.recordLostDemand(ctx, mc, in, store.InquiryDeclinedNoSpace)
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// quoteOf prices a stay on a solution and builds the public quote.
func (s *Service) quoteOf(ctx context.Context, mc *marinaCtx, channel string, arrival, departure calendar.Date, v Vessel,
	sol occupancy.Solution, category string, services []ServiceRequest, promo string,
) (*Quote, *contract.OnlinePrice, error) {
	p, err := s.d.Contracts.PriceOnline(ctx, mc.scope, contract.OnlinePriceInput{
		Channel: channel, Arrival: arrival, Departure: departure, Placement: placement(sol), Vessel: ptr(newVessel(v, "")),
		Services: contractServices(services), PromoCode: promo,
	})
	if err != nil {
		return nil, nil, err
	}
	b := p.Breakdown()
	q := &Quote{
		Category: category, Nights: b.Nights, TotalCents: b.Total, NetCents: b.Net, VATCents: b.VAT,
		PaymentMode: mc.set.PaymentMode, CancellationTiers: mc.set.CancellationTiers, Currency: mc.m.Currency,
		PromoApplied: promo != "" && b.DiscountTotal != 0, Lines: []PriceLine{},
	}
	if q.Currency == "" {
		q.Currency = "EUR"
	}
	q.DueNowCents = policy.Deposit(policy.DepositInput{Mode: mc.set.PaymentMode, BP: mc.set.DepositBP, Total: b.Total, PriceListDeposit: b.Deposit})
	ids := make([]string, 0, len(b.Lines))
	for _, l := range b.Lines {
		if l.ItemID != "" {
			ids = append(ids, l.ItemID)
		}
	}
	names := map[string]Name{}
	if items, err := store.ItemsByIDs(ctx, s.d.DB.DB, mc.scope, ids); err == nil {
		for _, it := range items {
			names[it.ID] = nameOf(it.Name)
		}
	}
	for _, l := range b.Lines {
		kind := "service"
		if l.Kind == "berth" {
			kind = "berth"
		}
		q.Lines = append(q.Lines, PriceLine{Kind: kind, Label: names[l.ItemID], ItemID: l.ItemID, QtyMilli: int64(l.Qty), TotalCents: l.Gross})
	}
	return q, p, nil
}

func contractServices(in []ServiceRequest) []contract.ServiceRequest {
	out := make([]contract.ServiceRequest, 0, len(in))
	for _, sv := range in {
		out = append(out, contract.ServiceRequest{ItemID: sv.ItemID, QtyMilli: sv.QtyMilli})
	}
	return out
}

// checkServices keeps only online visible service items with a positive quantity.
func (s *Service) checkServices(ctx context.Context, scope store.MarinaScope, in []ServiceRequest) ([]ServiceRequest, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > 20 {
		return nil, &RulesError{Field: "services", Code: "too_many"}
	}
	items, err := store.OnlineServiceItems(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	ok := map[string]bool{}
	for _, it := range items {
		ok[it.ID] = true
	}
	out := make([]ServiceRequest, 0, len(in))
	for _, sv := range in {
		if !ok[sv.ItemID] || sv.QtyMilli <= 0 || sv.QtyMilli > 1_000_000 {
			return nil, &RulesError{Field: "services", Code: "invalid"}
		}
		out = append(out, sv)
	}
	return out, nil
}

// Quote prices a stay in a category with services and promo code (pricing channel online). It writes nothing.
func (s *Service) Quote(ctx context.Context, slug string, in QuoteInput) (*Quote, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	if err := checkVessel(in.Vessel); err != nil {
		return nil, err
	}
	if reason, err := s.blockReason(ctx, mc, in.Arrival, in.Departure); err != nil {
		return nil, err
	} else if reason != "" {
		return nil, &RulesError{Field: "arrival", Code: reason}
	}
	services, err := s.checkServices(ctx, mc.scope, in.Services)
	if err != nil {
		return nil, err
	}
	cat, err := s.loadCatalogue(ctx, s.d.DB.DB, mc.scope)
	if err != nil {
		return nil, err
	}
	sols, err := s.search(ctx, mc.scope, occupancy.ChannelOnline, in.Arrival, in.Departure, in.Vessel)
	if err != nil {
		return nil, err
	}
	grouped := byCategory(cat, sols)
	list := grouped[in.Category]
	if len(list) == 0 {
		return nil, unavailableErr(cat, grouped)
	}
	q, _, err := s.quoteOf(ctx, mc, contract.ChannelOnline, in.Arrival, in.Departure, in.Vessel, list[0], in.Category, services, in.PromoCode)
	if err != nil {
		return nil, err
	}
	groups, err := store.GroupsOfResources(ctx, s.d.DB.DB, mc.scope, "")
	if err != nil {
		return nil, err
	}
	q.ApprovalRequired = policy.Approval(approvalRules(mc.set), policy.ApprovalInput{LOACm: in.Vessel.LOACm, GroupIDs: groups[list[0].Segments[0].ResourceID]}).Required
	return q, nil
}

func unavailableErr(cat *catalogue, grouped map[string][]occupancy.Solution) error {
	e := &UnavailableError{AvailableCategories: []string{}}
	for _, c := range cat.categories {
		if len(grouped[c.Code]) > 0 {
			e.AvailableCategories = append(e.AvailableCategories, c.Code)
		}
	}
	return e
}
