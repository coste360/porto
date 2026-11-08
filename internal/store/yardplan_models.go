package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M10 planner tables (internal/migrations/*/00021_work_orders.sql, m10-plan §6, §15). Owned by the
// PLANNER builder after the contract phase. Employee names are personal data: never in events or logs (INV-70).

// Absence kinds (m10-plan §6.1).
const (
	AbsenceVacation = "vacation"
	AbsenceSick     = "sick"
	AbsenceLeave    = "leave"
	AbsenceOther    = "other"
)

// Employee is a row of employees.
type Employee struct {
	bun.BaseModel `bun:"table:employees"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Code            string    `bun:"code"`
	FirstName       string    `bun:"first_name"`
	LastName        string    `bun:"last_name"`
	UserID          string    `bun:"user_id,nullzero"`
	Department      string    `bun:"department"`
	HourlyCostCents int64     `bun:"hourly_cost_cents"`
	Active          bool      `bun:"active"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// EmployeeQualification is a row of employee_qualifications.
type EmployeeQualification struct {
	bun.BaseModel `bun:"table:employee_qualifications"`

	EmployeeID      string `bun:"employee_id,pk"`
	QualificationID string `bun:"qualification_id,pk"`
	MarinaID        string `bun:"marina_id"`
}

// EmployeeCalendar is a row of employee_calendars (minutes of an ISO weekday, 1 = Monday).
type EmployeeCalendar struct {
	bun.BaseModel `bun:"table:employee_calendars"`

	EmployeeID string `bun:"employee_id,pk"`
	Weekday    int    `bun:"weekday,pk"`
	MarinaID   string `bun:"marina_id"`
	Minutes    int    `bun:"minutes"`
}

// EmployeeAbsence is a row of employee_absences (to_on inclusive; MinutesPerDay nil = whole day).
type EmployeeAbsence struct {
	bun.BaseModel `bun:"table:employee_absences"`

	ID            string        `bun:"id,pk"`
	MarinaID      string        `bun:"marina_id"`
	EmployeeID    string        `bun:"employee_id"`
	FromOn        calendar.Date `bun:"from_on"`
	ToOn          calendar.Date `bun:"to_on"`
	Kind          string        `bun:"kind"`
	MinutesPerDay *int          `bun:"minutes_per_day"`
	Note          string        `bun:"note"`
	CreatedAt     time.Time     `bun:"created_at"`
	UpdatedAt     time.Time     `bun:"updated_at"`
}

// TaskAssignment is a row of task_assignments (planned hours of a task, employee and day).
type TaskAssignment struct {
	bun.BaseModel `bun:"table:task_assignments"`

	ID                  string        `bun:"id,pk"`
	MarinaID            string        `bun:"marina_id"`
	TaskID              string        `bun:"task_id"`
	EmployeeID          string        `bun:"employee_id"`
	WorkDate            calendar.Date `bun:"work_date"`
	HoursMilli          int64         `bun:"hours_milli"`
	ForcedQualification bool          `bun:"forced_qualification"`
	Overload            bool          `bun:"overload"`
	CreatedByUserID     string        `bun:"created_by_user_id,nullzero"`
	Version             int           `bun:"version"`
	CreatedAt           time.Time     `bun:"created_at"`
	UpdatedAt           time.Time     `bun:"updated_at"`
}
