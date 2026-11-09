package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedPayments seeds the ledger of the demo marina (m5-plan §10): a payment history over the open charges (60 %
// bank transfers, 25 % POS cards, 15 % cheques — cash lands in the sessions seeded by seedCash), partial payments,
// three customers with credit (an overpayment, a goodwill credit, a mandate settlement), one refund and one
// deposit with a partial retention. Everything goes through the payment service as the demo owner; it is a no-op
// when the marina already has a goodwill credit or a deposit (the M4 seed already records instalment payments).
func (a *App) seedPayments(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("payments").Where("marina_id = ?", scope.ID()).
		Where("(source = 'goodwill' OR kind = 'deposit')").Count(ctx); err != nil || n > 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := occupancy.Today(a.Clock, loc)
	m := res.MarinaID
	rng := rand.New(rand.NewPCG(51, 5)) //nolint:gosec // demo data: deterministic by design

	open, err := store.OpenPositiveCharges(ctx, a.DB.DB, scope, store.OpenChargesFilter{})
	if err != nil {
		return err
	}
	method := func() string {
		switch x := rng.IntN(100); {
		case x < 60:
			return store.PaymentMethodBankTransfer
		case x < 85:
			return store.PaymentMethodCardPOS
		default:
			return store.PaymentMethodCheque
		}
	}
	var overpaid, goodwill string
	for i, ch := range open {
		if ch.CustomerID == "" || ch.DueOn.After(today) {
			continue // not due yet: the scadenzario shows what falls due next
		}
		x := rng.IntN(100)
		if x >= 75 {
			continue // left open: the scadenzario and the reminders have something to show
		}
		amount := money.Cents(ch.ResidualCents())
		if x >= 60 {
			amount = amount.MulRat(1, 2) // partial
		}
		if amount <= 0 {
			continue
		}
		on := ch.DueOn // paid on its due date
		if on.Before(ch.IssuedOn) {
			on = ch.IssuedOn
		}
		in := payment.RecordInput{
			CustomerID: ch.CustomerID, Kind: store.PaymentKindPayment, Method: method(), Amount: amount, ReceivedOn: on,
			Matches: []payment.MatchInput{{ChargeID: ch.ID, Amount: amount}},
		}
		if overpaid == "" && i%7 == 3 {
			// One customer pays 50,00 more than due: the rest stays as credit.
			in.Amount += 50_00
			overpaid = ch.CustomerID
		}
		if _, err := a.Payments.Record(ctx, p, m, in); err != nil {
			return fmt.Errorf("payment of %s: %w", ch.Number, err)
		}
		res.Payments++
	}

	cs, err := store.ListCustomers(ctx, a.DB.DB, scope, store.CustomerFilter{}, "", 20)
	if err != nil {
		return err
	}
	if overpaid == "" && len(cs.Items) > 0 {
		overpaid = cs.Items[0].ID
		if _, err := a.Payments.Record(ctx, p, m, payment.RecordInput{
			CustomerID: overpaid, Kind: store.PaymentKindPayment, Method: store.PaymentMethodBankTransfer, Amount: 50_00,
		}); err != nil {
			return fmt.Errorf("overpayment: %w", err)
		}
		res.Payments++
	}
	if overpaid != "" {
		res.CustomersCredit++
		// One refund: part of the overpayment back by bank transfer.
		pays, err := store.UnallocatedPayments(ctx, a.DB.DB, scope, overpaid, []string{store.PaymentKindPayment})
		if err != nil {
			return err
		}
		if len(pays) > 0 {
			if _, err := a.Payments.Refund(ctx, p, m, pays[0].ID, payment.RefundInput{
				Amount: min(20_00, money.Cents(pays[0].UnallocatedCents())), Method: store.PaymentMethodBankTransfer, Reason: "Rimborso eccedenza",
			}); err != nil {
				return fmt.Errorf("refund: %w", err)
			}
			res.Refunds++
		}
	}
	for _, c := range cs.Items {
		if c.ID != overpaid {
			goodwill = c.ID
			break
		}
	}
	if goodwill != "" {
		if _, err := a.Payments.GrantCredit(ctx, p, m, goodwill, payment.CreditInput{
			Amount: 30_00, Reason: "Disservizio colonnina elettrica", Source: store.PaymentSourceGoodwill,
		}, ""); err != nil {
			return fmt.Errorf("goodwill: %w", err)
		}
		res.Payments++
		res.CustomersCredit++
	}

	// A mandate settled as credit of its holders (when a mandate has revenues).
	var mandates []store.ManagementMandate
	if err := a.DB.NewSelect().Model(&mandates).Where("marina_id = ?", scope.ID()).OrderExpr("from_on, id").Scan(ctx); err != nil {
		return err
	}
	for _, md := range mandates {
		r, err := a.Payments.SettleMandate(ctx, p, m, md.ID, seedMinDate(today, md.ToOn), payment.SettleCredit)
		if errors.Is(err, payment.ErrNothingToSettle) {
			continue
		}
		if err != nil {
			return fmt.Errorf("mandate settlement: %w", err)
		}
		if r.Credited > 0 {
			res.CustomersCredit++
			break
		}
	}

	// A deposit for the keys of the gate, partly retained.
	if len(cs.Items) > 2 {
		dep, err := a.Payments.Record(ctx, p, m, payment.RecordInput{
			CustomerID: cs.Items[2].ID, Kind: store.PaymentKindDeposit, Method: store.PaymentMethodCardPOS, Amount: 100_00,
			Notes: "Cauzione chiavi e badge",
		})
		if err != nil {
			return fmt.Errorf("deposit: %w", err)
		}
		res.Payments++
		if _, err := a.Payments.Retain(ctx, p, m, dep.ID, payment.RetainInput{Amount: 30_00, Reason: "Badge non restituito"}); err != nil &&
			!errors.Is(err, payment.ErrRetentionItemMissing) {
			return fmt.Errorf("retention: %w", err)
		}
	}
	return nil
}

func seedMinDate(a, b calendar.Date) calendar.Date {
	if b.Before(a) {
		return b
	}
	return a
}
