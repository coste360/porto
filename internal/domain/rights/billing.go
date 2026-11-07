package rights

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
)

// BillingHooks receive the holder instalment events that create charges (m5-plan §8.1). The rights package never
// imports the M5 packages; internal/domain/charge implements the hooks (wired in internal/app). An error aborts
// the caller's transaction.
type BillingHooks interface {
	// OnHolderInstallmentIssuedTx runs after a holder instalment became issued (job installments.issue): it creates
	// the charge (item from charge_origin_items), sets holder_installments.charge_id and queues the payment request.
	OnHolderInstallmentIssuedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, h *store.HolderInstallment) error
}

// SetBilling registers the billing hooks (nil keeps the M4 behaviour: no charge, M4 payment request email).
func (s *Service) SetBilling(h BillingHooks) { s.billing = h }
