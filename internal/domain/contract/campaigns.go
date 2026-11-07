package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Renewal campaigns (spec 13.8; m4-plan §3.4, §15 D4–D6, D31): filter → preview (proposals and successor
// drafts holding the berths, exclusions with reasons) → launch → the renewals.sync job sends each proposal in its
// own transaction → open → closed (deadline or manual). Every proposal is processed in its own transaction.

const (
	entityCampaign = "renewal_campaign"
	syncBatch      = 200
	maxCandidates  = 5000
)

// Target start rules.
const (
	StartsNextTerm = "next_term"
	StartsFixed    = "fixed"
)

// CampaignFilter selects the active contracts of a campaign.
type CampaignFilter struct {
	Groups             []string       `json:"groups,omitempty"`
	Structures         []string       `json:"structures,omitempty"` // berth groups of kind structure
	Terms              []string       `json:"terms,omitempty"`
	EndsFrom           *calendar.Date `json:"ends_from,omitempty"`
	EndsTo             *calendar.Date `json:"ends_to,omitempty"`
	CustomerCategories []string       `json:"customer_categories,omitempty"`
	ContractIDs        []string       `json:"contract_ids,omitempty"`
}

// CampaignInput creates a campaign.
type CampaignInput struct {
	Name             string
	Filter           CampaignFilter
	DeadlineOn       calendar.Date
	PriceListID      string
	TargetStartsRule string // next_term (default), fixed
	TargetStartsOn   *calendar.Date
	Message          string
}

// CampaignPatch updates a draft campaign.
type CampaignPatch struct {
	Name           *string
	Filter         *CampaignFilter
	DeadlineOn     *calendar.Date
	PriceListID    *string
	TargetStartsOn *calendar.Date
	Message        *string
}

// Campaign is a campaign with its counts per proposal status.
type Campaign struct {
	Row    store.RenewalCampaign
	Filter CampaignFilter
	Counts map[string]int
}

func validateCampaign(v *auth.ValidationError, name, message string, f CampaignFilter, rule string, startsOn *calendar.Date) {
	if n := len([]rune(name)); n < 1 || n > 120 {
		v.Add("name", "invalid")
	}
	if len([]rune(message)) > 4000 {
		v.Add("message", "too_long")
	}
	for _, t := range f.Terms {
		if !contains(terms, t) {
			v.Add("filter.terms", "invalid")
		}
	}
	if len(f.ContractIDs) > maxCandidates || len(f.Groups) > 200 || len(f.Structures) > 200 {
		v.Add("filter", "too_many")
	}
	if rule != StartsNextTerm && rule != StartsFixed {
		v.Add("target_starts_rule", "invalid")
	}
	if rule == StartsFixed && startsOn == nil {
		v.Add("target_starts_on", "required")
	}
}

// CreateRenewalCampaign creates a draft campaign (contracts.write).
func (s *Service) CreateRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID string, in CampaignInput) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if in.TargetStartsRule == "" {
		in.TargetStartsRule = StartsNextTerm
	}
	v := &auth.ValidationError{}
	validateCampaign(v, in.Name, in.Message, in.Filter, in.TargetStartsRule, in.TargetStartsOn)
	today := s.today(ctx, s.d.DB.DB, scope)
	if in.DeadlineOn.IsZero() || in.DeadlineOn.Before(today) {
		v.Add("deadline_on", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a := actor{p}
	now := s.now()
	c := &store.RenewalCampaign{
		ID: store.NewID(), MarinaID: scope.ID(), Name: in.Name, Filter: store.MustJSON(in.Filter), PriceListID: in.PriceListID,
		TargetStartsRule: in.TargetStartsRule, DeadlineOn: in.DeadlineOn, Status: string(CampaignDraft), Message: in.Message,
		Counts: store.JSON(`{}`), CreatedByUserID: a.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if in.TargetStartsOn != nil {
		c.TargetStartsOn = *in.TargetStartsOn
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertRenewalCampaign(ctx, tx, c); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "renewal_campaign.create", entityCampaign, c.ID, map[string]any{
			"status": c.Status, "deadline_on": c.DeadlineOn,
		}); err != nil {
			return err
		}
		return s.emitCampaign(ctx, tx, scope, c, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, c.ID)
}

func (s *Service) emitCampaign(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.RenewalCampaign, counts map[string]int) error {
	if counts == nil {
		counts = map[string]int{}
	}
	return s.emit(ctx, tx, scope, event.RenewalCampaignChanged, event.RenewalCampaignData{CampaignID: c.ID, Status: c.Status, Counts: counts})
}

func (s *Service) campaign(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*Campaign, error) {
	c, err := store.GetRenewalCampaign(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	counts, err := store.CountProposalsByStatus(ctx, db, scope, c.ID)
	if err != nil {
		return nil, err
	}
	out := &Campaign{Row: *c, Counts: counts}
	_ = json.Unmarshal(c.Filter, &out.Filter)
	return out, nil
}

// GetRenewalCampaign returns a campaign (contracts.read).
func (s *Service) GetRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, id)
}

// ListRenewalCampaigns returns a page of campaigns (contracts.read).
func (s *Service) ListRenewalCampaigns(ctx context.Context, p *auth.Principal, marinaID, status, cursor string, limit int) (store.Page[Campaign], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[Campaign]{}, err
	}
	page, err := store.ListRenewalCampaigns(ctx, s.d.DB.DB, scope, status, cursor, limit)
	if err != nil {
		return store.Page[Campaign]{}, err
	}
	out := store.Page[Campaign]{NextCursor: page.NextCursor, Items: make([]Campaign, 0, len(page.Items))}
	for _, c := range page.Items {
		counts, err := store.CountProposalsByStatus(ctx, s.d.DB.DB, scope, c.ID)
		if err != nil {
			return store.Page[Campaign]{}, err
		}
		cc := Campaign{Row: c, Counts: counts}
		_ = json.Unmarshal(c.Filter, &cc.Filter)
		out.Items = append(out.Items, cc)
	}
	return out, nil
}

// ListCampaignProposals returns a page of a campaign's proposals with their contracts (contracts.read).
func (s *Service) ListCampaignProposals(ctx context.Context, p *auth.Principal, marinaID, id, status, cursor string, limit int) (store.Page[Proposal], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[Proposal]{}, err
	}
	db := s.d.DB.DB
	if _, err := store.GetRenewalCampaign(ctx, db, scope, id); err != nil {
		return store.Page[Proposal]{}, notFound(err)
	}
	page, err := store.ListRenewalProposals(ctx, db, scope, id, status, cursor, limit)
	if err != nil {
		return store.Page[Proposal]{}, err
	}
	return store.Page[Proposal]{NextCursor: page.NextCursor, Items: s.withContracts(ctx, db, scope, page.Items)}, nil
}

// AllCampaignProposals returns every proposal of a campaign with its contracts (CSV export, contracts.read).
func (s *Service) AllCampaignProposals(ctx context.Context, p *auth.Principal, marinaID, id, status string) ([]Proposal, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if _, err := store.GetRenewalCampaign(ctx, db, scope, id); err != nil {
		return nil, notFound(err)
	}
	var st []string
	if status != "" {
		st = []string{status}
	}
	rows, err := store.ProposalsOfCampaign(ctx, db, scope, id, st, 0)
	if err != nil {
		return nil, err
	}
	return s.withContracts(ctx, db, scope, rows), nil
}

func (s *Service) withContracts(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.RenewalProposal) []Proposal {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.SourceContractID)
		if r.NewContractID != "" {
			ids = append(ids, r.NewContractID)
		}
	}
	cs, _ := store.ContractsByIDs(ctx, db, scope, ids)
	byID := map[string]*store.Contract{}
	for i := range cs {
		byID[cs[i].ID] = &cs[i]
	}
	var succ []string
	for _, r := range rows {
		if r.NewContractID != "" {
			succ = append(succ, r.NewContractID)
		}
	}
	holds := successorHolds(ctx, db, scope, succ)
	out := make([]Proposal, 0, len(rows))
	for _, r := range rows {
		out = append(out, Proposal{Row: r, Source: byID[r.SourceContractID], Successor: byID[r.NewContractID], HoldExpiresAt: holds[r.NewContractID]})
	}
	return out
}

// UpdateRenewalCampaign changes a draft campaign (contracts.write, If-Match).
func (s *Service) UpdateRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in CampaignPatch) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.lockCampaign(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		if _, err := NextCampaign(CampaignStatus(c.Status), CampaignActionUpdate); err != nil {
			return err
		}
		var f CampaignFilter
		_ = json.Unmarshal(c.Filter, &f)
		var fields []string
		if in.Name != nil {
			c.Name, fields = *in.Name, append(fields, "name")
		}
		if in.Filter != nil {
			f, fields = *in.Filter, append(fields, "filter")
			c.Filter = store.MustJSON(f)
		}
		if in.DeadlineOn != nil {
			c.DeadlineOn, fields = *in.DeadlineOn, append(fields, "deadline_on")
		}
		if in.PriceListID != nil {
			c.PriceListID, fields = *in.PriceListID, append(fields, "price_list_id")
		}
		if in.TargetStartsOn != nil {
			c.TargetStartsOn, c.TargetStartsRule, fields = *in.TargetStartsOn, StartsFixed, append(fields, "target_starts_on")
		}
		if in.Message != nil {
			c.Message, fields = *in.Message, append(fields, "message")
		}
		v := &auth.ValidationError{}
		var startsOn *calendar.Date
		if !c.TargetStartsOn.IsZero() {
			startsOn = &c.TargetStartsOn
		}
		validateCampaign(v, c.Name, c.Message, f, c.TargetStartsRule, startsOn)
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.UpdateRenewalCampaign(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, a, "renewal_campaign.update", entityCampaign, c.ID, map[string]any{"fields": fields}); err != nil {
			return err
		}
		return s.emitCampaign(ctx, tx, scope, c, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, id)
}

func (s *Service) lockCampaign(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, ifMatch int) (*store.RenewalCampaign, error) {
	c, err := store.LockRenewalCampaign(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && c.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	return c, nil
}

// moveCampaignTx applies a campaign transition with its audit row and event.
func (s *Service) moveCampaignTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.RenewalCampaign, action Action, apply func()) error {
	t, err := NextCampaign(CampaignStatus(c.Status), action)
	if err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	if apply != nil {
		apply()
	}
	counts, err := store.CountProposalsByStatus(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	c.Counts = store.MustJSON(counts)
	if err := store.UpdateRenewalCampaign(ctx, tx, scope, c.Version, c, s.now()); err != nil {
		return versionErr(err)
	}
	if err := s.writeAudit(ctx, tx, scope, a, "renewal_campaign."+string(action), entityCampaign, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "counts": counts,
	}); err != nil {
		return err
	}
	return s.emitCampaign(ctx, tx, scope, c, counts)
}

// PreviewRenewalCampaign computes the proposals of a draft campaign (contracts.write, If-Match; idempotent:
// contracts already in the campaign are skipped). Each candidate is processed in its own transaction: a
// previewed proposal with its successor draft holding the berth until the deadline, or an excluded one with its
// reason.
func (s *Service) PreviewRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	c, err := store.GetRenewalCampaign(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && c.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if _, err := NextCampaign(CampaignStatus(c.Status), CampaignActionPreview); err != nil {
		return nil, err
	}
	var f CampaignFilter
	_ = json.Unmarshal(c.Filter, &f)
	cands, err := store.ActiveContractsFor(ctx, db, scope, store.CampaignCandidates{
		Terms: f.Terms, EndsFrom: f.EndsFrom, EndsTo: f.EndsTo, GroupIDs: append(append([]string{}, f.Groups...), f.Structures...),
		ContractIDs: f.ContractIDs, CustomerCat: f.CustomerCategories,
	}, maxCandidates)
	if err != nil {
		return nil, err
	}
	existing, err := store.ProposalsOfCampaign(ctx, db, scope, c.ID, nil, 0)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, pr := range existing {
		done[pr.SourceContractID] = true
	}
	cs, err := s.settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	for i := range cands {
		src := &cands[i]
		if done[src.ID] {
			continue
		}
		if err := s.previewOne(ctx, p, marinaID, scope, a, c, cs, src); err != nil {
			return nil, fmt.Errorf("preview %s: %w", src.ID, err)
		}
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.lockCampaign(ctx, tx, scope, id, 0)
		if err != nil {
			return err
		}
		return s.moveCampaignTx(ctx, tx, scope, a, c, CampaignActionPreview, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.campaign(ctx, db, scope, id)
}

// previewOne writes the proposal of one candidate in its own transaction.
func (s *Service) previewOne(ctx context.Context, p *auth.Principal, marinaID string, scope store.MarinaScope, a actor,
	c *store.RenewalCampaign, cs *store.ContractSettings, src *store.Contract,
) error {
	db := s.d.DB.DB
	var startsOn *calendar.Date
	if c.TargetStartsRule == StartsFixed && !c.TargetStartsOn.IsZero() {
		startsOn = &c.TargetStartsOn
	}
	var f exclusionFacts
	open, err := store.OpenProposalOfSource(ctx, db, scope, src.ID)
	if err != nil {
		return err
	}
	succ, err := store.SignedSuccessorOf(ctx, db, scope, src.ID)
	if err != nil {
		return err
	}
	f.AlreadyRenewed = open != nil || succ != nil
	today := s.today(ctx, db, scope)
	if f.Arrears, err = store.OverdueInstallmentExists(ctx, db, scope, src.CustomerID, "", today.AddDays(-cs.ArrearsDays)); err != nil {
		return err
	}
	ns, _ := NextPeriod(src.Term, src.StartsOn, src.EndsOn, startsOn)
	if f.InsuranceExpired, err = insuranceExpired(ctx, db, scope, src.VesselID, ns); err != nil {
		return err
	}
	var rd *renewalDraft
	if !f.AlreadyRenewed {
		rd, err = s.prepareRenewal(ctx, p, marinaID, scope, src, startsOn, c.PriceListID)
		switch {
		case isNoPrice(err):
			f.NoPrice = true
		case err != nil:
			var te *TransitionError
			if !errors.As(err, &te) {
				return err
			}
			f.BerthUnavailable = true
		}
	}
	var newTotal *int64
	if rd != nil {
		t := rd.price.total()
		newTotal = &t
	}
	reason := exclusionReason(f)
	if reason == "" {
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			_, err := s.proposeTx(ctx, tx, scope, a, rd, c.ID, c.DeadlineOn, ProposalActionPreview)
			return err
		})
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrRenewalExists):
			reason = ExcludeAlreadyRenewed
		case isTaken(err):
			reason = ExcludeBerthUnavailable
		case errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrVersionMismatch):
			return nil // the source changed (terminated, renewed) after it was read: nothing to propose
		default:
			return err
		}
	}
	if reason == ExcludeAlreadyRenewed && open != nil {
		// The open proposal of another campaign (or a simple renewal) owns the source: an excluded row of this
		// campaign records it without touching the other one.
		reason = ExcludeAlreadyRenewed
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := s.excludeTx(ctx, tx, scope, a, src, c.ID, c.DeadlineOn, reason, newTotal)
		if errors.Is(err, ErrRenewalExists) {
			return nil // a concurrent preview wrote it
		}
		return err
	})
}

// LaunchRenewalCampaign launches a draft campaign (contracts.write, If-Match): the campaign becomes sending and
// its previewed proposals proposed (one transaction each); the renewals.sync job sends them.
func (s *Service) LaunchRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.lockCampaign(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		return s.moveCampaignTx(ctx, tx, scope, a, c, CampaignActionLaunch, func() { c.LaunchedAt = s.now() })
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.proposeCampaign(ctx, scope, a, id); err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, id)
}

// proposeCampaign flips the previewed proposals of a sending campaign to proposed, one transaction each.
func (s *Service) proposeCampaign(ctx context.Context, scope store.MarinaScope, a actor, id string) (int, error) {
	rows, err := store.ProposalsOfCampaign(ctx, s.d.DB.DB, scope, id, []string{string(ProposalPreviewed)}, 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			pr, err := store.LockRenewalProposal(ctx, tx, scope, r.ID)
			if err != nil {
				return err
			}
			if pr.Status != string(ProposalPreviewed) {
				return nil
			}
			n++
			return s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionLaunch, "renewal_proposal.send", map[string]any{"launch": true}, nil)
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// CloseRenewalCampaign closes a sending or open campaign (contracts.write, If-Match): its open proposals expire
// (one transaction each).
func (s *Service) CloseRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if err := s.closeCampaign(ctx, scope, actor{p}, id, ifMatch); err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, id)
}

func (s *Service) closeCampaign(ctx context.Context, scope store.MarinaScope, a actor, id string, ifMatch int) error {
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.lockCampaign(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		return s.moveCampaignTx(ctx, tx, scope, a, c, CampaignActionClose, func() { c.ClosedAt = s.now() })
	})
	if err != nil {
		return err
	}
	rows, err := store.ProposalsOfCampaign(ctx, s.d.DB.DB, scope, id, []string{"previewed", "proposed", "sent", "accepted"}, 0)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			pr, err := store.LockRenewalProposal(ctx, tx, scope, r.ID)
			if err != nil {
				return err
			}
			if _, err := NextProposal(RenewalProposalStatus(pr.Status), ProposalActionExpire); err != nil {
				return nil //nolint:nilerr // moved on meanwhile (signed)
			}
			return s.expireProposalTx(ctx, tx, scope, a, pr)
		}); err != nil {
			return err
		}
	}
	return nil
}

// DiscardRenewalCampaign discards a draft campaign (contracts.write, If-Match): its proposals are deleted and
// their successor drafts cancelled with their holds.
func (s *Service) DiscardRenewalCampaign(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Campaign, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.lockCampaign(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		if _, err := NextCampaign(CampaignStatus(c.Status), CampaignActionDiscard); err != nil {
			return err
		}
		rows, err := store.ProposalsOfCampaign(ctx, tx, scope, id, nil, 0)
		if err != nil {
			return err
		}
		for i := range rows {
			if err := s.releaseSuccessorTx(ctx, tx, scope, a, &rows[i], "campaign discarded"); err != nil {
				return err
			}
			if err := store.DeleteRenewalProposal(ctx, tx, scope, rows[i].ID); err != nil {
				return err
			}
		}
		return s.moveCampaignTx(ctx, tx, scope, a, c, CampaignActionDiscard, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.campaign(ctx, s.d.DB.DB, scope, id)
}

// SyncResult counts one run of renewals.sync.
type SyncResult struct {
	Proposed, Sent, Expired, Reconciled, Opened, Closed int
}

// RunRenewalsSync is the renewals.sync job (every minute, idempotent, one transaction per proposal).
func (s *Service) RunRenewalsSync(ctx context.Context) error {
	_, err := s.RunRenewalsSyncCount(ctx)
	return err
}

// RunRenewalsSyncCount is RunRenewalsSync returning the counts (tests, seed).
func (s *Service) RunRenewalsSyncCount(ctx context.Context) (SyncResult, error) {
	var total SyncResult
	if s.alloc == nil {
		return total, nil
	}
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return total, err
	}
	var errs []error
	for _, id := range ids {
		r, err := s.SyncRenewalsMarina(ctx, id)
		if err != nil {
			s.d.Log.Error("renewals sync failed", "marina_id", id, "err", err)
			errs = append(errs, err)
		}
		total.Proposed += r.Proposed
		total.Sent += r.Sent
		total.Expired += r.Expired
		total.Reconciled += r.Reconciled
		total.Opened += r.Opened
		total.Closed += r.Closed
	}
	return total, errors.Join(errs...)
}

// SyncRenewalsMarina runs renewals.sync for one marina: sends the due proposals of sending campaigns, expires
// proposals past their deadline, reconciles accepted and signed proposals with their successor (recovery path),
// opens campaigns whose proposals are all sent and closes campaigns past their deadline.
func (s *Service) SyncRenewalsMarina(ctx context.Context, marinaID string) (SyncResult, error) {
	var r SyncResult
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return r, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	camps, err := store.CampaignsInStatus(ctx, db, scope, []string{string(CampaignSending), string(CampaignOpen)})
	if err != nil {
		return r, err
	}
	for _, c := range camps {
		if c.Status != string(CampaignSending) {
			continue
		}
		n, err := s.proposeCampaign(ctx, scope, system, c.ID)
		if err != nil {
			return r, err
		}
		r.Proposed += n
		rows, err := store.ProposalsOfCampaign(ctx, db, scope, c.ID, []string{string(ProposalProposed)}, syncBatch)
		if err != nil {
			return r, err
		}
		for _, row := range rows {
			sent := false
			err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				pr, err := store.LockRenewalProposal(ctx, tx, scope, row.ID)
				if err != nil {
					return err
				}
				if pr.Status != string(ProposalProposed) {
					return nil // a concurrent run sent it
				}
				sent = true
				return s.sendProposalTx(ctx, tx, scope, system, pr, false)
			})
			if err != nil && !errors.Is(err, ErrVersionMismatch) {
				return r, fmt.Errorf("send %s: %w", row.ID, err)
			}
			if err == nil && sent {
				r.Sent++
			}
		}
		left, err := store.ProposalsOfCampaign(ctx, db, scope, c.ID, []string{string(ProposalPreviewed), string(ProposalProposed)}, 1)
		if err != nil {
			return r, err
		}
		if len(left) == 0 {
			err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				cc, err := s.lockCampaign(ctx, tx, scope, c.ID, 0)
				if err != nil {
					return err
				}
				if cc.Status != string(CampaignSending) {
					return nil
				}
				r.Opened++
				return s.moveCampaignTx(ctx, tx, scope, system, cc, CampaignActionOpen, nil)
			})
			if err != nil {
				return r, err
			}
		}
	}
	past, err := store.ProposalsPastDeadline(ctx, db, scope, today, syncBatch)
	if err != nil {
		return r, err
	}
	for _, row := range past {
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			pr, err := store.LockRenewalProposal(ctx, tx, scope, row.ID)
			if err != nil {
				return err
			}
			if !pr.DeadlineOn.Before(today) {
				return nil
			}
			if _, err := NextProposal(RenewalProposalStatus(pr.Status), ProposalActionExpire); err != nil {
				return nil //nolint:nilerr // moved on meanwhile
			}
			if pr.Status == string(ProposalAccepted) && pr.NewContractID != "" {
				// D6: a signature started before the deadline may finish until the signature request expires.
				if succ, err := store.GetContract(ctx, tx, scope, pr.NewContractID); err == nil && succ.Status == string(StatusAwaitingSignature) {
					return nil
				}
			}
			r.Expired++
			return s.expireProposalTx(ctx, tx, scope, system, pr)
		})
		if err != nil {
			return r, fmt.Errorf("expire %s: %w", row.ID, err)
		}
	}
	for _, st := range []string{string(ProposalAccepted), string(ProposalSigned)} {
		rows, err := store.ProposalsInStatus(ctx, db, scope, st, syncBatch)
		if err != nil {
			return r, err
		}
		for _, row := range rows {
			err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				n, err := s.reconcileProposalTx(ctx, tx, scope, row.ID)
				r.Reconciled += n
				return err
			})
			if err != nil {
				return r, err
			}
		}
	}
	for _, c := range camps {
		if c.DeadlineOn.Before(today) {
			if err := s.closeCampaign(ctx, scope, system, c.ID, 0); err != nil && !errors.Is(err, ErrInvalidTransition) {
				return r, err
			}
			r.Closed++
		}
	}
	return r, nil
}

// reconcileProposalTx moves an accepted or signed proposal on from its successor's state (recovery: the main
// path runs in the transaction of the signature or the settlement).
func (s *Service) reconcileProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (int, error) {
	pr, err := store.LockRenewalProposal(ctx, tx, scope, id)
	if err != nil || pr.NewContractID == "" {
		return 0, err
	}
	c, err := store.GetContract(ctx, tx, scope, pr.NewContractID)
	if err != nil {
		return 0, err
	}
	if c.Status != string(StatusActive) {
		return 0, nil
	}
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return 0, err
	}
	var first *store.Installment
	if len(live) > 0 {
		first = &live[0]
	}
	switch RenewalProposalStatus(pr.Status) {
	case ProposalAccepted:
		if first != nil && first.AmountCents > 0 && first.Status != store.InstallmentPaid {
			return 1, s.moveProposalTx(ctx, tx, scope, system, pr, ProposalActionSign, "renewal_proposal.sign", map[string]any{"reconciled": true}, nil)
		}
		return 1, s.moveProposalTx(ctx, tx, scope, system, pr, ProposalActionComplete, "renewal_proposal.complete", map[string]any{"reconciled": true}, nil)
	case ProposalSigned:
		if first == nil || first.Status == store.InstallmentPaid {
			return 1, s.moveProposalTx(ctx, tx, scope, system, pr, ProposalActionComplete, "renewal_proposal.complete", map[string]any{"reconciled": true}, nil)
		}
	}
	return 0, nil
}
