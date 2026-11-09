package contract

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/schedule"
	"github.com/coste360/porto/internal/store"
)

// Renewals (spec 13.8; m4-plan §3.4): a renewal proposal links a source contract to its successor, a draft
// contract for the next period on the same berth (placement), vessel and services, priced on the new period's
// list, holding the berth until the proposal deadline (prelazione). One open proposal per source is a database
// guarantee (renewal_proposals_open_uidx, INV-33).

const entityProposal = "renewal_proposal"

// Exclusion reasons of a renewal (m4-plan §3.4, §15 D5).
const (
	ExcludeArrears          = "arrears"
	ExcludeInsuranceExpired = "insurance_expired"
	ExcludeAlreadyRenewed   = "already_renewed"
	ExcludeNoPrice          = "no_price"
	ExcludeBerthUnavailable = "berth_unavailable"
	ExcludeManual           = "manual"
)

// NextPeriod returns the period after [starts, ends) for a term (m3-plan §13 D27 defaults): annual and monthly
// start at the old end and use the term default; seasonal and period keep their length one year later.
// startsOn, when set, overrides the start (annual/monthly keep the default length from it).
func NextPeriod(term string, starts, ends calendar.Date, startsOn *calendar.Date) (calendar.Date, calendar.Date) {
	switch term {
	case TermAnnual, TermMonthly:
		ns := ends
		if startsOn != nil {
			ns = *startsOn
		}
		ne, _ := DefaultEndsOn(term, ns)
		return ns, ne
	default:
		ns := schedule.AddMonths(starts, 12)
		if startsOn != nil {
			ns = *startsOn
		}
		return ns, ns.AddDays(starts.DaysUntil(ends))
	}
}

// exclusionFacts are the checks of a renewal candidate.
type exclusionFacts struct {
	AlreadyRenewed   bool
	Arrears          bool
	InsuranceExpired bool // only counts with the insurance policy as given by the caller
	NoPrice          bool
	BerthUnavailable bool
}

// ExclusionReason returns the reason a candidate is excluded ("" = renewable), in a fixed priority order.
func exclusionReason(f exclusionFacts) string {
	switch {
	case f.AlreadyRenewed:
		return ExcludeAlreadyRenewed
	case f.Arrears:
		return ExcludeArrears
	case f.InsuranceExpired:
		return ExcludeInsuranceExpired
	case f.NoPrice:
		return ExcludeNoPrice
	case f.BerthUnavailable:
		return ExcludeBerthUnavailable
	}
	return ""
}

// renewalDraft is a successor ready to be written (priced before the transaction).
type renewalDraft struct {
	source   *store.Contract
	starts   calendar.Date
	ends     calendar.Date
	segments []Segment
	extras   []extraLine
	services []ServiceRequest
	price    *priced
	policy   Policy
}

// prepareRenewal builds the successor of src for the next period and prices it (outside any write transaction).
// A missing price list is pricelist's error (422 no_price).
func (s *Service) prepareRenewal(ctx context.Context, p *auth.Principal, marinaID string, scope store.MarinaScope, src *store.Contract,
	startsOn *calendar.Date, priceListID string,
) (*renewalDraft, error) {
	db := s.d.DB.DB
	starts, ends := NextPeriod(src.Term, src.StartsOn, src.EndsOn, startsOn)
	lines, err := store.LinesOfContracts(ctx, db, scope, []string{src.ID})
	if err != nil {
		return nil, err
	}
	allocs, err := contractAllocs(ctx, db, scope, src.ID)
	if err != nil {
		return nil, err
	}
	rd := &renewalDraft{source: src, starts: starts, ends: ends}
	last := src.EndsOn.AddDays(-1)
	for _, l := range lines {
		switch {
		case l.LineKind == "service":
			rd.services = append(rd.services, ServiceRequest{ItemID: l.ItemID, QtyMilli: l.QtyMilli})
		case l.ResourceType == store.ResourceLand:
			rd.extras = append(rd.extras, extraLine{LandResourceID: l.ResourceID, ItemID: l.ItemID})
		default:
			al := latestAlloc(allocs, l.ID, last)
			if al == nil {
				continue
			}
			g := segmentOf(al)
			g.First, g.Last = starts, ends.AddDays(-1)
			rd.segments = append(rd.segments, g)
		}
	}
	if len(rd.segments) == 0 {
		return nil, &TransitionError{From: src.Status, Action: "renew"}
	}
	if len(rd.segments) > 1 {
		rd.segments = rd.segments[len(rd.segments)-1:]
	}
	pol, err := DecodePolicy(src.Policy)
	if err != nil {
		return nil, err
	}
	pol.Version = PolicyVersion
	rd.policy = pol
	price, err := s.price(ctx, p, marinaID, priceRequest{
		contractType: PricingType(KindMooring, src.Term), arrival: starts, departure: ends, first: rd.segments[0],
		vesselID: src.VesselID, customerID: src.CustomerID, channel: ChannelDesk, services: rd.services,
	})
	if err != nil {
		return nil, err
	}
	if priceListID != "" && price.listID() != priceListID {
		v := &auth.ValidationError{}
		v.Add("price_list_id", "not_active_for_period")
		return nil, v
	}
	rd.price = price
	return rd, nil
}

// latestAlloc returns the allocation of a line that covers night (else the one ending last), active or ended.
func latestAlloc(allocs []store.Allocation, lineID string, night calendar.Date) *store.Allocation {
	var best *store.Allocation
	for i := range allocs {
		a := &allocs[i]
		if a.ContractLineID != lineID || a.Status == store.AllocCancelled {
			continue
		}
		if !a.FirstNight.After(night) && !a.LastNight.Before(night) && a.Status == store.AllocActive {
			return a
		}
		if best == nil || a.LastNight.After(best.LastNight) {
			best = a
		}
	}
	return best
}

// createSuccessorTx writes the successor draft holding until holdUntil (or active when activate).
func (s *Service) createSuccessorTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, rd *renewalDraft,
	holdUntil *time.Time, activate bool, plan string,
) (*store.Contract, error) {
	st, err := store.ContractOccupancySettings(ctx, tx, scope, s.now())
	if err != nil {
		return nil, err
	}
	src := rd.source
	n := newContract{
		kind: KindMooring, term: src.Term, customerID: src.CustomerID, billingCustomerID: src.BillingCustomerID,
		vesselID: src.VesselID, startsOn: rd.starts, endsOn: rd.ends, channel: ChannelDesk, price: rd.price, policy: rd.policy,
		segments: rd.segments, extras: rd.extras, services: rd.services, arrival: false, plan: plan,
	}
	if !activate {
		n.expiresAt = holdUntil
	}
	return s.createMooringTx(ctx, tx, scope, a, n, activate, st)
}

// insuranceExpired reports whether the vessel has no valid insurance on the night.
func insuranceExpired(ctx context.Context, db bun.IDB, scope store.MarinaScope, vesselID string, on calendar.Date) (bool, error) {
	ok, err := deadline.InsuranceValid(ctx, db, scope, vesselID, on)
	return !ok, err
}

// checkInsurance applies the insurance policy (m4-plan §15 D29): block refuses with *InsuranceError, warn
// returns a warning.
func checkInsurance(ctx context.Context, db bun.IDB, scope store.MarinaScope, cs *store.ContractSettings, vesselID string, on calendar.Date) ([]string, error) {
	expired, err := insuranceExpired(ctx, db, scope, vesselID, on)
	if err != nil || !expired {
		return nil, err
	}
	if cs.InsurancePolicy == InsuranceBlock {
		return nil, &InsuranceError{VesselID: vesselID}
	}
	return []string{"insurance_missing"}, nil
}

// RenewInput is a simple renewal.
type RenewInput struct {
	StartsOn    *calendar.Date
	PriceListID string
	DeadlineOn  *calendar.Date // default today + renewal_deadline_days
	Send        bool
}

// Proposal is a renewal proposal with its contracts.
type Proposal struct {
	Row       store.RenewalProposal
	Source    *store.Contract
	Successor *store.Contract
	// Customer and Vessel of the source contract, for the campaign table (the customer's name only with
	// customers.read; nil when not loaded).
	Customer *Party
	Vessel   *store.Vessel
	// HoldExpiresAt is the earliest expiry of the successor draft's active holds (zero = none).
	HoldExpiresAt time.Time
	Warnings      []string
}

// successorHolds returns the hold expiry of each successor draft (contracts without an active hold are absent).
func successorHolds(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) map[string]time.Time {
	out := map[string]time.Time{}
	rows, err := store.AllocationsOfContracts(ctx, db, scope, ids)
	if err != nil {
		return out
	}
	by := map[string][]store.Allocation{}
	for _, r := range rows {
		by[r.ContractID] = append(by[r.ContractID], r.Allocation)
	}
	for id, as := range by {
		if t := holdExpiry(as); !t.IsZero() {
			out[id] = t
		}
	}
	return out
}

// RenewContract creates the renewal proposal of an active mooring contract and its successor draft holding the
// berth until the deadline (contracts.write, If-Match on the source). A second open proposal is 409
// renewal_exists; a taken berth is 409 resource_taken; nothing is written in both cases.
func (s *Service) RenewContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in RenewInput) (*Proposal, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	src, err := store.GetContract(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if src.Kind != KindMooring {
		return nil, auth.ErrNotFound
	}
	if ifMatch != 0 && src.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if src.Status != string(StatusActive) {
		return nil, &TransitionError{From: src.Status, Action: "renew"}
	}
	if open, err := store.OpenProposalOfSource(ctx, db, scope, src.ID); err != nil {
		return nil, err
	} else if open != nil {
		return nil, ErrRenewalExists
	}
	cs, err := s.settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	deadlineOn := today.AddDays(cs.RenewalDeadlineDays)
	if in.DeadlineOn != nil {
		if in.DeadlineOn.Before(today) {
			v := &auth.ValidationError{}
			v.Add("deadline_on", "in_past")
			return nil, v
		}
		deadlineOn = *in.DeadlineOn
	}
	rd, err := s.prepareRenewal(ctx, p, marinaID, scope, src, in.StartsOn, in.PriceListID)
	if err != nil {
		return nil, err
	}
	warnings, err := checkInsurance(ctx, db, scope, cs, src.VesselID, rd.starts)
	if err != nil {
		return nil, err
	}
	a := actor{p: p}
	var pid string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pr, err := s.proposeTx(ctx, tx, scope, a, rd, "", deadlineOn, ProposalActionPropose)
		if err != nil {
			return err
		}
		pid = pr.ID
		if in.Send {
			return s.sendProposalTx(ctx, tx, scope, a, pr, true)
		}
		return nil
	})
	if err != nil {
		return nil, s.withAlternatives(ctx, p, marinaID, src.VesselID, err)
	}
	out, err := s.proposal(ctx, db, scope, pid)
	if out != nil {
		out.Warnings = warnings
	}
	return out, err
}

// proposeTx writes a proposal (preview or propose) with its successor draft holding until the deadline end.
// The proposal row is inserted first so a concurrent renewal of the same source fails before any allocation.
func (s *Service) proposeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, rd *renewalDraft, campaignID string,
	deadlineOn calendar.Date, action Action,
) (*store.RenewalProposal, error) {
	t, err := NextProposal("", action)
	if err != nil {
		return nil, err
	}
	// The source is locked and re-checked first: a termination or a tacit renewal committed after the
	// unlocked read must not leave an open proposal on a contract that is no longer active.
	if err := lockSourceActive(ctx, tx, scope, rd.source); err != nil {
		return nil, err
	}
	now := s.now()
	total := rd.price.total()
	pr := &store.RenewalProposal{
		ID: store.NewID(), MarinaID: scope.ID(), CampaignID: campaignID, SourceContractID: rd.source.ID,
		Status: string(t.To), OldTotalCents: rd.source.TotalCents, NewTotalCents: &total, DeadlineOn: deadlineOn,
		CreatedByUserID: a.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertRenewalProposal(ctx, tx, pr); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrRenewalExists
		}
		return nil, err
	}
	until := endOfDay(deadlineOn, location(ctx, tx, scope))
	succ, err := s.createSuccessorTx(ctx, tx, scope, a, rd, &until, false, PlanStandard)
	if err != nil {
		return nil, err
	}
	pr.NewContractID = succ.ID
	if err := store.UpdateRenewalProposal(ctx, tx, scope, pr.Version, pr, now); err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "renewal_proposal.create", entityProposal, pr.ID, map[string]any{
		"status": pr.Status, "campaign_id": campaignID, "source_contract_id": rd.source.ID, "new_contract_id": succ.ID,
		"old_total_cents": pr.OldTotalCents, "new_total_cents": total, "deadline_on": deadlineOn,
	}); err != nil {
		return nil, err
	}
	return pr, s.emitProposal(ctx, tx, scope, t.Events, pr)
}

// lockSourceActive locks the source of a renewal and requires it still active at the version it was priced at
// (lock order: source contract first, then its proposals and successor).
func lockSourceActive(ctx context.Context, tx bun.IDB, scope store.MarinaScope, src *store.Contract) error {
	c, err := lockContract(ctx, tx, scope, src.ID, KindMooring, 0)
	if err != nil {
		return err
	}
	if c.Status != string(StatusActive) {
		return &TransitionError{From: c.Status, Action: "renew"}
	}
	if c.Version != src.Version {
		return ErrVersionMismatch
	}
	return nil
}

// excludeTx records an excluded proposal (no successor, no hold).
func (s *Service) excludeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, src *store.Contract, campaignID string,
	deadlineOn calendar.Date, reason string, newTotal *int64,
) (*store.RenewalProposal, error) {
	now := s.now()
	pr := &store.RenewalProposal{
		ID: store.NewID(), MarinaID: scope.ID(), CampaignID: campaignID, SourceContractID: src.ID,
		Status: string(ProposalExcluded), ExclusionReason: reason, OldTotalCents: src.TotalCents, NewTotalCents: newTotal,
		DeadlineOn: deadlineOn, CreatedByUserID: a.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertRenewalProposal(ctx, tx, pr); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrRenewalExists
		}
		return nil, err
	}
	return pr, s.writeAudit(ctx, tx, scope, a, "renewal_proposal.create", entityProposal, pr.ID, map[string]any{
		"status": pr.Status, "campaign_id": campaignID, "source_contract_id": src.ID, "exclusion_reason": reason,
	})
}

// emitProposal writes the events of a proposal transition.
func (s *Service) emitProposal(ctx context.Context, tx bun.IDB, scope store.MarinaScope, events []string, pr *store.RenewalProposal) error {
	for _, typ := range events {
		var data any
		switch typ {
		case event.RenewalCampaignChanged:
			data = event.RenewalCampaignData{CampaignID: pr.CampaignID, Status: string(CampaignDraft), Counts: map[string]int{}}
		default:
			data = event.RenewalData{
				ProposalID: pr.ID, CampaignID: pr.CampaignID, SourceContractID: pr.SourceContractID,
				NewContractID: pr.NewContractID, Status: pr.Status,
			}
		}
		if err := s.emit(ctx, tx, scope, typ, data); err != nil {
			return err
		}
	}
	return nil
}

// proposal loads a proposal with its contracts.
func (s *Service) proposal(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*Proposal, error) {
	pr, err := store.GetRenewalProposal(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	out := &Proposal{Row: *pr}
	ids := []string{pr.SourceContractID}
	if pr.NewContractID != "" {
		ids = append(ids, pr.NewContractID)
	}
	cs, err := store.ContractsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		switch cs[i].ID {
		case pr.SourceContractID:
			out.Source = &cs[i]
		case pr.NewContractID:
			out.Successor = &cs[i]
		}
	}
	if pr.NewContractID != "" {
		out.HoldExpiresAt = successorHolds(ctx, db, scope, []string{pr.NewContractID})[pr.NewContractID]
	}
	return out, nil
}

// GetRenewalProposal returns a proposal (contracts.read).
func (s *Service) GetRenewalProposal(ctx context.Context, p *auth.Principal, marinaID, id string) (*Proposal, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	out, err := s.proposal(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	ps := []Proposal{*out}
	s.withParties(ctx, s.d.DB.DB, scope, s.can(ctx, p, marinaID, auth.PermCustomersRead), ps)
	return &ps[0], nil
}

// releaseSuccessorTx cancels the successor of a closed proposal unless it is active (holds released at once).
func (s *Service) releaseSuccessorTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal, reason string) error {
	if pr.NewContractID == "" {
		return nil
	}
	c, err := lockContract(ctx, tx, scope, pr.NewContractID, "", 0)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return nil
		}
		return err
	}
	t, err := Next(MooringTransitions, Status(c.Status), ActionCancel)
	if err != nil {
		return nil //nolint:nilerr // active (or later) successors are kept
	}
	return s.cancelContractTx(ctx, tx, scope, a, c, t, reason)
}

// jobPrincipal is the principal the renewal jobs price with (pricing.read on one marina only).
func jobPrincipal(scope store.MarinaScope) *auth.Principal {
	return &auth.Principal{
		Kind: auth.KindAPIKey, Grants: []auth.Grant{{OrganizationID: scope.OrgID(), MarinaID: scope.ID()}},
		Scopes: []auth.Permission{auth.PermPricingRead},
	}
}

// isNoPrice reports whether err means no active list prices the period.
func isNoPrice(err error) bool {
	return errors.Is(err, pricelist.ErrNoActiveList)
}

// isTaken reports whether err is an occupancy conflict or a blocking fit (the berth cannot be held).
func isTaken(err error) bool {
	_, conflict := errors.AsType[*occupancy.ConflictError](err)
	return conflict || errors.Is(err, occupancy.ErrResourceTaken) || errors.Is(err, occupancy.ErrFitBlocked)
}
