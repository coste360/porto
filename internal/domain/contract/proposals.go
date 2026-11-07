package contract

import (
	"context"
	"errors"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Renewal proposal transitions (m4-plan §3.4 proposal table). Every change writes an audit row and its events in
// the transaction of the change; the successor's activation never depends on the renewals.sync job.

// Acceptance channels.
const (
	ViaPanel = "panel"
	ViaLink  = "link"
)

// Link purposes and subject types of this package.
const (
	LinkQuote   = "quote"
	LinkRenewal = "renewal"
)

// closedProposal maps a refused transition of a proposal no longer open to ErrProposalClosed.
func closedProposal(pr *store.RenewalProposal, err error) error {
	switch RenewalProposalStatus(pr.Status) {
	case ProposalDeclined, ProposalExpired, ProposalCompleted:
		return ErrProposalClosed
	}
	return err
}

// moveProposalTx applies a proposal transition, writes the audit row and the events.
func (s *Service) moveProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal,
	action Action, auditAction string, diff map[string]any, apply func(),
) error {
	t, err := NextProposal(RenewalProposalStatus(pr.Status), action)
	if err != nil {
		return closedProposal(pr, err)
	}
	from := pr.Status
	pr.Status = string(t.To)
	if apply != nil {
		apply()
	}
	if err := store.UpdateRenewalProposal(ctx, tx, scope, pr.Version, pr, s.now()); err != nil {
		if errors.Is(err, store.ErrVersionMismatch) {
			return ErrVersionMismatch
		}
		return err
	}
	if diff == nil {
		diff = map[string]any{}
	}
	diff["from_status"], diff["status"] = from, pr.Status
	if err := s.writeAudit(ctx, tx, scope, a, auditAction, entityProposal, pr.ID, diff); err != nil {
		return err
	}
	return s.emitProposal(ctx, tx, scope, t.Events, pr)
}

// sendProposalTx sends a proposed (or resends a sent) proposal: proposal PDF, magic link (purpose renewal,
// expiring at the deadline end, rotated on resend), email with the PDF (dedupe renewal:<id>:sent:<n>).
// Without an email the panel gets 422 customer_email_missing; the job (requireEmail false) marks the proposal
// sent anyway so the office can hand the printed proposal over (audited as no_email).
func (s *Service) sendProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal, requireEmail bool) error {
	if _, err := NextProposal(RenewalProposalStatus(pr.Status), ProposalActionSend); err != nil {
		return closedProposal(pr, err)
	}
	src, err := store.GetContract(ctx, tx, scope, pr.SourceContractID)
	if err != nil {
		return err
	}
	to, _, err := customerEmail(ctx, tx, scope, src.CustomerID)
	if err != nil {
		return err
	}
	if to == "" && requireEmail {
		return ErrCustomerEmailMissing
	}
	docID, err := s.composeTx(ctx, tx, scope, a, docs.KindRenewalProposal, docs.SubjectRenewalProposal, pr.ID, false)
	if err != nil {
		return err
	}
	link, linkID := "", ""
	if s.links != nil {
		token, id, err := s.links.CreateTx(ctx, tx, scope, LinkRenewal, string(docs.SubjectRenewalProposal), pr.ID, src.CustomerID,
			endOfDay(pr.DeadlineOn, location(ctx, tx, scope)))
		if err != nil {
			return err
		}
		m, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return err
		}
		link, linkID = s.links.URL(m.Slug, LinkRenewal, token), id
	}
	n := pr.SentCount + 1
	vals := map[string]string{
		"contract_code": Code(src), "deadline_on": pr.DeadlineOn.String(), "link": link,
		"old_total": money.Cents(pr.OldTotalCents).String(),
	}
	if pr.NewTotalCents != nil {
		vals["new_total"] = money.Cents(*pr.NewTotalCents).String()
	}
	var attach []string
	if docID != "" {
		attach = []string{docID}
	}
	if to != "" {
		if _, err := s.queueMailTx(ctx, tx, scope, Mail{
			Template: MailRenewalProposal, CustomerID: src.CustomerID, DedupeKey: "renewal:" + pr.ID + ":sent:" + strconv.Itoa(n),
			Values: vals, DocumentIDs: attach,
		}); err != nil {
			return err
		}
	}
	return s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionSend, "renewal_proposal.send", map[string]any{
		"sent_count": n, "document_id": docID, "link_id": linkID, "no_email": to == "",
	}, func() {
		pr.SentCount, pr.SentAt = n, s.now()
		if linkID != "" {
			pr.LinkID = linkID
		}
	})
}

// SendRenewalProposal sends or resends a proposal from the panel (contracts.write, If-Match).
func (s *Service) SendRenewalProposal(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Proposal, error) {
	return s.onProposal(ctx, p, marinaID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, pr *store.RenewalProposal) error {
		return s.sendProposalTx(ctx, tx, scope, a, pr, true)
	})
}

// onProposal runs fn on a locked proposal (contracts.write, If-Match).
func (s *Service) onProposal(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int,
	fn func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, pr *store.RenewalProposal) error,
) (*Proposal, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pr, err := store.LockRenewalProposal(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ifMatch != 0 && pr.Version != ifMatch {
			return ErrVersionMismatch
		}
		return fn(ctx, tx, scope, a, pr)
	})
	if err != nil {
		return nil, err
	}
	return s.proposal(ctx, s.d.DB.DB, scope, id)
}

// AcceptRenewalProposal accepts a sent proposal on behalf of the customer (contracts.write, If-Match).
func (s *Service) AcceptRenewalProposal(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, plan string) (*Proposal, error) {
	return s.onProposal(ctx, p, marinaID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, pr *store.RenewalProposal) error {
		_, err := s.acceptProposalTx(ctx, tx, scope, a, pr, plan, ViaPanel)
		return err
	})
}

// acceptProposalTx is the accept transition: the plan choice replans the successor (early payment), then with a
// signature driver other than paper the successor goes to awaiting_signature (the signature request id is
// returned); with paper it waits for "firmato su carta".
func (s *Service) acceptProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal, plan, via string) (string, error) {
	if plan == "" {
		plan = PlanStandard
	}
	if plan != PlanStandard && plan != PlanEarlyPayment {
		v := &auth.ValidationError{}
		v.Add("plan", "invalid")
		return "", v
	}
	if pr.DeadlineOn.Before(s.today(ctx, tx, scope)) {
		return "", ErrProposalClosed
	}
	if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionAccept, "renewal_proposal.accept", map[string]any{
		"plan": plan, "via": via,
	}, func() {
		pr.AcceptedAt, pr.AcceptedVia, pr.AcceptedPlan, pr.DecidedAt = s.now(), via, plan, s.now()
	}); err != nil {
		return "", err
	}
	c, err := lockContract(ctx, tx, scope, pr.NewContractID, KindMooring, 0)
	if err != nil {
		return "", err
	}
	if plan == PlanEarlyPayment {
		if _, err := s.replanTx(ctx, tx, scope, a, c, PlanEarlyPayment); err != nil {
			return "", err
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return "", err
		}
		if err := s.repricedTx(ctx, tx, scope, a, c, RepriceEarlyPayment); err != nil {
			return "", err
		}
	}
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	if cs.SignatureDriver == DriverPaper || s.signer == nil {
		return "", nil
	}
	return s.requestContractSignatureTx(ctx, tx, scope, a, c, SignatureInput{})
}

// DeclineRenewalProposal declines a proposed or sent proposal (contracts.write, If-Match): the successor is
// cancelled and its hold released at once.
func (s *Service) DeclineRenewalProposal(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Proposal, error) {
	reason, ok := cleanText(reason, 500)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("reason", "too_long")
		return nil, v
	}
	return s.onProposal(ctx, p, marinaID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, pr *store.RenewalProposal) error {
		return s.declineProposalTx(ctx, tx, scope, a, pr, reason, ViaPanel)
	})
}

func (s *Service) declineProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal, reason, via string) error {
	if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionDecline, "renewal_proposal.decline", map[string]any{"via": via}, func() {
		pr.DeclineReason, pr.DecidedAt = reason, s.now()
	}); err != nil {
		return err
	}
	return s.releaseSuccessorTx(ctx, tx, scope, a, pr, "renewal declined")
}

// expireProposalTx expires an open proposal (deadline passed or campaign closed): the successor is cancelled
// unless active, its signature request with it.
func (s *Service) expireProposalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, pr *store.RenewalProposal) error {
	if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionExpire, "renewal_proposal.expire", nil, func() {
		pr.DecidedAt = s.now()
	}); err != nil {
		return err
	}
	return s.releaseSuccessorTx(ctx, tx, scope, a, pr, "renewal expired")
}

// IncludeRenewalProposal includes an excluded proposal with a recorded reason (contracts.write, If-Match): the
// successor and its hold are created now (409 when the berth is taken). berth_unavailable cannot be included.
func (s *Service) IncludeRenewalProposal(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Proposal, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, 500)
	if reason == "" || !ok {
		v := &auth.ValidationError{}
		v.Add("reason", "required")
		return nil, v
	}
	db := s.d.DB.DB
	pr, err := store.GetRenewalProposal(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && pr.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if pr.Status != string(ProposalExcluded) || pr.ExclusionReason == ExcludeBerthUnavailable || pr.ExclusionReason == ExcludeAlreadyRenewed {
		return nil, &TransitionError{From: pr.Status, Action: ProposalActionInclude}
	}
	src, err := store.GetContract(ctx, db, scope, pr.SourceContractID)
	if err != nil {
		return nil, err
	}
	rd, err := s.prepareRenewal(ctx, p, marinaID, scope, src, nil, "")
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := lockSourceActive(ctx, tx, scope, src); err != nil {
			return err
		}
		pr, err := store.LockRenewalProposal(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		action := ProposalActionInclude
		if pr.CampaignID != "" {
			if c, err := store.GetRenewalCampaign(ctx, tx, scope, pr.CampaignID); err == nil && c.Status != string(CampaignDraft) {
				action = ProposalActionPropose
			}
		}
		t, err := NextProposal(RenewalProposalStatus(pr.Status), action)
		if err != nil {
			return err
		}
		// The proposal becomes open first (open unique index), then the successor holds the berth.
		from := pr.Status
		pr.Status, pr.IncludedReason = string(t.To), reason
		total := rd.price.total()
		pr.NewTotalCents = &total
		if err := store.UpdateRenewalProposal(ctx, tx, scope, pr.Version, pr, s.now()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrRenewalExists
			}
			return err
		}
		until := endOfDay(pr.DeadlineOn, location(ctx, tx, scope))
		succ, err := s.createSuccessorTx(ctx, tx, scope, a, rd, &until, false, PlanStandard)
		if err != nil {
			return err
		}
		pr.NewContractID = succ.ID
		if err := store.UpdateRenewalProposal(ctx, tx, scope, pr.Version, pr, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "renewal_proposal.include", entityProposal, pr.ID, map[string]any{
			"from_status": from, "status": pr.Status, "exclusion_reason": pr.ExclusionReason, "new_contract_id": succ.ID,
		}); err != nil {
			return err
		}
		return s.emitProposal(ctx, tx, scope, t.Events, pr)
	})
	if err != nil {
		return nil, err
	}
	return s.proposal(ctx, db, scope, id)
}

// onSuccessorActivatedTx advances the proposal of a successor that was just signed or activated on paper: an
// unaccepted proposal is accepted first (the office signed for the customer), then signed when a deposit is
// waiting for its settlement, completed otherwise.
func (s *Service) onSuccessorActivatedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, first *store.Installment) error {
	pr, err := store.OpenProposalOfSuccessor(ctx, tx, scope, c.ID)
	if err != nil || pr == nil {
		return err
	}
	switch RenewalProposalStatus(pr.Status) {
	case ProposalPreviewed:
		if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionLaunch, "renewal_proposal.send", nil, nil); err != nil {
			return err
		}
		fallthrough
	case ProposalProposed:
		pr.SentAt = s.now()
		if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionSend, "renewal_proposal.send", map[string]any{"manual": true}, nil); err != nil {
			return err
		}
		fallthrough
	case ProposalSent:
		if err := s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionAccept, "renewal_proposal.accept", map[string]any{"via": ViaPanel}, func() {
			pr.AcceptedAt, pr.AcceptedVia, pr.AcceptedPlan, pr.DecidedAt = s.now(), ViaPanel, PlanStandard, s.now()
		}); err != nil {
			return err
		}
	}
	if pr.Status != string(ProposalAccepted) {
		return nil
	}
	if first != nil && first.AmountCents > 0 {
		return s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionSign, "renewal_proposal.sign", map[string]any{"deposit_installment_id": first.ID}, nil)
	}
	return s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionComplete, "renewal_proposal.complete", nil, nil)
}

// completeProposalOnDepositTx completes the signed proposal of a successor whose deposit was just settled.
func (s *Service) completeProposalOnDepositTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, contractID string) error {
	pr, err := store.OpenProposalOfSuccessor(ctx, tx, scope, contractID)
	if err != nil || pr == nil || pr.Status != string(ProposalSigned) {
		return err
	}
	return s.moveProposalTx(ctx, tx, scope, a, pr, ProposalActionComplete, "renewal_proposal.complete", map[string]any{"deposit_settled": true}, nil)
}
