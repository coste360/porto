package optimizer_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/optimizer/internal/fixture"
)

func witnessResult(f fixture.Fixture) optimizer.Result {
	r := optimizer.Result{Placements: make([]optimizer.ItemResult, len(f.Input.Items))}
	for i, it := range f.Input.Items {
		p := f.Witness[i]
		r.Placements[i] = optimizer.ItemResult{ID: it.ID, At: &p}
	}
	return r
}

func timelineOf(in optimizer.Input, r optimizer.Result) []optimizer.Access {
	items := make([]optimizer.TimelineItem, 0, len(in.Items))
	for i, it := range in.Items {
		if r.Placements[i].At == nil {
			continue
		}
		items = append(items, optimizer.TimelineItem{Placed: optimizer.Placed{
			ID: it.ID, LOA: it.LOA, Beam: it.Beam, At: *r.Placements[i].At, First: it.First, Last: it.Last, ExitOn: it.ExitOn,
		}, ArrivalOn: it.ArrivalOn})
	}
	return optimizer.AccessibilityTimeline(in.Grid, in.Fixed, items, in.From, in.To)
}

func blockedExits(acc []optimizer.Access) int {
	n := 0
	for _, a := range acc {
		if a.Event == optimizer.EventExit && !a.Accessible {
			n++
		}
	}
	return n
}

func TestFixture_WitnessValid(t *testing.T) {
	for seed := uint64(1); seed <= 10; seed++ {
		f := fixture.Generate(seed)
		w := witnessResult(f)
		if err := optimizer.Validate(f.Input, w); err != nil {
			t.Fatalf("seed %d: witness invalid: %v", seed, err)
		}
		if n := blockedExits(timelineOf(f.Input, w)); n != 0 {
			t.Fatalf("seed %d: witness has %d blocked exits", seed, n)
		}
		t.Logf("seed %d density %d bp", seed, f.Density())
	}
}

// TestArrange_Acceptance2000m2 is the M9 acceptance (m9-plan §6.4): in a 2,000 m² shed with 40 boats the
// optimizer produces in under 3 s a valid layout with no unplaced boat and no blocked exit.
func TestArrange_Acceptance2000m2(t *testing.T) {
	for seed := uint64(1); seed <= 10; seed++ {
		f := fixture.Generate(seed)
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		start := time.Now()
		res, err := optimizer.Arrange(ctx, f.Input)
		took := time.Since(start)
		cancel()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		t.Logf("seed %d: %v score %+v iterations %d stopped %s", seed, took, res.Score, res.Iterations, res.StoppedBy)
		if res.Score.Unplaced != 0 || res.Score.ExitsBlocked != 0 {
			t.Errorf("seed %d: score %+v", seed, res.Score)
		}
		if !raceEnabled && took >= 3*time.Second {
			t.Errorf("seed %d: took %v", seed, took)
		}
	}
}
