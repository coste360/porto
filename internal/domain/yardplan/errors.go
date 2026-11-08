package yardplan

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until PLANNER lands the bodies (501).
	ErrNotImplemented = errors.New("yardplan: not implemented")
	// ErrVersionMismatch means If-Match (or a task version of the batch) did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("yardplan: version mismatch")
	// ErrCodeTaken means the employee code or user link is used (409 code_taken).
	ErrCodeTaken = errors.New("yardplan: code taken")
	// ErrQualificationMismatch means the employee does not hold the task qualification (422
	// qualification_mismatch, overridable with force_qualification).
	ErrQualificationMismatch = errors.New("yardplan: qualification mismatch")
	// ErrDependencyViolation means an assignment day is before the last assigned day of a predecessor (422
	// dependency_violation).
	ErrDependencyViolation = errors.New("yardplan: dependency violation")
	// ErrCapacityExceeded is the sentinel of CapacityError (409 capacity_exceeded).
	ErrCapacityExceeded = errors.New("yardplan: capacity exceeded")
	// ErrClosed means the task or its order no longer takes assignments (409 work_order_closed).
	ErrClosed = errors.New("yardplan: task or order closed")
)

// CapacityError is 409 capacity_exceeded {employee_id, date, capacity, assigned} (overridable with
// allow_overload); nothing was written.
type CapacityError struct {
	EmployeeID string
	Date       calendar.Date
	Capacity   qty.Milli
	Assigned   qty.Milli
}

func (e *CapacityError) Error() string { return "yardplan: capacity exceeded" }

// Unwrap lets errors.Is(err, ErrCapacityExceeded) match.
func (e *CapacityError) Unwrap() error { return ErrCapacityExceeded }
