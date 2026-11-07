package payment

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Mandate compensation (spec 14.3; m5-plan §4.4, §16 D21): the owner shares of a management mandate's revenues
// up to a date, plus the guaranteed-minimum top-up once the mandate period is closed, become credit of the right's
// holders (split by their millis with the largest remainder) or a recorded payout. What a mandate already paid is
// subtracted, so a settlement never pays twice; the revenue rows are marked settled with a conditional update.

// Mandate settlement modes.
const (
	SettleCredit = "credit"
	SettlePayout = "payout"
)

// SettleMandateResult is what a settlement paid.
type SettleMandateResult struct {
	Credited, PaidOut money.Cents
}

// SettleMandate settles the owner compensation of a mandate through a date (refunds.issue).
func (s *Service) SettleMandate(ctx context.Context, p *auth.Principal, marinaID, mandateID string, through calendar.Date, mode string) (SettleMandateResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRefundsIssue)
	if err != nil {
		return SettleMandateResult{}, err
	}
	if _, err := store.GetMandate(ctx, s.d.DB.DB, scope, mandateID); err != nil {
		return SettleMandateResult{}, err // another marina's mandate is 404 before any validation
	}
	v := &auth.ValidationError{}
	if through.IsZero() {
		v.Add("through", "required")
	}
	if mode != SettleCredit && mode != SettlePayout {
		v.Add("mode", "invalid")
	}
	if err := v.Err(); err != nil {
		return SettleMandateResult{}, err
	}
	actor := p.Actor()
	var res SettleMandateResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res = SettleMandateResult{}
		m, err := store.LockMandate(ctx, tx, scope, mandateID)
		if err != nil {
			return err
		}
		all, err := store.MandateRevenues(ctx, tx, scope, m.ID, through, false)
		if err != nil {
			return err
		}
		var target int64
		var unsettled []string
		for _, r := range all {
			target += r.OwnerShareCents
			if r.SettledAt.IsZero() {
				unsettled = append(unsettled, r.ID)
			}
		}
		closed := !through.Before(m.ToOn) || (!m.EndedOn.IsZero() && !through.Before(m.EndedOn))
		if closed && target < m.MinGuaranteedCents {
			target = m.MinGuaranteedCents
		}
		settled, err := store.MandateSettledCents(ctx, tx, scope, m.ID)
		if err != nil {
			return err
		}
		amount := money.Cents(target - settled)
		if amount <= 0 && len(unsettled) == 0 {
			return ErrNothingToSettle
		}
		now := s.now()
		if amount > 0 {
			holders, err := store.HoldersOfRights(ctx, tx, scope, []string{m.RightID})
			if err != nil {
				return err
			}
			if len(holders) == 0 {
				return ErrNothingToSettle
			}
			weights := make([]int64, len(holders))
			for i, h := range holders {
				weights[i] = h.ShareMillis
			}
			shares, err := money.Allocate(amount, weights)
			if err != nil {
				return err
			}
			today := s.today(ctx, tx, scope)
			for i, h := range holders {
				if shares[i] <= 0 {
					continue
				}
				if mode == SettleCredit {
					if _, err := s.RecordTx(ctx, tx, scope, actor, RecordInput{
						CustomerID: h.CustomerID, Kind: store.PaymentKindCredit, Source: store.PaymentSourceMandate,
						Amount: shares[i], MandateID: m.ID, ReceivedOn: today,
					}); err != nil {
						return err
					}
					res.Credited += shares[i]
					continue
				}
				n, err := store.NextLedgerNumber(ctx, tx, scope, seriesRefund, today.Year())
				if err != nil {
					return err
				}
				r := &store.Refund{
					ID: store.NewID(), MarinaID: scope.ID(), Number: ledgerNumber("R", today.Year(), n), Kind: store.RefundKindPayout,
					Method: store.PaymentMethodBankTransfer, Source: store.PaymentSourceManual, Status: store.RefundSucceeded,
					AmountCents: int64(shares[i]), Reason: "mandate", BusinessDate: today, MandateID: m.ID, CustomerID: h.CustomerID,
					RecordedByUserID: userOf(actor), RecordedByActor: actorString(actor), Version: 1, CreatedAt: now, UpdatedAt: now,
					CompletedAt: now,
				}
				if err := store.InsertRefund(ctx, tx, r); err != nil {
					return err
				}
				if err := s.emit(ctx, tx, scope, event.PaymentRefunded, event.PaymentRefundedData{
					RefundID: r.ID, Kind: r.Kind, AmountCents: r.AmountCents, Status: r.Status,
				}); err != nil {
					return err
				}
				res.PaidOut += shares[i]
			}
		}
		n, err := store.MarkMandateRevenuesSettled(ctx, tx, scope, unsettled, now)
		if err != nil {
			return err
		}
		if n != len(unsettled) {
			return store.ErrConflict // settled concurrently: the caller retries on fresh data
		}
		if err := s.audit(ctx, tx, scope, actor, "mandate.settle", "management_mandate", m.ID, map[string]any{
			"through": through, "mode": mode, "credited_cents": int64(res.Credited), "paid_out_cents": int64(res.PaidOut),
			"revenues": len(unsettled), "closed": closed,
		}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "mandate", ID: m.ID, Change: "settled"})
	})
	return res, err
}
