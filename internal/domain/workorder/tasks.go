package workorder

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

var taskStatuses = []string{store.WorkTaskTodo, store.WorkTaskInProgress, store.WorkTaskDone, store.WorkTaskCancelled}

func (s *Service) taskChanged(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t *store.WorkTask) error {
	return s.emit(ctx, tx, scope, event.WorkTaskChanged, event.WorkTaskChangedData{
		WorkOrderID: t.WorkOrderID, TaskID: t.ID, Status: t.Status, ProgressBP: t.ProgressBP,
	})
}

// taskViews builds task views with predecessors and hours.
func (s *Service) taskViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.WorkTask) ([]TaskView, error) {
	out := make([]TaskView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	deps, err := store.WorkTaskDepsOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byTask := map[string][]string{}
	for _, d := range deps {
		byTask[d.TaskID] = append(byTask[d.TaskID], d.DependsOnTaskID)
	}
	hours, err := store.WorkTaskHours(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		d := byTask[r.ID]
		if d == nil {
			d = []string{}
		}
		out = append(out, TaskView{
			Row: r, DependsOn: d, HoursApproved: qty.Milli(hours[r.ID].Approved), HoursAssigned: qty.Milli(hours[r.ID].Assigned),
		})
	}
	return out, nil
}

func (s *Service) taskView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*TaskView, error) {
	t, err := store.GetWorkTask(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.taskViews(ctx, db, scope, []store.WorkTask{*t})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// workerScope authorises an operation of a field worker (yard.manage, or yard.timesheets with the caller's employee).
// The employee is nil for yard.manage callers without one.
func (s *Service) workerScope(ctx context.Context, p *auth.Principal, marinaID string, extra ...auth.Permission) (store.MarinaScope, auth.Permission, *store.Employee, error) {
	perms := append([]auth.Permission{auth.PermYardManage}, extra...)
	perms = append(perms, auth.PermYardTimesheets)
	scope, perm, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, perms...)
	if err != nil {
		return scope, perm, nil, err
	}
	e, err := store.WOEmployeeByUser(ctx, s.d.DB.DB, scope, p.UserID)
	switch {
	case err == nil:
		return scope, perm, e, nil
	case !errors.Is(err, store.ErrNotFound):
		return scope, perm, nil, err
	}
	if perm == auth.PermYardTimesheets {
		return scope, perm, nil, ErrNoEmployee
	}
	return scope, perm, nil, nil
}

// assignedTask checks that a field worker has an assignment on the task (otherwise the task is not theirs: 403).
func (s *Service) assignedTask(ctx context.Context, db bun.IDB, scope store.MarinaScope, perm auth.Permission, e *store.Employee, taskID string) error {
	if perm != auth.PermYardTimesheets {
		return nil
	}
	ok, err := store.WorkTaskAssignedTo(ctx, db, scope, taskID, e.ID)
	if err != nil {
		return err
	}
	if !ok {
		return auth.ErrForbidden
	}
	return nil
}

// ListTasks lists the tasks of an order (yard.read; yard.timesheets sees the tasks assigned to the caller).
func (s *Service) ListTasks(ctx context.Context, p *auth.Principal, marinaID, orderID string) ([]TaskView, error) {
	scope, perm, e, err := s.workerScope(ctx, p, marinaID, auth.PermYardRead)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if _, err := store.GetWorkOrder(ctx, db, scope, orderID); err != nil {
		return nil, notFound(err)
	}
	rows, err := store.WorkTasksOfOrders(ctx, db, scope, []string{orderID})
	if err != nil {
		return nil, err
	}
	if perm == auth.PermYardTimesheets {
		kept := rows[:0]
		for _, r := range rows {
			if ok, err := store.WorkTaskAssignedTo(ctx, db, scope, r.ID, e.ID); err != nil {
				return nil, err
			} else if ok {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	return s.taskViews(ctx, db, scope, rows)
}

func validateChecklist(list []ChecklistItem) ([]ChecklistItem, error) {
	if len(list) > maxChecklist {
		return nil, invalid("checklist", "too_many")
	}
	seen := map[string]bool{}
	out := make([]ChecklistItem, 0, len(list))
	for i, c := range list {
		label, ok := cleanText(c.Label, maxLabelRunes)
		if !ok || label == "" {
			return nil, invalid("checklist["+itoa(i)+"].label", "invalid")
		}
		key := c.Key
		if key == "" {
			key = checkKey(i)
		}
		if len(key) > 40 || seen[key] {
			return nil, invalid("checklist["+itoa(i)+"].key", "invalid")
		}
		seen[key] = true
		out = append(out, ChecklistItem{Key: key, Label: label, Done: c.Done})
	}
	return out, nil
}

// checkDeps validates the predecessors of a task: tasks of the same order, not itself, and no cycle in the order's
// dependency graph with the new edges (checked under the order lock).
func checkDeps(taskID string, dependsOn []string, tasks []store.WorkTask, deps []store.WorkTaskDep) error {
	inOrder := map[string]bool{}
	for _, t := range tasks {
		inOrder[t.ID] = true
	}
	graph := map[string][]string{}
	for _, d := range deps {
		if d.TaskID != taskID {
			graph[d.TaskID] = append(graph[d.TaskID], d.DependsOnTaskID)
		}
	}
	seen := map[string]bool{}
	for _, d := range dependsOn {
		if d == taskID || !inOrder[d] || seen[d] {
			return ErrDependencyViolation
		}
		seen[d] = true
		graph[taskID] = append(graph[taskID], d)
	}
	// DFS from taskID: reaching taskID again is a cycle.
	visited := map[string]bool{}
	var walk func(n string) bool
	walk = func(n string) bool {
		for _, m := range graph[n] {
			if m == taskID {
				return true
			}
			if !visited[m] {
				visited[m] = true
				if walk(m) {
					return true
				}
			}
		}
		return false
	}
	if walk(taskID) {
		return ErrDependencyViolation
	}
	return nil
}

// CreateTask adds a task to an open order (yard.manage).
func (s *Service) CreateTask(ctx context.Context, p *auth.Principal, marinaID, orderID string, in TaskInput) (*TaskView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	title, ok := cleanText(in.Title, maxTitleRunes)
	if !ok || title == "" {
		return nil, invalid("title", "invalid")
	}
	if in.EstimatedHours < 0 {
		return nil, invalid("estimated_hours_milli", "negative")
	}
	items := make([]ChecklistItem, 0, len(in.Checklist))
	for _, l := range in.Checklist {
		items = append(items, ChecklistItem{Label: l})
	}
	cl, err := validateChecklist(items)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.LockWorkOrder(ctx, tx, scope, orderID)
		if err != nil {
			return notFound(err)
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		if err := s.checkTaskRefs(ctx, tx, scope, in.QualificationID, in.WorkTypeID); err != nil {
			return err
		}
		tasks, err := store.WorkTasksOfOrders(ctx, tx, scope, []string{o.ID})
		if err != nil {
			return err
		}
		seq := 0
		for _, t := range tasks {
			seq = max(seq, t.Seq)
		}
		now := s.now()
		b, _ := json.Marshal(cl)
		t := &store.WorkTask{
			ID: store.NewID(), WorkOrderID: o.ID, Seq: seq + 1, Title: title, QualificationID: in.QualificationID,
			WorkTypeID: in.WorkTypeID, EstimatedHoursMilli: int64(in.EstimatedHours), Status: store.WorkTaskTodo, Checklist: b,
			CreatedAt: now, UpdatedAt: now,
		}
		if len(in.DependsOn) > 0 {
			deps, err := store.WorkTaskDepsOfOrder(ctx, tx, scope, o.ID)
			if err != nil {
				return err
			}
			if err := checkDeps(t.ID, in.DependsOn, append(tasks, *t), deps); err != nil {
				return err
			}
		}
		if err := store.InsertWorkTask(ctx, tx, scope, t); err != nil {
			return err
		}
		if err := store.ReplaceWorkTaskDeps(ctx, tx, scope, t.ID, in.DependsOn); err != nil {
			return err
		}
		id = t.ID
		if err := s.recomputeCompletion(ctx, tx, scope, o.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.create", "work_task", t.ID, map[string]any{
			"work_order_id": o.ID, "estimated_hours_milli": t.EstimatedHoursMilli, "depends_on": len(in.DependsOn),
		}); err != nil {
			return err
		}
		return s.taskChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.taskView(ctx, s.d.DB.DB, scope, id)
}

func (s *Service) checkTaskRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, qualificationID, workTypeID string) error {
	if ok, err := store.WOQualificationsExist(ctx, db, scope, []string{qualificationID}); err != nil {
		return err
	} else if !ok {
		return invalid("qualification_id", "invalid")
	}
	if ok, err := store.WOWorkTypesExist(ctx, db, scope, []string{workTypeID}); err != nil {
		return err
	} else if !ok {
		return invalid("work_type_id", "invalid")
	}
	return nil
}

// setStatus applies a task status with the progress rule (done ⇔ 10000).
func setStatus(t *store.WorkTask, status string) {
	t.Status = status
	switch status {
	case store.WorkTaskDone:
		t.ProgressBP = 10000
	case store.WorkTaskTodo:
		t.ProgressBP = 0
	case store.WorkTaskInProgress:
		t.ProgressBP = min(t.ProgressBP, 9999)
	}
}

// UpdateTask changes a task (yard.manage, If-Match).
func (s *Service) UpdateTask(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch TaskPatch) (*TaskView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		if patch.Title != nil {
			title, ok := cleanText(*patch.Title, maxTitleRunes)
			if !ok || title == "" {
				return invalid("title", "invalid")
			}
			t.Title = title
		}
		if patch.QualificationID != nil {
			t.QualificationID = *patch.QualificationID
		}
		if patch.WorkTypeID != nil {
			t.WorkTypeID = *patch.WorkTypeID
		}
		if err := s.checkTaskRefs(ctx, tx, scope, t.QualificationID, t.WorkTypeID); err != nil {
			return err
		}
		if patch.EstimatedHours != nil {
			if *patch.EstimatedHours < 0 {
				return invalid("estimated_hours_milli", "negative")
			}
			t.EstimatedHoursMilli = int64(*patch.EstimatedHours)
		}
		if patch.Checklist != nil {
			cl, err := validateChecklist(*patch.Checklist)
			if err != nil {
				return err
			}
			t.Checklist, _ = json.Marshal(cl)
		}
		if patch.Status != nil {
			if !slices.Contains(taskStatuses, *patch.Status) {
				return invalid("status", "invalid")
			}
			setStatus(t, *patch.Status)
		}
		if err := store.UpdateWorkTask(ctx, tx, scope, t, int(version), s.now(), "title", "qualification_id", "work_type_id",
			"estimated_hours_milli", "checklist", "status", "progress_bp"); err != nil {
			return versionErr(err)
		}
		if err := s.recomputeCompletion(ctx, tx, scope, o.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.update", "work_task", t.ID, map[string]any{
			"status": t.Status, "progress_bp": t.ProgressBP, "estimated_hours_milli": t.EstimatedHoursMilli,
		}); err != nil {
			return err
		}
		return s.taskChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.taskView(ctx, s.d.DB.DB, scope, id)
}

// DeleteTask deletes a task without time entries or material movements (yard.manage, If-Match).
func (s *Service) DeleteTask(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) error {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		has, err := store.WorkTaskHasEntries(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if has {
			return ErrHasEntries
		}
		if err := store.DeleteWorkTask(ctx, tx, scope, id, int(version)); err != nil {
			return versionErr(err)
		}
		if err := s.recomputeCompletion(ctx, tx, scope, o.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.delete", "work_task", id, map[string]any{"work_order_id": o.ID}); err != nil {
			return err
		}
		t.Status = store.WorkTaskCancelled
		return s.taskChanged(ctx, tx, scope, t)
	})
}

// SetDependencies replaces the finish-to-start predecessors of a task (yard.manage, If-Match on the task).
func (s *Service) SetDependencies(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, dependsOn []string) (*TaskView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		tasks, err := store.WorkTasksOfOrders(ctx, tx, scope, []string{o.ID})
		if err != nil {
			return err
		}
		deps, err := store.WorkTaskDepsOfOrder(ctx, tx, scope, o.ID)
		if err != nil {
			return err
		}
		if err := checkDeps(t.ID, dependsOn, tasks, deps); err != nil {
			return err
		}
		if err := store.ReplaceWorkTaskDeps(ctx, tx, scope, t.ID, dependsOn); err != nil {
			return err
		}
		if err := store.UpdateWorkTask(ctx, tx, scope, t, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.dependencies", "work_task", t.ID, map[string]any{"depends_on": dependsOn}); err != nil {
			return err
		}
		return s.taskChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.taskView(ctx, s.d.DB.DB, scope, id)
}

// checkDeviceTime checks a device time of an offline replay: queued_at − 48 h ≤ t ≤ queued_at + 5 min (D25).
// Online, a device time may not be in the future beyond 5 minutes.
func (s *Service) checkDeviceTime(ctx context.Context, t time.Time) error {
	if t.IsZero() {
		return nil
	}
	ref := s.d.Clock.Now()
	from := time.Time{}
	if q, ok := reqctx.OfflineReplay(ctx); ok {
		ref, from = q, q.Add(-OfflineBefore)
	}
	if t.After(ref.Add(OfflineAfter)) || (!from.IsZero() && t.Before(from)) {
		return ErrOfflineTimeOutOfRange
	}
	return nil
}

// UpdateProgress records progress, checklist and completion of a task (yard.manage, or yard.timesheets on an
// assigned task; offline kind yard.activity_done). A replay never decreases the progress (D25).
func (s *Service) UpdateProgress(ctx context.Context, p *auth.Principal, marinaID, id string, in ProgressInput) (*TaskView, error) {
	scope, perm, e, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetWorkTask(ctx, s.d.DB.DB, scope, id); err != nil {
		return nil, notFound(err)
	}
	if in.ClientRef == "" || len(in.ClientRef) > maxClientRef {
		return nil, invalid("client_ref", "invalid")
	}
	if in.ProgressBP != nil && (*in.ProgressBP < 0 || *in.ProgressBP > 10000) {
		return nil, invalid("progress_bp", "invalid")
	}
	if in.RecordedAt != nil {
		if err := s.checkDeviceTime(ctx, *in.RecordedAt); err != nil {
			return nil, err
		}
	}
	_, replay := reqctx.OfflineReplay(ctx)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if err := s.assignedTask(ctx, tx, scope, perm, e, id); err != nil {
			return err
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockWorkTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		if t.Status == store.WorkTaskCancelled {
			return ErrInvalidTransition
		}
		before := t.ProgressBP
		if len(in.Checklist) > 0 {
			cl := ChecklistOf(t.Checklist)
			for _, m := range in.Checklist {
				found := false
				for i := range cl {
					if cl[i].Key == m.Key {
						cl[i].Done, found = m.Done, true
					}
				}
				if !found {
					return invalid("checklist.key", "invalid")
				}
			}
			t.Checklist, _ = json.Marshal(cl)
		}
		if in.ProgressBP != nil {
			np := int(*in.ProgressBP)
			if replay || perm == auth.PermYardTimesheets {
				np = max(np, t.ProgressBP)
			}
			t.ProgressBP = np
		}
		if in.Done != nil && *in.Done {
			t.ProgressBP = 10000
		}
		switch {
		case t.ProgressBP == 10000:
			t.Status = store.WorkTaskDone
		case t.ProgressBP > 0:
			t.Status = store.WorkTaskInProgress
		case t.Status == store.WorkTaskDone:
			t.Status = store.WorkTaskInProgress
		}
		if err := store.UpdateWorkTask(ctx, tx, scope, t, t.Version, s.now(), "checklist", "progress_bp", "status"); err != nil {
			return versionErr(err)
		}
		if err := s.recomputeCompletion(ctx, tx, scope, o.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.progress", "work_task", t.ID, map[string]any{
			"from_bp": before, "progress_bp": t.ProgressBP, "status": t.Status, "offline": replay,
		}); err != nil {
			return err
		}
		return s.taskChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.taskView(ctx, s.d.DB.DB, scope, id)
}
