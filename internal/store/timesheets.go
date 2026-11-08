package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of time entries (M10, m10-plan §7). Owned by ORDERS. One running timer per employee is the partial
// unique timesheets_running_uidx (INV-95); offline replays dedupe through timesheets_client_ref_uidx and
// timesheets_stop_client_ref_uidx (INV-83).

func normaliseTimesheet(t *Timesheet) {
	t.StartedAt, t.EndedAt, t.ApprovedAt, t.VoidedAt = UTC(t.StartedAt), UTC(t.EndedAt), UTC(t.ApprovedAt), UTC(t.VoidedAt)
	t.CreatedAt, t.UpdatedAt = UTC(t.CreatedAt), UTC(t.UpdatedAt)
}

// InsertTimesheet inserts a time entry (a second running timer of the employee or a repeated client_ref is a unique
// violation; on PostgreSQL the caller's transaction is then aborted).
func InsertTimesheet(ctx context.Context, tx bun.IDB, s MarinaScope, t *Timesheet) error {
	t.MarinaID = s.ID()
	if t.Version == 0 {
		t.Version = 1
	}
	normaliseTimesheet(t)
	_, err := tx.NewInsert().Model(t).Exec(ctx)
	return err
}

// GetTimesheet returns a time entry of the marina.
func GetTimesheet(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Timesheet, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(Timesheet)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// LockTimesheet reads a time entry FOR UPDATE on PostgreSQL.
func LockTimesheet(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*Timesheet, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(Timesheet)
	q := tx.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// TimesheetByClientRef returns the entry started or created with client_ref, ErrNotFound when none.
func TimesheetByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, clientRef string) (*Timesheet, error) {
	t := new(Timesheet)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("client_ref = ?", clientRef).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// TimesheetByStopClientRef returns the entry stopped with client_ref, ErrNotFound when none.
func TimesheetByStopClientRef(ctx context.Context, db bun.IDB, s MarinaScope, clientRef string) (*Timesheet, error) {
	t := new(Timesheet)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("stop_client_ref = ?", clientRef).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// RunningTimer returns the running entry of an employee, ErrNotFound when none.
func RunningTimer(ctx context.Context, db bun.IDB, s MarinaScope, employeeID string) (*Timesheet, error) {
	t := new(Timesheet)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("employee_id = ?", employeeID).
		Where("status = ?", TimesheetRunning).Limit(1).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateTimesheet writes the given columns (plus updated_at) when the version equals expected and bumps it.
func UpdateTimesheet(ctx context.Context, tx bun.IDB, s MarinaScope, t *Timesheet, expected int, now time.Time, cols ...string) error {
	t.UpdatedAt = UTC(now)
	normaliseTimesheet(t)
	res, err := tx.NewUpdate().Model(t).Column(append(cols, "updated_at")...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", t.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetTimesheet(ctx, tx, s, t.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	t.Version = expected + 1
	return nil
}

// TimesheetFilter filters the entry list.
type TimesheetFilter struct {
	EmployeeID  string
	WorkOrderID string
	Status      string
	From, To    calendar.Date
}

// ListTimesheets returns a page of entries, newest first.
func ListTimesheets(ctx context.Context, db bun.IDB, s MarinaScope, f TimesheetFilter, cursor string, limit int) (Page[Timesheet], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Timesheet]{}, err
	}
	limit = ClampLimit(limit)
	var rows []Timesheet
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	for col, v := range map[string]string{"employee_id": f.EmployeeID, "work_order_id": f.WorkOrderID} {
		if v == "" {
			continue
		}
		if !ValidID(v) {
			return Page[Timesheet]{Items: []Timesheet{}}, nil
		}
		q = q.Where("? = ?", bun.Ident(col), v)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if !f.From.IsZero() {
		q = q.Where("work_date >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("work_date <= ?", f.To)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Timesheet]{}, err
	}
	return pageOf(rows, limit, func(t Timesheet) string { return t.ID }), nil
}

// ApprovedEntryRow is an approved entry with the qualification and work type of its task.
type ApprovedEntryRow struct {
	ID              string        `bun:"id"`
	EmployeeID      string        `bun:"employee_id"`
	WorkOrderID     string        `bun:"work_order_id"`
	TaskID          string        `bun:"task_id"`
	WorkDate        calendar.Date `bun:"work_date"`
	HoursMilli      int64         `bun:"hours_milli"`
	CostCents       int64         `bun:"cost_cents"`
	Late            bool          `bun:"late"`
	QualificationID string        `bun:"qualification_id"`
	WorkTypeID      string        `bun:"work_type_id"`
}

// ApprovedEntriesOfOrders returns the approved entries of orders with the task qualification and work type.
func ApprovedEntriesOfOrders(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) ([]ApprovedEntryRow, error) {
	out := []ApprovedEntryRow{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("timesheets AS e").
		ColumnExpr("e.id, e.employee_id, e.work_order_id, COALESCE(CAST(e.task_id AS TEXT), '') AS task_id, e.work_date, e.hours_milli").
		ColumnExpr("COALESCE(e.cost_cents, 0) AS cost_cents, e.late").
		ColumnExpr("COALESCE(CAST(t.qualification_id AS TEXT), '') AS qualification_id, COALESCE(CAST(t.work_type_id AS TEXT), '') AS work_type_id").
		Join("LEFT JOIN work_tasks AS t ON t.id = e.task_id AND t.marina_id = e.marina_id").
		Where("e.marina_id = ?", s.ID()).Where("e.status = ?", TimesheetApproved).Where("e.work_order_id IN (?)", bun.List(ids)).
		OrderExpr("e.id").Scan(ctx, &out)
	return out, err
}

// StaleRunningTimers returns the running entries started before the instant (timers guard).
func StaleRunningTimers(ctx context.Context, db bun.IDB, before time.Time) ([]Timesheet, error) {
	out := []Timesheet{}
	err := db.NewSelect().Model(&out).Where("status = ?", TimesheetRunning).Where("started_at < ?", UTC(before)).
		OrderExpr("marina_id, id").Limit(1000).Scan(ctx)
	return out, err
}

// ---- employees as the time entries need them (the planner owns the employee registry) ----

// WOEmployee returns an employee of the marina.
func WOEmployee(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Employee, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(Employee)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// WOEmployeeByUser returns the employee linked to a user in the marina, ErrNotFound when none.
func WOEmployeeByUser(ctx context.Context, db bun.IDB, s MarinaScope, userID string) (*Employee, error) {
	if !ValidID(userID) {
		return nil, ErrNotFound
	}
	e := new(Employee)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("user_id = ?", userID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// WOEmployees returns employees of the marina by id.
func WOEmployees(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]Employee, error) {
	out := map[string]Employee{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Employee
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// WOEmployeeQualifications returns the qualification ids of an employee.
func WOEmployeeQualifications(ctx context.Context, db bun.IDB, s MarinaScope, employeeID string) ([]string, error) {
	var out []string
	err := db.NewSelect().TableExpr("employee_qualifications").Column("qualification_id").Where("marina_id = ?", s.ID()).
		Where("employee_id = ?", employeeID).OrderExpr("qualification_id").Scan(ctx, &out)
	return out, err
}
