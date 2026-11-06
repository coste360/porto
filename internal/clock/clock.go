// Package clock provides the injectable time source used by every Porto package.
//
// Domain code never calls time.Now directly (enforced by forbidigo): it receives a Clock.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current instant, always in UTC.
type Clock interface {
	Now() time.Time
}

// System is the real wall clock.
type System struct{}

// Now returns the current instant in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fake is a manually driven clock for tests. It is safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake clock set to t (converted to UTC).
func NewFake(t time.Time) *Fake { return &Fake{now: t.UTC()} }

// Now returns the fake current instant.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock forward by d (backwards if d is negative).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the clock to t (converted to UTC).
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

var (
	_ Clock = System{}
	_ Clock = (*Fake)(nil)
)
