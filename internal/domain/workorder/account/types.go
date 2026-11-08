package account

import (
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Billing modes (D10).
const (
	ModeActual   = "actual"
	ModeFixed    = "fixed"
	ModeProgress = "progress"
)

// Line sources.
const (
	SourceLabour   = "labour"
	SourceMaterial = "material"
	SourceService  = "service"
	SourceExternal = "external"
)

// VAT is the VAT data of an item.
type VAT struct {
	RateBP           int64
	Nature           string
	PriceIncludesVAT bool
}

// BudgetLine is a work order budget line (quote and variation lines).
type BudgetLine struct {
	LineID          string
	ItemID          string
	ItemType        string // labour, material, product, service, fee
	WorkTypeID      string
	QualificationID string
	Qty             qty.Milli
	Unit            money.Cents
	Discount        money.Cents
	Net             money.Cents
	VAT             money.Cents
	Gross           money.Cents
	UnitCost        money.Cents
	ConsumedQty     *qty.Milli // services/products: actual quantity, nil = budget quantity
}

// RateSnapshot holds the unit prices frozen at the order creation: by item id (budget items and the labour item of
// every active qualification).
type RateSnapshot struct {
	Units         map[string]money.Cents
	LabourItems   map[string]string // qualification id → labour item id
	OrderWorkType string            // fallback work type of entries without a task work type
}

// ApprovedEntry is an approved, non-voided time entry.
type ApprovedEntry struct {
	ID              string
	QualificationID string // of the task (or of the employee when the entry has no task)
	WorkTypeID      string // of the task, else the order's
	Hours           qty.Milli
	CostCents       money.Cents // frozen at approval
	Late            bool
}

// Movement is a material issue (positive) or return of an order.
type Movement struct {
	ID         string
	ItemID     string
	WorkTypeID string
	Return     bool
	Qty        qty.Milli
	UnitCost   money.Cents
	UnitPrice  money.Cents
	Late       bool
}

// ExternalCost is an external cost of an order.
type ExternalCost struct {
	ID         string
	ItemID     string
	WorkTypeID string
	Cost       money.Cents
	Billable   bool
	Price      money.Cents
	Late       bool
}

// Input of Build.
type Input struct {
	Mode                  string
	FixedIncludesExternal bool
	Budget                []BudgetLine
	Rates                 RateSnapshot
	Entries               []ApprovedEntry
	Movements             []Movement
	External              []ExternalCost
	ItemVAT               map[string]VAT
	ItemCost              map[string]money.Cents
}

// Line is one actual line (key item:<item_id>|wt:<work_type_id>; external ext:<id>).
type Line struct {
	Key             string
	Source          string
	ItemID          string
	QualificationID string
	WorkTypeID      string
	Qty             qty.Milli
	Revenue         money.Cents
	Cost            money.Cents
	Margin          money.Cents
	MarginBP        *int64
	VAT             VAT
	VATCents        money.Cents
	Gross           money.Cents
	BudgetQty       qty.Milli
	BudgetNet       money.Cents
	DeltaNet        money.Cents
}

// Totals of an account (§9.2).
type Totals struct {
	Revenue, Cost, Margin                                      money.Cents
	MarginBP                                                   *int64
	RevenueLabour, RevenueMaterial, RevenueService, RevenueExt money.Cents
	CostLabour, CostMaterial, CostService, CostExt             money.Cents
	HoursLabour                                                qty.Milli
	VATCents, Gross                                            money.Cents
}

// WorkTypeTotals are the totals of the lines of one work type.
type WorkTypeTotals struct {
	WorkTypeID            string
	Revenue, Cost, Margin money.Cents
	MarginBP              *int64
}

// TargetLine is a revenue line with VAT, the billing target of the mode (actual lines or budget lines), keyed by the
// charge line plan_key so that chargeplan.Remaining works across billings.
type TargetLine struct {
	PlanKey     string
	ItemID      string
	Description string
	Qty         qty.Milli
	Net         money.Cents
	VAT         VAT
	VATCents    money.Cents
	Gross       money.Cents
}

// Account is the final account of an order.
type Account struct {
	Lines      []Line
	Totals     Totals
	ByWorkType []WorkTypeTotals
	QuoteNet   money.Cents
	DeltaNet   money.Cents
	Target     []TargetLine
}
