package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M5 cash and fiscal tables (internal/migrations/*/00011_cash.sql, fiscal_documents of
// 00012_payments.sql). Owned by the CASH-DRIVERS builder after the contract phase.

// Cash session statuses.
const (
	CashSessionOpen   = "open"
	CashSessionClosed = "closed"
)

// Fiscal document statuses.
const (
	FiscalPending = "pending"
	FiscalIssued  = "issued"
	FiscalFailed  = "failed"
	FiscalVoid    = "void"
)

// CashRegister is a row of cash_registers (default main created lazily).
type CashRegister struct {
	bun.BaseModel `bun:"table:cash_registers"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Code      string    `bun:"code"`
	Name      string    `bun:"name"`
	Active    bool      `bun:"active"`
	IsDefault bool      `bun:"is_default"`
	Version   int       `bun:"version"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// CashSession is a row of cash_sessions (INV-55: one open per register; closed rows are immutable).
type CashSession struct {
	bun.BaseModel `bun:"table:cash_sessions"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	RegisterID      string        `bun:"register_id"`
	Status          string        `bun:"status"`
	OpenedByUserID  string        `bun:"opened_by_user_id,nullzero"`
	OpenedAt        time.Time     `bun:"opened_at"`
	BusinessDate    calendar.Date `bun:"business_date"`
	FloatCents      int64         `bun:"float_cents"`
	ClosedByUserID  string        `bun:"closed_by_user_id,nullzero"`
	ClosedAt        time.Time     `bun:"closed_at,nullzero"`
	Expected        JSON          `bun:"expected"` // {method: cents}
	Counted         JSON          `bun:"counted"`  // {method: cents}
	DifferenceCents int64         `bun:"difference_cents"`
	Notes           string        `bun:"notes"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// CashMovement is a row of cash_movements (insert-only).
type CashMovement struct {
	bun.BaseModel `bun:"table:cash_movements"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	SessionID       string    `bun:"session_id"`
	Kind            string    `bun:"kind"` // in, out
	AmountCents     int64     `bun:"amount_cents"`
	Reason          string    `bun:"reason"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}

// FiscalSequence is a row of fiscal_sequences (daily numbering of fiscal documents).
type FiscalSequence struct {
	bun.BaseModel `bun:"table:fiscal_sequences"`

	MarinaID   string        `bun:"marina_id,pk"`
	Day        calendar.Date `bun:"day,pk"`
	LastNumber int64         `bun:"last_number"`
}

// FiscalDocument is a row of fiscal_documents (documento commerciale tracked by Porto, issued by a driver).
type FiscalDocument struct {
	bun.BaseModel `bun:"table:fiscal_documents"`

	ID           string        `bun:"id,pk"`
	MarinaID     string        `bun:"marina_id"`
	PaymentID    string        `bun:"payment_id"`
	ChargeID     string        `bun:"charge_id"`
	RefundID     string        `bun:"refund_id,nullzero"`
	Driver       string        `bun:"driver"`
	Number       string        `bun:"number"` // YYYYMMDD-NNNN
	BusinessDate calendar.Date `bun:"business_date"`
	Status       string        `bun:"status"`
	Lines        JSON          `bun:"lines"`
	VAT          JSON          `bun:"vat"`
	TotalCents   int64         `bun:"total_cents"`
	ExternalRef  string        `bun:"external_ref,nullzero"`
	ErrorCode    string        `bun:"error_code,nullzero"`
	Attempts     int           `bun:"attempts"`
	IssuedAt     time.Time     `bun:"issued_at,nullzero"`
	VoidedAt     time.Time     `bun:"voided_at,nullzero"`
	Version      int           `bun:"version"`
	CreatedAt    time.Time     `bun:"created_at"`
	UpdatedAt    time.Time     `bun:"updated_at"`
}
