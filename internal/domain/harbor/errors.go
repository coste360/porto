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
)

// MapInvalidError carries the validation report of a refused map document (422 map_invalid with issues).
type MapInvalidError struct {
	Report layout.Report
}

func (e *MapInvalidError) Error() string { return ErrMapInvalid.Error() }

// Is makes errors.Is(err, ErrMapInvalid) true.
func (e *MapInvalidError) Is(target error) bool { return target == ErrMapInvalid }
