package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the yard employees, their qualifications, weekly calendars and absences (M10, m10-plan §6.1). Owned by
// PLANNER. Employee names are personal data: they never reach events or logs (INV-70). A duplicate code or user
// link is a unique violation of employees_code_uidx / employees_user_uidx (the service maps it to code_taken).

// EmployeeFilter filters the employee list.
type EmployeeFilter struct {
	Active          *bool
	Department      string
	QualificationID string
	IDs             []string
}

// InsertEmployee inserts an employee of the marina.
func InsertEmployee(ctx context.Context, tx bun.IDB, s MarinaScope, e *Employee) error {
	e.MarinaID = s.ID()
	if e.Version == 0 {
		e.Version = 1
	}
	e.CreatedAt, e.UpdatedAt = UTC(e.CreatedAt), UTC(e.UpdatedAt)
	_, err := tx.NewInsert().Model(e).Exec(ctx)
	return err
}

// GetEmployee returns an employee of the marina.
func GetEmployee(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Employee, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(Employee)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// LockEmployee locks an employee row (PostgreSQL FOR UPDATE; SQLite has one writer) and returns it.
func LockEmployee(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Employee, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(Employee)
	q := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// LockEmployeesSorted locks the employees with the given ids in ascending id order. Unknown ids are ErrNotFound.
func LockEmployeesSorted(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Employee, error) {
	want := planUniqSorted(ids)
	if len(want) == 0 {
		return nil, nil
	}
	for _, id := range want {
		if !ValidID(id) {
			return nil, ErrNotFound
		}
	}
	var rows []Employee
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(want)).OrderExpr("id")
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	if len(rows) != len(want) {
		return nil, ErrNotFound
	}
	return rows, nil
}

// UpdateEmployee writes the mutable columns when the version equals expected and bumps it.
func UpdateEmployee(ctx context.Context, tx bun.IDB, s MarinaScope, e *Employee, expected int, now time.Time) error {
	e.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(e).
		Column("code", "first_name", "last_name", "user_id", "department", "hourly_cost_cents", "active", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetEmployee(ctx, tx, s, e.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	e.Version = expected + 1
	return nil
}

// TouchEmployee bumps the version of an employee whose calendar or qualifications changed.
func TouchEmployee(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int, now time.Time) error {
	res, err := tx.NewUpdate().TableExpr("employees").Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetEmployee(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListEmployees returns a page of employees ordered by code then id (the cursor is the id of the last row of the
// previous page; codes are unique per marina so the order is total).
func ListEmployees(ctx context.Context, db bun.IDB, s MarinaScope, f EmployeeFilter, cursor string, limit int) (Page[Employee], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Employee]{}, err
	}
	limit = ClampLimit(limit)
	var rows []Employee
	q := db.NewSelect().Model(&rows).Where("e.marina_id = ?", s.ID())
	if f.Active != nil {
		q = q.Where("e.active = ?", *f.Active)
	}
	if f.Department != "" {
		q = q.Where("e.department = ?", f.Department)
	}
	if f.QualificationID != "" {
		if !ValidID(f.QualificationID) {
			return Page[Employee]{Items: []Employee{}}, nil
		}
		q = q.Where("EXISTS (SELECT 1 FROM employee_qualifications AS q WHERE q.employee_id = e.id AND q.qualification_id = ?)",
			f.QualificationID)
	}
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return Page[Employee]{Items: []Employee{}}, nil
		}
		q = q.Where("e.id IN (?)", bun.List(f.IDs))
	}
	if after != "" {
		q = q.Where("(e.code > (SELECT c.code FROM employees AS c WHERE c.id = ?) OR (e.code = (SELECT c.code FROM employees AS c WHERE c.id = ?) AND e.id > ?))",
			after, after, after)
	}
	if err := q.ModelTableExpr("employees AS e").ColumnExpr("e.*").OrderExpr("e.code, e.id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Employee]{}, err
	}
	return pageOf(rows, limit, func(e Employee) string { return e.ID }), nil
}

// ActiveEmployees returns every active employee of the marina (department filter optional), ordered by code.
func ActiveEmployees(ctx context.Context, db bun.IDB, s MarinaScope, department string) ([]Employee, error) {
	var rows []Employee
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("active = ?", true)
	if department != "" {
		q = q.Where("department = ?", department)
	}
	if err := q.OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// EmployeeByUser returns the employee linked to a user in the marina (ErrNotFound when none).
func EmployeeByUser(ctx context.Context, db bun.IDB, s MarinaScope, userID string) (*Employee, error) {
	if !ValidID(userID) {
		return nil, ErrNotFound
	}
	e := new(Employee)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("user_id = ?", userID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// EmployeeQualifications returns the qualification ids per employee for the given employees (all when ids is nil).
func EmployeeQualifications(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string][]string, error) {
	var rows []EmployeeQualification
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if ids != nil {
		if len(ids) == 0 {
			return map[string][]string{}, nil
		}
		q = q.Where("employee_id IN (?)", bun.List(ids))
	}
	if err := q.OrderExpr("employee_id, qualification_id").Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.EmployeeID] = append(out[r.EmployeeID], r.QualificationID)
	}
	return out, nil
}

// ReplaceEmployeeQualifications replaces the qualifications of an employee.
func ReplaceEmployeeQualifications(ctx context.Context, tx bun.IDB, s MarinaScope, employeeID string, qualIDs []string) error {
	if _, err := tx.NewDelete().TableExpr("employee_qualifications").Where("marina_id = ?", s.ID()).
		Where("employee_id = ?", employeeID).Exec(ctx); err != nil {
		return err
	}
	rows := make([]EmployeeQualification, 0, len(qualIDs))
	for _, q := range planUniqSorted(qualIDs) {
		rows = append(rows, EmployeeQualification{EmployeeID: employeeID, QualificationID: q, MarinaID: s.ID()})
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// EmployeeCalendars returns the weekly minutes (ISO weekday 1..7 at index 0..6) per employee.
func EmployeeCalendars(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string][7]int, error) {
	var rows []EmployeeCalendar
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if ids != nil {
		if len(ids) == 0 {
			return map[string][7]int{}, nil
		}
		q = q.Where("employee_id IN (?)", bun.List(ids))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string][7]int{}
	for _, r := range rows {
		if r.Weekday < 1 || r.Weekday > 7 {
			continue
		}
		w := out[r.EmployeeID]
		w[r.Weekday-1] = r.Minutes
		out[r.EmployeeID] = w
	}
	return out, nil
}

// ReplaceEmployeeCalendar writes the seven weekdays of an employee (0 minutes rows are kept: explicit day off).
func ReplaceEmployeeCalendar(ctx context.Context, tx bun.IDB, s MarinaScope, employeeID string, week [7]int) error {
	if _, err := tx.NewDelete().TableExpr("employee_calendars").Where("marina_id = ?", s.ID()).
		Where("employee_id = ?", employeeID).Exec(ctx); err != nil {
		return err
	}
	rows := make([]EmployeeCalendar, 0, 7)
	for i, m := range week {
		rows = append(rows, EmployeeCalendar{EmployeeID: employeeID, Weekday: i + 1, MarinaID: s.ID(), Minutes: m})
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// QualificationsExist reports whether every id is a yard qualification of the marina.
func QualificationsExist(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (bool, error) {
	want := planUniqSorted(ids)
	if len(want) == 0 {
		return true, nil
	}
	for _, id := range want {
		if !ValidID(id) {
			return false, nil
		}
	}
	n, err := db.NewSelect().TableExpr("yard_qualifications").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(want)).Count(ctx)
	return n == int64(len(want)), err
}

// LiftOperatorQualified returns the employees (of ids) holding at least one qualification with lift_operator.
func LiftOperatorQualified(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var got []string
	if err := db.NewSelect().TableExpr("employee_qualifications AS eq").ColumnExpr("DISTINCT eq.employee_id").
		Join("JOIN yard_qualifications AS q ON q.id = eq.qualification_id AND q.marina_id = eq.marina_id").
		Where("eq.marina_id = ?", s.ID()).Where("eq.employee_id IN (?)", bun.List(ids)).Where("q.lift_operator = ?", true).
		Scan(ctx, &got); err != nil {
		return nil, err
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// UserExists reports whether a user is an active member of the scope's marina (a marina-level grant or an
// organization-wide grant of its organization): the phone identity of an employee (INV-73).
func UserExists(ctx context.Context, db bun.IDB, s MarinaScope, userID string) (bool, error) {
	_, err := MarinaMemberUser(ctx, db, s, userID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// InsertAbsence inserts an absence of an employee.
func InsertAbsence(ctx context.Context, tx bun.IDB, s MarinaScope, a *EmployeeAbsence) error {
	a.MarinaID = s.ID()
	a.CreatedAt, a.UpdatedAt = UTC(a.CreatedAt), UTC(a.UpdatedAt)
	_, err := tx.NewInsert().Model(a).Exec(ctx)
	return err
}

// GetAbsence returns an absence of an employee of the marina.
func GetAbsence(ctx context.Context, db bun.IDB, s MarinaScope, employeeID, id string) (*EmployeeAbsence, error) {
	if !ValidID(id) || !ValidID(employeeID) {
		return nil, ErrNotFound
	}
	a := new(EmployeeAbsence)
	if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("employee_id = ?", employeeID).
		Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// DeleteAbsence deletes an absence.
func DeleteAbsence(ctx context.Context, tx bun.IDB, s MarinaScope, id string) error {
	_, err := tx.NewDelete().TableExpr("employee_absences").Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// ListAbsences returns the absences of the employees (all when ids is nil) overlapping [from, to] (zero = open).
func ListAbsences(ctx context.Context, db bun.IDB, s MarinaScope, ids []string, from, to calendar.Date) ([]EmployeeAbsence, error) {
	var rows []EmployeeAbsence
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if ids != nil {
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("employee_id IN (?)", bun.List(ids))
	}
	if !from.IsZero() {
		q = q.Where("to_on >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("from_on <= ?", to)
	}
	if err := q.OrderExpr("employee_id, from_on, id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// uniqSorted returns the distinct non-empty values in ascending order.
func planUniqSorted(v []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(v))
	for _, x := range v {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
