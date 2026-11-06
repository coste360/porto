package customer

import "errors"

// Sentinel errors (m1-plan §4.5), mapped to problem types in internal/http/respond/errmap.go.
var (
	ErrAnonymized      = errors.New("customer: anonymized")
	ErrMerged          = errors.New("customer: merged")
	ErrVersionMismatch = errors.New("customer: version mismatch")
)
