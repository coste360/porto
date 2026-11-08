package maintenance

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/store"
)

// TestTaskTransitions_Table: the state table is the one of m9-plan §8.3 (open → scheduled → in_progress → done;
// open → in_progress; open, scheduled, in_progress → cancelled): done only from in_progress.
func TestTaskTransitions_Table(t *testing.T) {
	cases := []struct {
		from   string
		action Action
		to     string
	}{
		{store.TaskOpen, ActionSchedule, store.TaskScheduled},
		{store.TaskScheduled, ActionStart, store.TaskInProgress},
		{store.TaskOpen, ActionStart, store.TaskInProgress},
		{store.TaskInProgress, ActionComplete, store.TaskDone},
		{store.TaskOpen, ActionComplete, ""},
		{store.TaskScheduled, ActionComplete, ""},
		{store.TaskDone, ActionStart, ""},
		{store.TaskCancelled, ActionComplete, ""},
		{store.TaskInProgress, ActionCancel, store.TaskCancelled},
	}
	for _, c := range cases {
		got, err := Next(c.from, c.action)
		if c.to == "" {
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s %s: want invalid, got %q %v", c.from, c.action, got, err)
			}
			continue
		}
		if err != nil || got != c.to {
			t.Errorf("%s %s: got %q %v, want %q", c.from, c.action, got, err, c.to)
		}
	}
}
