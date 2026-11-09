package watch

import "errors"

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until RECONCILE lands the bodies (501).
	ErrNotImplemented = errors.New("sensor watch: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("sensor watch: version mismatch")
	// ErrAlertStale is a condition that no longer holds; the alert is resolved in the same call (409 alert_stale).
	ErrAlertStale = errors.New("sensor watch: alert stale")
	// ErrActionNotAllowed is an action not valid for the kind or the status of the alert (422
	// alert_action_not_allowed).
	ErrActionNotAllowed = errors.New("sensor watch: action not allowed")
	// ErrConsentRequired is a confirm_release without consent_confirmed (422 consent_required).
	ErrConsentRequired = errors.New("sensor watch: consent required")
	// ErrRangeTooLong is a history range over 31 days (422 validation_failed on to).
	ErrRangeTooLong = errors.New("sensor watch: range too long")
)
