package booking

import (
	"errors"

	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/onlineconf"
)

// Sentinel errors (m8-plan §11; names fixed), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrLinkInvalid is every unknown, expired or foreign token or id of the public surfaces, and every public
	// operation of a marina with booking disabled (404 link_invalid, no enumeration).
	ErrLinkInvalid = errors.New("booking: link invalid")
	// ErrUnavailable means no solution of the category is left (409 booking_unavailable); the error is an
	// *UnavailableError carrying the categories still available.
	ErrUnavailable = errors.New("booking: unavailable")
	// ErrQuotaExhausted is internal: an online quota is used up; it is mapped to booking_unavailable.
	ErrQuotaExhausted = occupancy.ErrQuotaExhausted
	// ErrHoldExpired means the hold of the booking is gone (409 hold_expired).
	ErrHoldExpired = occupancy.ErrHoldExpired
	// ErrRules means an online rule refused the stay or the vessel (422 booking_rules); the error is a *RulesError.
	ErrRules = errors.New("booking: rules")
	// ErrBusy means the marina has max_live_holds live online holds (503 booking_busy with Retry-After).
	ErrBusy = errors.New("booking: busy")
	// ErrConsentRequired means terms or privacy were not accepted (422 consent_required).
	ErrConsentRequired = errors.New("booking: consent required")
	// ErrTextOutdated means a consent names a text version that is not the current one (409 text_outdated).
	ErrTextOutdated = onlineconf.ErrTextOutdated
	// ErrRefundChanged means the refund differs from expected_refund_cents (409 refund_changed).
	ErrRefundChanged = errors.New("booking: refund changed")
	// ErrPriceChanged means the new total differs from expected_total_cents (409 price_changed).
	ErrPriceChanged = errors.New("booking: price changed")
	// ErrCancellationNotAllowed means the transit cannot be cancelled online (status or arrival day reached)
	// (422 cancellation_not_allowed).
	ErrCancellationNotAllowed = errors.New("booking: cancellation not allowed")
	// ErrNothingDue means the residual of the transit is zero (422 nothing_due).
	ErrNothingDue = errors.New("booking: nothing due")
	// ErrIdempotencyMismatch means the Idempotency-Key was used with a different body (422 idempotency_mismatch).
	ErrIdempotencyMismatch = errors.New("booking: idempotency mismatch")
	// ErrProviderDisabled means no online provider is enabled and allow_without_provider is off (422
	// provider_disabled). Details or documents missing at /pay are a *RulesError (field details or documents).
	ErrProviderDisabled = collect.ErrProviderDisabled
	// ErrNotImplemented is returned by the CONTRACT skeleton until ENGINE lands the bodies.
	ErrNotImplemented = errors.New("booking: not implemented")
)

// UnavailableError carries the categories that still have availability for the same search.
type UnavailableError struct {
	AvailableCategories []string
}

func (e *UnavailableError) Error() string { return ErrUnavailable.Error() }

// Is makes errors.Is(err, ErrUnavailable) true.
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// RulesError names the rule that refused the request: Field is the input field (arrival, departure, loa_cm,
// documents, details, ...) and Code the rule (min_nights, max_nights, advance, weekday, closed, too_large,
// missing, ...).
type RulesError struct {
	Field string
	Code  string
}

func (e *RulesError) Error() string { return ErrRules.Error() + ": " + e.Field + " " + e.Code }

// Is makes errors.Is(err, ErrRules) true.
func (e *RulesError) Is(target error) bool { return target == ErrRules }
