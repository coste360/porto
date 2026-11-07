package contract

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors (m3-plan §7.4; names fixed), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrInvalidTransition means the action is not allowed in the current state (§4 tables); the error is a
	// *TransitionError. 409 invalid_transition.
	ErrInvalidTransition = errors.New("contract: invalid transition")
	// ErrQuoteExpired means the quote is past its validity. 409 quote_expired.
	ErrQuoteExpired = errors.New("contract: quote expired")
	// ErrCustomerRequired means a transit cannot be confirmed without a customer. 422 customer_required.
	ErrCustomerRequired = errors.New("contract: customer required")
	// ErrPriceListChanged means the price list of the snapshot is no longer the active one for the arrival
	// (priced before the write transaction, m3-plan §4.6). 409 conflict: the client retries.
	ErrPriceListChanged = errors.New("contract: price list changed")
	// ErrRepriceRefused means a move (or its undo) would replace the price of a contract that has no price
	// snapshot, or bring its total below zero (m3-plan §13 D34). 409 conflict: nothing is written.
	ErrRepriceRefused = errors.New("contract: reprice refused")
	// ErrVersionMismatch means If-Match did not match the current version. 412 version_conflict.
	ErrVersionMismatch = errors.New("contract: version mismatch")
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound
)

// TransitionError names the refused transition.
type TransitionError struct {
	From   string // current status
	Action Action
}

func (e *TransitionError) Error() string {
	return ErrInvalidTransition.Error() + ": " + string(e.Action) + " from " + e.From
}

// Is makes errors.Is(err, ErrInvalidTransition) true.
func (e *TransitionError) Is(target error) bool { return target == ErrInvalidTransition }
