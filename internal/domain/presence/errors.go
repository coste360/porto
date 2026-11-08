package presence

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors, mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrInvalidTransition means the presence event does not follow the vessel's state (e.g. an arrival of a
	// vessel already in port, m3-plan §13 D21). 409 invalid_transition.
	ErrInvalidTransition = errors.New("presence: invalid transition")
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound
	// ErrAllocationChanged: expected_resource_id no longer matches the allocation of the vessel for the night (moved,
	// cancelled, not active, M6b §5.2). Use *AllocationChangedError for the current berth. 409 allocation_changed.
	ErrAllocationChanged = errors.New("presence: allocation changed")
	// ErrOfflineNotAllowed: an offline replay (Porto-Offline-Queued-At) of an event that offline cannot record
	// (no allocation, or not arrived/departed, M6b §5.2). 422 offline_not_allowed.
	ErrOfflineNotAllowed = errors.New("presence: not allowed offline")
	// ErrIdempotencyMismatch: the presence key was already used with a different request (M6b domain dedupe).
	// 422 idempotency_key_reused.
	ErrIdempotencyMismatch = errors.New("presence: idempotency key reused with a different input")
)

// AllocationChangedError carries the current resource of the allocation (empty when the allocation was cancelled
// or is no longer active). It matches ErrAllocationChanged with errors.Is. Ids and codes only (INV-70).
type AllocationChangedError struct {
	CurrentResourceID   string
	CurrentResourceCode string
}

func (e *AllocationChangedError) Error() string { return ErrAllocationChanged.Error() }

// Is makes errors.Is(err, ErrAllocationChanged) true.
func (e *AllocationChangedError) Is(target error) bool { return target == ErrAllocationChanged }
