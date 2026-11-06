package layout

import (
	"errors"
	"fmt"
)

// ErrBadParams reports invalid generator parameters; the error wraps a *ValidationError with the fields.
var ErrBadParams = errors.New("layout: bad generator parameters")

// FieldError is one invalid parameter: field is the snake_case parameter name, code a stable reason.
type FieldError struct{ Field, Code string }

// ValidationError lists invalid generator parameters. errors.Is(err, ErrBadParams) is true.
type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("layout: bad generator parameters (%d fields)", len(e.Fields))
}

// Is makes errors.Is(err, ErrBadParams) true.
func (e *ValidationError) Is(target error) bool { return target == ErrBadParams }

// Add appends a field error.
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldError{field, code})
}

// Err returns e when it holds fields, else nil.
func (e *ValidationError) Err() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

func badParam(field, code string) error {
	return &ValidationError{Fields: []FieldError{{field, code}}}
}
