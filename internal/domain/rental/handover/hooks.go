package handover

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// The handover service receives the provider events of rental deposit pre-authorisations (collect calls these in
// the webhook transaction, INV-56 dedupe unchanged) and the outcome of rental contract signatures (subject kind
// rental).

var (
	_ collect.RentalDepositHooks = (*Service)(nil)
	_ signature.Subject          = (*Service)(nil)
)

// depositBooking locks the booking of a rental deposit intent (nil when the intent is not a booking's deposit).
func (s *Service) depositBooking(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in *store.PaymentIntent) (*store.RentalBooking, error) {
	b, err := store.RentalBookingByDepositIntent(ctx, tx, scope, in.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.d.Reservations.LockBookingTx(ctx, tx, scope, b.ID)
}

// OnRentalDepositAuthorizedTx moves the booking deposit pending -> authorized.
func (s *Service) OnRentalDepositAuthorizedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error {
	b, err := s.depositBooking(ctx, tx, scope, in)
	if err != nil || b == nil || b.DepositStatus != store.RentalDepositStatusPending {
		return err
	}
	if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, map[string]any{
		"deposit_status": store.RentalDepositStatusAuthorized,
	}, s.now()); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.deposit", "rental_booking", b.ID, map[string]any{
		"status": store.RentalDepositStatusAuthorized, "intent_id": in.ID,
	}); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, b, "deposit")
}

// OnRentalDepositCapturedTx records the captured deposit payment, retains it in full and closes the booking.
func (s *Service) OnRentalDepositCapturedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, amount money.Cents, providerRef string) error {
	b, err := s.depositBooking(ctx, tx, scope, in)
	if err != nil || b == nil {
		return err
	}
	if b.DepositDecisionCents != nil {
		// Only what the operator decided at check-in is retained (INV-98): an event without an amount (collect
		// falls back to the authorised amount) or with more than the decision is clamped to it.
		if d := money.Cents(*b.DepositDecisionCents); amount <= 0 || amount > d {
			amount = d
		}
	}
	if b.DepositPaymentID != "" || amount <= 0 {
		return nil // already recorded by another event of the same capture
	}
	set, err := store.LoadRentalSettings(ctx, tx, scope)
	if err != nil {
		return err
	}
	pay, err := s.d.Payments.RecordTx(ctx, tx, scope, actor, payment.RecordInput{
		CustomerID: in.CustomerID, Kind: store.PaymentKindDeposit, Method: store.PaymentMethodCardOnline,
		Source: store.PaymentSourceProvider, Amount: amount, ProviderCode: in.ProviderCode, ProviderRef: providerRef,
		IntentID: in.ID, Notes: "Cauzione noleggio",
	})
	if err != nil {
		return err
	}
	if _, err := s.d.Payments.RetainDepositTx(ctx, tx, scope, actor, payment.RetainDepositInput{
		DepositPaymentID: pay.ID, Amount: amount, FiscalMode: fiscalMode(b), CustomerID: b.CustomerID, BookingID: b.ID,
		ItemID: s.retentionItem(ctx, tx, scope, set), Reason: "Trattenuta cauzione noleggio",
	}); err != nil {
		return err
	}
	if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, map[string]any{
		"deposit_payment_id": pay.ID, "deposit_retained_cents": int64(amount),
		"deposit_status": retainedStatus(money.Cents(b.DepositCents), amount),
	}, s.now()); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.deposit", "rental_booking", b.ID, map[string]any{
		"status": "captured", "intent_id": in.ID, "payment_id": pay.ID, "retained_cents": int64(amount),
	}); err != nil {
		return err
	}
	b.DepositStatus = retainedStatus(money.Cents(b.DepositCents), amount)
	if err := s.changed(ctx, tx, scope, b, "deposit"); err != nil {
		return err
	}
	return s.closeIfDoneTx(ctx, tx, scope, actor, b)
}

// OnRentalDepositVoidedTx releases the deposit of a voided authorisation.
func (s *Service) OnRentalDepositVoidedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent) error {
	b, err := s.depositBooking(ctx, tx, scope, in)
	if err != nil || b == nil || depositFinal(b) {
		return err
	}
	if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, map[string]any{
		"deposit_status": store.RentalDepositStatusReleased, "deposit_retained_cents": int64(0),
	}, s.now()); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "rental_booking.deposit", "rental_booking", b.ID, map[string]any{
		"status": store.RentalDepositStatusReleased, "intent_id": in.ID, "voided": true,
	}); err != nil {
		return err
	}
	b.DepositStatus = store.RentalDepositStatusReleased
	if err := s.changed(ctx, tx, scope, b, "deposit"); err != nil {
		return err
	}
	return s.closeIfDoneTx(ctx, tx, scope, actor, b)
}

// closeIfDoneTx closes a returned booking whose deposit is final (§6: the return charge is decided at check-in).
func (s *Service) closeIfDoneTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error {
	if b.Status != store.RentalStatusReturned || !depositFinal(b) {
		return nil
	}
	_, err := s.d.Reservations.CloseTx(ctx, tx, scope, actor, b.ID)
	return err
}

// Kind implements signature.Subject (signature_requests.subject_type rental; subject id = booking id).
func (s *Service) Kind() string { return store.SignatureSubjectRental }

// OnSignedTx records the signed contract of a booking.
func (s *Service) OnSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, signedDocumentID string, at time.Time) error {
	b, err := store.GetRentalBooking(ctx, tx, scope, subjectID)
	if err != nil {
		return notFound(err)
	}
	if err := s.audit(ctx, tx, scope, audit.System, "rental_booking.contract_signed", "rental_booking", b.ID, map[string]any{
		"signed_document_id": signedDocumentID, "at": store.UTC(at),
	}); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, b, "contract")
}

// OnFailedTx records a declined, expired or cancelled contract signature.
func (s *Service) OnFailedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, status string) error {
	b, err := store.GetRentalBooking(ctx, tx, scope, subjectID)
	if err != nil {
		return notFound(err)
	}
	return s.changed(ctx, tx, scope, b, "contract")
}

// ---- reservation side effects (reservation.Hooks, installed with reservation.Service.SetHooks) ----

var _ reservation.Hooks = (*Service)(nil)

// RentalConfirmedTx composes the rental contract when a booking becomes confirmed (rendered after commit, signed at
// check-out, §6, D14).
func (s *Service) RentalConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error {
	if s.d.Docs == nil {
		return nil
	}
	if _, err := store.RentalDocument(ctx, tx, scope, string(docs.KindRentalContract), b.ID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err := s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
		Kind: docs.KindRentalContract, SubjectType: docs.SubjectRentalBooking, SubjectID: b.ID, PDFA: true, CreatedBy: userOf(actor),
	})
	return err
}

// RentalEndedTx follows a cancellation (desk or hold expiry) or a no-show (§6, D22): a cancelled booking's unpaid
// main charge is offset in full; a no-show keeps (or creates) the main charge; an open contract signature is
// cancelled; a pending or authorised pre-authorisation is released and voided at the provider after commit.
func (s *Service) RentalEndedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.RentalBooking) error {
	switch b.Status {
	case store.RentalStatusNoShow:
		if _, err := s.mainChargeTx(ctx, tx, scope, actor, b); err != nil {
			return err
		}
	case store.RentalStatusCancelled:
		if err := s.CancelMainChargeTx(ctx, tx, scope, actor, b); err != nil {
			return err
		}
	}
	if s.d.Signatures != nil {
		if err := s.d.Signatures.CancelTx(ctx, tx, scope, store.SignatureSubjectRental, b.ID); err != nil {
			return err
		}
	}
	if b.DepositMode != store.RentalDepositPreauth || b.DepositIntentID == "" ||
		(b.DepositStatus != store.RentalDepositStatusPending && b.DepositStatus != store.RentalDepositStatusAuthorized) {
		return nil
	}
	if err := store.SetRentalBookingDeposit(ctx, tx, scope, b.ID, map[string]any{
		"deposit_status": store.RentalDepositStatusReleased,
	}, s.now()); err != nil {
		return err
	}
	intentID := b.DepositIntentID
	if b.DepositStatus == store.RentalDepositStatusPending {
		if _, err := store.SetIntentStatus(ctx, tx, scope, intentID, []string{store.IntentCreated, store.IntentOpen}, store.IntentCancelled, "", s.now()); err != nil {
			return err
		}
		return nil
	}
	if s.d.Collect != nil {
		store.AfterCommit(ctx, func() {
			bg := context.WithoutCancel(ctx)
			if err := s.d.Collect.VoidIntent(bg, scope, intentID); err != nil {
				s.d.Log.WarnContext(bg, "rental pre-authorisation not voided", "intent_id", intentID)
			}
		})
	}
	return nil
}

// RentalRepricedTx adjusts the main charge after a reschedule or an extension (§9.1): a linked delta adjustment
// when the main charge exists (exported charges never change, INV-51).
func (s *Service) RentalRepricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, r *reservation.RepriceResult) error {
	if r == nil || r.Booking == nil {
		return nil
	}
	_, err := s.AdjustMainChargeTx(ctx, tx, scope, actor, r.Booking)
	return err
}
