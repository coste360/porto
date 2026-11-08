// Package charge is the single state-change point of charges (spec 16.1, 16.5; m5-plan §3): charges from every
// origin with their lines, true-ups, adjustments and offsets, cancellation, the termination charge, the backfill of
// M4 instalments, opening balances, the "da fatturare" export with its files and re-export, invoice
// reconciliation and the ChargeExporter jobs. It implements contract.BillingHooks and rights.BillingHooks.
// Porto never issues invoices: exported charges are immutable (INV-51) and corrections are linked adjustments.
package charge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// PaymentRequester queues the payment request of a new charge (implemented by internal/domain/collect). The
// dedupe key keeps the M4 key for instalments (installment:<id>:issued), so no email is sent twice.
type PaymentRequester interface {
	RequestForChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, dedupeKey string) error
}

// CreditGranter turns the remainder of a negative charge into customer credit (implemented by
// internal/domain/payment: a credit payment, source adjustment; source opening_balance for opening balances).
type CreditGranter interface {
	GrantAdjustmentCreditTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, amount money.Cents) error
}

// BackfillPayer records the historical payment of an M4 instalment settled manually before M5 (m5-plan §3.7): a
// manual payment with source backfill, the settled method and date, matched in full to the new charge. It is
// implemented by internal/domain/payment next to CreditGranter (the registered CreditGranter is asserted to it);
// without it the backfill creates the charge and leaves it open.
type BackfillPayer interface {
	RecordBackfillPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, in BackfillPayment) error
}

// OriginFollower makes an instalment or holder instalment follow the payment status of its charge after an
// offset (a negative adjustment) changed it (m5-plan §8.2, query B32). The payment service implements it next to
// CreditGranter; the parents are locked by the caller before the charge.
type OriginFollower interface {
	FollowChargeOriginTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, chargeID string) error
}

// BackfillPayment is the settlement of an M4 instalment as the backfill hands it to the payment service.
type BackfillPayment struct {
	Method     string // the instalment settlement_method
	ReceivedOn calendar.Date
	RefEnc     string // the instalment settlement_ref_enc (AAD of the instalment); decrypt and re-encrypt
	Amount     money.Cents
}

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Keys   *crypto.KeyRing
	Files  *filestore.Store
	Prices *pricelist.Service
	Docs   *docs.Service
	Log    *slog.Logger
	// Exporters are the registered ChargeExporters (none ships with Porto, spec 33 D4).
	Exporters []ext.ChargeExporter
}

// Service is the charge service.
type Service struct {
	d         Deps
	requester PaymentRequester
	credits   CreditGranter
	onCancel  map[string]CancelHook // by origin type
}

// CancelHook runs inside the transaction that cancels a charge of its origin type, after the charge row is
// cancelled: the origin releases what the charge had claimed (utility consumptions back to pending, INV-90).
type CancelHook func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge) error

// SetCancelHook registers the cancel hook of an origin type (utility registers itself in its NewService).
func (s *Service) SetCancelHook(originType string, h CancelHook) {
	if s.onCancel == nil {
		s.onCancel = map[string]CancelHook{}
	}
	s.onCancel[originType] = h
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

// SetPaymentRequester registers the payment request queue (collect).
func (s *Service) SetPaymentRequester(r PaymentRequester) { s.requester = r }

// SetCreditGranter registers the credit granter (payment).
func (s *Service) SetCreditGranter(c CreditGranter) { s.credits = c }

// SetExporters replaces the registered ChargeExporters (tests).
func (s *Service) SetExporters(x []ext.ChargeExporter) { s.d.Exporters = x }

// ResidualTx returns what is still owed on a charge (total − paid − credited; 0 for negative or cancelled).
func (s *Service) ResidualTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, chargeID string) (money.Cents, error) {
	c, err := store.GetCharge(ctx, tx, scope, chargeID)
	if err != nil {
		return 0, notFound(err)
	}
	return money.Cents(c.ResidualCents()), nil
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// location returns the marina time zone (UTC when unknown).
func location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// today is the marina business date.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return calendar.In(s.d.Clock.Now(), location(ctx, db, scope))
}

// dateOf is the marina date of an instant.
func dateOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, at time.Time) calendar.Date {
	return calendar.In(at, location(ctx, db, scope))
}

func (s *Service) write(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entityType, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entityType,
		EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, data any) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// actorLabel is the created_by_actor of a charge: user, system:<job> or provider:<code>.
func actorLabel(a audit.Actor) (label, userID string) {
	switch a.Type {
	case audit.ActorUser:
		return audit.ActorUser, a.ID
	case audit.ActorSystem:
		if a.ID != "" {
			return "system:" + a.ID, ""
		}
		return "system", ""
	default:
		return a.Type, ""
	}
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// mapStore maps the store errors of charge writes to the charge sentinels.
func mapStore(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrImmutable):
		return fmt.Errorf("%w: %w", ErrImmutable, err)
	case errors.Is(err, store.ErrVersionMismatch):
		return ErrVersionMismatch
	case errors.Is(err, store.ErrOriginCharged):
		return ErrChargeExists
	case errors.Is(err, store.ErrNotFound):
		return auth.ErrNotFound
	}
	return err
}

func invalid(field, code string) error {
	return &auth.ValidationError{Fields: []auth.FieldError{{Field: field, Code: code}}}
}
