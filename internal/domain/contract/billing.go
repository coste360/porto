package contract

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
)

// M5 billing hooks (m5-plan §8.1). The contract package never imports the M5 packages: it calls these hooks in
// the caller's transaction and internal/domain/charge implements them (wired in internal/app). An error from a
// hook aborts the caller's transaction.

// RepriceReason says why the price of a contract or transit changed (OnContractRepricedTx).
type RepriceReason string

// Reprice reasons.
const (
	RepriceMove         RepriceReason = "move"
	RepriceMoveUndo     RepriceReason = "move_undo"
	RepriceChangeStay   RepriceReason = "change_stay"
	RepriceAddendum     RepriceReason = "addendum"
	RepriceVesselChange RepriceReason = "vessel_change"
	RepriceEarlyPayment RepriceReason = "early_payment"
	RepriceReplan       RepriceReason = "replan"
)

// CloseChange says how a contract or transit ended (OnContractClosedTx).
type CloseChange string

// Close changes.
const (
	CloseCancelled  CloseChange = "cancelled"
	CloseNoShow     CloseChange = "no_show"
	CloseTerminated CloseChange = "terminated"
)

// BillingHooks receive the contract events that create or correct charges. Every method runs in the caller's
// transaction with the contract already locked; the actor is the one of the state change (audit.System for jobs).
type BillingHooks interface {
	// OnInstallmentIssuedTx runs after an instalment became issued (job, bulk issue, deposit at signature): it
	// creates the instalment's charge, sets installments.charge_id and queues the payment request (dedupe key
	// installment:<id>:issued).
	OnInstallmentIssuedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, inst *store.Installment) error
	// OnTransitConfirmedTx runs after a transit became confirmed (manual driver or provider webhook).
	OnTransitConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract) error
	// OnContractRepricedTx runs after the total or the snapshot adjustments of a contract or transit changed.
	OnContractRepricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, reason RepriceReason) error
	// OnContractClosedTx runs after a transit was cancelled or marked no-show, or a contract terminated.
	OnContractClosedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, change CloseChange) error
}

// noBilling is the default: no charge is created (M4 behaviour, contract unit tests).
type noBilling struct{}

func (noBilling) OnInstallmentIssuedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.Contract, *store.Installment) error {
	return nil
}

func (noBilling) OnTransitConfirmedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.Contract) error {
	return nil
}

func (noBilling) OnContractRepricedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.Contract, RepriceReason) error {
	return nil
}

func (noBilling) OnContractClosedTx(context.Context, bun.IDB, store.MarinaScope, audit.Actor, *store.Contract, CloseChange) error {
	return nil
}

// billing returns the registered hooks (noBilling when none).
func (s *Service) billing() BillingHooks {
	if s.d.Billing == nil {
		return noBilling{}
	}
	return s.d.Billing
}

// billingActive reports whether real billing hooks are registered. While they are not, the M4 payment request
// email of an issued instalment is still queued here (with hooks, charge queues the M5 payment_request email with
// the same dedupe key).
func (s *Service) billingActive() bool { return s.d.Billing != nil }

// repricedTx calls OnContractRepricedTx.
func (s *Service) repricedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, reason RepriceReason) error {
	return s.billing().OnContractRepricedTx(ctx, tx, scope, a.audit(), c, reason)
}

// closedTx calls OnContractClosedTx.
func (s *Service) closedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, change CloseChange) error {
	return s.billing().OnContractClosedTx(ctx, tx, scope, a.audit(), c, change)
}
