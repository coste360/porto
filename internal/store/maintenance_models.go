package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M9 maintenance tables (internal/migrations/*/00019_maintenance_utilities.sql, m9-plan §8, §13).
// Owned by the UTILITIES builder after the contract phase; PLANNER reads them for the maintenance report.

// Maintenance enums (m9-plan §8).
const (
	ElementBerth        = "berth"
	ElementStructure    = "structure"
	ElementDynamicArea  = "dynamic_area"
	ElementLandResource = "land_resource"
	ElementPedestal     = "pedestal"
	ElementEquipment    = "equipment"

	PeriodMonths    = "months"
	PeriodCycles    = "cycles"
	PeriodFixedDate = "fixed_date"

	TaskOpen       = "open"
	TaskScheduled  = "scheduled"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskCancelled  = "cancelled"

	TaskSourcePlan   = "plan"
	TaskSourceOffice = "office"
	TaskSourceReport = "report"

	// DeadlineSourceMaintenanceTask is deadlines.source_type of a maintenance task (kind maintenance_due).
	DeadlineSourceMaintenanceTask = "maintenance_task"
)

// Equipment is a row of equipment.
type Equipment struct {
	bun.BaseModel `bun:"table:equipment"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	Code       string    `bun:"code"`
	Name       string    `bun:"name"`
	Kind       string    `bun:"kind"`
	MapID      string    `bun:"map_id,nullzero"`
	XCm        *int64    `bun:"x_cm"`
	YCm        *int64    `bun:"y_cm"`
	CapacityKg *int64    `bun:"capacity_kg"`
	Active     bool      `bun:"active"`
	Version    int       `bun:"version"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// MaintenancePlan is a row of maintenance_plans.
type MaintenancePlan struct {
	bun.BaseModel `bun:"table:maintenance_plans"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	Name              string        `bun:"name"`
	Intervention      string        `bun:"intervention"`
	InterventionLabel string        `bun:"intervention_label,nullzero"`
	PeriodKind        string        `bun:"period_kind"`
	PeriodEvery       *int64        `bun:"period_every"` // months, or meter units in milli
	PeriodMonthDay    string        `bun:"period_month_day,nullzero"`
	PeriodMeterID     string        `bun:"period_meter_id,nullzero"`
	PeriodStartsOn    calendar.Date `bun:"period_starts_on,nullzero"`
	LeadDays          int           `bun:"lead_days"`
	AssigneeUserID    string        `bun:"assignee_user_id,nullzero"`
	SupplierName      string        `bun:"supplier_name,nullzero"`
	Checklist         JSON          `bun:"checklist"`
	RequiresFree      bool          `bun:"requires_free"`
	Nights            int           `bun:"nights"`
	Active            bool          `bun:"active"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}

// MaintenancePlanTarget is a row of maintenance_plan_targets.
type MaintenancePlanTarget struct {
	bun.BaseModel `bun:"table:maintenance_plan_targets"`

	PlanID      string `bun:"plan_id,pk"`
	MarinaID    string `bun:"marina_id"`
	ElementType string `bun:"element_type,pk"`
	ElementID   string `bun:"element_id,pk"`
}

// MaintenanceTask is a row of maintenance_tasks.
type MaintenanceTask struct {
	bun.BaseModel `bun:"table:maintenance_tasks"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	ElementType       string        `bun:"element_type,nullzero"`
	ElementID         string        `bun:"element_id,nullzero"`
	PlanID            string        `bun:"plan_id,nullzero"`
	Intervention      string        `bun:"intervention,nullzero"`
	Source            string        `bun:"source"`
	Title             string        `bun:"title"`
	Description       string        `bun:"description"`
	Priority          string        `bun:"priority"`
	Status            string        `bun:"status"`
	DueOn             calendar.Date `bun:"due_on,nullzero"`
	ScheduledFrom     calendar.Date `bun:"scheduled_from,nullzero"`
	ScheduledTo       calendar.Date `bun:"scheduled_to,nullzero"`
	OutOfService      bool          `bun:"out_of_service"`
	MapID             string        `bun:"map_id,nullzero"`
	PositionXCm       *int64        `bun:"position_x_cm"`
	PositionYCm       *int64        `bun:"position_y_cm"`
	Checklist         JSON          `bun:"checklist"`
	ChecklistResults  JSON          `bun:"checklist_results"`
	Outcome           string        `bun:"outcome,nullzero"`
	OutcomeNotes      string        `bun:"outcome_notes,nullzero"`
	OutcomeClientRef  string        `bun:"outcome_client_ref,nullzero"`
	InternalMinutes   *int64        `bun:"internal_minutes"`
	ExternalCostCents *int64        `bun:"external_cost_cents"`
	SupplierRef       string        `bun:"supplier_ref,nullzero"`
	AssigneeUserID    string        `bun:"assignee_user_id,nullzero"`
	ReportedByUserID  string        `bun:"reported_by_user_id,nullzero"`
	AllocationIDs     JSON          `bun:"allocation_ids"`
	FollowsTaskID     string        `bun:"follows_task_id,nullzero"`
	ClientRef         string        `bun:"client_ref,nullzero"`
	DoneAt            time.Time     `bun:"done_at,nullzero"`
	DoneByUserID      string        `bun:"done_by_user_id,nullzero"`
	CancelReason      string        `bun:"cancel_reason,nullzero"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}

// MaintenanceTaskFile is a row of maintenance_task_files (photo or document of a task, encrypted stored file).
type MaintenanceTaskFile struct {
	bun.BaseModel `bun:"table:maintenance_task_files"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	TaskID          string    `bun:"task_id"`
	FileID          string    `bun:"file_id"`
	Kind            string    `bun:"kind"` // photo, document
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}
