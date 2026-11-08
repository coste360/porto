package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Charter (spec 13.6; m4-plan §3.5, §15 D7): a charter company's fleet of registered vessels and a seasonal
// contract of kind charter on a set of berths (or dynamic-area placements), each resource line allocated with a
// concrete fleet vessel. Fleet vessels rotate on the contract's lines from a night without a new contract and
// without repricing.

const entityFleet = "charter_fleet"

// Pricing modes of a charter.
const (
	CharterPerBerth  = "per_berth"
	CharterPerVessel = "per_vessel"
	CharterFlat      = "flat"
)

// Fleet is a charter fleet with its vessels.
type Fleet struct {
	Row      store.CharterFleet
	Vessels  []store.CharterFleetVessel
	Customer *Party
	Names    map[string]store.Vessel
}

// FleetInput creates a fleet.
type FleetInput struct {
	CustomerID string
	Name       string
	VesselIDs  []string
}

// FleetPatch changes a fleet.
type FleetPatch struct {
	Name      *string
	VesselIDs *[]string
}

func (s *Service) fleet(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Fleet, error) {
	f, err := store.GetCharterFleet(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.fleetView(ctx, db, scope, p, f)
}

func (s *Service) fleetView(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, f *store.CharterFleet) (*Fleet, error) {
	vs, err := store.FleetVessels(ctx, db, scope, []string{f.ID})
	if err != nil {
		return nil, err
	}
	ps, err := parties(ctx, db, scope, []string{f.CustomerID}, s.can(ctx, p, scope.ID(), auth.PermCustomersRead))
	if err != nil {
		return nil, err
	}
	out := &Fleet{Row: *f, Vessels: vs, Customer: partyOf(ps, f.CustomerID), Names: map[string]store.Vessel{}}
	for _, v := range vs {
		if row, err := store.GetVessel(ctx, db, scope, v.VesselID); err == nil {
			out.Names[v.VesselID] = *row
		}
	}
	return out, nil
}

func checkFleetInput(v *auth.ValidationError, name string, vessels []string) {
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > 120 {
		v.Add("name", "invalid")
	}
	if len(vessels) > 200 {
		v.Add("vessel_ids", "too_many")
	}
	seen := map[string]bool{}
	for _, id := range vessels {
		if !store.ValidID(id) || seen[id] {
			v.Add("vessel_ids", "invalid")
		}
		seen[id] = true
	}
}

// CreateCharterFleet creates a fleet (contracts.write); a vessel active in another fleet is 409
// vessel_in_other_fleet.
func (s *Service) CreateCharterFleet(ctx context.Context, p *auth.Principal, marinaID string, in FleetInput) (*Fleet, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	checkFleetInput(v, in.Name, in.VesselIDs)
	if !store.ValidID(in.CustomerID) {
		v.Add("customer_id", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a := actor{p: p}
	now := s.now()
	f := &store.CharterFleet{ID: store.NewID(), MarinaID: scope.ID(), CustomerID: in.CustomerID, Name: strings.TrimSpace(in.Name), Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkParties(ctx, tx, scope, "", in.CustomerID); err != nil {
			return err
		}
		for _, vid := range in.VesselIDs {
			if err := checkParties(ctx, tx, scope, vid); err != nil {
				return err
			}
		}
		if err := store.InsertCharterFleet(ctx, tx, f); err != nil {
			return err
		}
		if err := store.SetFleetVessels(ctx, tx, scope, f.ID, in.VesselIDs, now); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrVesselInOtherFleet
			}
			return err
		}
		return s.writeAudit(ctx, tx, scope, a, "fleet.create", entityFleet, f.ID, map[string]any{"vessel_ids": in.VesselIDs})
	})
	if err != nil {
		return nil, err
	}
	return s.fleet(ctx, s.d.DB.DB, scope, p, f.ID)
}

// UpdateCharterFleet renames a fleet or replaces its active vessels (contracts.write, If-Match).
func (s *Service) UpdateCharterFleet(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in FleetPatch) (*Fleet, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		f, err := store.GetCharterFleet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ifMatch != 0 && f.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		name := f.Name
		if in.Name != nil {
			name = *in.Name
		}
		var vessels []string
		if in.VesselIDs != nil {
			vessels = *in.VesselIDs
		}
		checkFleetInput(v, name, vessels)
		if err := v.Err(); err != nil {
			return err
		}
		f.Name = strings.TrimSpace(name)
		now := s.now()
		if in.VesselIDs != nil {
			for _, vid := range vessels {
				if err := checkParties(ctx, tx, scope, vid); err != nil {
					return err
				}
			}
			if err := store.SetFleetVessels(ctx, tx, scope, f.ID, vessels, now); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return ErrVesselInOtherFleet
				}
				return err
			}
		}
		if err := store.UpdateCharterFleet(ctx, tx, scope, f.Version, f, now); err != nil {
			return versionErr(err)
		}
		return s.writeAudit(ctx, tx, scope, a, "fleet.update", entityFleet, f.ID, map[string]any{"vessel_ids": vessels, "renamed": in.Name != nil})
	})
	if err != nil {
		return nil, err
	}
	return s.fleet(ctx, s.d.DB.DB, scope, p, id)
}

// GetCharterFleet returns a fleet (contracts.read).
func (s *Service) GetCharterFleet(ctx context.Context, p *auth.Principal, marinaID, id string) (*Fleet, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	return s.fleet(ctx, s.d.DB.DB, scope, p, id)
}

// ListCharterFleets returns the fleets of a marina (contracts.read).
func (s *Service) ListCharterFleets(ctx context.Context, p *auth.Principal, marinaID, customerID string) ([]Fleet, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListCharterFleets(ctx, s.d.DB.DB, scope, customerID)
	if err != nil {
		return nil, err
	}
	out := make([]Fleet, 0, len(rows))
	for i := range rows {
		f, err := s.fleetView(ctx, s.d.DB.DB, scope, p, &rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, nil
}

// CharterPlacementInput is one berth (or area placement) of a charter with its initial fleet vessel.
type CharterPlacementInput struct {
	Target   Segment // nights are the whole season
	VesselID string
}

// OutNight is a planned "vessel out" range (informative).
type OutNight struct {
	From    calendar.Date `json:"from"`
	To      calendar.Date `json:"to"`
	BerthID string        `json:"berth_id,omitempty"`
}

// CharterInput creates a charter contract (createCharter).
type CharterInput struct {
	FleetID           string
	StartsOn, EndsOn  calendar.Date
	Placements        []CharterPlacementInput
	PricingMode       string
	FlatCents         *int64
	FlatReason        string
	ChangeoverWeekday int
	OutNights         []OutNight
	Services          []ServiceRequest
	Policy            *PolicyInput
	Activate          bool
	FitOverride       *FitOverrideInput
}

// CharterPrice sums the quotes of a charter by pricing mode (pure): per_berth = Σ quote of each placement with
// its vessel, per_vessel = Σ quote of each active fleet vessel on its placement (fleet vessels without a
// placement are quoted on the first one), flat = the staff amount.
func CharterPrice(mode string, placementQuotes []money.Cents, spareQuotes []money.Cents, flat money.Cents) money.Cents {
	var sum money.Cents
	switch mode {
	case CharterFlat:
		return flat
	case CharterPerVessel:
		for _, q := range spareQuotes {
			sum += q
		}
		fallthrough
	default:
		for _, q := range placementQuotes {
			sum += q
		}
	}
	return sum
}

// CreateCharter creates a charter contract on the fleet (contracts.write; a fit override also
// occupancy.override_fit). Each placement is a resource line allocated with its vessel for the whole season.
func (s *Service) CreateCharter(ctx context.Context, p *auth.Principal, marinaID string, in CharterInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(in.FitOverride)); err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	v := &auth.ValidationError{}
	checkStay(v, in.StartsOn, in.EndsOn, "starts_on", "ends_on", maxContractNights)
	if len(in.Placements) == 0 {
		v.Add("placements", "required")
	} else if len(in.Placements) > 100 {
		v.Add("placements", "too_many")
	}
	if in.ChangeoverWeekday < 1 || in.ChangeoverWeekday > 7 {
		v.Add("changeover_weekday", "invalid")
	}
	if !contains([]string{CharterPerBerth, CharterPerVessel, CharterFlat}, in.PricingMode) {
		v.Add("pricing_mode", "invalid")
	}
	flatReason, _ := cleanText(in.FlatReason, 500)
	if in.PricingMode == CharterFlat && (in.FlatCents == nil || *in.FlatCents < 0 || flatReason == "") {
		v.Add("flat_cents", "required")
	}
	for i := range in.Placements {
		pl := &in.Placements[i]
		pl.Target.First, pl.Target.Last = in.StartsOn, in.EndsOn.AddDays(-1)
		checkSegments(v, fmt.Sprintf("placements[%d].target", i), []Segment{pl.Target}, in.StartsOn, in.EndsOn, 1)
	}
	if len(in.OutNights) > 200 {
		v.Add("out_nights", "too_many")
	}
	st, err := store.ContractOccupancySettings(ctx, db, scope, s.now())
	if err != nil {
		return nil, err
	}
	policy := mergePolicy(v, defaultPolicy(st), in.Policy)
	if err := v.Err(); err != nil {
		return nil, err
	}
	fl, err := store.GetCharterFleet(ctx, db, scope, in.FleetID)
	if err != nil {
		return nil, notFound(err)
	}
	fvs, err := store.FleetVessels(ctx, db, scope, []string{fl.ID})
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, fv := range fvs {
		active[fv.VesselID] = active[fv.VesselID] || fv.Active
	}
	placed := map[string]bool{}
	for _, pl := range in.Placements {
		if !active[pl.VesselID] {
			return nil, ErrVesselNotInFleet
		}
		placed[pl.VesselID] = true
	}
	// Pricing before the write transaction (m3-plan D11).
	var quotes, spares []money.Cents
	if in.PricingMode != CharterFlat {
		for _, pl := range in.Placements {
			pr, err := s.price(ctx, p, marinaID, priceRequest{
				contractType: PricingType(KindMooring, TermSeasonal), arrival: in.StartsOn, departure: in.EndsOn, first: pl.Target,
				vesselID: pl.VesselID, customerID: fl.CustomerID, channel: ChannelDesk, services: in.Services,
			})
			if err != nil {
				return nil, err
			}
			quotes = append(quotes, money.Cents(pr.total()))
		}
		if in.PricingMode == CharterPerVessel {
			for _, fv := range fvs {
				if !fv.Active || placed[fv.VesselID] {
					continue
				}
				pr, err := s.price(ctx, p, marinaID, priceRequest{
					contractType: PricingType(KindMooring, TermSeasonal), arrival: in.StartsOn, departure: in.EndsOn,
					first: in.Placements[0].Target, vesselID: fv.VesselID, customerID: fl.CustomerID, channel: ChannelDesk,
				})
				if err != nil {
					return nil, err
				}
				spares = append(spares, money.Cents(pr.total()))
			}
		}
	}
	var flat money.Cents
	if in.FlatCents != nil {
		flat = money.Cents(*in.FlatCents)
	}
	total := CharterPrice(in.PricingMode, quotes, spares, flat)
	a := actor{p: p}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.createCharterTx(ctx, tx, scope, a, in, fl, policy, total, flatReason, st)
		if err != nil {
			return err
		}
		id = c.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

func (s *Service) createCharterTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, in CharterInput, fl *store.CharterFleet,
	policy Policy, total money.Cents, flatReason string, st *store.OccupancySettings,
) (*store.Contract, error) {
	action := ActionCreate
	if in.Activate {
		action = ActionCreateActive
	}
	t, err := Next(CharterTransitions, StatusNone, action)
	if err != nil {
		return nil, err
	}
	now := s.now()
	year := s.today(ctx, tx, scope).Year()
	number, err := store.NextNumber(ctx, tx, scope, SeriesCharter, year)
	if err != nil {
		return nil, err
	}
	c := &store.Contract{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: KindCharter, Term: TermSeasonal, Number: number, Year: year,
		Status: string(t.To), CustomerID: fl.CustomerID, VesselID: in.Placements[0].VesselID, StartsOn: in.StartsOn,
		EndsOn: in.EndsOn, Channel: ChannelDesk, TotalCents: int64(total), Policy: store.MustJSON(policy),
		CreatedByUserID: a.userID(), Charter: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if in.Activate {
		c.ActivatedAt, c.SignedOnPaper = now, true
	}
	if err := store.InsertContract(ctx, tx, c); err != nil {
		return nil, fmt.Errorf("insert charter: %w", err)
	}
	lines := make([]store.ContractLine, 0, len(in.Placements)+len(in.Services))
	for i, pl := range in.Placements {
		lines = append(lines, store.ContractLine{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: i + 1, LineKind: "resource",
			ResourceType: pl.Target.ResourceType, ResourceID: pl.Target.ResourceID, QtyMilli: 1000, CreatedAt: now, UpdatedAt: now,
		})
	}
	for _, sv := range in.Services {
		lines = append(lines, store.ContractLine{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: len(lines) + 1, LineKind: "service", ItemID: sv.ItemID,
			QtyMilli: sv.QtyMilli, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := store.InsertContractLines(ctx, tx, lines); err != nil {
		return nil, err
	}
	kind, exp := occupancy.KindContract, (*time.Time)(nil)
	if !in.Activate {
		e := now.Add(time.Duration(st.DraftHoldDays) * 24 * time.Hour)
		kind, exp = occupancy.KindHold, &e
	}
	fit := fitOverride(a, in.FitOverride)
	allocs := make([]store.Allocation, 0, len(in.Placements))
	for i, pl := range in.Placements {
		al, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
			lineID: lines[i].ID, vesselID: pl.VesselID, kind: kind, seg: pl.Target, expiresAt: exp, fit: fit,
		})
		if err != nil {
			return nil, err
		}
		allocs = append(allocs, *al)
	}
	if in.OutNights == nil {
		in.OutNights = []OutNight{}
	}
	out, _ := json.Marshal(in.OutNights)
	if err := store.InsertCharterContract(ctx, tx, &store.CharterContract{
		ContractID: c.ID, MarinaID: scope.ID(), FleetID: fl.ID, ChangeoverWeekday: in.ChangeoverWeekday,
		PricingMode: in.PricingMode, OutNights: out, Version: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	plan, err := s.planOnCreateTx(ctx, tx, scope, c, PlanStandard)
	if err != nil {
		return nil, err
	}
	diff := map[string]any{
		"status": c.Status, "fleet_id": fl.ID, "from": c.StartsOn, "to": c.EndsOn, "allocation_ids": idsOf(allocs),
		"total_cents": c.TotalCents, "pricing_mode": in.PricingMode, "fit_override": fit != nil, "installments": planDiff(c, plan),
	}
	if in.PricingMode == CharterFlat {
		diff["flat_reason_set"], diff["flat_by_user_id"] = flatReason != "", a.userID()
	}
	if in.Activate {
		first, err := s.issueFirstTx(ctx, tx, scope, a, c)
		if err != nil {
			return nil, err
		}
		if first != nil {
			diff["issued_installment_id"] = first.ID
		}
	}
	if err := s.writeAudit(ctx, tx, scope, a, "charter.create", entityContract, c.ID, diff); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return nil, err
	}
	return c, s.emitAllocated(ctx, tx, scope, allocs, c.ID)
}

// RotationAssignment puts a fleet vessel on a line of the charter.
type RotationAssignment struct {
	LineID   string
	VesselID string
}

// RotateCharter puts fleet vessels on the charter's lines from a night (contracts.write, If-Match): the line's
// allocation is truncated and allocated again with the new vessel, never repriced, never a new contract. Swaps
// between lines are applied in two phases (all truncations, then all allocations) in one transaction.
func (s *Service) RotateCharter(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, fromNight calendar.Date, as []RotationAssignment) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		if c.Kind != KindCharter {
			return auth.ErrNotFound
		}
		if fromNight.IsZero() {
			v.Add("from_night", "required")
		}
		if len(as) == 0 || len(as) > 100 {
			v.Add("assignments", "invalid")
		}
		if err := v.Err(); err != nil {
			return err
		}
		t, err := Next(CharterTransitions, Status(c.Status), ActionUpdate)
		if err != nil {
			return err
		}
		if fromNight.Before(c.StartsOn) || !fromNight.Before(c.EndsOn) {
			v.Add("from_night", "outside_contract")
			return v
		}
		ch, err := store.GetCharterContract(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		fvs, err := store.FleetVessels(ctx, tx, scope, []string{ch.FleetID})
		if err != nil {
			return err
		}
		inFleet := map[string]bool{}
		for _, fv := range fvs {
			inFleet[fv.VesselID] = inFleet[fv.VesselID] || fv.Active
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		type move struct {
			al     store.Allocation
			vessel string
		}
		var moves []move
		for _, asg := range as {
			if !inFleet[asg.VesselID] {
				return ErrVesselNotInFleet
			}
			found := false
			for _, al := range activeOnly(allocs) {
				if al.ContractLineID != asg.LineID || al.LastNight.Before(fromNight) {
					continue
				}
				found = true
				if al.VesselID != asg.VesselID {
					moves = append(moves, move{al: al, vessel: asg.VesselID})
				}
			}
			if !found {
				v.Add("assignments.line_id", "invalid")
				return v
			}
		}
		now := s.now()
		var created []store.Allocation
		var touched []string
		for _, m := range moves { // phase 1: free the nights of every moved line
			al := m.al
			if al.FirstNight.Before(fromNight) {
				err = s.alloc.TruncateTx(ctx, tx, scope, al.ID, fromNight)
			} else {
				err = s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusCancelled)
			}
			if err != nil {
				return err
			}
			touched = append(touched, al.ID)
		}
		for _, m := range moves { // phase 2: allocate them again with the new vessels
			seg := segmentOf(&m.al)
			if seg.First.Before(fromNight) {
				seg.First = fromNight
			}
			var exp *time.Time
			if m.al.Kind == store.AllocKindHold {
				e := m.al.ExpiresAt
				exp = &e
			}
			n, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
				lineID: m.al.ContractLineID, vesselID: m.vessel, kind: occupancy.Kind(m.al.Kind), seg: seg, expiresAt: exp,
			})
			if err != nil {
				return err
			}
			created = append(created, *n)
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "charter.rotate", entityContract, c.ID, map[string]any{
			"from_night": fromNight, "truncated": touched, "allocated": idsOf(created),
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeRotated}); err != nil {
			return err
		}
		return s.emitAllocated(ctx, tx, scope, created, c.ID)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// CharterInfo returns the charter fields of a contract (nil when not a charter).
func (s *Service) CharterInfo(ctx context.Context, p *auth.Principal, marinaID, contractID string) (*store.CharterContract, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	ch, err := store.GetCharterContract(ctx, s.d.DB.DB, scope, contractID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return ch, err
}
