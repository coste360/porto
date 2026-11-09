package fleet

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// UnitView is a unit with its vessel data and the computed licence rule (without a skipper).
type UnitView struct {
	store.RentalUnit
	VesselName      string
	EngineKWMilli   int64
	LicenceRequired bool
	HomeBerthCode   string
}

// UnitFilter filters ListUnits.
type UnitFilter struct {
	Status   string
	Category string
	Cursor   string
	Limit    int
}

// UnitInput creates a unit.
type UnitInput struct {
	VesselID        string
	Code            string
	Category        string
	Seats           int
	Licence         string
	FuelType        string
	Tank            qty.Milli
	Deposit         money.Cents
	Status          string
	HomeBerthID     string
	ItemID          string
	Ownership       string
	OwnerCustomerID string
	Note            string
	Sort            int
}

// UnitPatch changes a unit; nil = unchanged, an empty string clears an optional reference.
type UnitPatch struct {
	Code            *string
	Category        *string
	Seats           *int
	Licence         *string
	FuelType        *string
	Tank            *qty.Milli
	Deposit         *money.Cents
	Status          *string
	HomeBerthID     *string
	ItemID          *string
	Ownership       *string
	OwnerCustomerID *string
	Note            *string
	Sort            *int
}

// SkipperView is a skipper with the decrypted contact and the weekly hours (rental.manage only).
type SkipperView struct {
	store.RentalSkipper
	Phone string
	Email string
	Hours []store.RentalSkipperHours
}

// SkipperFilter filters ListSkippers.
type SkipperFilter struct {
	Active *bool
	Cursor string
	Limit  int
}

// SkipperInput creates a skipper.
type SkipperInput struct {
	Name             string
	Phone            string
	Email            string
	LicenceExpiresOn calendar.Date
	UserID           string
	Active           *bool
	Sort             int
}

// SkipperPatch changes a skipper; nil = unchanged, an empty value clears.
type SkipperPatch struct {
	Name             *string
	Phone            *string
	Email            *string
	LicenceExpiresOn *calendar.Date
	ClearLicence     bool
	UserID           *string
	Active           *bool
	Sort             *int
}

// HoursRow is a weekly working window of a skipper (weekday 1 = Monday).
type HoursRow struct {
	Weekday  int
	FromSlot int
	ToSlot   int
}

// FormulaInput creates a formula (marina.configure).
type FormulaInput struct {
	Code            string
	Name            map[string]string
	Kind            string
	BandCode        string
	MinSlots        int
	MaxSlots        int // 0 = the open window
	MinDays         int
	MaxDays         int
	Skipper         string
	Fuel            string
	FuelItemID      string
	LateItemID      string
	GraceMinutes    *int
	LateStepMinutes int
	Categories      []string
	Active          *bool
	Sort            int
}

// FormulaPatch changes a formula; nil = unchanged.
type FormulaPatch struct {
	Code            *string
	Name            map[string]string
	BandCode        *string
	MinSlots        *int
	MaxSlots        *int // pointer to 0 = the open window
	MinDays         *int
	MaxDays         *int
	Skipper         *string
	Fuel            *string
	FuelItemID      *string
	LateItemID      *string
	GraceMinutes    *int
	LateStepMinutes *int
	Categories      []string
	Active          *bool
	Sort            *int
}

// ExtraView is an extra with its item code and name.
type ExtraView struct {
	store.RentalExtra
	ItemCode string
	Name     store.JSON
}

// ExtraInput creates an extra (marina.configure).
type ExtraInput struct {
	ItemID          string
	ChargeMode      string
	DefaultQtyMilli qty.Milli
	MaxQtyMilli     qty.Milli
	Categories      []string
	Active          *bool
	Sort            int
}

// ExtraPatch changes an extra; nil = unchanged.
type ExtraPatch struct {
	ChargeMode      *string
	DefaultQtyMilli *qty.Milli
	MaxQtyMilli     *qty.Milli
	Categories      []string
	Active          *bool
	Sort            *int
}

// ExtraQty is an extra of a quote or booking.
type ExtraQty struct {
	ExtraID string
	Qty     qty.Milli
}

// Settings are the decoded rental settings (Version 0 = defaults never saved).
type Settings struct {
	Row              *store.RentalSettings
	Bands            []store.RentalBand
	Checklist        []store.RentalChecklistItem
	DepositModes     []string
	PreauthAvailable bool
}

// Slots returns the time settings for the pure slots package.
func (s Settings) Slots() slots.Settings {
	out := slots.Settings{OpenFrom: s.Row.OpenFromSlot, OpenTo: s.Row.OpenToSlot, Buffer: s.Row.BufferSlots}
	for _, b := range s.Bands {
		out.Bands = append(out.Bands, slots.Band{Code: b.Code, From: b.FromSlot, To: b.ToSlot})
	}
	return out
}

// SettingsInput saves the settings (marina.configure).
type SettingsInput struct {
	OpenFromSlot            int
	OpenToSlot              int
	Bands                   []store.RentalBand
	BufferSlots             int
	DeskHoldMinutes         int
	NoShowAfterMinutes      int
	LicenceThresholdKWMilli int64
	SkipperItemID           string
	DamageItemID            string
	Checklist               []store.RentalChecklistItem
	CheckoutMinPhotos       int
	CheckinMinPhotos        int
	DepositModes            []string
	IdentityRetentionDays   int
	DefaultRegisterID       string
}

// QuoteInput is a stateless quote (formula "" = every valid formula, sorted by total).
type QuoteInput struct {
	UnitID      string
	Range       slots.Range
	FormulaID   string
	SkipperID   string
	WithSkipper bool
	Extras      []ExtraQty
	CustomerID  string
}

// QuoteOption is the quote of one formula: OK with the snapshot, or the problem slug that refuses it.
type QuoteOption struct {
	FormulaID   string
	FormulaCode string
	Kind        string
	OK          bool
	Problem     string
	Snapshot    price.Snapshot
}

// SnapshotRequest is what reservation prices in its transaction (create, reschedule, extend).
type SnapshotRequest struct {
	UnitID     string
	FormulaID  string
	SkipperID  string // "" = no skipper
	Range      slots.Range
	Extras     []ExtraQty
	CustomerID string
	Channel    string
}

// Row aliases for the HTTP layer (handlers never import the store).
type (
	// Formula is a rental formula row.
	Formula = store.RentalFormula
	// Band is a half-day band of the settings.
	Band = store.RentalBand
	// ChecklistItem is an item of the settings checklist.
	ChecklistItem = store.RentalChecklistItem
)
