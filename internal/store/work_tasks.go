package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of work order tasks, finish-to-start dependencies and task files (M10, m10-plan §5.3). Owned by ORDERS.
// Dependencies stay within one order and acyclic, checked in Go under the order lock.

func normaliseWorkTask(t *WorkTask) {
	t.Checklist = emptyJSONArray(t.Checklist)
	t.CreatedAt, t.UpdatedAt = UTC(t.CreatedAt), UTC(t.UpdatedAt)
}

// InsertWorkTask inserts a task of the marina.
func InsertWorkTask(ctx context.Context, tx bun.IDB, s MarinaScope, t *WorkTask) error {
	t.MarinaID = s.ID()
	if t.Version == 0 {
		t.Version = 1
	}
	normaliseWorkTask(t)
	_, err := tx.NewInsert().Model(t).Exec(ctx)
	return err
}

// GetWorkTask returns a task of the marina.
func GetWorkTask(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WorkTask, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(WorkTask)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// LockWorkTask reads a task FOR UPDATE on PostgreSQL.
func LockWorkTask(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*WorkTask, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(WorkTask)
	q := tx.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateWorkTask writes the given columns (plus updated_at) when the version equals expected and bumps it.
func UpdateWorkTask(ctx context.Context, tx bun.IDB, s MarinaScope, t *WorkTask, expected int, now time.Time, cols ...string) error {
	t.UpdatedAt = UTC(now)
	normaliseWorkTask(t)
	res, err := tx.NewUpdate().Model(t).Column(append(cols, "updated_at")...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", t.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetWorkTask(ctx, tx, s, t.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	t.Version = expected + 1
	return nil
}

// DeleteWorkTask deletes a task (its dependencies, files links and assignments cascade).
func DeleteWorkTask(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := tx.NewDelete().Model((*WorkTask)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetWorkTask(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// WorkTasksOfOrders returns the tasks of orders ordered by order and seq.
func WorkTasksOfOrders(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) ([]WorkTask, error) {
	out := []WorkTask{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("work_order_id IN (?)", bun.List(ids)).
		OrderExpr("work_order_id, seq").Scan(ctx)
	return out, err
}

// WorkTasksByIDs returns tasks of the marina by id.
func WorkTasksByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]WorkTask, error) {
	out := []WorkTask{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// MaxWorkTaskSeq returns the highest task seq of an order (0 when none).
func MaxWorkTaskSeq(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) (int, error) {
	var n int
	err := db.NewSelect().TableExpr("work_tasks").ColumnExpr("COALESCE(MAX(seq), 0)").Where("marina_id = ?", s.ID()).
		Where("work_order_id = ?", orderID).Scan(ctx, &n)
	return n, err
}

// SetWorkOrderCompletion stores the completion of an order (recomputed on every task change; no version bump: it
// is derived data written under the order lock).
func SetWorkOrderCompletion(ctx context.Context, tx bun.IDB, s MarinaScope, orderID string, bp int) error {
	_, err := tx.NewUpdate().TableExpr("work_orders").Set("completion_bp = ?", bp).Where("marina_id = ?", s.ID()).
		Where("id = ?", orderID).Exec(ctx)
	return err
}

// ---- dependencies ----

// WorkTaskDepsOfOrder returns the dependencies of the tasks of an order.
func WorkTaskDepsOfOrder(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) ([]WorkTaskDep, error) {
	out := []WorkTaskDep{}
	err := db.NewSelect().Model(&out).Where("work_task_dep.marina_id = ?", s.ID()).
		Where("EXISTS (SELECT 1 FROM work_tasks AS t WHERE t.id = work_task_dep.task_id AND t.work_order_id = ?)", orderID).
		OrderExpr("task_id, depends_on_task_id").Scan(ctx)
	return out, err
}

// WorkTaskDepsOf returns the dependencies of tasks.
func WorkTaskDepsOf(ctx context.Context, db bun.IDB, s MarinaScope, taskIDs []string) ([]WorkTaskDep, error) {
	out := []WorkTaskDep{}
	ids := validIDs(taskIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("task_id IN (?)", bun.List(ids)).
		OrderExpr("task_id, depends_on_task_id").Scan(ctx)
	return out, err
}

// ReplaceWorkTaskDeps replaces the predecessors of a task.
func ReplaceWorkTaskDeps(ctx context.Context, tx bun.IDB, s MarinaScope, taskID string, dependsOn []string) error {
	if _, err := tx.NewDelete().Model((*WorkTaskDep)(nil)).Where("marina_id = ?", s.ID()).Where("task_id = ?", taskID).Exec(ctx); err != nil {
		return err
	}
	if len(dependsOn) == 0 {
		return nil
	}
	rows := make([]WorkTaskDep, 0, len(dependsOn))
	for _, d := range dependsOn {
		rows = append(rows, WorkTaskDep{TaskID: taskID, DependsOnTaskID: d, MarinaID: s.ID()})
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// ---- hours of tasks ----

// TaskHours are the approved and assigned hours of a task.
type TaskHours struct {
	TaskID   string `bun:"task_id"`
	Approved int64  `bun:"approved"`
	Assigned int64  `bun:"assigned"`
}

// WorkTaskHours returns the approved (non-voided) hours and the planner assigned hours per task.
func WorkTaskHours(ctx context.Context, db bun.IDB, s MarinaScope, taskIDs []string) (map[string]TaskHours, error) {
	out := map[string]TaskHours{}
	ids := validIDs(taskIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var approved []TaskHours
	if err := db.NewSelect().TableExpr("timesheets").ColumnExpr("task_id, SUM(hours_milli) AS approved").
		Where("marina_id = ?", s.ID()).Where("status = ?", TimesheetApproved).Where("task_id IN (?)", bun.List(ids)).
		GroupExpr("task_id").Scan(ctx, &approved); err != nil {
		return nil, err
	}
	var assigned []TaskHours
	if err := db.NewSelect().TableExpr("task_assignments").ColumnExpr("task_id, SUM(hours_milli) AS assigned").
		Where("marina_id = ?", s.ID()).Where("task_id IN (?)", bun.List(ids)).GroupExpr("task_id").Scan(ctx, &assigned); err != nil {
		return nil, err
	}
	for _, a := range approved {
		h := out[a.TaskID]
		h.TaskID, h.Approved = a.TaskID, a.Approved
		out[a.TaskID] = h
	}
	for _, a := range assigned {
		h := out[a.TaskID]
		h.TaskID, h.Assigned = a.TaskID, a.Assigned
		out[a.TaskID] = h
	}
	return out, nil
}

// WorkTaskHasEntries reports whether a task has any time entry (any status).
func WorkTaskHasEntries(ctx context.Context, db bun.IDB, s MarinaScope, taskID string) (bool, error) {
	n, err := db.NewSelect().TableExpr("timesheets").Where("marina_id = ?", s.ID()).Where("task_id = ?", taskID).Count(ctx)
	if err != nil || n > 0 {
		return n > 0, err
	}
	n, err = db.NewSelect().TableExpr("stock_movements").Where("marina_id = ?", s.ID()).Where("task_id = ?", taskID).Count(ctx)
	return n > 0, err
}

// WorkTaskAssignedTo reports whether an employee has an assignment on the task.
func WorkTaskAssignedTo(ctx context.Context, db bun.IDB, s MarinaScope, taskID, employeeID string) (bool, error) {
	n, err := db.NewSelect().TableExpr("task_assignments").Where("marina_id = ?", s.ID()).Where("task_id = ?", taskID).
		Where("employee_id = ?", employeeID).Count(ctx)
	return n > 0, err
}

// ---- files ----

// WorkTaskFileRow is a task file with its stored file data.
type WorkTaskFileRow struct {
	TaskID      string    `bun:"task_id"`
	FileID      string    `bun:"file_id"`
	Kind        string    `bun:"kind"`
	ClientRef   string    `bun:"client_ref"`
	ContentType string    `bun:"content_type"`
	SizeBytes   int64     `bun:"size_bytes"`
	SHA256      string    `bun:"sha256"`
	StorageKey  string    `bun:"storage_key"`
	CreatedAt   time.Time `bun:"created_at"`
}

// InsertWorkTaskFile links a stored file to a task.
func InsertWorkTaskFile(ctx context.Context, tx bun.IDB, s MarinaScope, f *WorkTaskFile) error {
	f.MarinaID = s.ID()
	f.CreatedAt = UTC(f.CreatedAt)
	_, err := tx.NewInsert().Model(f).Exec(ctx)
	return err
}

// WorkTaskFiles returns the files of tasks, oldest first.
func WorkTaskFiles(ctx context.Context, db bun.IDB, s MarinaScope, taskIDs []string) ([]WorkTaskFileRow, error) {
	out := []WorkTaskFileRow{}
	ids := validIDs(taskIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("work_task_files AS f").
		ColumnExpr("f.task_id, f.file_id, f.kind, COALESCE(f.client_ref, '') AS client_ref, f.created_at").
		ColumnExpr("sf.content_type, sf.size_bytes, sf.sha256, sf.storage_key").
		Join("JOIN stored_files AS sf ON sf.id = f.file_id AND sf.marina_id = f.marina_id").
		Where("f.marina_id = ?", s.ID()).Where("f.task_id IN (?)", bun.List(ids)).OrderExpr("f.created_at, f.file_id").Scan(ctx, &out)
	return out, err
}

// WorkTaskFileByClientRef returns the file uploaded with a client_ref (offline replay), ErrNotFound when none.
func WorkTaskFileByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, clientRef string) (*WorkTaskFileRow, error) {
	var rows []WorkTaskFileRow
	err := db.NewSelect().TableExpr("work_task_files AS f").
		ColumnExpr("f.task_id, f.file_id, f.kind, COALESCE(f.client_ref, '') AS client_ref, f.created_at").
		ColumnExpr("sf.content_type, sf.size_bytes, sf.sha256, sf.storage_key").
		Join("JOIN stored_files AS sf ON sf.id = f.file_id AND sf.marina_id = f.marina_id").
		Where("f.marina_id = ?", s.ID()).Where("f.client_ref = ?", clientRef).Limit(1).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// ---- my work (phone) ----

// MyAssignmentRow is an assignment of an employee with its task, order and vessel.
type MyAssignmentRow struct {
	AssignmentID string        `bun:"assignment_id"`
	WorkDate     calendar.Date `bun:"work_date"`
	HoursMilli   int64         `bun:"hours_milli"`
	TaskID       string        `bun:"task_id"`
	TaskTitle    string        `bun:"task_title"`
	TaskStatus   string        `bun:"task_status"`
	TaskVersion  int64         `bun:"task_version"`
	ProgressBP   int64         `bun:"progress_bp"`
	Checklist    JSON          `bun:"checklist"`
	WorkOrderID  string        `bun:"work_order_id"`
	OrderYear    int           `bun:"order_year"`
	OrderNumber  int64         `bun:"order_number"`
	VesselName   string        `bun:"vessel_name"`
	AllocationID string        `bun:"allocation_id"`
	PositionNote string        `bun:"position_note"`
}

// MyAssignments returns the assignments of an employee between from and to (inclusive), by date then order.
func MyAssignments(ctx context.Context, db bun.IDB, s MarinaScope, employeeID string, from, to calendar.Date) ([]MyAssignmentRow, error) {
	out := []MyAssignmentRow{}
	err := db.NewSelect().TableExpr("task_assignments AS a").
		ColumnExpr("a.id AS assignment_id, a.work_date, a.hours_milli, t.id AS task_id, t.title AS task_title").
		ColumnExpr("t.status AS task_status, t.version AS task_version, t.progress_bp, t.checklist").
		ColumnExpr("o.id AS work_order_id, o.year AS order_year, o.number AS order_number, v.name AS vessel_name").
		ColumnExpr("COALESCE(CAST(o.allocation_id AS TEXT), '') AS allocation_id, o.position_note").
		Join("JOIN work_tasks AS t ON t.id = a.task_id AND t.marina_id = a.marina_id").
		Join("JOIN work_orders AS o ON o.id = t.work_order_id AND o.marina_id = t.marina_id").
		Join("JOIN vessels AS v ON v.id = o.vessel_id AND v.marina_id = o.marina_id").
		Where("a.marina_id = ?", s.ID()).Where("a.employee_id = ?", employeeID).
		Where("a.work_date >= ?", from).Where("a.work_date <= ?", to).
		OrderExpr("a.work_date, o.year, o.number, t.seq").Scan(ctx, &out)
	return out, err
}
