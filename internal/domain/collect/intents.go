package collect

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Payment intents and provider events (m5-plan §5.4, INV-09, INV-56). Intents are written first, the provider is
// called after commit and its answer applied in a new transaction. Provider events are applied once (dedupe row
// in the same transaction as their effects); a transit is confirmed online only here, from a verified event,
// never from the browser return.

// CheckoutTTL is the life of a hosted checkout session (the link outlives it: a new session is opened on click).
const CheckoutTTL = time.Hour

// ErrPaymentInFlight: a payment of these charges is being processed by the provider (409).
var ErrPaymentInFlight = fmt.Errorf("collect: payment in flight: %w", store.ErrConflict)

// ErrCustomerRequired: a payment link needs the customer of the transit (422 customer_required).
var ErrCustomerRequired = errors.New("collect: customer required")

// Purposes and statuses (aliases of the store constants).
const (
	PurposePaymentRequest = store.IntentPurposePaymentRequest
	PurposeTransit        = store.IntentPurposeTransit
	PurposeSEPACollect    = store.IntentPurposeSEPACollect
	PurposeMandateSetup   = store.IntentPurposeMandateSetup
)

var (
	fromAnyButSucceeded = []string{store.IntentCreated, store.IntentOpen, store.IntentProcessing, store.IntentFailed, store.IntentExpired, store.IntentCancelled}
	fromLive            = store.LiveIntentStatuses
	fromCreatedOpen     = []string{store.IntentCreated, store.IntentOpen}
)

// newIntentTx inserts an intent with its charges; the attempt numbers the idempotency key
// <purpose>:<subject>:<attempt>. A charge with a live intent is ErrPaymentInFlight.
func (s *Service) newIntentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, subject string, charges []store.PaymentIntentCharge) (*store.PaymentIntent, error) {
	prefix := in.Purpose + ":" + subject + ":"
	n, err := store.CountIntents(ctx, tx, scope, prefix)
	if err != nil {
		return nil, err
	}
	now := s.now()
	in.ID, in.MarinaID, in.Attempt, in.IdempotencyKey = store.NewID(), scope.ID(), n+1, prefix+strconv.Itoa(n+1)
	in.Status, in.Currency, in.CreatedByActor, in.Version, in.CreatedAt, in.UpdatedAt = store.IntentCreated, "EUR", actorString(actor), 1, now, now
	out, created, err := store.InsertPaymentIntent(ctx, tx, scope, in, charges)
	if errors.Is(err, store.ErrConflict) {
		return nil, ErrPaymentInFlight
	}
	if err != nil {
		return nil, err
	}
	if !created {
		return out, nil
	}
	if err := s.audit(ctx, tx, scope, actor, "intent.create", "payment_intent", out.ID, map[string]any{
		"purpose": out.Purpose, "provider_code": out.ProviderCode, "amount_cents": out.AmountCents, "attempt": out.Attempt,
		"request_id": out.RequestID, "contract_id": out.ContractID, "charge_count": len(charges),
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// methodsFor maps a pay page method to the provider methods of a checkout.
func methodsFor(method string) []string {
	if method == "sepa_debit" {
		return []string{"sepa_debit"}
	}
	return []string{"card", "wallet"}
}

// paymentMethod maps an event or the intent's requested method to a ledger method.
func paymentMethod(ev *ext.PaymentEvent, in *store.PaymentIntent) string {
	m := ev.Method
	if m == "" {
		m = in.Method
	}
	if in.Purpose == PurposeSEPACollect {
		m = "sepa_debit"
	}
	switch m {
	case "sepa_debit":
		return store.PaymentMethodSEPADebit
	case "wallet":
		return store.PaymentMethodWallet
	}
	return store.PaymentMethodCardOnline
}

// ApplyProviderEvent applies a verified provider event in one transaction (dedupe by event id, INV-56; intent
// locked by reference; transit confirmation only here or through the manual driver, INV-09). An event for an intent
// that is plausibly ours but not visible yet is ErrRetryLater (503, the provider retries); a foreign or unknown
// event is recorded as ignored.
func (s *Service) ApplyProviderEvent(ctx context.Context, providerCode, marinaID string, ev *ext.PaymentEvent) error {
	if ev == nil || ev.EventID == "" {
		return ErrInvalidWebhookSignature
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return ErrNotFound
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return s.applyTx(ctx, tx, scope, providerCode, ev)
	})
}

func (s *Service) applyTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, provider string, ev *ext.PaymentEvent) error {
	now := s.now()
	row := &store.PaymentProviderEvent{
		ID: store.NewID(), MarinaID: scope.ID(), ProviderCode: provider, EventID: limit(ev.EventID, 200), Kind: ev.Kind,
		Status: EventApplied, ReceivedAt: now, CreatedAt: now,
	}
	inserted, err := store.InsertProviderEvent(ctx, tx, row)
	if err != nil {
		return err
	}
	if !inserted {
		return nil // duplicate delivery: already applied or ignored
	}
	status, intentID, err := s.dispatchTx(ctx, tx, scope, provider, ev)
	if err != nil {
		return err
	}
	if status != EventApplied || intentID != "" {
		if _, err := tx.NewUpdate().Model((*store.PaymentProviderEvent)(nil)).Set("status = ?", status).
			Set("intent_id = ?", nullID(intentID)).Where("id = ?", row.ID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func nullID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func limit(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// dispatchTx applies an event and reports the event status and the intent it concerned.
func (s *Service) dispatchTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, provider string, ev *ext.PaymentEvent) (string, string, error) {
	actor := providerActor(provider)
	switch ev.Kind {
	case ext.PaymentEventRefundSucceeded, ext.PaymentEventRefundFailed:
		return s.refundEventTx(ctx, tx, scope, actor, provider, ev)
	case ext.PaymentEventMandateRevoked:
		return s.mandateRevokedTx(ctx, tx, scope, actor, provider, ev)
	case ext.PaymentEventDisputed:
		return s.disputeTx(ctx, tx, scope, provider, ev)
	case ext.PaymentEventSucceeded, ext.PaymentEventProcessing, ext.PaymentEventFailed, ext.PaymentEventExpired, ext.PaymentEventMandateActive:
	default:
		return EventIgnored, "", nil
	}
	in, err := s.intentOfEvent(ctx, tx, scope, provider, ev)
	if err != nil {
		return "", "", err
	}
	if in == nil {
		return EventIgnored, "", nil
	}
	if ev.Kind == ext.PaymentEventMandateActive {
		return EventApplied, in.ID, s.mandateActiveTx(ctx, tx, scope, actor, in, ev)
	}
	switch ev.Kind {
	case ext.PaymentEventSucceeded:
		err = s.succeededTx(ctx, tx, scope, actor, in, ev)
	case ext.PaymentEventProcessing:
		if ev.PaymentRef != "" {
			err = store.SetIntentPaymentRef(ctx, tx, scope, in.ID, ev.PaymentRef, s.now())
		}
		if err == nil {
			_, err = store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentProcessing, "", s.now())
		}
	case ext.PaymentEventFailed:
		err = s.failedTx(ctx, tx, scope, actor, in, ev.FailureCode)
	case ext.PaymentEventExpired:
		err = s.expiredTx(ctx, tx, scope, actor, in)
	}
	return EventApplied, in.ID, err
}

// disputeTx handles a dispute (chargeback) of a provider payment (m5-plan §1.2): the event is recorded as ignored
// (no money moves automatically) and, when the payment is ours, a dispute worklist item is raised on it in the same
// transaction as the event row, so that the office learns that money was withdrawn.
func (s *Service) disputeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, provider string, ev *ext.PaymentEvent) (string, string, error) {
	if ev.PaymentRef == "" {
		return EventIgnored, "", nil
	}
	pay, err := store.PaymentByProviderRef(ctx, tx, scope, provider, ev.PaymentRef)
	if errors.Is(err, store.ErrNotFound) {
		intentID, err := store.IntentIDByRef(ctx, tx, scope, provider, "", ev.PaymentRef)
		if err != nil || intentID == "" {
			return EventIgnored, "", err
		}
		ids, err := store.PaymentIDsOfIntent(ctx, tx, scope, intentID)
		if err != nil || len(ids) == 0 {
			return EventIgnored, intentID, err
		}
		return EventIgnored, intentID, store.RaiseWorkItem(ctx, tx, scope, payment.WorkDispute, ids[0], disputeOccurrence(ev), s.now())
	}
	if err != nil {
		return "", "", err
	}
	return EventIgnored, pay.IntentID, store.RaiseWorkItem(ctx, tx, scope, payment.WorkDispute, pay.ID, disputeOccurrence(ev), s.now())
}

// disputeOccurrence keys a dispute item by the provider event: each dispute event (opened, updated, closed, or a
// new dispute on the same payment) reopens an item the office has already closed.
func disputeOccurrence(ev *ext.PaymentEvent) string {
	return "event:" + ev.EventID
}

// intentOfEvent locks the intent of an event: by metadata (same marina), then by session or payment reference.
// nil means the event is not ours (ignored); ErrRetryLater means it names this marina but the intent is not
// visible yet.
func (s *Service) intentOfEvent(ctx context.Context, tx bun.IDB, scope store.MarinaScope, provider string, ev *ext.PaymentEvent) (*store.PaymentIntent, error) {
	metaMarina, metaIntent := ev.Metadata["marina_id"], ev.Metadata["intent_id"]
	if metaMarina != "" && metaMarina != scope.ID() {
		return nil, nil
	}
	id := ""
	if metaIntent != "" && store.ValidID(metaIntent) {
		if in, err := store.GetPaymentIntent(ctx, tx, scope, metaIntent); err == nil && in.ProviderCode == provider {
			id = in.ID
		}
	}
	if id == "" {
		found, err := store.IntentIDByRef(ctx, tx, scope, provider, ev.SessionRef, ev.PaymentRef)
		if err != nil {
			return nil, err
		}
		id = found
	}
	if id == "" {
		foreign, err := store.IntentRefExistsElsewhere(ctx, tx, scope, provider, ev.SessionRef, ev.PaymentRef)
		if err != nil {
			return nil, err
		}
		if !foreign && metaMarina == scope.ID() && metaIntent != "" {
			return nil, ErrRetryLater
		}
		return nil, nil
	}
	return store.LockPaymentIntent(ctx, tx, scope, id)
}

// succeededTx records the provider payment of an intent once and matches it: transit → confirm (INV-09) and the
// transit charge; requests and SEPA collections → their charges in due order capped at the residuals. Money
// that cannot be matched stays as customer credit (§16 D14).
func (s *Service) succeededTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, ev *ext.PaymentEvent) error {
	now := s.now()
	if err := store.SetIntentPaymentRef(ctx, tx, scope, in.ID, ev.PaymentRef, now); err != nil {
		return err
	}
	if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromAnyButSucceeded, store.IntentSucceeded, "", now); err != nil {
		return err
	}
	if in.Purpose == PurposeMandateSetup {
		return nil
	}
	existing, err := store.PaymentIDsOfIntent(ctx, tx, scope, in.ID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil // already recorded by another event of the same payment
	}
	ref := ev.PaymentRef
	if ref == "" {
		ref = in.ProviderPaymentRef
	}
	if ref == "" {
		ref = "intent:" + in.ID
	}
	if _, err := store.PaymentByProviderRef(ctx, tx, scope, in.ProviderCode, ref); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	amount := money.Cents(ev.AmountCents)
	if amount <= 0 {
		amount = money.Cents(in.AmountCents)
	}
	if amount <= 0 {
		return nil
	}
	rec := payment.RecordInput{
		CustomerID: in.CustomerID, Kind: store.PaymentKindPayment, Method: paymentMethod(ev, in), Source: store.PaymentSourceProvider,
		Amount: amount, ProviderCode: in.ProviderCode, ProviderRef: ref, IntentID: in.ID, Fee: money.Cents(ev.FeeCents),
	}
	var chargeIDs []string
	switch in.Purpose {
	case PurposeTransit:
		ids, err := s.transitSucceededTx(ctx, tx, scope, actor, in, amount, &rec)
		if err != nil {
			return err
		}
		chargeIDs = ids
	default:
		ids, err := s.intentChargeIDs(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		// Lock order contract/right → payment → charges (INV-31, INV-52): planMatches locks the charges.
		if err := s.d.Payments.LockChargeParentsTx(ctx, tx, scope, ids); err != nil {
			return err
		}
		matches, err := planMatches(ctx, tx, scope, ids, amount)
		if err != nil {
			return err
		}
		rec.Matches, chargeIDs = matches, ids
	}
	pay, err := s.d.Payments.RecordTx(ctx, tx, scope, actor, rec)
	if err != nil {
		return err
	}
	// Money left unallocated is customer credit (never auto-refunded, §16 D14): the office sees it in the worklist.
	if pay.AllocatedCents < pay.AmountCents {
		kind := "overpayment"
		if in.Purpose == PurposeTransit && len(rec.Matches) == 0 {
			kind = "late_payment"
		}
		if err := store.RaiseWorkItem(ctx, tx, scope, kind, pay.ID, "", s.now()); err != nil {
			return err
		}
	}
	if err := s.audit(ctx, tx, scope, actor, "intent.apply", "payment_intent", in.ID, map[string]any{
		"status": store.IntentSucceeded, "amount_cents": int64(amount), "matched": len(rec.Matches),
	}); err != nil {
		return err
	}
	if in.RequestID != "" && in.Purpose == PurposeTransit {
		if r, err := store.GetPaymentRequest(ctx, tx, scope, in.RequestID); err == nil {
			if err := s.closeRequestPaidTx(ctx, tx, scope, actor, r, 0); err != nil {
				return err
			}
		}
	}
	return s.markRequestsPaidTx(ctx, tx, scope, actor, chargeIDs)
}

// intentChargeIDs are the charges an intent pays: its own rows, else the charges of its request.
func (s *Service) intentChargeIDs(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in *store.PaymentIntent) ([]string, error) {
	rows, err := store.IntentCharges(ctx, tx, scope, in.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ChargeID)
	}
	if len(ids) == 0 && in.RequestID != "" {
		byReq, err := store.RequestChargeIDs(ctx, tx, scope, []string{in.RequestID})
		if err != nil {
			return nil, err
		}
		ids = byReq[in.RequestID]
	}
	return ids, nil
}

// planMatches locks the charges (ascending id) and fills them in due order up to their residuals.
func planMatches(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ids []string, amount money.Cents) ([]payment.MatchInput, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	charges, err := store.LockChargesSorted(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(charges, func(a, b store.Charge) int {
		if c := a.DueOn.Compare(b.DueOn); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	var out []payment.MatchInput
	left := amount
	for _, c := range charges {
		res := money.Cents(c.ResidualCents())
		if left <= 0 {
			break
		}
		if res <= 0 {
			continue
		}
		a := min(res, left)
		out = append(out, payment.MatchInput{ChargeID: c.ID, Amount: a})
		left -= a
	}
	return out, nil
}

// transitSucceededTx confirms a held or pending_payment transit (source provider) and plans the match to its
// charge; a transit that can no longer be confirmed leaves the money as credit with an office email.
func (s *Service) transitSucceededTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, amount money.Cents, rec *payment.RecordInput) ([]string, error) {
	c, err := store.GetContract(ctx, tx, scope, in.ContractID)
	if err != nil {
		return nil, err
	}
	switch contract.Status(c.Status) {
	case contract.StatusPendingPayment, contract.StatusHeld:
		if _, err := s.d.Contracts.ConfirmTransitPaidTx(ctx, tx, scope, nil, c.ID, "provider"); err != nil {
			if !errors.Is(err, contract.ErrInvalidTransition) {
				return nil, err
			}
			return nil, s.latePaymentTx(ctx, tx, scope, in, c, amount)
		}
	case contract.StatusConfirmed, contract.StatusArrived, contract.StatusDeparted:
	default:
		return nil, s.latePaymentTx(ctx, tx, scope, in, c, amount)
	}
	ch, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTransit, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil // no charge (billing hooks off or a free stay): the payment stays as credit
	}
	if err != nil {
		return nil, err
	}
	matches, err := planMatches(ctx, tx, scope, []string{ch.ID}, amount)
	if err != nil {
		return nil, err
	}
	rec.Matches = matches
	return []string{ch.ID}, nil
}

// latePaymentTx emails the office about online money that arrived for a transit that cannot be confirmed any
// more (expired with its hold lost, cancelled): the payment stays as customer credit (§16 D14).
func (s *Service) latePaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in *store.PaymentIntent, c *store.Contract, amount money.Cents) error {
	if s.d.Notify == nil || s.d.Keys == nil {
		return nil
	}
	cs, err := store.GetContractSettings(ctx, tx, scope, s.now())
	if err != nil || cs.OfficeEmailEnc == "" {
		return err
	}
	to, err := s.d.Keys.DecryptString(cs.OfficeEmailEnc, "office-email:"+scope.ID())
	if err != nil || to == "" {
		return nil
	}
	_, name, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return err
	}
	_, _, err = s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: notify.TemplateLatePaymentOffice, MarinaID: scope.ID(), To: to, DedupeKey: "late_payment:" + in.ID,
		SourceType: "payment_intent", SourceID: in.ID,
		Values: map[string]string{
			notify.ValueMarinaName: name, notify.ValueContractCode: contract.Code(c), notify.ValueAmount: euro(int64(amount)),
		},
	})
	return err
}

func (s *Service) failedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, code string) error {
	code = limit(code, 64)
	changed, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromLive, store.IntentFailed, code, s.now())
	if err != nil || !changed {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "intent.apply", "payment_intent", in.ID, map[string]any{"status": store.IntentFailed, "failure_code": code}); err != nil {
		return err
	}
	if in.Purpose == PurposeMandateSetup {
		if m, err := store.SEPAMandateBySetupIntent(ctx, tx, scope, in.ID); err == nil {
			if _, err := store.SetSEPAMandateStatus(ctx, tx, scope, m.ID, []string{store.MandatePending}, store.MandateFailed, s.now()); err != nil {
				return err
			}
		}
	}
	return s.emit(ctx, tx, scope, event.PaymentFailed, event.PaymentFailedData{
		IntentID: in.ID, ProviderCode: in.ProviderCode, Purpose: in.Purpose, FailureCode: code,
	})
}

// expiredTx expires an open intent; a transit whose link expired goes back to held (or the M3 expiry path).
func (s *Service) expiredTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error {
	changed, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentExpired, "", s.now())
	if err != nil || !changed {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "intent.apply", "payment_intent", in.ID, map[string]any{"status": store.IntentExpired}); err != nil {
		return err
	}
	if in.Purpose != PurposeTransit || in.RequestID == "" {
		return nil
	}
	r, err := store.GetPaymentRequest(ctx, tx, scope, in.RequestID)
	if err != nil {
		return err
	}
	if r.Status == store.RequestOpen && (r.ExpiresAt.IsZero() || r.ExpiresAt.After(s.now())) {
		return nil // the link is still valid: the customer may try again
	}
	return s.revertTransitTx(ctx, tx, scope, in.ContractID)
}

func (s *Service) revertTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) error {
	c, err := store.GetContract(ctx, tx, scope, contractID)
	if err != nil || contract.Status(c.Status) != contract.StatusPendingPayment {
		return err
	}
	// A lapsed hold is left to the M3 expiry path (contract expiry job: pending_payment → expired).
	if _, err := s.d.Contracts.RevertTransitPaymentTx(ctx, tx, scope, nil, contractID); err != nil &&
		!errors.Is(err, contract.ErrInvalidTransition) && !errors.Is(err, occupancy.ErrHoldExpired) {
		return err
	}
	return nil
}

// refundEventTx completes or fails the pending provider refund of the event's payment.
func (s *Service) refundEventTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, provider string, ev *ext.PaymentEvent) (string, string, error) {
	if ev.PaymentRef == "" {
		return EventIgnored, "", nil
	}
	pay, err := store.PaymentByProviderRef(ctx, tx, scope, provider, ev.PaymentRef)
	if errors.Is(err, store.ErrNotFound) {
		return EventIgnored, "", nil
	}
	if err != nil {
		return "", "", err
	}
	pending, err := store.PendingProviderRefunds(ctx, tx, scope, pay.ID, time.Time{}, 100)
	if err != nil {
		return "", "", err
	}
	var pick *store.Refund
	for i := range pending {
		r := &pending[i]
		if ev.RefundRef != "" && r.ProviderRef == ev.RefundRef {
			pick = r
			break
		}
	}
	if pick == nil {
		for i := range pending {
			if pending[i].ProviderRef == "" && pending[i].AmountCents == ev.AmountCents {
				pick = &pending[i]
				break
			}
		}
	}
	if pick == nil {
		return EventIgnored, "", nil // a refund made at the provider: Porto records only its own refunds
	}
	if ev.Kind == ext.PaymentEventRefundSucceeded {
		_, err = s.d.Payments.CompleteProviderRefundTx(ctx, tx, scope, actor, pick.ID, ev.RefundRef)
	} else {
		_, err = s.d.Payments.FailProviderRefundTx(ctx, tx, scope, actor, pick.ID, limit(ev.FailureCode, 64))
	}
	return EventApplied, pay.IntentID, err
}

// RefundSupported implements payment.ProviderRefunder.
// It is called inside the refund transaction without it, so it answers from the driver registry alone (no
// database read): the provider settings are read when the refund is sent after commit.
func (s *Service) RefundSupported(_ context.Context, _ store.MarinaScope, providerCode string) bool {
	drv, err := s.onlineDriver(providerCode)
	return err == nil && drv.Capabilities().Refund
}

// SendRefund implements payment.ProviderRefunder: called after the refund committed; the outcome is applied in a
// new transaction (a pending answer waits for the webhook or the reconciliation).
func (s *Service) SendRefund(ctx context.Context, scope store.MarinaScope, refundID string) {
	if err := s.sendRefund(ctx, scope, refundID); err != nil {
		s.d.Log.WarnContext(ctx, "provider refund not sent", "marina_id", scope.ID(), "refund_id", refundID, "code", providerCode(err))
	}
}

func (s *Service) sendRefund(ctx context.Context, scope store.MarinaScope, refundID string) error {
	db := s.d.DB.DB
	r, err := store.GetRefund(ctx, db, scope, refundID)
	if err != nil || r.Status != store.RefundPending || r.PaymentID == "" {
		return err
	}
	pay, err := store.GetPayment(ctx, db, scope, r.PaymentID)
	if err != nil {
		return err
	}
	e, err := s.providerByCode(ctx, db, scope, pay.ProviderCode)
	if err != nil {
		return err
	}
	cctx, err := s.withCreds(ctx, scope, e.row)
	if err != nil {
		return err
	}
	res, err := e.drv.Refund(cctx, ext.RefundRequest{
		MarinaID: scope.ID(), RefundID: r.ID, IdempotencyKey: "refund:" + r.ID, PaymentRef: pay.ProviderRef,
		AmountCents: r.AmountCents, Currency: pay.Currency,
	})
	actor := providerActor(pay.ProviderCode)
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		switch {
		case err != nil:
			var se interface{ ProviderCode() string }
			if errors.As(err, &se) {
				_, ferr := s.d.Payments.FailProviderRefundTx(ctx, tx, scope, actor, r.ID, limit(se.ProviderCode(), 64))
				return ferr
			}
			return nil // transport error: the reconciliation sends it again (same idempotency key)
		case res.Status == "succeeded":
			_, err := s.d.Payments.CompleteProviderRefundTx(ctx, tx, scope, actor, r.ID, res.RefundRef)
			return err
		case res.Status == "failed":
			_, err := s.d.Payments.FailProviderRefundTx(ctx, tx, scope, actor, r.ID, "provider_failed")
			return err
		}
		return nil
	})
}

// openCheckoutTx prepares the intent of a checkout from the public page and returns it with what to send.
type checkoutPlan struct {
	intent  *store.PaymentIntent
	reuse   string // URL of a live session to reuse
	e       *enabled
	desc    string
	locale  string
	success string
	cancel  string
}

// startCheckout calls the provider for a planned intent (after commit) and applies its answer.
func (s *Service) startCheckout(ctx context.Context, scope store.MarinaScope, plan *checkoutPlan) (string, error) {
	if plan.reuse != "" {
		return plan.reuse, nil
	}
	in := plan.intent
	cctx, err := s.withCreds(ctx, scope, plan.e.row)
	if err != nil {
		return "", ErrProviderDisabled
	}
	exp := s.now().Add(CheckoutTTL)
	sess, callErr := plan.e.drv.CreateCheckout(cctx, ext.CheckoutRequest{
		MarinaID: scope.ID(), IntentID: in.ID, IdempotencyKey: in.IdempotencyKey, AmountCents: in.AmountCents, Currency: in.Currency,
		Description: plan.desc, Methods: methodsFor(in.Method), SuccessURL: plan.success, CancelURL: plan.cancel, ExpiresAt: exp,
		Locale: plan.locale,
	})
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if callErr != nil {
			_, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentFailed, providerCode(callErr), s.now())
			return err
		}
		if !sess.ExpiresAt.IsZero() {
			exp = sess.ExpiresAt
		}
		return store.SetIntentRefs(ctx, tx, scope, in.ID, sess.SessionRef, sess.PaymentRef, sess.URL, exp, s.now())
	})
	if callErr != nil {
		return "", providerError(callErr)
	}
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

// TransitLinkInput is a payment link for a held transit.
type TransitLinkInput struct {
	ExpiresInHours int
	Send           bool
}

// CreateTransitLink moves a held transit to pending_payment with a payment link (transits.desk, If-Match on the
// contract). The checkout session is opened on click from the pay page, so the emailed link survives session
// expiry; the transit is confirmed only by the provider webhook (INV-09).
func (s *Service) CreateTransitLink(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int, in TransitLinkInput) (*RequestView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if in.ExpiresInHours == 0 {
		in.ExpiresInHours = 24
	}
	if _, err := store.GetContract(ctx, s.d.DB.DB, scope, contractID); err != nil {
		return nil, err // another marina's transit is 404 before any other answer
	}
	if in.ExpiresInHours < 1 || in.ExpiresInHours > 72 {
		return nil, invalid("expires_in_hours", "range")
	}
	e, err := s.enabledProvider(ctx, s.d.DB.DB, scope, needCheckout)
	if err != nil {
		return nil, err
	}
	var out *RequestView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		holdUntil := s.now().Add(time.Duration(in.ExpiresInHours) * time.Hour)
		c, err := s.d.Contracts.RequestTransitPaymentTx(ctx, tx, scope, p, contractID, ifMatch, holdUntil)
		if err != nil {
			return err
		}
		cust := c.BillingCustomerID
		if cust == "" {
			cust = c.CustomerID
		}
		if cust == "" {
			return ErrCustomerRequired
		}
		if c.TotalCents <= 0 {
			return invalid("total_cents", "zero")
		}
		r, tok, err := s.createRequestTx(ctx, tx, scope, p.Actor(), cust, RequestTransit, nil, c.TotalCents, holdUntil)
		if err != nil {
			return err
		}
		if _, err := s.newIntentTx(ctx, tx, scope, p.Actor(), &store.PaymentIntent{
			Purpose: PurposeTransit, ProviderCode: e.row.ProviderCode, CustomerID: cust, AmountCents: c.TotalCents,
			RequestID: r.ID, ContractID: c.ID, ExpiresAt: holdUntil,
		}, c.ID, nil); err != nil {
			return err
		}
		if in.Send {
			id, err := s.transitMailTx(ctx, tx, scope, r, tok, c)
			if err != nil {
				return err
			}
			r.LastMessageID = id
			if err := store.UpdateRequestSent(ctx, tx, scope, r, 0, s.now()); err != nil {
				return err
			}
		}
		slug, _, err := marinaSlug(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = &RequestView{Request: *r, URL: s.PayURL(slug, tok)}
		return nil
	})
	return out, err
}

func (s *Service) transitMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.PaymentRequest, token string, c *store.Contract) (string, error) {
	if s.d.Notify == nil {
		return "", nil
	}
	to, locale, name, err := customerContact(ctx, tx, scope, r.CustomerID)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", ErrCustomerEmailMissing
	}
	slug, marinaName, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	vals := s.mailValues(ctx, tx, scope, marinaName, name, nil, r.AmountCents)
	vals[notify.ValueLink], vals[notify.ValueContractCode], vals["reference"] = s.PayURL(slug, token), contract.Code(c), contract.Code(c)
	id, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: notify.TemplatePaymentRequest, MarinaID: scope.ID(), CustomerID: r.CustomerID, To: to, Locale: locale,
		DedupeKey: "payment_request:" + r.ID + ":1", SourceType: "payment_request", SourceID: r.ID, Values: vals,
	})
	return id, err
}
