package yardquote

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Job names (m10-plan §16.1).
const (
	// JobQuotesExpire expires sent and draft quotes at expires_at (hourly, idempotent).
	JobQuotesExpire = "yard.quotes_expire"
)

// Defaults of yard_settings when the row is missing (m10-plan §4.2, §8, §11).
const (
	DefaultQuoteValidityDays = 30
	DefaultDepositBP         = 3000
	DefaultExternalMarkupBP  = 2000
	DefaultLiftFromSlot      = 28 // 07:00
	DefaultLiftToSlot        = 76 // 19:00
)

// Settings returns the yard settings of a marina with the defaults applied (Version 0 = never saved). QUOTE owns
// the store query; this is the shape ORDERS and PLANNER read through Service.SettingsTx.
type Settings = store.YardSettings

// WorkType is a work type row.
type WorkType = store.WorkType

// Qualification is a qualification row.
type Qualification = store.YardQualification

// I18n is a translated text (it required).
type I18n = catalog.I18nText

// Value is a formula variable value (number in milli, bool or enum).
type Value = price.VariableValue

// TestVessel is the marina test vessel of the formula validation (yard_settings.test_vessel, m10-plan §3.5).
type TestVessel struct {
	LOACm          int64   `json:"loa_cm"`
	BeamCm         int64   `json:"beam_cm"`
	DraftCm        int64   `json:"draft_cm,omitempty"`
	AirDraftCm     int64   `json:"air_draft_cm,omitempty"`
	LWLCm          int64   `json:"lwl_cm,omitempty"`
	DisplacementKg int64   `json:"displacement_kg,omitempty"`
	HullType       string  `json:"hull_type"`
	HullMaterial   string  `json:"hull_material,omitempty"`
	EnginesKW      []int64 `json:"engines_kw,omitempty"`
	YearBuilt      int     `json:"year_built,omitempty"`
}

// DefaultTestVessel is the test vessel when none is saved: LOA 12 m, beam 4 m, draft 1.5 m, sailing monohull, one
// 55 kW engine, built 2010.
func DefaultTestVessel() TestVessel {
	return TestVessel{LOACm: 1200, BeamCm: 400, DraftCm: 150, HullType: "monohull_sail", EnginesKW: []int64{55}, YearBuilt: 2010}
}

// SettingsInput changes the yard settings (nil = unchanged; ClearExternalItem empties external_item_id).
type SettingsInput struct {
	TestVessel            *TestVessel
	QuoteValidityDays     *int
	DepositBP             *int
	AdvanceOnAccept       *bool
	ExternalMarkupBP      *int
	ExternalItemID        *string
	ClearExternalItem     bool
	FixedIncludesExternal *bool
	WorkersIssueMaterials *bool
	LiftFromSlot          *int
	LiftToSlot            *int
	LiftRequestOnMove     *bool
}

// SettingsView is the yard settings with the decoded test vessel.
type SettingsView struct {
	Row        store.YardSettings
	TestVessel TestVessel
}

// WorkTypeInput creates a work type.
type WorkTypeInput struct {
	Code           string
	Name           I18n
	RevenueClassID string
	Active         *bool
	SortOrder      *int
}

// WorkTypePatch changes a work type (nil = unchanged; RevenueClassID "" clears it).
type WorkTypePatch struct {
	Code           *string
	Name           *I18n
	RevenueClassID *string
	Active         *bool
	SortOrder      *int
}

// QualificationInput creates a qualification.
type QualificationInput struct {
	Code             string
	Name             I18n
	LabourItemID     string
	DefaultCostCents *int64
	LiftOperator     *bool
	Active           *bool
	SortOrder        *int
}

// QualificationPatch changes a qualification (nil = unchanged).
type QualificationPatch struct {
	Code             *string
	Name             *I18n
	LabourItemID     *string
	DefaultCostCents *int64
	LiftOperator     *bool
	Active           *bool
	SortOrder        *int
}

// TestCase is the optional test of a formula: variable values and the expected quantity or price.
type TestCase struct {
	Vars        map[string]Value `json:"vars,omitempty"`
	ExpectQty   *qty.Milli       `json:"expect_qty_milli,omitempty"`
	ExpectPrice *money.Cents     `json:"expect_price_cents,omitempty"`
}

// ItemSettingsInput sets the yard settings of an item (nil = unchanged; QualificationID "" clears it).
type ItemSettingsInput struct {
	YardEnabled     *bool
	CostCents       *int64
	QualificationID *string
	QtyFormula      *string
	PriceFormula    *string
	Category        *string
	TestCase        *TestCase
	ClearTestCase   bool
}

// YardItem is an item with its yard settings (Settings nil = not configured).
type YardItem struct {
	Item     store.Item
	Settings *store.YardItemSettings
	TestCase *TestCase
}

// ItemFilter filters ListItems.
type ItemFilter struct {
	Query       string
	Category    string
	ItemType    string
	YardEnabled *bool
}

// VariableInput creates a derived or job variable.
type VariableInput struct {
	Name        string
	Source      string // derived, job
	Kind        string // number, bool, enum
	Unit        string
	Expression  string
	Min         *qty.Milli
	Max         *qty.Milli
	Default     *qty.Milli
	DefaultEnum string
	EnumValues  []string
	Label       *I18n
}

// OptMilli is a nullable patch field: Set false = unchanged, Set with nil = cleared.
type OptMilli struct {
	Set bool
	V   *qty.Milli
}

// OptString is a nullable patch field: Set false = unchanged, Set with "" = cleared.
type OptString struct {
	Set bool
	V   string
}

// VariablePatch changes a variable (name, source and kind never change).
type VariablePatch struct {
	Unit        *string
	Expression  *string
	Min         OptMilli
	Max         OptMilli
	Default     OptMilli
	DefaultEnum OptString
	EnumValues  *[]string
	Label       *I18n
}

// VariableView is a formula variable: built in (vessel and context, Row nil) or stored (derived, job).
type VariableView struct {
	Row       *store.FormulaVariable
	Name      string
	Source    string
	Kind      string
	Unit      string
	Label     I18n
	Builtin   bool
	Enum      []string
	TestValue *Value
}

// TableInput creates a band table.
type TableInput struct {
	Name  string
	Label *I18n
	Rows  []formula.Row
}

// TablePatch changes a band table.
type TablePatch struct {
	Label *I18n
	Rows  *[]formula.Row
}

// TableView is a band table with its decoded rows.
type TableView struct {
	Row  store.FormulaTable
	Rows []formula.Row
}

// PackageTask is the task a package line creates at acceptance.
type PackageTask struct {
	Title     I18n     `json:"title"`
	Checklist []string `json:"checklist,omitempty"`
}

// PackageLine is a line of a work package version (work_package_versions.lines).
type PackageLine struct {
	Seq          int          `json:"seq"`
	ItemID       string       `json:"item_id"`
	WorkTypeID   string       `json:"work_type_id,omitempty"`
	QtyFormula   string       `json:"qty_formula"`
	PriceFormula string       `json:"price_formula,omitempty"`
	Task         *PackageTask `json:"task,omitempty"`
	Notes        string       `json:"notes,omitempty"`
	TestCase     *TestCase    `json:"test_case,omitempty"`
}

// PackageInput creates a package with its version 1.
type PackageInput struct {
	Code       string
	Name       I18n
	WorkTypeID string
	Active     *bool
	Lines      []PackageLine
	JobVars    []string
}

// VersionInput saves version N+1 of a package (name and work type optionally changed with it).
type VersionInput struct {
	Name       *I18n
	WorkTypeID *string
	Lines      []PackageLine
	JobVars    []string
}

// PackageView is a package with the requested version and the version history.
type PackageView struct {
	Row      store.WorkPackage
	Content  *store.WorkPackageVersion
	Lines    []PackageLine
	Versions []store.WorkPackageVersionBrief
}

// PackageFilter filters ListPackages.
type PackageFilter struct {
	Active     *bool
	WorkTypeID string
}

// Formula purposes of ValidateFormula.
const (
	PurposeQuantity = "quantity"
	PurposePrice    = "price"
	PurposeDerived  = "derived"
	PurposeBoolean  = "boolean"
)

// ValidateInput is a formula to parse, check and evaluate on the test vessel (or a vessel of the marina).
type ValidateInput struct {
	Formula  string
	Purpose  string
	VesselID string
	Vars     map[string]Value
	ItemID   string
	Qty      *qty.Milli
}

// ValidateResult is the outcome of ValidateFormula.
type ValidateResult struct {
	Valid  bool
	Issues []Issue
	Idents []string
	Value  *Value
	Qty    *qty.Milli
	Price  *money.Cents
}

// Selection is a package (current version) or a single item chosen in step 2.
type Selection = price.Selection

// ManualLine is a line priced by hand (marked manual).
type ManualLine struct {
	ItemID      string
	WorkTypeID  string
	Description string
	Qty         qty.Milli
	Unit        money.Cents
	Discount    money.Cents
}

// QuoteInput is the input of preview and create.
type QuoteInput struct {
	CustomerID        string
	VesselID          string
	Selections        []Selection
	JobInputs         map[string]Value
	VesselOverrides   map[string]Value
	Overrides         []price.Override
	ManualLines       []ManualLine
	QuotedOn          *calendar.Date
	ValidUntil        *calendar.Date
	BillingMode       string
	DepositBP         *int64
	DepositCents      *int64
	Conditions        string
	Notes             string
	PlannedStartOn    *calendar.Date
	PlannedDeliveryOn *calendar.Date
}

// OptInt64 is a nullable patch field.
type OptInt64 struct {
	Set bool
	V   *int64
}

// OptDate is a nullable patch field.
type OptDate struct {
	Set bool
	V   *calendar.Date
}

// QuotePatch changes a draft (nil or unset = unchanged).
type QuotePatch struct {
	Selections        *[]Selection
	JobInputs         *map[string]Value
	VesselOverrides   *map[string]Value
	Overrides         *[]price.Override
	ManualLines       *[]ManualLine
	ValidUntil        *calendar.Date
	BillingMode       *string
	DepositBP         OptInt64
	DepositCents      OptInt64
	Conditions        *string
	Notes             *string
	PlannedStartOn    OptDate
	PlannedDeliveryOn OptDate
}

// LineView is a priced line with the item data the client shows.
type LineView struct {
	price.Line
	ItemCode string
	ItemType string
	ItemUnit string
}

// Manual reports whether the line carries the "manuale" badge.
func (l LineView) Manual() bool {
	return l.QtyOverridden || l.PriceOverridden || l.Spec.Source == price.SourceManual
}

// HoursView are the estimated hours of a qualification with their cost at the default internal cost.
type HoursView struct {
	QualificationID string
	Hours           qty.Milli
	Cost            money.Cents
}

// VariableAsk is a variable asked in step 3: a job variable or missing vessel data.
type VariableAsk struct {
	Name     string
	Source   string
	Kind     string
	Unit     string
	Label    I18n
	Min      *qty.Milli
	Max      *qty.Milli
	Default  *Value
	Enum     []string
	Required bool
	Value    *Value
}

// Preview is the computed quote of an input (nothing written).
type Preview struct {
	Lines     []LineView
	Totals    price.Totals
	Hours     []HoursView
	Variables []VariableAsk
	Issues    []price.LineIssue
	Blocking  bool
}

// QuoteView is a stored quote with its computed lines.
type QuoteView struct {
	Row      store.YardQuote
	Number   string
	Snapshot price.Snapshot
	Preview
}

// QuoteFilter filters ListQuotes.
type QuoteFilter struct {
	Status      string
	Kind        string
	CustomerID  string
	VesselID    string
	WorkOrderID string
}

// SendResult is the outcome of a send.
type SendResult struct {
	Quote      *QuoteView
	DocumentID string
	LinkURL    string
	MessageID  string
}

// AcceptResult is the outcome of an acceptance.
type AcceptResult struct {
	Quote *QuoteView
	Order OrderRef
}

// VariationOrder is the work order a variation quote belongs to (validated and locked by the caller, D27).
type VariationOrder struct {
	WorkOrderID string
	Number      string
	CustomerID  string
	VesselID    string
}

// PublicLine is a customer-facing line of a link page.
type PublicLine struct {
	Description string
	Unit        string
	Qty         qty.Milli
	UnitCents   money.Cents
	Discount    money.Cents
	Net         money.Cents
	VATRateBP   int64
}

// PublicQuote is the customer-facing yard quote of a link (no internal costs, no formulas).
type PublicQuote struct {
	Number            string
	Kind              string
	Status            string
	ValidUntil        calendar.Date
	VesselName        string
	WorkOrderNumber   string
	Lines             []PublicLine
	Net, VAT, Gross   money.Cents
	Deposit           money.Cents
	BillingMode       string
	Conditions        string
	PlannedStartOn    calendar.Date
	PlannedDeliveryOn calendar.Date
	CanAccept         bool
}

// PublicAcceptance is the outcome of a link acceptance.
type PublicAcceptance struct {
	QuoteNumber     string
	WorkOrderNumber string
}
