package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// ---- R-maintenance (m9-plan §11) ----

// Maintenance task states (maintenance_tasks.status).
const (
	TaskOpen       = "open"
	TaskScheduled  = "scheduled"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskCancelled  = "cancelled"
)

// taskPending reports a task not done nor cancelled.
func taskPending(status string) bool {
	return status == TaskOpen || status == TaskScheduled || status == TaskInProgress
}

// MaintenanceTask is a maintenance_tasks row as the report reads it (no title, description or notes: free text).
type MaintenanceTask struct {
	ID              string
	ElementType     string
	ElementID       string
	ElementCode     string
	Intervention    string
	Source          string
	Priority        string
	Status          string
	DueOn           calendar.Date // zero when none
	DoneOn          calendar.Date // marina-local date of done_at; zero when not done
	CreatedOn       calendar.Date // marina-local date of created_at
	ClosedOn        calendar.Date // marina-local date of the last update of a cancelled task
	InternalMinutes int64
	ExternalCost    money.Cents
}

// MaintenanceInput is the input of BuildMaintenance.
type MaintenanceInput struct {
	From, To    calendar.Date
	Today       calendar.Date
	GroupBy     string // status, priority, element_type, intervention, month
	Status      string
	ElementType string
	OverdueOnly bool
	Full        bool // reports.all: external costs
	Tasks       []MaintenanceTask
}

// MaintenanceTotals (OpenAPI MaintenanceReportTotals).
type MaintenanceTotals struct {
	Tasks           int64
	Open            int64
	Scheduled       int64
	InProgress      int64
	Done            int64
	Cancelled       int64
	Overdue         int64
	InternalMinutes int64
	ExternalCost    Opt[money.Cents]
}

// MaintenanceRecord (OpenAPI MaintenanceReportRecord).
type MaintenanceRecord struct {
	TaskID          string
	ElementType     string
	ElementID       string
	ElementCode     string
	Intervention    string
	Source          string
	Priority        string
	Status          string
	DueOn           calendar.Date
	DoneOn          calendar.Date
	Overdue         bool  // currently overdue (pending with due_on before today)
	OverdueDays     int64 // pending: today − due_on; done: done_on − due_on (lateness), 0 when on time
	InternalMinutes int64
	ExternalCost    Opt[money.Cents]
	Ref             Ref
}

// MaintenanceResult is the maintenance report.
type MaintenanceResult = Result[MaintenanceTotals, MaintenanceRecord]

// TaskInPeriod reports whether a task belongs to [from, to]: created by to and not closed before from (a pending
// task stays in every period after its creation; a done or cancelled one until it was closed), or due in the
// period.
func TaskInPeriod(t *MaintenanceTask, from, to calendar.Date) bool {
	if !t.DueOn.IsZero() && inPeriod(t.DueOn, from, to) {
		return true
	}
	if t.CreatedOn.After(to) {
		return false
	}
	switch t.Status {
	case TaskDone:
		return !t.DoneOn.Before(from)
	case TaskCancelled:
		return !t.ClosedOn.Before(from)
	}
	return true
}

// FoldMaintenance is the only producer of maintenance totals.
func FoldMaintenance(recs []MaintenanceRecord, full bool) MaintenanceTotals {
	t := MaintenanceTotals{}
	var cost money.Cents
	for i := range recs {
		r := &recs[i]
		t.Tasks++
		switch r.Status {
		case TaskOpen:
			t.Open++
		case TaskScheduled:
			t.Scheduled++
		case TaskInProgress:
			t.InProgress++
		case TaskDone:
			t.Done++
		case TaskCancelled:
			t.Cancelled++
		}
		if r.Overdue {
			t.Overdue++
		}
		t.InternalMinutes += r.InternalMinutes
		cost += r.ExternalCost.Value
	}
	if full {
		t.ExternalCost = Some(cost)
	}
	return t
}

// BuildMaintenance builds the maintenance report: one record per task of the period (TaskInPeriod) matching the
// filters, overdue first (most days first), then by due date and id. Facts: maintenance_overdue on today.
func BuildMaintenance(in MaintenanceInput) (MaintenanceResult, Facts) {
	recs := make([]MaintenanceRecord, 0, len(in.Tasks))
	for i := range in.Tasks {
		t := &in.Tasks[i]
		if !TaskInPeriod(t, in.From, in.To) || (in.Status != "" && t.Status != in.Status) ||
			(in.ElementType != "" && t.ElementType != in.ElementType) {
			continue
		}
		r := MaintenanceRecord{
			TaskID: t.ID, ElementType: t.ElementType, ElementID: t.ElementID, ElementCode: t.ElementCode,
			Intervention: t.Intervention, Source: t.Source, Priority: t.Priority, Status: t.Status, DueOn: t.DueOn,
			DoneOn: t.DoneOn, InternalMinutes: t.InternalMinutes,
			Ref: Ref{Type: "maintenance_task", ID: t.ID, Label: t.ElementCode},
		}
		if !t.DueOn.IsZero() {
			switch {
			case taskPending(t.Status) && t.DueOn.Before(in.Today):
				r.Overdue, r.OverdueDays = true, int64(t.DueOn.DaysUntil(in.Today))
			case t.Status == TaskDone && !t.DoneOn.IsZero() && t.DoneOn.After(t.DueOn):
				r.OverdueDays = int64(t.DueOn.DaysUntil(t.DoneOn))
			}
		}
		if in.OverdueOnly && !r.Overdue {
			continue
		}
		if in.Full {
			r.ExternalCost = Some(t.ExternalCost)
		}
		recs = append(recs, r)
	}
	slices.SortStableFunc(recs, func(a, b MaintenanceRecord) int {
		if a.Overdue != b.Overdue {
			if a.Overdue {
				return -1
			}
			return 1
		}
		if a.OverdueDays != b.OverdueDays {
			if a.OverdueDays > b.OverdueDays {
				return -1
			}
			return 1
		}
		if c := cmpDate(a.DueOn, b.DueOn); c != 0 {
			return c
		}
		return compareKeys([]string{a.TaskID}, []string{b.TaskID})
	})
	res := MaintenanceResult{Records: recs, Totals: FoldMaintenance(recs, in.Full)}
	fold := func(rs []MaintenanceRecord) MaintenanceTotals { return FoldMaintenance(rs, in.Full) }
	key := map[string]func(MaintenanceRecord) string{
		"status":       func(r MaintenanceRecord) string { return r.Status },
		"priority":     func(r MaintenanceRecord) string { return r.Priority },
		"element_type": func(r MaintenanceRecord) string { return orNone(r.ElementType) },
		"intervention": func(r MaintenanceRecord) string { return orNone(r.Intervention) },
		"month": func(r MaintenanceRecord) string {
			if r.DueOn.IsZero() {
				return "none"
			}
			return MonthKey(r.DueOn)
		},
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(recs, func(r MaintenanceRecord) []string { return []string{key(r)} }, fold)
	}
	facts := Daily{}
	for i := range recs {
		if recs[i].Overdue {
			facts.Add(in.Today, 1, 0)
		}
	}
	return res, Facts{IndMaintenanceOverdue: facts}
}
