package harbor

import (
	"errors"

	"github.com/coste360/porto/internal/domain/layout"
)

// Sentinel errors (m1-plan §4.5), mapped to problem types in internal/http/respond/errmap.go.
var (
	ErrMapInvalid       = errors.New("harbor: map invalid")
	ErrMapNotEmpty      = errors.New("harbor: map not empty")
	ErrNotGeoreferenced = errors.New("harbor: map not georeferenced")
	ErrVersionMismatch  = errors.New("harbor: version mismatch")
	ErrCSVInvalid       = errors.New("harbor: csv invalid")
	ErrInvalidFile      = errors.New("harbor: invalid file")
	ErrFileTooLarge     = errors.New("harbor: file too large")
	ErrUnsupportedMedia = errors.New("harbor: unsupported media type")
	// ErrElementInUse means a map save would delete an element that has occupancy rows, or change the cell grid
	// of an area with current or future allocations (M3, m3-plan §3.7). The error may be an *ElementInUseError.
	ErrElementInUse = errors.New("harbor: element in use")
)

// ElementInUseError names the elements a map save may not delete or reshape (409 in_use, details.references).
type ElementInUseError struct {
	References []string // element ids
}

func (e *ElementInUseError) Error() string { return ErrElementInUse.Error() }

// Is makes errors.Is(err, ErrElementInUse) true.
func (e *ElementInUseError) Is(target error) bool { return target == ErrElementInUse }

// MapInvalidError carries the validation report of a refused map document (422 map_invalid with issues).
type MapInvalidError struct {
	Report layout.Report
}

func (e *MapInvalidError) Error() string { return ErrMapInvalid.Error() }

// Is makes errors.Is(err, ErrMapInvalid) true.
func (e *MapInvalidError) Is(target error) bool { return target == ErrMapInvalid }
