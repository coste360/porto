package payment

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Refunds and payouts (m5-plan §4.3, §16 D6): money is refunded only from what is unallocated, refunds in flight
// included; refunding a paid charge is "reverse match + refund" in one transaction. Provider refunds are written
// pending and sent after commit by the registered ProviderRefunder (internal/domain/collect); its outcome comes
// back through CompleteProviderRefundTx / FailProviderRefundTx.

// ErrProviderRefundUnsupported: the payment's provider cannot refund, or the payment is not a provider payment
// (422 provider_refund_unsupported).
var ErrProviderRefundUnsupported = errors.New("payment: provider refund unsupported")

// ProviderRefunder sends provider refunds (implemented by internal/domain/collect).
type ProviderRefunder interface {
	// RefundSupported reports whether the provider of a payment can refund it.
	RefundSupported(ctx context.Context, scope store.MarinaScope, providerCode string) bool
	// SendRefund calls the provider for a pending refund; it runs after the commit of the refund, never inside a
	// transaction, and applies the outcome in a new one.
	SendRefund(ctx context.Context, scope store.MarinaScope, refundID string)
}

// SetProviderRefunder registers the provider refund sender (collect).
func (s *Service) SetProviderRefunder(r ProviderRefunder) { s.refunder = r }

var refundMethods = []string{store.PaymentMethodCash, store.PaymentMethodCardPOS, store.PaymentMethodBankTransfer, store.PaymentMethodCheque, store.PaymentMethodOther}

// Refund refunds unallocated money of a payment, optionally reversing matches first (refunds.issue).
func (s *Service) Refund(ctx context.Context, p *auth.Principal, marinaID, paymentID string, in RefundInput) (*store.Refund, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRefundsIssue)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetPayment(ctx, s.d.DB.DB, scope, paymentID); err != nil {
		return nil, err // another marina's payment is 404 before any validation
	}
	var out *store.Refund
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := s.RefundTx(ctx, tx, scope, p.Actor(), paymentID, in)
		if err != nil {
			return err
		}
		out = r
		if r.Status == store.RefundPending && r.Source == store.PaymentSourceProvider && s.refunder != nil {
			id, refunder := r.ID, s.refunder
			store.AfterCommit(ctx, func() { refunder.SendRefund(context.WithoutCancel(ctx), scope, id) })
		}
		return nil
	})
	if err != nil && in.IdempotencyKey != "" && store.IsUniqueViolation(err) {
		if r, rerr := store.RefundByIdempotencyKey(ctx, s.d.DB.DB, scope, in.IdempotencyKey); rerr == nil && r.PaymentID == paymentID {
			return r, nil
		}
	}
	return out, err
}

func checkRefund(in *RefundInput) error {
	v := &auth.ValidationError{}
	if in.Amount <= 0 {
		v.Add("amount_cents", "positive")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	switch n := utf8.RuneCountInString(in.Reason); {
	case n == 0:
		v.Add("reason", "required")
	case n > 500:
		v.Add("reason", "too_long")
	}
	if in.Method != "" && !slices.Contains(refundMethods, in.Method) {
		v.Add("method", "invalid")
	}
	if len(in.Unmatch) > 100 {
		v.Add("unmatch", "count")
	}
	for _, u := range in.Unmatch {
		if u.Amount < 0 {
			v.Add("unmatch.amount_cents", "positive")
		}
	}
	return v.Err()
}

// RefundTx refunds unallocated money of a payment in tx (optional reversals first); provider refunds are pending
// and sent after commit by collect.
func (s *Service) RefundTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, paymentID string, in RefundInput) (*store.Refund, error) {
	if err := checkRefund(&in); err != nil {
		return nil, err
	}
	if in.IdempotencyKey != "" {
		if r, err := store.RefundByIdempotencyKey(ctx, tx, scope, in.IdempotencyKey); err == nil {
			if r.PaymentID != paymentID || r.AmountCents != int64(in.Amount) {
				return nil, ErrIdempotencyMismatch
			}
			return r, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	pre, err := store.GetPayment(ctx, tx, scope, paymentID)
	if err != nil {
		return nil, err
	}
	source, method := store.PaymentSourceManual, in.Method
	if in.ViaProvider {
		if pre.Source != store.PaymentSourceProvider || s.refunder == nil || !s.refunder.RefundSupported(ctx, scope, pre.ProviderCode) {
			return nil, ErrProviderRefundUnsupported
		}
		source, method = store.PaymentSourceProvider, pre.Method
	} else if method == "" {
		method = pre.Method
		if !slices.Contains(refundMethods, method) {
			method = store.PaymentMethodBankTransfer
		}
	}
	for _, u := range in.Unmatch {
		m, err := store.GetPaymentMatch(ctx, tx, scope, u.MatchID)
		if err != nil {
			return nil, err
		}
		if m.PaymentID != paymentID {
			return nil, ErrNotFound
		}
		if _, err := s.ReverseMatchTx(ctx, tx, scope, actor, u.MatchID, u.Amount); err != nil {
			return nil, err
		}
	}
	var sessionID string
	if source == store.PaymentSourceManual && method == store.PaymentMethodCash {
		if s.cash == nil {
			return nil, ErrCashUnavailable
		}
		if sessionID, err = s.cash.SessionForPaymentTx(ctx, tx, scope, in.CashRegisterID, true); err != nil {
			return nil, err
		}
	}
	p, err := store.LockPayment(ctx, tx, scope, paymentID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	ok, err := store.AddPaymentRefunded(ctx, tx, scope, p.ID, int64(in.Amount), now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRefundExceeds
	}
	today := s.today(ctx, tx, scope)
	n, err := store.NextLedgerNumber(ctx, tx, scope, seriesRefund, today.Year())
	if err != nil {
		return nil, err
	}
	r := &store.Refund{
		ID: store.NewID(), MarinaID: scope.ID(), Number: ledgerNumber("R", today.Year(), n), PaymentID: p.ID,
		Kind: store.RefundKindRefund, Method: method, Source: source, Status: store.RefundSucceeded,
		AmountCents: int64(in.Amount), Reason: in.Reason, BusinessDate: today, CashSessionID: sessionID,
		CustomerID: p.CustomerID, IdempotencyKey: in.IdempotencyKey, RecordedByUserID: userOf(actor),
		RecordedByActor: actorString(actor), Version: 1, CreatedAt: now, UpdatedAt: now, CompletedAt: now,
	}
	if source == store.PaymentSourceProvider {
		r.Status, r.ProviderCode, r.CompletedAt = store.RefundPending, p.ProviderCode, time.Time{}
	}
	if err := store.InsertRefund(ctx, tx, r); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "refund.create", "refund", r.ID, map[string]any{
		"number": r.Number, "payment_id": p.ID, "kind": r.Kind, "method": r.Method, "source": r.Source, "status": r.Status,
		"amount_cents": r.AmountCents, "business_date": r.BusinessDate, "cash_session_id": r.CashSessionID,
		"unmatched": len(in.Unmatch),
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentRefunded, event.PaymentRefundedData{
		RefundID: r.ID, PaymentID: p.ID, Kind: r.Kind, AmountCents: r.AmountCents, Status: r.Status,
	}); err != nil {
		return nil, err
	}
	return r, nil
}

// CompleteProviderRefundTx marks a pending provider refund succeeded (webhook or reconciliation). Idempotent: a
// refund already finished is left as it is.
func (s *Service) CompleteProviderRefundTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, refundID, providerRef string) (*store.Refund, error) {
	return s.finishRefundTx(ctx, tx, scope, actor, refundID, store.RefundSucceeded, providerRef, "")
}

// FailProviderRefundTx marks a pending provider refund failed and releases its amount on the payment.
func (s *Service) FailProviderRefundTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, refundID, failureCode string) (*store.Refund, error) {
	return s.finishRefundTx(ctx, tx, scope, actor, refundID, store.RefundFailed, "", failureCode)
}

func (s *Service) finishRefundTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, refundID, status, providerRef, failure string) (*store.Refund, error) {
	pre, err := store.GetRefund(ctx, tx, scope, refundID)
	if err != nil {
		return nil, err
	}
	if pre.PaymentID != "" {
		if _, err := store.LockPayment(ctx, tx, scope, pre.PaymentID); err != nil {
			return nil, err
		}
	}
	r, err := store.LockRefund(ctx, tx, scope, refundID)
	if err != nil {
		return nil, err
	}
	if r.Status != store.RefundPending {
		return r, nil
	}
	now := s.now()
	ok, err := store.FinishRefund(ctx, tx, scope, r.ID, status, providerRef, failure, now)
	if err != nil || !ok {
		if err == nil {
			return store.GetRefund(ctx, tx, scope, refundID)
		}
		return nil, err
	}
	if status == store.RefundFailed && r.PaymentID != "" {
		ok, err := store.AddPaymentRefunded(ctx, tx, scope, r.PaymentID, -r.AmountCents, now)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrRefundExceeds
		}
	}
	action := "refund.complete"
	if status == store.RefundFailed {
		action = "refund.fail"
	}
	if err := s.audit(ctx, tx, scope, actor, action, "refund", r.ID, map[string]any{
		"payment_id": r.PaymentID, "status": status, "amount_cents": r.AmountCents, "failure_code": failure,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentRefunded, event.PaymentRefundedData{
		RefundID: r.ID, PaymentID: r.PaymentID, Kind: r.Kind, AmountCents: r.AmountCents, Status: status,
	}); err != nil {
		return nil, err
	}
	return store.GetRefund(ctx, tx, scope, refundID)
}
