package price

import (
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Line sources (yard_quote_lines.source).
const (
	SourcePackage = "package"
	SourceItem    = "item"
	SourceManual  = "manual"
)

// Issue codes of a line besides the formula codes.
const (
	IssueNoPrice         = "no_price"
	IssueMissingVariable = formula.CodeMissingVariable
)

// UnitPrice is a catalogue unit price frozen in the snapshot.
type UnitPrice struct {
	ItemID           string      `json:"item_id"`
	UnitCents        money.Cents `json:"unit_cents"`
	PriceListID      string      `json:"price_list_id,omitempty"`
	RuleID           string      `json:"rule_id,omitempty"`
	PriceIncludesVAT bool        `json:"price_includes_vat"`
	Found            bool        `json:"found"`
}

// ItemVAT is the VAT data of an item at quote time.
type ItemVAT struct {
	RateBP           int64  `json:"vat_rate_bp"`
	Nature           string `json:"vat_nature,omitempty"`
	PriceIncludesVAT bool   `json:"price_includes_vat"`
}

// ItemData is what a line needs of an item at quote time (frozen in the snapshot).
type ItemData struct {
	ItemID          string      `json:"item_id"`
	Code            string      `json:"code"`
	ItemType        string      `json:"item_type"`
	Unit            string      `json:"unit"`
	VAT             ItemVAT     `json:"vat"`
	CostCents       money.Cents `json:"cost_cents"`
	QualificationID string      `json:"qualification_id,omitempty"`
}

// TaskSpec is the task a line creates at acceptance (labour lines always do).
type TaskSpec struct {
	Title     map[string]string `json:"title"` // I18nText
	Checklist []string          `json:"checklist,omitempty"`
}

// LineSpec is one line before pricing: from a package version line, a single item or a manual line.
type LineSpec struct {
	Seq            int          `json:"seq"`
	Source         string       `json:"source"`
	PackageID      string       `json:"package_id,omitempty"`
	PackageVersion int          `json:"package_version,omitempty"`
	PackageLineSeq int          `json:"package_line_seq,omitempty"`
	ItemID         string       `json:"item_id"`
	WorkTypeID     string       `json:"work_type_id"`
	Description    string       `json:"description,omitempty"`
	QtyFormula     string       `json:"qty_formula,omitempty"`
	PriceFormula   string       `json:"price_formula,omitempty"`
	Task           *TaskSpec    `json:"task,omitempty"`
	ManualQty      *qty.Milli   `json:"manual_qty_milli,omitempty"`
	ManualUnit     *money.Cents `json:"manual_unit_cents,omitempty"`
}

// Override is a manual change of a computed line (marked).
type Override struct {
	Seq      int          `json:"seq"`
	Qty      *qty.Milli   `json:"qty_milli,omitempty"`
	Unit     *money.Cents `json:"unit_cents,omitempty"`
	Discount *money.Cents `json:"discount_cents,omitempty"`
	// Description replaces the line description (not a price change: the line is not marked).
	Description *string `json:"description,omitempty"`
	Remove      bool    `json:"remove,omitempty"`
}

// VariableValue is a formula variable value in the snapshot (number in milli, bool, enum).
type VariableValue struct {
	Kind string    `json:"kind"` // number, bool, enum
	Num  qty.Milli `json:"num,omitempty"`
	Bool bool      `json:"bool,omitempty"`
	Enum string    `json:"enum,omitempty"`
}

// TableSnapshot is a band table used by the quote.
type TableSnapshot struct {
	Name string             `json:"name"`
	Rows []TableRowSnapshot `json:"rows"`
}

// TableRowSnapshot is one band row.
type TableRowSnapshot struct {
	UpTo  *qty.Milli `json:"up_to_milli"`
	Value qty.Milli  `json:"value_milli"`
}

// DerivedSnapshot is a derived variable definition with its value at quote time.
type DerivedSnapshot struct {
	Name  string        `json:"name"`
	Src   string        `json:"src"`
	Value VariableValue `json:"value"`
}

// Snapshot is everything needed to recompute a quote (yard_quotes.snapshot, m10-plan §3.6).
type Snapshot struct {
	LanguageVersion int                      `json:"language_version"`
	QuotedOn        string                   `json:"quoted_on"`
	Vessel          map[string]VariableValue `json:"vessel"`
	VesselOverrides map[string]VariableValue `json:"vessel_overrides,omitempty"`
	JobInputs       map[string]VariableValue `json:"job_inputs,omitempty"`
	Derived         []DerivedSnapshot        `json:"derived,omitempty"`
	Tables          []TableSnapshot          `json:"tables,omitempty"`
	Lines           []LineSpec               `json:"lines"`
	Overrides       []Override               `json:"overrides,omitempty"`
	Prices          map[string]UnitPrice     `json:"prices"` // by item id
	Items           map[string]ItemData      `json:"items"`  // by item id
	DepositBP       *int64                   `json:"deposit_bp,omitempty"`
	DepositCents    *money.Cents             `json:"deposit_cents,omitempty"`
	// Inputs of a re-pricing (Ricalcola): the step-2 selections, the customer category and the job variables asked.
	Selections       []Selection `json:"selections,omitempty"`
	CustomerCategory string      `json:"customer_category,omitempty"`
	JobVars          []JobVar    `json:"job_vars,omitempty"`
}

// Selection is a package (current version at quote time) or a single item chosen in step 2.
type Selection struct {
	PackageID string `json:"package_id,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
}

// JobVar is a job variable asked in step 3, as declared at quote time.
type JobVar struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Unit    string            `json:"unit"`
	Label   map[string]string `json:"label,omitempty"`
	Min     *qty.Milli        `json:"min_milli,omitempty"`
	Max     *qty.Milli        `json:"max_milli,omitempty"`
	Default *VariableValue    `json:"default,omitempty"`
	Enum    []string          `json:"enum,omitempty"`
}

// LineIssue is a problem of one line (formula code, no_price, missing_variable).
type LineIssue struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
	Pos   int    `json:"pos,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// Line is a priced line.
type Line struct {
	Spec            LineSpec    `json:"spec"`
	QualificationID string      `json:"qualification_id,omitempty"`
	Qty             qty.Milli   `json:"qty_milli"`
	Unit            money.Cents `json:"unit_cents"`
	Discount        money.Cents `json:"discount_cents"`
	QtyOverridden   bool        `json:"qty_overridden"`
	PriceOverridden bool        `json:"price_overridden"`
	Net             money.Cents `json:"net_cents"`
	VAT             money.Cents `json:"vat_cents"`
	Gross           money.Cents `json:"gross_cents"`
	VATData         ItemVAT     `json:"vat_data"`
	UnitCost        money.Cents `json:"unit_cost_cents"`
	EstimatedHours  qty.Milli   `json:"estimated_hours_milli"` // labour lines: = Qty
	PriceListID     string      `json:"price_list_id,omitempty"`
	RuleID          string      `json:"rule_id,omitempty"`
	Issues          []LineIssue `json:"issues,omitempty"`
}

// VATBucket sums lines by VAT rate and nature.
type VATBucket struct {
	RateBP int64       `json:"vat_rate_bp"`
	Nature string      `json:"vat_nature,omitempty"`
	Net    money.Cents `json:"net_cents"`
	VAT    money.Cents `json:"vat_cents"`
	Gross  money.Cents `json:"gross_cents"`
}

// Totals of a quote: Σ lines, VAT summary and deposit (gross.PercentBP(deposit_bp) or the fixed amount <= gross).
type Totals struct {
	Net            money.Cents          `json:"net_cents"`
	VAT            money.Cents          `json:"vat_cents"`
	Gross          money.Cents          `json:"gross_cents"`
	Deposit        money.Cents          `json:"deposit_amount_cents"`
	Buckets        []VATBucket          `json:"vat"`
	EstimatedHours map[string]qty.Milli `json:"estimated_hours"` // by qualification id
	Blocking       bool                 `json:"blocking"`
}
