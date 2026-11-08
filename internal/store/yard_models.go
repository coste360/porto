package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M10 yard catalogue and yard quote tables (internal/migrations/*/00020_yard_catalog_quotes.sql,
// m10-plan §3, §4, §15). Owned by the QUOTE builder after the contract phase; ORDERS and PLANNER read them.

// Yard quote and catalogue enums (m10-plan §3, §4).
const (
	FormulaSourceDerived = "derived"
	FormulaSourceJob     = "job"

	FormulaKindNumber = "number"
	FormulaKindBool   = "bool"
	FormulaKindEnum   = "enum"

	YardQuoteJob       = "job"
	YardQuoteVariation = "variation"

	YardQuoteDraft    = "draft"
	YardQuoteSent     = "sent"
	YardQuoteAccepted = "accepted"
	YardQuoteDeclined = "declined"
	YardQuoteExpired  = "expired"

	BillingModeActual   = "actual"
	BillingModeFixed    = "fixed"
	BillingModeProgress = "progress"

	YardLineSourcePackage = "package"
	YardLineSourceItem    = "item"
	YardLineSourceManual  = "manual"

	AcceptedViaPanel = "panel"
	AcceptedViaLink  = "link"

	// SeriesYardQuote and SeriesWorkOrder are number_sequences.series of yard quotes (PC<year>-NNNNNN) and work
	// orders (C<year>-NNNNNN) (m10-plan D12).
	SeriesYardQuote = "yard_quote"
	SeriesWorkOrder = "work_order"

	// SubjectYardQuote and SubjectWorkOrder are generated_documents.subject_type and access_links.subject_type of
	// yard documents.
	SubjectYardQuote = "yard_quote"
	SubjectWorkOrder = "work_order"
)

// YardSettings is a row of yard_settings (missing row = defaults).
type YardSettings struct {
	bun.BaseModel `bun:"table:yard_settings"`

	MarinaID              string    `bun:"marina_id,pk"`
	TestVessel            JSON      `bun:"test_vessel"`
	QuoteValidityDays     int       `bun:"quote_validity_days"`
	DepositBP             int       `bun:"deposit_bp"`
	AdvanceOnAccept       bool      `bun:"advance_on_accept"`
	ExternalMarkupBP      int       `bun:"external_markup_bp"`
	ExternalItemID        string    `bun:"external_item_id,nullzero"`
	FixedIncludesExternal bool      `bun:"fixed_includes_external"`
	WorkersIssueMaterials bool      `bun:"workers_issue_materials"`
	LiftFromSlot          int       `bun:"lift_from_slot"`
	LiftToSlot            int       `bun:"lift_to_slot"`
	LiftRequestOnMove     bool      `bun:"lift_request_on_move"`
	Version               int       `bun:"version"`
	CreatedAt             time.Time `bun:"created_at"`
	UpdatedAt             time.Time `bun:"updated_at"`
}

// WorkType is a row of work_types.
type WorkType struct {
	bun.BaseModel `bun:"table:work_types"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	Code           string    `bun:"code"`
	Name           JSON      `bun:"name"` // I18nText
	RevenueClassID string    `bun:"revenue_class_id,nullzero"`
	Active         bool      `bun:"active"`
	SortOrder      int       `bun:"sort_order"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// YardQualification is a row of yard_qualifications.
type YardQualification struct {
	bun.BaseModel `bun:"table:yard_qualifications"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	Code             string    `bun:"code"`
	Name             JSON      `bun:"name"` // I18nText
	LabourItemID     string    `bun:"labour_item_id"`
	DefaultCostCents int64     `bun:"default_cost_cents"`
	LiftOperator     bool      `bun:"lift_operator"`
	Active           bool      `bun:"active"`
	SortOrder        int       `bun:"sort_order"`
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// YardItemSettings is a row of yard_item_settings (yard data of an M2 item).
type YardItemSettings struct {
	bun.BaseModel `bun:"table:yard_item_settings"`

	ItemID          string    `bun:"item_id,pk"`
	MarinaID        string    `bun:"marina_id"`
	YardEnabled     bool      `bun:"yard_enabled"`
	CostCents       int64     `bun:"cost_cents"`
	QualificationID string    `bun:"qualification_id,nullzero"`
	QtyFormula      string    `bun:"qty_formula"`
	PriceFormula    string    `bun:"price_formula"`
	Category        string    `bun:"category"`
	TestCase        JSON      `bun:"test_case,nullzero"` // {vars, expect_qty?, expect_price_cents?}
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// FormulaVariable is a row of formula_variables (derived or job variable of the formula language).
type FormulaVariable struct {
	bun.BaseModel `bun:"table:formula_variables"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	Name         string    `bun:"name"`
	Source       string    `bun:"source"` // derived, job
	Kind         string    `bun:"kind"`   // number, bool, enum
	Unit         string    `bun:"unit"`
	Expression   string    `bun:"expression"`
	MinMilli     *int64    `bun:"min_milli"`
	MaxMilli     *int64    `bun:"max_milli"`
	DefaultMilli *int64    `bun:"default_milli"`
	DefaultEnum  string    `bun:"default_enum,nullzero"`
	EnumValues   Strings   `bun:"enum_values"`
	Label        JSON      `bun:"label"` // I18nText
	Version      int       `bun:"version"`
	CreatedAt    time.Time `bun:"created_at"`
	UpdatedAt    time.Time `bun:"updated_at"`
}

// FormulaTable is a row of formula_tables (band table used by band(x, name)).
type FormulaTable struct {
	bun.BaseModel `bun:"table:formula_tables"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Name      string    `bun:"name"`
	Label     JSON      `bun:"label"`     // I18nText
	BandRows  JSON      `bun:"band_rows"` // [{up_to_milli|null, value_milli}]
	Version   int       `bun:"version"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// WorkPackage is a row of work_packages.
type WorkPackage struct {
	bun.BaseModel `bun:"table:work_packages"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	Code           string    `bun:"code"`
	Name           JSON      `bun:"name"` // I18nText
	WorkTypeID     string    `bun:"work_type_id"`
	Active         bool      `bun:"active"`
	CurrentVersion int       `bun:"current_version"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// WorkPackageVersion is a row of work_package_versions (insert-only, INV-28).
type WorkPackageVersion struct {
	bun.BaseModel `bun:"table:work_package_versions"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	PackageID       string    `bun:"package_id"`
	Version         int       `bun:"version"`
	LanguageVersion int       `bun:"language_version"`
	Lines           JSON      `bun:"lines"`    // [{seq, item_id, work_type_id?, qty_formula, price_formula?, task?, notes}]
	JobVars         Strings   `bun:"job_vars"` // names asked in step 3
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}

// YardQuote is a row of yard_quotes.
type YardQuote struct {
	bun.BaseModel `bun:"table:yard_quotes"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	Number             int64         `bun:"number"`
	Year               int           `bun:"year"`
	Kind               string        `bun:"kind"`
	WorkOrderID        string        `bun:"work_order_id,nullzero"`
	CustomerID         string        `bun:"customer_id"`
	VesselID           string        `bun:"vessel_id"`
	Status             string        `bun:"status"`
	QuotedOn           calendar.Date `bun:"quoted_on"`
	ValidUntil         calendar.Date `bun:"valid_until"`
	ExpiresAt          time.Time     `bun:"expires_at"`
	BillingMode        string        `bun:"billing_mode"`
	DepositBP          *int          `bun:"deposit_bp"`
	DepositCents       *int64        `bun:"deposit_cents"`
	Conditions         string        `bun:"conditions"`
	Notes              string        `bun:"notes"`
	PlannedStartOn     calendar.Date `bun:"planned_start_on,nullzero"`
	PlannedDeliveryOn  calendar.Date `bun:"planned_delivery_on,nullzero"`
	JobInputs          JSON          `bun:"job_inputs"`
	VesselOverrides    JSON          `bun:"vessel_overrides"`
	Snapshot           JSON          `bun:"snapshot"`
	NetCents           int64         `bun:"net_cents"`
	VATCents           int64         `bun:"vat_cents"`
	GrossCents         int64         `bun:"gross_cents"`
	DepositAmountCents int64         `bun:"deposit_amount_cents"`
	EstimatedHours     JSON          `bun:"estimated_hours"` // {qualification_id: hours milli}
	SentAt             time.Time     `bun:"sent_at,nullzero"`
	DecidedAt          time.Time     `bun:"decided_at,nullzero"`
	AcceptedVia        string        `bun:"accepted_via,nullzero"`
	DeclineReason      string        `bun:"decline_reason"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// YardQuoteLine is a row of yard_quote_lines.
type YardQuoteLine struct {
	bun.BaseModel `bun:"table:yard_quote_lines"`

	ID                  string    `bun:"id,pk"`
	MarinaID            string    `bun:"marina_id"`
	QuoteID             string    `bun:"quote_id"`
	Seq                 int       `bun:"seq"`
	Source              string    `bun:"source"`
	PackageID           string    `bun:"package_id,nullzero"`
	PackageVersion      *int      `bun:"package_version"`
	PackageLineSeq      *int      `bun:"package_line_seq"`
	ItemID              string    `bun:"item_id"`
	WorkTypeID          string    `bun:"work_type_id"`
	QualificationID     string    `bun:"qualification_id,nullzero"`
	Description         string    `bun:"description"`
	QtyFormula          string    `bun:"qty_formula"`
	PriceFormula        string    `bun:"price_formula"`
	QtyMilli            int64     `bun:"qty_milli"`
	UnitCents           int64     `bun:"unit_cents"`
	DiscountCents       int64     `bun:"discount_cents"`
	QtyOverridden       bool      `bun:"qty_overridden"`
	PriceOverridden     bool      `bun:"price_overridden"`
	NetCents            int64     `bun:"net_cents"`
	VATCents            int64     `bun:"vat_cents"`
	GrossCents          int64     `bun:"gross_cents"`
	VATRateBP           int       `bun:"vat_rate_bp"`
	VATNature           string    `bun:"vat_nature,nullzero"`
	PriceIncludesVAT    bool      `bun:"price_includes_vat"`
	UnitCostCents       int64     `bun:"unit_cost_cents"`
	EstimatedHoursMilli int64     `bun:"estimated_hours_milli"`
	PriceListID         string    `bun:"price_list_id,nullzero"`
	RuleID              string    `bun:"rule_id,nullzero"`
	Task                JSON      `bun:"task,nullzero"` // {title I18n, checklist[]}
	Issues              JSON      `bun:"issues"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
}
