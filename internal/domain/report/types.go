package report

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
)

// CONTRACT (m7-plan §12.1): exported parameters, keys and errors of the report service, frozen for the builders.
// Result, record and tile types live in calc (pure, so export and the dashboard can read them without the
// service); they are re-exported here for the handlers.

// Errors of the report service (problem slugs in internal/http/respond/errmap.go).
var (
	// ErrInvalidPeriod: from/to unparsable, to before from, more than MaxPeriodDays, or as_of after today.
	ErrInvalidPeriod = errors.New("report: invalid period")
	// ErrInvalidFilter: an unknown value of an enumerated parameter (group_by, section, variant, ...). Returned
	// wrapped in *FilterError, which names the parameter.
	ErrInvalidFilter = errors.New("report: invalid filter")
	// ErrTooLarge: an export with more than MaxExportRows records.
	ErrTooLarge = errors.New("report: too many records to export")
	// ErrUnknownIndicator: a comparison indicator that is not comparable (m7-plan §5).
	ErrUnknownIndicator = errors.New("report: unknown indicator")
	// ErrNotImplemented is answered by the CONTRACT stubs until the builders land (501 not_implemented).
	ErrNotImplemented = errors.New("report: not implemented")
)

// FilterError names the query parameter whose value is invalid; it unwraps to ErrInvalidFilter.
type FilterError struct {
	Param string
}

func (e *FilterError) Error() string { return "report: invalid filter " + e.Param }

// Unwrap returns ErrInvalidFilter.
func (e *FilterError) Unwrap() error { return ErrInvalidFilter }

// Limits (m7-plan D1, D16, §3.0).
const (
	MaxPeriodDays   = 400
	DefaultPageSize = 100
	MaxPageSize     = 1000
	MaxExportRows   = 200_000
	MaxYearsBack    = 5
)

// Format of a report response.
type Format string

// Formats.
const (
	FormatJSON Format = "json"
	FormatCSV  Format = "csv"
	FormatXLSX Format = "xlsx"
)

// Key aliases calc.ReportKey (path segment of GET .../reports/{key}).
type Key = calc.ReportKey

// Dims are the dimension filters shared by the occupancy family, the vessels, the berth revenue and the
// dashboard (m7-plan §3.0, §4). Empty means "all".
type Dims struct {
	MapID       string
	MapKind     string // water, yard, shed, parking, boxes, other
	StructureID string
	GroupID     string
	Category    string // price_class code
}

// Params is the parsed and validated request of one report (m7-plan §3, §6.3). ParseParams builds it from the
// query string; Values is its exact inverse, used for the tile drill parameters, so a drill link reproduces the
// report request (round trip tested). Fields not meaningful for Report are reported in the result's
// ignored_filters.
type Params struct {
	Report Key

	// From and To are zero when absent: the service defaults them to January 1 of today's year and today.
	From, To calendar.Date
	Format   Format
	Lang     string // it, en ("" = the user's locale, else it)
	Page     int    // 1-based (0 = 1)
	PageSize int    // 0 = DefaultPageSize; max MaxPageSize
	GroupBy  string
	Section  string // record section ("" = the report's first section)

	Dims Dims

	// R1/R2
	Variant      string // physical, contractual
	Family       string // berth, land, linear_area, surface_area
	ResourceType string
	// R3
	ThresholdBP   *int64
	NoSuggestions bool // suggestions=false
	// R4
	Channel string
	Purpose string
	// R5
	NewOnly bool // new=true
	// R6
	Kind string // contract kind (utilities: meter kind)
	// R6, R7, R8, R9, R12
	CustomerID string
	// R8
	ItemID          string
	Origins         []string // charge origin types (R8: default every origin except opening_balance)
	PaymentStatus   string
	InvoicingStatus string
	FiscalMode      string
	// R8, R10
	Basis string // issued, competence
	// R9
	Method string
	// R12
	AsOf        calendar.Date // zero = today
	OverdueOnly bool          // overdue=true
	Origin      string

	// M9 storage (m9-plan §11)
	AreaID           string
	ExitsBlockedOnly bool // exits_blocked_only=true
	// M9 maintenance (status: task status) and utilities (status: consumption status, Kind: meter kind)
	Status          string
	ElementType     string
	TaskOverdueOnly bool // overdue_only=true
	MeterID         string
	ContractID      string

	// M10 yard (m10-plan §12)
	View             string // hours (default), load, margins, margins_by_work_type, lifting
	EmployeeID       string
	WorkOrderID      string
	WorkTypeID       string
	EquipmentID      string
	OpenOnly         bool // open_only=true
	InvoicedInPeriod bool // invoiced_in_period=true

	// M11 rental (m11-plan §11): View is the rental view; the category filter is Dims.Category.
	UnitID    string
	FormulaID string
}

// DashboardParams is the request of GET .../dashboard (m7-plan §4).
type DashboardParams struct {
	From, To calendar.Date // zero = year to date
	Compare  string        // previous_year (default), none
	Align    string        // date (default), weekday
	Dims     Dims
}

// ComparisonParams is the request of GET .../reports/comparison (m7-plan §5).
type ComparisonParams struct {
	Indicator   string
	From, To    calendar.Date
	YearsBack   int    // 1..MaxYearsBack (0 = 1)
	Align       string // date (default), weekday
	Granularity string // day, week, month ("" = by period length)
	Format      Format
	Lang        string
	Dims        Dims
}

// Results re-exported from calc for the handlers.
type (
	OccupancyResult      = calc.OccupancyResult
	EfficiencyResult     = calc.EfficiencyResult
	WasteResult          = calc.WasteResult
	LostDemandResult     = calc.LostDemandResult
	CustomersResult      = calc.CustomersResult
	ProductionResult     = calc.ProductionResult
	AccrualsResult       = calc.AccrualsResult
	ChargesResult        = calc.ChargesResult
	PaymentsResult       = calc.PaymentsResult
	RevenueClassesResult = calc.RevenueClassesResult
	ReceivablesResult    = calc.ReceivablesResult
	PresencesResult      = calc.PresencesResult
	RightsResult         = calc.RightsResult
	CashResult           = calc.CashResult
	VesselsResult        = calc.VesselsResult
	BerthRevenueResult   = calc.BerthRevenueResult
	StorageResult        = calc.StorageResult
	MaintenanceResult    = calc.MaintenanceResult
	UtilitiesResult      = calc.UtilitiesResult
	Dashboard            = calc.Dashboard
	Comparison           = calc.Comparison
)
