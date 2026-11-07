// Package payment is the single state-change point of the ledger (spec 16.2; m5-plan §4): payments, matches to
// charges and their reversals, refunds and payouts, customer credits, deposits and their retention, mandate
// compensation, and the read models built on them (statement, balances, ageing, receivables, customer dashboard,
// map payment status, worklist). Only this package changes charges.paid_amount_cents and
// payments.allocated_cents/refunded_cents (INV-50, INV-52).
package payment

import (
	"log/slog"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Keys      *crypto.KeyRing
	Charges   *charge.Service
	Contracts *contract.Service
	Rights    *rights.Service
	Docs      *docs.Service
	Notify    *notify.Service
	Log       *slog.Logger
}

// Service is the payment service.
type Service struct {
	d        Deps
	cash     CashSessions
	receipt  FiscalRecorder
	refunder ProviderRefunder
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d}
	// The M4 settle routes of contracts and rights record their payments here (m5-plan §8.2); contract and rights
	// cannot import payment, so the payment service registers itself.
	if d.Contracts != nil {
		d.Contracts.SetChargeSettler(s)
	}
	if d.Rights != nil {
		d.Rights.SetChargeSettler(s)
	}
	return s
}

// SetCash registers the cash sessions (cash.Service.SessionForPaymentTx).
func (s *Service) SetCash(c CashSessions) { s.cash = c }

// SetFiscal registers the fiscal recorder of receipt-mode charges (fiscal.Service.RecordTx).
func (s *Service) SetFiscal(f FiscalRecorder) { s.receipt = f }
