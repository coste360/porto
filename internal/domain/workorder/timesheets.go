package workorder

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Time entries (m10-plan §7): timers and manual entries from the phone (offline kind yard.timesheet), approval by
// the yard manager (cost rate and cost frozen), void before the final billing, late entries after it. One running
// timer per employee is the DB unique timesheets_running_uidx (INV-95), never an application check.

func (s *Service) entryChanged(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t *store.Timesheet) error {
	return s.emit(ctx, tx, scope, event.TimesheetChanged, event.TimesheetChangedData{
		TimesheetID: t.ID, EmployeeID: t.EmployeeID, WorkOrderID: t.WorkOrderID, TaskID: t.TaskID, Status: t.Status,
	})
}

// entryEmployee resolves the employee of an entry operation: yard.manage may name anyone (default: the caller's
// employee); yard.timesheets works on the caller's employee only.
func (s *Service) entryEmployee(ctx context.Context, scope store.MarinaScope, perm auth.Permission, own *store.Employee, employeeID string) (*store.Employee, error) {
	if employeeID == "" || (own != nil && employeeID == own.ID) {
		if own == nil {
			return nil, invalid("employee_id", "required")
		}
		return own, nil
	}
	if perm != auth.PermYardManage {
		return nil, auth.ErrForbidden
	}
	e, err := store.WOEmployee(ctx, s.d.DB.DB, scope, employeeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, invalid("employee_id", "invalid")
		}
		return nil, err
	}
	return e, nil
}

// entryTarget resolves the order (and task) of an entry: the order must take work; after the final billing only
// yard.manage may record, and the entry is late (§10.3).
func (s *Service) entryTarget(ctx context.Context, tx bun.IDB, scope store.MarinaScope, perm auth.Permission, taskID, orderID string) (*store.WorkOrder, *store.WorkTask, bool, error) {
	var t *store.WorkTask
	if taskID != "" {
		var err error
		if t, err = store.GetWorkTask(ctx, tx, scope, taskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, false, invalid("task_id", "invalid")
			}
			return nil, nil, false, err
		}
		if orderID != "" && orderID != t.WorkOrderID {
			return nil, nil, false, invalid("work_order_id", "mismatch")
		}
		orderID = t.WorkOrderID
	}
	if orderID == "" {
		return nil, nil, false, invalid("work_order_id", "required")
	}
	o, err := store.LockWorkOrder(ctx, tx, scope, orderID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, false, invalid("work_order_id", "invalid")
		}
		return nil, nil, false, err
	}
	late := false
	switch {
	case Open(o.Status):
	case o.Status == store.WorkOrderInvoiced && perm == auth.PermYardManage:
		late = true
	default:
		return nil, nil, false, ErrClosed
	}
	return o, t, late, nil
}

// timerHours is round((ended − started) in whole minutes / 60, 2) (D24).
func timerHours(started, ended time.Time) (qty.Milli, error) {
	minutes := int64(ended.Sub(started) / time.Minute)
	h, err := qty.FromFraction(minutes, 60)
	if err != nil {
		return 0, err
	}
	return h.Round(2), nil
}

func (s *Service) entryView(ctx context.Context, db bun.IDB, scope store.MarinaScope, t *store.Timesheet, warnings []string) (*EntryView, error) {
	vs, err := s.entryViews(ctx, db, scope, []store.Timesheet{*t})
	if err != nil {
		return nil, err
	}
	vs[0].Warnings = warnings
	return &vs[0], nil
}

func (s *Service) entryViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Timesheet) ([]EntryView, error) {
	out := make([]EntryView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.WorkOrderID)
	}
	orders, err := store.WorkOrdersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	num := map[string]string{}
	for _, o := range orders {
		num[o.ID] = FormatNumber(o.Year, o.Number)
	}
	for _, r := range rows {
		out = append(out, EntryView{Row: r, OrderNumber: num[r.WorkOrderID]})
	}
	return out, nil
}

// replayEntry returns the entry recorded with client_ref (offline replay), nil when none. A client_ref of another
// employee is a reuse (422).
func (s *Service) replayEntry(ctx context.Context, scope store.MarinaScope, clientRef, employeeID string) (*store.Timesheet, error) {
	t, err := store.TimesheetByClientRef(ctx, s.d.DB.DB, scope, clientRef)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil //nolint:nilnil // no replay
	}
	if err != nil {
		return nil, err
	}
	if t.EmployeeID != employeeID {
		return nil, invalid("client_ref", "reused")
	}
	return t, nil
}

func checkClientRef(ref string) error {
	if ref == "" || len(ref) > maxClientRef {
		return invalid("client_ref", "invalid")
	}
	return nil
}

// openPredecessors reports whether a task has predecessors not done (warning dependencies_open).
func openPredecessors(ctx context.Context, db bun.IDB, scope store.MarinaScope, t *store.WorkTask) (bool, error) {
	if t == nil {
		return false, nil
	}
	deps, err := store.WorkTaskDepsOf(ctx, db, scope, []string{t.ID})
	if err != nil || len(deps) == 0 {
		return false, err
	}
	ids := make([]string, 0, len(deps))
	for _, d := range deps {
		ids = append(ids, d.DependsOnTaskID)
	}
	tasks, err := store.WorkTasksByIDs(ctx, db, scope, ids)
	if err != nil {
		return false, err
	}
	for _, x := range tasks {
		if x.Status != store.WorkTaskDone && x.Status != store.WorkTaskCancelled {
			return true, nil
		}
	}
	return false, nil
}

// StartTimer starts a timer on a task or an order (yard.timesheets own, yard.manage anyone; offline kind
// yard.timesheet). A replay with the same client_ref returns the first result; a second running timer of the
// employee is 409 timer_already_running (INV-95, DB unique).
func (s *Service) StartTimer(ctx context.Context, p *auth.Principal, marinaID string, in TimerStart) (*EntryView, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if err := checkClientRef(in.ClientRef); err != nil {
		return nil, err
	}
	e, err := s.entryEmployee(ctx, scope, perm, own, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if prev, err := s.replayEntry(ctx, scope, in.ClientRef, e.ID); err != nil || prev != nil {
		if err != nil {
			return nil, err
		}
		return s.entryView(ctx, s.d.DB.DB, scope, prev, nil)
	}
	started := s.d.Clock.Now()
	if in.StartedAt != nil {
		if err := s.checkDeviceTime(ctx, *in.StartedAt); err != nil {
			return nil, err
		}
		started = *in.StartedAt
	}
	var out store.Timesheet
	var warnings []string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, t, late, err := s.entryTarget(ctx, tx, scope, perm, in.TaskID, in.WorkOrderID)
		if err != nil {
			return err
		}
		if t != nil {
			if err := s.assignedTask(ctx, tx, scope, perm, e, t.ID); err != nil {
				return err
			}
			if open, err := openPredecessors(ctx, tx, scope, t); err != nil {
				return err
			} else if open {
				warnings = append(warnings, WarnDependenciesOpen)
			}
		}
		now := s.now()
		out = store.Timesheet{
			ID: store.NewID(), EmployeeID: e.ID, WorkOrderID: o.ID, WorkDate: calendar.In(started, location(ctx, tx, scope)),
			StartedAt: started, Source: store.TimesheetSourceTimer, Status: store.TimesheetRunning, Late: late,
			ClientRef: in.ClientRef, CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if t != nil {
			out.TaskID = t.ID
		}
		if err := store.InsertTimesheet(ctx, tx, scope, &out); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "timesheet.start", "timesheet", out.ID, map[string]any{
			"employee_id": e.ID, "work_order_id": o.ID, "task_id": out.TaskID, "late": late,
		}); err != nil {
			return err
		}
		return s.entryChanged(ctx, tx, scope, &out)
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			if prev, rerr := s.replayEntry(ctx, scope, in.ClientRef, e.ID); rerr == nil && prev != nil {
				return s.entryView(ctx, s.d.DB.DB, scope, prev, nil)
			}
			return nil, ErrTimerAlreadyRunning
		}
		return nil, err
	}
	return s.entryView(ctx, s.d.DB.DB, scope, &out, warnings)
}

// ownEntry checks that a yard.timesheets caller acts on an entry of their employee.
func ownEntry(perm auth.Permission, own *store.Employee, t *store.Timesheet) error {
	if perm == auth.PermYardManage {
		return nil
	}
	if own == nil || own.ID != t.EmployeeID {
		return auth.ErrForbidden
	}
	return nil
}

// StopTimer stops a running timer: hours rounded to 0.01 h, the entry is submitted (offline kind yard.timesheet,
// dependent on the start). A replay returns the stopped entry; longer than 16 h is 422 duration_too_long.
func (s *Service) StopTimer(ctx context.Context, p *auth.Principal, marinaID, id string, in TimerStop) (*EntryView, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if len(in.ClientRef) > maxClientRef {
		return nil, invalid("client_ref", "invalid")
	}
	if in.EndedAt != nil {
		if err := s.checkDeviceTime(ctx, *in.EndedAt); err != nil {
			return nil, err
		}
	}
	var out *store.Timesheet
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if err := ownEntry(perm, own, cur); err != nil {
			return err
		}
		if _, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID); err != nil {
			return notFound(err)
		}
		t, err := store.LockTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if t.Status != store.TimesheetRunning {
			// A replay of the stop returns the stopped entry.
			if t.Source == store.TimesheetSourceTimer && !t.EndedAt.IsZero() && (in.ClientRef == "" || in.ClientRef == t.StopClientRef) {
				out = t
				return nil
			}
			return ErrInvalidTransition
		}
		ended := s.d.Clock.Now()
		if in.EndedAt != nil {
			ended = *in.EndedAt
		}
		if ended.Before(t.StartedAt) {
			return invalid("ended_at", "before_start")
		}
		if ended.Sub(t.StartedAt) > MaxEntryHoursMilli*time.Hour/1000 {
			return ErrDurationTooLong
		}
		h, err := timerHours(t.StartedAt, ended)
		if err != nil {
			return err
		}
		t.EndedAt, t.HoursMilli, t.Status, t.StopClientRef = ended, int64(h), store.TimesheetSubmitted, in.ClientRef
		cols := []string{"ended_at", "hours_milli", "status", "stop_client_ref"}
		if in.Note != nil {
			note, ok := cleanText(*in.Note, maxNoteRunes)
			if !ok {
				return invalid("note", "too_long")
			}
			t.Note = note
			cols = append(cols, "note")
		}
		if err := store.UpdateTimesheet(ctx, tx, scope, t, t.Version, s.now(), cols...); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "timesheet.stop", "timesheet", t.ID, map[string]any{"hours_milli": t.HoursMilli}); err != nil {
			return err
		}
		out = t
		return s.entryChanged(ctx, tx, scope, t)
	})
	if err != nil {
		if store.IsUniqueViolation(err) && in.ClientRef != "" {
			if t, gerr := store.TimesheetByStopClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); gerr == nil && t.ID == id {
				return s.entryView(ctx, s.d.DB.DB, scope, t, nil)
			}
			return nil, invalid("client_ref", "reused")
		}
		return nil, err
	}
	return s.entryView(ctx, s.d.DB.DB, scope, out, nil)
}

// entryHours validates the hours of a manual entry: 0.25..16 h in 0.25 h steps, or started and ended (≤ 16 h,
// rounded as timers). It returns the hours and the work date (the local date of started_at when given).
func (s *Service) entryHours(ctx context.Context, db bun.IDB, scope store.MarinaScope, hours *qty.Milli, started, ended *time.Time, day calendar.Date) (qty.Milli, calendar.Date, error) {
	switch {
	case hours != nil && (started != nil || ended != nil):
		return 0, day, invalid("hours_milli", "exclusive")
	case hours != nil:
		h := *hours
		if h < ManualStepMilli || h > MaxEntryHoursMilli || h%ManualStepMilli != 0 {
			if h > MaxEntryHoursMilli {
				return 0, day, ErrDurationTooLong
			}
			return 0, day, invalid("hours_milli", "invalid")
		}
		if day.IsZero() {
			day = s.today(ctx, db, scope)
		}
		return h, day, nil
	case started != nil && ended != nil:
		if err := s.checkDeviceTime(ctx, *started); err != nil {
			return 0, day, err
		}
		if err := s.checkDeviceTime(ctx, *ended); err != nil {
			return 0, day, err
		}
		if !ended.After(*started) {
			return 0, day, invalid("ended_at", "before_start")
		}
		if ended.Sub(*started) > MaxEntryHoursMilli*time.Hour/1000 {
			return 0, day, ErrDurationTooLong
		}
		h, err := timerHours(*started, *ended)
		if err != nil {
			return 0, day, err
		}
		return h, calendar.In(*started, location(ctx, db, scope)), nil
	default:
		return 0, day, invalid("hours_milli", "required")
	}
}

// CreateEntry records a manual time entry (yard.timesheets own, yard.manage anyone; offline kind yard.timesheet,
// deduped by client_ref).
func (s *Service) CreateEntry(ctx context.Context, p *auth.Principal, marinaID string, in EntryInput) (*EntryView, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if err := checkClientRef(in.ClientRef); err != nil {
		return nil, err
	}
	e, err := s.entryEmployee(ctx, scope, perm, own, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if prev, err := s.replayEntry(ctx, scope, in.ClientRef, e.ID); err != nil || prev != nil {
		if err != nil {
			return nil, err
		}
		return s.entryView(ctx, s.d.DB.DB, scope, prev, nil)
	}
	note, ok := cleanText(in.Note, maxNoteRunes)
	if !ok {
		return nil, invalid("note", "too_long")
	}
	hours, day, err := s.entryHours(ctx, s.d.DB.DB, scope, in.Hours, in.StartedAt, in.EndedAt, in.WorkDate)
	if err != nil {
		return nil, err
	}
	if day.After(s.today(ctx, s.d.DB.DB, scope)) {
		return nil, invalid("work_date", "future")
	}
	var out store.Timesheet
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, t, late, err := s.entryTarget(ctx, tx, scope, perm, in.TaskID, in.WorkOrderID)
		if err != nil {
			return err
		}
		if t != nil {
			if err := s.assignedTask(ctx, tx, scope, perm, e, t.ID); err != nil {
				return err
			}
		}
		now := s.now()
		out = store.Timesheet{
			ID: store.NewID(), EmployeeID: e.ID, WorkOrderID: o.ID, WorkDate: day, HoursMilli: int64(hours),
			Source: store.TimesheetSourceManual, Status: store.TimesheetSubmitted, Note: note, Late: late, ClientRef: in.ClientRef,
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if in.StartedAt != nil {
			out.StartedAt, out.EndedAt = *in.StartedAt, *in.EndedAt
		}
		if t != nil {
			out.TaskID = t.ID
		}
		if err := store.InsertTimesheet(ctx, tx, scope, &out); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "timesheet.create", "timesheet", out.ID, map[string]any{
			"employee_id": e.ID, "work_order_id": o.ID, "task_id": out.TaskID, "hours_milli": out.HoursMilli, "late": late,
		}); err != nil {
			return err
		}
		return s.entryChanged(ctx, tx, scope, &out)
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			if prev, rerr := s.replayEntry(ctx, scope, in.ClientRef, e.ID); rerr == nil && prev != nil {
				return s.entryView(ctx, s.d.DB.DB, scope, prev, nil)
			}
		}
		return nil, err
	}
	return s.entryView(ctx, s.d.DB.DB, scope, &out, nil)
}

// UpdateEntry changes a submitted or rejected entry and resubmits it (owner with yard.timesheets, yard.manage;
// If-Match).
func (s *Service) UpdateEntry(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch EntryPatch) (*EntryView, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var out *store.Timesheet
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if err := ownEntry(perm, own, cur); err != nil {
			return err
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if t.Status != store.TimesheetSubmitted && t.Status != store.TimesheetRejected {
			return ErrInvalidTransition
		}
		if !Open(o.Status) && (o.Status != store.WorkOrderInvoiced || perm != auth.PermYardManage) {
			return ErrClosed
		}
		if patch.TaskID != nil {
			if *patch.TaskID == "" {
				t.TaskID = ""
			} else {
				task, err := store.GetWorkTask(ctx, tx, scope, *patch.TaskID)
				if err != nil || task.WorkOrderID != t.WorkOrderID {
					return invalid("task_id", "invalid")
				}
				t.TaskID = task.ID
			}
		}
		if patch.Note != nil {
			note, ok := cleanText(*patch.Note, maxNoteRunes)
			if !ok {
				return invalid("note", "too_long")
			}
			t.Note = note
		}
		if patch.Hours != nil || patch.StartedAt != nil || patch.EndedAt != nil || patch.WorkDate != nil {
			day := t.WorkDate
			if patch.WorkDate != nil {
				day = *patch.WorkDate
			}
			// Only the date changes: the hours stay; otherwise the new hours are validated.
			h, d := qty.Milli(t.HoursMilli), day
			if patch.Hours != nil || patch.StartedAt != nil || patch.EndedAt != nil {
				var err error
				if h, d, err = s.entryHours(ctx, tx, scope, patch.Hours, patch.StartedAt, patch.EndedAt, day); err != nil {
					return err
				}
			}
			t.HoursMilli, t.WorkDate = int64(h), d
			if patch.StartedAt != nil {
				t.StartedAt, t.EndedAt = *patch.StartedAt, *patch.EndedAt
			} else if patch.Hours != nil {
				t.StartedAt, t.EndedAt = time.Time{}, time.Time{}
			}
		}
		t.Status, t.RejectReason = store.TimesheetSubmitted, ""
		if err := store.UpdateTimesheet(ctx, tx, scope, t, int(version), s.now(), "task_id", "note", "hours_milli", "work_date",
			"started_at", "ended_at", "status", "reject_reason"); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "timesheet.update", "timesheet", t.ID, map[string]any{"hours_milli": t.HoursMilli}); err != nil {
			return err
		}
		out = t
		return s.entryChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.entryView(ctx, s.d.DB.DB, scope, out, nil)
}

// Approve approves submitted entries in batch (yard.manage): the cost rate of the employee and the cost
// (hours × rate, half-even) are frozen; the first approved entry starts a planned order; entries of an invoiced
// order are late. Orders are locked before entries, both in id order.
func (s *Service) Approve(ctx context.Context, p *auth.Principal, marinaID string, entries []EntryVersion) ([]EntryView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > 200 {
		return nil, invalid("entries", "invalid")
	}
	var done []store.Timesheet
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		want := map[string]int64{}
		var ids []string
		for _, e := range entries {
			if _, dup := want[e.ID]; dup {
				return invalid("entries", "duplicate")
			}
			want[e.ID] = e.Version
			ids = append(ids, e.ID)
		}
		slices.Sort(ids)
		var orderIDs []string
		for _, id := range ids {
			t, err := store.GetTimesheet(ctx, tx, scope, id)
			if err != nil {
				return notFound(err)
			}
			orderIDs = append(orderIDs, t.WorkOrderID)
		}
		slices.Sort(orderIDs)
		orderIDs = slices.Compact(orderIDs)
		orders := map[string]*store.WorkOrder{}
		for _, oid := range orderIDs {
			o, err := store.LockWorkOrder(ctx, tx, scope, oid)
			if err != nil {
				return notFound(err)
			}
			orders[oid] = o
		}
		now := s.now()
		for _, id := range ids {
			t, err := store.LockTimesheet(ctx, tx, scope, id)
			if err != nil {
				return notFound(err)
			}
			if int64(t.Version) != want[id] {
				return ErrVersionMismatch
			}
			if t.Status != store.TimesheetSubmitted {
				return ErrInvalidTransition
			}
			o := orders[t.WorkOrderID]
			if o.Status == store.WorkOrderCancelled {
				return ErrClosed
			}
			emp, err := store.WOEmployee(ctx, tx, scope, t.EmployeeID)
			if err != nil {
				return err
			}
			cost, err := qty.Milli(t.HoursMilli).MulCents(money.Cents(emp.HourlyCostCents))
			if err != nil {
				return err
			}
			rate, c := emp.HourlyCostCents, int64(cost)
			t.Status, t.CostRateCents, t.CostCents, t.ApprovedByUserID, t.ApprovedAt = store.TimesheetApproved, &rate, &c, p.UserID, now
			t.Late = t.Late || o.Status == store.WorkOrderInvoiced
			if err := store.UpdateTimesheet(ctx, tx, scope, t, t.Version, now, "status", "cost_rate_cents", "cost_cents",
				"approved_by_user_id", "approved_at", "late"); err != nil {
				return versionErr(err)
			}
			if err := s.audit(ctx, tx, scope, p.Actor(), "timesheet.approve", "timesheet", t.ID, map[string]any{
				"hours_milli": t.HoursMilli, "cost_cents": c, "late": t.Late,
			}); err != nil {
				return err
			}
			if err := s.entryChanged(ctx, tx, scope, t); err != nil {
				return err
			}
			if o.Status == store.WorkOrderPlanned {
				o.Status, o.StartedAt = store.WorkOrderInProgress, now
				if err := store.UpdateWorkOrder(ctx, tx, scope, o, o.Version, now, "status", "started_at"); err != nil {
					return err
				}
				if err := s.audit(ctx, tx, scope, p.Actor(), "work_order.start", "work_order", o.ID, map[string]any{
					"from": store.WorkOrderPlanned, "to": store.WorkOrderInProgress, "implicit": true,
				}); err != nil {
					return err
				}
				if err := s.statusChanged(ctx, tx, scope, o.ID, store.WorkOrderPlanned, store.WorkOrderInProgress); err != nil {
					return err
				}
			}
			done = append(done, *t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.entryViews(ctx, s.d.DB.DB, scope, done)
}

// Reject rejects a submitted entry with a reason (yard.manage, If-Match); the owner edits and resubmits it.
func (s *Service) Reject(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*EntryView, error) {
	reason, ok := cleanText(reason, maxReasonRunes)
	return s.entryTransition(ctx, p, marinaID, id, version, store.TimesheetSubmitted, func(t *store.Timesheet, _ *store.WorkOrder) ([]string, error) {
		if !ok || reason == "" {
			return nil, invalid("reason", "invalid")
		}
		t.Status, t.RejectReason = store.TimesheetRejected, reason
		return []string{"status", "reject_reason"}, nil
	}, "timesheet.reject")
}

// Void voids an approved entry while the order is not invoiced (yard.manage, If-Match, D26).
func (s *Service) Void(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*EntryView, error) {
	reason, ok := cleanText(reason, maxReasonRunes)
	return s.entryTransition(ctx, p, marinaID, id, version, store.TimesheetApproved, func(t *store.Timesheet, o *store.WorkOrder) ([]string, error) {
		if !ok {
			return nil, invalid("reason", "too_long")
		}
		if o.Status == store.WorkOrderInvoiced || o.Status == store.WorkOrderCancelled {
			return nil, ErrClosed
		}
		t.Status, t.VoidedAt, t.VoidedByUserID = store.TimesheetVoided, s.now(), p.UserID
		if reason != "" {
			t.Note = clip(reason, maxNoteRunes)
		}
		return []string{"status", "voided_at", "voided_by_user_id", "note"}, nil
	}, "timesheet.void")
}

func (s *Service) entryTransition(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, from string,
	apply func(*store.Timesheet, *store.WorkOrder) ([]string, error), action string,
) (*EntryView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var out *store.Timesheet
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, cur.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		t, err := store.LockTimesheet(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if t.Status != from {
			return ErrInvalidTransition
		}
		cols, err := apply(t, o)
		if err != nil {
			return err
		}
		if err := store.UpdateTimesheet(ctx, tx, scope, t, int(version), s.now(), cols...); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, "timesheet", t.ID, map[string]any{"status": t.Status}); err != nil {
			return err
		}
		out = t
		return s.entryChanged(ctx, tx, scope, t)
	})
	if err != nil {
		return nil, err
	}
	return s.entryView(ctx, s.d.DB.DB, scope, out, nil)
}

// ListEntries lists time entries, newest first (yard.manage and yard.read: all; yard.timesheets: own).
func (s *Service) ListEntries(ctx context.Context, p *auth.Principal, marinaID string, f EntryFilter) ([]EntryView, string, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID, auth.PermYardRead)
	if err != nil {
		return nil, "", err
	}
	sf := store.TimesheetFilter{EmployeeID: f.EmployeeID, WorkOrderID: f.WorkOrderID, Status: f.Status, From: f.From, To: f.To}
	if perm == auth.PermYardTimesheets {
		if f.EmployeeID != "" && f.EmployeeID != own.ID {
			return []EntryView{}, "", nil
		}
		sf.EmployeeID = own.ID
	}
	page, err := store.ListTimesheets(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.entryViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// MyWork returns the caller's employee assignments from 7 days ago to 7 days ahead and the running timer
// (yard.timesheets or yard.manage; a manager without an employee gets an empty view).
func (s *Service) MyWork(ctx context.Context, p *auth.Principal, marinaID string) (*MyWork, error) {
	scope, _, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	out := &MyWork{Today: today, Assignments: []MyAssignment{}}
	if own == nil {
		return out, nil
	}
	out.EmployeeID = own.ID
	rows, err := store.MyAssignments(ctx, db, scope, own.ID, today.AddDays(-myWorkDays), today.AddDays(myWorkDays))
	if err != nil {
		return nil, err
	}
	var allocIDs []string
	for _, r := range rows {
		if r.AllocationID != "" {
			allocIDs = append(allocIDs, r.AllocationID)
		}
	}
	labels, err := store.WOAllocationLabels(ctx, db, scope, allocIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		pos := labels[r.AllocationID].Code
		if pos == "" {
			pos = r.PositionNote
		}
		out.Assignments = append(out.Assignments, MyAssignment{
			Row: r, OrderNumber: FormatNumber(r.OrderYear, r.OrderNumber), Position: pos, Checklist: ChecklistOf(r.Checklist),
		})
	}
	if t, err := store.RunningTimer(ctx, db, scope, own.ID); err == nil {
		if out.RunningTimer, err = s.entryView(ctx, db, scope, t, nil); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return out, nil
}
