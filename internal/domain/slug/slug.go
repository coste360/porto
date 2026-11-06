// Package slug validates marina slugs (public URLs /b/{slug}, /api/public/v1/{slug}). It is a leaf package so
// that both marina and auth (owner bootstrap) can use it.
package slug

import (
	"errors"
	"regexp"
	"slices"
)

// ErrInvalid is returned for a malformed or reserved slug.
var ErrInvalid = errors.New("slug: invalid")

var pattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Reserved slugs collide with top-level routes.
var Reserved = []string{"admin", "api", "b", "static", "assets", "healthz", "readyz", "webhooks"}

// Validate checks s: lowercase letters, digits and single dashes between them, 3 to 63 characters, not reserved.
func Validate(s string) error {
	if len(s) < 3 || len(s) > 63 || !pattern.MatchString(s) || slices.Contains(Reserved, s) {
		return ErrInvalid
	}
	return nil
}
