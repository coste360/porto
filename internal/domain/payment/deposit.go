package payment

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Deposits (cauzioni, spec 16.2; m5-plan §4.4): a deposit is never revenue and never matched to ordinary charges.
// Retaining (part of) it creates a deposit_retention charge (one per deposit, INV-54) matched to the deposit in
// the same transaction; releasing it is a refund.

// ErrRetentionItemMissing: no item given and no origin item configured for deposit retention (422
// billing_item_missing).
var ErrRetentionItemMissing = charge.ErrBillingItemMissing

// RetainInput is a deposit retention.
type RetainInput struct {
	Amount money.Cents
	ItemID string // default: the origin item deposit_retention of the billing settings
	Reason string
}

// Retain retains (part of) a deposit (charges.manage) and returns the retention charge.
func (s *Service) Retain(ctx context.Context, p *auth.Principal, marinaID, paymentID string, in RetainInput) (*store.Charge, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetPayment(ctx, s.d.DB.DB, scope, paymentID); err != nil {
		return nil, err // another marina's payment is 404 before any validation
	}
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
	if in.ItemID != "" && !store.ValidID(in.ItemID) {
		v.Add("item_id", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if s.d.Charges == nil {
		return nil, errors.New("payment: charge service not wired")
	}
	actor := p.Actor()
	var out *store.Charge
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		dep, err := store.LockPayment(ctx, tx, scope, paymentID)
		if err != nil {
			return err
		}
		if dep.Kind != store.PaymentKindDeposit {
			return ErrNotDeposit
		}
		if int64(in.Amount) > dep.UnallocatedCents() {
			return ErrRefundExceeds
		}
		itemID := in.ItemID
		if itemID == "" {
			if itemID, err = store.LedgerOriginItemID(ctx, tx, scope, "deposit_retention"); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return ErrRetentionItemMissing
				}
				return err
			}
		}
		item, err := store.GetItem(ctx, tx, scope, itemID)
		if err != nil {
			return err
		}
		line, err := s.grossLine(ctx, tx, scope, item, in.Amount, in.Reason)
		if err != nil {
			return err
		}
		today := s.today(ctx, tx, scope)
		ch, _, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, charge.CreateInput{
			OriginType: store.ChargeOriginDepositRetention, OriginID: dep.ID, CustomerID: dep.CustomerID,
			IssuedOn: today, DueOn: today, Description: in.Reason, Lines: []charge.LineInput{line},
		})
		if err != nil {
			return err
		}
		if ch.TotalCents != int64(in.Amount) || ch.PaidAmountCents != 0 {
			return charge.ErrChargeExists // one retention per deposit; release the rest with a refund
		}
		if _, err := s.matchTx(ctx, tx, scope, actor, dep.ID, []MatchInput{{ChargeID: ch.ID, Amount: in.Amount}}, matchOpts{allowDeposit: true}); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, actor, "payment.retain", "payment", dep.ID, map[string]any{
			"charge_id": ch.ID, "amount_cents": int64(in.Amount), "item_id": itemID,
		}); err != nil {
			return err
		}
		out, err = store.LedgerCharge(ctx, tx, scope, ch.ID)
		return err
	})
	return out, err
}

// grossLine builds a one-piece charge line of a gross amount with the item's VAT (net and VAT split half-even
// with money.VATSplit; a nature item has no VAT).
func (s *Service) grossLine(ctx context.Context, tx bun.IDB, scope store.MarinaScope, item *store.Item, gross money.Cents, description string) (charge.LineInput, error) {
	net, vat := money.VATSplit(gross, item.VATRateBP)
	class := ""
	if item.RevenueClassID != "" {
		rc, err := store.GetRevenueClass(ctx, tx, scope, item.RevenueClassID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return charge.LineInput{}, err
		}
		if rc != nil {
			class = rc.Code
		}
	}
	return charge.LineInput{
		ItemID: item.ID, ItemCode: item.Code, Description: description, Qty: qty.Milli(1000), Unit: gross,
		Net: net, VAT: vat, Gross: gross, VATRateBP: item.VATRateBP, VATNature: item.VATNature, PriceIncludesVAT: true,
		RevenueClassCode: class, ExternalCode: item.ExternalCode,
	}, nil
}
