package perfbudget_test

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/testutil/perfbudget"
)

type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Logf(string, ...any)   {}
func (r *recorder) Errorf(string, ...any) { r.failed = true }

func TestCheck_LogsOnlyByDefault(t *testing.T) {
	t.Setenv(perfbudget.Env, "")
	r := &recorder{TB: t}
	perfbudget.Check(r, "slow", 2*time.Second, time.Second)
	if r.failed {
		t.Fatal("a budget failed without PORTO_PERF_BUDGET=1")
	}
}

func TestCheck_FailsWhenEnforced(t *testing.T) {
	t.Setenv(perfbudget.Env, "1")
	r := &recorder{TB: t}
	perfbudget.Check(r, "fast", time.Millisecond, time.Second)
	if r.failed {
		t.Fatal("a budget within the limit failed")
	}
	perfbudget.Check(r, "slow", 2*time.Second, time.Second)
	if !r.failed {
		t.Fatal("an enforced budget over the limit passed")
	}
}
