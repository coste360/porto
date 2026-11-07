package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Recording payments (m5-plan §4.2): one transaction per payment, idempotent by Idempotency-Key, cash landing in
// the open session of the register, then the requested or automatic matches.

var (
	manualMethods   = []string{store.PaymentMethodCash, store.PaymentMethodCardPOS, store.PaymentMethodBankTransfer, store.PaymentMethodCheque, store.PaymentMethodOther}
	providerMethods = []string{store.PaymentMethodCardOnline, store.PaymentMethodWallet, store.PaymentMethodSEPADebit, store.PaymentMethodBankTransfer, store.PaymentMethodOther}
	// deskMethods attach to the open session of the register when one exists (Z totals per method).
	deskMethods  = []string{store.PaymentMethodCardPOS, store.PaymentMethodCheque, store.PaymentMethodOther}
	creditSource = []string{store.PaymentSourceAdjustment, store.PaymentSourceGoodwill, store.PaymentSourceRelease, store.PaymentSourceMandate, store.PaymentSourceOpeningBalance}
)

// ErrCashUnavailable is returned for a cash payment when no cash service is wired (tests only).
var ErrCashUnavailable = errors.New("payment: cash sessions not wired")

// Record records a payment (payments.collect; payments.collect_cash allows method cash only) and returns it with
// its matches. A replay with the same Idempotency-Key and input returns the stored payment.
func (s *Service) Record(ctx context.Context, p *auth.Principal, marinaID string, in RecordInput) (*Detail, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermPaymentsCollectCash)
	if err != nil {
		return nil, err
	}
	if perm == auth.PermPaymentsCollectCash && in.Method != store.PaymentMethodCash {
		return nil, auth.ErrForbidden
	}
	in.Source, in.ProviderCode, in.ProviderRef, in.IntentID, in.Fee = store.PaymentSourceManual, "", "", "", 0
	in.AdjustmentChargeID, in.ReleaseID, in.MandateID = "", "", ""
	if in.Kind == store.PaymentKindCredit {
		return nil, invalid("kind", "invalid")
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pay, err := s.RecordTx(ctx, tx, scope, p.Actor(), in)
		if err != nil {
			return err
		}
		id = pay.ID
		return nil
	})
	if err != nil && in.IdempotencyKey != "" && store.IsUniqueViolation(err) {
		// A concurrent request with the same key won the unique index: return its payment (or the mismatch).
		pay, rerr := store.PaymentByIdempotencyKey(ctx, s.d.DB.DB, scope, in.IdempotencyKey)
		if rerr != nil {
			return nil, err
		}
		norm := in
		_ = s.checkRecord(&norm)
		if pay.IdempotencyHash != inputHash(norm) {
			return nil, ErrIdempotencyMismatch
		}
		id, err = pay.ID, nil
	}
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// inputHash is the SHA-256 of the canonical input of a payment (idempotency replays).
func inputHash(in RecordInput) string {
	c := in
	c.IdempotencyKey, c.SendReceipt = "", false
	c.Reference = strings.TrimSpace(c.Reference)
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Service) checkRecord(in *RecordInput) error {
	v := &auth.ValidationError{}
	switch in.Kind {
	case store.PaymentKindPayment, store.PaymentKindDeposit:
		methods := manualMethods
		if in.Source == store.PaymentSourceProvider {
			methods = providerMethods
		}
		if !slices.Contains(methods, in.Method) {
			v.Add("method", "invalid")
		}
	case store.PaymentKindCredit:
		in.Method = store.PaymentMethodCreditNote
		if !slices.Contains(creditSource, in.Source) {
			v.Add("source", "invalid")
		}
	default:
		v.Add("kind", "invalid")
	}
	switch in.Source {
	case store.PaymentSourceManual, store.PaymentSourceBackfill:
	case store.PaymentSourceProvider:
		if in.ProviderCode == "" {
			v.Add("provider_code", "required")
		}
	default:
		if in.Kind != store.PaymentKindCredit {
			v.Add("source", "invalid")
		}
	}
	if in.Amount <= 0 {
		v.Add("amount_cents", "positive")
	}
	if in.CustomerID == "" && in.Kind != store.PaymentKindPayment {
		v.Add("customer_id", "required")
	}
	if in.CustomerID != "" && !store.ValidID(in.CustomerID) {
		v.Add("customer_id", "invalid")
	}
	in.Reference = strings.TrimSpace(in.Reference)
	if utf8.RuneCountInString(in.Reference) > 200 {
		v.Add("reference", "too_long")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if utf8.RuneCountInString(in.Notes) > 500 {
		v.Add("notes", "too_long")
	}
	if in.AutoMatch != "" && in.AutoMatch != AutoMatchOldestDueFirst {
		v.Add("auto_match", "invalid")
	}
	if in.AutoMatch != "" && len(in.Matches) > 0 {
		v.Add("matches", "with_auto_match")
	}
	if in.Kind == store.PaymentKindDeposit && (len(in.Matches) > 0 || in.AutoMatch != "") {
		v.Add("matches", "deposit")
	}
	if len(in.Matches) > 0 {
		if err := checkMatches(in.Matches); err != nil {
			return err
		}
	}
	if len(in.IdempotencyKey) > 255 {
		v.Add("idempotency_key", "too_long")
	}
	return v.Err()
}

// RecordTx records a payment in tx (idempotent by key), lands cash in the open session, applies the requested or
// automatic matches and writes audit payment.record and payment.succeeded.
func (s *Service) RecordTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in RecordInput) (*store.Payment, error) {
	if in.Source == "" {
		in.Source = store.PaymentSourceManual
	}
	if err := s.checkRecord(&in); err != nil {
		return nil, err
	}
	hash := inputHash(in)
	if in.IdempotencyKey != "" {
		prev, err := store.PaymentByIdempotencyKey(ctx, tx, scope, in.IdempotencyKey)
		switch {
		case err == nil:
			if prev.IdempotencyHash != hash {
				return nil, ErrIdempotencyMismatch
			}
			return prev, nil
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
	}
	if in.Source == store.PaymentSourceProvider && in.ProviderRef != "" {
		if prev, err := store.PaymentByProviderRef(ctx, tx, scope, in.ProviderCode, in.ProviderRef); err == nil {
			return prev, nil // the same provider payment is recorded once
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	today := s.today(ctx, tx, scope)
	if in.ReceivedOn.IsZero() {
		in.ReceivedOn = today
	} else if in.ReceivedOn.After(today) {
		return nil, invalid("received_on", "in_future")
	}
	if in.CustomerID != "" {
		cs, err := store.CustomersByIDs(ctx, tx, scope, []string{in.CustomerID})
		if err != nil {
			return nil, err
		}
		if len(cs) == 0 {
			return nil, ErrNotFound
		}
	}
	// Cash lands in the open session of the register (required); desk methods attach to it when one is open.
	var sessionID string
	if in.Kind != store.PaymentKindCredit && (in.Source == store.PaymentSourceManual) {
		required := in.Method == store.PaymentMethodCash
		if required || slices.Contains(deskMethods, in.Method) {
			if s.cash == nil {
				if required {
					return nil, ErrCashUnavailable
				}
			} else {
				id, err := s.cash.SessionForPaymentTx(ctx, tx, scope, in.CashRegisterID, required)
				if err != nil {
					return nil, err
				}
				sessionID = id
			}
		}
	}
	now := s.now()
	n, err := store.NextLedgerNumber(ctx, tx, scope, seriesPayment, in.ReceivedOn.Year())
	if err != nil {
		return nil, err
	}
	p := &store.Payment{
		ID: store.NewID(), MarinaID: scope.ID(), Number: ledgerNumber("P", in.ReceivedOn.Year(), n), CustomerID: in.CustomerID,
		Kind: in.Kind, Method: in.Method, Source: in.Source, ProviderCode: in.ProviderCode, ProviderRef: in.ProviderRef,
		IntentID: in.IntentID, AdjustmentChargeID: in.AdjustmentChargeID, ReleaseID: in.ReleaseID, MandateID: in.MandateID,
		AmountCents: int64(in.Amount), FeeCents: int64(in.Fee), Currency: "EUR", ReceivedOn: in.ReceivedOn,
		CashSessionID: sessionID, VATSplit: store.JSON(`[]`), Notes: in.Notes, IdempotencyKey: in.IdempotencyKey,
		RecordedByUserID: userOf(actor), RecordedByActor: actorString(actor), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if in.IdempotencyKey != "" {
		p.IdempotencyHash = hash
	}
	if in.Reference != "" {
		if s.d.Keys == nil {
			return nil, errors.New("payment: key ring not wired")
		}
		enc, err := s.d.Keys.EncryptString(in.Reference, paymentRefAAD(p.ID))
		if err != nil {
			return nil, err
		}
		p.ReferenceEnc = enc
	}
	if err := store.InsertPayment(ctx, tx, p); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "payment.record", "payment", p.ID, map[string]any{
		"number": p.Number, "customer_id": p.CustomerID, "kind": p.Kind, "method": p.Method, "source": p.Source,
		"provider_code": p.ProviderCode, "amount_cents": p.AmountCents, "received_on": p.ReceivedOn,
		"cash_session_id": p.CashSessionID, "intent_id": p.IntentID, "has_reference": in.Reference != "",
	}); err != nil {
		return nil, err
	}
	switch {
	case len(in.Matches) > 0:
		if _, err := s.matchTx(ctx, tx, scope, actor, p.ID, in.Matches, matchOpts{recordContext: true}); err != nil {
			return nil, err
		}
	case in.AutoMatch == AutoMatchOldestDueFirst:
		if _, err := s.matchTx(ctx, tx, scope, actor, p.ID, nil, matchOpts{auto: true, recordContext: true}); err != nil {
			return nil, err
		}
	}
	cur, err := store.GetPayment(ctx, tx, scope, p.ID)
	if err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.PaymentSucceeded, event.PaymentSucceededData{
		PaymentID: cur.ID, CustomerID: cur.CustomerID, Kind: cur.Kind, Method: cur.Method, Source: cur.Source,
		ProviderCode: cur.ProviderCode, AmountCents: cur.AmountCents, AllocatedCents: cur.AllocatedCents,
	}); err != nil {
		return nil, err
	}
	if in.SendReceipt && cur.CustomerID != "" {
		if err := s.queueReceiptTx(ctx, tx, scope, cur); err != nil {
			return nil, err
		}
	}
	return cur, nil
}

// queueReceiptTx queues the payment_receipt email (best effort: no email address or template yet = no email).
func (s *Service) queueReceiptTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *store.Payment) error {
	if s.d.Notify == nil {
		return nil
	}
	cs, err := store.CustomersByIDs(ctx, tx, scope, []string{p.CustomerID})
	if err != nil || len(cs) == 0 {
		return err
	}
	var emails []struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(cs[0].Emails, &emails)
	to := ""
	for _, e := range emails {
		if v := strings.TrimSpace(e.Value); v != "" {
			to = v
			break
		}
	}
	if to == "" {
		return nil
	}
	_, _, err = s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: notify.TemplatePaymentReceipt, MarinaID: scope.ID(), CustomerID: p.CustomerID, To: to, Locale: cs[0].Locale,
		DedupeKey: "payment:" + p.ID + ":receipt", SourceType: "payment", SourceID: p.ID,
		Values: map[string]string{
			"payment_number": p.Number, "amount": money.Cents(p.AmountCents).String(), "received_on": p.ReceivedOn.String(),
			"method": p.Method,
		},
	})
	if err != nil && strings.Contains(err.Error(), "unknown template") {
		return nil // the M5 templates are written by CASH-DRIVERS
	}
	return err
}
