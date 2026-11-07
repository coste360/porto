package contract

import (
	"context"
	"regexp"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Transits (m3-plan §4.2): created held (desk hold of desk_hold_hours) or confirmed with the manual driver,
// cancelled, no-show, stay changes, and the presence hooks of hooks.go.

// maxTransitNights bounds a transit stay.
const maxTransitNights = 366

var localTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// TransitInput creates a transit (createTransit).
type TransitInput struct {
	VesselID            string
	NewVessel           *NewVessel
	CustomerID          string
	Arrival, Departure  calendar.Date // departure exclusive
	Placements          []Segment
	Channel             string // desk (default), api
	ExpectedArrivalTime string
	Notes               string
	Services            []ServiceRequest
	FitOverride         *FitOverrideInput
	Confirm             *ManualConfirmation
	InquiryID           string
}

func (in *TransitInput) check(maxSegs int) error {
	v := &auth.ValidationError{}
	switch {
	case in.VesselID == "" && in.NewVessel == nil:
		v.Add("vessel_id", "required")
	case in.VesselID != "" && in.NewVessel != nil:
		v.Add("new_vessel", "not_allowed")
	case in.NewVessel != nil:
		checkNewVessel(v, "new_vessel", in.NewVessel)
	}
	if in.Channel == "" {
		in.Channel = ChannelDesk
	}
	if in.Channel != ChannelDesk && in.Channel != ChannelAPI {
		v.Add("channel", "invalid")
	}
	checkStay(v, in.Arrival, in.Departure, "arrival", "departure", maxTransitNights)
	checkSegments(v, "placements", in.Placements, in.Arrival, in.Departure, maxSegs)
	if in.ExpectedArrivalTime != "" && !localTime.MatchString(in.ExpectedArrivalTime) {
		v.Add("expected_arrival_time", "invalid")
	}
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	in.Notes = notes
	if in.FitOverride != nil && cleanReason(in.FitOverride) == "" {
		v.Add("fit_override.reason", "required")
	}
	if in.Confirm != nil && in.CustomerID == "" {
		return ErrCustomerRequired
	}
	if len(in.Services) > 50 {
		v.Add("services", "too_many")
	}
	return v.Err()
}

func cleanReason(in *FitOverrideInput) string {
	r, _ := cleanText(in.Reason, 500)
	in.Reason = r
	return r
}

// checkParties verifies that the vessel and the customers belong to the marina (another marina's ids are 404).
func checkParties(ctx context.Context, db bun.IDB, scope store.MarinaScope, vesselID string, customerIDs ...string) error {
	if vesselID != "" {
		if _, err := store.GetVessel(ctx, db, scope, vesselID); err != nil {
			return notFound(err)
		}
	}
	var ids []string
	for _, id := range customerIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	cs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for i := range cs {
		found[cs[i].ID] = true
	}
	for _, id := range ids {
		if !found[id] {
			return auth.ErrNotFound
		}
	}
	return nil
}

// CreateTransit creates a held transit, or a confirmed one with the manual driver (transits.desk; a fit
// override also needs occupancy.override_fit).
func (s *Service) CreateTransit(ctx context.Context, p *auth.Principal, marinaID string, in TransitInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(in.FitOverride)); err != nil {
		return nil, err
	}
	st, err := store.ContractOccupancySettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return nil, err
	}
	if err := in.check(st.MaxSegments + 1); err != nil {
		return nil, err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, in.VesselID, in.CustomerID); err != nil {
		return nil, err
	}
	pr := priceRequest{
		contractType: PricingType(KindTransit, ""), arrival: in.Arrival, departure: in.Departure, first: in.Placements[0],
		vesselID: in.VesselID, customerID: in.CustomerID, channel: in.Channel, services: in.Services,
	}
	if in.NewVessel != nil {
		pr.vessel = &pricelist.SimVessel{LOA: geo.Cm(in.NewVessel.LOACm), Beam: geo.Cm(in.NewVessel.BeamCm), HullType: in.NewVessel.HullType}
	}
	price, err := s.price(ctx, p, marinaID, pr)
	if err != nil {
		return nil, err
	}
	s.priced()
	a := actor{p}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.createTransitTx(ctx, tx, scope, a, in, price, st, true, nil)
		if err != nil {
			return err
		}
		id = c.ID
		return s.commitHook()
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// createTransitTx writes a held transit (and confirms it when in.Confirm is set) in tx. arrival applies the
// closures to the first night (false for vessels already in the harbour).
func (s *Service) createTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, in TransitInput,
	price *priced, st *store.OccupancySettings, arrival bool, from *fromQuote,
) (*store.Contract, error) {
	t, err := Next(TransitTransitions, StatusNone, ActionCreate)
	if err != nil {
		return nil, err
	}
	now := s.now()
	vesselID := in.VesselID
	if in.NewVessel != nil {
		v, err := s.NewVesselTx(ctx, tx, scope, a.p, *in.NewVessel, now)
		if err != nil {
			return nil, err
		}
		vesselID = v.ID
	}
	exp := now.Add(time.Duration(st.DeskHoldHours) * time.Hour)
	n := newContract{
		kind: KindTransit, status: t.To, customerID: in.CustomerID, vesselID: vesselID,
		startsOn: in.Arrival, endsOn: in.Departure, channel: in.Channel, price: price, policy: defaultPolicy(st),
		notes: in.Notes, expectedArrival: in.ExpectedArrivalTime, segments: in.Placements, services: in.Services,
		allocKind: occupancy.KindHold, expiresAt: &exp, arrival: arrival, fit: fitOverride(a, in.FitOverride),
	}
	if from != nil {
		n.quoteID, n.priceIsBinding, n.fit, n.policy = from.quoteID, true, from.fit, from.policy
	}
	c, allocs, err := s.createTx(ctx, tx, scope, a, n)
	if err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "transit.create", entityTransit, c.ID, map[string]any{
		"status": c.Status, "from": c.StartsOn, "to": c.EndsOn, "allocation_ids": idsOf(allocs), "total_cents": c.TotalCents,
		"channel": c.Channel, "fit_override": in.FitOverride != nil,
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return nil, err
	}
	if err := s.emitAllocated(ctx, tx, scope, allocs, c.ID); err != nil {
		return nil, err
	}
	if in.InquiryID != "" {
		if err := s.linkInquiryTx(ctx, tx, scope, a, in.InquiryID, "booked", "", c.ID); err != nil {
			return nil, err
		}
	}
	if in.Confirm != nil {
		if err := s.confirmTx(ctx, tx, scope, a, c, SourceManual, *in.Confirm); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// CancelTransit cancels a held or confirmed transit and frees its nights (transits.desk, If-Match).
func (s *Service) CancelTransit(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Detail, error) {
	return s.endTransit(ctx, p, marinaID, id, ifMatch, ActionCancel, reason)
}

// MarkNoShow marks a confirmed transit as no-show from its arrival date and frees its nights (transits.desk,
// If-Match; m3-plan §13 D14).
func (s *Service) MarkNoShow(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Detail, error) {
	return s.endTransit(ctx, p, marinaID, id, ifMatch, ActionNoShow, "")
}

func (s *Service) endTransit(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, action Action, reason string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, 500)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("reason", "too_long")
		return nil, v
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindTransit, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(TransitTransitions, Status(c.Status), action)
		if err != nil {
			return err
		}
		if action == ActionNoShow && s.today(ctx, tx, scope).Before(c.StartsOn) {
			return &TransitionError{From: c.Status, Action: action}
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		if err := s.cancelAllocs(ctx, tx, scope, allocs, occupancy.StatusCancelled, event.ReleasedCancel); err != nil {
			return err
		}
		from := c.Status
		now := s.now()
		c.Status = string(t.To)
		if action == ActionCancel {
			c.CancelledAt, c.CancelReason = now, reason
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
			return err
		}
		auditAction, change := "transit.cancel", ""
		if action == ActionNoShow {
			auditAction, change = "transit.no_show", event.ChangeNoShow
		}
		if err := s.writeAudit(ctx, tx, scope, a, auditAction, entityTransit, c.ID, map[string]any{
			"from_status": from, "status": c.Status, "allocation_ids": idsOf(activeOnly(allocs)),
		}); err != nil {
			return err
		}
		return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: change})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// StayChange changes the stay of a transit (changeTransitStay): nil fields keep their value.
type StayChange struct {
	Arrival         *calendar.Date
	Departure       *calendar.Date // exclusive
	ExtensionTarget *Segment       // resource (and placement) for nights added after the current departure
	FitOverride     *FitOverrideInput
}

// PriceEffect is the effect of a change on the stored total.
type PriceEffect struct {
	OldTotalCents, NewTotalCents, DeltaCents int64
	Applied                                  bool
}

// ChangeStay extends, shortens or shifts a held, confirmed or arrived transit (arrived: departure only). Added
// nights must be free (409 resource_taken); the stay is priced again and the snapshot replaced (m3-plan §4.2,
// §4.6).
func (s *Service) ChangeStay(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in StayChange) (*Detail, PriceEffect, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(in.FitOverride)); err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, PriceEffect{}, err
	}
	c, err := store.GetContract(ctx, s.d.DB.DB, scope, id)
	if err != nil || c.Kind != KindTransit {
		return nil, PriceEffect{}, auth.ErrNotFound
	}
	if c.Version != ifMatch {
		return nil, PriceEffect{}, ErrVersionMismatch
	}
	if _, err := Next(TransitTransitions, Status(c.Status), ActionChangeStay); err != nil {
		return nil, PriceEffect{}, err
	}
	na, nd := c.StartsOn, c.EndsOn
	if in.Arrival != nil {
		na = *in.Arrival
	}
	if in.Departure != nil {
		nd = *in.Departure
	}
	v := &auth.ValidationError{}
	if c.Status == string(StatusArrived) && !na.Equal(c.StartsOn) {
		v.Add("arrival", "frozen")
	}
	checkStay(v, na, nd, "arrival", "departure", maxTransitNights)
	if in.ExtensionTarget != nil {
		if !nd.After(c.EndsOn) {
			v.Add("extension_target", "not_extending")
		}
		if na.Before(c.StartsOn) {
			v.Add("arrival", "not_with_extension")
		}
		g := *in.ExtensionTarget
		g.First, g.Last = c.EndsOn, nd.AddDays(-1)
		checkSegments(v, "extension_target", []Segment{g}, c.EndsOn, nd, 1)
		in.ExtensionTarget = &g
	}
	if in.FitOverride != nil && cleanReason(in.FitOverride) == "" {
		v.Add("fit_override.reason", "required")
	}
	if err := v.Err(); err != nil {
		return nil, PriceEffect{}, err
	}
	allocs, err := contractAllocs(ctx, s.d.DB.DB, scope, c.ID)
	if err != nil {
		return nil, PriceEffect{}, err
	}
	active := activeOnly(allocs)
	if len(active) == 0 {
		return nil, PriceEffect{}, occupancy.ErrHoldExpired
	}
	var services []ServiceRequest
	if old := DecodeSnapshot(c.PriceSnapshot); old != nil {
		for _, sv := range old.Request.Services {
			services = append(services, ServiceRequest{ItemID: sv.ItemID, QtyMilli: int64(sv.Qty)})
		}
	}
	price, err := s.price(ctx, p, marinaID, priceRequest{
		contractType: PricingType(KindTransit, ""), arrival: na, departure: nd, first: segmentOf(&active[0]),
		vesselID: c.VesselID, customerID: c.CustomerID, channel: c.Channel, services: services,
	})
	if err != nil {
		return nil, PriceEffect{}, err
	}
	effect := PriceEffect{OldTotalCents: c.TotalCents, NewTotalCents: price.total(), DeltaCents: price.total() - c.TotalCents, Applied: true}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindTransit, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(TransitTransitions, Status(c.Status), ActionChangeStay)
		if err != nil {
			return err
		}
		if err := checkListStillActive(ctx, tx, scope, price); err != nil {
			return err
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		active := activeOnly(allocs)
		if len(active) == 0 {
			return occupancy.ErrHoldExpired
		}
		now := s.now()
		changed, err := s.resizeStayTx(ctx, tx, scope, a, now, c, active, na, nd, in)
		if err != nil {
			return err
		}
		old := map[string]any{"from": c.StartsOn, "to": c.EndsOn, "total_cents": c.TotalCents}
		c.StartsOn, c.EndsOn = na, nd
		c.PriceSnapshot, c.PriceListID, c.EngineVersion = price.raw, price.listID(), price.engineVersion()
		c.TotalCents, c.DepositCents = price.total(), price.deposit()
		if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "transit.stay", entityTransit, c.ID, map[string]any{
			"old": old, "new": map[string]any{"from": na, "to": nd, "total_cents": c.TotalCents},
			"allocation_ids": idsOf(changed),
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeStay}); err != nil {
			return err
		}
		return s.emitAllocated(ctx, tx, scope, changed, c.ID)
	})
	if err != nil {
		return nil, PriceEffect{}, s.withAlternatives(ctx, p, marinaID, c.VesselID, err)
	}
	d, err := s.detail(ctx, s.d.DB.DB, scope, p, id)
	return d, effect, err
}

// resizeStayTx fits the active allocations (sorted by first night, contiguous) to [na, nd): the first starts at
// na, the last ends at nd − 1 (or an extension allocation covers the added nights), allocations outside are
// cancelled. It returns the allocations that changed or were created.
func (s *Service) resizeStayTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, now time.Time,
	c *store.Contract, active []store.Allocation, na, nd calendar.Date, in StayChange,
) ([]store.Allocation, error) {
	lastNight := nd.AddDays(-1)
	var changed []store.Allocation
	k := len(active)
	for i := range active {
		al := active[i]
		f, l := al.FirstNight, al.LastNight
		if i == 0 {
			f = na
		}
		if i == k-1 && in.ExtensionTarget == nil {
			l = lastNight
		}
		if f.Before(na) {
			f = na
		}
		if l.After(lastNight) {
			l = lastNight
		}
		switch {
		case f.After(l):
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusCancelled); err != nil {
				return nil, err
			}
			if err := s.emit(ctx, tx, scope, event.OccupancyReleased, event.OccupancyReleasedData{
				AllocationIDs: []string{al.ID}, ResourceIDs: []string{al.ResourceID}, MapID: al.MapID,
				From: al.FirstNight, To: al.LastNight, Reason: event.ReleasedTruncate,
			}); err != nil {
				return nil, err
			}
		case !f.Equal(al.FirstNight) || !l.Equal(al.LastNight):
			if err := s.alloc.ResizeTx(ctx, tx, scope, now, al.ID, f, l); err != nil {
				return nil, err
			}
			al.FirstNight, al.LastNight = f, l
			changed = append(changed, al)
		}
	}
	if in.ExtensionTarget != nil {
		// The extension continues the last allocation on its contract line (INV-10: the allocations of a line are
		// contiguous and do not overlap), on another resource.
		last := active[k-1]
		g := *in.ExtensionTarget
		var exp *time.Time
		if !last.ExpiresAt.IsZero() {
			e := last.ExpiresAt
			exp = &e
		}
		al, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
			lineID: last.ContractLineID, vesselID: c.VesselID, kind: occupancy.Kind(last.Kind), seg: g, expiresAt: exp,
			fit: fitOverride(a, in.FitOverride), movedFrom: last.ID,
		})
		if err != nil {
			return nil, err
		}
		changed = append(changed, *al)
	}
	return changed, nil
}

// TransitPatch changes the editable fields of a transit (updateTransit); nil keeps the value, "" clears.
type TransitPatch struct {
	CustomerID            *string
	Notes                 *string
	ExpectedArrivalTime   *string
	ExpectedDepartureTime *string
}

// UpdateTransit changes the customer, the notes and the expected times of a held, confirmed or arrived transit
// (transits.desk, If-Match).
func (s *Service) UpdateTransit(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in TransitPatch) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	for _, f := range []struct {
		name string
		val  *string
	}{{"expected_arrival_time", in.ExpectedArrivalTime}, {"expected_departure_time", in.ExpectedDepartureTime}} {
		if f.val != nil && *f.val != "" && !localTime.MatchString(*f.val) {
			v.Add(f.name, "invalid")
		}
	}
	if in.Notes != nil {
		n, ok := cleanText(*in.Notes, 2000)
		if !ok {
			v.Add("notes", "too_long")
		}
		in.Notes = &n
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindTransit, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(TransitTransitions, Status(c.Status), ActionUpdate)
		if err != nil {
			return err
		}
		var fields []string
		if in.CustomerID != nil {
			if *in.CustomerID == "" && c.Status != string(StatusHeld) {
				return ErrCustomerRequired
			}
			if err := checkParties(ctx, tx, scope, "", *in.CustomerID); err != nil {
				return err
			}
			c.CustomerID = *in.CustomerID
			fields = append(fields, "customer_id")
		}
		if in.Notes != nil {
			c.Notes = *in.Notes
			fields = append(fields, "notes")
		}
		if in.ExpectedArrivalTime != nil {
			c.ExpectedArrivalTime = *in.ExpectedArrivalTime
			fields = append(fields, "expected_arrival_time")
		}
		if in.ExpectedDepartureTime != nil {
			c.ExpectedDepartureTime = *in.ExpectedDepartureTime
			fields = append(fields, "expected_departure_time")
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "transit.update", entityTransit, c.ID, map[string]any{"fields": fields}); err != nil {
			return err
		}
		return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeUpdated})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// fromQuote carries what an accepted quote option gives to the contract or transit it creates.
type fromQuote struct {
	quoteID string
	fit     *occupancy.FitOverride
	policy  Policy
}
