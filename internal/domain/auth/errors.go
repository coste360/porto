package auth

import (
	"errors"
	"fmt"
	"time"
)

// Errors returned by the auth package. internal/http/errmap.go maps them to problem types.
var (
	ErrInvalidCredentials    = errors.New("auth: invalid credentials")
	ErrTOTPRequired          = errors.New("auth: totp code required")
	ErrInvalidTOTP           = errors.New("auth: invalid totp code")
	ErrTOTPAlreadyEnabled    = errors.New("auth: totp already enabled")
	ErrTOTPNotEnrolled       = errors.New("auth: totp not enrolled")
	ErrTOTPRequiredByPolicy  = errors.New("auth: totp required by policy")
	ErrTOTPEnrolmentRequired = errors.New("auth: totp enrolment required")
	ErrUnauthenticated       = errors.New("auth: unauthenticated")
	ErrForbidden             = errors.New("auth: forbidden")
	ErrNotFound              = errors.New("auth: not found")
	ErrRateLimited           = errors.New("auth: rate limited")
	ErrWeakPassword          = errors.New("auth: weak password")
	ErrUserExists            = errors.New("auth: user exists")
	ErrInvalidResetToken     = errors.New("auth: invalid reset token")
	ErrInvalidInput          = errors.New("auth: invalid input")
)

// RateLimitError carries the wait before the next allowed attempt. It matches ErrRateLimited with errors.Is.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("auth: rate limited, retry after %s", e.RetryAfter)
}

// Is makes errors.Is(err, ErrRateLimited) true.
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// FieldError is one invalid input field. Code is a stable slug (required, too_long, invalid, …).
type FieldError struct {
	Field string
	Code  string
}

// ValidationError lists invalid fields. It matches ErrInvalidInput with errors.Is. Other domain packages use it
// for their own validation failures, so that the HTTP layer maps them uniformly.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("auth: invalid input (%d fields)", len(e.Fields))
}

// Is makes errors.Is(err, ErrInvalidInput) true.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

// Add appends a field error.
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldError{field, code})
}

// Err returns e when it holds at least one field, else nil.
func (e *ValidationError) Err() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}
