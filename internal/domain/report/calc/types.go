package calc

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// CONTRACT (m7-plan §12.1): the types below mirror the OpenAPI report schemas field by field (snake_case JSON
// name → Go name) and are frozen for the builders. Enumerations are plain strings carrying the OpenAPI values.

// ReportKey is the path segment of GET .../reports/{key} (OpenAPI ReportKey).
type ReportKey string

// Report keys (m7-plan §1.1). KeyYard and KeyRental are reserved for M10/M11 and never served in M7.
const (
	KeyOccupancy       ReportKey = "occupancy"
	KeyEfficiency      ReportKey = "efficiency"
	KeyAllocationWaste ReportKey = "allocation-waste"
	KeyLostDemand      ReportKey = "lost-demand"
	KeyCustomers       ReportKey = "customers"
	KeyProduction      ReportKey = "production"
	KeyAccruals        ReportKey = "accruals"
	KeyCharges         ReportKey = "charges"
	KeyPayments        ReportKey = "payments"
	KeyRevenueClasses  ReportKey = "revenue-classes"
	KeyComparison      ReportKey = "comparison"
	KeyReceivables     ReportKey = "receivables"
	KeyPresences       ReportKey = "presences"
	KeyRights          ReportKey = "rights"
	KeyCash            ReportKey = "cash"
	KeyVessels         ReportKey = "vessels"
	KeyBerthRevenue    ReportKey = "berth-revenue"
	KeyYard            ReportKey = "yard"
	KeyRental          ReportKey = "rental"
)

// Unit is the unit of an integer value (OpenAPI ReportUnit).
type Unit string

// Units.
const (
	UnitCents         Unit = "cents"
	UnitBP            Unit = "bp"
	UnitCount         Unit = "count"
	UnitCm            Unit = "cm"
	UnitCm2           Unit = "cm2"
	UnitCentsPerM     Unit = "cents_per_m"
	UnitCentsPerNight Unit = "cents_per_night"
	UnitBerthNight    Unit = "berth_night"
	UnitCmNight       Unit = "cm_night"
	UnitCm2Night      Unit = "cm2_night"
)

// BP is a ratio in integer basis points; OK is false when the denominator is zero (JSON null). Ratios are
// recomputed from Σ numerator / Σ denominator at every level, never averaged (INV-64).
type BP struct {
	Value int64
	OK    bool
}

// Ptr returns the value for the API (nil when not OK).
func (b BP) Ptr() *int64 {
	if !b.OK {
		return nil
	}
	v := b.Value
	return &v
}

// Opt is an optional integer (JSON null when !OK): an amount hidden without reports.all, an unpriced value, an
// average with no denominator.
type Opt[T ~int64] struct {
	Value T
	OK    bool
}

// Ptr returns the value for the API (nil when not OK).
func (o Opt[T]) Ptr() *int64 {
	if !o.OK {
		return nil
	}
	v := int64(o.Value)
	return &v
}

// Ref links a record to its entity page (OpenAPI ReportRef; Type is one of its enum values).
type Ref struct {
	Type  string
	ID    string
	Label string
}

// Note is a counted remark about a result (OpenAPI ReportNote).
type Note struct {
	Code  string
	Count int64
}

// Group is one group_by bucket with the totals recomputed from its records.
type Group[T any] struct {
	Key    string
	Label  string
	Totals T
}

// Page is the page of the selected section (Total counts every record of the section).
type Page struct {
	Page     int
	PageSize int
	Total    int64
}

// Envelope holds the fields common to every report result (OpenAPI ReportEnvelope).
type Envelope struct {
	Report         ReportKey
	From, To       calendar.Date
	Today          calendar.Date
	Filters        map[string]string
	IgnoredFilters []string
	GroupBy        string
	Section        string
	Page           Page
	Notes          []Note
}

// Result is a report: Totals over every record of every section (only ever produced by the fold of Records
// before paging), Groups by group_by, Records of the selected section (the page for JSON, all of them for an
// export).
type Result[T, R any] struct {
	Envelope
	Totals  T
	Groups  []Group[T]
	Records []R
}

// ---- R1 occupancy ----

// OccupancyTotals (OpenAPI OccupancyTotals).
type OccupancyTotals struct {
	Unit              Unit
	OccupiedUnits     int64
	AvailableUnits    int64
	ClosedUnits       int64
	OutOfServiceUnits int64
	ReleasedUnits     int64
	OccupancyBP       BP
}

// OccupancyRecord (OpenAPI OccupancyRecord): Role numerator or denominator.
type OccupancyRecord struct {
	Role              string
	ResourceID        string
	ResourceCode      string
	ResourceType      string
	MapID             string
	Category          string
	Kind              string // contract, transit, owner, release ("" on denominator records)
	AllocationID      string
	ContractID        string
	ContractNumber    string
	OccupiedUnits     int64
	AvailableUnits    int64
	ClosedUnits       int64
	OutOfServiceUnits int64
	Ref               Ref
}

// OccupancyResult is the R1 report.
type OccupancyResult struct {
	Result[OccupancyTotals, OccupancyRecord]
	Variant string // physical, contractual
	Family  string // berth, land, linear_area, surface_area
}

// ---- R2 efficiency ----

// EfficiencyTotals (OpenAPI EfficiencyTotals).
type EfficiencyTotals struct {
	Unit          Unit
	Nights        int64
	VesselUnits   int64
	ResourceUnits int64
	EfficiencyBP  BP
}

// EfficiencyRecord (OpenAPI EfficiencyRecord).
type EfficiencyRecord struct {
	ResourceID     string
	ResourceCode   string
	ResourceType   string
	AllocationID   string
	Kind           string
	ContractID     string
	ContractNumber string
	VesselID       string
	VesselName     string
	LOA, Beam      geo.Cm
	Nights         int64
	VesselUnits    int64
	ResourceUnits  int64
	EfficiencyBP   BP
	Ref            Ref
}

// EfficiencyResult is the R2 report.
type EfficiencyResult struct {
	Result[EfficiencyTotals, EfficiencyRecord]
	Family string
}

// ---- R3 allocation waste ----

// WasteTotals (OpenAPI WasteTotals): money is !OK without reports.all.
type WasteTotals struct {
	Count         int64
	Nights        int64
	WasteBPNights int64
	Applied       Opt[money.Cents]
	List          Opt[money.Cents]
	Lost          Opt[money.Cents]
	Unpriced      int64
}

// WasteRecord (OpenAPI WasteRecord).
type WasteRecord struct {
	AllocationID   string
	Kind           string
	ContractID     string
	ContractNumber string
	BerthID        string
	BerthCode      string
	Category       string
	VesselID       string
	VesselName     string
	LOA, Beam      geo.Cm
	BerthLength    geo.Cm
	BerthWidth     geo.Cm
	FillBP         int64
	WasteBP        int64
	NightsInPeriod int64
	Applied        Opt[money.Cents]
	List           Opt[money.Cents]
	Lost           Opt[money.Cents]
	Ref            Ref
}

// ResourceBrief identifies a berth in a suggestion.
type ResourceBrief struct {
	ID   string
	Code string
}

// WasteSuggestion (OpenAPI WasteSuggestion): Kind swap or move.
type WasteSuggestion struct {
	Kind          string
	AllocationIDs []string
	FromBerths    []ResourceBrief
	ToBerths      []ResourceBrief
	WasteBeforeBP int64
	WasteAfterBP  int64
	GainCm2Nights int64
}

// WasteResult is the R3 report.
type WasteResult struct {
	Result[WasteTotals, WasteRecord]
	ThresholdBP int64
	Suggestions []WasteSuggestion
}

// ---- R4 lost demand ----

// LostDemandTotals (OpenAPI LostDemandTotals).
type LostDemandTotals struct {
	Count    int64
	Nights   int64
	Value    Opt[money.Cents]
	Unpriced int64
}

// LostDemandRecord (OpenAPI LostDemandRecord).
type LostDemandRecord struct {
	InquiryID      string
	Arrival        calendar.Date
	Departure      calendar.Date
	NightsInPeriod int64
	Outcome        string
	Purpose        string
	Term           string
	Channel        string
	Source         string
	LOA, Beam      geo.Cm
	LengthBand     string
	PricedCategory string
	Value          Opt[money.Cents]
	Ref            Ref
}

// LostDemandResult is the R4 report.
type LostDemandResult = Result[LostDemandTotals, LostDemandRecord]

// ---- R5 customers (+ renewals) ----

// CustomersTotals (OpenAPI CustomersTotals).
type CustomersTotals struct {
	Customers       int64
	NewCustomers    int64
	Contracts       int64
	Value           money.Cents
	Arrears         money.Cents
	RenewalEligible int64
	RenewalRenewed  int64
	RenewalRateBP   BP
}

// CustomersRecord (OpenAPI CustomersRecord): Section customers or renewals. CustomerName is set only when the
// principal holds customers.read.
type CustomersRecord struct {
	Section            string
	CustomerID         string
	CustomerName       string
	Category           string
	Nationality        string
	FirstContractOn    calendar.Date
	AcquisitionChannel string
	SeniorityYears     int64
	SeniorityBand      string
	New                int64
	Value              money.Cents
	Arrears            money.Cents
	Contracts          int64
	ContractID         string
	ContractNumber     string
	Term               string
	LastNight          calendar.Date
	Renewed            int64
	NewContractID      string
	Ref                Ref
}

// CustomersResult is the R5 report.
type CustomersResult = Result[CustomersTotals, CustomersRecord]

// ---- R6 production ----

// ProductionTotals (OpenAPI ProductionTotals).
type ProductionTotals struct {
	Contracts         int64
	Nights            int64
	Competence        money.Cents
	Transits          int64
	TransitNights     int64
	TransitCompetence money.Cents
	TransitAvgNight   Opt[money.Cents]
}

// ProductionRecord (OpenAPI ProductionRecord).
type ProductionRecord struct {
	ContractID     string
	Number         string
	Kind           string
	Term           string
	Status         string
	Channel        string
	CustomerID     string
	CustomerName   string
	Category       string
	StartsOn       calendar.Date
	EndsOn         calendar.Date
	Total          money.Cents
	NightsInPeriod int64
	Competence     money.Cents
	Ref            Ref
}

// ProductionResult is the R6 report.
type ProductionResult = Result[ProductionTotals, ProductionRecord]

// ---- R7 accruals ----

// AccrualsTotals (OpenAPI AccrualsTotals).
type AccrualsTotals struct {
	ClosingDate     calendar.Date
	CompetenceNet   money.Cents
	CompetenceGross money.Cents
	DeferralNet     money.Cents
	DeferralGross   money.Cents
	Accrual         money.Cents
}

// AccrualsRecord (OpenAPI AccrualsRecord): Section competence, deferral or accrual.
type AccrualsRecord struct {
	Section         string
	Date            calendar.Date
	ChargeID        string
	ChargeNumber    string
	LineSeq         int
	ContractID      string
	ContractNumber  string
	CustomerID      string
	CustomerName    string
	Description     string
	ServiceFrom     calendar.Date
	ServiceTo       calendar.Date
	AmountNet       money.Cents
	AmountGross     money.Cents
	CompetenceNet   money.Cents
	CompetenceGross money.Cents
	DeferralNet     money.Cents
	DeferralGross   money.Cents
	Matured         money.Cents
	Charged         money.Cents
	Accrual         money.Cents
	Ref             Ref
}

// AccrualsResult is the R7 report.
type AccrualsResult = Result[AccrualsTotals, AccrualsRecord]

// ---- R8 charges and R10 records ----

// ChargesTotals (OpenAPI ChargesTotals).
type ChargesTotals struct {
	Charges int64
	Lines   int64
	Net     money.Cents
	VAT     money.Cents
	Gross   money.Cents
	Charged money.Cents
}

// ChargesRecord (OpenAPI ChargesRecord): one charge line (competence basis: its quota in the period).
type ChargesRecord struct {
	ChargeID        string
	Number          string
	Seq             int
	IssuedOn        calendar.Date
	DueOn           calendar.Date
	Month           string // YYYY-MM
	OriginType      string
	CustomerID      string
	CustomerName    string
	ItemID          string
	ItemCode        string
	Description     string
	RevenueClass    string
	VATRateBP       int64
	VATNature       string
	ServiceFrom     calendar.Date
	ServiceTo       calendar.Date
	Net             money.Cents
	VAT             money.Cents
	Gross           money.Cents
	PaymentStatus   string
	InvoicingStatus string
	FiscalMode      string
	Ref             Ref
}

// ChargesResult is the R8 report.
type ChargesResult struct {
	Result[ChargesTotals, ChargesRecord]
	Basis string // issued, competence
}

// ---- R9 payments ----

// PaymentsTotals (OpenAPI PaymentsTotals).
type PaymentsTotals struct {
	Count     int64
	Collected money.Cents
	Payments  money.Cents
	Refunds   money.Cents
	Fees      money.Cents
	Payouts   money.Cents
}

// PaymentsRecord (OpenAPI PaymentsRecord): Section main or payouts; Amount signed.
type PaymentsRecord struct {
	Section      string
	Kind         string
	ID           string
	Number       string
	Date         calendar.Date
	Method       string
	Source       string
	ProviderCode string
	CustomerID   string
	CustomerName string
	Amount       money.Cents
	Fee          money.Cents
	Ref          Ref
}

// PaymentsResult is the R9 report.
type PaymentsResult = Result[PaymentsTotals, PaymentsRecord]

// ---- R10 revenue classes ----

// RevenueClassMonth is the net of a class in one month.
type RevenueClassMonth struct {
	Month string // YYYY-MM
	Net   money.Cents
}

// RevenueClassGroup (OpenAPI RevenueClassGroup).
type RevenueClassGroup struct {
	Key       string
	Label     string
	ParentKey string
	Net       money.Cents
	ShareBP   BP
	Months    []RevenueClassMonth
}

// RevenueClassesTotals (OpenAPI RevenueClassesTotals).
type RevenueClassesTotals struct {
	Lines        int64
	Net          money.Cents
	Unclassified money.Cents
}

// RevenueClassesResult is the R10 report: Groups are the classes (not Group[T]).
type RevenueClassesResult struct {
	Envelope
	Basis   string
	Totals  RevenueClassesTotals
	Classes []RevenueClassGroup
	Records []ChargesRecord
}

// ---- R12 receivables ----

// AgeingAmounts are residuals by AgeingBucket.
type AgeingAmounts struct {
	NotDue, D0_30, D31_60, D61_90, D90Plus, Total money.Cents //nolint:revive // bucket names of the API
}

// ReceivablesTotals (OpenAPI ReceivablesTotals).
type ReceivablesTotals struct {
	AsOf          calendar.Date
	Count         int64
	Open          money.Cents
	Overdue       money.Cents
	Buckets       AgeingAmounts
	Future        money.Cents
	RemindersSent int64
}

// ReceivablesRecord (OpenAPI ReceivablesRecord): Section open, future or reminders.
type ReceivablesRecord struct {
	Section       string
	Kind          string
	ID            string
	Number        string
	CustomerID    string
	CustomerName  string
	OriginType    string
	IssuedOn      calendar.Date
	DueOn         calendar.Date
	Total         money.Cents
	Paid          money.Cents
	Credited      money.Cents
	Residual      money.Cents
	Overdue       bool
	Bucket        string
	ReminderLevel string
	SentOn        calendar.Date
	Ref           Ref
}

// ReceivablesResult is the R12 report.
type ReceivablesResult = Result[ReceivablesTotals, ReceivablesRecord]

// ---- R13 presences ----

// PresencesTotals (OpenAPI PresencesTotals): money !OK without reports.all.
type PresencesTotals struct {
	Arrivals       int64
	Departures     int64
	AbsenceNights  int64
	Releases       int64
	ReleasedNights int64
	ResoldNights   int64
	ReleaseCredit  Opt[money.Cents]
	ReleaseRevenue Opt[money.Cents]
}

// PresencesRecord (OpenAPI PresencesRecord): Section movements, absences or releases.
type PresencesRecord struct {
	Section        string
	PresenceID     string
	Event          string
	Night          calendar.Date
	ContractID     string
	ContractNumber string
	ContractKind   string
	VesselID       string
	VesselName     string
	BerthCode      string
	AbsenceNights  int64
	ReleaseID      string
	ReleaseFrom    calendar.Date
	ReleaseTo      calendar.Date
	ReleasedNights int64
	ResoldNights   int64
	Credit         Opt[money.Cents]
	Revenue        Opt[money.Cents]
	Ref            Ref
}

// PresencesResult is the R13 report.
type PresencesResult = Result[PresencesTotals, PresencesRecord]

// ---- R14 rights ----

// RightsTotals (OpenAPI RightsTotals).
type RightsTotals struct {
	Rights           int64
	Holders          int64
	Berths           int64
	ChargesIssued    money.Cents
	ChargesCollected money.Cents
	ChargesResidual  money.Cents
	Advance          money.Cents
	Balance          money.Cents
	MarinaShare      money.Cents
	MandateRevenue   money.Cents
	MandatePayout    money.Cents
}

// RightsRecord (OpenAPI RightsRecord): Section holders, charges, allotments or mandates.
type RightsRecord struct {
	Section     string
	RightID     string
	RightNumber string
	BerthID     string
	BerthCode   string
	HolderID    string
	HolderName  string
	ShareMillis int64
	ChargeID    string
	Number      string
	IssuedOn    calendar.Date
	Issued      money.Cents
	Paid        money.Cents
	Residual    money.Cents
	RunID       string
	RunKind     string
	ItemCode    string
	MandateID   string
	Kind        string
	Date        calendar.Date
	Amount      money.Cents
	Ref         Ref
}

// RightsResult is the R14 report.
type RightsResult = Result[RightsTotals, RightsRecord]

// ---- R15 cash ----

// CashVATTotal is the corrispettivi of one VAT rate/nature.
type CashVATTotal struct {
	RateBP int64
	Nature string
	Net    money.Cents
	VAT    money.Cents
	Gross  money.Cents
}

// CashMethodTotal is the corrispettivi of one payment method.
type CashMethodTotal struct {
	Method string
	Total  money.Cents
}

// CashTotals (OpenAPI CashTotals).
type CashTotals struct {
	Sessions    int64
	Difference  money.Cents
	Documents   int64
	FiscalTotal money.Cents
	FiscalVoid  money.Cents
	VAT         []CashVATTotal
	Methods     []CashMethodTotal
}

// CashRecord (OpenAPI CashRecord): Section sessions or fiscal.
type CashRecord struct {
	Section      string
	BusinessDate calendar.Date
	SessionID    string
	RegisterCode string
	Status       string
	Float        money.Cents
	Expected     money.Cents
	Counted      money.Cents
	Difference   money.Cents
	DocumentID   string
	Number       string
	Method       string
	Total        money.Cents
	Void         money.Cents
	Ref          Ref
}

// CashResult is the R15 report.
type CashResult = Result[CashTotals, CashRecord]

// ---- R16 vessels ----

// VesselsTotals (OpenAPI VesselsTotals).
type VesselsTotals struct {
	Vessels int64
	Nights  int64
	LOASum  geo.Cm
	AvgLOA  Opt[geo.Cm]
}

// VesselsRecord (OpenAPI VesselsRecord).
type VesselsRecord struct {
	VesselID   string
	Name       string
	Nights     int64
	LOA, Beam  geo.Cm
	HullType   string
	Flag       string
	LengthBand string
	Kind       string
	Ref        Ref
}

// VesselsResult is the R16 report.
type VesselsResult = Result[VesselsTotals, VesselsRecord]

// ---- R17 berth revenue ----

// BerthRevenueTotals (OpenAPI BerthRevenueTotals).
type BerthRevenueTotals struct {
	Berths      int64
	Length      geo.Cm
	Revenue     money.Cents
	RevenuePerM Opt[money.Cents]
}

// BerthRevenueRecord (OpenAPI BerthRevenueRecord).
type BerthRevenueRecord struct {
	BerthID        string
	BerthCode      string
	Category       string
	Length         geo.Cm
	OccupiedNights int64
	Revenue        money.Cents
	RevenuePerM    Opt[money.Cents]
	Ref            Ref
}

// BerthRevenueResult is the R17 report.
type BerthRevenueResult = Result[BerthRevenueTotals, BerthRevenueRecord]

// ---- dashboard tiles (21.2) ----

// TileKey identifies a dashboard tile (OpenAPI TileKey).
type TileKey string

// Tile keys (m7-plan §4). The yard_* and rental_* tiles are seams: always Available=false.
const (
	TileProduction         TileKey = "production"
	TileCharged            TileKey = "charged"
	TileCollected          TileKey = "collected"
	TileReceivablesOpen    TileKey = "receivables_open"
	TileReceivablesOverdue TileKey = "receivables_overdue"
	TileOccupancy          TileKey = "occupancy"
	TileEfficiency         TileKey = "efficiency"
	TileBerthRevenuePerM   TileKey = "berth_revenue_per_m"
	TileTransitAvgNight    TileKey = "transit_avg_night"
	TileNewCustomers       TileKey = "new_customers"
	TileRenewalRate        TileKey = "renewal_rate"
	TileVesselsHosted      TileKey = "vessels_hosted"
	TileVesselsAvgLOA      TileKey = "vessels_avg_loa"
	TileYardOpenOrders     TileKey = "yard_open_orders"
	TileYardHours          TileKey = "yard_hours"
	TileYardMargin         TileKey = "yard_margin"
	TileRentalUtilisation  TileKey = "rental_utilisation"
	TileRentalRevenue      TileKey = "rental_revenue"
)

// Unavailable reasons of the seam tiles.
const (
	ReasonMilestoneM10 = "milestone_m10"
	ReasonMilestoneM11 = "milestone_m11"
)

// TileValue is a tile value with its fraction (ratios).
type TileValue struct {
	Value       Opt[int64]
	Numerator   int64
	Denominator int64
	HasFraction bool
}

// SeriesPoint is one bucket of a tile series (Bucket = first day of the day, ISO week or month).
type SeriesPoint struct {
	Bucket   calendar.Date
	Value    Opt[int64]
	Previous Opt[int64]
}

// Breakdown is one entry of a tile breakdown (top nationalities, channels, hull types, ...).
type Breakdown struct {
	Key   string
	Label string
	Value int64
}

// Drill is the report and parameters (without from/to) that list the records of a tile.
type Drill struct {
	Report ReportKey
	Params map[string]string
}

// Tile is a dashboard tile (OpenAPI Tile). Trend is up, down, flat or none.
type Tile struct {
	Key         TileKey
	Unit        Unit
	Available   bool
	Reason      string
	Current     TileValue
	Previous    *TileValue
	Delta       Opt[int64]
	DeltaBP     BP
	Trend       string
	Series      []SeriesPoint
	Breakdown   []Breakdown
	DimsApplied bool
	Drill       *Drill
}

// Dashboard is the manager dashboard (OpenAPI Dashboard).
type Dashboard struct {
	From, To         calendar.Date
	PrevFrom, PrevTo calendar.Date // zero without comparison
	Today            calendar.Date
	Compare          string // previous_year, none
	Align            string // date, weekday
	Granularity      string // day, week, month
	Filters          map[string]string
	Tiles            []Tile
}

// ---- comparison (R11) ----

// ComparisonPeriod is the value of one period.
type ComparisonPeriod struct {
	From, To    calendar.Date
	Value       Opt[int64]
	Numerator   int64
	Denominator int64
	HasFraction bool
}

// ComparisonPoint is one bucket of the current period with the aligned previous values.
type ComparisonPoint struct {
	Bucket              calendar.Date
	Current             Opt[int64]
	Previous            Opt[int64]
	CurrentNumerator    int64
	CurrentDenominator  int64
	PreviousNumerator   int64
	PreviousDenominator int64
	HasFraction         bool
}

// Comparison is the R11 result (OpenAPI ComparisonResult).
type Comparison struct {
	Indicator   string
	Unit        Unit
	Align       string
	Granularity string
	YearsBack   int
	Filters     map[string]string
	Current     ComparisonPeriod
	Previous    ComparisonPeriod
	Delta       Opt[int64]
	DeltaBP     BP
	Series      []ComparisonPoint
}
