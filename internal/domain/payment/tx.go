package payment

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
)

// Transaction primitives (m5-plan §4.2–§4.4, §14.3). They take an authorised scope and an actor and are never
// called from handlers. Lock order: contract → instalments → payment → charges (ascending id) → cash session →
// sequences.

// CashSessions resolves the cash session a payment or refund lands in (implemented by internal/domain/cash).
// required is true for cash: no open session ⇒ cash.ErrSessionRequired; a session closed meanwhile ⇒
// cash.ErrSessionClosed. It returns "" when not required and no session is open.
type CashSessions interface {
	SessionForPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, registerID string, required bool) (string, error)
}

// FiscalRecorder issues the commercial document of a receipt-mode charge paid by a payment (implemented by
// internal/domain/fiscal), in the payment transaction.
type FiscalRecorder interface {
	RecordTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, p *store.Payment, ch *store.Charge) (*store.FiscalDocument, error)
}
