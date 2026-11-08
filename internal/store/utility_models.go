package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M9 utility tables (internal/migrations/*/00019_maintenance_utilities.sql, m9-plan §9, §13).
// Owned by the UTILITIES builder after the contract phase; PLANNER reads them for the utilities report. Meter
// values and consumptions are qty milli.

// Utility enums (m9-plan §9).
const (
	MeterPower  = "power"
	MeterWater  = "water"
	MeterHours  = "hours"
	MeterCycles = "cycles"

	ReadingRegular        = "regular"
	ReadingInitial        = "initial"
	ReadingReplacementOld = "replacement_old"
	ReadingReplacementNew = "replacement_new"

	ReadingValid     = "valid"
	ReadingAnomaly   = "anomaly"
	ReadingConfirmed = "confirmed"
	ReadingVoid      = "void"

	ConsumptionPending    = "pending"
	ConsumptionAnomaly    = "anomaly"
	ConsumptionUnassigned = "unassigned"
	ConsumptionCharged    = "charged"
	ConsumptionExcluded   = "excluded"

	// ConsumptionShareUnassigned is utility_consumptions.share_key of the nights without an occupant.
	ConsumptionShareUnassigned = "unassigned"

	UtilityModeWithInstallment = "with_installment"
	UtilityModeMonthly         = "monthly"
)

// UtilitySettings is the row of utility_settings of a marina (missing row = defaults).
type UtilitySettings struct {
	bun.BaseModel `bun:"table:utility_settings"`

	MarinaID                 string    `bun:"marina_id,pk"`
	PowerItemID              string    `bun:"power_item_id,nullzero"`
	WaterItemID              string    `bun:"water_item_id,nullzero"`
	Mode                     string    `bun:"mode"`
	PowerDailyThresholdMilli *int64    `bun:"power_daily_threshold_milli"`
	WaterDailyThresholdMilli *int64    `bun:"water_daily_threshold_milli"`
	Version                  int       `bun:"version"`
	CreatedAt                time.Time `bun:"created_at"`
	UpdatedAt                time.Time `bun:"updated_at"`
}

// UtilityMeter is a row of utility_meters.
type UtilityMeter struct {
	bun.BaseModel `bun:"table:utility_meters"`

	ID                  string    `bun:"id,pk"`
	MarinaID            string    `bun:"marina_id"`
	Code                string    `bun:"code"`
	Kind                string    `bun:"kind"`
	Unit                string    `bun:"unit"`
	PedestalID          string    `bun:"pedestal_id,nullzero"`
	SocketCode          string    `bun:"socket_code,nullzero"`
	Serial              string    `bun:"serial,nullzero"`
	ResourceType        string    `bun:"resource_type,nullzero"`
	ResourceID          string    `bun:"resource_id,nullzero"`
	EquipmentID         string    `bun:"equipment_id,nullzero"`
	ItemID              string    `bun:"item_id,nullzero"`
	MultiplierMilli     int64     `bun:"multiplier_milli"`
	RolloverMilli       *int64    `bun:"rollover_milli"`
	DailyThresholdMilli *int64    `bun:"daily_threshold_milli"`
	Active              bool      `bun:"active"`
	Version             int       `bun:"version"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
}

// MeterReading is a row of meter_readings (cumulative index).
type MeterReading struct {
	bun.BaseModel `bun:"table:meter_readings"`

	ID           string        `bun:"id,pk"`
	MarinaID     string        `bun:"marina_id"`
	MeterID      string        `bun:"meter_id"`
	ReadAt       time.Time     `bun:"read_at"`
	ReadOn       calendar.Date `bun:"read_on"` // marina-local date of ReadAt
	ValueMilli   int64         `bun:"value_milli"`
	Kind         string        `bun:"kind"`
	Source       string        `bun:"source"`
	PhotoFileID  string        `bun:"photo_file_id,nullzero"`
	UserID       string        `bun:"user_id,nullzero"`
	Note         string        `bun:"note,nullzero"`
	Status       string        `bun:"status"`
	AnomalyCodes JSON          `bun:"anomaly_codes"`
	ClientRef    string        `bun:"client_ref,nullzero"`
	VoidReason   string        `bun:"void_reason,nullzero"`
	Version      int           `bun:"version"`
	CreatedAt    time.Time     `bun:"created_at"`
	UpdatedAt    time.Time     `bun:"updated_at"`
}

// UtilityBilling is a row of utility_billings: the origin of one utility charge (or adjustment) of a contract.
type UtilityBilling struct {
	bun.BaseModel `bun:"table:utility_billings"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	ContractID      string        `bun:"contract_id"`
	Kind            string        `bun:"kind"` // primary, adjustment
	RunOn           calendar.Date `bun:"run_on"`
	ChargeID        string        `bun:"charge_id,nullzero"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time     `bun:"created_at"`
}

// UtilityConsumption is a row of utility_consumptions: the share of one occupant (or unassigned) of the interval
// between two readings.
type UtilityConsumption struct {
	bun.BaseModel `bun:"table:utility_consumptions"`

	ID            string        `bun:"id,pk"`
	MarinaID      string        `bun:"marina_id"`
	MeterID       string        `bun:"meter_id"`
	FromReadingID string        `bun:"from_reading_id"`
	ToReadingID   string        `bun:"to_reading_id"`
	ShareKey      string        `bun:"share_key"` // allocation id or ConsumptionShareUnassigned
	ContractID    string        `bun:"contract_id,nullzero"`
	AllocationID  string        `bun:"allocation_id,nullzero"`
	CustomerID    string        `bun:"customer_id,nullzero"`
	ResourceType  string        `bun:"resource_type,nullzero"`
	ResourceID    string        `bun:"resource_id,nullzero"`
	FirstNight    calendar.Date `bun:"first_night"`
	LastNight     calendar.Date `bun:"last_night"`
	QtyMilli      int64         `bun:"qty_milli"`
	Status        string        `bun:"status"`
	ChargeID      string        `bun:"charge_id,nullzero"`
	BillingID     string        `bun:"billing_id,nullzero"`
	ExcludeReason string        `bun:"exclude_reason,nullzero"`
	Version       int           `bun:"version"`
	CreatedAt     time.Time     `bun:"created_at"`
	UpdatedAt     time.Time     `bun:"updated_at"`
}
