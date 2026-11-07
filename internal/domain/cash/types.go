package cash

import (
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// DefaultRegisterCode is the code of the register created lazily (EnsureDefaultRegister).
const DefaultRegisterCode = "main"

// CountedInput is a counted amount per method at close.
type CountedInput struct {
	Method string
	Amount money.Cents
}

// CounterSaleLine is one line of a counter sale (m5-plan §5.3).
type CounterSaleLine struct {
	ItemID   string
	QtyMilli int64
	Unit     *money.Cents // override (charges.manage)
	Discount money.Cents
}

// CounterSaleInput is a counter sale: charge, payment, match and optional fiscal document in one transaction.
type CounterSaleInput struct {
	CustomerID     string
	VesselID       string
	Lines          []CounterSaleLine
	Method         string
	Amount         *money.Cents
	RegisterID     string
	FiscalDocument bool
	Receipt        string // none, print, email
	IdempotencyKey string
}

// Store types re-exported for the HTTP layer (which never imports internal/store).
type (
	// Movement is a cash movement.
	Movement = store.CashMovement
	// SessionFilter narrows ListSessions.
	SessionFilter = store.CashSessionFilter
	// SessionPage is a page of sessions.
	SessionPage = store.Page[Session]
)
