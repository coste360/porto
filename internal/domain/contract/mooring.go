package contract

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// Mooring contracts, base (m3-plan §4.1): draft (holding its resources until draft_hold_days) or active
// ("firmato su carta"), cancel, terminate; expiry is the job's.

// maxContractNights bounds a mooring contract (a little more than a year).
const maxContractNights = 400

var terms = []string{TermAnnual, TermSeasonal, TermMonthly, TermPeriod}

// MooringInput creates a mooring contract (createContract).
type MooringInput struct {
	Term              string
	CustomerID        string
	BillingCustomerID string
	VesselID          string
	StartsOn          calendar.Date
	EndsOn            *calendar.Date // default by term (§13 D27)
	Placements        []Segment
	Extras            []ExtraResource
	Services          []ServiceRequest
	Policy            *PolicyInput
	Notes             string
	Activate          bool
	FitOverride       *FitOverrideInput
}

// ExtraResource is a land resource added to a mooring contract for its whole period.
type ExtraResource struct {
	LandResourceID string
	ItemID         string
}

// seasonEnd returns the end (exclusive) of the season window of the active seasonal list covering starts.
func seasonEnd(ctx context.Context, db bun.IDB, scope store.MarinaScope, starts calendar.Date) (calendar.Date, bool) {
	lists, err := store.ActivePriceLists(ctx, db, scope)
	if err != nil {
		return calendar.Date{}, false
	}
	for i := range lists {
		l := &lists[i]
		if !slices.Contains(l.ContractTypes, string(pricing.ContractSeasonal)) || starts.Before(l.ValidFrom) || starts.After(l.ValidTo) {
			continue
		}
		var set pricing.ListSettings
		if json.Unmarshal(l.Settings, &set) != nil || set.SeasonWindow == nil || set.SeasonWindow.To.Before(starts) {
			return calendar.Date{}, false
		}
		return set.SeasonWindow.To.AddDays(1), true
	}
	return calendar.Date{}, false
}

// CreateContract creates a mooring contract, draft or active (contracts.write; a fit override also needs
// occupancy.override_fit).
func (s *Service) CreateContract(ctx context.Context, p *auth.Principal, marinaID string, in MooringInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
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
	v := &auth.ValidationError{}
	if !slices.Contains(terms, in.Term) {
		v.Add("term", "invalid")
	}
	if in.StartsOn.IsZero() {
		v.Add("starts_on", "required")
	}
	var ends calendar.Date
	switch {
	case in.EndsOn != nil:
		ends = *in.EndsOn
	case in.StartsOn.IsZero():
	case in.Term == TermSeasonal:
		e, ok := seasonEnd(ctx, s.d.DB.DB, scope, in.StartsOn)
		if !ok {
			v.Add("ends_on", "required")
		}
		ends = e
	default:
		e, ok := DefaultEndsOn(in.Term, in.StartsOn)
		if !ok {
			v.Add("ends_on", "required")
		}
		ends = e
	}
	if len(v.Fields) == 0 {
		checkStay(v, in.StartsOn, ends, "starts_on", "ends_on", maxContractNights)
		checkSegments(v, "placements", in.Placements, in.StartsOn, ends, 2)
	}
	if len(in.Extras) > 10 {
		v.Add("extra_resources", "too_many")
	}
	for _, x := range in.Extras {
		if !store.ValidID(x.LandResourceID) {
			v.Add("extra_resources.land_resource_id", "invalid")
		}
	}
	policy := mergePolicy(v, defaultPolicy(st), in.Policy)
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	if in.FitOverride != nil && cleanReason(in.FitOverride) == "" {
		v.Add("fit_override.reason", "required")
	}
	if in.CustomerID == "" {
		v.Add("customer_id", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, in.VesselID, in.CustomerID, in.BillingCustomerID); err != nil {
		return nil, err
	}
	if in.VesselID != "" {
		// Insurance policy (11.5, m4-plan §15 D29): block refuses the new contract; warn lets it through.
		cs, err := s.settingsTx(ctx, s.d.DB.DB, scope)
		if err != nil {
			return nil, err
		}
		if _, err := checkInsurance(ctx, s.d.DB.DB, scope, cs, in.VesselID, in.StartsOn); err != nil {
			return nil, err
		}
	}
	price, err := s.price(ctx, p, marinaID, priceRequest{
		contractType: PricingType(KindMooring, in.Term), arrival: in.StartsOn, departure: ends, first: in.Placements[0],
		vesselID: in.VesselID, customerID: in.CustomerID, channel: ChannelDesk, services: in.Services,
	})
	if err != nil {
		return nil, err
	}
	extras := make([]extraLine, 0, len(in.Extras))
	for _, x := range in.Extras {
		extras = append(extras, extraLine(x))
	}
	a := actor{p: p}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.createMooringTx(ctx, tx, scope, a, newContract{
			kind: KindMooring, term: in.Term, customerID: in.CustomerID, billingCustomerID: in.BillingCustomerID,
			vesselID: in.VesselID, startsOn: in.StartsOn, endsOn: ends, channel: ChannelDesk, price: price, policy: policy,
			notes: notes, segments: in.Placements, extras: extras, services: in.Services, arrival: true,
			fit: fitOverride(a, in.FitOverride),
		}, in.Activate, st)
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

// createMooringTx writes a draft (holding) or active mooring contract with its audit row and events.
func (s *Service) createMooringTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, n newContract, activate bool, st *store.OccupancySettings) (*store.Contract, error) {
	action := ActionCreate
	if activate {
		action = ActionCreateActive
	}
	t, err := Next(MooringTransitions, StatusNone, action)
	if err != nil {
		return nil, err
	}
	n.status = t.To
	if activate {
		n.allocKind = occupancy.KindContract
	} else {
		exp := s.now().Add(time.Duration(st.DraftHoldDays) * 24 * time.Hour)
		if n.expiresAt != nil { // renewal prelazione until the proposal deadline (m4-plan §3.4)
			exp = *n.expiresAt
		}
		n.allocKind, n.expiresAt = occupancy.KindHold, &exp
	}
	c, allocs, err := s.createTx(ctx, tx, scope, a, n)
	if err != nil {
		return nil, err
	}
	var plan []store.Installment
	if !n.skipPlan {
		if plan, err = s.planOnCreateTx(ctx, tx, scope, c, n.plan); err != nil {
			return nil, err
		}
	}
	var first *store.Installment
	if activate && !n.skipPlan {
		if first, err = s.issueFirstTx(ctx, tx, scope, a, c); err != nil {
			return nil, err
		}
	}
	diff := map[string]any{
		"status": c.Status, "term": c.Term, "from": c.StartsOn, "to": c.EndsOn, "allocation_ids": idsOf(allocs),
		"total_cents": c.TotalCents, "fit_override": n.fit != nil, "quote_id": c.QuoteID, "installments": planDiff(c, plan),
	}
	if first != nil {
		diff["issued_installment_id"] = first.ID
	}
	if err := s.writeAudit(ctx, tx, scope, a, "contract.create", entityContract, c.ID, diff); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return nil, err
	}
	if err := s.emitAllocated(ctx, tx, scope, allocs, c.ID); err != nil {
		return nil, err
	}
	return c, nil
}

// ActivateInput is the paper confirmation of an activation ("firmato su carta").
type ActivateInput struct {
	PaperScanFileID string // a stored file with the scan; required when the marina sets require_paper_scan
}

// ActivateContract activates a draft mooring contract ("firmato su carta", contracts.write, If-Match): holds
// become contract allocations; lost holds are allocated again or the activation fails with 409.
func (s *Service) ActivateContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Detail, error) {
	return s.ActivateContractWith(ctx, p, marinaID, id, ifMatch, ActivateInput{})
}

// ActivateContractWith is ActivateContract with the M4 paper confirmation (m4-plan §3.1, §15 D11): a contract
// awaiting signature has its request cancelled (paper override); when the marina's settings require a paper
// scan, the scan file must be given. The first instalment is issued.
func (s *Service) ActivateContractWith(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in ActivateInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		return s.activatePaperTx(ctx, tx, scope, a, c, in)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// activatePaperTx is the activate transition of a locked contract.
func (s *Service) activatePaperTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, in ActivateInput) error {
	t, err := Next(MooringTransitions, Status(c.Status), ActionActivate)
	if err != nil {
		return err
	}
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return err
	}
	if in.PaperScanFileID != "" {
		if _, err := store.GetStoredFile(ctx, tx, scope, in.PaperScanFileID); err != nil {
			v := &auth.ValidationError{}
			v.Add("paper_scan_file_id", "invalid")
			return v
		}
	} else if cs.Stored && cs.RequirePaperScan {
		return ErrPaperScanRequired
	}
	cancelled := ""
	if c.Status == string(StatusAwaitingSignature) && s.signer != nil {
		if err := s.signer.CancelTx(ctx, tx, scope, SubjectContract, c.ID); err != nil {
			return err
		}
		cancelled = c.SignatureRequestID
	}
	c.SignedOnPaper, c.SignedAt = true, s.now()
	return s.activateTx(ctx, tx, scope, a, c, t, "contract.activate", map[string]any{
		"paper_scan_file_id": in.PaperScanFileID, "cancelled_signature_request_id": cancelled,
	}, subject{driver: DriverPaper})
}

// CancelContract cancels a draft mooring contract and frees its holds (contracts.write, If-Match).
func (s *Service) CancelContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
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
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), ActionCancel)
		if err != nil {
			return err
		}
		return s.cancelContractTx(ctx, tx, scope, a, c, t, reason)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// cancelContractTx cancels a draft or awaiting-signature contract: open signature request cancelled, holds
// cancelled, instalments cancelled, renewal proposal and waiting list told.
func (s *Service) cancelContractTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, t Transition, reason string) error {
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	if c.Status == string(StatusAwaitingSignature) && s.signer != nil {
		if err := s.signer.CancelTx(ctx, tx, scope, SubjectContract, c.ID); err != nil {
			return err
		}
	}
	if err := s.cancelAllocs(ctx, tx, scope, allocs, occupancy.StatusCancelled, event.ReleasedCancel); err != nil {
		return err
	}
	cancelledPlan, err := s.cancelPlanTx(ctx, tx, scope, c, true)
	if err != nil {
		return err
	}
	from := c.Status
	now := s.now()
	c.Status, c.CancelledAt, c.CancelReason = string(t.To), now, reason
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "contract.cancel", entityContract, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "allocation_ids": idsOf(activeOnly(allocs)), "installment_ids": cancelledPlan,
	}); err != nil {
		return err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeCancelled}); err != nil {
		return err
	}
	return s.onContractFreedTx(ctx, tx, scope, c)
}

// TerminateContract ends an active mooring contract on effectiveOn (contracts.write, If-Match): its
// allocations lose the nights from effectiveOn (those starting later are cancelled) and its active releases
// after that date are closed; future scheduled instalments are cancelled and the refund (negative) or penalty
// (positive) of the cancellation policy is stored (never charged in M4, §15 D10).
func (s *Service) TerminateContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, effectiveOn calendar.Date, reason string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	reason, ok := cleanText(reason, 500)
	if reason == "" {
		v.Add("reason", "required")
	} else if !ok {
		v.Add("reason", "too_long")
	}
	if effectiveOn.IsZero() {
		v.Add("effective_on", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), ActionTerminate)
		if err != nil {
			return err
		}
		if effectiveOn.Before(c.StartsOn) || !effectiveOn.Before(c.EndsOn) {
			v.Add("effective_on", "outside_contract")
			return v
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		var touched []string
		for _, al := range activeOnly(allocs) {
			if al.LastNight.Before(effectiveOn) {
				continue
			}
			if !al.FirstNight.Before(effectiveOn) {
				err = s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusCancelled)
			} else {
				err = s.alloc.TruncateTx(ctx, tx, scope, al.ID, effectiveOn)
			}
			if err != nil {
				return err
			}
			touched = append(touched, al.ID)
			from := effectiveOn
			if al.FirstNight.After(from) {
				from = al.FirstNight
			}
			if err := s.emit(ctx, tx, scope, event.OccupancyReleased, event.OccupancyReleasedData{
				AllocationIDs: []string{al.ID}, ResourceIDs: []string{al.ResourceID}, MapID: al.MapID,
				From: from, To: al.LastNight, Reason: event.ReleasedTruncate,
			}); err != nil {
				return err
			}
		}
		now := s.now()
		closed, err := store.CloseReleasesFrom(ctx, tx, scope, c.ID, effectiveOn, now)
		if err != nil {
			return err
		}
		amount, err := s.terminationAmountTx(ctx, tx, scope, c, effectiveOn)
		if err != nil {
			return err
		}
		cancelledPlan, err := s.cancelPlanTx(ctx, tx, scope, c, false)
		if err != nil {
			return err
		}
		from := c.Status
		c.Status, c.TerminatedOn, c.TerminateReason = string(t.To), effectiveOn, reason
		c.TerminationAmountCents = &amount
		if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "contract.terminate", entityContract, c.ID, map[string]any{
			"from_status": from, "status": c.Status, "effective_on": effectiveOn,
			"allocation_ids": touched, "release_ids": closed, "termination_amount_cents": amount,
			"installment_ids": cancelledPlan,
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
			return err
		}
		if err := s.closedTx(ctx, tx, scope, a, c, CloseTerminated); err != nil {
			return err
		}
		return s.onContractFreedTx(ctx, tx, scope, c)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// MooringPatch changes a mooring contract (updateContract): draft accepts notes, billing customer and policy;
// active accepts notes only.
type MooringPatch struct {
	Notes             *string
	BillingCustomerID *string // "" clears
	Policy            *PolicyInput
}

// UpdateContract applies a patch (contracts.write, If-Match).
func (s *Service) UpdateContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in MooringPatch) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), ActionUpdate)
		if err != nil {
			return err
		}
		v := &auth.ValidationError{}
		var fields []string
		if in.Notes != nil {
			n, ok := cleanText(*in.Notes, 2000)
			if !ok {
				v.Add("notes", "too_long")
			}
			c.Notes = n
			fields = append(fields, "notes")
		}
		if c.Status != string(StatusDraft) {
			if in.BillingCustomerID != nil {
				v.Add("billing_customer_id", "not_editable")
			}
			if in.Policy != nil {
				v.Add("policy", "not_editable")
			}
		} else {
			if in.BillingCustomerID != nil {
				if err := checkParties(ctx, tx, scope, "", *in.BillingCustomerID); err != nil {
					return err
				}
				c.BillingCustomerID = *in.BillingCustomerID
				fields = append(fields, "billing_customer_id")
			}
			if in.Policy != nil {
				base, err := DecodePolicy(c.Policy)
				if err != nil {
					return err
				}
				c.Policy = store.MustJSON(mergePolicy(v, base, in.Policy))
				fields = append(fields, "policy")
			}
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "contract.update", entityContract, c.ID, map[string]any{"fields": fields}); err != nil {
			return err
		}
		return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeUpdated})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}
