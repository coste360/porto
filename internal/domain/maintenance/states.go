package maintenance

import "github.com/coste360/porto/internal/store"

// Action is a task state change.
type Action string

// Task actions (m9-plan §8.3).
const (
	ActionSchedule Action = "schedule"
	ActionStart    Action = "start"
	ActionComplete Action = "complete"
	ActionCancel   Action = "cancel"
)

// Transition is one row of the task state table.
type Transition struct {
	From   string
	Action Action
	To     string
}

// TaskTransitions is the task state table (m9-plan §8.3): open → scheduled → in_progress → done; open → in_progress;
// open, scheduled and in_progress → cancelled. done and cancelled are final (a follow-up is a new task).
var TaskTransitions = []Transition{
	{store.TaskOpen, ActionSchedule, store.TaskScheduled},
	{store.TaskScheduled, ActionSchedule, store.TaskScheduled}, // reschedule
	{store.TaskOpen, ActionStart, store.TaskInProgress},
	{store.TaskScheduled, ActionStart, store.TaskInProgress},
	{store.TaskInProgress, ActionComplete, store.TaskDone},
	{store.TaskOpen, ActionCancel, store.TaskCancelled},
	{store.TaskScheduled, ActionCancel, store.TaskCancelled},
	{store.TaskInProgress, ActionCancel, store.TaskCancelled},
}

// Next returns the target status of action from status, ErrInvalidTransition otherwise.
func Next(from string, action Action) (string, error) {
	for _, t := range TaskTransitions {
		if t.From == from && t.Action == action {
			return t.To, nil
		}
	}
	return "", ErrInvalidTransition
}
