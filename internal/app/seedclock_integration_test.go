//go:build integration

package app_test

import (
	"time"

	"github.com/coste360/porto/internal/clock"
)

// seedTestNow is the moment the demo seed tests run at. The demo is built around "today" and the time of day (the
// yard plan of today starts from the current hour), so a test on the real clock passes or fails depending on when it
// runs. A fixed moment makes every run the same: Friday 2026-11-13 at 07:00 in Rome, early in the working day, the
// moment that once left the demo without hours to approve.
var seedTestNow = time.Date(2026, time.November, 13, 6, 0, 0, 0, time.UTC)

// seedTestClock is a fake clock at seedTestNow for the App of a seed test.
func seedTestClock() *clock.Fake { return clock.NewFake(seedTestNow) }
