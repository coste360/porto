package deadline

import "errors"

// Sentinel errors (m1-plan §4.5), mapped to problem types in internal/http/respond/errmap.go.
var ErrVersionMismatch = errors.New("deadline: version mismatch")
