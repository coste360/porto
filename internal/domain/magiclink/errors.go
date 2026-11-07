package magiclink

import "errors"

// Sentinel errors (m4-plan §7.4; names fixed).
var (
	// ErrInvalid means the token is unknown, expired, revoked or of another marina: every case has the same
	// answer (INV-75). 404 link_invalid.
	ErrInvalid = errors.New("magiclink: invalid link")
)
