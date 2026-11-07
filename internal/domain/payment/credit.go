package payment

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Customer credit (m5-plan §4.4, §16 D5): the credit of a customer is the unallocated money of their payment and
// credit rows; non-cash credits (adjustment remainders, goodwill, release, mandate, opening balances) are payments
// of kind credit, never in cash sessions or corrispettivi.

// GrantCredit grants a goodwill or release credit to a customer (refunds.issue).
func (s *Service) GrantCredit(ctx context.Context, p *auth.Principal, marinaID, customerID string, in CreditInput, idempotencyKey string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRefundsIssue)
	if err != nil {
		return nil, err
	}
	// Another marina's customer is 404 before any validation (existence must not leak).
	if cs, err := store.CustomersByIDs(ctx, s.d.DB.DB, scope, []string{customerID}); err != nil {
		return nil, err
	} else if len(cs) == 0 {
		return nil, ErrNotFound
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if idempotencyKey != "" {
			if prev, err := store.PaymentByIdempotencyKey(ctx, tx, scope, idempotencyKey); err == nil {
				id = prev.ID
				return nil
			}
		}
		pay, err := s.grantCreditTx(ctx, tx, scope, p.Actor(), customerID, in, idempotencyKey)
		if err != nil {
			return err
		}
		id = pay.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// GrantCreditTx records a credit payment (kind credit, method credit_note) of a customer.
func (s *Service) GrantCreditTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID string, in CreditInput) (*store.Payment, error) {
	return s.grantCreditTx(ctx, tx, scope, actor, customerID, in, "")
}

func (s *Service) grantCreditTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID string, in CreditInput, key string) (*store.Payment, error) {
	v := &auth.ValidationError{}
	if in.Amount <= 0 {
		v.Add("amount_cents", "positive")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := utf8.RuneCountInString(in.Reason); n == 0 {
		v.Add("reason", "required")
	} else if n > 500 {
		v.Add("reason", "too_long")
	}
	switch in.Source {
	case store.PaymentSourceGoodwill:
	case store.PaymentSourceRelease:
		if !store.ValidID(in.ReleaseID) {
			v.Add("release_id", "required")
		}
	default:
		v.Add("source", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if in.Source == store.PaymentSourceRelease {
		ok, err := store.AddReleaseCredit(ctx, tx, scope, in.ReleaseID, customerID, int64(in.Amount), s.now())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrReleaseNotResold
		}
	}
	pay, err := s.RecordTx(ctx, tx, scope, actor, RecordInput{
		CustomerID: customerID, Kind: store.PaymentKindCredit, Source: in.Source, Amount: in.Amount, Notes: in.Reason,
		ReleaseID: in.ReleaseID, IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.credit_grant", "payment", pay.ID, map[string]any{
		"customer_id": customerID, "source": in.Source, "amount_cents": int64(in.Amount), "release_id": in.ReleaseID,
	}); err != nil {
		return nil, err
	}
	return pay, nil
}

// GrantAdjustmentCreditTx implements charge.CreditGranter: the remainder of a negative charge becomes customer
// credit (source adjustment; opening_balance for a negative opening balance).
func (s *Service) GrantAdjustmentCreditTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, amount money.Cents) error {
	if amount <= 0 {
		return nil
	}
	if ch.CustomerID == "" {
		return invalid("customer_id", "required")
	}
	source := store.PaymentSourceAdjustment
	if ch.OriginType == store.ChargeOriginOpeningBalance {
		source = store.PaymentSourceOpeningBalance
	}
	_, err := s.RecordTx(ctx, tx, scope, actor, RecordInput{
		CustomerID: ch.CustomerID, Kind: store.PaymentKindCredit, Source: source, Amount: amount,
		AdjustmentChargeID: ch.ID, ReceivedOn: ch.IssuedOn,
	})
	return err
}

// ApplyCredit applies the credit of a customer to their open charges (all, or chargeIDs), oldest payment first and
// oldest due charge first (payments.collect). It returns the matched amount.
func (s *Service) ApplyCredit(ctx context.Context, p *auth.Principal, marinaID, customerID string, chargeIDs []string) (money.Cents, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return 0, err
	}
	if len(chargeIDs) > 500 {
		return 0, invalid("charge_ids", "count")
	}
	var total money.Cents
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		total, err = s.ApplyCreditTx(ctx, tx, scope, p.Actor(), customerID, chargeIDs)
		return err
	})
	return total, err
}

// ApplyCreditTx applies credit in tx (see ApplyCredit); ErrCreditInsufficient when the customer has no credit.
func (s *Service) ApplyCreditTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID string, chargeIDs []string) (money.Cents, error) {
	cs, err := store.CustomersByIDs(ctx, tx, scope, []string{customerID})
	if err != nil {
		return 0, err
	}
	if len(cs) == 0 {
		return 0, ErrNotFound
	}
	if chargeIDs != nil && len(chargeIDs) == 0 {
		chargeIDs = nil
	}
	pays, err := store.UnallocatedPayments(ctx, tx, scope, customerID, []string{store.PaymentKindPayment, store.PaymentKindCredit})
	if err != nil {
		return 0, err
	}
	var total money.Cents
	var touched []string
	for _, pay := range pays {
		ms, err := s.matchTx(ctx, tx, scope, actor, pay.ID, nil, matchOpts{auto: true, autoIDs: chargeIDs})
		if err != nil {
			return 0, err
		}
		for _, m := range ms {
			total += money.Cents(m.AmountCents)
		}
		if len(ms) > 0 {
			touched = append(touched, pay.ID)
		}
	}
	if total == 0 {
		return 0, ErrCreditInsufficient
	}
	for _, id := range touched {
		if err := s.emit(ctx, tx, scope, event.PaymentChanged, event.PaymentChangedData{
			PaymentID: id, Change: event.ChangeCreditApplied, ChargeIDs: chargeIDs,
		}); err != nil {
			return 0, err
		}
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.credit_apply", "customer", customerID, map[string]any{
		"matched_cents": int64(total), "payment_ids": touched, "charge_ids": chargeIDs,
	}); err != nil {
		return 0, err
	}
	return total, nil
}

// CreditRow is the credit of one customer.
type CreditRow struct {
	CustomerID          string
	CustomerName        string
	Credit, Deposits    money.Cents
	UnallocatedPayments []store.Payment
}

// ListCredits returns the customers with credit or unallocated deposits (charges.manage).
func (s *Service) ListCredits(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[CreditRow], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[CreditRow]{}, err
	}
	page, err := store.CustomersWithCredit(ctx, s.d.DB.DB, scope, cursor, limit)
	if err != nil {
		return store.Page[CreditRow]{}, err
	}
	ids := make([]string, 0, len(page.Items))
	for _, r := range page.Items {
		ids = append(ids, r.CustomerID)
	}
	names, err := s.customerNames(ctx, s.d.DB.DB, scope, p, ids)
	if err != nil {
		return store.Page[CreditRow]{}, err
	}
	out := store.Page[CreditRow]{NextCursor: page.NextCursor, Items: make([]CreditRow, 0, len(page.Items))}
	for _, r := range page.Items {
		out.Items = append(out.Items, CreditRow{CustomerID: r.CustomerID, CustomerName: names[r.CustomerID], Credit: money.Cents(r.CreditCents), Deposits: money.Cents(r.DepositsCents)})
	}
	return out, nil
}

// CustomerCredit returns the credit, deposits and unallocated payments of a customer (charges.manage).
func (s *Service) CustomerCredit(ctx context.Context, p *auth.Principal, marinaID, customerID string) (*CreditRow, []Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, nil, err
	}
	db := s.d.DB.DB
	names, err := s.customerNames(ctx, db, scope, p, []string{customerID})
	if err != nil {
		return nil, nil, err
	}
	if _, ok := names[customerID]; !ok {
		return nil, nil, ErrNotFound
	}
	t, err := store.CreditOf(ctx, db, scope, customerID)
	if err != nil {
		return nil, nil, err
	}
	pays, err := store.UnallocatedPayments(ctx, db, scope, customerID, []string{store.PaymentKindPayment, store.PaymentKindCredit, store.PaymentKindDeposit})
	if err != nil {
		return nil, nil, err
	}
	details, err := s.details(ctx, db, scope, p, pays, false)
	if err != nil {
		return nil, nil, err
	}
	return &CreditRow{CustomerID: customerID, CustomerName: names[customerID], Credit: money.Cents(t.CreditCents), Deposits: money.Cents(t.DepositsCents)}, details, nil
}
