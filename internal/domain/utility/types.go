// Package utility is the utility meters service (spec 20.4; m9-plan §9): meters, readings from the phone (offline
// kind utility.reading, deduplicated by client_ref, INV-83), anomalies, consumptions split by occupant nights
// (utility/consume, INV-91) and their charging through charge.CreateTx with origin utility, one live charge per
// billing (INV-90). Charged intervals are frozen; corrections become linked adjustment charges (INV-51).
// Permissions: readings presence.write; meters and settings marina.configure; review and charging charges.manage.
package utility

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// JobCharge is the hourly charging job (registered after installments.issue).
const JobCharge = "utilities.charge"

// OfflineReplayWindow is how old a queued reading may be for read_at to default to Porto-Offline-Queued-At.
const OfflineReplayWindow = 48 * time.Hour

// MeterInput creates a meter.
type MeterInput struct {
	Code, Kind, Unit                   string
	PedestalID, SocketCode, Serial     string
	ResourceType, ResourceID           string
	EquipmentID, ItemID                string
	MultiplierMilli                    int64 // 0 = 1000
	RolloverMilli, DailyThresholdMilli *int64
	Active                             *bool
}

// MeterPatch changes a meter: fields present in Set are written (nil value = null).
type MeterPatch struct {
	Set map[string]any
}

// MeterFilter filters the meter list (Route "dock" = the dock reading round).
type MeterFilter struct {
	Kind, PedestalID, ResourceID, Route, Q, Cursor string
	Active                                         *bool
	Limit                                          int
}

// MeterView is a meter with its last valid reading.
type MeterView struct {
	Row  store.UtilityMeter
	Last *store.MeterReading
}

// ReadingInput records a reading; Value is the decimal string of the API (qty.Parse, 3 decimals).
type ReadingInput struct {
	MeterID   string
	Value     string
	Kind      string
	Note      string
	ReadAt    *time.Time
	ClientRef string
	// QueuedAt is Porto-Offline-Queued-At of an offline replay (zero = online).
	QueuedAt time.Time
}

// ReadingFilter filters the readings.
type ReadingFilter struct {
	MeterID, Status, Cursor string
	From, To                *calendar.Date
	Limit                   int
}

// ReadingView is a reading with its delta from the previous valid reading.
type ReadingView struct {
	Row   store.MeterReading
	Delta *qty.Milli
}

// ConsumptionFilter filters the consumptions.
type ConsumptionFilter struct {
	Status, ContractID, MeterID, Kind, Cursor string
	From, To                                  *calendar.Date
	Limit                                     int
}

// RunCharge is one charge of a run.
type RunCharge struct {
	ChargeID, BillingID, ContractID string
	Lines                           int
	TotalCents                      int64
	Adjustment                      bool
}

// RunSkip is a consumption or contract left for later, with the reason (no_price, anomaly, unassigned, not_due).
type RunSkip struct {
	ContractID, ConsumptionID, Reason string
}

// RunResult is the outcome of a charging run.
type RunResult struct {
	Charges []RunCharge
	Skipped []RunSkip
}

// SettingsInput replaces the utility settings.
type SettingsInput struct {
	PowerItemID, WaterItemID                           *string
	Mode                                               string
	PowerDailyThresholdMilli, WaterDailyThresholdMilli *int64
}

// PhotoUpload is an uploaded reading photo.
type PhotoUpload struct {
	ContentType string
	Data        []byte
}

// Settings is the utility settings row of a marina.
type Settings = store.UtilitySettings
