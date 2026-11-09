package presence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

var events = []string{EventArrived, EventDeparted, EventHauledOut, EventLaunched}

// Input records a presence event (recordPresence).
type Input struct {
	VesselID         string
	Event            string
	At               *time.Time // default now
	AllocationID     string     // default: the vessel's allocation of the night
	Notes            string
	ExpectedReturnOn *calendar.Date // departures: when the vessel is expected back
	// ExpectedResourceID is the resource the operator saw for AllocationID (M6b §5.2): when it no longer matches
	// the active allocation of the vessel for the night the record is refused with *AllocationChangedError.
	ExpectedResourceID string
	// IdempotencyKey dedupes the presence in the domain (M6b §4.4, second layer under the middleware): a replay
	// with the same input returns the presence already recorded with the key.
	IdempotencyKey string
}

// inputHash is the SHA-256 of the canonical input (domain dedupe of a presence recorded with a key).
func inputHash(vesselID string, in Input) string {
	c := struct {
		VesselID, Event, AllocationID, Notes, ExpectedResourceID string
		At                                                       *time.Time
		ExpectedReturnOn                                         *calendar.Date
	}{vesselID, in.Event, in.AllocationID, in.Notes, in.ExpectedResourceID, nil, in.ExpectedReturnOn}
	if in.At != nil {
		t := store.UTC(*in.At)
		c.At = &t
	}
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Result is a recorded presence and the contract it changed (if any).
type Result struct {
	Presence View
	Contract *contract.Detail
}

// View is a presence with its vessel; Notes are empty without customers.read.
type View struct {
	Row      store.Presence
	Vessel   store.Vessel
	HasPhoto bool
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func (s *Service) can(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission) bool {
	_, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	return err == nil
}

// stateAfter is the vessel state after an event.
func stateAfter(ev string) string {
	switch ev {
	case EventDeparted:
		return StateOut
	case EventHauledOut:
		return StateAshore
	default:
		return StateInPort
	}
}

// Record records an arrival, departure, haul-out or launch (presence.write) and updates the vessel state in the
// same transaction; transit arrivals and departures go through the contract hooks (m3-plan §4.4).
func (s *Service) Record(ctx context.Context, p *auth.Principal, marinaID string, in Input) (*Result, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !slices.Contains(events, in.Event) {
		v.Add("event", "invalid")
	}
	notes, ok := cleanText(in.Notes, 1000)
	if !ok {
		v.Add("notes", "too_long")
	}
	in.Notes = notes
	now := s.now()
	if in.At != nil && in.At.After(now.Add(10*time.Minute)) {
		v.Add("at", "in_the_future")
	}
	if in.ExpectedReturnOn != nil && in.Event != EventDeparted {
		v.Add("expected_return_on", "not_allowed")
	}
	if in.ExpectedResourceID != "" && in.AllocationID == "" {
		v.Add("expected_resource_id", "requires_allocation")
	}
	if len(in.ExpectedResourceID) > 36 {
		v.Add("expected_resource_id", "too_long")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if queuedAt, offline := reqctx.OfflineReplay(ctx); offline {
		// An offline replay records only arrivals and departures of an existing allocation (29.2, INV-71), and names
		// the resource the operator saw, so that a moved or cancelled allocation surfaces as allocation_changed
		// instead of being recorded silently.
		if in.AllocationID == "" || in.ExpectedResourceID == "" || (in.Event != EventArrived && in.Event != EventDeparted) {
			return nil, ErrOfflineNotAllowed
		}
		if in.At == nil {
			at := queuedAt
			in.At = &at
		}
	}
	var presenceID, contractID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pr, c, err := s.recordTx(ctx, tx, scope, p, in, SourceStaff)
		if err != nil {
			return err
		}
		presenceID = pr.ID
		if c != nil {
			contractID = c.ID
		}
		return nil
	})
	if err != nil && in.IdempotencyKey != "" && store.IsUniqueViolation(err) {
		// A concurrent request with the same key won the unique index: answer with its presence.
		prev, rerr := store.PresenceByIdempotencyKey(ctx, s.d.DB.DB, scope, in.IdempotencyKey)
		if rerr != nil {
			return nil, err
		}
		if prev.IdempotencyHash != inputHash(in.VesselID, in) {
			return nil, ErrIdempotencyMismatch
		}
		presenceID, contractID, err = prev.ID, prev.ContractID, nil
	}
	if err != nil {
		return nil, err
	}
	return s.result(ctx, scope, p, presenceID, contractID)
}

// checkExpectedResource refuses the record when the allocation the operator saw moved or ended (M6b §5.2, D5):
// it must be active, of the vessel, on the expected resource and cover the night (for a departure the night
// before or the night itself). The error carries the current resource of the vessel, if any.
func checkExpectedResource(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in Input, alloc *store.Allocation, night calendar.Date) error {
	covers := func(n calendar.Date) bool { return !n.Before(alloc.FirstNight) && !n.After(alloc.LastNight) }
	ok := alloc.Status == store.AllocActive && alloc.ResourceID == in.ExpectedResourceID &&
		(covers(night) || (in.Event == EventDeparted && covers(night.AddDays(-1))))
	if ok {
		return nil
	}
	ce := &AllocationChangedError{}
	n := night
	if in.Event == EventDeparted {
		n = night.AddDays(-1)
	}
	cur, err := store.VesselAllocationOn(ctx, tx, scope, in.VesselID, n)
	if errors.Is(err, store.ErrNotFound) && in.Event == EventDeparted {
		cur, err = store.VesselAllocationOn(ctx, tx, scope, in.VesselID, night)
	}
	switch {
	case err == nil:
		ce.CurrentResourceID = cur.ResourceID
		refs, rerr := store.ResourceRefs(ctx, tx, scope, []string{cur.ResourceID})
		if rerr != nil {
			return rerr
		}
		ce.CurrentResourceCode = refs[cur.ResourceID].Code
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	return ce
}

func (s *Service) result(ctx context.Context, scope store.MarinaScope, p *auth.Principal, presenceID, contractID string) (*Result, error) {
	db := s.d.DB.DB
	pr, err := store.GetPresence(ctx, db, scope, presenceID)
	if err != nil {
		return nil, err
	}
	views, err := s.views(ctx, db, scope, p, []store.Presence{*pr})
	if err != nil {
		return nil, err
	}
	out := &Result{Presence: views[0]}
	if contractID != "" && s.can(ctx, p, scope.ID(), auth.PermContractsRead) {
		d, err := s.d.Contracts.GetContract(ctx, p, scope.ID(), contractID)
		if err != nil {
			return nil, err
		}
		out.Contract = d
	}
	return out, nil
}

// recordTx writes the presence, the vessel state, the contract hook, audit and event in tx. source is the
// presences.source value (SourceStaff, or SourceSensor from RecordSensorTx, which never calls the contract hooks:
// sensors never change contracts, INV-100).
func (s *Service) recordTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, in Input, source string) (*store.Presence, *store.Contract, error) {
	if _, err := store.GetVessel(ctx, tx, scope, in.VesselID); err != nil {
		return nil, nil, notFound(err)
	}
	var hash string
	if in.IdempotencyKey != "" {
		// Domain dedupe first: a replay must not meet the transition it caused itself.
		hash = inputHash(in.VesselID, in)
		prev, err := store.PresenceByIdempotencyKey(ctx, tx, scope, in.IdempotencyKey)
		switch {
		case err == nil:
			if prev.IdempotencyHash != hash {
				return nil, nil, ErrIdempotencyMismatch
			}
			var c *store.Contract
			if prev.ContractID != "" {
				c = &store.Contract{ID: prev.ContractID}
			}
			return prev, c, nil
		case !errors.Is(err, store.ErrNotFound):
			return nil, nil, err
		}
	}
	now := s.now()
	at := now
	if in.At != nil {
		at = store.UTC(*in.At)
	}
	night := calendar.In(at, location(ctx, tx, scope))
	vp, err := store.GetVesselPresence(ctx, tx, scope, in.VesselID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	state := ""
	if vp != nil {
		state = vp.State
	}
	switch in.Event {
	case EventArrived, EventLaunched:
		if state == StateInPort {
			return nil, nil, ErrInvalidTransition
		}
	case EventDeparted, EventHauledOut:
		if state == StateOut || state == StateAshore {
			return nil, nil, ErrInvalidTransition
		}
	}
	var alloc *store.Allocation
	if in.AllocationID != "" {
		as, err := store.AllocationsByIDs(ctx, tx, scope, []string{in.AllocationID})
		if err != nil {
			return nil, nil, err
		}
		if len(as) == 0 || as[0].VesselID != in.VesselID {
			return nil, nil, auth.ErrNotFound
		}
		alloc = &as[0]
		if in.ExpectedResourceID != "" {
			if err := checkExpectedResource(ctx, tx, scope, in, alloc, night); err != nil {
				return nil, nil, err
			}
		}
	} else {
		n := night
		if in.Event == EventDeparted {
			n = night.AddDays(-1) // the vessel occupied the night before it leaves
		}
		a, err := store.VesselAllocationOn(ctx, tx, scope, in.VesselID, n)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, nil, err
		}
		if a == nil && in.Event == EventDeparted {
			a, err = store.VesselAllocationOn(ctx, tx, scope, in.VesselID, night)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, nil, err
			}
		}
		alloc = a
	}
	pr := &store.Presence{
		ID: store.NewID(), MarinaID: scope.ID(), VesselID: in.VesselID, Event: in.Event, At: at, Night: night,
		Source: source, Notes: in.Notes, IdempotencyKey: in.IdempotencyKey, IdempotencyHash: hash,
		CreatedAt: now, UpdatedAt: now,
	}
	if p != nil && p.Kind == auth.KindUser {
		pr.UserID = p.UserID
	}
	if alloc != nil {
		pr.AllocationID = alloc.ID
		cid, err := store.ContractOfAllocation(ctx, tx, scope, alloc.ID)
		if err != nil {
			return nil, nil, err
		}
		pr.ContractID = cid
	}
	if err := store.InsertPresence(ctx, tx, pr); err != nil {
		return nil, nil, err
	}
	nvp := &store.VesselPresence{
		VesselID: in.VesselID, MarinaID: scope.ID(), State: stateAfter(in.Event), Since: at, AllocationID: pr.AllocationID,
		LastPresenceID: pr.ID, UpdatedAt: now,
	}
	if in.ExpectedReturnOn != nil {
		nvp.ExpectedReturnOn = *in.ExpectedReturnOn
	}
	if err := store.UpsertVesselPresence(ctx, tx, nvp); err != nil {
		return nil, nil, err
	}
	var c *store.Contract
	switch {
	case source == SourceSensor:
		err = nil // no contract hook; err may still hold the ErrNotFound of a vessel without a presence row
	case in.Event == EventArrived:
		c, err = s.d.Contracts.OnArrivalTx(ctx, tx, scope, p, pr.ContractID)
	case in.Event == EventDeparted:
		c, err = s.d.Contracts.OnDepartureTx(ctx, tx, scope, p, pr.ContractID, night)
	}
	if err != nil {
		return nil, nil, err
	}
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "presence.record",
		EntityType: "presence", EntityID: pr.ID,
		// offline_queued_at of a replay is added by audit.Writer (one place for every audited action).
		Diff: map[string]any{
			"event": pr.Event, "vessel_id": pr.VesselID, "allocation_id": pr.AllocationID, "night": pr.Night, "from_state": state,
		},
	}); err != nil {
		return nil, nil, err
	}
	typ := map[string]string{EventArrived: event.PresenceArrived, EventDeparted: event.PresenceDeparted}[in.Event]
	if typ != "" {
		data := event.PresenceData{
			PresenceID: pr.ID, VesselID: pr.VesselID, AllocationID: pr.AllocationID, ContractID: pr.ContractID,
			ResourceIDs: []string{}, Night: pr.Night,
		}
		if alloc != nil {
			data.ResourceIDs, data.MapID = []string{alloc.ResourceID}, alloc.MapID
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data); err != nil {
			return nil, nil, err
		}
	}
	return pr, c, nil
}

// UnexpectedInput records a vessel that arrived without a booking (recordUnexpectedArrival).
type UnexpectedInput struct {
	VesselID    string
	NewVessel   *contract.NewVessel
	Target      contract.Segment // resource and placement; nights are set from today to departure − 1
	Departure   *calendar.Date   // default tomorrow
	Notes       string
	FitOverride *contract.FitOverrideInput
}

// UnexpectedArrival creates (when new) the vessel, a held transit without customer on the target and the
// arrival presence in one transaction (presence.write and transits.desk; m3-plan §13 D29).
func (s *Service) UnexpectedArrival(ctx context.Context, p *auth.Principal, marinaID string, in UnexpectedInput) (*Result, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	if err != nil {
		return nil, err
	}
	if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk); err != nil {
		return nil, err
	}
	today := occupancy.Today(s.d.Clock, location(ctx, s.d.DB.DB, scope))
	departure := today.AddDays(1)
	if in.Departure != nil {
		departure = *in.Departure
	}
	if in.VesselID != "" {
		vp, err := store.GetVesselPresence(ctx, s.d.DB.DB, scope, in.VesselID)
		if err == nil && vp.State == StateInPort {
			return nil, ErrInvalidTransition
		}
	}
	notes, ok := cleanText(in.Notes, 1000)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("notes", "too_long")
		return nil, v
	}
	in.Target.First, in.Target.Last = today, departure.AddDays(-1)
	var presenceID, contractID string
	err = s.d.Contracts.UnexpectedTransit(ctx, p, marinaID, contract.TransitInput{
		VesselID: in.VesselID, NewVessel: in.NewVessel, Arrival: today, Departure: departure,
		Placements: []contract.Segment{in.Target}, Channel: contract.ChannelDesk, FitOverride: in.FitOverride,
	}, func(ctx context.Context, tx bun.IDB, c *store.Contract, allocs []store.Allocation) error {
		var aid string
		if len(allocs) > 0 {
			aid = allocs[0].ID
		}
		pr, _, err := s.recordTx(ctx, tx, scope, p, Input{VesselID: c.VesselID, Event: EventArrived, AllocationID: aid, Notes: notes}, SourceStaff)
		if err != nil {
			return err
		}
		presenceID, contractID = pr.ID, c.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.result(ctx, scope, p, presenceID, contractID)
}

// Filter narrows List.
type Filter = store.PresenceFilter

// List returns a page of presences (map.view; notes only with customers.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[View], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return store.Page[View]{}, err
	}
	if f.Night == nil {
		t := occupancy.Today(s.d.Clock, location(ctx, s.d.DB.DB, scope))
		f.Night = &t
	}
	page, err := store.ListPresences(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[View]{}, notFound(err)
	}
	views, err := s.views(ctx, s.d.DB.DB, scope, p, page.Items)
	if err != nil {
		return store.Page[View]{}, err
	}
	return store.Page[View]{Items: views, NextCursor: page.NextCursor}, nil
}

func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.Presence) ([]View, error) {
	vids := make([]string, 0, len(rows))
	for _, r := range rows {
		vids = append(vids, r.VesselID)
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = vs[i]
	}
	notes := s.can(ctx, p, scope.ID(), auth.PermCustomersRead)
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		hasPhoto := r.PhotoFileID != ""
		if !notes {
			r.Notes = ""
		}
		out = append(out, View{Row: r, Vessel: vm[r.VesselID], HasPhoto: hasPhoto})
	}
	return out, nil
}
