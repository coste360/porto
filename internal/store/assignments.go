package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the yard planner (M10, m10-plan §6.2-§6.4): task assignments, the planner grid, workload, at-risk
// orders and the auto-plan input. Owned by PLANNER. Tasks and orders are ORDERS' tables: the planner locks a task
// row (assignments are checked under it), updates its planned_from/planned_to cache and bumps its version, and
// otherwise only reads.

// OpenWorkOrderStatuses are the order statuses whose tasks the planner shows and plans.
var OpenWorkOrderStatuses = []string{WorkOrderPlanned, WorkOrderInProgress, WorkOrderOnHold}

// PlanTask is a task with what the planner needs of its order.
type PlanTask struct {
	ID                  string        `bun:"id"`
	WorkOrderID         string        `bun:"work_order_id"`
	Seq                 int           `bun:"seq"`
	Title               string        `bun:"title"`
	QualificationID     string        `bun:"qualification_id"`
	EstimatedHoursMilli int64         `bun:"estimated_hours_milli"`
	Status              string        `bun:"status"`
	ProgressBP          int           `bun:"progress_bp"`
	Version             int           `bun:"version"`
	OrderStatus         string        `bun:"order_status"`
	OrderNumber         int64         `bun:"order_number"`
	OrderYear           int           `bun:"order_year"`
	PlannedDeliveryOn   calendar.Date `bun:"planned_delivery_on"`
	VesselName          string        `bun:"vessel_name"`
	AllocationID        string        `bun:"allocation_id"`
}

// WorkOrderNumber formats an order number as C<year>-NNNNNN (D12).
func WorkOrderNumber(year int, number int64) string {
	return fmt.Sprintf("C%d-%06d", year, number)
}

func planTaskQuery(db bun.IDB, s MarinaScope) *bun.SelectQuery {
	return db.NewSelect().TableExpr("work_tasks AS t").
		ColumnExpr("t.id, t.work_order_id, t.seq, t.title, COALESCE(CAST(t.qualification_id AS TEXT), '') AS qualification_id").
		ColumnExpr("t.estimated_hours_milli, t.status, t.progress_bp, t.version").
		ColumnExpr("o.status AS order_status, o.number AS order_number, o.year AS order_year, o.planned_delivery_on").
		ColumnExpr("COALESCE(v.name, '') AS vessel_name, COALESCE(CAST(o.allocation_id AS TEXT), '') AS allocation_id").
		Join("JOIN work_orders AS o ON o.id = t.work_order_id AND o.marina_id = t.marina_id").
		Join("LEFT JOIN vessels AS v ON v.id = o.vessel_id AND v.marina_id = o.marina_id").
		Where("t.marina_id = ?", s.ID())
}

// LockPlanTasksSorted locks the tasks with the given ids in ascending id order (FOR UPDATE OF the task rows on
// PostgreSQL) and returns them with their order. Unknown ids are ErrNotFound.
func LockPlanTasksSorted(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]PlanTask, error) {
	want := planUniqSorted(ids)
	for _, id := range want {
		if !ValidID(id) {
			return nil, ErrNotFound
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	if IsPostgres(db) {
		var locked []string
		if err := db.NewSelect().TableExpr("work_tasks").Column("id").Where("marina_id = ?", s.ID()).
			Where("id IN (?)", bun.List(want)).OrderExpr("id").For("UPDATE").Scan(ctx, &locked); err != nil {
			return nil, err
		}
	}
	var rows []PlanTask
	if err := planTaskQuery(db, s).Where("t.id IN (?)", bun.List(want)).OrderExpr("t.id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) != len(want) {
		return nil, ErrNotFound
	}
	return rows, nil
}

// OpenPlanTasks returns the todo and in-progress tasks of the open orders (optionally of the given orders),
// ordered by planned delivery, order and seq.
func OpenPlanTasks(ctx context.Context, db bun.IDB, s MarinaScope, workOrderIDs []string) ([]PlanTask, error) {
	var rows []PlanTask
	q := planTaskQuery(db, s).Where("o.status IN (?)", bun.List(OpenWorkOrderStatuses)).
		Where("t.status IN (?)", bun.List([]string{WorkTaskTodo, WorkTaskInProgress}))
	if workOrderIDs != nil {
		if len(workOrderIDs) == 0 {
			return nil, nil
		}
		q = q.Where("t.work_order_id IN (?)", bun.List(workOrderIDs))
	}
	if err := q.OrderExpr("o.planned_delivery_on, o.year, o.number, t.seq").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// TaskDeps returns the finish-to-start dependencies touching the given tasks: predecessors[task] and
// successors[task].
func TaskDeps(ctx context.Context, db bun.IDB, s MarinaScope, taskIDs []string) (pred, succ map[string][]string, err error) {
	pred, succ = map[string][]string{}, map[string][]string{}
	if len(taskIDs) == 0 {
		return pred, succ, nil
	}
	var rows []WorkTaskDep
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("task_id IN (?)", bun.List(taskIDs)).WhereOr("depends_on_task_id IN (?)", bun.List(taskIDs))
		}).OrderExpr("task_id, depends_on_task_id").Scan(ctx); err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		pred[r.TaskID] = append(pred[r.TaskID], r.DependsOnTaskID)
		succ[r.DependsOnTaskID] = append(succ[r.DependsOnTaskID], r.TaskID)
	}
	return pred, succ, nil
}

// PlanAssignment is an assignment with the order and the order's position (bay).
type PlanAssignment struct {
	TaskAssignment `bun:",extend"`
	WorkOrderID    string `bun:"work_order_id"`
	AllocationID   string `bun:"allocation_id"`
}

func planAssignmentQuery(db bun.IDB, s MarinaScope, rows *[]PlanAssignment) *bun.SelectQuery {
	return db.NewSelect().Model(rows).ModelTableExpr("task_assignments AS a").ColumnExpr("a.*").
		ColumnExpr("t.work_order_id, COALESCE(CAST(o.allocation_id AS TEXT), '') AS allocation_id").
		Join("JOIN work_tasks AS t ON t.id = a.task_id AND t.marina_id = a.marina_id").
		Join("JOIN work_orders AS o ON o.id = t.work_order_id AND o.marina_id = t.marina_id").
		Where("a.marina_id = ?", s.ID())
}

// AssignmentFilter selects assignments.
type AssignmentFilter struct {
	From, To    calendar.Date // inclusive; zero = open
	TaskIDs     []string
	EmployeeIDs []string
}

// ListPlanAssignments returns the assignments matching f ordered by date, employee and task.
func ListPlanAssignments(ctx context.Context, db bun.IDB, s MarinaScope, f AssignmentFilter) ([]PlanAssignment, error) {
	var rows []PlanAssignment
	q := planAssignmentQuery(db, s, &rows)
	if !f.From.IsZero() {
		q = q.Where("a.work_date >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("a.work_date <= ?", f.To)
	}
	if f.TaskIDs != nil {
		if len(f.TaskIDs) == 0 {
			return nil, nil
		}
		q = q.Where("a.task_id IN (?)", bun.List(f.TaskIDs))
	}
	if f.EmployeeIDs != nil {
		if len(f.EmployeeIDs) == 0 {
			return nil, nil
		}
		q = q.Where("a.employee_id IN (?)", bun.List(f.EmployeeIDs))
	}
	if err := q.OrderExpr("a.work_date, a.employee_id, a.task_id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetAssignment returns an assignment of the marina.
func GetAssignment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*TaskAssignment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	a := new(TaskAssignment)
	if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// InsertAssignment inserts an assignment.
func InsertAssignment(ctx context.Context, tx bun.IDB, s MarinaScope, a *TaskAssignment) error {
	a.MarinaID = s.ID()
	if a.Version == 0 {
		a.Version = 1
	}
	a.CreatedAt, a.UpdatedAt = UTC(a.CreatedAt), UTC(a.UpdatedAt)
	_, err := tx.NewInsert().Model(a).Exec(ctx)
	return err
}

// UpdateAssignmentHours rewrites hours and flags of an assignment and bumps its version.
func UpdateAssignmentHours(ctx context.Context, tx bun.IDB, s MarinaScope, a *TaskAssignment, now time.Time) error {
	a.UpdatedAt = UTC(now)
	_, err := tx.NewUpdate().Model(a).Column("hours_milli", "forced_qualification", "overload", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", a.ID).Exec(ctx)
	if err == nil {
		a.Version++
	}
	return err
}

// DeleteAssignment deletes an assignment; expected > 0 checks its version (ErrVersionMismatch).
func DeleteAssignment(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	q := tx.NewDelete().TableExpr("task_assignments").Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetAssignment(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// RefreshTaskPlan recomputes the planned_from/planned_to cache of a task from its assignments and bumps its
// version; it returns the new version.
func RefreshTaskPlan(ctx context.Context, tx bun.IDB, s MarinaScope, taskID string, now time.Time) (int, error) {
	var span struct {
		From calendar.Date `bun:"f"`
		To   calendar.Date `bun:"l"`
	}
	if err := tx.NewSelect().TableExpr("task_assignments").ColumnExpr("MIN(work_date) AS f, MAX(work_date) AS l").
		Where("marina_id = ?", s.ID()).Where("task_id = ?", taskID).Scan(ctx, &span); err != nil {
		return 0, err
	}
	q := tx.NewUpdate().TableExpr("work_tasks").Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", taskID)
	if span.From.IsZero() {
		q = q.Set("planned_from = NULL").Set("planned_to = NULL")
	} else {
		q = q.Set("planned_from = ?", span.From).Set("planned_to = ?", span.To)
	}
	if _, err := q.Exec(ctx); err != nil {
		return 0, err
	}
	var v int
	err := tx.NewSelect().TableExpr("work_tasks").Column("version").Where("marina_id = ?", s.ID()).Where("id = ?", taskID).Scan(ctx, &v)
	return v, err
}

// DayHours is hours of an employee on a day.
type DayHours struct {
	EmployeeID string        `bun:"employee_id"`
	Date       calendar.Date `bun:"work_date"`
	HoursMilli int64         `bun:"hours"`
}

// ApprovedHours returns the approved time entry hours per employee and day in [from, to].
func ApprovedHours(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]DayHours, error) {
	var rows []DayHours
	if err := db.NewSelect().TableExpr("timesheets").ColumnExpr("employee_id, work_date, SUM(hours_milli) AS hours").
		Where("marina_id = ?", s.ID()).Where("status = ?", TimesheetApproved).
		Where("work_date >= ?", from).Where("work_date <= ?", to).
		GroupExpr("employee_id, work_date").OrderExpr("employee_id, work_date").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// AssignedFrom returns the assigned hours per task on or after from.
func AssignedFrom(ctx context.Context, db bun.IDB, s MarinaScope, from calendar.Date) (map[string]int64, error) {
	var rows []struct {
		TaskID string `bun:"task_id"`
		Hours  int64  `bun:"hours"`
	}
	if err := db.NewSelect().TableExpr("task_assignments").ColumnExpr("task_id, SUM(hours_milli) AS hours").
		Where("marina_id = ?", s.ID()).Where("work_date >= ?", from).GroupExpr("task_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.TaskID] = r.Hours
	}
	return out, nil
}

// AllocationLabels returns the code of the resource of each allocation ("" when unknown).
func AllocationLabels(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	out := map[string]string{}
	want := planUniqSorted(ids)
	if len(want) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	if err := db.NewSelect().TableExpr("allocations AS a").ColumnExpr("a.id").
		ColumnExpr("COALESCE(b.code, d.code, l.code, '') AS code").
		Join("LEFT JOIN berths AS b ON a.resource_type = 'berth' AND b.id = a.resource_id").
		Join("LEFT JOIN dynamic_areas AS d ON a.resource_type = 'dynamic_area' AND d.id = a.resource_id").
		Join("LEFT JOIN land_resources AS l ON a.resource_type = 'land_resource' AND l.id = a.resource_id").
		Where("a.marina_id = ?", s.ID()).Where("a.id IN (?)", bun.List(want)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}

// QualificationCodes returns the code of each yard qualification of the marina.
func QualificationCodes(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	var rows []struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	if err := db.NewSelect().TableExpr("yard_qualifications").Column("id", "code").Where("marina_id = ?", s.ID()).
		Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}

// PlanOrder is an open work order for the at-risk view.
type PlanOrder struct {
	ID                string        `bun:"id"`
	Number            int64         `bun:"number"`
	Year              int           `bun:"year"`
	Status            string        `bun:"status"`
	PlannedDeliveryOn calendar.Date `bun:"planned_delivery_on"`
	VesselName        string        `bun:"vessel_name"`
}

// OpenPlanOrders returns the planned, in-progress and on-hold orders with a planned delivery.
func OpenPlanOrders(ctx context.Context, db bun.IDB, s MarinaScope) ([]PlanOrder, error) {
	var rows []PlanOrder
	if err := db.NewSelect().TableExpr("work_orders AS o").
		ColumnExpr("o.id, o.number, o.year, o.status, o.planned_delivery_on, COALESCE(v.name, '') AS vessel_name").
		Join("LEFT JOIN vessels AS v ON v.id = o.vessel_id AND v.marina_id = o.marina_id").
		Where("o.marina_id = ?", s.ID()).Where("o.status IN (?)", bun.List(OpenWorkOrderStatuses)).
		Where("o.planned_delivery_on IS NOT NULL").OrderExpr("o.planned_delivery_on, o.year, o.number").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// PlanTasksByIDs reads tasks with their order (no lock).
func PlanTasksByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]PlanTask, error) {
	want := planUniqSorted(ids)
	if len(want) == 0 {
		return nil, nil
	}
	var rows []PlanTask
	if err := planTaskQuery(db, s).Where("t.id IN (?)", bun.List(want)).OrderExpr("t.id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
