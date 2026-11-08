package maintenance

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/store"
)

// Task limits and enums (m9-plan §8.3).
const (
	maxTitleRunes = 200
	maxDescRunes  = 2000
	maxNotesRunes = 2000
	maxRefRunes   = 100
	maxReasonRune = 500
	worklistDays  = 7
)

var (
	priorities = []string{"low", "normal", "high", "urgent"}
	outcomes   = []string{"ok", "fixed", "needs_followup", "not_done"}
	statuses   = []string{store.TaskOpen, store.TaskScheduled, store.TaskInProgress, store.TaskDone, store.TaskCancelled}
)

func isLive(status string) bool { return contains(store.TaskLiveStatuses, status) }

// views builds the task views: files, overdue flag (live and due before today) and the element code.
func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.MaintenanceTask) ([]TaskView, error) {
	out := make([]TaskView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	today := s.today(ctx, db, scope)
	ids := make([]string, 0, len(rows))
	refs := map[string][]string{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		if r.ElementID != "" {
			refs[r.ElementType] = append(refs[r.ElementType], r.ElementID)
		}
	}
	files, err := store.MaintenanceTaskFiles(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byTask := map[string][]TaskFile{}
	for _, f := range files {
		byTask[f.TaskID] = append(byTask[f.TaskID], f)
	}
	infos, err := store.ElementInfos(ctx, db, scope, refs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		fs := byTask[r.ID]
		if fs == nil {
			fs = []TaskFile{}
		}
		out = append(out, TaskView{
			Row: r, Files: fs, Overdue: isLive(r.Status) && !r.DueOn.IsZero() && r.DueOn.Before(today),
			Code: infos[r.ElementType+":"+r.ElementID].Code,
		})
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*TaskView, error) {
	t, err := store.GetMaintenanceTask(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.views(ctx, db, scope, []store.MaintenanceTask{*t})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// ListTasks lists tasks, earliest due (overdue) first (maintenance.manage; maintenance.report sees the tasks
// assigned to the user or reported by the user).
func (s *Service) ListTasks(ctx context.Context, p *auth.Principal, marinaID string, f TaskFilter) ([]TaskView, string, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, "", err
	}
	v := &auth.ValidationError{}
	if f.Status != "" && !contains(statuses, f.Status) {
		v.Add("status", "invalid")
	}
	if f.Priority != "" && !contains(priorities, f.Priority) {
		v.Add("priority", "invalid")
	}
	if f.ElementType != "" && !contains(elementTypes, f.ElementType) {
		v.Add("element_type", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	sf := store.MaintenanceTaskFilter{
		Priority: f.Priority, ElementType: f.ElementType, ElementID: f.ElementID, PlanID: f.PlanID, AssigneeUserID: f.AssigneeUserID,
	}
	if f.Status != "" {
		sf.Statuses = []string{f.Status}
	}
	if f.OverdueOnly {
		sf.OverdueBefore = s.today(ctx, s.d.DB.DB, scope)
	}
	if perm == auth.PermMaintenanceReport {
		// Field roles see their own work only: their assignments (the worklist covers the rest).
		sf.AssigneeUserID = p.UserID
		if p.UserID == "" {
			return []TaskView{}, "", nil
		}
	}
	page, err := store.ListMaintenanceTasks(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.views(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// validateTaskText checks title, description and priority.
func validateTaskText(t *store.MaintenanceTask, v *auth.ValidationError) {
	var ok bool
	if t.Title, ok = cleanText(t.Title, maxTitleRunes); !ok || t.Title == "" {
		v.Add("title", "invalid")
	}
	if t.Description, ok = cleanText(t.Description, maxDescRunes); !ok {
		v.Add("description", "too_long")
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if !contains(priorities, t.Priority) {
		v.Add("priority", "invalid")
	}
	if (t.PositionXCm == nil) != (t.PositionYCm == nil) {
		v.Add("position_x_cm", "position_incomplete")
	}
	if t.PositionXCm != nil && t.MapID == "" {
		v.Add("map_id", "required")
	}
	if t.ElementID == "" && t.MapID == "" {
		v.Add("element", "required")
	}
}

// checkTarget verifies the element and the map of a task.
func checkTarget(ctx context.Context, db bun.IDB, scope store.MarinaScope, t *store.MaintenanceTask) error {
	if t.ElementID != "" {
		infos, err := checkElements(ctx, db, scope, []Element{{Type: t.ElementType, ID: t.ElementID}})
		if err != nil {
			return err
		}
		if t.MapID == "" {
			t.MapID = infos[t.ElementType+":"+t.ElementID].MapID
		}
	}
	if t.MapID != "" {
		ok, err := mapExists(ctx, db, scope, t.MapID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrElementInvalid
		}
	}
	return nil
}

// insertTask writes a new task with its deadline, audit and event in tx.
func (s *Service) insertTask(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, t *store.MaintenanceTask, action string) error {
	if err := store.InsertMaintenanceTask(ctx, tx, scope, t); err != nil {
		if store.IsForeignKeyViolation(err) {
			return ErrElementInvalid
		}
		return err
	}
	if _, err := deadline.SyncMaintenanceTask(ctx, tx, scope, t, s.now()); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, p.Actor(), action, "maintenance_task", t.ID,
		map[string]any{"source": t.Source, "element_type": t.ElementType, "priority": t.Priority}); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status)
}

// CreateTask creates a task from the office (maintenance.manage).
func (s *Service) CreateTask(ctx context.Context, p *auth.Principal, marinaID string, in TaskInput) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &store.MaintenanceTask{
		ID: store.NewID(), Source: store.TaskSourceOffice, Title: in.Title, Description: in.Description, Priority: in.Priority,
		Status: store.TaskOpen, OutOfService: in.OutOfService, AssigneeUserID: in.AssigneeUserID, FollowsTaskID: in.FollowsTaskID,
		MapID: in.MapID, PositionXCm: in.X, PositionYCm: in.Y, ReportedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
	}
	if in.Element != nil {
		t.ElementType, t.ElementID = in.Element.Type, in.Element.ID
	}
	if in.DueOn != nil {
		t.DueOn = *in.DueOn
	}
	v := &auth.ValidationError{}
	validateTaskText(t, v)
	if t.AssigneeUserID != "" && !store.ValidID(t.AssigneeUserID) {
		v.Add("assignee_user_id", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkTarget(ctx, tx, scope, t); err != nil {
			return err
		}
		if t.FollowsTaskID != "" {
			prev, err := store.GetMaintenanceTask(ctx, tx, scope, t.FollowsTaskID)
			if err != nil {
				return ErrElementInvalid
			}
			if t.ElementID == "" && prev.ElementID != "" {
				t.ElementType, t.ElementID = prev.ElementType, prev.ElementID
			}
			t.PlanID, t.Intervention = "", prev.Intervention
		}
		return s.insertTask(ctx, tx, scope, p, t, "maintenance_task.create")
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, t.ID)
}

// canWork reports whether the principal may work on the task: maintenance.manage, or maintenance.report on a task
// assigned to the user or to nobody.
func canWork(p *auth.Principal, scope store.MarinaScope, perm auth.Permission, t *store.MaintenanceTask) bool {
	if perm == auth.PermMaintenanceManage {
		return true
	}
	return p != nil && (t.AssigneeUserID == "" || t.AssigneeUserID == p.UserID)
}

// GetTask reads a task (maintenance.manage, or maintenance.report on the user's own or unassigned tasks).
func (s *Service) GetTask(ctx context.Context, p *auth.Principal, marinaID, id string) (*TaskView, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	v, err := s.view(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	if !canWork(p, scope, perm, &v.Row) && v.Row.ReportedByUserID != p.UserID {
		return nil, auth.ErrForbidden
	}
	return v, nil
}

// UpdateTask changes a task (maintenance.manage, If-Match). Done and cancelled tasks are final.
func (s *Service) UpdateTask(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch TaskPatch) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if !isLive(t.Status) {
			return ErrInvalidTransition
		}
		v := &auth.ValidationError{}
		if patch.Title != nil {
			t.Title = *patch.Title
		}
		if patch.Description != nil {
			t.Description = *patch.Description
		}
		if patch.Priority != nil {
			t.Priority = *patch.Priority
		}
		validateTaskText(t, v)
		if patch.ClearDue {
			t.DueOn = calendar.Date{}
		}
		if patch.DueOn != nil {
			t.DueOn = *patch.DueOn
		}
		if patch.ClearAssignee {
			t.AssigneeUserID = ""
		}
		if patch.AssigneeUserID != nil {
			t.AssigneeUserID = *patch.AssigneeUserID
			if !store.ValidID(t.AssigneeUserID) {
				v.Add("assignee_user_id", "invalid")
			}
		}
		if patch.SupplierRef != nil {
			ref, ok := cleanText(*patch.SupplierRef, maxRefRunes)
			if !ok {
				v.Add("supplier_ref", "too_long")
			}
			t.SupplierRef = ref
		}
		if patch.OutOfService != nil {
			t.OutOfService = *patch.OutOfService
		}
		if err := v.Err(); err != nil {
			return err
		}
		return s.saveTask(ctx, tx, scope, p, t, int(version), "maintenance_task.update", nil)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

// saveTask writes a changed task with its deadline, audit and event.
func (s *Service) saveTask(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, t *store.MaintenanceTask, expected int,
	action string, diff map[string]any,
) error {
	if err := store.UpdateMaintenanceTask(ctx, tx, scope, t, expected, s.now()); err != nil {
		if store.IsForeignKeyViolation(err) {
			return ErrElementInvalid
		}
		return versionErr(err)
	}
	if _, err := deadline.SyncMaintenanceTask(ctx, tx, scope, t, s.now()); err != nil {
		return err
	}
	if diff == nil {
		diff = map[string]any{}
	}
	diff["status"] = t.Status
	if err := s.audit(ctx, tx, scope, actorOf(p), action, "maintenance_task", t.ID, diff); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status)
}

// Start sets a task in progress (maintenance.manage, or maintenance.report on the user's own or unassigned tasks;
// If-Match).
func (s *Service) Start(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) (*TaskView, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !canWork(p, scope, perm, t) {
			return auth.ErrForbidden
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		next, err := Next(t.Status, ActionStart)
		if err != nil {
			return err
		}
		t.Status = next
		return s.saveTask(ctx, tx, scope, p, t, int(version), "maintenance_task.start", nil)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

// Complete records the outcome (offline kind maintenance.outcome: no If-Match, the state is re-checked). Done with
// the same ClientRef returns the task; done otherwise is ErrAlreadyDone. Maintenance nights are not released
// (INV-71): the office shortens them with Release.
func (s *Service) Complete(ctx context.Context, p *auth.Principal, marinaID, id string, in CompleteInput) (*TaskView, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !contains(outcomes, in.Outcome) {
		v.Add("outcome", "invalid")
	}
	notes, ok := cleanText(in.Notes, maxNotesRunes)
	if !ok {
		v.Add("notes", "too_long")
	}
	ref, ok := cleanText(in.SupplierRef, maxRefRunes)
	if !ok {
		v.Add("supplier_ref", "too_long")
	}
	if in.InternalMinutes != nil && (*in.InternalMinutes < 0 || *in.InternalMinutes > 1_000_000) {
		v.Add("internal_minutes", "invalid")
	}
	if in.ExternalCostCents != nil && *in.ExternalCostCents < 0 {
		v.Add("external_cost_cents", "invalid")
	}
	if len(in.ClientRef) > 100 {
		v.Add("client_ref", "too_long")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !canWork(p, scope, perm, t) {
			return auth.ErrForbidden
		}
		if t.Status == store.TaskDone {
			if in.ClientRef != "" && t.OutcomeClientRef == in.ClientRef {
				return errSameResult
			}
			return ErrAlreadyDone
		}
		// An outcome recorded on a task that was never started (the phone records it in one step, offline) passes
		// through in_progress: the state table stays open → in_progress → done (m9-plan §8.3).
		from, startedNow := t.Status, false
		if from != store.TaskInProgress {
			if from, err = Next(from, ActionStart); err != nil {
				return err
			}
			startedNow = true
		}
		next, err := Next(from, ActionComplete)
		if err != nil {
			return err
		}
		keys := map[string]bool{}
		for _, c := range ChecklistOf(t.Checklist) {
			keys[c.Key] = true
		}
		results := make([]ChecklistResult, 0, len(in.Checklist))
		seen := map[string]bool{}
		for _, r := range in.Checklist {
			if !keys[r.Key] || seen[r.Key] {
				v.Add("checklist_results", "invalid")
				continue
			}
			seen[r.Key] = true
			note, ok := cleanText(r.Note, maxLabelRunes)
			if !ok {
				v.Add("checklist_results", "invalid")
			}
			results = append(results, ChecklistResult{Key: r.Key, Done: r.Done, Note: note})
		}
		if err := v.Err(); err != nil {
			return err
		}
		version := t.Version
		t.Status, t.Outcome, t.OutcomeNotes, t.OutcomeClientRef = next, in.Outcome, notes, in.ClientRef
		t.ChecklistResults = resultsJSON(results)
		t.InternalMinutes, t.ExternalCostCents = in.InternalMinutes, in.ExternalCostCents
		if ref != "" {
			t.SupplierRef = ref
		}
		t.DoneAt, t.DoneByUserID = s.now(), p.UserID
		diff := map[string]any{"outcome": t.Outcome}
		if startedNow {
			diff["started"] = true
		}
		return s.saveTask(ctx, tx, scope, p, t, version, "maintenance_task.complete", diff)
	})
	if err != nil && !errors.Is(err, errSameResult) {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

var errSameResult = errors.New("maintenance: same result")

// Cancel cancels a task and releases its future maintenance nights (maintenance.manage, online, If-Match).
func (s *Service) Cancel(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, maxReasonRune)
	if !ok || reason == "" {
		v := &auth.ValidationError{}
		v.Add("reason", "invalid")
		return nil, v
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		next, err := Next(t.Status, ActionCancel)
		if err != nil {
			return err
		}
		today := s.today(ctx, tx, scope)
		if err := s.releaseFrom(ctx, tx, scope, t, today); err != nil {
			return err
		}
		t.Status, t.CancelReason = next, reason
		return s.saveTask(ctx, tx, scope, p, t, int(version), "maintenance_task.cancel", nil)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

// Report creates an open task from a field report (maintenance.report; offline kind maintenance.report: the same
// client_ref returns the stored task, INV-83). It never schedules and never occupies.
func (s *Service) Report(ctx context.Context, p *auth.Principal, marinaID string, in ReportInput) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &store.MaintenanceTask{
		ID: store.NewID(), Source: store.TaskSourceReport, Title: in.Title, Description: in.Description, Priority: in.Priority,
		Status: store.TaskOpen, MapID: in.MapID, PositionXCm: in.X, PositionYCm: in.Y, ReportedByUserID: p.UserID,
		ClientRef: in.ClientRef, CreatedAt: now, UpdatedAt: now,
	}
	if in.Element != nil {
		t.ElementType, t.ElementID = in.Element.Type, in.Element.ID
	}
	v := &auth.ValidationError{}
	validateTaskText(t, v)
	if in.ClientRef == "" || len(in.ClientRef) > 100 {
		v.Add("client_ref", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if prev, err := store.MaintenanceTaskByClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); err == nil {
		return s.view(ctx, s.d.DB.DB, scope, prev.ID)
	}
	id := t.ID
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkTarget(ctx, tx, scope, t); err != nil {
			return err
		}
		inserted, err := store.InsertMaintenanceTaskIfAbsent(ctx, tx, scope, t)
		if err != nil {
			if store.IsForeignKeyViolation(err) {
				return ErrElementInvalid
			}
			return err
		}
		if !inserted { // a concurrent replay committed first
			prev, err := store.MaintenanceTaskByClientRef(ctx, tx, scope, in.ClientRef)
			if err != nil {
				return err
			}
			id = prev.ID
			return nil
		}
		if _, err := deadline.SyncMaintenanceTask(ctx, tx, scope, t, s.now()); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "maintenance_task.report", "maintenance_task", t.ID,
			map[string]any{"source": t.Source, "element_type": t.ElementType, "priority": t.Priority}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

// Worklist returns the live tasks assigned to the user plus the live tasks due within 7 days (maintenance.report):
// the offline allowlist slot activities.assigned.
func (s *Service) Worklist(ctx context.Context, p *auth.Principal, marinaID string) ([]TaskView, calendar.Date, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceReport)
	if err != nil {
		return nil, calendar.Date{}, err
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	rows, err := store.MaintenanceWorklist(ctx, s.d.DB.DB, scope, p.UserID, today.AddDays(worklistDays))
	if err != nil {
		return nil, calendar.Date{}, err
	}
	items, err := s.views(ctx, s.d.DB.DB, scope, rows)
	return items, today, err
}

// Map returns the maintenance lens of a map on date (default today): per element the live tasks, the overdue ones
// and the out-of-service flag (maintenance.manage or maintenance.report). Without a map it covers every map.
func (s *Service) Map(ctx context.Context, p *auth.Principal, marinaID, mapID string, date *calendar.Date) (string, calendar.Date, []MapEntry, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return "", calendar.Date{}, nil, err
	}
	db := s.d.DB.DB
	if mapID != "" {
		ok, err := mapExists(ctx, db, scope, mapID)
		if err != nil {
			return "", calendar.Date{}, nil, err
		}
		if !ok {
			return "", calendar.Date{}, nil, auth.ErrNotFound
		}
	}
	day := s.today(ctx, db, scope)
	if date != nil {
		day = *date
	}
	tasks, err := store.LiveTasksWithElement(ctx, db, scope)
	if err != nil {
		return "", calendar.Date{}, nil, err
	}
	refs := map[string][]string{}
	for _, t := range tasks {
		refs[t.ElementType] = append(refs[t.ElementType], t.ElementID)
	}
	infos, err := store.ElementInfos(ctx, db, scope, refs)
	if err != nil {
		return "", calendar.Date{}, nil, err
	}
	by := map[string]*MapEntry{}
	var order []string
	for i := range tasks {
		t := &tasks[i]
		k := t.ElementType + ":" + t.ElementID
		info, ok := infos[k]
		if !ok || (mapID != "" && info.MapID != mapID) {
			continue
		}
		e := by[k]
		if e == nil {
			e = &MapEntry{ElementType: t.ElementType, ElementID: t.ElementID}
			by[k] = e
			order = append(order, k)
		}
		e.Open++
		if !t.DueOn.IsZero() && t.DueOn.Before(day) {
			e.Overdue++
		}
		if isOutOfService(t, day) {
			e.OutOfService = true
		}
	}
	out := make([]MapEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *by[k])
	}
	return mapID, day, out, nil
}
