package yardplan

import (
	"context"
	"errors"
	"sort"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/yardplan/auto"
	"github.com/coste360/porto/internal/domain/yardplan/load"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// loadEmployees builds the load model of the employees with their qualifications, calendars and the absences
// overlapping [from, to].
func loadEmployees(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Employee, from, to calendar.Date) ([]load.Employee, map[string][]store.EmployeeAbsence, error) {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	quals, err := store.EmployeeQualifications(ctx, db, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	cals, err := store.EmployeeCalendars(ctx, db, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	abs, err := store.ListAbsences(ctx, db, scope, ids, from, to)
	if err != nil {
		return nil, nil, err
	}
	byEmp := map[string][]store.EmployeeAbsence{}
	for _, a := range abs {
		byEmp[a.EmployeeID] = append(byEmp[a.EmployeeID], a)
	}
	out := make([]load.Employee, 0, len(rows))
	for _, r := range rows {
		e := load.Employee{ID: r.ID, Department: r.Department, Qualifications: quals[r.ID], WeekMinutes: cals[r.ID]}
		for _, a := range byEmp[r.ID] {
			e.Absences = append(e.Absences, load.Absence{From: a.FromOn, To: a.ToOn, Minutes: a.MinutesPerDay})
		}
		out = append(out, e)
	}
	return out, byEmp, nil
}

func assignmentViews(rows []store.PlanAssignment) []AssignmentView {
	out := make([]AssignmentView, 0, len(rows))
	for _, r := range rows {
		out = append(out, AssignmentView{Row: r.TaskAssignment, WorkOrderID: r.WorkOrderID})
	}
	return out
}

func closed(t store.PlanTask) bool {
	return t.Status == store.WorkTaskDone || t.Status == store.WorkTaskCancelled ||
		t.OrderStatus == store.WorkOrderInvoiced || t.OrderStatus == store.WorkOrderCancelled
}

type triple struct {
	task, emp string
	date      calendar.Date
}

type empDay struct {
	emp  string
	date calendar.Date
}

func validateChanges(changes []AssignmentChange) error {
	v := &auth.ValidationError{}
	if len(changes) == 0 || len(changes) > 500 {
		v.Add("changes", "invalid_count")
	}
	seen := map[triple]bool{}
	for _, c := range changes {
		switch {
		case !store.ValidID(c.TaskID):
			v.Add("changes.task_id", "invalid")
		case !store.ValidID(c.EmployeeID):
			v.Add("changes.employee_id", "invalid")
		case c.Date.IsZero():
			v.Add("changes.work_date", "required")
		case c.Hours < 0 || c.Hours > MaxAssignmentHours:
			v.Add("changes.hours_milli", "invalid")
		}
		k := triple{c.TaskID, c.EmployeeID, c.Date}
		if seen[k] {
			v.Add("changes", "duplicate")
		}
		seen[k] = true
	}
	return v.Err()
}

// PutAssignments creates, changes or deletes assignments in one transaction under the task locks (m10-plan §6.2):
// task and order open, task versions (If-Match), qualification (overridable, audited), finish-to-start
// dependencies and capacity (overridable, highlighted). Nothing is written when a check fails.
func (s *Service) PutAssignments(ctx context.Context, p *auth.Principal, marinaID string, in BatchInput) (*BatchResult, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if err := validateChanges(in.Changes); err != nil {
		return nil, err
	}
	var touched []string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		touched, err = s.applyBatch(ctx, tx, scope, p, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.batchResult(ctx, s.d.DB.DB, scope, touched)
}

func (s *Service) batchResult(ctx context.Context, db bun.IDB, scope store.MarinaScope, taskIDs []string) (*BatchResult, error) {
	rows, err := store.ListPlanAssignments(ctx, db, scope, store.AssignmentFilter{TaskIDs: taskIDs})
	if err != nil {
		return nil, err
	}
	tasks, err := store.PlanTasksByIDs(ctx, db, scope, taskIDs)
	if err != nil {
		return nil, notFound(err)
	}
	out := &BatchResult{Assignments: assignmentViews(rows)}
	for _, t := range tasks {
		out.Tasks = append(out.Tasks, TaskVersion{TaskID: t.ID, Version: t.Version})
	}
	return out, nil
}

func (s *Service) applyBatch(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, in BatchInput) ([]string, error) {
	var taskIDs, empIDs []string
	minD, maxD := in.Changes[0].Date, in.Changes[0].Date
	for _, c := range in.Changes {
		taskIDs, empIDs = append(taskIDs, c.TaskID), append(empIDs, c.EmployeeID)
		if c.Date.Before(minD) {
			minD = c.Date
		}
		if c.Date.After(maxD) {
			maxD = c.Date
		}
	}
	tasks, err := store.LockPlanTasksSorted(ctx, tx, scope, taskIDs)
	if err != nil {
		return nil, notFound(err)
	}
	taskBy := map[string]store.PlanTask{}
	for _, t := range tasks {
		taskBy[t.ID] = t
	}
	for _, c := range in.Changes {
		if int64(taskBy[c.TaskID].Version) != c.TaskVersion {
			return nil, ErrVersionMismatch
		}
	}
	for _, t := range tasks {
		if closed(t) {
			return nil, ErrClosed
		}
	}
	emps, err := store.LockEmployeesSorted(ctx, tx, scope, empIDs)
	if errors.Is(err, store.ErrNotFound) {
		v := &auth.ValidationError{}
		v.Add("changes.employee_id", "not_found")
		return nil, v
	}
	if err != nil {
		return nil, err
	}
	model, _, err := loadEmployees(ctx, tx, scope, emps, minD, maxD)
	if err != nil {
		return nil, err
	}
	empBy := map[string]load.Employee{}
	activeBy := map[string]bool{}
	for i, e := range emps {
		empBy[e.ID] = model[i]
		activeBy[e.ID] = e.Active
	}
	forced := map[triple]bool{}
	for _, c := range in.Changes {
		if c.Hours == 0 {
			continue
		}
		if !activeBy[c.EmployeeID] {
			v := &auth.ValidationError{}
			v.Add("changes.employee_id", "inactive")
			return nil, v
		}
		if q := taskBy[c.TaskID].QualificationID; !empBy[c.EmployeeID].Holds(q) {
			if !in.ForceQualification {
				return nil, ErrQualificationMismatch
			}
			forced[triple{c.TaskID, c.EmployeeID, c.Date}] = true
		}
	}

	// Current assignments: of the touched tasks and their dependencies (any date) and of the touched employees on
	// the touched dates (capacity).
	pred, succ, err := store.TaskDeps(ctx, tx, scope, taskIDs)
	if err != nil {
		return nil, err
	}
	related := append([]string(nil), taskIDs...)
	for _, t := range taskIDs {
		related = append(related, pred[t]...)
		related = append(related, succ[t]...)
	}
	byTask, err := store.ListPlanAssignments(ctx, tx, scope, store.AssignmentFilter{TaskIDs: related})
	if err != nil {
		return nil, err
	}
	byEmp, err := store.ListPlanAssignments(ctx, tx, scope, store.AssignmentFilter{From: minD, To: maxD, EmployeeIDs: empIDs})
	if err != nil {
		return nil, err
	}
	rows := map[triple]store.TaskAssignment{}
	hours := map[triple]qty.Milli{}
	for _, a := range byTask {
		k := triple{a.TaskID, a.EmployeeID, a.WorkDate}
		rows[k], hours[k] = a.TaskAssignment, qty.Milli(a.HoursMilli)
	}
	dayTotal := map[empDay]qty.Milli{}
	for _, a := range byEmp {
		dayTotal[empDay{a.EmployeeID, a.WorkDate}] += qty.Milli(a.HoursMilli)
		k := triple{a.TaskID, a.EmployeeID, a.WorkDate}
		rows[k], hours[k] = a.TaskAssignment, qty.Milli(a.HoursMilli)
	}
	oldTotal := map[empDay]qty.Milli{}
	for k, v := range dayTotal {
		oldTotal[k] = v
	}
	for _, c := range in.Changes {
		k := triple{c.TaskID, c.EmployeeID, c.Date}
		dayTotal[empDay{c.EmployeeID, c.Date}] += c.Hours - hours[k]
		hours[k] = c.Hours
	}

	// Finish-to-start: a day of a task is never before the last day of a predecessor nor after the first day of a
	// successor.
	lastOf, firstOf := map[string]calendar.Date{}, map[string]calendar.Date{}
	for k, h := range hours {
		if h <= 0 {
			continue
		}
		if l, ok := lastOf[k.task]; !ok || k.date.After(l) {
			lastOf[k.task] = k.date
		}
		if f, ok := firstOf[k.task]; !ok || k.date.Before(f) {
			firstOf[k.task] = k.date
		}
	}
	for _, c := range in.Changes {
		if c.Hours == 0 {
			continue
		}
		for _, pt := range pred[c.TaskID] {
			if l, ok := lastOf[pt]; ok && c.Date.Before(l) {
				return nil, ErrDependencyViolation
			}
		}
		for _, st := range succ[c.TaskID] {
			if f, ok := firstOf[st]; ok && c.Date.After(f) {
				return nil, ErrDependencyViolation
			}
		}
	}

	// Capacity (soft): refuse a day that ends above capacity and grew, unless allow_overload.
	overload := map[empDay]bool{}
	var days []empDay
	for k := range dayTotal {
		days = append(days, k)
	}
	sort.Slice(days, func(i, j int) bool {
		if c := days[i].date.Compare(days[j].date); c != 0 {
			return c < 0
		}
		return days[i].emp < days[j].emp
	})
	for _, k := range days {
		capa, err := load.Capacity(empBy[k.emp], k.date)
		if err != nil {
			return nil, err
		}
		if t := dayTotal[k]; t > capa && t > oldTotal[k] {
			if !in.AllowOverload {
				return nil, &CapacityError{EmployeeID: k.emp, Date: k.date, Capacity: capa, Assigned: t}
			}
			overload[k] = true
		}
	}

	now := s.now()
	var dates []calendar.Date
	for _, c := range in.Changes {
		k := triple{c.TaskID, c.EmployeeID, c.Date}
		dates = append(dates, c.Date)
		row, exists := rows[k]
		switch {
		case c.Hours == 0 && exists:
			if err := store.DeleteAssignment(ctx, tx, scope, row.ID, 0); err != nil {
				return nil, err
			}
		case c.Hours == 0:
		case exists:
			row.HoursMilli, row.ForcedQualification = int64(c.Hours), forced[k]
			row.Overload = overload[empDay{c.EmployeeID, c.Date}]
			if err := store.UpdateAssignmentHours(ctx, tx, scope, &row, now); err != nil {
				return nil, err
			}
		default:
			row = store.TaskAssignment{
				ID: store.NewID(), TaskID: c.TaskID, EmployeeID: c.EmployeeID, WorkDate: c.Date, HoursMilli: int64(c.Hours),
				ForcedQualification: forced[k], Overload: overload[empDay{c.EmployeeID, c.Date}], CreatedByUserID: p.UserID,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := store.InsertAssignment(ctx, tx, scope, &row); err != nil {
				return nil, err
			}
		}
	}
	uniq := map[string]bool{}
	var out []string
	for _, t := range tasks {
		if uniq[t.ID] {
			continue
		}
		uniq[t.ID] = true
		out = append(out, t.ID)
		if _, err := store.RefreshTaskPlan(ctx, tx, scope, t.ID, now); err != nil {
			return nil, err
		}
		n, nf := 0, 0
		for _, c := range in.Changes {
			if c.TaskID == t.ID {
				n++
				if forced[triple{c.TaskID, c.EmployeeID, c.Date}] {
					nf++
				}
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "task.assignments", "work_task", t.ID, map[string]any{
			"changes": n, "forced_qualification": nf, "allow_overload": in.AllowOverload && len(overload) > 0,
		}); err != nil {
			return nil, err
		}
	}
	return out, s.changed(ctx, tx, scope, "assignments", "", dates)
}

// DeleteAssignment deletes an assignment (yard.manage, If-Match on the assignment).
func (s *Service) DeleteAssignment(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) error {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		a, err := store.GetAssignment(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		tasks, err := store.LockPlanTasksSorted(ctx, tx, scope, []string{a.TaskID})
		if err != nil {
			return notFound(err)
		}
		if int64(a.Version) != version {
			return ErrVersionMismatch
		}
		if closed(tasks[0]) {
			return ErrClosed
		}
		if err := store.DeleteAssignment(ctx, tx, scope, id, int(version)); err != nil {
			return versionErr(err)
		}
		if _, err := store.RefreshTaskPlan(ctx, tx, scope, a.TaskID, s.now()); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "task.assignment_delete", "work_task", a.TaskID, map[string]any{
			"assignment_id": a.ID, "employee_id": a.EmployeeID, "date": a.WorkDate, "hours_milli": a.HoursMilli,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "assignments", "", []calendar.Date{a.WorkDate})
	})
}

func remainingOf(t store.PlanTask) qty.Milli {
	return load.RiskTask{EstHours: qty.Milli(t.EstimatedHoursMilli), ProgressBP: int64(t.ProgressBP)}.Remaining()
}

// GetPlanner returns the grid employees × days with capacity, assignments and the open tasks (yard.read or
// yard.manage). Default period: the ISO week of today.
func (s *Service) GetPlanner(ctx context.Context, p *auth.Principal, marinaID string, from, to calendar.Date, department string) (*PlannerView, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	from, to, err = rangeOrWeek(today, from, to)
	if err != nil {
		return nil, err
	}
	emps, err := store.ActiveEmployees(ctx, db, scope, department)
	if err != nil {
		return nil, err
	}
	model, absBy, err := loadEmployees(ctx, db, scope, emps, from, to)
	if err != nil {
		return nil, err
	}
	asg, err := store.ListPlanAssignments(ctx, db, scope, store.AssignmentFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	actual, err := store.ApprovedHours(ctx, db, scope, from, to)
	if err != nil {
		return nil, err
	}
	assigned := map[empDay]qty.Milli{}
	for _, a := range asg {
		assigned[empDay{a.EmployeeID, a.WorkDate}] += qty.Milli(a.HoursMilli)
	}
	act := map[empDay]qty.Milli{}
	for _, h := range actual {
		act[empDay{h.EmployeeID, h.Date}] += qty.Milli(h.HoursMilli)
	}
	days := calendar.Range(from, to)
	view := &PlannerView{From: from, To: to, Today: today, Assignments: assignmentViews(asg)}
	inGrid := map[string]bool{}
	for i, e := range emps {
		inGrid[e.ID] = true
		row := PlannerRow{Employee: e, QualificationIDs: model[i].Qualifications}
		if row.QualificationIDs == nil {
			row.QualificationIDs = []string{}
		}
		for _, d := range days {
			c, err := load.Capacity(model[i], d)
			if err != nil {
				return nil, err
			}
			k := empDay{e.ID, d}
			pd := PlannerDay{Date: d, Cell: load.NewCell(c, assigned[k], act[k])}
			for _, a := range absBy[e.ID] {
				if !d.Before(a.FromOn) && !d.After(a.ToOn) {
					pd.Absence = a.Kind
					break
				}
			}
			row.Days = append(row.Days, pd)
		}
		view.Rows = append(view.Rows, row)
	}
	if department != "" {
		kept := view.Assignments[:0]
		for _, a := range view.Assignments {
			if inGrid[a.Row.EmployeeID] {
				kept = append(kept, a)
			}
		}
		view.Assignments = kept
	}
	tasks, err := store.OpenPlanTasks(ctx, db, scope, nil)
	if err != nil {
		return nil, err
	}
	future, err := store.AssignedFrom(ctx, db, scope, today)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	pred, _, err := store.TaskDeps(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		deps := pred[t.ID]
		if deps == nil {
			deps = []string{}
		}
		view.Tasks = append(view.Tasks, PlannerTask{
			Task: t, Number: store.WorkOrderNumber(t.OrderYear, t.OrderNumber), Remaining: remainingOf(t),
			Assigned: qty.Milli(future[t.ID]), DependsOn: deps,
		})
	}
	return view, nil
}

// GetYardLoad returns capacity, assigned and actual hours grouped by employee, department, qualification or bay
// per day, week or month; every aggregate is a ratio of sums (D21), bays have no capacity (D22).
func (s *Service) GetYardLoad(ctx context.Context, p *auth.Principal, marinaID, groupBy, period string, from, to calendar.Date) (*LoadView, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if groupBy == "" {
		groupBy = GroupEmployee
	}
	if period == "" {
		period = load.PeriodDay
	}
	v := &auth.ValidationError{}
	switch groupBy {
	case GroupEmployee, GroupDepartment, GroupQualification, GroupBay:
	default:
		v.Add("group_by", "invalid")
	}
	switch period {
	case load.PeriodDay, load.PeriodWeek, load.PeriodMonth:
	default:
		v.Add("period", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	from, to, err = rangeOrWeek(s.today(ctx, db, scope), from, to)
	if err != nil {
		return nil, err
	}
	asg, err := store.ListPlanAssignments(ctx, db, scope, store.AssignmentFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	view := &LoadView{From: from, To: to, GroupBy: groupBy, Period: period}
	days := calendar.Range(from, to)

	// cells[key][periodStart] accumulate day cells.
	type acc struct {
		label string
		cells map[calendar.Date][]load.Cell
	}
	groups := map[string]*acc{}
	add := func(key, label string, d calendar.Date, c load.Cell) {
		g := groups[key]
		if g == nil {
			g = &acc{label: label, cells: map[calendar.Date][]load.Cell{}}
			groups[key] = g
		}
		ps := load.PeriodStart(d, period)
		g.cells[ps] = append(g.cells[ps], c)
	}

	if groupBy == GroupBay {
		allocIDs := make([]string, 0, len(asg))
		for _, a := range asg {
			allocIDs = append(allocIDs, a.AllocationID)
		}
		labels, err := store.AllocationLabels(ctx, db, scope, allocIDs)
		if err != nil {
			return nil, err
		}
		for _, a := range asg {
			key, label := a.AllocationID, labels[a.AllocationID]
			if key == "" {
				key = NoBay
			}
			if label == "" {
				label = NoBay
			}
			add(key, label, a.WorkDate, load.NewCell(0, qty.Milli(a.HoursMilli), 0))
		}
	} else {
		emps, err := store.ActiveEmployees(ctx, db, scope, "")
		if err != nil {
			return nil, err
		}
		model, _, err := loadEmployees(ctx, db, scope, emps, from, to)
		if err != nil {
			return nil, err
		}
		actual, err := store.ApprovedHours(ctx, db, scope, from, to)
		if err != nil {
			return nil, err
		}
		assigned := map[empDay]qty.Milli{}
		for _, a := range asg {
			assigned[empDay{a.EmployeeID, a.WorkDate}] += qty.Milli(a.HoursMilli)
		}
		act := map[empDay]qty.Milli{}
		for _, h := range actual {
			act[empDay{h.EmployeeID, h.Date}] += qty.Milli(h.HoursMilli)
		}
		var qcodes map[string]string
		if groupBy == GroupQualification {
			if qcodes, err = store.QualificationCodes(ctx, db, scope); err != nil {
				return nil, err
			}
		}
		for i, e := range emps {
			type kl struct{ key, label string }
			var keys []kl
			switch groupBy {
			case GroupEmployee:
				keys = []kl{{e.ID, e.Code + " " + e.FirstName + " " + e.LastName}}
			case GroupDepartment:
				if e.Department == "" {
					keys = []kl{{NoBay, NoBay}}
				} else {
					keys = []kl{{e.Department, e.Department}}
				}
			case GroupQualification:
				for _, q := range model[i].Qualifications {
					keys = append(keys, kl{q, qcodes[q]})
				}
				if len(keys) == 0 {
					keys = []kl{{NoBay, NoBay}}
				}
			}
			for _, d := range days {
				c, err := load.Capacity(model[i], d)
				if err != nil {
					return nil, err
				}
				k := empDay{e.ID, d}
				cell := load.NewCell(c, assigned[k], act[k])
				for _, x := range keys {
					add(x.key, x.label, d, cell)
				}
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if groups[keys[i]].label != groups[keys[j]].label {
			return groups[keys[i]].label < groups[keys[j]].label
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		g := groups[k]
		row := LoadRow{Key: k, Label: g.label}
		starts := make([]calendar.Date, 0, len(g.cells))
		for ps := range g.cells {
			starts = append(starts, ps)
		}
		sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
		var all []load.Cell
		for _, ps := range starts {
			c, err := load.Aggregate(g.cells[ps])
			if err != nil {
				return nil, err
			}
			row.Cells = append(row.Cells, LoadCell{PeriodStart: ps, Cell: c})
			all = append(all, c)
		}
		if row.Total, err = load.Aggregate(all); err != nil {
			return nil, err
		}
		view.Rows = append(view.Rows, row)
	}
	return view, nil
}

// GetAtRiskOrders returns the open orders that are late or whose remaining hours per qualification exceed the
// free qualified capacity until delivery (m10-plan §6.3).
func (s *Service) GetAtRiskOrders(ctx context.Context, p *auth.Principal, marinaID string) (*AtRiskView, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	view := &AtRiskView{Today: today, Items: []AtRiskOrder{}}
	orders, err := store.OpenPlanOrders(ctx, db, scope)
	if err != nil || len(orders) == 0 {
		return view, err
	}
	last := today
	for _, o := range orders {
		if o.PlannedDeliveryOn.After(last) {
			last = o.PlannedDeliveryOn
		}
	}
	if today.DaysUntil(last) > 366 {
		last = today.AddDays(366)
	}
	tasks, err := store.OpenPlanTasks(ctx, db, scope, nil)
	if err != nil {
		return nil, err
	}
	byOrder := map[string][]load.RiskTask{}
	for _, t := range tasks {
		byOrder[t.WorkOrderID] = append(byOrder[t.WorkOrderID], load.RiskTask{
			QualificationID: t.QualificationID, EstHours: qty.Milli(t.EstimatedHoursMilli), ProgressBP: int64(t.ProgressBP),
		})
	}
	emps, err := store.ActiveEmployees(ctx, db, scope, "")
	if err != nil {
		return nil, err
	}
	model, _, err := loadEmployees(ctx, db, scope, emps, today, last)
	if err != nil {
		return nil, err
	}
	asg, err := store.ListPlanAssignments(ctx, db, scope, store.AssignmentFilter{From: today, To: last})
	if err != nil {
		return nil, err
	}
	all := make([]load.Assignment, 0, len(asg))
	for _, a := range asg {
		all = append(all, load.Assignment{
			EmployeeID: a.EmployeeID, TaskID: a.TaskID, WorkOrderID: a.WorkOrderID, Date: a.WorkDate, Hours: qty.Milli(a.HoursMilli),
		})
	}
	for _, o := range orders {
		delivery := o.PlannedDeliveryOn
		if delivery.After(last) {
			delivery = last
		}
		v, err := load.AtRisk(load.RiskInput{
			WorkOrderID: o.ID, Today: today, Delivery: delivery, Tasks: byOrder[o.ID], Employees: model, Assignments: all,
		})
		if err != nil {
			return nil, err
		}
		if !v.Late && len(v.Risks) == 0 {
			continue
		}
		view.Items = append(view.Items, AtRiskOrder{
			WorkOrderID: o.ID, Number: store.WorkOrderNumber(o.Year, o.Number), VesselName: o.VesselName,
			Delivery: o.PlannedDeliveryOn, Late: v.Late, Risks: v.Risks,
		})
	}
	return view, nil
}

// ProposeYardPlan computes the greedy auto-plan (yard.manage); it writes nothing (D23).
func (s *Service) ProposeYardPlan(ctx context.Context, p *auth.Principal, marinaID string, in ProposalInput) (*ProposalView, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	from := in.From
	if from.IsZero() {
		from = today.AddDays(1)
	}
	h := in.HorizonDays
	if h == 0 {
		h = DefaultHorizonDays
	}
	v := &auth.ValidationError{}
	if h < 1 || h > MaxHorizonDays {
		v.Add("horizon_days", "invalid")
	}
	if from.Before(today) {
		v.Add("from", "in_the_past")
	}
	for _, id := range in.WorkOrderIDs {
		if !store.ValidID(id) {
			v.Add("work_order_ids", "invalid")
			break
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	last := from.AddDays(h - 1)
	tasks, err := store.OpenPlanTasks(ctx, db, scope, in.WorkOrderIDs)
	if err != nil {
		return nil, err
	}
	future, err := store.AssignedFrom(ctx, db, scope, from)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	pred, _, err := store.TaskDeps(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	version := map[string]int{}
	input := auto.Input{From: from, HorizonDays: h}
	for _, t := range tasks {
		version[t.ID] = t.Version
		rem := remainingOf(t) - qty.Milli(future[t.ID])
		if rem <= 0 {
			continue
		}
		input.Tasks = append(input.Tasks, auto.Task{
			ID: t.ID, WorkOrderID: t.WorkOrderID, OrderNumber: store.WorkOrderNumber(t.OrderYear, t.OrderNumber),
			PlannedDelivery: t.PlannedDeliveryOn, Seq: t.Seq, QualificationID: t.QualificationID, Remaining: rem,
			DependsOn: pred[t.ID],
		})
	}
	emps, err := store.ActiveEmployees(ctx, db, scope, "")
	if err != nil {
		return nil, err
	}
	if input.Employees, _, err = loadEmployees(ctx, db, scope, emps, from, last); err != nil {
		return nil, err
	}
	asg, err := store.ListPlanAssignments(ctx, db, scope, store.AssignmentFilter{From: from, To: last})
	if err != nil {
		return nil, err
	}
	for _, a := range asg {
		input.Existing = append(input.Existing, load.Assignment{
			EmployeeID: a.EmployeeID, TaskID: a.TaskID, WorkOrderID: a.WorkOrderID, Date: a.WorkDate, Hours: qty.Milli(a.HoursMilli),
		})
	}
	prop, err := auto.Propose(input)
	if err != nil {
		return nil, err
	}
	out := &ProposalView{From: from, To: last, Changes: []AssignmentChange{}, Unplaced: prop.Unplaced}
	if out.Unplaced == nil {
		out.Unplaced = []auto.Unplaced{}
	}
	for _, c := range prop.Changes {
		out.Changes = append(out.Changes, AssignmentChange{
			TaskID: c.TaskID, EmployeeID: c.EmployeeID, Date: c.Date, Hours: c.Hours, TaskVersion: int64(version[c.TaskID]),
		})
	}
	return out, nil
}
