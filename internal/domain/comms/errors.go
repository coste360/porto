package comms

import "errors"

// Sentinel errors.
var (
	// ErrVersionMismatch means If-Match did not match the current version. 412 version_conflict.
	ErrVersionMismatch = errors.New("comms: version mismatch")
)
