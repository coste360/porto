package cash

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Counter sales (m5-plan §5.3): items sold at the desk or the cash register, with or without a customer, in one
// transaction: the counter_sale charge, the payment, its match and (fiscal_document) the commercial document,
// issued through the payment's fiscal hook. A walk-in sale carries a commercial document (§16 D23).

// Receipt delivery options of a counter sale.
const (
	ReceiptNone  = "none"
	ReceiptPrint = "print"
	ReceiptEmail = "email"
)

// MaxCounterSaleLines bounds a sale.
const MaxCounterSaleLines = 100

// CounterSaleResult is the outcome of a counter sale.
type CounterSaleResult struct {
	Charge         *store.Charge
	Lines          []store.ChargeLine
	Payment        *store.Payment
	FiscalDocument *store.FiscalDocument
}

// ErrCustomerRequired is charge.ErrCustomerRequired (a sale without customer needs a commercial document).
var ErrCustomerRequired = charge.ErrCustomerRequired

// CounterSale records a counter sale (payments.collect; payments.collect_cash allows cash only; a unit price
// override needs charges.manage). The Idempotency-Key replays the stored sale.
func (s *Service) CounterSale(ctx context.Context, p *auth.Principal, marinaID string, in CounterSaleInput) (*CounterSaleResult, error) {
	scope, cashOnly, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if cashOnly && in.Method != store.PaymentMethodCash {
		return nil, auth.ErrForbidden
	}
	if err := validateSale(&in); err != nil {
		return nil, err
	}
	for _, l := range in.Lines {
		if l.Unit != nil {
			if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage); err != nil {
				return nil, err
			}
			break
		}
	}
	if in.IdempotencyKey != "" {
		if prev, err := store.PaymentByIdempotencyKey(ctx, s.d.DB.DB, scope, "counter_sale:"+in.IdempotencyKey); err == nil {
			return s.saleOfPayment(ctx, s.d.DB.DB, scope, prev)
		}
	}
	var out *CounterSaleResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res, err := s.CounterSaleTx(ctx, tx, scope, p.Actor(), in)
		out = res
		return err
	})
	if err != nil {
		var prev *store.Payment
		if in.IdempotencyKey != "" && store.IsUniqueViolation(err) {
			if prev, _ = store.PaymentByIdempotencyKey(ctx, s.d.DB.DB, scope, "counter_sale:"+in.IdempotencyKey); prev != nil {
				return s.saleOfPayment(ctx, s.d.DB.DB, scope, prev)
			}
		}
		return nil, err
	}
	return out, nil
}

func validateSale(in *CounterSaleInput) error {
	v := &auth.ValidationError{}
	if len(in.Lines) == 0 || len(in.Lines) > MaxCounterSaleLines {
		v.Add("lines", "invalid")
	}
	for _, l := range in.Lines {
		if !store.ValidID(l.ItemID) || l.QtyMilli <= 0 || l.Discount < 0 || (l.Unit != nil && *l.Unit < 0) {
			v.Add("lines", "invalid")
			break
		}
	}
	switch in.Method {
	case store.PaymentMethodCash, store.PaymentMethodCardPOS, store.PaymentMethodBankTransfer, store.PaymentMethodCheque, store.PaymentMethodOther:
	default:
		v.Add("payment.method", "invalid")
	}
	if in.Amount != nil && *in.Amount <= 0 {
		v.Add("payment.amount_cents", "invalid")
	}
	if in.Receipt == "" {
		in.Receipt = ReceiptNone
	}
	if in.Receipt != ReceiptNone && in.Receipt != ReceiptPrint && in.Receipt != ReceiptEmail {
		v.Add("receipt", "invalid")
	}
	if len(v.Fields) > 0 {
		return v
	}
	if in.CustomerID == "" && !in.FiscalDocument {
		return ErrCustomerRequired
	}
	if in.CustomerID == "" && in.Receipt == ReceiptEmail {
		return invalid("receipt", "customer_required")
	}
	return nil
}

// CounterSaleTx is the transaction of a counter sale (tests and the seed call it inside their own transaction).
func (s *Service) CounterSaleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in CounterSaleInput) (*CounterSaleResult, error) {
	if err := validateSale(&in); err != nil {
		return nil, err
	}
	today := s.today(ctx, tx, scope)
	lines, err := s.priceLines(ctx, tx, scope, in.Lines, today)
	if err != nil {
		return nil, err
	}
	mode := "invoice"
	if in.FiscalDocument {
		mode = "receipt"
	}
	ch, chLines, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, charge.CreateInput{
		OriginType: store.ChargeOriginCounterSale, CustomerID: in.CustomerID, VesselID: in.VesselID, IssuedOn: today,
		DueOn: today, FiscalMode: mode, Lines: lines,
	})
	if err != nil {
		return nil, err
	}
	if ch.TotalCents <= 0 {
		return nil, invalid("lines", "invalid")
	}
	amount := money.Cents(ch.TotalCents)
	if in.Amount != nil {
		amount = *in.Amount
	}
	if in.CustomerID == "" && amount != money.Cents(ch.TotalCents) {
		return nil, invalid("payment.amount_cents", "must_equal_total")
	}
	match := min(amount, money.Cents(ch.TotalCents))
	key := ""
	if in.IdempotencyKey != "" {
		key = "counter_sale:" + in.IdempotencyKey
	}
	pay, err := s.d.Payments.RecordTx(ctx, tx, scope, actor, payment.RecordInput{
		CustomerID: in.CustomerID, Kind: store.PaymentKindPayment, Method: in.Method, Source: store.PaymentSourceManual,
		Amount: amount, ReceivedOn: today, Matches: []payment.MatchInput{{ChargeID: ch.ID, Amount: match}},
		CashRegisterID: in.RegisterID, IdempotencyKey: key, SendReceipt: in.Receipt == ReceiptEmail,
	})
	if err != nil {
		return nil, err
	}
	res := &CounterSaleResult{Charge: ch, Lines: chLines, Payment: pay}
	if cur, err := store.GetCharge(ctx, tx, scope, ch.ID); err == nil {
		res.Charge = cur
	}
	if docs, err := store.FiscalDocumentsOfPayment(ctx, tx, scope, pay.ID); err == nil && len(docs) > 0 {
		res.FiscalDocument = &docs[len(docs)-1]
	} else if err != nil {
		return nil, err
	}
	if in.FiscalDocument && res.FiscalDocument == nil {
		return nil, errors.New("cash: fiscal document not issued")
	}
	diff := map[string]any{
		"charge_id": ch.ID, "payment_id": pay.ID, "total_cents": ch.TotalCents, "amount_cents": int64(amount),
		"method": in.Method, "fiscal_document": in.FiscalDocument, "walk_in": in.CustomerID == "",
	}
	if err := s.audit(ctx, tx, scope, actor, "counter_sale.create", "charge", ch.ID, diff); err != nil {
		return nil, err
	}
	return res, nil
}

// priceLines prices the sale lines through the charge service (unit price given, else the active price list;
// the same rules as manual charges), with the service period of today.
func (s *Service) priceLines(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in []CounterSaleLine, today calendar.Date) ([]charge.LineInput, error) {
	lines := make([]charge.ItemLine, 0, len(in))
	for _, l := range in {
		lines = append(lines, charge.ItemLine{
			ItemID: l.ItemID, Qty: qty.Milli(l.QtyMilli), Unit: l.Unit, Discount: l.Discount, ServiceFrom: today, ServiceTo: today,
		})
	}
	return s.d.Charges.PriceLines(ctx, tx, scope, lines, false)
}

// saleOfPayment rebuilds the result of a stored sale (idempotent replay).
func (s *Service) saleOfPayment(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *store.Payment) (*CounterSaleResult, error) {
	var chargeIDs []string
	if err := db.NewSelect().TableExpr("payment_matches").Column("charge_id").Where("marina_id = ?", scope.ID()).
		Where("payment_id = ?", p.ID).OrderExpr("id").Limit(1).Scan(ctx, &chargeIDs); err != nil {
		return nil, err
	}
	res := &CounterSaleResult{Payment: p}
	if len(chargeIDs) == 1 {
		ch, err := store.GetCharge(ctx, db, scope, chargeIDs[0])
		if err != nil {
			return nil, err
		}
		res.Charge = ch
		if res.Lines, err = store.CollectChargeLines(ctx, db, scope, ch.ID); err != nil {
			return nil, err
		}
	}
	docs, err := store.FiscalDocumentsOfPayment(ctx, db, scope, p.ID)
	if err != nil {
		return nil, err
	}
	if len(docs) > 0 {
		res.FiscalDocument = &docs[len(docs)-1]
	}
	return res, nil
}
