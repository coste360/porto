package presence

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors, mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrInvalidTransition means the presence event does not follow the vessel's state (e.g. an arrival of a
	// vessel already in port, m3-plan §13 D21). 409 invalid_transition.
	ErrInvalidTransition = errors.New("presence: invalid transition")
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound
)
