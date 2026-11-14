// Package perfbudget checks the wall-clock performance budgets of the spec in tests.
//
// A duration measured on a shared machine depends on the machine, not only on the code: a budget that fails the
// ordinary test runs makes them flaky. So the ordinary runs (make test, make test-db, pull requests, main) never
// fail on a duration:
//
//   - Check, inside a test that also checks correctness: the duration is logged on every run and fails the test
//     only when PORTO_PERF_BUDGET=1;
//   - Require, at the top of a test that exists only to measure (budget or load tests): the test is skipped unless
//     PORTO_PERF_BUDGET=1.
//
// PORTO_PERF_BUDGET=1 is set by the Performance workflow (.github/workflows/perf.yml, nightly and manual, never a
// gate for merges) and by `make test-perf`.
package perfbudget

import (
	"os"
	"testing"
	"time"
)

// Env is the variable that turns the budgets into assertions.
const Env = "PORTO_PERF_BUDGET"

// Enforced reports whether the budgets fail the test (PORTO_PERF_BUDGET=1).
func Enforced() bool { return os.Getenv(Env) == "1" }

// Require skips a test that only measures wall time unless the budgets are enforced (PORTO_PERF_BUDGET=1).
func Require(t testing.TB) {
	t.Helper()
	if !Enforced() {
		t.Skip("wall-clock budget: runs with PORTO_PERF_BUDGET=1 (make test-perf, Performance workflow)")
	}
}

// Check logs took against limit and, when the budgets are enforced, fails the test (without stopping it) if took
// is over the limit.
func Check(t testing.TB, name string, took, limit time.Duration) {
	t.Helper()
	over := took > limit
	t.Logf("perf %s: %v (budget %v, over: %t)", name, took, limit, over)
	if over && Enforced() {
		t.Errorf("perf %s: took %v, budget %v", name, took, limit)
	}
}
