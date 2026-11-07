// Package domainerr re-exports the store errors that reach the HTTP layer unchanged (paging cursors, rows that
// vanished between two reads), so that internal/http can map them without importing internal/store.
package domainerr

import "github.com/coste360/porto/internal/store"

// The values are the store sentinels themselves: errors.Is matches either name.
var (
	ErrNotFound        = store.ErrNotFound
	ErrConflict        = store.ErrConflict
	ErrVersionMismatch = store.ErrVersionMismatch
	ErrInvalidCursor   = store.ErrInvalidCursor
	// ErrImmutable is a price list immutability trigger error (INV-22, M2).
	ErrImmutable = store.ErrImmutable
)
