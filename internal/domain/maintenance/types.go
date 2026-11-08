// Package maintenance is the maintenance service (spec 20.1, 20.2; m9-plan §8): the equipment registry, plans
// with targets and periodicity, the hourly maintenance.schedule job (tasks unique per plan, element and due date,
// plus deadlines rows), tasks with their state table, scheduling with maintenance occupancy on berths and land
// resources through occupancy.AllocateTx (never overriding a vessel: 409 maintenance_conflicts with alternatives),
// completion (offline safe: never releases nights, INV-71), field reports and the maintenance map lens.
// Permissions: maintenance.manage; maintenance.report for field reports and the assignee's own tasks.
package maintenance

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// JobSchedule is the name of the hourly plan scheduling job.
const JobSchedule = "maintenance.schedule"

// DeadlineKind is deadlines.kind of a maintenance task due date (source_type store.DeadlineSourceMaintenanceTask).
const DeadlineKind = "maintenance_due"

// Element is a polymorphic element reference (validated in Go against the marina's map rows and equipment).
type Element struct {
	Type string // store.Element*
	ID   string
}

// EquipmentInput creates equipment.
type EquipmentInput struct {
	Code, Name, Kind string
	MapID            string
	X, Y             *int64
	CapacityKg       *int64
	Active           *bool
}

// EquipmentPatch changes equipment; Clear* set a field to null.
type EquipmentPatch struct {
	Code, Name, Kind *string
	MapID            *string
	ClearPosition    bool
	X, Y             *int64
	CapacityKg       *int64
	ClearCapacity    bool
	Active           *bool
}

// EquipmentView is equipment with its out-of-service flag (an active out-of-service task covers today).
type EquipmentView struct {
	Row          store.Equipment
	OutOfService bool
}

// ChecklistEntry is one line of a plan checklist.
type ChecklistEntry struct {
	Key, Label string
}

// ChecklistResult is the outcome of one checklist line.
type ChecklistResult struct {
	Key  string
	Done bool
	Note string
}

// PlanInput creates or replaces a plan.
type PlanInput struct {
	Name              string
	Intervention      string
	InterventionLabel string
	Targets           []Element
	Periodicity       due.Periodicity
	MeterID           string // cycles
	LeadDays          int
	AssigneeUserID    string
	SupplierName      string
	Checklist         []ChecklistEntry
	RequiresFree      bool
	Nights            int
	Active            *bool
}

// PlanView is a plan with its targets.
type PlanView struct {
	Row     store.MaintenancePlan
	Targets []store.MaintenancePlanTarget
}

// TaskFilter filters the task list.
type TaskFilter struct {
	Status, Priority, ElementType, ElementID, PlanID, AssigneeUserID string
	OverdueOnly                                                      bool
	Cursor                                                           string
	Limit                                                            int
}

// TaskInput creates a task from the office.
type TaskInput struct {
	Element        *Element
	Title          string
	Description    string
	Priority       string
	DueOn          *calendar.Date
	AssigneeUserID string
	OutOfService   bool
	FollowsTaskID  string
	MapID          string
	X, Y           *int64
}

// TaskPatch changes a task; Clear* set a field to null.
type TaskPatch struct {
	Title, Description, Priority *string
	DueOn                        *calendar.Date
	ClearDue                     bool
	AssigneeUserID               *string
	ClearAssignee                bool
	SupplierRef                  *string
	OutOfService                 *bool
}

// ScheduleInput schedules a task on [From, To] (inclusive nights).
type ScheduleInput struct {
	From, To       calendar.Date
	OutOfService   *bool
	OccupyBerthIDs []string // structure tasks (D24)
}

// CompleteInput completes a task (offline kind maintenance.outcome).
type CompleteInput struct {
	Outcome           string
	Notes             string
	Checklist         []ChecklistResult
	InternalMinutes   *int64
	ExternalCostCents *int64
	SupplierRef       string
	ClientRef         string
}

// ReportInput is a field report (offline kind maintenance.report).
type ReportInput struct {
	ClientRef   string
	Element     *Element
	MapID       string
	X, Y        *int64
	Title       string
	Description string
	Priority    string
}

// TaskView is a task with its files and the overdue flag.
type TaskView struct {
	Row     store.MaintenanceTask
	Files   []TaskFile
	Overdue bool
	Code    string // display code of the element
}

// TaskFile is a photo or document of a task with its content type.
type TaskFile = store.MaintenanceTaskFileWithFile

// FileLink is a signed, short-lived link to a task file.
type FileLink struct {
	URL       string
	ExpiresAt time.Time
}

// File is a decrypted task file.
type File struct {
	Content     []byte
	ContentType string
	FileName    string
}

// Conflict is one entry of ConflictsError.
type Conflict struct {
	AllocationID string
	VesselID     string
	ContractID   string
	ResourceType string
	ResourceID   string
	From, To     calendar.Date
	Alternatives []occupancy.Solution
}

// MapEntry is one element of the maintenance lens.
type MapEntry struct {
	ElementType, ElementID string
	Open, Overdue          int
	OutOfService           bool
}

// FileUpload is an uploaded photo or document.
type FileUpload struct {
	ContentType string
	Data        []byte
}

// TemplateMaintenanceDue is the notification template of a maintenance task coming due (values task_title,
// element_code, due_on, link, marina_name).
const TemplateMaintenanceDue = "maintenance_due"
