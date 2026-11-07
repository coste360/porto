package contract

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Tacit renewal (spec 13.8; m4-plan §3.1 tacit_renew): at the end of an active contract whose policy has the
// tacit clause, with no open proposal and no exclusion, the expiry job creates the successor active (same
// berth, vessel and services, new list price, plan, contract allocation), marks the source renewed and tells the
// customer. Excluded contracts are not renewed and are reported in the audit log for the office.

// tacitRenewalsMarina renews tacitly the ended contracts of a marina, one transaction each, pricing each
// successor before its transaction.
func (s *Service) tacitRenewalsMarina(ctx context.Context, scope store.MarinaScope) (int, error) {
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	rows, err := store.ActiveContractsEnded(ctx, db, scope, today, expiryBatch)
	if err != nil {
		return 0, err
	}
	cs, err := s.settingsTx(ctx, db, scope)
	if err != nil {
		return 0, err
	}
	p := jobPrincipal(scope)
	n := 0
	for i := range rows {
		src := &rows[i]
		if src.Kind != KindMooring {
			continue
		}
		pol, err := DecodePolicy(src.Policy)
		if err != nil {
			continue
		}
		if _, ren, _ := effective(pol, cs); !ren.Tacit {
			continue
		}
		open, err := store.OpenProposalOfSource(ctx, db, scope, src.ID)
		if err != nil {
			return n, err
		}
		succ, err := store.SignedSuccessorOf(ctx, db, scope, src.ID)
		if err != nil {
			return n, err
		}
		if open != nil || succ != nil {
			continue
		}
		reason, rd, err := s.tacitCheck(ctx, p, scope, cs, src)
		if err != nil {
			return n, err
		}
		if reason == "" {
			err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				return s.tacitRenewTx(ctx, tx, scope, src.ID, rd)
			})
			switch {
			case err == nil:
				n++
				continue
			case isTaken(err):
				reason = ExcludeBerthUnavailable
			case errors.Is(err, ErrRenewalExists), errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrVersionMismatch):
				continue
			default:
				return n, err
			}
		}
		if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			return s.tacitBlockedTx(ctx, tx, scope, src, reason, today)
		}); err != nil {
			return n, err
		}
	}
	return n, nil
}

// tacitCheck returns the exclusion reason of a tacit renewal, or the priced successor.
func (s *Service) tacitCheck(ctx context.Context, p *auth.Principal, scope store.MarinaScope, cs *store.ContractSettings, src *store.Contract) (string, *renewalDraft, error) {
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	var f exclusionFacts
	var err error
	if f.Arrears, err = store.OverdueInstallmentExists(ctx, db, scope, src.CustomerID, "", today.AddDays(-cs.ArrearsDays)); err != nil {
		return "", nil, err
	}
	ns, _ := NextPeriod(src.Term, src.StartsOn, src.EndsOn, nil)
	if expired, err := insuranceExpired(ctx, db, scope, src.VesselID, ns); err != nil {
		return "", nil, err
	} else if expired && cs.InsurancePolicy == InsuranceBlock {
		f.InsuranceExpired = true
	}
	var rd *renewalDraft
	if r := exclusionReason(f); r != "" {
		return r, nil, nil
	}
	rd, err = s.prepareRenewal(ctx, p, scope.ID(), scope, src, nil, "")
	if isNoPrice(err) {
		return ExcludeNoPrice, nil, nil
	}
	if _, ok := errors.AsType[*TransitionError](err); ok {
		return ExcludeBerthUnavailable, nil, nil
	}
	return "", rd, err
}

// tacitRenewTx creates the active successor and marks the source renewed (contract.renewed tacit, successor
// contract.created + contract.activated).
func (s *Service) tacitRenewTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, srcID string, rd *renewalDraft) error {
	src, err := lockContract(ctx, tx, scope, srcID, KindMooring, 0)
	if err != nil {
		return err
	}
	t, err := Next(MooringTransitions, Status(src.Status), ActionTacitRenew)
	if err != nil {
		return err
	}
	if src.Version != rd.source.Version {
		return ErrVersionMismatch
	}
	// Re-checked under the source lock (proposeTx takes the same lock): a desk renewal or campaign proposal
	// created after the unlocked read wins over the tacit clause.
	if open, err := store.OpenProposalOfSource(ctx, tx, scope, src.ID); err != nil {
		return err
	} else if open != nil {
		return ErrRenewalExists
	}
	if succ, err := store.SignedSuccessorOf(ctx, tx, scope, src.ID); err != nil {
		return err
	} else if succ != nil {
		return ErrRenewalExists
	}
	succ, err := s.createSuccessorTx(ctx, tx, scope, system, rd, nil, true, PlanStandard)
	if err != nil {
		return err
	}
	src.Status, src.RenewedAt = string(t.To), s.now()
	if err := updateContract(ctx, tx, scope, src.Version, src, s.now()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, scope, system, "contract.tacit_renew", entityContract, src.ID, map[string]any{
		"from_status": string(StatusActive), "status": src.Status, "successor_contract_id": succ.ID,
		"new_total_cents": succ.TotalCents,
	}); err != nil {
		return err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: src, successor: succ.ID, tacit: true}); err != nil {
		return err
	}
	if _, err := s.queueMailTx(ctx, tx, scope, Mail{
		Template: MailRenewalTacitNotice, CustomerID: succ.CustomerID, DedupeKey: "contract:" + succ.ID + ":tacit",
		Values: map[string]string{"contract_code": Code(succ), "starts_on": succ.StartsOn.String(), "ends_on": succ.EndsOn.String()},
	}); err != nil && !errors.Is(err, ErrCustomerEmailMissing) {
		return err
	}
	return nil
}

// Deadline written for the office when a tacit renewal is blocked (m4-plan §3.4). The deadline engine does not
// reconcile this kind: it stays open until the office closes it.
const (
	deadlineKindTacitBlocked = "tacit_renewal_blocked"
	deadlineSourceContract   = "contract"
)

// tacitBlockedTx records a blocked tacit renewal: the audit row plus an open tacit_renewal_blocked deadline on
// the source contract (due today, the exclusion reason as note), idempotent through deadlines_source_uidx.
func (s *Service) tacitBlockedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, src *store.Contract, reason string, today calendar.Date) error {
	now := s.now()
	d := &store.Deadline{
		ID: store.NewID(), MarinaID: scope.ID(), SourceType: deadlineSourceContract, SourceID: src.ID,
		Kind: deadlineKindTacitBlocked, CustomerID: src.CustomerID, VesselID: src.VesselID, DueOn: today,
		Status: "open", Note: reason, NotifiedAt: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	created, err := store.InsertDeadlineIfAbsent(ctx, tx, d)
	if err != nil {
		return err
	}
	if !created {
		return nil // already reported by an earlier run
	}
	// A distinct action: C35 accepts "contract.tacit_renew" as the proof of a tacit renewal.
	if err := s.writeAudit(ctx, tx, scope, system, "contract.tacit_renew_blocked", entityContract, src.ID, map[string]any{
		"blocked": true, "reason": reason, "deadline_kind": deadlineKindTacitBlocked, "deadline_id": d.ID,
	}); err != nil {
		return err
	}
	_, err = s.d.Outbox.Write(ctx, tx, &scope, event.DeadlineChanged, event.DeadlineChangedData{DeadlineID: d.ID, Change: event.ChangeCreated})
	return err
}
