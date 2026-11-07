package cash

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Errors (m5-plan §7.4).
var (
	// ErrSessionRequired: a cash payment or refund needs an open session (409 cash_session_required).
	ErrSessionRequired = errors.New("cash: open session required")
	// ErrSessionOpen: the register already has an open session (409 cash_session_open, INV-55).
	ErrSessionOpen = errors.New("cash: session already open")
	// ErrSessionClosed: the session was closed (409 cash_session_closed).
	ErrSessionClosed = errors.New("cash: session closed")
	// ErrInsufficient: a withdrawal above the expected cash (409 cash_insufficient).
	ErrInsufficient = errors.New("cash: insufficient cash")
	// ErrRegisterInUse: the register has an open session or history that blocks the change (409 register_in_use).
	ErrRegisterInUse = errors.New("cash: register in use")
	// ErrCodeTaken: another register has the code (409 code_taken).
	ErrCodeTaken = errors.New("cash: code taken")
	// ErrVersionMismatch: If-Match differs (412).
	ErrVersionMismatch = errors.New("cash: version mismatch")
	// ErrNotFound is the store sentinel (404).
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the contract-phase skeleton until CASH-DRIVERS lands M5.
	ErrNotImplemented = errors.New("cash: not implemented")
)
