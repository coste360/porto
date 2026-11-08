package workorder

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Job names (m10-plan §7, §16.1).
const (
	// JobTimersGuard flags running timers older than 16 h for the manager (every 15 minutes; never closes them).
	JobTimersGuard = "yard.timers_guard"
)

// Offline kinds of the yard operations (m10-plan §7, spec 29.2), as in the web OFFLINE_OPERATION_KINDS.
const (
	OfflineKindTimesheet    = "yard.timesheet"     // startTimer, stopTimer, createTimeEntry
	OfflineKindActivityDone = "yard.activity_done" // updateTaskProgress, uploadTaskPhoto
)

// Limits of time entries (m10-plan §7, D24).
const (
	MaxEntryHoursMilli = 16000 // 16 h
	ManualStepMilli    = 250   // 0.25 h
)

// Other limits.
const (
	maxChecklist     = 40
	maxTitleRunes    = 200
	maxNoteRunes     = 500
	maxNotesRunes    = 2000
	maxReasonRunes   = 500
	maxPositionRunes = 200
	maxLabelRunes    = 200
	maxClientRef     = 64
	myWorkDays       = 7
	// OfflineBefore and OfflineAfter bound the device times of an offline replay around its queued instant.
	OfflineBefore = 48 * time.Hour
	OfflineAfter  = 5 * time.Minute
)

// Warnings returned with an order, a task or an entry (never errors).
const (
	WarnTasksIncomplete  = "tasks_incomplete"
	WarnDependenciesOpen = "dependencies_open"
)

// BookingCanceller cancels the future lifting bookings of a cancelled work order in the caller's transaction
// (m10-plan §5.2, D19). Implemented by lifting.Service and wired in internal/app, so that workorder never imports
// lifting.
type BookingCanceller interface {
	CancelForWorkOrderTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, workOrderID string, from calendar.Date) error
}

// ChecklistItem is one entry of a task checklist (work_tasks.checklist).
type ChecklistItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
}

// ChecklistOf decodes a stored checklist.
func ChecklistOf(j store.JSON) []ChecklistItem {
	out := []ChecklistItem{}
	_ = json.Unmarshal(j, &out)
	return out
}

// rateSnapshot is work_orders.rate_snapshot: the unit prices frozen at the creation (budget items and the labour
// item of every active qualification), the labour item per qualification and the VAT of the budget items.
type rateSnapshot struct {
	Units       map[string]money.Cents `json:"units"`
	LabourItems map[string]string      `json:"labour_items"`
	VAT         map[string]account.VAT `json:"vat"`
}

func rateSnapshotOf(j store.JSON) rateSnapshot {
	var r rateSnapshot
	_ = json.Unmarshal(j, &r)
	if r.Units == nil {
		r.Units = map[string]money.Cents{}
	}
	if r.LabourItems == nil {
		r.LabourItems = map[string]string{}
	}
	if r.VAT == nil {
		r.VAT = map[string]account.VAT{}
	}
	return r
}

// OrderView is an order with its budget lines and the figures of its card.
type OrderView struct {
	Row            store.WorkOrder
	Lines          []store.WorkOrderLine
	Items          map[string]store.Item
	VesselName     string
	HoursApproved  qty.Milli
	HoursEstimated qty.Milli
	Warnings       []string
}

// OrderFilter filters the order list.
type OrderFilter struct {
	Status     string
	OpenOnly   bool
	CustomerID string
	VesselID   string
	Q          string
	Cursor     string
	Limit      int
}

// ManualLine is a budget line typed by hand on an order without a quote.
type ManualLine struct {
	ItemID      string
	WorkTypeID  string
	Description string
	Qty         qty.Milli
	Unit        money.Cents
	Discount    money.Cents
}

// Selection is a package or a single item chosen for an order without a quote (priced by the quote line engine).
type Selection = price.Selection

// OrderInput creates an order without a quote.
type OrderInput struct {
	CustomerID        string
	VesselID          string
	WorkTypeID        string
	ResponsibleUserID string
	BillingMode       string
	PlannedStartOn    calendar.Date
	PlannedDeliveryOn calendar.Date
	AllocationID      string
	PositionNote      string
	Notes             string
	ManualLines       []ManualLine
	Selections        []Selection
	JobInputs         map[string]price.VariableValue
}

// OrderPatch changes an order (nil = unchanged; Clear* empties the field).
type OrderPatch struct {
	ResponsibleUserID *string
	PlannedStartOn    *calendar.Date
	PlannedDeliveryOn *calendar.Date
	AllocationID      *string
	PositionNote      *string
	Notes             *string
	WorkTypeID        *string
	Consumed          []ConsumedPatch
}

// ConsumedPatch sets the consumed quantity of a budget line (nil = the budget quantity).
type ConsumedPatch struct {
	LineID   string
	Consumed *int64
}

// BoardCard is a card of the board.
type BoardCard struct {
	Order          store.WorkOrder
	VesselName     string
	LOACm          *int64
	DelayDays      *int
	HoursApproved  qty.Milli
	HoursEstimated qty.Milli
}

// BoardColumn is a status column of the board.
type BoardColumn struct {
	Status string
	Cards  []BoardCard
}

// Board is the board "barche in lavorazione".
type Board struct {
	Today   calendar.Date
	Columns []BoardColumn
}

// TaskView is a task with its predecessors and hours.
type TaskView struct {
	Row           store.WorkTask
	DependsOn     []string
	HoursApproved qty.Milli
	HoursAssigned qty.Milli
	Warnings      []string
}

// TaskInput creates a task.
type TaskInput struct {
	Title           string
	QualificationID string
	WorkTypeID      string
	EstimatedHours  qty.Milli
	Checklist       []string
	DependsOn       []string
}

// TaskPatch changes a task.
type TaskPatch struct {
	Title           *string
	QualificationID *string
	WorkTypeID      *string
	EstimatedHours  *qty.Milli
	Checklist       *[]ChecklistItem
	Status          *string
}

// ChecklistMark marks a checklist item done or not.
type ChecklistMark struct {
	Key  string
	Done bool
}

// ProgressInput is updateTaskProgress (offline kind yard.activity_done).
type ProgressInput struct {
	ClientRef  string
	ProgressBP *int64
	Done       *bool
	Checklist  []ChecklistMark
	RecordedAt *time.Time
}

// FileUpload is a task photo or document (raw body).
type FileUpload struct {
	ContentType string
	Data        []byte
	ClientRef   string // Idempotency-Key of the upload (offline replay)
}

// ExternalCostRow is an external cost of an order (the handlers read it without the store package).
type ExternalCostRow = store.WorkOrderExternalCost

// TaskFile is a task file.
type TaskFile = store.WorkTaskFileRow

// FileLink is a signed link to a task file.
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

// EntryView is a time entry with its order number and warnings.
type EntryView struct {
	Row         store.Timesheet
	OrderNumber string
	Warnings    []string
}

// EntryFilter filters the entry list.
type EntryFilter struct {
	EmployeeID  string
	WorkOrderID string
	Status      string
	From, To    calendar.Date
	Cursor      string
	Limit       int
}

// TimerStart is startTimer.
type TimerStart struct {
	ClientRef   string
	EmployeeID  string
	TaskID      string
	WorkOrderID string
	StartedAt   *time.Time
}

// TimerStop is stopTimer.
type TimerStop struct {
	ClientRef string
	EndedAt   *time.Time
	Note      *string
}

// EntryInput is createTimeEntry (hours, or started and ended).
type EntryInput struct {
	ClientRef   string
	EmployeeID  string
	TaskID      string
	WorkOrderID string
	WorkDate    calendar.Date
	Hours       *qty.Milli
	StartedAt   *time.Time
	EndedAt     *time.Time
	Note        string
}

// EntryPatch changes a submitted or rejected entry (resubmits it).
type EntryPatch struct {
	TaskID    *string
	WorkDate  *calendar.Date
	Hours     *qty.Milli
	StartedAt *time.Time
	EndedAt   *time.Time
	Note      *string
}

// EntryVersion is an entry and its expected version (batch approval).
type EntryVersion struct {
	ID      string
	Version int64
}

// MyAssignment is an assignment of the caller's employee.
type MyAssignment struct {
	Row         store.MyAssignmentRow
	OrderNumber string
	Position    string
	Checklist   []ChecklistItem
}

// MyWork is the phone view of the caller's employee.
type MyWork struct {
	Today        calendar.Date
	EmployeeID   string
	Assignments  []MyAssignment
	RunningTimer *EntryView
}

// MovementInput is createStockMovement.
type MovementInput struct {
	WorkOrderID string
	TaskID      string
	ItemID      string
	Kind        string
	Qty         qty.Milli
	UnitCost    *money.Cents
	UnitPrice   *money.Cents
	MovedOn     calendar.Date
	Note        string
}

// MovementFilter filters the movement list.
type MovementFilter struct {
	WorkOrderID string
	ItemID      string
	From, To    calendar.Date
	Cursor      string
	Limit       int
}

// MovementView is a movement with the item code.
type MovementView struct {
	Row      store.StockMovement
	ItemCode string
}

// ExternalInput creates an external cost.
type ExternalInput struct {
	SupplierName string
	Reference    string
	IncurredOn   calendar.Date
	Description  string
	WorkTypeID   string
	Cost         money.Cents
	Billable     *bool
	Price        *money.Cents
	ItemID       string
}

// ExternalPatch changes an external cost.
type ExternalPatch struct {
	SupplierName *string
	Reference    *string
	IncurredOn   *calendar.Date
	Description  *string
	WorkTypeID   *string
	Cost         *money.Cents
	Billable     *bool
	Price        *money.Cents
	ItemID       *string
}

// AccountView is the final account of an order with what is already charged.
type AccountView struct {
	Order          store.WorkOrder
	Account        account.Account
	Buckets        []VATBucket
	ChargedGross   money.Cents
	RemainingGross money.Cents
	Items          map[string]store.Item
	Descriptions   map[string]string // line key → description (external costs, budget lines)
}

// VATBucket sums account lines per VAT rate and nature.
type VATBucket struct {
	RateBP int64
	Nature string
	Net    money.Cents
	VAT    money.Cents
	Gross  money.Cents
}

// Billing kinds and statuses (work_order_billings).
const (
	BillingAdvance    = store.WOBillingAdvance
	BillingProgress   = store.WOBillingProgress
	BillingFinal      = store.WOBillingFinal
	BillingAdjustment = store.WOBillingAdjustment
)

// BillingInput is createWorkOrderBilling.
type BillingInput struct {
	Kind      string
	PercentBP *int64
	Amount    *money.Cents
	DueOn     calendar.Date
}

// BillingLine is a line of a billing preview or of the charge created.
type BillingLine struct {
	PlanKey     string
	Description string
	Net         money.Cents
	VAT         money.Cents
	Gross       money.Cents
	VATRateBP   int64
}

// BillingResult is the outcome (or the preview) of a billing.
type BillingResult struct {
	DryRun           bool
	Kind             string
	Gross            money.Cents
	Lines            []BillingLine
	Billing          *BillingView
	ChargeID         string
	CorrectsChargeID string
}

// BillingView is a billing with its charge number.
type BillingView struct {
	Row          store.WorkOrderBilling
	ChargeNumber string
	PercentBP    *int64
	Amount       *money.Cents
}
