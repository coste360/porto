package deadline

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// Maintenance task deadlines (M9, m9-plan §8.2, D26): every maintenance task with a due date has one deadline of
// kind maintenance_due (source maintenance_task), kept in step inside the task's transaction, so that it appears in
// the deadline list and the lead-day notifications. A live task is an open deadline, a done task closes it, a
// cancelled task dismisses it, a task without a due date has none. The engine reconciliation does not touch it.

// KindMaintenanceDue is the deadline kind of a maintenance task due date.
const KindMaintenanceDue = "maintenance_due"

// SourceMaintenanceTask is the deadline source type of a maintenance task.
const SourceMaintenanceTask = store.DeadlineSourceMaintenanceTask

func maintenanceStatus(taskStatus string) string {
	switch taskStatus {
	case store.TaskDone:
		return StatusDone
	case store.TaskCancelled:
		return StatusDismissed
	default:
		return StatusOpen
	}
}

// SyncMaintenanceTask upserts (or deletes) the deadline of a maintenance task in tx and returns its id ("" when
// the task has no due date).
func SyncMaintenanceTask(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t *store.MaintenanceTask, now time.Time) (string, error) {
	if t.DueOn.IsZero() {
		return "", store.DeleteDeadlinesOfSource(ctx, tx, scope, SourceMaintenanceTask, t.ID, "")
	}
	existing, err := store.DeadlinesOfSource(ctx, tx, scope, SourceMaintenanceTask, t.ID)
	if err != nil {
		return "", err
	}
	status := maintenanceStatus(t.Status)
	now = store.UTC(now)
	if len(existing) == 0 {
		row := &store.Deadline{
			ID: store.NewID(), MarinaID: scope.ID(), SourceType: SourceMaintenanceTask, SourceID: t.ID, Kind: KindMaintenanceDue,
			DueOn: t.DueOn, Status: status, NotifiedAt: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		return row.ID, store.InsertDeadline(ctx, tx, row)
	}
	row := &existing[0]
	if row.DueOn.Equal(t.DueOn) && row.Status == status {
		return row.ID, nil
	}
	// A manual dismissal of a live task stays unless the due date moves.
	if row.Status == StatusDismissed && status == StatusOpen && row.DueOn.Equal(t.DueOn) {
		return row.ID, nil
	}
	row.DueOn, row.Status, row.UpdatedAt = t.DueOn, status, now
	return row.ID, store.UpdateDeadline(ctx, tx, scope, row.Version, row)
}
