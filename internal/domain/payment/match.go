package payment

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Matching (m5-plan §4.2). matchTx is the only code that changes charges.paid_amount_cents (with reverseTx), and
// it does so only through conditional updates whose affected count is checked (INV-52): two staff matching the
// same payment or charge at once can never exceed either side. Lock order: contracts and rights of the charges'
// origins (their settlement callbacks lock them), then the payment, then the charges in ascending id order.

// matchOpts tunes matchTx for its callers.
type matchOpts struct {
	auto          bool // fill the customer's open charges oldest first (matches ignored)
	autoIDs       []string
	allowDeposit  bool // retention: a deposit may be matched (to its deposit_retention charge only)
	recordContext bool // called from RecordTx (fiscal documents of receipt-mode charges)
}

// MatchTx matches a payment to charges in tx (see matchTx).
func (s *Service) MatchTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, paymentID string, matches []MatchInput) ([]store.PaymentMatch, error) {
	return s.matchTx(ctx, tx, scope, actor, paymentID, matches, matchOpts{})
}

func (s *Service) matchTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, paymentID string, matches []MatchInput, o matchOpts) ([]store.PaymentMatch, error) {
	pre, err := store.GetPayment(ctx, tx, scope, paymentID)
	if err != nil {
		return nil, err
	}
	// Candidate charges, read without locks to find the parents to lock first.
	var candidates []store.Charge
	if o.auto {
		if pre.CustomerID == "" {
			return nil, nil
		}
		if candidates, err = store.OpenCustomerCharges(ctx, tx, scope, pre.CustomerID, o.autoIDs); err != nil {
			return nil, err
		}
	} else {
		if err := checkMatches(matches); err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ChargeID)
		}
		if candidates, err = store.LedgerChargesByIDs(ctx, tx, scope, ids); err != nil {
			return nil, err
		}
		if len(candidates) != len(ids) {
			return nil, ErrNotFound
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if err := lockParents(ctx, tx, scope, candidates); err != nil {
		return nil, err
	}
	p, err := store.LockPayment(ctx, tx, scope, paymentID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	locked, err := store.LockChargesSorted(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*store.Charge, len(locked))
	for i := range locked {
		byID[locked[i].ID] = &locked[i]
	}
	if o.auto {
		open := make([]Open, 0, len(candidates))
		for _, c := range candidates { // candidates keep the due order
			if ch := byID[c.ID]; ch != nil && (ch.PaymentStatus == store.ChargeOpen || ch.PaymentStatus == store.ChargePartiallyPaid) {
				open = append(open, Open{ChargeID: ch.ID, Residual: money.Cents(ch.ResidualCents())})
			}
		}
		matches, _ = PlanOldestFirst(open, money.Cents(p.UnallocatedCents()))
		if len(matches) == 0 {
			return nil, nil
		}
	}
	return s.applyMatches(ctx, tx, scope, actor, p, byID, matches, o)
}

// checkMatches validates explicit matches: positive amounts, at most 500, each charge once.
func checkMatches(matches []MatchInput) error {
	if len(matches) == 0 || len(matches) > 500 {
		return invalid("matches", "count")
	}
	seen := map[string]bool{}
	for _, m := range matches {
		if m.Amount <= 0 {
			return invalid("matches.amount_cents", "positive")
		}
		if seen[m.ChargeID] {
			return invalid("matches", "duplicate")
		}
		seen[m.ChargeID] = true
	}
	return nil
}

// LockChargeParentsTx locks the contracts and rights that follow the given charges (lockParents) for a caller that
// locks the charges itself before matching (the provider webhook plans its matches under charge locks): taking the
// parents first keeps the lock order contract/right → payment → charges (INV-31, INV-52) and avoids a deadlock
// with a desk settlement of the same contract.
func (s *Service) LockChargeParentsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, chargeIDs []string) error {
	if len(chargeIDs) == 0 {
		return nil
	}
	charges, err := store.LedgerChargesByIDs(ctx, tx, scope, chargeIDs)
	if err != nil {
		return err
	}
	return lockParents(ctx, tx, scope, charges)
}

// lockParents locks the contracts and rights whose instalments follow the charges (contract → instalments,
// right → holder instalments come before the payment in the lock order), each set in ascending id order.
func lockParents(ctx context.Context, tx bun.IDB, scope store.MarinaScope, charges []store.Charge) error {
	var contracts, rightIDs []string
	for _, c := range charges {
		switch c.OriginType {
		case store.ChargeOriginInstallment:
			if c.ContractID != "" {
				contracts = append(contracts, c.ContractID)
			}
		case store.ChargeOriginHolderInstallment:
			if c.RightID != "" {
				rightIDs = append(rightIDs, c.RightID)
			}
		}
	}
	sort.Strings(contracts)
	for _, id := range slices.Compact(contracts) {
		if _, err := store.LockContract(ctx, tx, scope, id); err != nil {
			return err
		}
	}
	sort.Strings(rightIDs)
	for _, id := range slices.Compact(rightIDs) {
		if _, err := store.LockBerthRight(ctx, tx, scope, id); err != nil {
			return err
		}
	}
	return nil
}

// applyMatches writes the matches of a locked payment to locked charges.
func (s *Service) applyMatches(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, p *store.Payment,
	charges map[string]*store.Charge, matches []MatchInput, o matchOpts,
) ([]store.PaymentMatch, error) {
	if p.Kind == store.PaymentKindDeposit && !o.allowDeposit {
		return nil, ErrNotDeposit
	}
	var sum int64
	for _, m := range matches {
		ch := charges[m.ChargeID]
		if ch == nil {
			return nil, ErrNotFound
		}
		if ch.CustomerID != p.CustomerID {
			return nil, invalid("matches.charge_id", "other_customer")
		}
		if p.Kind == store.PaymentKindDeposit && ch.OriginType != store.ChargeOriginDepositRetention {
			return nil, ErrNotDeposit
		}
		if ch.PaymentStatus == store.ChargeCancelled || ch.TotalCents <= 0 {
			return nil, &LimitError{Limit: LimitCharge, ChargeID: ch.ID}
		}
		sum += int64(m.Amount)
	}
	now := s.now()
	ok, err := store.AddPaymentAllocated(ctx, tx, scope, p.ID, sum, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &LimitError{Limit: LimitPayment}
	}
	buckets, err := store.ChargeVATBuckets(ctx, tx, scope, chargeIDs(matches))
	if err != nil {
		return nil, err
	}
	split := decodeSplit(p.VATSplit)
	out := make([]store.PaymentMatch, 0, len(matches))
	var changed []string
	for _, m := range matches {
		ch := charges[m.ChargeID]
		from := ch.PaymentStatus
		paidBefore := money.Cents(ch.PaidAmountCents)
		status, paid, ok, err := store.AddChargePaid(ctx, tx, scope, ch.ID, int64(m.Amount), now)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &LimitError{Limit: LimitCharge, ChargeID: ch.ID}
		}
		ch.PaymentStatus, ch.PaidAmountCents = status, paid
		row := store.PaymentMatch{
			ID: store.NewID(), MarinaID: scope.ID(), PaymentID: p.ID, ChargeID: ch.ID, AmountCents: int64(m.Amount),
			CreatedByUserID: userOf(actor), CreatedByActor: actorString(actor), CreatedAt: now,
		}
		if err := store.InsertPaymentMatch(ctx, tx, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
		split = AddBuckets(split, ShareOf(bucketsOf(buckets[ch.ID]), paidBefore, m.Amount))
		if status != from {
			changed = append(changed, ch.ID)
			if err := s.followOrigin(ctx, tx, scope, actor, ch, p); err != nil {
				return nil, err
			}
			if status == store.ChargePaid {
				if err := s.emit(ctx, tx, scope, event.ChargePaid, event.ChargePaidData{
					ChargeID: ch.ID, TotalCents: ch.TotalCents, PaidAmountCents: paid,
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	p.AllocatedCents += sum
	p.Version++
	p.VATSplit = encodeSplit(split)
	if err := store.SetPaymentVATSplit(ctx, tx, scope, p.ID, p.VATSplit); err != nil {
		return nil, err
	}
	diff := make([]map[string]any, 0, len(out))
	for _, m := range out {
		diff = append(diff, map[string]any{"match_id": m.ID, "charge_id": m.ChargeID, "amount_cents": m.AmountCents})
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.match", "payment", p.ID, map[string]any{
		"matches": diff, "allocated_cents": p.AllocatedCents, "status_changed": changed,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentChanged, event.PaymentChangedData{
		PaymentID: p.ID, Change: event.ChangeMatched, ChargeIDs: chargeIDs(matches),
	}); err != nil {
		return nil, err
	}
	if o.recordContext && s.receipt != nil {
		for _, m := range matches {
			if ch := charges[m.ChargeID]; ch.FiscalMode == "receipt" {
				if _, err := s.receipt.RecordTx(ctx, tx, scope, actor, p, ch); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

func chargeIDs(matches []MatchInput) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.ChargeID)
	}
	return out
}

// followOrigin makes an instalment or holder instalment follow its charge's new payment status (m5-plan §8.2).
func (s *Service) followOrigin(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, p *store.Payment) error {
	ref := ""
	if p.ReferenceEnc != "" && s.d.Keys != nil && ch.PaymentStatus == store.ChargePaid {
		if v, err := s.d.Keys.DecryptString(p.ReferenceEnc, paymentRefAAD(p.ID)); err == nil {
			ref = v
		}
	}
	switch ch.OriginType {
	case store.ChargeOriginInstallment:
		if s.d.Contracts == nil {
			return nil
		}
		return s.d.Contracts.ApplyChargeSettlementTx(ctx, tx, scope, principalOf(actor), ch.OriginID, ch.PaymentStatus,
			contract.SettlementPayment{PaymentID: p.ID, Method: p.Method, ReceivedOn: p.ReceivedOn, Reference: ref})
	case store.ChargeOriginHolderInstallment:
		if s.d.Rights == nil {
			return nil
		}
		return s.d.Rights.ApplyChargeSettlementTx(ctx, tx, scope, principalOf(actor), ch.OriginID, ch.PaymentStatus,
			rights.SettlementPayment{PaymentID: p.ID, Method: p.Method, ReceivedOn: p.ReceivedOn, Reference: ref})
	}
	return nil
}

// ReverseMatchTx reverses a match in tx: the opposite row (amount = the whole match, or part of it when amount is
// set), both sides decremented with conditional updates, the origin follows, audit payment.unmatch. A match is
// reversed at most once (unique index).
func (s *Service) ReverseMatchTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, matchID string, amount money.Cents) (*store.PaymentMatch, error) {
	m, err := store.GetPaymentMatch(ctx, tx, scope, matchID)
	if err != nil {
		return nil, err
	}
	if m.ReversesMatchID != "" || m.AmountCents <= 0 {
		return nil, ErrAlreadyReversed
	}
	if amount == 0 {
		amount = money.Cents(m.AmountCents)
	}
	if amount < 0 || int64(amount) > m.AmountCents {
		return nil, invalid("amount_cents", "above_match")
	}
	pre, err := store.LedgerChargesByIDs(ctx, tx, scope, []string{m.ChargeID})
	if err != nil {
		return nil, err
	}
	if len(pre) != 1 {
		return nil, ErrNotFound
	}
	if err := lockParents(ctx, tx, scope, pre); err != nil {
		return nil, err
	}
	p, err := store.LockPayment(ctx, tx, scope, m.PaymentID)
	if err != nil {
		return nil, err
	}
	locked, err := store.LockChargesSorted(ctx, tx, scope, []string{m.ChargeID})
	if err != nil {
		return nil, err
	}
	ch := &locked[0]
	if _, err := store.ReversalOf(ctx, tx, scope, m.ID); err == nil {
		return nil, ErrAlreadyReversed
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	now := s.now()
	ok, err := store.AddPaymentAllocated(ctx, tx, scope, p.ID, -int64(amount), now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &LimitError{Limit: LimitPayment}
	}
	from, paidBefore := ch.PaymentStatus, money.Cents(ch.PaidAmountCents)
	status, paid, ok, err := store.AddChargePaid(ctx, tx, scope, ch.ID, -int64(amount), now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &LimitError{Limit: LimitCharge, ChargeID: ch.ID}
	}
	ch.PaymentStatus, ch.PaidAmountCents = status, paid
	rev := &store.PaymentMatch{
		ID: store.NewID(), MarinaID: scope.ID(), PaymentID: p.ID, ChargeID: ch.ID, AmountCents: -int64(amount),
		ReversesMatchID: m.ID, CreatedByUserID: userOf(actor), CreatedByActor: actorString(actor), CreatedAt: now,
	}
	if err := store.InsertPaymentMatch(ctx, tx, rev); err != nil {
		if store.IsUniqueViolation(err) {
			return nil, ErrAlreadyReversed
		}
		return nil, err
	}
	buckets, err := store.ChargeVATBuckets(ctx, tx, scope, []string{ch.ID})
	if err != nil {
		return nil, err
	}
	p.VATSplit = encodeSplit(AddBuckets(decodeSplit(p.VATSplit), ShareOf(bucketsOf(buckets[ch.ID]), paidBefore, -amount)))
	if err := store.SetPaymentVATSplit(ctx, tx, scope, p.ID, p.VATSplit); err != nil {
		return nil, err
	}
	if status != from {
		if err := s.followOrigin(ctx, tx, scope, actor, ch, p); err != nil {
			return nil, err
		}
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.unmatch", "payment", p.ID, map[string]any{
		"match_id": m.ID, "reversal_id": rev.ID, "charge_id": ch.ID, "amount_cents": int64(amount),
		"charge_status": status, "from_status": from,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentChanged, event.PaymentChangedData{
		PaymentID: p.ID, Change: event.ChangeReversed, ChargeIDs: []string{ch.ID},
	}); err != nil {
		return nil, err
	}
	return rev, nil
}

// Match matches unallocated money of a payment to charges (payments.collect).
func (s *Service) Match(ctx context.Context, p *auth.Principal, marinaID, paymentID string, matches []MatchInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return nil, err
	}
	if err := checkMatches(matches); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := s.matchTx(ctx, tx, scope, p.Actor(), paymentID, matches, matchOpts{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, paymentID)
}

// ReverseMatch reverses a match of a payment (refunds.issue).
func (s *Service) ReverseMatch(ctx context.Context, p *auth.Principal, marinaID, paymentID, matchID string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRefundsIssue)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetPaymentMatch(ctx, tx, scope, matchID)
		if err != nil {
			return err
		}
		if m.PaymentID != paymentID {
			return ErrNotFound
		}
		_, err = s.ReverseMatchTx(ctx, tx, scope, p.Actor(), matchID, 0)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, paymentID)
}
