package pricing

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Normative types of the engine (m2-plan §2.2). JSON names are snake_case and follow the API conventions
// (money *_cents, lengths *_cm, basis points *_bp, quantities *_milli): the OpenAPI schemas of the price list
// document, the simulator and the breakdown mirror them, and the corpus in testdata/pricing uses them directly.
// PRICING-ENGINE may add fields and helpers; exported names, shapes and JSON tags stay as they are.

// EngineVersion is bumped whenever the output for an existing input can change (INV-20).
const EngineVersion = 1

// ContractType is what a price list serves and what a quote is for.
type ContractType string

// Contract types.
const (
	ContractAnnual   ContractType = "annual"
	ContractSeasonal ContractType = "seasonal"
	ContractMonthly  ContractType = "monthly"
	ContractTransit  ContractType = "transit"
	ContractStorage  ContractType = "storage"
	ContractCharter  ContractType = "charter"
)

// ContractTypes returns every contract type in display order.
func ContractTypes() []ContractType {
	return []ContractType{ContractAnnual, ContractSeasonal, ContractMonthly, ContractTransit, ContractStorage, ContractCharter}
}

// Channel is where a quote is made.
type Channel string

// Channels.
const (
	ChannelDesk     Channel = "desk"
	ChannelOnline   Channel = "online"
	ChannelAPI      Channel = "api"
	ChannelExternal Channel = "external"
)

// Channels returns every channel.
func Channels() []Channel { return []Channel{ChannelDesk, ChannelOnline, ChannelAPI, ChannelExternal} }

// Stage is the role of a rule in a quote.
type Stage string

// Stages.
const (
	StageBase     Stage = "base"
	StageModifier Stage = "modifier"
	StageDiscount Stage = "discount"
)

// Basis is how a base rule computes the amount of one unit (spec 12.3).
type Basis string

// Bases.
const (
	BasisPerBerth   Basis = "per_berth"
	BasisLengthBand Basis = "length_band"
	BasisPerMeter   Basis = "per_meter"
	BasisPerM2      Basis = "per_m2"
	BasisPerM2Berth Basis = "per_m2_berth"
	BasisFlat       Basis = "flat"
	BasisMetered    Basis = "metered"
)

// Bases returns every basis.
func Bases() []Basis {
	return []Basis{BasisPerBerth, BasisLengthBand, BasisPerMeter, BasisPerM2, BasisPerM2Berth, BasisFlat, BasisMetered}
}

// TimeUnit is the time unit a base rule prices.
type TimeUnit string

// Time units. UnitStay is one-off (services).
const (
	UnitNight  TimeUnit = "night"
	UnitWeek   TimeUnit = "week"
	UnitMonth  TimeUnit = "month"
	UnitSeason TimeUnit = "season"
	UnitYear   TimeUnit = "year"
	UnitStay   TimeUnit = "stay"
)

// TimeUnits returns every time unit.
func TimeUnits() []TimeUnit {
	return []TimeUnit{UnitNight, UnitWeek, UnitMonth, UnitSeason, UnitYear, UnitStay}
}

// SplitRule decides how a stay that is not an exact multiple of the reference unit is cut into blocks.
type SplitRule string

// Split rules.
const (
	SplitProRataDaily    SplitRule = "pro_rata_daily"
	SplitWholeMonths     SplitRule = "whole_months"
	SplitBestCombination SplitRule = "best_combination"
)

// DimensionSource picks the dimensions used by the bases.
type DimensionSource string

// Dimension sources.
const (
	DimensionVessel DimensionSource = "vessel"
	DimensionBerth  DimensionSource = "berth"
	DimensionMax    DimensionSource = "max"
)

// TotalRounding rounds the total of a quote.
type TotalRounding string

// Total roundings.
const (
	RoundNone     TotalRounding = "none"
	RoundEuro     TotalRounding = "euro"
	RoundFiveEuro TotalRounding = "five_euro"
)

// AmountRounding rounds the amounts of a renewed list.
type AmountRounding string

// Amount roundings.
const (
	AmountRoundNone     AmountRounding = "none"
	AmountRoundTenCents AmountRounding = "ten_cents"
	AmountRoundEuro     AmountRounding = "euro"
)

// DiscountKind is the group of a discount rule, applied in this order: customer, promo, automatic.
type DiscountKind string

// Discount kinds.
const (
	DiscountCustomer  DiscountKind = "customer"
	DiscountPromo     DiscountKind = "promo"
	DiscountAutomatic DiscountKind = "automatic"
)

// ItemType is the type of an item (spec 12.1).
type ItemType string

// Item types.
const (
	ItemBerthFee ItemType = "berth_fee"
	ItemService  ItemType = "service"
	ItemUtility  ItemType = "utility"
	ItemProduct  ItemType = "product"
	ItemLabour   ItemType = "labour"
	ItemMaterial ItemType = "material"
	ItemFee      ItemType = "fee"
	ItemDeposit  ItemType = "deposit"
)

// ItemTypes returns every item type.
func ItemTypes() []ItemType {
	return []ItemType{ItemBerthFee, ItemService, ItemUtility, ItemProduct, ItemLabour, ItemMaterial, ItemFee, ItemDeposit}
}

// PeriodKind is the kind of a rate period: events override seasons.
type PeriodKind string

// Period kinds.
const (
	PeriodSeason PeriodKind = "season"
	PeriodEvent  PeriodKind = "event"
)

// Line kinds.
const (
	LineBerth    = "berth"
	LineService  = "service"
	LineIncluded = "included"
)

// Item is what the engine needs of an item.
type Item struct {
	ID               string   `json:"id"`
	Code             string   `json:"code"`
	Type             ItemType `json:"item_type"`
	Unit             string   `json:"unit"` // informational unit of measure (12.1)
	VATRateBP        int64    `json:"vat_rate_bp"`
	VATNature        string   `json:"vat_nature,omitempty"` // a FatturaPA nature when VATRateBP == 0
	PriceIncludesVAT bool     `json:"price_includes_vat"`
	RevenueClass     string   `json:"revenue_class,omitempty"` // revenue class code (copied to lines)
	ExternalCode     string   `json:"external_code,omitempty"`
	Active           bool     `json:"active"`
}

// DateRange is an inclusive range of nights.
type DateRange struct {
	From calendar.Date `json:"from"`
	To   calendar.Date `json:"to"`
}

// RatePeriod is a season or an event of a price list.
type RatePeriod struct {
	Code   string      `json:"code"`
	Kind   PeriodKind  `json:"kind"`
	Ranges []DateRange `json:"ranges"`
}

// Band is a length band, inclusive up to MaxCm.
type Band struct {
	Code     string  `json:"code"`
	MaxCm    *geo.Cm `json:"max_cm,omitempty"`   // nil = open-ended (only the last band)
	Category string  `json:"category,omitempty"` // optional berth category linked to the band (matrix probes)
}

// LengthRounding rounds lengths for per_meter, per_m2 and per_m2_berth.
type LengthRounding struct {
	StepCm geo.Cm `json:"step_cm"` // 1, 10, 50, 100
	Mode   string `json:"mode"`    // up, half_even
}

// Length rounding modes.
const (
	LengthRoundUp       = "up"
	LengthRoundHalfEven = "half_even"
)

// Ratio is Num/Den with Den > 0.
type Ratio struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

// DepositPolicy is a percentage of the total or a fixed amount capped at the total (at most one is set).
type DepositPolicy struct {
	PercentBP   *int64       `json:"percent_bp,omitempty"`
	AmountCents *money.Cents `json:"amount_cents,omitempty"`
}

// ListSettings are the engine settings of a price list (defaults m2-plan §2.3). Map keys are contract types or
// "*" for every other contract type.
type ListSettings struct {
	DimensionSource DimensionSource                `json:"dimension_source"`
	LengthRounding  LengthRounding                 `json:"length_rounding"`
	MinBeamRatio    *Ratio                         `json:"min_beam_ratio,omitempty"` // per_m2: beam >= loa × Num/Den; nil = none
	Split           map[ContractType]SplitRule     `json:"split,omitempty"`
	Rounding        map[ContractType]TotalRounding `json:"rounding,omitempty"`
	Deposits        map[ContractType]DepositPolicy `json:"deposits,omitempty"`
	SeasonWindow    *DateRange                     `json:"season_window,omitempty"` // required when seasonal is served
	PrimaryItems    map[ContractType]string        `json:"primary_items,omitempty"` // item id of the berth fee
	// PublishMatrix is stored for the public transit matrix (M8/M12); the engine ignores it.
	PublishMatrix bool `json:"publish_matrix,omitempty"`
}

// AnyContractType is the map key of ListSettings that applies to contract types without their own entry.
const AnyContractType ContractType = "*"

// DefaultSettings returns the defaults of m2-plan §2.3.
func DefaultSettings() ListSettings {
	return ListSettings{
		DimensionSource: DimensionVessel,
		LengthRounding:  LengthRounding{StepCm: 10, Mode: LengthRoundUp},
		MinBeamRatio:    &Ratio{Num: 1, Den: 3},
		Split:           map[ContractType]SplitRule{AnyContractType: SplitProRataDaily},
		Rounding:        map[ContractType]TotalRounding{AnyContractType: RoundNone},
		Deposits:        map[ContractType]DepositPolicy{},
	}
}

// PriceList is the list part of a catalog.
type PriceList struct {
	ID            string         `json:"id"`
	Code          string         `json:"code"`
	ValidFrom     calendar.Date  `json:"valid_from"`
	ValidTo       calendar.Date  `json:"valid_to"`
	ContractTypes []ContractType `json:"contract_types"`
	Settings      ListSettings   `json:"settings"`
	Periods       []RatePeriod   `json:"periods"`
	Bands         []Band         `json:"bands"`
}

// Scope restricts a rule; an empty list means any value.
type Scope struct {
	ContractTypes      []ContractType `json:"contract_types,omitempty"`
	Categories         []string       `json:"categories,omitempty"`
	GroupIDs           []string       `json:"group_ids,omitempty"`
	Channels           []Channel      `json:"channels,omitempty"`
	CustomerCategories []string       `json:"customer_categories,omitempty"`
	Periods            []string       `json:"periods,omitempty"` // rate period codes
	// TimeBands are rental half-day band codes (M11): honoured only by RentalQuote, omitempty so that published
	// catalogues stay byte-identical (INV-26).
	TimeBands []string `json:"time_bands,omitempty"`
}

// Conditions of modifiers and discounts; nil = absent.
type Conditions struct {
	HullTypes     []string `json:"hull_types,omitempty"`
	OverhangMinCm *geo.Cm  `json:"overhang_min_cm,omitempty"`
	MinNights     *int     `json:"min_nights,omitempty"`
	MaxNights     *int     `json:"max_nights,omitempty"`
	LeadMinDays   *int     `json:"lead_min_days,omitempty"`
	LeadMaxDays   *int     `json:"lead_max_days,omitempty"`
}

// Effect of a modifier or discount: exactly one of the two is set.
type Effect struct {
	PercentBP   *int64       `json:"percent_bp,omitempty"`
	AmountCents *money.Cents `json:"amount_cents,omitempty"`
}

// Inclusion is a service included in a base rule.
type Inclusion struct {
	ItemID string    `json:"item_id"`
	Qty    qty.Milli `json:"qty_milli"`
}

// Rule is a base rule, a modifier or a discount of a price list.
type Rule struct {
	ID        string `json:"id"`
	Seq       int64  `json:"seq"` // recency: higher = more recent (assigned by the service on save)
	Stage     Stage  `json:"stage"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Priority  int64  `json:"priority"` // -1000..1000, higher wins
	Stackable bool   `json:"stackable"`
	// base
	ItemID      string                 `json:"item_id,omitempty"`
	Basis       Basis                  `json:"basis,omitempty"`
	Unit        TimeUnit               `json:"unit,omitempty"`
	AmountCents *money.Cents           `json:"amount_cents,omitempty"` // flat; default for per_berth and length_band
	UnitCents   *money.Cents           `json:"unit_cents,omitempty"`   // per_meter (¢/m), per_m2 and per_m2_berth (¢/m²), metered (¢/unit)
	Amounts     map[string]money.Cents `json:"amounts,omitempty"`      // length_band: band code → ¢; per_berth: category → ¢
	Includes    []Inclusion            `json:"includes,omitempty"`
	// modifier / discount
	TargetItemIDs   []string     `json:"target_item_ids,omitempty"`
	TargetItemTypes []ItemType   `json:"target_item_types,omitempty"` // default [berth_fee] when both target lists are empty
	Conditions      Conditions   `json:"conditions"`
	Effect          Effect       `json:"effect"`
	DiscountKind    DiscountKind `json:"discount_kind,omitempty"`
	PromoCode       string       `json:"promo_code,omitempty"` // upper case; discount_kind = promo
	Scope           Scope        `json:"scope"`
}

// Catalog is everything a quote needs: one price list, its rules and the items they reference.
type Catalog struct {
	PriceList PriceList `json:"price_list"`
	Items     []Item    `json:"items"`
	Rules     []Rule    `json:"rules"`
}

// Vessel holds the effective dimensions chosen by the service (measured or declared) and the hull type.
type Vessel struct {
	LOA      geo.Cm `json:"loa_cm"`
	Beam     geo.Cm `json:"beam_cm"`
	HullType string `json:"hull_type"`
}

// Resource is the berth (or area, land resource) being priced; every field is optional.
type Resource struct {
	Category string   `json:"category,omitempty"`
	Length   geo.Cm   `json:"length_cm,omitempty"`
	Width    geo.Cm   `json:"width_cm,omitempty"`
	GroupIDs []string `json:"group_ids,omitempty"`
}

// ServiceRequest is an added service; Qty 0 means 1.
type ServiceRequest struct {
	ItemID string    `json:"item_id"`
	Qty    qty.Milli `json:"qty_milli,omitempty"`
}

// Input is one quote request. Nights are [Arrival, Departure) (ADR 0002).
type Input struct {
	ContractType     ContractType     `json:"contract_type"`
	Arrival          calendar.Date    `json:"arrival"`
	Departure        calendar.Date    `json:"departure"`
	ItemID           string           `json:"item_id,omitempty"` // "" = PriceList.Settings.PrimaryItems[ContractType]
	Vessel           *Vessel          `json:"vessel,omitempty"`
	Resource         Resource         `json:"resource"`
	Channel          Channel          `json:"channel,omitempty"` // "" = desk
	CustomerCategory string           `json:"customer_category,omitempty"`
	PromoCode        string           `json:"promo_code,omitempty"`
	QuotedOn         calendar.Date    `json:"quoted_on"` // "today" in the marina timezone, explicit
	Services         []ServiceRequest `json:"services,omitempty"`
	Deposit          *DepositPolicy   `json:"deposit,omitempty"` // overrides the list policy
}

// Applied is the effect of one modifier or discount rule.
type Applied struct {
	RuleID string      `json:"rule_id"`
	Delta  money.Cents `json:"delta_cents"`
}

// Block is one priced time unit of a line.
type Block struct {
	From      calendar.Date `json:"from"`
	To        calendar.Date `json:"to"` // exclusive; may exceed Departure (best_combination overshoot)
	Unit      TimeUnit      `json:"unit"`
	Nights    int           `json:"nights"`           // nights of the stay covered by this block
	Period    string        `json:"period,omitempty"` // period of the first night
	RuleID    string        `json:"rule_id"`
	Base      money.Cents   `json:"base_cents"`
	Modifiers []Applied     `json:"modifiers"`
	Amount    money.Cents   `json:"amount_cents"` // base + modifiers, >= 0
}

// Line is one line of a breakdown: the berth fee, an included service or an added service.
type Line struct {
	Seq              int           `json:"seq"`
	Kind             string        `json:"kind"` // berth, service, included
	ItemID           string        `json:"item_id"`
	ItemCode         string        `json:"item_code"`
	ItemType         ItemType      `json:"item_type"`
	RevenueClass     string        `json:"revenue_class,omitempty"`
	ExternalCode     string        `json:"external_code,omitempty"`
	Qty              qty.Milli     `json:"qty_milli"`
	Blocks           []Block       `json:"blocks"`
	Base             money.Cents   `json:"base_cents"`
	ModifierTotal    money.Cents   `json:"modifier_total_cents"`
	Discounts        []Applied     `json:"discounts"`
	Amount           money.Cents   `json:"amount_cents"` // after discounts, in the item's price terms (net or gross)
	RoundingShare    money.Cents   `json:"rounding_share_cents"`
	Gross            money.Cents   `json:"gross_cents"` // Gross = Net + VAT
	Net              money.Cents   `json:"net_cents"`
	VAT              money.Cents   `json:"vat_cents"`
	VATRateBP        int64         `json:"vat_rate_bp"`
	VATNature        string        `json:"vat_nature,omitempty"`
	PriceIncludesVAT bool          `json:"price_includes_vat"`
	ServiceFrom      calendar.Date `json:"service_from"`
	ServiceTo        calendar.Date `json:"service_to"` // last night (inclusive)
}

// VATBucket sums the lines of one rate and nature.
type VATBucket struct {
	RateBP int64       `json:"vat_rate_bp"`
	Nature string      `json:"vat_nature,omitempty"`
	Net    money.Cents `json:"net_cents"`
	VAT    money.Cents `json:"vat_cents"`
	Gross  money.Cents `json:"gross_cents"`
}

// TraceStep is one step of the explanation. Params carry integers as decimal strings, codes and ids; never PII.
type TraceStep struct {
	Code   string            `json:"code"`  // trace.go constants
	Line   int               `json:"line"`  // -1 = whole quote
	Block  int               `json:"block"` // -1 = none
	RuleID string            `json:"rule_id,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// Breakdown is the result of a quote.
type Breakdown struct {
	EngineVersion int          `json:"engine_version"`
	PriceListID   string       `json:"price_list_id"`
	CatalogHash   string       `json:"catalog_hash"` // sha256 of the canonical catalog (hash.go)
	ContractType  ContractType `json:"contract_type"`
	Nights        int          `json:"nights"`
	Lines         []Line       `json:"lines"`
	Subtotal      money.Cents  `json:"subtotal_cents"` // Σ line Base + ModifierTotal, before discounts
	DiscountTotal money.Cents  `json:"discount_total_cents"`
	RoundingTotal money.Cents  `json:"rounding_total_cents"` // signed
	Total         money.Cents  `json:"total_cents"`          // Σ Gross
	Net           money.Cents  `json:"net_cents"`
	VAT           money.Cents  `json:"vat_cents"`
	VATSummary    []VATBucket  `json:"vat_summary"` // sorted by RateBP desc, then Nature
	Deposit       money.Cents  `json:"deposit_cents"`
	Trace         []TraceStep  `json:"trace"`
	Warnings      []string     `json:"warnings"`
}

// Snapshot is what a quote stores to reproduce its price later (INV-20).
type Snapshot struct {
	EngineVersion int       `json:"engine_version"`
	Input         Input     `json:"input"`
	Catalog       Catalog   `json:"catalog"`
	Breakdown     Breakdown `json:"breakdown"`
}

// ---- validation ----

// Issue severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Issue is a problem of a catalog found by Validate (m2-plan §4.8). Errors block activation.
type Issue struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"`
	Ref      IssueRef          `json:"ref"`
	Params   map[string]string `json:"params,omitempty"`
}

// IssueRef points at what an issue is about. ID is the rule, period or band id, or the client key of an unsaved
// one; Field is the snake_case field of that object when known.
type IssueRef struct {
	Kind  string `json:"kind"` // list, settings, period, band, rule, item
	ID    string `json:"id,omitempty"`
	Field string `json:"field,omitempty"`
}

// Issue ref kinds.
const (
	RefList     = "list"
	RefSettings = "settings"
	RefPeriod   = "period"
	RefBand     = "band"
	RefRule     = "rule"
	RefItem     = "item"
)

// Issue codes: errors.
const (
	IssuePeriodOverlap         = "period_overlap"
	IssueRangeInvalid          = "range_invalid"
	IssuePeriodCodeUnknown     = "period_code_unknown"
	IssueBandOrder             = "band_order"
	IssueBandOpenNotLast       = "band_open_not_last"
	IssueBandCodeUnknown       = "band_code_unknown"
	IssueRuleItemUnknown       = "rule_item_unknown"
	IssueRuleItemInactive      = "rule_item_inactive"
	IssueRuleBasisInvalid      = "rule_basis_invalid"
	IssueRuleUnitInvalid       = "rule_unit_invalid"
	IssueRuleEffectInvalid     = "rule_effect_invalid"
	IssuePromoWithoutCode      = "promo_without_code"
	IssueCustomerNoCategory    = "customer_discount_without_category"
	IssueSeasonalWithoutWindow = "seasonal_without_window"
	IssuePrimaryItemMissing    = "primary_item_missing"
	IssueContractTypeUnknown   = "contract_type_unknown"
	IssueAmountOutOfRange      = "amount_out_of_range"
)

// Issue codes: warnings.
const (
	IssuePeriodOutsideValidity = "period_outside_validity"
	IssueValidityNotCovered    = "validity_not_covered_by_periods"
	IssueCategoryUnknown       = "category_unknown"
	IssueSplitUnitMissing      = "split_unit_missing"
	IssueNoBaseRuleForContract = "no_base_rule_for_contract_type"
	IssueDuplicateRule         = "duplicate_rule"
)

// IssueCodes returns every issue code (the i18n catalogue test checks each has a label).
func IssueCodes() []string {
	return []string{
		IssuePeriodOverlap, IssueRangeInvalid, IssuePeriodCodeUnknown, IssueBandOrder, IssueBandOpenNotLast,
		IssueBandCodeUnknown, IssueRuleItemUnknown, IssueRuleItemInactive, IssueRuleBasisInvalid,
		IssueRuleUnitInvalid, IssueRuleEffectInvalid, IssuePromoWithoutCode, IssueCustomerNoCategory,
		IssueSeasonalWithoutWindow, IssuePrimaryItemMissing, IssueContractTypeUnknown, IssueAmountOutOfRange,
		IssuePeriodOutsideValidity, IssueValidityNotCovered, IssueCategoryUnknown, IssueSplitUnitMissing,
		IssueNoBaseRuleForContract, IssueDuplicateRule,
	}
}

// ---- matrix, coverage, renewal ----

// Matrix row kinds.
const (
	RowsBands       = "bands"
	RowsCategories  = "categories"
	RowKindBand     = "band"
	RowKindCategory = "category"
)

// MatrixSpec selects a matrix.
type MatrixSpec struct {
	ContractType     ContractType       `json:"contract_type"`
	ItemID           string             `json:"item_id,omitempty"` // "" = primary item
	Rows             string             `json:"rows,omitempty"`    // bands (default), categories
	Categories       []CoverageCategory `json:"categories,omitempty"`
	Channel          Channel            `json:"channel,omitempty"`
	CustomerCategory string             `json:"customer_category,omitempty"`
	QuotedOn         calendar.Date      `json:"quoted_on"`
}

// MatrixRow is a band or a category with the dimensions of its probe.
type MatrixRow struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"` // band, category
	ProbeLOA  geo.Cm `json:"probe_loa_cm"`
	ProbeBeam geo.Cm `json:"probe_beam_cm"`
}

// MatrixColumn is a period (or "year" / "season") with the stay of its probe.
type MatrixColumn struct {
	Key       string        `json:"key"`
	Arrival   calendar.Date `json:"arrival"`
	Departure calendar.Date `json:"departure"`
}

// MatrixCell is the engine result for one probe: Breakdown or Error (an error code such as no_price:no_rule).
type MatrixCell struct {
	Row       int        `json:"row"`
	Col       int        `json:"col"`
	Probe     Input      `json:"probe"`
	Breakdown *Breakdown `json:"breakdown,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// MatrixResult is a full matrix.
type MatrixResult struct {
	ContractType ContractType   `json:"contract_type"`
	ItemID       string         `json:"item_id"`
	Rows         []MatrixRow    `json:"rows"`
	Columns      []MatrixColumn `json:"columns"`
	Cells        []MatrixCell   `json:"cells"`
}

// CoverageCategory is a berth category with the dimensions of its probe (largest elements of the category).
type CoverageCategory struct {
	Code        string `json:"code"`
	LOA         geo.Cm `json:"loa_cm"`
	Beam        geo.Cm `json:"beam_cm"`
	BerthLength geo.Cm `json:"berth_length_cm"`
	BerthWidth  geo.Cm `json:"berth_width_cm"`
}

// CoverageSpec selects a coverage check.
type CoverageSpec struct {
	Categories []CoverageCategory `json:"categories"`
	QuotedOn   calendar.Date      `json:"quoted_on"`
}

// Gap is a combination without a price.
type Gap struct {
	ContractType ContractType `json:"contract_type"`
	Category     string       `json:"category,omitempty"`
	Band         string       `json:"band,omitempty"`
	Period       string       `json:"period,omitempty"`
	ItemID       string       `json:"item_id"`
	Reason       string       `json:"reason"`
}

// CoverageReport lists the gaps and how many combinations were checked.
type CoverageReport struct {
	Gaps    []Gap `json:"gaps"`
	Checked int   `json:"checked"`
}

// Adjustment scopes.
const (
	AdjustAll      = "all"
	AdjustItem     = "item"
	AdjustCategory = "category"
)

// Adjustment is a percent or fixed increase of base-rule amounts in a renewal.
type Adjustment struct {
	Scope       string       `json:"scope"` // all, item, category
	ItemID      string       `json:"item_id,omitempty"`
	Category    string       `json:"category,omitempty"`
	PercentBP   *int64       `json:"percent_bp,omitempty"`
	AmountCents *money.Cents `json:"amount_cents,omitempty"`
}

// RenewalSpec describes a renewal or copy of a price list (spec 12.6).
type RenewalSpec struct {
	ShiftYears     int            `json:"shift_years"` // 0..5; 0 = copy
	ISTATBP        int64          `json:"istat_bp"`    // 0..10000, applied first to every adjustable amount
	Adjustments    []Adjustment   `json:"adjustments,omitempty"`
	AmountRounding AmountRounding `json:"amount_rounding"`
	Code           string         `json:"code"`
	ValidFrom      *calendar.Date `json:"valid_from,omitempty"` // overrides after the shift
	ValidTo        *calendar.Date `json:"valid_to,omitempty"`
}

// DiffCell compares one matrix cell before and after a renewal; nil means no price.
type DiffCell struct {
	Row        int          `json:"row"`
	Col        int          `json:"col"`
	OldCents   *money.Cents `json:"old_cents,omitempty"`
	NewCents   *money.Cents `json:"new_cents,omitempty"`
	DeltaCents *money.Cents `json:"delta_cents,omitempty"`
	DeltaBP    *int64       `json:"delta_bp,omitempty"` // nil when old is 0 or missing
}

// MatrixDiff is the matrix of a list before and after a renewal, for one contract type.
type MatrixDiff struct {
	ContractType ContractType `json:"contract_type"`
	Before       MatrixResult `json:"before"`
	After        MatrixResult `json:"after"`
	Cells        []DiffCell   `json:"cells"`
}
