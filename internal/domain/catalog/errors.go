package catalog

import (
	"errors"
	"fmt"
)

// Sentinel errors (m2-plan §4.6), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrInUse means the row is referenced (by items, rules, lists or map elements) and cannot be deleted; the
	// error is an *InUseError.
	ErrInUse = errors.New("catalog: in use")
	// ErrVersionMismatch means If-Match did not match the current version.
	ErrVersionMismatch = errors.New("catalog: version mismatch")
)

// InUseError tells how many references keep a row alive (in_use problem, details.references).
type InUseError struct {
	References int
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("catalog: in use (%d references)", e.References)
}

// Is makes errors.Is(err, ErrInUse) true.
func (e *InUseError) Is(target error) bool { return target == ErrInUse }
