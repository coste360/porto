package sensorsim

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
)

// TestSim_ExpectedMatchesHysteresis folds every device's readings through the real state machine (N = 2, T = 5
// min) and checks that the changes are exactly the expected ones: no wave ever produces a change.
func TestSim_ExpectedMatchesHysteresis(t *testing.T) {
	const minutes = 120
	s := New(12, 200, Defaults(minutes))
	start := time.Date(2027, 6, 12, 8, 0, 0, 0, time.UTC)
	p := hysteresis.Params{Readings: 2, Window: 5 * time.Minute}
	got := map[[2]int]string{}
	waves := 0
	for d := range s.Devices() {
		st := hysteresis.Initial()
		for m := range minutes {
			if s.wave[d][m] {
				waves++
			}
			var ch *hysteresis.Change
			st, ch, _ = hysteresis.Step(st, hysteresis.Reading{ObservedAt: s.At(start, d, m), State: s.State(d, m)}, p)
			if ch != nil {
				if !ch.At.Equal(s.At(start, d, m-1)) {
					t.Fatalf("device %d minute %d: change at %v", d, m, ch.At)
				}
				got[[2]int{d, m}] = ch.To
			}
		}
	}
	exp := s.Expected(minutes)
	if len(exp) != len(got) || waves == 0 {
		t.Fatalf("expected %d changes, folded %d (waves %d)", len(exp), len(got), waves)
	}
	for _, e := range exp {
		if got[[2]int{e.Device, e.Completing}] != e.To {
			t.Fatalf("expected %+v missing", e)
		}
	}
	// Deterministic for a seed.
	s2 := New(12, 200, Defaults(minutes))
	for d := range 200 {
		for m := range minutes {
			if s.State(d, m) != s2.State(d, m) {
				t.Fatal("not deterministic")
			}
		}
	}
}
