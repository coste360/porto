package workorder

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until ORDERS lands the bodies (501).
	ErrNotImplemented = errors.New("workorder: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("workorder: version mismatch")
	// ErrInvalidTransition means the action is not allowed from the order, task or entry status (409
	// invalid_transition).
	ErrInvalidTransition = errors.New("workorder: invalid transition")
	// ErrClosed means the order does not accept the change in its state (409 work_order_closed).
	ErrClosed = errors.New("workorder: work order closed")
	// ErrHasCharges means the order has live charges and cannot be cancelled (409 work_order_has_charges).
	ErrHasCharges = errors.New("workorder: work order has charges")
	// ErrHasEntries means the task has time entries and cannot be deleted (409 conflict).
	ErrHasEntries = errors.New("workorder: task has time entries")
	// ErrDependencyViolation means the dependencies are cyclic or cross orders (422 dependency_violation).
	ErrDependencyViolation = errors.New("workorder: dependency violation")
	// ErrDurationTooLong means a time entry is longer than 16 hours (422 duration_too_long).
	ErrDurationTooLong = errors.New("workorder: duration too long")
	// ErrOfflineTimeOutOfRange means a device time is outside [queued_at - 48 h, queued_at + 5 min] (422
	// offline_time_out_of_range).
	ErrOfflineTimeOutOfRange = errors.New("workorder: offline time out of range")
	// ErrTimerAlreadyRunning means the employee has a running timer (409 timer_already_running; INV-95).
	ErrTimerAlreadyRunning = errors.New("workorder: timer already running")
	// ErrNoEmployee means the caller has no employee record for an own-entry operation (403 forbidden).
	ErrNoEmployee = errors.New("workorder: caller has no employee")
	// ErrReturnExceedsIssued means a return exceeds the net issued quantity of the item (422
	// return_exceeds_issued).
	ErrReturnExceedsIssued = errors.New("workorder: return exceeds issued")
	// ErrNothingToBill means the requested billing has nothing to charge (409 nothing_to_bill).
	ErrNothingToBill = errors.New("workorder: nothing to bill")
	// ErrNoPrice means a line or material has no catalogue price (422 no_price).
	ErrNoPrice = errors.New("workorder: no price")
)
