package app

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/lifting"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/domain/yardplan"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedYardPlanner adds the M10 demo of PLANNER (m10-plan §16.2): four employees with calendars and qualifications
// (the yard worker and the yard manager demo users are linked to two of them for the phone), the operators of the
// travel lift and the crane of the M9 equipment seed (their bookings follow the work orders: seedYardOrders).
// Everything goes through the services as the demo owner; a no-op when the marina already has employees. The
// assignments come after the work orders: seedYardPlannerAssignments.
func (a *App) seedYardPlanner(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("employees").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	var quals []store.YardQualification
	if err := a.DB.NewSelect().Model(&quals).Where("marina_id = ?", scope.ID()).OrderExpr("sort_order, code").Scan(ctx); err != nil {
		return err
	}
	byCode := map[string]string{}
	var liftQuals []string
	for _, q := range quals {
		byCode[q.Code] = q.ID
		if q.LiftOperator {
			liftQuals = append(liftQuals, q.ID)
		}
	}
	pick := func(codes ...string) []string {
		var out []string
		for _, c := range codes {
			if id := byCode[c]; id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	userOf := func(role auth.Role) string {
		u, err := store.UserByEmail(ctx, a.DB.DB, string(role)+"@"+DemoEmailDomain)
		if err != nil {
			return ""
		}
		return u.ID
	}
	type emp struct {
		code, first, last, dept, user string
		cost                          int64
		quals                         []string
		week                          []yardplan.CalendarDay
	}
	partTime := []yardplan.CalendarDay{{Weekday: 1, Minutes: 360}, {Weekday: 2, Minutes: 360}, {Weekday: 3, Minutes: 360}, {Weekday: 4, Minutes: 360}, {Weekday: 5, Minutes: 360}, {Weekday: 6, Minutes: 240}}
	emps := []emp{
		{code: "Y01", first: "Marco", last: "Esposito", dept: "Piazzale", user: userOf(auth.RoleYard), cost: 2200, quals: append(pick("hand"), liftQuals...)},
		{code: "Y02", first: "Luca", last: "Ferraro", dept: "Verniciatura", cost: 2600, quals: pick("painter", "hand")},
		{code: "Y03", first: "Giulia", last: "Rinaldi", dept: "Verniciatura", cost: 2600, quals: pick("painter"), week: partTime},
		{code: "Y04", first: "Andrea", last: "Galli", dept: "Piazzale", user: userOf(auth.RoleYardMgr), cost: 3000, quals: append(pick("hand"), liftQuals...)},
	}
	var operators []string
	for _, e := range emps {
		cost := e.cost
		v, err := a.YardPlan.CreateEmployee(ctx, p, m, yardplan.EmployeeInput{
			Code: e.code, FirstName: e.first, LastName: e.last, Department: e.dept, UserID: e.user, HourlyCostCents: &cost,
			QualificationIDs: e.quals, Calendar: e.week,
		})
		if err != nil {
			return fmt.Errorf("employee %s: %w", e.code, err)
		}
		if len(liftQuals) > 0 && (e.code == "Y01" || e.code == "Y04") {
			operators = append(operators, v.Row.ID)
		}
	}
	today := calendar.In(a.Clock.Now(), seedPlannerLocation(ctx, a, scope))
	if _, err := a.YardPlan.CreateAbsence(ctx, p, m, mustEmployeeID(ctx, a, scope, "Y03"), yardplan.AbsenceInput{
		From: today.AddDays(7), To: today.AddDays(11), Kind: store.AbsenceVacation, Note: "Ferie",
	}); err != nil {
		return fmt.Errorf("absence: %w", err)
	}

	day, err := a.Lifting.ListDay(ctx, p, m, lifting.DayFilter{Date: today.AddDays(1)})
	if err != nil {
		return err
	}
	var lift, crane string
	for _, e := range day.Equipment {
		switch e.Row.Kind {
		case "travel_lift":
			if lift == "" {
				lift = e.Row.ID
			}
		case "crane":
			if crane == "" {
				crane = e.Row.ID
			}
		}
	}
	if len(operators) > 0 {
		for _, eq := range []string{lift, crane} {
			if eq == "" {
				continue
			}
			if _, err := a.Lifting.PutOperators(ctx, p, m, eq, operators); err != nil {
				return fmt.Errorf("operators: %w", err)
			}
		}
	}
	return nil
}

// seedYardPlannerAssignments plans the open tasks of the demo work orders with the auto-plan proposal, confirmed as
// is (m10-plan §6.4), so that the planner week around today is full: first the started orders from four days ago
// (through an App whose clock is set to that day), with the hours of the past days logged and approved and part of
// today's logged and waiting for approval, then every open order (not on hold) from today for three weeks. It runs
// after seedYardOrders; a no-op when there are assignments already or no open task.
func (a *App) seedYardPlannerAssignments(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.YardPlan == nil || a.WorkOrders == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("task_assignments").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	loc := seedPlannerLocation(ctx, a, scope)
	now := a.Clock.Now()
	today := calendar.In(now, loc)
	ordersIn := func(statuses ...string) ([]string, error) {
		var ids []string
		err := a.DB.NewSelect().TableExpr("work_orders").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", scope.ID()).
			Where("status IN (?)", bun.List(statuses)).OrderExpr("year, number").Scan(ctx, &ids)
		return ids, err
	}
	started, err := ordersIn(store.WorkOrderInProgress)
	if err != nil {
		return err
	}
	from := today.AddDays(-4)
	if len(started) > 0 {
		fake := clock.NewFake(from.Time(loc).Add(7 * time.Hour))
		past, err := NewWithOptions(ctx, a.Config, a.Version, a.ext, a.Log, Options{
			DB: a.DB, Clock: fake, Argon2: a.opts.Argon2, Transport: a.opts.Transport,
		})
		if err != nil {
			return err
		}
		defer func() { _ = past.Close() }()
		if err := seedPlanConfirm(ctx, past, p, m, yardplan.ProposalInput{From: from, HorizonDays: 5, WorkOrderIDs: started}); err != nil {
			return err
		}
		if err := a.seedPlannedHours(ctx, p, scope, from, today.AddDays(-1)); err != nil {
			return err
		}
	}
	open, err := ordersIn(store.WorkOrderPlanned, store.WorkOrderInProgress)
	if err != nil || len(open) == 0 {
		return err
	}
	if err := seedPlanConfirm(ctx, a, p, m, yardplan.ProposalInput{From: today, HorizonDays: 21, WorkOrderIDs: open}); err != nil {
		return err
	}
	return a.seedPlannedHours(ctx, p, scope, today, today)
}

// seedPlanConfirm confirms the auto-plan proposal of in as is.
func seedPlanConfirm(ctx context.Context, a *App, p *auth.Principal, m string, in yardplan.ProposalInput) error {
	prop, err := a.YardPlan.ProposeYardPlan(ctx, p, m, in)
	if err != nil || len(prop.Changes) == 0 {
		return err
	}
	if _, err := a.YardPlan.PutAssignments(ctx, p, m, yardplan.BatchInput{Changes: prop.Changes}); err != nil {
		return fmt.Errorf("assignments: %w", err)
	}
	return nil
}

// seedPlannedHours logs the planned hours of the days from..to as worked: approved before today; today (from = to
// = today), the first three assignments half done and waiting for approval. Then each task records its progress.
func (a *App) seedPlannedHours(ctx context.Context, p *auth.Principal, scope store.MarinaScope, from, to calendar.Date) error {
	m := scope.ID()
	today := calendar.In(a.Clock.Now(), seedPlannerLocation(ctx, a, scope))
	asg, err := store.ListPlanAssignments(ctx, a.DB.DB, scope, store.AssignmentFilter{From: from, To: to})
	if err != nil {
		return err
	}
	var approve []workorder.EntryVersion
	done := map[string]int64{}
	pending := 0
	for _, x := range asg {
		h := x.HoursMilli
		if x.WorkDate == today {
			if pending == 3 {
				continue
			}
			h /= 2
		}
		// An assignment too short to log half of it in manual steps is skipped before it takes one of today's three
		// pending entries: otherwise, depending on the time of day the plan starts from, today's first assignments
		// round to zero and the demo has no hours to approve.
		if h = seedStep(h); h <= 0 {
			continue
		}
		if x.WorkDate == today {
			pending++
		}
		hm := qty.Milli(h)
		v, err := a.WorkOrders.CreateEntry(ctx, p, m, workorder.EntryInput{
			ClientRef: "seed-asg-" + x.ID, EmployeeID: x.EmployeeID, TaskID: x.TaskID, WorkDate: x.WorkDate, Hours: &hm,
		})
		if err != nil {
			return fmt.Errorf("seed planned entry: %w", err)
		}
		done[x.TaskID] += h
		if x.WorkDate.Before(today) {
			approve = append(approve, workorder.EntryVersion{ID: v.Row.ID, Version: int64(v.Row.Version)})
		}
	}
	if len(approve) > 0 {
		if _, err := a.WorkOrders.Approve(ctx, p, m, approve); err != nil {
			return fmt.Errorf("seed approve: %w", err)
		}
	}
	for _, x := range asg {
		h, ok := done[x.TaskID]
		if !ok {
			continue
		}
		delete(done, x.TaskID)
		t, err := store.GetWorkTask(ctx, a.DB.DB, scope, x.TaskID)
		if err != nil || t.EstimatedHoursMilli == 0 {
			continue
		}
		bp := min(9500, h*10000/t.EstimatedHoursMilli)
		if _, err := a.WorkOrders.UpdateProgress(ctx, p, m, x.TaskID, workorder.ProgressInput{ClientRef: "seed-pp-" + x.TaskID, ProgressBP: &bp}); err != nil {
			return fmt.Errorf("seed progress: %w", err)
		}
	}
	return nil
}

func mustEmployeeID(ctx context.Context, a *App, scope store.MarinaScope, code string) string {
	var id string
	_ = a.DB.NewSelect().TableExpr("employees").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", scope.ID()).
		Where("code = ?", code).Scan(ctx, &id)
	return id
}

func seedPlannerLocation(ctx context.Context, a *App, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
