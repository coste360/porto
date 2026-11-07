package contract

import (
	"context"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// expiryBatch bounds the documents reconciled per marina and run (m3-plan §7.6).
const expiryBatch = 500

// ExpiryResult counts what one run of the job changed.
type ExpiryResult struct {
	Holds, Transits, Quotes, DraftsLost, Contracts int
	Renewed, Tacit                                 int // M4: sources with an active successor, tacit renewals
}

// RunExpiry is the contracts.expire job (every minute, m3-plan §7.6): expired holds through
// occupancy.ExpireHoldsTx and the reconciliation of transits, quotes and contracts of §3.3 and §4.1, one
// transaction per marina.
func (s *Service) RunExpiry(ctx context.Context) error {
	_, err := s.RunExpiryCount(ctx)
	return err
}

// RunExpiryCount is RunExpiry returning the counts (tests, seed).
func (s *Service) RunExpiryCount(ctx context.Context) (ExpiryResult, error) {
	var total ExpiryResult
	if s.alloc == nil {
		return total, nil
	}
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return total, err
	}
	var errs []error
	for _, id := range ids {
		r, err := s.ExpireMarina(ctx, id)
		if err != nil {
			s.d.Log.Error("contracts expiry failed", "marina_id", id, "err", err)
			errs = append(errs, err)
			continue
		}
		total.Holds += r.Holds
		total.Transits += r.Transits
		total.Quotes += r.Quotes
		total.DraftsLost += r.DraftsLost
		total.Contracts += r.Contracts
		total.Renewed += r.Renewed
		total.Tacit += r.Tacit
	}
	return total, errors.Join(errs...)
}

// ExpireMarina runs the expiry of one marina in one transaction.
func (s *Service) ExpireMarina(ctx context.Context, marinaID string) (ExpiryResult, error) {
	var r ExpiryResult
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return r, err
	}
	tacit, err := s.tacitRenewalsMarina(ctx, scope)
	if err != nil {
		return r, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r = ExpiryResult{Tacit: tacit}
		now := s.now()
		expired, err := s.alloc.ExpireHoldsTx(ctx, tx, scope, now)
		if err != nil {
			return fmt.Errorf("expire holds: %w", err)
		}
		r.Holds = len(expired)
		if r.Transits, err = s.expireTransitsTx(ctx, tx, scope); err != nil {
			return err
		}
		if r.Quotes, err = s.expireQuotesTx(ctx, tx, scope); err != nil {
			return err
		}
		if r.DraftsLost, err = s.flagLostDraftsTx(ctx, tx, scope); err != nil {
			return err
		}
		r.Contracts, r.Renewed, err = s.expireContractsTx(ctx, tx, scope)
		return err
	})
	return r, err
}

// endHolds ends the still-active allocations of as with status expired and returns every allocation that is now
// expired (for occupancy.hold_expired).
func (s *Service) endHolds(ctx context.Context, tx bun.IDB, scope store.MarinaScope, as []store.Allocation) ([]store.Allocation, error) {
	var out []store.Allocation
	for _, al := range as {
		switch al.Status {
		case store.AllocActive:
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusExpired); err != nil {
				return nil, err
			}
			out = append(out, al)
		case store.AllocExpired:
			out = append(out, al)
		}
	}
	return out, nil
}

func (s *Service) expireTransitsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (int, error) {
	rows, err := store.HeldTransitsToExpire(ctx, tx, scope, s.now(), expiryBatch)
	if err != nil {
		return 0, err
	}
	for i := range rows {
		c := &rows[i]
		t, err := Next(TransitTransitions, Status(c.Status), ActionExpire)
		if err != nil {
			return 0, err
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return 0, err
		}
		ended, err := s.endHolds(ctx, tx, scope, allocs)
		if err != nil {
			return 0, err
		}
		c.Status = string(t.To)
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return 0, err
		}
		if err := s.writeAudit(ctx, tx, scope, system, "transit.expire", entityTransit, c.ID, map[string]any{
			"from_status": string(StatusHeld), "status": c.Status, "allocation_ids": idsOf(ended),
		}); err != nil {
			return 0, err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeExpired, expired: ended}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

func (s *Service) expireQuotesTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (int, error) {
	rows, err := store.QuotesToExpire(ctx, tx, scope, s.now(), expiryBatch)
	if err != nil {
		return 0, err
	}
	for i := range rows {
		q := &rows[i]
		t, err := Next(QuoteTransitions, Status(q.Status), ActionExpire)
		if err != nil {
			return 0, err
		}
		_, pls, allocs, err := quoteHolds(ctx, tx, scope, q.ID)
		if err != nil {
			return 0, err
		}
		var holds []store.Allocation
		for _, pl := range pls {
			if al, ok := allocs[pl.AllocationID]; ok {
				holds = append(holds, al)
			}
		}
		ended, err := s.endHolds(ctx, tx, scope, holds)
		if err != nil {
			return 0, err
		}
		from := q.Status
		q.Status = string(t.To)
		if err := updateQuote(ctx, tx, scope, q, s.now()); err != nil {
			return 0, err
		}
		if err := s.writeAudit(ctx, tx, scope, system, "quote.expire", entityQuote, q.ID, map[string]any{
			"from_status": from, "status": q.Status, "allocation_ids": idsOf(ended),
		}); err != nil {
			return 0, err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{quote: q, expired: ended}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// flagLostDraftsTx marks the draft mooring contracts whose holds are gone: they stay draft with hold_lost and
// activation allocates again or fails (m3-plan §3.3, §13 D6).
func (s *Service) flagLostDraftsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (int, error) {
	rows, err := store.DraftContractsLostHold(ctx, tx, scope, s.now(), expiryBatch)
	if err != nil {
		return 0, err
	}
	for i := range rows {
		c := &rows[i]
		t, err := Next(MooringTransitions, Status(c.Status), ActionUpdate)
		if err != nil {
			return 0, err
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return 0, err
		}
		var holds []store.Allocation
		for _, al := range allocs {
			if al.Kind == store.AllocKindHold && (al.Status == store.AllocExpired || (al.Status == store.AllocActive && !al.ExpiresAt.After(s.now()))) {
				holds = append(holds, al)
			}
		}
		ended, err := s.endHolds(ctx, tx, scope, holds)
		if err != nil {
			return 0, err
		}
		c.HoldLost = true
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return 0, err
		}
		if err := s.writeAudit(ctx, tx, scope, system, "contract.update", entityContract, c.ID, map[string]any{
			"fields": []string{"hold_lost"}, "hold_lost": true, "allocation_ids": idsOf(ended),
		}); err != nil {
			return 0, err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeUpdated}); err != nil {
			return 0, err
		}
		if len(ended) > 0 {
			if err := s.emitTransition(ctx, tx, scope, Transition{Events: []string{event.OccupancyHoldExpired}},
				subject{contract: c, expired: ended}); err != nil {
				return 0, err
			}
		}
	}
	return len(rows), nil
}

func (s *Service) expireContractsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (int, int, error) {
	rows, err := store.ActiveContractsEnded(ctx, tx, scope, s.today(ctx, tx, scope), expiryBatch)
	if err != nil {
		return 0, 0, err
	}
	renewed := 0
	for i := range rows {
		c := &rows[i]
		// M4 (m4-plan §3.1, INV-35): renewed only when a successor of a signed or completed proposal is active.
		succ, err := store.SignedSuccessorOf(ctx, tx, scope, c.ID)
		if err != nil {
			return 0, 0, err
		}
		action := ActionExpire
		if succ != nil {
			action = ActionExpireRenewed
		}
		t, err := Next(MooringTransitions, Status(c.Status), action)
		if err != nil {
			return 0, 0, err
		}
		c.Status = string(t.To)
		diff := map[string]any{"from_status": string(StatusActive), "status": c.Status, "ends_on": c.EndsOn}
		sub := subject{contract: c, change: event.ChangeExpired}
		if succ != nil {
			c.RenewedAt = s.now()
			diff["successor_contract_id"] = succ.ID
			sub.successor = succ.ID
			renewed++
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return 0, 0, err
		}
		auditAction := "contract.expire"
		if succ != nil {
			auditAction = "contract.renew"
		}
		if err := s.writeAudit(ctx, tx, scope, system, auditAction, entityContract, c.ID, diff); err != nil {
			return 0, 0, err
		}
		if err := s.emitTransition(ctx, tx, scope, t, sub); err != nil {
			return 0, 0, err
		}
	}
	return len(rows), renewed, nil
}
