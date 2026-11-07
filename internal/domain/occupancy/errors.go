package occupancy

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/store"
)

// Sentinel errors (m3-plan §2.3; names fixed), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrResourceTaken means some rows of the plan are taken by another allocation (INV-01); the error is a
	// *ConflictError. 409 resource_taken.
	ErrResourceTaken = errors.New("occupancy: resource taken")
	// ErrFitBlocked means the vessel does not fit and no override was given; the error is a *FitError. 422.
	ErrFitBlocked = errors.New("occupancy: fit blocked")
	// ErrOverrideForbidden means a fit_override was given without occupancy.override_fit. 403.
	ErrOverrideForbidden = errors.New("occupancy: fit override forbidden")
	// ErrRepriceForbidden means an explicit reprice was asked for a move without contracts.write. 403.
	ErrRepriceForbidden = errors.New("occupancy: reprice forbidden")
	// ErrOverrideReasonRequired means a fit_override has an empty reason. 422 validation_failed.
	ErrOverrideReasonRequired = errors.New("occupancy: fit override reason required")
	// ErrPlacementInvalid means the placement is not valid in the area; the error is a *PlacementError. 422.
	ErrPlacementInvalid = errors.New("occupancy: placement invalid")
	// ErrClosed means a closure covers the arrival; the error is a *ClosureError. 409 closure_active.
	ErrClosed = errors.New("occupancy: closed for arrivals")
	// ErrHoldExpired means the hold is gone. 409 hold_expired.
	ErrHoldExpired = errors.New("occupancy: hold expired")
	// ErrAllocationNotActive means the allocation is cancelled, expired or superseded. 409 conflict.
	ErrAllocationNotActive = errors.New("occupancy: allocation not active")
	// ErrVersionMismatch means the expected version is stale. 412 version_conflict.
	ErrVersionMismatch = errors.New("occupancy: version mismatch")
	// ErrUndoUnavailable means the move cannot be undone (expired, superseded or conflicting). 409.
	ErrUndoUnavailable = errors.New("occupancy: undo unavailable")
	// ErrReleaseInvalid means the release is not allowed; the error may be a *ReleaseError. 409 release_invalid.
	ErrReleaseInvalid = errors.New("occupancy: release invalid")
	// ErrElementInUse is the harbour guard refusal (m3-plan §3.7); it is harbor.ErrElementInUse. 409 in_use.
	ErrElementInUse = harbor.ErrElementInUse
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound
)

// ConflictError names the rows that made an allocation fail.
type ConflictError struct {
	ResourceType  string
	ResourceID    string
	From, To      calendar.Date // inclusive nights in conflict
	Cells         []int         // dynamic areas only
	AllocationIDs []string      // the allocations holding the rows
	// Alternatives are search solutions for the taken nights, added by the operations that search them (revoke,
	// and the contract service for stay changes and quote acceptance).
	Alternatives []Solution
}

func (e *ConflictError) Error() string { return ErrResourceTaken.Error() }

// Is makes errors.Is(err, ErrResourceTaken) true.
func (e *ConflictError) Is(target error) bool { return target == ErrResourceTaken }

// FitError carries the fit result of a blocked allocation.
type FitError struct {
	Result fit.Result
}

func (e *FitError) Error() string { return ErrFitBlocked.Error() }

// Is makes errors.Is(err, ErrFitBlocked) true.
func (e *FitError) Is(target error) bool { return target == ErrFitBlocked }

// Placement error reasons.
const (
	PlacementUnsnapped     = "unsnapped"
	PlacementOutsideArea   = "outside_area"
	PlacementBlockedCell   = "blocked_cell"
	PlacementInvalidVessel = "invalid_vessel"
	PlacementRequired      = "placement_required"
	PlacementNotAllowed    = "placement_not_allowed"
)

// PlacementError tells why a placement is invalid.
type PlacementError struct {
	Reason       string // unsnapped, outside_area, blocked_cell, invalid_vessel, placement_required, placement_not_allowed
	AllocationID string // set by batch operations (layout save, swap)
	VesselID     string
}

func (e *PlacementError) Error() string { return ErrPlacementInvalid.Error() + ": " + e.Reason }

// Is makes errors.Is(err, ErrPlacementInvalid) true.
func (e *PlacementError) Is(target error) bool { return target == ErrPlacementInvalid }

// ClosureError names the closure that refused an arrival.
type ClosureError struct {
	ClosureID        string
	FromDate, ToDate calendar.Date
}

func (e *ClosureError) Error() string { return ErrClosed.Error() }

// Is makes errors.Is(err, ErrClosed) true.
func (e *ClosureError) Is(target error) bool { return target == ErrClosed }

// ReleaseError tells why a release or revoke is refused.
type ReleaseError struct {
	Reason string // not_contract, past_night, outside_allocation, overlapping, not_active
}

func (e *ReleaseError) Error() string { return ErrReleaseInvalid.Error() + ": " + e.Reason }

// Is makes errors.Is(err, ErrReleaseInvalid) true.
func (e *ReleaseError) Is(target error) bool { return target == ErrReleaseInvalid }
