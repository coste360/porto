package storage

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until STORAGE lands the bodies (501 not_implemented).
	ErrNotImplemented = errors.New("storage: not implemented")
	// ErrTargetInvalid means the target is not a surface area of a yard or shed map or a trailer spot
	// (422 storage_target_invalid).
	ErrTargetInvalid = errors.New("storage: target invalid")
	// ErrNoSpace means no free position exists in the area for the period (409 storage_no_space).
	ErrNoSpace = errors.New("storage: no space")
	// ErrExitsInvalid means a gate of the area is not usable or not on the border (422 area_exits_invalid).
	ErrExitsInvalid = errors.New("storage: area exits invalid")
	// ErrPinnedInvalid means a pinned item is not valid in its position (422 pinned_invalid).
	ErrPinnedInvalid = errors.New("storage: pinned item invalid")
	// ErrTooManyItems means more boats than one optimizer run accepts (422 too_many_items).
	ErrTooManyItems = errors.New("storage: too many items")
	// ErrWindowInvalid means a scenario window is empty, reversed or longer than MaxScenarioNights
	// (422 scenario_window_invalid).
	ErrWindowInvalid = errors.New("storage: scenario window invalid")
	// ErrScenarioNotDraft means the scenario is applied or discarded (409 scenario_not_draft).
	ErrScenarioNotDraft = errors.New("storage: scenario not draft")
	// ErrMovementNotPending means the movement is done or cancelled (409 movement_not_pending).
	ErrMovementNotPending = errors.New("storage: movement not pending")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("storage: version mismatch")
)

// ConflictsError is 409 scenario_conflicts: every conflict found under the locks; nothing was written.
type ConflictsError struct {
	Conflicts []Conflict
}

func (e *ConflictsError) Error() string { return "storage: scenario conflicts" }

// Conflict reasons (StorageScenarioConflict.reason).
const (
	ConflictStale         = "stale"
	ConflictResourceTaken = "resource_taken"
	ConflictFit           = "fit"
	ConflictUnplaced      = "unplaced"
	ConflictOverlap       = "overlap"
)
