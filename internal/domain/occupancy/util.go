package occupancy

import "github.com/coste360/porto/internal/domain/auth"

// invalid returns a validation error (400 validation_failed) on one field.
func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}
