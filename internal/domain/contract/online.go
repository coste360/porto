package contract

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Online and channel transits (M8, m8-plan §4-§8). These Tx primitives are the only way the booking, portal and
// channel packages change an online transit: each one locks the contract (lockContract), re-checks the state under
// the lock (PostgreSQL READ COMMITTED), applies one row of TransitTransitions and writes the audit row and the
// outbox events in the caller's transaction (INV-30). Confirmation stays in confirmTx (INV-09): the provider
// webhook (ConfirmTransitPaidTx with SourceProvider, also from awaiting_approval and, with confirm_late, from
// expired), the manual driver, or a verified paid channel reservation (SourceChannel, ADR 0013).

// ApprovalHoldGrace is how long the hold of an awaiting_approval transit outlives its approval deadline: the
// bookings.approval_expire job rejects at the deadline, the hold is still active until then (INV-08).
const ApprovalHoldGrace = 15 * time.Minute

// ErrOnlinePriceChanged means the total of a date change differs from the expected one (409 price_changed).
var ErrOnlinePriceChanged = errors.New("contract: online price changed")

// Who performs an online state change: a desk principal (approve, reject, desk cancel) or one of the non-user
// actors of the public surfaces. Actor is what the audit row records; Principal is nil for public, customer,
// channel and job actors.
type Who struct {
	Principal *auth.Principal
	Actor     audit.Actor
}

// Desk is a desk principal (transits.desk already checked by the caller).
func Desk(p *auth.Principal) Who { return Who{Principal: p, Actor: p.Actor()} }

// PublicActor is the anonymous booking page (audit actor system/"public").
func PublicActor() Who { return Who{Actor: audit.Actor{Type: audit.ActorSystem, ID: "public"}} }

// CustomerActor is a customer acting through the customer area (audit actor system/"customer:<id>").
func CustomerActor(customerID string) Who {
	return Who{Actor: audit.Actor{Type: audit.ActorSystem, ID: "customer:" + customerID}}
}

// ChannelActor is a booking channel (audit actor system/"channel:<code>").
func ChannelActor(code string) Who {
	return Who{Actor: audit.Actor{Type: audit.ActorSystem, ID: "channel:" + code}}
}

// JobActor is a background job (audit actor system/"system:<job>", e.g. system:bookings.approval_expire).
func JobActor(job string) Who {
	return Who{Actor: audit.Actor{Type: audit.ActorSystem, ID: "system:" + job}}
}

func (w Who) actor() actor { return actor{p: w.Principal, as: w.Actor} }

func (w Who) userID() string { return w.actor().userID() }

// auditWho writes an audit row with the actor of who.
func (s *Service) auditWho(ctx context.Context, tx bun.IDB, scope store.MarinaScope, w Who, action, id string, diff any) error {
	e := auditEntry(scope, w.actor(), action, entityTransit, id, diff)
	if w.Actor.Type != "" {
		e.Actor = w.Actor
	}
	return s.d.Audit.Write(ctx, tx, e)
}

// OnlineHooks is implemented by the booking service and called inside the transaction of a transition of an
// online or channel transit (m8-plan §5.4, §6): the confirmation email with the customer-area link, the
// "booking received" email of an approval request. An error rolls the transition back.
type OnlineHooks interface {
	OnOnlineConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) error
	OnOnlineApprovalRequestedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, reason string) error
}

// onlineHooks maps a contract service to its online hooks. It lives here (not in Service) so that the M8 wiring
// stays in this file; SetOnlineHooks is called once by internal/app.
var onlineHooks sync.Map // *Service → OnlineHooks

// SetOnlineHooks registers the booking hooks (nil removes them).
func (s *Service) SetOnlineHooks(h OnlineHooks) {
	if h == nil {
		onlineHooks.Delete(s)
		return
	}
	onlineHooks.Store(s, h)
}

func (s *Service) hooks() OnlineHooks {
	if h, ok := onlineHooks.Load(s); ok {
		if oh, ok := h.(OnlineHooks); ok {
			return oh
		}
	}
	return nil
}

// IsOnline reports whether a contract is an online or channel transit.
func IsOnline(c *store.Contract) bool {
	return c != nil && c.Kind == KindTransit && occupancy.QuotaChannel(c.Channel)
}

// releaseQuotaTx releases the online quota claims of an online or channel contract (all nights).
func (s *Service) releaseQuotaTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) error {
	if !IsOnline(c) || s.d.Occupancy == nil {
		return nil
	}
	return s.d.Occupancy.ReleaseQuotaTx(ctx, tx, scope, c.ID, nil)
}

// onlineInquiryTx sets the outcome of the inquiry linked to an online booking (lost demand funnel, m8-plan §9).
func (s *Service) onlineInquiryTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, outcome string) error {
	if !IsOnline(c) {
		return nil
	}
	ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil || ob.InquiryID == "" {
		return err
	}
	return s.linkInquiryTx(ctx, tx, scope, system, ob.InquiryID, outcome, "", c.ID)
}

// emitOnline writes the events of an online transition: transit.approval_requested carries its reason, the other
// types go through emitTransition.
func (s *Service) emitOnline(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t Transition, sub subject, reason string) error {
	for _, typ := range t.Events {
		if typ == event.TransitApprovalRequested {
			if err := s.emit(ctx, tx, scope, typ, event.TransitApprovalData{ContractID: sub.contract.ID, Reason: reason}); err != nil {
				return err
			}
			continue
		}
		if err := s.emitTransition(ctx, tx, scope, Transition{From: t.From, Action: t.Action, To: t.To, Events: []string{typ}}, sub); err != nil {
			return err
		}
	}
	return nil
}

// ---- Prices ----

// OnlinePrice is a price of an online stay computed before the write transaction (pricing channel online or
// external, with services and promo code). It is passed to CreateOnlineTransitTx or ChangeOnlineStayTx.
type OnlinePrice struct {
	p *priced
}

// Breakdown is the engine breakdown of the price.
func (o *OnlinePrice) Breakdown() pricing.Breakdown { return o.p.snapshot.Breakdown }

// Resolved is what the quote resolved (price list, category, groups).
func (o *OnlinePrice) Resolved() pricelist.Resolved { return o.p.snapshot.Resolved }

// Total is the gross total in cents.
func (o *OnlinePrice) Total() int64 { return o.p.total() }

// Deposit is the price-list deposit in cents (0 = none).
func (o *OnlinePrice) Deposit() int64 { return o.p.deposit() }

// OnlinePriceInput is an online stay to price.
type OnlinePriceInput struct {
	Channel            string // online, external
	Arrival, Departure calendar.Date
	Placement          Segment
	VesselID           string // a stored vessel, or
	Vessel             *NewVessel
	CustomerID         string
	Services           []ServiceRequest
	PromoCode          string
}

// scopedPrincipal is a principal limited to perms in one marina, for the public surfaces that act on behalf of a
// marina without a user (the M4 public pages do the same).
func scopedPrincipal(scope store.MarinaScope, perms ...auth.Permission) *auth.Principal {
	return &auth.Principal{
		Kind: auth.KindAPIKey, Grants: []auth.Grant{{OrganizationID: scope.OrgID(), MarinaID: scope.ID()}}, Scopes: perms,
	}
}

// PriceOnline quotes an online stay with the pricing channel of in.Channel (outside any transaction).
func (s *Service) PriceOnline(ctx context.Context, scope store.MarinaScope, in OnlinePriceInput) (*OnlinePrice, error) {
	ch := pricing.ChannelOnline
	if in.Channel == ChannelExternal {
		ch = pricing.ChannelExternal
	}
	req := pricelist.QuoteRequest{
		ContractType: pricing.ContractTransit, Arrival: in.Arrival, Departure: in.Departure, VesselID: in.VesselID,
		Resource:   &pricelist.SimResource{Type: in.Placement.ResourceType, ID: in.Placement.ResourceID},
		CustomerID: in.CustomerID, Channel: ch, PromoCode: in.PromoCode,
	}
	if in.Vessel != nil {
		req.VesselID = ""
		req.Vessel = &pricelist.SimVessel{LOA: geo.Cm(in.Vessel.LOACm), Beam: geo.Cm(in.Vessel.BeamCm), HullType: in.Vessel.HullType}
	}
	for _, sv := range in.Services {
		req.Services = append(req.Services, pricing.ServiceRequest{ItemID: sv.ItemID, Qty: qty.Milli(sv.QtyMilli)})
	}
	b, res, err := s.d.Prices.Quote(ctx, scopedPrincipal(scope, auth.PermPricingRead), scope.ID(), req)
	if err != nil {
		return nil, err
	}
	req.PriceListID = res.PriceListID
	q := res.QuotedOn
	req.QuotedOn = &q
	if req.Vessel == nil && res.LOA > 0 {
		req.Vessel = &pricelist.SimVessel{LOA: res.LOA, Beam: res.Beam, HullType: res.HullType}
		req.VesselID = ""
	}
	snap := PriceSnapshot{Version: SnapshotVersion, EngineVersion: b.EngineVersion, Request: req, Resolved: res, Breakdown: b}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	return &OnlinePrice{p: &priced{snapshot: snap, raw: raw}}, nil
}

// ---- Creation ----

// OnlineBookingRow carries the online_bookings columns decided by the booking package (reference, token nonce,
// domain idempotency, locale, embed origin, the inquiry of the lost-demand funnel).
type OnlineBookingRow struct {
	ID                string // UUIDv7, chosen by the caller (the booking token is HMAC(id ‖ nonce))
	Reference         string
	TokenNonce        string
	IdempotencyHash   string
	BodySHA256        string
	Locale            string
	EmbedParentOrigin string
	InquiryID         string
}

// OnlineTransitInput creates an online (channel online) or channel (channel external) transit held on one concrete
// solution chosen by occupancy.SearchScoped (m8-plan §4.3).
type OnlineTransitInput struct {
	Channel            string // ChannelOnline or ChannelExternal
	Vessel             NewVessel
	Arrival, Departure calendar.Date // departure exclusive
	Placement          Segment       // the resource (and area placement) of the solution, First/Last = the stay
	Services           []ServiceRequest
	PromoCode          string
	// HoldUntil is the expiry of the hold allocation (now + PORTO_HOLD_TTL online, 24 h for unpaid channel
	// reservations).
	HoldUntil time.Time
	// Policy is the online policy snapshot (an OnlinePolicy as JSON): online.cancellation_tiers, payment_mode,
	// deposit_bp, refund_as, reveal_berth.
	Policy  store.JSON
	Booking OnlineBookingRow
	// Price is the price computed by PriceOnline before the transaction (nil: priced here, outside tx reads).
	Price *OnlinePrice
	// CustomerID is set for channel reservations whose guest is already resolved (optional).
	CustomerID string
}

// OnlineTransit is the outcome of CreateOnlineTransitTx.
type OnlineTransit struct {
	Contract    *store.Contract
	Booking     *store.OnlineBooking
	Allocations []store.Allocation
}

func (in *OnlineTransitInput) check() error {
	v := &auth.ValidationError{}
	if in.Channel != ChannelOnline && in.Channel != ChannelExternal {
		v.Add("channel", "invalid")
	}
	checkNewVessel(v, "vessel", &in.Vessel)
	checkStay(v, in.Arrival, in.Departure, "arrival", "departure", maxTransitNights)
	g := in.Placement
	g.First, g.Last = in.Arrival, in.Departure.AddDays(-1)
	in.Placement = g
	if len(v.Fields) == 0 {
		checkSegments(v, "placement", []Segment{g}, in.Arrival, in.Departure, 1)
	}
	if in.Placement.ResourceType == store.ResourceLand {
		v.Add("placement.resource_type", "invalid")
	}
	if in.Booking.ID == "" || in.Booking.Reference == "" || in.Booking.TokenNonce == "" {
		v.Add("booking", "required")
	}
	if in.HoldUntil.IsZero() {
		v.Add("hold_until", "required")
	}
	return v.Err()
}

// CreateOnlineTransitTx creates the vessel from the declared dimensions, the held transit with the online or
// external channel and the binding price snapshot (pricing channel online, INV-20), claims the online quotas
// (occupancy.ClaimQuotaTx) and the hold allocation (occupancy.AllocateTx, kind hold) of in.Placement, inserts the
// online_bookings row, and writes audit and transit.held / occupancy.allocated, all in tx. A taken resource is
// occupancy.ErrResourceTaken and an exhausted quota occupancy.ErrQuotaExhausted: the caller rolls back and tries
// the next solution.
func (s *Service) CreateOnlineTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	in OnlineTransitInput,
) (*OnlineTransit, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if s.d.Occupancy == nil {
		return nil, errors.New("contract: occupancy service not wired")
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	price := in.Price
	if price == nil {
		var err error
		price, err = s.PriceOnline(ctx, scope, OnlinePriceInput{
			Channel: in.Channel, Arrival: in.Arrival, Departure: in.Departure, Placement: in.Placement, Vessel: &in.Vessel,
			Services: in.Services, PromoCode: in.PromoCode,
		})
		if err != nil {
			return nil, err
		}
	}
	var op OnlinePolicy
	if len(in.Policy) > 0 {
		if err := json.Unmarshal(in.Policy, &op); err != nil {
			return nil, err
		}
	}
	t, err := Next(TransitTransitions, StatusNone, ActionCreate)
	if err != nil {
		return nil, err
	}
	now := s.now()
	st, err := store.ContractOccupancySettings(ctx, tx, scope, now)
	if err != nil {
		return nil, err
	}
	a := who.actor()
	v, err := s.NewVesselTx(ctx, tx, scope, who.Principal, in.Vessel, now)
	if err != nil {
		return nil, err
	}
	pol := defaultPolicy(st)
	pol.Online = &op
	exp := store.UTC(in.HoldUntil)
	n := newContract{
		kind: KindTransit, status: t.To, customerID: in.CustomerID, vesselID: v.ID, startsOn: in.Arrival,
		endsOn: in.Departure, channel: in.Channel, price: price.p, policy: pol, segments: []Segment{in.Placement},
		services: in.Services, allocKind: occupancy.KindHold, expiresAt: &exp, arrival: true,
	}
	c, allocs, err := s.createTx(ctx, tx, scope, a, n)
	if err != nil {
		return nil, err
	}
	if err := s.d.Occupancy.ClaimQuotaTx(ctx, tx, scope, c.ID, occupancy.QuotaResource{
		ResourceType: in.Placement.ResourceType, ResourceID: in.Placement.ResourceID,
	}, in.Arrival, in.Departure.AddDays(-1), in.Channel); err != nil {
		return nil, err
	}
	ob := &store.OnlineBooking{
		ID: in.Booking.ID, MarinaID: scope.ID(), ContractID: c.ID, Channel: in.Channel, Reference: in.Booking.Reference,
		TokenNonce: in.Booking.TokenNonce, IdempotencyHash: in.Booking.IdempotencyHash, BodySHA256: in.Booking.BodySHA256,
		EmbedParentOrigin: in.Booking.EmbedParentOrigin, Locale: in.Booking.Locale, InquiryID: in.Booking.InquiryID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if ob.Locale == "" {
		ob.Locale = "it"
	}
	if err := store.InsertOnlineBooking(ctx, tx, ob); err != nil {
		return nil, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit.create", c.ID, map[string]any{
		"status": c.Status, "from": c.StartsOn, "to": c.EndsOn, "allocation_ids": idsOf(allocs), "total_cents": c.TotalCents,
		"channel": c.Channel, "online_booking_id": ob.ID,
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return nil, err
	}
	if err := s.emitAllocated(ctx, tx, scope, allocs, c.ID); err != nil {
		return nil, err
	}
	return &OnlineTransit{Contract: c, Booking: ob, Allocations: allocs}, nil
}

// ---- Details ----

// OnlineCustomer is the customer and vessel data of the details step (m8-plan §5.1). CustomerID is the reused or
// newly created customer (the booking package resolves it); the vessel becomes owned by that customer.
type OnlineCustomer struct {
	CustomerID          string
	VesselName          string
	Registration        string
	Flag                string
	HullType            string
	DraftCm             int64 // 0 = unchanged
	ExpectedArrivalTime string
	Notes               string
}

// SetOnlineCustomerTx sets the customer, the vessel details, the expected arrival time and the notes of a held
// online transit (ActionUpdate, transit.changed). Any status other than held is ErrInvalidTransition.
func (s *Service) SetOnlineCustomerTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, in OnlineCustomer,
) (*store.Contract, error) {
	v := &auth.ValidationError{}
	name, ok := cleanText(in.VesselName, 120)
	if name == "" {
		v.Add("vessel.name", "required")
	} else if !ok {
		v.Add("vessel.name", "too_long")
	}
	if in.ExpectedArrivalTime != "" && !localTime.MatchString(in.ExpectedArrivalTime) {
		v.Add("expected_arrival_time", "invalid")
	}
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	if in.HullType != "" && !contains(hullTypes, in.HullType) {
		v.Add("vessel.hull_type", "invalid")
	}
	if in.DraftCm < 0 || in.DraftCm > 5000 {
		v.Add("vessel.draft_cm", "out_of_range")
	}
	if in.CustomerID == "" {
		v.Add("customer_id", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return nil, err
	}
	if !IsOnline(c) || c.Status != string(StatusHeld) {
		return nil, &TransitionError{From: c.Status, Action: ActionUpdate}
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionUpdate)
	if err != nil {
		return nil, err
	}
	if err := checkParties(ctx, tx, scope, c.VesselID, in.CustomerID); err != nil {
		return nil, err
	}
	now := s.now()
	vs, err := store.GetVessel(ctx, tx, scope, c.VesselID)
	if err != nil {
		return nil, notFound(err)
	}
	vs.Name, vs.Registration, vs.Flag = name, limitText(in.Registration, 40), limitText(in.Flag, 3)
	if in.HullType != "" {
		vs.HullType = in.HullType
	}
	if in.DraftCm > 0 {
		vs.DraftCm = in.DraftCm
	}
	vs.UpdatedAt = now
	if err := store.UpdateVessel(ctx, tx, scope, vs.Version, vs); err != nil {
		return nil, versionErr(err)
	}
	if err := store.ReplaceVesselParties(ctx, tx, scope, vs.ID, []store.VesselParty{{
		ID: store.NewID(), MarinaID: scope.ID(), VesselID: vs.ID, CustomerID: in.CustomerID, Role: "owner", ShareMillis: 1000,
		CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.VesselChanged, event.VesselChangedData{VesselID: vs.ID, Change: event.ChangeUpdated}); err != nil {
		return nil, err
	}
	c.CustomerID, c.ExpectedArrivalTime, c.Notes = in.CustomerID, in.ExpectedArrivalTime, notes
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit.update", c.ID, map[string]any{
		"fields": []string{"customer_id", "expected_arrival_time", "notes", "vessel"},
	}); err != nil {
		return nil, err
	}
	return c, s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeUpdated})
}

func limitText(s string, n int) string {
	t, _ := cleanText(s, n)
	if len([]rune(t)) > n {
		return string([]rune(t)[:n])
	}
	return t
}

// ---- Holds ----

// ExtendHoldTx sets the expiry of the live holds of a transit to until under the contract lock. A hold already
// expired and reclaimed is allocated again (convertOrReallocate semantics) or the call fails with
// occupancy.ErrHoldExpired (409 hold_expired).
func (s *Service) ExtendHoldTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string,
	until time.Time,
) error {
	if err := s.requireAllocator(); err != nil {
		return err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return err
	}
	return s.extendOnlineHoldsTx(ctx, tx, scope, c, until)
}

// extendOnlineHoldsTx is ExtendHoldTx on a locked contract.
func (s *Service) extendOnlineHoldsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, until time.Time) error {
	now := s.now()
	until = store.UTC(until)
	if !until.After(now) {
		return occupancy.ErrHoldExpired
	}
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	var live []string
	for i := range allocs {
		al := &allocs[i]
		if al.Status != store.AllocActive {
			continue
		}
		if al.Kind != store.AllocKindHold {
			return occupancy.ErrAllocationNotActive
		}
		if !al.ExpiresAt.After(now) {
			// Expired but not reclaimed yet: lost, allocated again below.
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusExpired); err != nil {
				return err
			}
			al.Status = store.AllocExpired
			continue
		}
		live = append(live, al.ID)
	}
	if err := s.alloc.ConvertTx(ctx, tx, scope, now, live, occupancy.KindHold, &until); err != nil {
		return err
	}
	var again []store.Allocation
	for _, al := range lapsedChainEnds(allocs) {
		n, err := s.allocateLine(ctx, tx, scope, system, now, allocation{
			lineID: al.ContractLineID, vesselID: c.VesselID, kind: occupancy.KindHold, seg: segmentOf(&al), expiresAt: &until,
			movedFrom: al.ID,
		})
		if err != nil {
			if _, ok := errors.AsType[*occupancy.ConflictError](err); ok {
				return occupancy.ErrHoldExpired
			}
			return err
		}
		again = append(again, *n)
	}
	if len(live) == 0 && len(again) == 0 {
		return occupancy.ErrHoldExpired
	}
	return s.emitAllocated(ctx, tx, scope, again, c.ID)
}

// ---- Approval ----

// approvalTx applies request_approval (held) or payment_authorized (pending_payment): holds extended to the
// deadline plus ApprovalHoldGrace, reason and deadline on online_bookings, transit.approval_requested.
func (s *Service) approvalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who, contractID, reason string,
	deadline time.Time, action Action,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if !contains([]string{store.ApprovalReasonLOA, store.ApprovalReasonGroup, store.ApprovalReasonAlways, store.ApprovalReasonNoProvider}, reason) {
		return nil, errors.New("contract: invalid approval reason")
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return nil, err
	}
	if !IsOnline(c) {
		return nil, &TransitionError{From: c.Status, Action: action}
	}
	t, err := Next(TransitTransitions, Status(c.Status), action)
	if err != nil {
		return nil, err
	}
	ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, notFound(err)
	}
	if err := s.extendOnlineHoldsTx(ctx, tx, scope, c, deadline.Add(ApprovalHoldGrace)); err != nil {
		return nil, err
	}
	now := s.now()
	from := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	ob.ApprovalReason, ob.ApprovalDeadline = reason, store.UTC(deadline)
	if err := store.UpdateOnlineBooking(ctx, tx, scope, ob, now); err != nil {
		return nil, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit."+string(action), c.ID, map[string]any{
		"from_status": from, "status": c.Status, "reason": reason, "deadline": ob.ApprovalDeadline,
	}); err != nil {
		return nil, err
	}
	if err := s.emitOnline(ctx, tx, scope, t, subject{contract: c, change: string(StatusAwaitingApproval)}, reason); err != nil {
		return nil, err
	}
	if h := s.hooks(); h != nil {
		if err := h.OnOnlineApprovalRequestedTx(ctx, tx, scope, c, reason); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// RequestApprovalTx moves a held transit to awaiting_approval (approval without card authorisation, or no
// provider), records the reason and the deadline (holdUntil) on online_bookings, and extends the holds to the
// deadline plus ApprovalHoldGrace (transit.changed, transit.approval_requested).
func (s *Service) RequestApprovalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID, reason string, holdUntil time.Time,
) (*store.Contract, error) {
	return s.approvalTx(ctx, tx, scope, who, contractID, reason, holdUntil, ActionRequestApproval)
}

// PaymentAuthorizedTx moves a pending_payment transit to awaiting_approval when the provider reports the card
// authorisation (webhook kind authorized), records the reason and the deadline (holdUntil), and extends the holds.
func (s *Service) PaymentAuthorizedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID, reason string, holdUntil time.Time,
) (*store.Contract, error) {
	return s.approvalTx(ctx, tx, scope, who, contractID, reason, holdUntil, ActionPaymentAuthorized)
}

// ApproveTx approves an awaiting_approval transit without an authorisation: awaiting_approval → pending_payment,
// holds extended to holdUntil (24 h payment link), online_bookings.approved_at/by set (ifMatch 0 = no check).
// With an authorisation the transit stays awaiting_approval: the booking package marks the approval with
// MarkApprovedTx and captures after commit; the capture webhook confirms through confirmTx.
func (s *Service) ApproveTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, ifMatch int, holdUntil time.Time,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, err
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionApprove)
	if err != nil {
		return nil, err
	}
	ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, notFound(err)
	}
	if err := s.extendOnlineHoldsTx(ctx, tx, scope, c, holdUntil); err != nil {
		return nil, err
	}
	now := s.now()
	from := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	ob.ApprovedAt, ob.ApprovedByUserID = now, who.userID()
	if err := store.UpdateOnlineBooking(ctx, tx, scope, ob, now); err != nil {
		return nil, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit.approve", c.ID, map[string]any{
		"from_status": from, "status": c.Status, "hold_until": store.UTC(holdUntil),
	}); err != nil {
		return nil, err
	}
	return c, s.emitOnline(ctx, tx, scope, t, subject{contract: c, change: string(StatusPendingPayment)}, "")
}

// MarkApprovedTx records the approval of an awaiting_approval transit that stays awaiting_approval (an authorised
// card waiting for its capture, or no provider and a manual confirmation): online_bookings.approved_at/by and the
// audit row, and its holds are extended to holdUntil so the approved booking keeps an active allocation (INV-08)
// while the capture or the manual confirmation lands. The approval deadline job skips approved bookings
// (ifMatch 0 = no check).
func (s *Service) MarkApprovedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, ifMatch int, holdUntil time.Time,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, err
	}
	if c.Status != string(StatusAwaitingApproval) {
		return nil, &TransitionError{From: c.Status, Action: ActionApprove}
	}
	ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, notFound(err)
	}
	if !ob.ApprovedAt.IsZero() {
		return nil, &TransitionError{From: c.Status, Action: ActionApprove}
	}
	if err := s.extendOnlineHoldsTx(ctx, tx, scope, c, holdUntil); err != nil {
		return nil, err
	}
	now := s.now()
	ob.ApprovedAt, ob.ApprovedByUserID = now, who.userID()
	if err := store.UpdateOnlineBooking(ctx, tx, scope, ob, now); err != nil {
		return nil, err
	}
	// The contract version moves so that a second approval with the same If-Match is refused.
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit.approve", c.ID, map[string]any{
		"status": c.Status, "authorized": true, "hold_until": store.UTC(holdUntil),
	}); err != nil {
		return nil, err
	}
	return c, s.emit(ctx, tx, scope, event.TransitChanged, event.TransitChangedData{ContractID: c.ID, Change: event.ChangeUpdated})
}

// RejectTx ends an awaiting_approval transit: action ActionReject (desk) or ActionExpire (approval deadline job,
// reason approval_timeout). Holds are cancelled, online quotas released, online_bookings.rejected_at and
// reject_reason set (transit.cancelled).
func (s *Service) RejectTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, ifMatch int, action Action, reason string,
) (*store.Contract, error) {
	if action != ActionReject && action != ActionExpire {
		return nil, errors.New("contract: reject needs reject or expire")
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
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, err
	}
	if c.Status != string(StatusAwaitingApproval) {
		return nil, &TransitionError{From: c.Status, Action: action}
	}
	// Lock then re-check: an approval committed after the deadline job picked the booking wins over the timeout.
	if action == ActionExpire {
		ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if ob != nil && !ob.ApprovedAt.IsZero() {
			return nil, &TransitionError{From: c.Status, Action: action}
		}
	}
	t, err := Next(TransitTransitions, Status(c.Status), action)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.endOnlineTx(ctx, tx, scope, who, c, t, reason, now); err != nil {
		return nil, err
	}
	if ob, err := store.OnlineBookingByContract(ctx, tx, scope, c.ID); err == nil {
		ob.RejectedAt, ob.RejectReason = now, reason
		if ob.RejectReason == "" {
			ob.RejectReason = "rejected"
		}
		if err := store.UpdateOnlineBooking(ctx, tx, scope, ob, now); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err := s.onlineInquiryTx(ctx, tx, scope, c, store.InquiryDeclinedOther); err != nil {
		return nil, err
	}
	return c, nil
}

// endOnlineTx cancels the allocations of a transit, releases its quotas and moves it along t (cancelled), with
// audit, events and the billing close hook.
func (s *Service) endOnlineTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who, c *store.Contract, t Transition,
	reason string, now time.Time,
) error {
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	if err := s.cancelAllocs(ctx, tx, scope, allocs, occupancy.StatusCancelled, event.ReleasedCancel); err != nil {
		return err
	}
	if err := s.releaseQuotaTx(ctx, tx, scope, c); err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	c.CancelledAt, c.CancelReason = now, reason
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit."+string(t.Action), c.ID, map[string]any{
		"from_status": from, "status": c.Status, "allocation_ids": idsOf(activeOnly(allocs)), "reason": reason,
	}); err != nil {
		return err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return err
	}
	return s.closedTx(ctx, tx, scope, who.actor(), c, CloseCancelled)
}

// CancelOnlineTransitTx cancels an online or channel transit in held, pending_payment, awaiting_approval or
// confirmed (customer cancellation, abandonment, channel cancel): holds and allocations cancelled, online quotas
// released, audit and transit.cancelled. Refunds are not decided here (booking applies the policy snapshot).
func (s *Service) CancelOnlineTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, ifMatch int, reason string,
) (*store.Contract, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	reason, _ = cleanText(reason, 500)
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, err
	}
	switch Status(c.Status) {
	case StatusHeld, StatusPendingPayment, StatusAwaitingApproval, StatusConfirmed:
	default:
		return nil, &TransitionError{From: c.Status, Action: ActionCancel}
	}
	if !IsOnline(c) {
		return nil, &TransitionError{From: c.Status, Action: ActionCancel}
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionCancel)
	if err != nil {
		return nil, err
	}
	wasPaid := c.Status == string(StatusConfirmed)
	if err := s.endOnlineTx(ctx, tx, scope, who, c, t, reason, s.now()); err != nil {
		return nil, err
	}
	if !wasPaid {
		if err := s.onlineInquiryTx(ctx, tx, scope, c, store.InquiryLost); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// ---- Stay change ----

// OnlineStayChange is a date change asked from the customer area (m8-plan §7.2, §16 D12). Placement is the
// resource of the new stay (the same one when it is still free, else a solution of the same category).
type OnlineStayChange struct {
	Arrival, Departure calendar.Date
	Placement          Segment
	ExpectedTotalCents int64 // 409 price_changed when the new total differs
	// Price is the price of the new stay computed by PriceOnline before the transaction (required).
	Price *OnlinePrice
}

// ChangeOnlineStayTx changes the dates of a confirmed online transit before arrival: the allocations are replaced
// by one transit allocation of Placement on the new nights (same contract line), the online quotas are released
// and claimed again for the new nights, and the price snapshot is replaced (the PriceEffect drives the
// supplementary charge or the refund; the billing reprice hook corrects the charge).
func (s *Service) ChangeOnlineStayTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who Who,
	contractID string, ifMatch int, in OnlineStayChange,
) (*store.Contract, PriceEffect, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, PriceEffect{}, err
	}
	if in.Price == nil || s.d.Occupancy == nil {
		return nil, PriceEffect{}, errors.New("contract: online stay change needs a price and the occupancy service")
	}
	v := &auth.ValidationError{}
	checkStay(v, in.Arrival, in.Departure, "arrival", "departure", maxTransitNights)
	g := in.Placement
	g.First, g.Last = in.Arrival, in.Departure.AddDays(-1)
	if len(v.Fields) == 0 {
		checkSegments(v, "placement", []Segment{g}, in.Arrival, in.Departure, 1)
	}
	if err := v.Err(); err != nil {
		return nil, PriceEffect{}, err
	}
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, ifMatch)
	if err != nil {
		return nil, PriceEffect{}, err
	}
	if !IsOnline(c) || c.Status != string(StatusConfirmed) || !s.today(ctx, tx, scope).Before(c.StartsOn) ||
		!s.today(ctx, tx, scope).Before(in.Arrival) {
		return nil, PriceEffect{}, &TransitionError{From: c.Status, Action: ActionChangeStay}
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionChangeStay)
	if err != nil {
		return nil, PriceEffect{}, err
	}
	newTotal := in.Price.Total()
	if newTotal != in.ExpectedTotalCents {
		return nil, PriceEffect{}, ErrOnlinePriceChanged
	}
	if err := checkListStillActive(ctx, tx, scope, in.Price.p); err != nil {
		return nil, PriceEffect{}, err
	}
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, PriceEffect{}, err
	}
	active := activeOnly(allocs)
	if len(active) == 0 {
		return nil, PriceEffect{}, occupancy.ErrHoldExpired
	}
	now := s.now()
	if err := s.cancelAllocs(ctx, tx, scope, active, occupancy.StatusCancelled, event.ReleasedTruncate); err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.d.Occupancy.ReleaseQuotaTx(ctx, tx, scope, c.ID, nil); err != nil {
		return nil, PriceEffect{}, err
	}
	al, err := s.allocateLine(ctx, tx, scope, who.actor(), now, allocation{
		lineID: active[0].ContractLineID, vesselID: c.VesselID, kind: occupancy.KindTransit, seg: g, arrival: true,
	})
	if err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.d.Occupancy.ClaimQuotaTx(ctx, tx, scope, c.ID, occupancy.QuotaResource{
		ResourceType: g.ResourceType, ResourceID: g.ResourceID,
	}, g.First, g.Last, c.Channel); err != nil {
		return nil, PriceEffect{}, err
	}
	effect := PriceEffect{OldTotalCents: c.TotalCents, NewTotalCents: newTotal, DeltaCents: newTotal - c.TotalCents, Applied: true}
	old := map[string]any{"from": c.StartsOn, "to": c.EndsOn, "total_cents": c.TotalCents}
	p := in.Price.p
	c.StartsOn, c.EndsOn = in.Arrival, in.Departure
	c.PriceSnapshot, c.PriceListID, c.EngineVersion = p.raw, p.listID(), p.engineVersion()
	c.TotalCents, c.DepositCents = p.total(), p.deposit()
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.auditWho(ctx, tx, scope, who, "transit.stay", c.ID, map[string]any{
		"old": old, "new": map[string]any{"from": c.StartsOn, "to": c.EndsOn, "total_cents": c.TotalCents},
		"allocation_ids": []string{al.ID}, "online": true,
	}); err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeStay}); err != nil {
		return nil, PriceEffect{}, err
	}
	if err := s.emitAllocated(ctx, tx, scope, []store.Allocation{*al}, c.ID); err != nil {
		return nil, PriceEffect{}, err
	}
	return c, effect, s.repricedTx(ctx, tx, scope, who.actor(), c, RepriceChangeStay)
}

// ---- Confirmation helpers (confirm.go) ----

// lateRecoverable reports whether an expired online transit may still be confirmed by a late provider payment
// (m8-plan §5.5): every lapsed allocation is a berth whose nights are free again (expired holds count as free).
// The re-allocation in confirmTx is still protected by the unique keys (INV-01); a lost race fails the event,
// which the provider retries, and the retry refunds.
func (s *Service) lateRecoverable(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) (bool, error) {
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return false, err
	}
	ends := lapsedChainEnds(allocs)
	if len(ends) == 0 {
		return false, nil
	}
	now := s.now()
	for _, al := range ends {
		if al.ResourceType != store.ResourceBerth {
			return false, nil
		}
		owners, err := store.OccupancyOwners(ctx, tx, scope, al.ResourceType, al.ResourceID, al.FirstNight, al.LastNight, nil, nil)
		if err != nil {
			return false, err
		}
		for _, o := range owners {
			if o.Kind == store.AllocKindHold && !o.ExpiresAt.IsZero() && o.ExpiresAt.Before(now) {
				continue
			}
			return false, nil
		}
	}
	return true, nil
}

// LateRecoverableTx reports, under the contract lock, whether an expired online transit can be confirmed by a late
// payment (used by collect before calling ConfirmTransitPaidTx with late recovery).
func (s *Service) LateRecoverableTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) (bool, error) {
	c, err := lockContract(ctx, tx, scope, contractID, KindTransit, 0)
	if err != nil {
		return false, err
	}
	if c.Status != string(StatusExpired) || c.Channel != ChannelOnline {
		return false, nil
	}
	return s.lateRecoverable(ctx, tx, scope, c)
}

// reclaimQuotaTx claims the online quotas again for the lapsed allocations of an expired transit before its late
// confirmation re-allocates them (its claims were released at expiry).
func (s *Service) reclaimQuotaTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) error {
	if s.d.Occupancy == nil {
		return nil
	}
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	for _, al := range lapsedChainEnds(allocs) {
		if err := s.d.Occupancy.ClaimQuotaTx(ctx, tx, scope, c.ID, occupancy.QuotaResource{
			ResourceType: al.ResourceType, ResourceID: al.ResourceID,
		}, al.FirstNight, al.LastNight, c.Channel); err != nil {
			return err
		}
	}
	return nil
}
