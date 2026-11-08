package maintenance

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until UTILITIES lands the bodies (501).
	ErrNotImplemented = errors.New("maintenance: not implemented")
	// ErrInvalidTransition means the action is not allowed from the task status (409 invalid_transition).
	ErrInvalidTransition = errors.New("maintenance: invalid transition")
	// ErrElementInvalid means the element does not exist in the marina or does not suit the operation
	// (422 element_invalid).
	ErrElementInvalid = errors.New("maintenance: element invalid")
	// ErrAlreadyDone means the task was completed by someone else (409 task_already_done).
	ErrAlreadyDone = errors.New("maintenance: task already done")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("maintenance: version mismatch")
	// ErrCodeTaken means the equipment code is used (409 code_taken).
	ErrCodeTaken = errors.New("maintenance: code taken")
)

// ConflictsError is 409 maintenance_conflicts: the vessel allocations on the nights to book, each with up to three
// alternatives from occupancy.Search. Nothing was written.
type ConflictsError struct {
	Conflicts []Conflict
}

func (e *ConflictsError) Error() string { return "maintenance: occupancy conflicts" }
