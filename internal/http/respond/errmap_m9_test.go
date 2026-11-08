package respond

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/storage"
	"github.com/coste360/porto/internal/domain/utility"
	"github.com/coste360/porto/internal/http/problem"
)

func TestMapError_M9Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		storage.ErrTargetInvalid:         problem.StorageTargetInvalid,
		storage.ErrNoSpace:               problem.StorageNoSpace,
		storage.ErrExitsInvalid:          problem.AreaExitsInvalid,
		optimizer.ErrGatesInvalid:        problem.AreaExitsInvalid,
		storage.ErrPinnedInvalid:         problem.PinnedInvalid,
		optimizer.ErrPinnedInvalid:       problem.PinnedInvalid,
		storage.ErrTooManyItems:          problem.TooManyItems,
		optimizer.ErrTooManyItems:        problem.TooManyItems,
		storage.ErrWindowInvalid:         problem.ScenarioWindowInvalid,
		storage.ErrScenarioNotDraft:      problem.ScenarioNotDraft,
		storage.ErrMovementNotPending:    problem.MovementNotPending,
		storage.ErrVersionMismatch:       problem.VersionConflict,
		maintenance.ErrInvalidTransition: problem.InvalidTransition,
		maintenance.ErrElementInvalid:    problem.ElementInvalid,
		maintenance.ErrAlreadyDone:       problem.TaskAlreadyDone,
		maintenance.ErrVersionMismatch:   problem.VersionConflict,
		maintenance.ErrCodeTaken:         problem.CodeTaken,
		utility.ErrReadingCharged:        problem.ReadingCharged,
		utility.ErrConsumptionFrozen:     problem.ConsumptionFrozen,
		utility.ErrCodeTaken:             problem.CodeTaken,
		utility.ErrElementInvalid:        problem.ElementInvalid,
		utility.ErrVersionMismatch:       problem.VersionConflict,
		utility.ErrInvalidTransition:     problem.InvalidTransition,
		storage.ErrNotImplemented:        problem.NotImplemented,
		maintenance.ErrNotImplemented:    problem.NotImplemented,
		utility.ErrNotImplemented:        problem.NotImplemented,
		contract.ErrNotImplementedM9:     problem.NotImplemented,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M9Details(t *testing.T) {
	from, to := calendar.MustParse("2026-10-01"), calendar.MustParse("2026-10-05")
	cases := []struct {
		err  error
		want problem.Type
		has  []string
	}{
		{&storage.ConflictsError{Conflicts: []storage.Conflict{
			{AllocationID: "a1", Reason: storage.ConflictStale},
			{AllocationID: "a2", Reason: storage.ConflictResourceTaken, Cells: []int{3, 4}, From: &from, To: &to, WithAllocationID: []string{"a9"}},
		}}, problem.ScenarioConflicts, []string{`"reason":"stale"`, `"cells":[3,4]`, `"nights":{"from":"2026-10-01","to":"2026-10-05"}`}},
		{&maintenance.ConflictsError{Conflicts: []maintenance.Conflict{
			{AllocationID: "a1", VesselID: "v1", ResourceType: "berth", ResourceID: "b1", From: from, To: to},
		}}, problem.MaintenanceConflicts, []string{`"vessel_id":"v1"`, `"alternatives":[]`, `"from":"2026-10-01"`}},
	}
	for _, c := range cases {
		typ, opts, ok := MapError(fmt.Errorf("x: %w", c.err))
		if !ok || typ != c.want || len(opts) != 1 {
			t.Fatalf("%v: %q %d %v, want %q with details", c.err, typ, len(opts), ok, c.want)
		}
		p := problem.New(typ, opts...)
		b, _ := json.Marshal(p.Details)
		for _, h := range c.has {
			if !strings.Contains(string(b), h) {
				t.Errorf("%s: details %s miss %s", c.want, b, h)
			}
		}
	}
}
