package payment

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Rental deposits (M11, m11-plan §7 step 3, §8.1, D15, D16, INV-98). A rental deposit taken in cash or by card POS
// is a payment of kind deposit; it is never revenue and never matched to ordinary charges. Retaining part of it at
// check-in creates the deposit_retention charge of the booking (origin_id = booking) and matches the deposit to it
// (the only match a deposit may have, query B60); receipt mode also issues the commercial document of the retention
// through the FiscalDriver. Unlike Retain these run inside the rental service's transaction under rental.manage.

// RetainDepositInput retains part of a deposit payment for a rental booking.
type RetainDepositInput struct {
	DepositPaymentID string
	Amount           money.Cents
	// FiscalMode is the booking's mode (charge.FiscalReceipt or charge.FiscalInvoice): receipt issues the commercial document of the retention through the
	// FiscalDriver (fiscal.RecordTx); invoice sends the retention charge to the export (16.5).
	FiscalMode string
	ItemID     string // default: the origin item deposit_retention of the billing settings
	CustomerID string // the booking customer, empty for a walk-in renter (receipt mode only)
	BookingID  string // origin of the retention charge (origin_type deposit_retention, origin_id booking)
	Reason     string
}

// RetainDepositTx creates the deposit_retention charge of in.Amount and matches the deposit payment to it (the only
// match a deposit payment may have, query B60). in.Amount above the unallocated deposit is ErrRefundExceeds.
func (s *Service) RetainDepositTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in RetainDepositInput) (*store.Charge, error) {
	if in.Amount <= 0 {
		return nil, invalid("amount_cents", "positive")
	}
	if !store.ValidID(in.BookingID) {
		return nil, invalid("booking_id", "invalid")
	}
	if in.FiscalMode == "" {
		in.FiscalMode = charge.FiscalReceipt
	}
	if in.FiscalMode != charge.FiscalReceipt && in.FiscalMode != charge.FiscalInvoice {
		return nil, invalid("fiscal_mode", "invalid")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 500 {
		return nil, invalid("reason", "too_long")
	}
	if s.d.Charges == nil {
		return nil, errors.New("payment: charge service not wired")
	}
	dep, err := store.LockPayment(ctx, tx, scope, in.DepositPaymentID)
	if err != nil {
		return nil, err
	}
	if dep.Kind != store.PaymentKindDeposit {
		return nil, ErrNotDeposit
	}
	if int64(in.Amount) > dep.UnallocatedCents() {
		return nil, ErrRefundExceeds
	}
	if in.CustomerID != "" && in.CustomerID != dep.CustomerID {
		return nil, invalid("customer_id", "other_customer")
	}
	if in.FiscalMode == charge.FiscalInvoice && dep.CustomerID == "" {
		return nil, charge.ErrCustomerRequired
	}
	itemID := in.ItemID
	if itemID == "" {
		if itemID, err = store.LedgerOriginItemID(ctx, tx, scope, "deposit_retention"); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrRetentionItemMissing
			}
			return nil, err
		}
	}
	item, err := store.GetItem(ctx, tx, scope, itemID)
	if err != nil {
		return nil, err
	}
	desc := in.Reason
	if desc == "" {
		desc = "Trattenuta cauzione"
	}
	line, err := s.grossLine(ctx, tx, scope, item, in.Amount, desc)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, tx, scope)
	ch, _, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, charge.CreateInput{
		OriginType: store.ChargeOriginDepositRetention, OriginID: in.BookingID, CustomerID: dep.CustomerID,
		IssuedOn: today, DueOn: today, FiscalMode: in.FiscalMode, Description: desc, Lines: []charge.LineInput{line},
	})
	if err != nil {
		return nil, err
	}
	if ch == nil || ch.TotalCents != int64(in.Amount) || ch.PaidAmountCents != 0 {
		return nil, charge.ErrChargeExists // one retention per booking deposit; the rest is released with a refund
	}
	if _, err := s.matchTx(ctx, tx, scope, actor, dep.ID, []MatchInput{{ChargeID: ch.ID, Amount: in.Amount}}, matchOpts{allowDeposit: true}); err != nil {
		return nil, err
	}
	if in.FiscalMode == charge.FiscalReceipt && s.receipt != nil {
		cur, err := store.GetPayment(ctx, tx, scope, dep.ID)
		if err != nil {
			return nil, err
		}
		ledger, err := store.LedgerCharge(ctx, tx, scope, ch.ID)
		if err != nil {
			return nil, err
		}
		if _, err := s.receipt.RecordTx(ctx, tx, scope, actor, cur, ledger); err != nil {
			return nil, err
		}
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.retain", "payment", dep.ID, map[string]any{
		"charge_id": ch.ID, "amount_cents": int64(in.Amount), "item_id": itemID, "rental_booking_id": in.BookingID,
		"fiscal_mode": in.FiscalMode,
	}); err != nil {
		return nil, err
	}
	out, err := store.LedgerCharge(ctx, tx, scope, ch.ID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RentalDepositInput records a rental deposit taken at the desk (cash or card POS).
type RentalDepositInput struct {
	CustomerID     string // empty for a walk-in renter
	Method         string // cash, card_pos
	Amount         money.Cents
	CashRegisterID string
	IdempotencyKey string
	Notes          string
}

// RecordRentalDepositTx records a rental deposit payment (kind deposit, source manual) in tx. With a customer it is
// RecordTx; a walk-in renter has no customer (payments.customer_id is NULL only for walk-ins), so the deposit is
// written here with the same numbering, cash session, audit and event. Cash needs an open session
// (cash_session_required).
func (s *Service) RecordRentalDepositTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in RentalDepositInput) (*store.Payment, error) {
	if in.Method != store.PaymentMethodCash && in.Method != store.PaymentMethodCardPOS {
		return nil, invalid("method", "invalid")
	}
	if in.Amount <= 0 {
		return nil, invalid("amount_cents", "positive")
	}
	if in.CustomerID != "" {
		return s.RecordTx(ctx, tx, scope, actor, RecordInput{
			CustomerID: in.CustomerID, Kind: store.PaymentKindDeposit, Method: in.Method, Source: store.PaymentSourceManual,
			Amount: in.Amount, CashRegisterID: in.CashRegisterID, IdempotencyKey: in.IdempotencyKey, Notes: in.Notes,
		})
	}
	if in.IdempotencyKey != "" {
		if prev, err := store.PaymentByIdempotencyKey(ctx, tx, scope, in.IdempotencyKey); err == nil {
			if prev.Kind != store.PaymentKindDeposit || prev.AmountCents != int64(in.Amount) || prev.Method != in.Method {
				return nil, ErrIdempotencyMismatch
			}
			return prev, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	var sessionID string
	required := in.Method == store.PaymentMethodCash
	if s.cash == nil {
		if required {
			return nil, ErrCashUnavailable
		}
	} else if required || slices.Contains(deskMethods, in.Method) {
		id, err := s.cash.SessionForPaymentTx(ctx, tx, scope, in.CashRegisterID, required)
		if err != nil {
			return nil, err
		}
		sessionID = id
	}
	today := s.today(ctx, tx, scope)
	n, err := store.NextLedgerNumber(ctx, tx, scope, seriesPayment, today.Year())
	if err != nil {
		return nil, err
	}
	now := s.now()
	p := &store.Payment{
		ID: store.NewID(), MarinaID: scope.ID(), Number: ledgerNumber("P", today.Year(), n), Kind: store.PaymentKindDeposit,
		Method: in.Method, Source: store.PaymentSourceManual, AmountCents: int64(in.Amount), Currency: "EUR",
		ReceivedOn: today, CashSessionID: sessionID, VATSplit: store.JSON(`[]`), Notes: strings.TrimSpace(in.Notes),
		IdempotencyKey: in.IdempotencyKey, RecordedByUserID: userOf(actor), RecordedByActor: actorString(actor),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertPayment(ctx, tx, p); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.record", "payment", p.ID, map[string]any{
		"number": p.Number, "kind": p.Kind, "method": p.Method, "source": p.Source, "amount_cents": p.AmountCents,
		"received_on": p.ReceivedOn, "cash_session_id": p.CashSessionID, "walk_in": true,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentSucceeded, event.PaymentSucceededData{
		PaymentID: p.ID, Kind: p.Kind, Method: p.Method, Source: p.Source, AmountCents: p.AmountCents,
	}); err != nil {
		return nil, err
	}
	return store.GetPayment(ctx, tx, scope, p.ID)
}
