package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M10 work order tables (internal/migrations/*/00021_work_orders.sql, m10-plan §5, §7-§10, §15).
// Owned by the ORDERS builder after the contract phase; PLANNER and the reports read them.

// Work order enums (m10-plan §5, §7, §8, §10).
const (
	WorkOrderPlanned    = "planned"
	WorkOrderInProgress = "in_progress"
	WorkOrderOnHold     = "on_hold"
	WorkOrderCompleted  = "completed"
	WorkOrderDelivered  = "delivered"
	WorkOrderInvoiced   = "invoiced"
	WorkOrderCancelled  = "cancelled"

	WorkLineOriginQuote     = "quote"
	WorkLineOriginVariation = "variation"
	WorkLineOriginManual    = "manual"

	WorkTaskTodo       = "todo"
	WorkTaskInProgress = "in_progress"
	WorkTaskDone       = "done"
	WorkTaskCancelled  = "cancelled"

	TimesheetRunning   = "running"
	TimesheetSubmitted = "submitted"
	TimesheetApproved  = "approved"
	TimesheetRejected  = "rejected"
	TimesheetVoided    = "voided"

	TimesheetSourceTimer  = "timer"
	TimesheetSourceManual = "manual"

	StockIssue  = "issue"
	StockReturn = "return"

	WOBillingAdvance    = "advance"
	WOBillingProgress   = "progress"
	WOBillingFinal      = "final"
	WOBillingAdjustment = "adjustment"

	WOBillingCharged     = "charged"
	WOBillingSkippedZero = "skipped_zero"
)

// WorkOrder is a row of work_orders.
type WorkOrder struct {
	bun.BaseModel `bun:"table:work_orders"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	Number            int64         `bun:"number"`
	Year              int           `bun:"year"`
	QuoteID           string        `bun:"quote_id,nullzero"`
	CustomerID        string        `bun:"customer_id"`
	VesselID          string        `bun:"vessel_id"`
	WorkTypeID        string        `bun:"work_type_id,nullzero"`
	ResponsibleUserID string        `bun:"responsible_user_id,nullzero"`
	PlannedStartOn    calendar.Date `bun:"planned_start_on,nullzero"`
	PlannedDeliveryOn calendar.Date `bun:"planned_delivery_on,nullzero"`
	AllocationID      string        `bun:"allocation_id,nullzero"`
	PositionNote      string        `bun:"position_note"`
	Status            string        `bun:"status"`
	BillingMode       string        `bun:"billing_mode"`
	DepositCents      int64         `bun:"deposit_cents"`
	RateSnapshot      JSON          `bun:"rate_snapshot"`
	CompletionBP      int           `bun:"completion_bp"`
	Notes             string        `bun:"notes"`
	HoldReason        string        `bun:"hold_reason"`
	StartedAt         time.Time     `bun:"started_at,nullzero"`
	CompletedAt       time.Time     `bun:"completed_at,nullzero"`
	DeliveredAt       time.Time     `bun:"delivered_at,nullzero"`
	InvoicedAt        time.Time     `bun:"invoiced_at,nullzero"`
	CancelledAt       time.Time     `bun:"cancelled_at,nullzero"`
	CancelReason      string        `bun:"cancel_reason"`
	CreatedByUserID   string        `bun:"created_by_user_id,nullzero"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}

// WorkOrderVariation is a row of work_order_variations (an accepted variation quote).
type WorkOrderVariation struct {
	bun.BaseModel `bun:"table:work_order_variations"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	WorkOrderID string    `bun:"work_order_id"`
	QuoteID     string    `bun:"quote_id"`
	AcceptedAt  time.Time `bun:"accepted_at"`
	CreatedAt   time.Time `bun:"created_at"`
}

// WorkOrderLine is a row of work_order_lines (budget line).
type WorkOrderLine struct {
	bun.BaseModel `bun:"table:work_order_lines"`

	ID                  string    `bun:"id,pk"`
	MarinaID            string    `bun:"marina_id"`
	WorkOrderID         string    `bun:"work_order_id"`
	Seq                 int       `bun:"seq"`
	Origin              string    `bun:"origin"`
	VariationID         string    `bun:"variation_id,nullzero"`
	QuoteLineID         string    `bun:"quote_line_id,nullzero"`
	ItemID              string    `bun:"item_id"`
	WorkTypeID          string    `bun:"work_type_id"`
	QualificationID     string    `bun:"qualification_id,nullzero"`
	Description         string    `bun:"description"`
	QtyMilli            int64     `bun:"qty_milli"`
	UnitCents           int64     `bun:"unit_cents"`
	DiscountCents       int64     `bun:"discount_cents"`
	NetCents            int64     `bun:"net_cents"`
	VATCents            int64     `bun:"vat_cents"`
	GrossCents          int64     `bun:"gross_cents"`
	VATRateBP           int       `bun:"vat_rate_bp"`
	VATNature           string    `bun:"vat_nature,nullzero"`
	PriceIncludesVAT    bool      `bun:"price_includes_vat"`
	UnitCostCents       int64     `bun:"unit_cost_cents"`
	EstimatedHoursMilli int64     `bun:"estimated_hours_milli"`
	ConsumedQtyMilli    *int64    `bun:"consumed_qty_milli"`
	Version             int       `bun:"version"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
}

// WorkTask is a row of work_tasks.
type WorkTask struct {
	bun.BaseModel `bun:"table:work_tasks"`

	ID                  string        `bun:"id,pk"`
	MarinaID            string        `bun:"marina_id"`
	WorkOrderID         string        `bun:"work_order_id"`
	Seq                 int           `bun:"seq"`
	Title               string        `bun:"title"`
	QualificationID     string        `bun:"qualification_id,nullzero"`
	WorkTypeID          string        `bun:"work_type_id,nullzero"`
	EstimatedHoursMilli int64         `bun:"estimated_hours_milli"`
	Status              string        `bun:"status"`
	ProgressBP          int           `bun:"progress_bp"`
	Checklist           JSON          `bun:"checklist"` // [{key, label, done}]
	PlannedFrom         calendar.Date `bun:"planned_from,nullzero"`
	PlannedTo           calendar.Date `bun:"planned_to,nullzero"`
	SourceLineID        string        `bun:"source_line_id,nullzero"`
	ClientRef           string        `bun:"client_ref,nullzero"`
	Version             int           `bun:"version"`
	CreatedAt           time.Time     `bun:"created_at"`
	UpdatedAt           time.Time     `bun:"updated_at"`
}

// WorkTaskDep is a row of work_task_deps (finish-to-start dependency).
type WorkTaskDep struct {
	bun.BaseModel `bun:"table:work_task_deps"`

	TaskID          string `bun:"task_id,pk"`
	DependsOnTaskID string `bun:"depends_on_task_id,pk"`
	MarinaID        string `bun:"marina_id"`
}

// WorkTaskFile is a row of work_task_files (photo or document of a task, encrypted stored file).
type WorkTaskFile struct {
	bun.BaseModel `bun:"table:work_task_files"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	TaskID          string    `bun:"task_id"`
	FileID          string    `bun:"file_id"`
	Kind            string    `bun:"kind"` // photo, document
	ClientRef       string    `bun:"client_ref,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}

// Timesheet is a row of timesheets (time entry).
type Timesheet struct {
	bun.BaseModel `bun:"table:timesheets"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	EmployeeID       string        `bun:"employee_id"`
	WorkOrderID      string        `bun:"work_order_id"`
	TaskID           string        `bun:"task_id,nullzero"`
	WorkDate         calendar.Date `bun:"work_date"`
	StartedAt        time.Time     `bun:"started_at,nullzero"`
	EndedAt          time.Time     `bun:"ended_at,nullzero"`
	HoursMilli       int64         `bun:"hours_milli"`
	Source           string        `bun:"source"`
	Status           string        `bun:"status"`
	CostRateCents    *int64        `bun:"cost_rate_cents"`
	CostCents        *int64        `bun:"cost_cents"`
	Note             string        `bun:"note"`
	Late             bool          `bun:"late"`
	ApprovedByUserID string        `bun:"approved_by_user_id,nullzero"`
	ApprovedAt       time.Time     `bun:"approved_at,nullzero"`
	RejectReason     string        `bun:"reject_reason"`
	VoidedAt         time.Time     `bun:"voided_at,nullzero"`
	VoidedByUserID   string        `bun:"voided_by_user_id,nullzero"`
	VoidsID          string        `bun:"voids_id,nullzero"`
	ClientRef        string        `bun:"client_ref,nullzero"`
	StopClientRef    string        `bun:"stop_client_ref,nullzero"`
	CreatedByUserID  string        `bun:"created_by_user_id,nullzero"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// StockMovement is a row of stock_movements (material issue or return on a work order).
type StockMovement struct {
	bun.BaseModel `bun:"table:stock_movements"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	WorkOrderID     string        `bun:"work_order_id"`
	TaskID          string        `bun:"task_id,nullzero"`
	ItemID          string        `bun:"item_id"`
	Kind            string        `bun:"kind"` // issue, return
	QtyMilli        int64         `bun:"qty_milli"`
	UnitCostCents   int64         `bun:"unit_cost_cents"`
	UnitPriceCents  int64         `bun:"unit_price_cents"`
	PriceOverridden bool          `bun:"price_overridden"`
	MovedOn         calendar.Date `bun:"moved_on"`
	Note            string        `bun:"note"`
	Late            bool          `bun:"late"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time     `bun:"created_at"`
}

// WorkOrderExternalCost is a row of work_order_external_costs.
type WorkOrderExternalCost struct {
	bun.BaseModel `bun:"table:work_order_external_costs"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	WorkOrderID     string        `bun:"work_order_id"`
	SupplierName    string        `bun:"supplier_name"`
	Reference       string        `bun:"reference"`
	IncurredOn      calendar.Date `bun:"incurred_on"`
	Description     string        `bun:"description"`
	WorkTypeID      string        `bun:"work_type_id,nullzero"`
	CostCents       int64         `bun:"cost_cents"`
	Billable        bool          `bun:"billable"`
	PriceCents      int64         `bun:"price_cents"`
	ItemID          string        `bun:"item_id,nullzero"`
	Late            bool          `bun:"late"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// WorkOrderBilling is a row of work_order_billings.
type WorkOrderBilling struct {
	bun.BaseModel `bun:"table:work_order_billings"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	WorkOrderID     string    `bun:"work_order_id"`
	Seq             int       `bun:"seq"`
	Kind            string    `bun:"kind"`
	Requested       JSON      `bun:"requested"` // {percent_bp?, amount_cents?}
	Status          string    `bun:"status"`
	ChargeID        string    `bun:"charge_id,nullzero"`
	GrossCents      int64     `bun:"gross_cents"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}
