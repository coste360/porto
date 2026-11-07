package contract

import (
	"context"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Settlement of instalments (m4-plan §3.3, §15 D1, INV-32): SettleInstallment and SettleInstallments are the
// only code paths that make an instalment paid. In M4 the source is the manual driver (an operator records a bank
// transfer, cash, ...); M5 payment matching calls settleTx with source payment.

// Settlement methods.
var settlementMethods = []string{"bank_transfer", "cash", "card_pos", "cheque", "other"}

// SettleInput is a manual settlement.
type SettleInput struct {
	Method    string
	Reference string // stored encrypted, never logged or emitted
	SettledOn *calendar.Date
}

// SettleItem is one instalment of a bulk settlement with the version the client saw (0 = any).
type SettleItem struct {
	ID      string
	Version int
}

func settlementAAD(id string) string { return "installment-ref:" + id }

func (s *Service) checkSettle(ctx context.Context, scope store.MarinaScope, in *SettleInput) error {
	v := &auth.ValidationError{}
	if !contains(settlementMethods, in.Method) {
		v.Add("method", "invalid")
	}
	ref, ok := cleanText(in.Reference, 200)
	if !ok {
		v.Add("reference", "too_long")
	}
	in.Reference = strings.TrimSpace(ref)
	today := s.today(ctx, s.d.DB.DB, scope)
	if in.SettledOn == nil {
		in.SettledOn = &today
	} else if in.SettledOn.After(today) {
		v.Add("settled_on", "in_future")
	}
	return v.Err()
}

// SettleInstallment records the manual settlement of one issued or scheduled instalment (payments.collect,
// If-Match on the instalment version).
func (s *Service) SettleInstallment(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in SettleInput) (*InstallmentRow, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return nil, err
	}
	if err := s.checkSettle(ctx, scope, &in); err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetInstallment(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Status != store.InstallmentIssued && r.Status != store.InstallmentScheduled {
			return &TransitionError{From: r.Status, Action: "settle"}
		}
		if ifMatch != 0 && r.Version != ifMatch {
			return ErrVersionMismatch
		}
		ok, err := s.settleTx(ctx, tx, scope, a, r, ifMatch, store.SettlementManual, in)
		if err != nil {
			return err
		}
		if !ok {
			return ErrVersionMismatch
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r, err := store.GetInstallment(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.installmentRows(ctx, s.d.DB.DB, scope, p, []store.Installment{*r}, s.today(ctx, s.d.DB.DB, scope))
	if err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// SettleInstallments settles up to 500 instalments in one transaction, all or nothing (payments.collect): the
// first instalment not settleable (status or version) fails the whole call with a *SettleError.
func (s *Service) SettleInstallments(ctx context.Context, p *auth.Principal, marinaID string, items []SettleItem, in SettleInput) (int, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if err := checkBulkIDs(ids); err != nil {
		return 0, err
	}
	if len(uniq(ids)) != len(ids) {
		v := &auth.ValidationError{}
		v.Add("items", "duplicate")
		return 0, v
	}
	if err := s.checkSettle(ctx, scope, &in); err != nil {
		return 0, err
	}
	a := actor{p}
	n := 0
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n = 0
		rows, err := store.InstallmentsByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		byID := map[string]*store.Installment{}
		cids := make([]string, 0, len(rows))
		for i := range rows {
			byID[rows[i].ID] = &rows[i]
			cids = append(cids, rows[i].ContractID)
		}
		// Contracts are locked up front in id order so two bulk settlements cannot deadlock.
		if err := lockContractsSorted(ctx, tx, scope, cids); err != nil {
			return err
		}
		for _, it := range items {
			r := byID[it.ID]
			if r == nil {
				return &SettleError{ID: it.ID}
			}
			ok, err := s.settleTx(ctx, tx, scope, a, r, it.Version, store.SettlementManual, in)
			if err != nil {
				return err
			}
			if !ok {
				return &SettleError{ID: it.ID}
			}
			n++
		}
		return nil
	})
	return n, err
}

// settleTx marks one instalment paid (conditional on its status and expected version), audits it, emits
// installment.changed and completes the renewal proposal whose deposit it is. False when the row was not
// settleable.
func (s *Service) settleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, r *store.Installment, expected int, source string, in SettleInput) (bool, error) {
	now := s.now()
	st := store.Settlement{Source: source, Method: in.Method, SettledOn: *in.SettledOn, At: now, UserID: a.userID()}
	if in.Reference != "" {
		if s.d.Keys == nil {
			return false, ErrSettleConflict
		}
		enc, err := s.d.Keys.EncryptString(in.Reference, settlementAAD(r.ID))
		if err != nil {
			return false, err
		}
		st.RefEnc = enc
	}
	// Lock order contract, then its instalments (INV-31): a replan holding the contract cannot see this row
	// change between its read of the plan and its cancellation of the scheduled rows.
	if _, err := store.LockContract(ctx, tx, scope, r.ContractID); err != nil {
		return false, err
	}
	ok, err := store.SettleInstallmentRow(ctx, tx, scope, r.ID, expected, st)
	if err != nil || !ok {
		return false, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "installment.settle", entityInstallment, r.ID, map[string]any{
		"contract_id": r.ContractID, "seq": r.Seq, "amount_cents": r.AmountCents, "from_status": r.Status,
		"status": store.InstallmentPaid, "source": source, "method": in.Method, "settled_on": *in.SettledOn,
	}); err != nil {
		return false, err
	}
	if err := s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		InstallmentID: r.ID, ContractID: r.ContractID, Change: event.ChangeSettled, PlanVersion: r.PlanVersion,
	}); err != nil {
		return false, err
	}
	if r.Kind == "deposit" || r.Seq == 1 {
		if err := s.completeProposalOnDepositTx(ctx, tx, scope, a, r.ContractID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// lockContractsSorted locks the distinct contracts of ids in ascending id order.
func lockContractsSorted(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ids []string) error {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if _, err := store.LockContract(ctx, tx, scope, id); err != nil {
			return err
		}
	}
	return nil
}
