package maintenance

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// RunSchedule is the hourly maintenance.schedule job (m9-plan §8.2): for each active plan target, a task when
// today ≥ due − lead_days, no task of the target is still to do and none exists for (plan, element, due_on)
// (unique index), with audit, maintenance.due and a deadlines row. Idempotent.
func (s *Service) RunSchedule(ctx context.Context) error {
	ids, err := store.ActiveMarinaIDs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if _, err := s.ScheduleMarina(ctx, scope); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ScheduleMarina generates the due tasks of one marina and returns how many it created. The caller has authorised
// scope (the job runs as the system).
func (s *Service) ScheduleMarina(ctx context.Context, scope store.MarinaScope) (int, error) {
	db := s.d.DB.DB
	loc := location(ctx, db, scope)
	today := calendar.In(s.d.Clock.Now(), loc)
	plans, err := store.ActiveMaintenancePlans(ctx, db, scope)
	if err != nil {
		return 0, err
	}
	created := 0
	for i := range plans {
		plan := &plans[i]
		n := 0
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			n = 0
			targets, err := store.MaintenancePlanTargets(ctx, tx, scope, []string{plan.ID})
			if err != nil {
				return err
			}
			refs := map[string][]string{}
			for _, t := range targets {
				refs[t.ElementType] = append(refs[t.ElementType], t.ElementID)
			}
			infos, err := store.ElementInfos(ctx, tx, scope, refs)
			if err != nil {
				return err
			}
			for _, t := range targets {
				info, ok := infos[t.ElementType+":"+t.ElementID]
				if !ok {
					continue // the element was removed from the map
				}
				made, err := s.scheduleTarget(ctx, tx, scope, plan, t, info, today, loc)
				if err != nil {
					return err
				}
				if made {
					n++
				}
			}
			return nil
		})
		if err != nil {
			return created, err
		}
		created += n
	}
	return created, nil
}

// cyclesState reads the meter of a cycles plan: the value at the last done (or at the plan creation, or the first
// reading) and the latest value.
func cyclesState(ctx context.Context, db bun.IDB, scope store.MarinaScope, plan *store.MaintenancePlan, last *store.MaintenanceTask) (due.State, bool, error) {
	var st due.State
	cur, ok, err := store.MeterValueAt(ctx, db, scope, plan.PeriodMeterID, time.Time{})
	if err != nil || !ok {
		return st, false, err
	}
	at := plan.CreatedAt
	if last != nil {
		at = last.DoneAt
	}
	base, ok, err := store.MeterValueAt(ctx, db, scope, plan.PeriodMeterID, at)
	if err != nil {
		return st, false, err
	}
	if !ok {
		rs, err := store.MeterReadingsOf(ctx, db, scope, plan.PeriodMeterID)
		if err != nil || len(rs) == 0 {
			return st, false, err
		}
		base = rs[0].ValueMilli
	}
	st.LastCycles, st.CurrentCycles = qty.Milli(base), qty.Milli(cur)
	return st, true, nil
}

// scheduleTarget creates the task of one target when due; it reports whether it created one.
func (s *Service) scheduleTarget(ctx context.Context, tx bun.IDB, scope store.MarinaScope, plan *store.MaintenancePlan,
	target store.MaintenancePlanTarget, info store.ElementInfo, today calendar.Date, loc *time.Location,
) (bool, error) {
	live, err := store.LiveTaskOfTarget(ctx, tx, scope, plan.ID, target.ElementType, target.ElementID)
	if err != nil || live {
		return false, err
	}
	var last *store.MaintenanceTask
	switch t, err := store.LastDoneTaskOfTarget(ctx, tx, scope, plan.ID, target.ElementType, target.ElementID); {
	case err == nil:
		last = t
	case !isNotFound(err):
		return false, err
	}
	var st due.State
	if last != nil && !last.DoneAt.IsZero() {
		d := calendar.In(last.DoneAt, loc)
		st.LastDone = &d
	}
	period := periodicityOf(plan)
	if plan.PeriodKind == due.KindCycles {
		cs, ok, err := cyclesState(ctx, tx, scope, plan, last)
		if err != nil || !ok {
			return false, err
		}
		cs.LastDone = st.LastDone
		st = cs
	}
	if period.StartsOn == nil && plan.PeriodKind != due.KindCycles {
		created := calendar.In(plan.CreatedAt, loc)
		period.StartsOn = &created
	}
	dueOn, ok := due.Next(period, st, today)
	if !ok || today.Before(dueOn.AddDays(-plan.LeadDays)) {
		return false, nil
	}
	now := s.now()
	title := plan.Name
	t := &store.MaintenanceTask{
		ID: store.NewID(), ElementType: target.ElementType, ElementID: target.ElementID, PlanID: plan.ID,
		Intervention: plan.Intervention, Source: store.TaskSourcePlan, Title: title, Priority: "normal", Status: store.TaskOpen,
		DueOn: dueOn, MapID: info.MapID, Checklist: plan.Checklist, AssigneeUserID: plan.AssigneeUserID,
		SupplierRef: plan.SupplierName, CreatedAt: now, UpdatedAt: now,
	}
	if t.MapID != "" {
		ok, err := mapExists(ctx, tx, scope, t.MapID)
		if err != nil {
			return false, err
		}
		if !ok {
			t.MapID = ""
		}
	}
	if t.MapID == "" && t.ElementID == "" {
		return false, nil
	}
	inserted, err := store.InsertMaintenanceTaskIfAbsent(ctx, tx, scope, t)
	if err != nil || !inserted {
		return false, err
	}
	if _, err := deadline.SyncMaintenanceTask(ctx, tx, scope, t, now); err != nil {
		return false, err
	}
	if err := s.audit(ctx, tx, scope, audit.System, "maintenance_task.due", "maintenance_task", t.ID,
		map[string]any{"plan_id": plan.ID, "element_type": t.ElementType, "due_on": dueOn.String()}); err != nil {
		return false, err
	}
	if s.d.Outbox != nil {
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.MaintenanceDue, event.MaintenanceDueData{
			TaskID: t.ID, PlanID: plan.ID, ElementType: t.ElementType, ElementID: t.ElementID, DueOn: dueOn.String(),
		}); err != nil {
			return false, err
		}
	}
	return true, s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status)
}
