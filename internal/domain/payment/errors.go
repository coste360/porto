package payment

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Errors (m5-plan §7.4). Mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrMatchExceeds: a match would exceed the payment or the charge residual (409 match_exceeds, INV-52); see
	// *LimitError for the details.
	ErrMatchExceeds = errors.New("payment: match exceeds the limit")
	// ErrRefundExceeds: a refund above the unallocated money, refunds in flight included (409 refund_exceeds).
	ErrRefundExceeds = errors.New("payment: refund exceeds the unallocated amount")
	// ErrAlreadyReversed: the match was reversed already (409 already_reversed).
	ErrAlreadyReversed = errors.New("payment: match already reversed")
	// ErrCreditInsufficient: not enough customer credit (409 credit_insufficient).
	ErrCreditInsufficient = errors.New("payment: credit insufficient")
	// ErrNotDeposit: retention on a payment that is not a deposit (409 not_deposit).
	ErrNotDeposit = errors.New("payment: not a deposit")
	// ErrNothingToSettle: no unsettled mandate revenue up to the date (409 nothing_to_settle).
	ErrNothingToSettle = errors.New("payment: nothing to settle")
	// ErrIdempotencyMismatch: same Idempotency-Key with a different input (422 idempotency_key_reused).
	ErrIdempotencyMismatch = errors.New("payment: idempotency key reused with a different input")
	// ErrReleaseNotResold: a release credit needs a resold release of the customer (422 release_not_resold).
	ErrReleaseNotResold = errors.New("payment: release not resold")
	// ErrVersionMismatch: If-Match differs (412).
	ErrVersionMismatch = errors.New("payment: version mismatch")
	// ErrNotFound is the store sentinel (404).
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the contract-phase skeleton until PAYMENTS lands M5.
	ErrNotImplemented = errors.New("payment: not implemented")
)

// Limits of a match (LimitError.Limit).
const (
	LimitPayment = "payment"
	LimitCharge  = "charge"
)

// LimitError says which side of a match was exceeded (details.limit, details.charge_id).
type LimitError struct {
	Limit    string // payment, charge
	ChargeID string
}

func (e *LimitError) Error() string {
	if e.ChargeID != "" {
		return ErrMatchExceeds.Error() + ": " + e.Limit + " " + e.ChargeID
	}
	return ErrMatchExceeds.Error() + ": " + e.Limit
}

// Is makes errors.Is(err, ErrMatchExceeds) true.
func (e *LimitError) Is(target error) bool { return target == ErrMatchExceeds }
