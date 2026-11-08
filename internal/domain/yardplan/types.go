package yardplan

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/yardplan/auto"
	"github.com/coste360/porto/internal/domain/yardplan/load"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Defaults of the planner (m10-plan §6).
const (
	MaxHorizonDays     = 90
	ProposalStepMilli  = 500 // 0.5 h
	MaxCalendarMinutes = 960
	// DefaultHorizonDays is the horizon of a proposal when the request gives none.
	DefaultHorizonDays = 14
	// MaxRangeDays is the longest period of the planner grid and the load view.
	MaxRangeDays = 93
	// MaxAssignmentHours is the most hours of one assignment (16 h).
	MaxAssignmentHours qty.Milli = 16000
	// DefaultDayMinutes is the calendar of a new employee Monday to Friday (8 h).
	DefaultDayMinutes = 480
)

// Load groupings (getYardLoad group_by).
const (
	GroupEmployee      = "employee"
	GroupDepartment    = "department"
	GroupQualification = "qualification"
	GroupBay           = "bay"
)

// EmployeeFilter filters the employee list (the store's filter: active, department, qualification).
type EmployeeFilter = store.EmployeeFilter

// Absence is an absence of an employee (to_on inclusive; MinutesPerDay nil = whole day).
type Absence = store.EmployeeAbsence

// NoBay is the key and label of the orders without a work position (D22).
const NoBay = "—"

// CalendarDay is the minutes of an ISO weekday (1 = Monday).
type CalendarDay struct {
	Weekday int
	Minutes int
}

// EmployeeView is an employee with qualifications and the weekly calendar.
type EmployeeView struct {
	Row              store.Employee
	QualificationIDs []string
	Week             [7]int
}

// EmployeeInput creates an employee. Calendar nil = Monday to Friday 8 h.
type EmployeeInput struct {
	Code, FirstName, LastName string
	UserID                    string
	Department                string
	HourlyCostCents           *int64
	Active                    *bool
	QualificationIDs          []string
	Calendar                  []CalendarDay
}

// EmployeePatch changes an employee; nil fields are kept. ClearUser unlinks the user.
type EmployeePatch struct {
	Code, FirstName, LastName *string
	UserID                    *string
	ClearUser                 bool
	Department                *string
	HourlyCostCents           *int64
	Active                    *bool
	QualificationIDs          *[]string
}

// AbsenceInput adds an absence (MinutesPerDay nil = whole day).
type AbsenceInput struct {
	From, To      calendar.Date
	Kind          string
	MinutesPerDay *int
	Note          string
}

// AssignmentChange sets the hours of (task, employee, date); 0 deletes the assignment.
type AssignmentChange struct {
	TaskID      string
	EmployeeID  string
	Date        calendar.Date
	Hours       qty.Milli
	TaskVersion int64
}

// BatchInput is putAssignments.
type BatchInput struct {
	Changes            []AssignmentChange
	ForceQualification bool
	AllowOverload      bool
}

// AssignmentView is an assignment with its work order.
type AssignmentView struct {
	Row         store.TaskAssignment
	WorkOrderID string
}

// TaskVersion is the new version of a task touched by a batch.
type TaskVersion struct {
	TaskID  string
	Version int
}

// BatchResult is the outcome of putAssignments: the current assignments of the touched tasks and their versions.
type BatchResult struct {
	Assignments []AssignmentView
	Tasks       []TaskVersion
}

// PlannerDay is one cell of the planner grid.
type PlannerDay struct {
	Date    calendar.Date
	Cell    load.Cell
	Absence string // absence kind covering the day, "" when none
}

// PlannerRow is one employee of the grid.
type PlannerRow struct {
	Employee         store.Employee
	QualificationIDs []string
	Days             []PlannerDay
}

// PlannerTask is an open task of an open order (the tray shows those with Remaining > Assigned).
type PlannerTask struct {
	Task      store.PlanTask
	Number    string
	Remaining qty.Milli
	Assigned  qty.Milli // assigned from today on
	DependsOn []string
}

// PlannerView is the planner grid.
type PlannerView struct {
	From, To, Today calendar.Date
	Rows            []PlannerRow
	Assignments     []AssignmentView
	Tasks           []PlannerTask
}

// LoadCell is the load of a row in a period.
type LoadCell struct {
	PeriodStart calendar.Date
	Cell        load.Cell
}

// LoadRow is one group of the load view.
type LoadRow struct {
	Key, Label string
	Cells      []LoadCell
	Total      load.Cell
}

// LoadView is getYardLoad.
type LoadView struct {
	From, To        calendar.Date
	GroupBy, Period string
	Rows            []LoadRow
}

// AtRiskOrder is an order at risk.
type AtRiskOrder struct {
	WorkOrderID string
	Number      string
	VesselName  string
	Delivery    calendar.Date
	Late        bool
	Risks       []load.Risk
}

// AtRiskView is getAtRiskOrders.
type AtRiskView struct {
	Today calendar.Date
	Items []AtRiskOrder
}

// ProposalInput is proposeYardPlan (zero From = tomorrow, 0 horizon = DefaultHorizonDays, nil orders = all open).
type ProposalInput struct {
	From         calendar.Date
	HorizonDays  int
	WorkOrderIDs []string
}

// ProposalView is the auto-plan proposal, ready to be confirmed through putAssignments.
type ProposalView struct {
	From, To calendar.Date
	Changes  []AssignmentChange
	Unplaced []auto.Unplaced
}
