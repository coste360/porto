package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M11 rental fleet tables (internal/migrations/*/00023_rental_fleet.sql, m11-plan §3, §15) and the
// frozen read helpers shared by the FLEET, BOOKING and HANDOVER builders (GetRentalUnit, LockRentalUnit,
// GetRentalSkipper, LockRentalSkipper, GetRentalFormula, LoadRentalSettings, RentalExists). FLEET owns the
// writes (rental_fleet.go).

// Rental fleet enums (m11-plan §3).
const (
	RentalCategoryRIB    = "rib"
	RentalCategoryOpen   = "open"
	RentalCategoryCabin  = "cabin"
	RentalCategorySail   = "sail"
	RentalCategoryPedalo = "pedalo"
	RentalCategorySUP    = "sup"

	RentalLicenceNever     = "never"
	RentalLicenceAlways    = "always"
	RentalLicenceThreshold = "threshold"

	RentalFuelPetrol   = "petrol"
	RentalFuelDiesel   = "diesel"
	RentalFuelElectric = "electric"
	RentalFuelNone     = "none"

	RentalUnitAvailable   = "available"
	RentalUnitMaintenance = "maintenance"
	RentalUnitRetired     = "retired"

	RentalOwnershipMarina     = "marina"
	RentalOwnershipThirdParty = "third_party"

	RentalKindHourly   = "hourly"
	RentalKindHalfDay  = "half_day"
	RentalKindDay      = "day"
	RentalKindMultiDay = "multi_day"
	RentalKindWeek     = "week"

	RentalSkipperNone     = "none"
	RentalSkipperOptional = "optional"
	RentalSkipperRequired = "required"

	RentalFuelIncluded = "included"
	RentalFuelMetered  = "metered"

	RentalExtraPerRental = "per_rental"
	RentalExtraPerDay    = "per_day"
)

// RentalSettings is the row of rental_settings (one per marina). LoadRentalSettings returns the defaults (Version
// 0) when the marina never saved them.
type RentalSettings struct {
	bun.BaseModel `bun:"table:rental_settings"`

	MarinaID                string    `bun:"marina_id,pk"`
	OpenFromSlot            int       `bun:"open_from_slot"`
	OpenToSlot              int       `bun:"open_to_slot"`
	Bands                   JSON      `bun:"bands"` // []RentalBand
	BufferSlots             int       `bun:"buffer_slots"`
	DeskHoldMinutes         int       `bun:"desk_hold_minutes"`
	NoShowAfterMinutes      int       `bun:"no_show_after_minutes"`
	LicenceThresholdKWMilli int64     `bun:"licence_threshold_kw_milli"`
	SkipperItemID           string    `bun:"skipper_item_id,nullzero"`
	DamageItemID            string    `bun:"damage_item_id,nullzero"`
	Checklist               JSON      `bun:"checklist"` // []RentalChecklistItem
	CheckoutMinPhotos       int       `bun:"checkout_min_photos"`
	CheckinMinPhotos        int       `bun:"checkin_min_photos"`
	DepositModes            JSON      `bun:"deposit_modes"` // []string
	IdentityRetentionDays   int       `bun:"identity_retention_days"`
	DefaultRegisterID       string    `bun:"default_register_id,nullzero"`
	Version                 int       `bun:"version"`
	CreatedAt               time.Time `bun:"created_at"`
	UpdatedAt               time.Time `bun:"updated_at"`
}

// RentalBand is a half-day band of rental_settings.bands ([from_slot, to_slot) of the local day).
type RentalBand struct {
	Code     string            `json:"code"`
	FromSlot int               `json:"from_slot"`
	ToSlot   int               `json:"to_slot"`
	Label    map[string]string `json:"label"`
}

// RentalChecklistItem is an item of rental_settings.checklist (categories empty = every category).
type RentalChecklistItem struct {
	Key        string            `json:"key"`
	Label      map[string]string `json:"label"`
	Categories []string          `json:"categories"`
}

// DefaultRentalSettings returns the settings of a marina that never saved them (m11-plan §3.5).
func DefaultRentalSettings(marinaID string) *RentalSettings {
	bands, _ := json.Marshal([]RentalBand{
		{Code: "morning", FromSlot: 16, ToSlot: 26, Label: map[string]string{"it": "Mattina", "en": "Morning"}},
		{Code: "afternoon", FromSlot: 28, ToSlot: 38, Label: map[string]string{"it": "Pomeriggio", "en": "Afternoon"}},
	})
	type item struct{ key, it, en string }
	var list []RentalChecklistItem
	for _, i := range []item{
		{"hull", "Scafo", "Hull"},
		{"engine", "Motore", "Engine"},
		{"lifejackets", "Giubbotti salvagente", "Life jackets"},
		{"anchor", "Ancora", "Anchor"},
		{"extinguisher", "Estintore", "Fire extinguisher"},
		{"vhf", "VHF", "VHF"},
		{"documents", "Documenti di bordo", "Documents on board"},
		{"cleanliness", "Pulizia", "Cleanliness"},
	} {
		list = append(list, RentalChecklistItem{Key: i.key, Label: map[string]string{"it": i.it, "en": i.en}, Categories: []string{}})
	}
	checklist, _ := json.Marshal(list)
	return &RentalSettings{
		MarinaID: marinaID, OpenFromSlot: 16, OpenToSlot: 38, Bands: JSON(bands), BufferSlots: 1, DeskHoldMinutes: 60,
		NoShowAfterMinutes: 60, LicenceThresholdKWMilli: 30000, Checklist: JSON(checklist), CheckoutMinPhotos: 2,
		CheckinMinPhotos: 1, DepositModes: JSON(`["cash","card_pos","preauth"]`), IdentityRetentionDays: 365,
	}
}

// RentalUnit is a row of rental_units (a rental boat, linked 1:1 to a registry vessel).
type RentalUnit struct {
	bun.BaseModel `bun:"table:rental_units"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	VesselID         string    `bun:"vessel_id"`
	Code             string    `bun:"code"`
	Category         string    `bun:"category"`
	Seats            int       `bun:"seats"`
	Licence          string    `bun:"licence"`
	FuelType         string    `bun:"fuel_type"`
	TankMilli        int64     `bun:"tank_milli"`
	DepositCents     int64     `bun:"deposit_cents"`
	Status           string    `bun:"status"`
	HomeBerthID      string    `bun:"home_berth_id,nullzero"`
	ItemID           string    `bun:"item_id"`
	Ownership        string    `bun:"ownership"`
	OwnerCustomerID  string    `bun:"owner_customer_id,nullzero"`
	EngineHoursMilli int64     `bun:"engine_hours_milli"`
	Note             string    `bun:"note"`
	Sort             int       `bun:"sort"`
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// RentalSkipper is a row of rental_skippers. Name is personal data (shown only to rental.manage, never logged
// or put in events); the contact is encrypted (KeyRing, AAD rental-skipper:<id>).
type RentalSkipper struct {
	bun.BaseModel `bun:"table:rental_skippers"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	Name             string        `bun:"name"`
	ContactEnc       string        `bun:"contact_enc,nullzero"`
	LicenceExpiresOn calendar.Date `bun:"licence_expires_on,nullzero"`
	UserID           string        `bun:"user_id,nullzero"`
	Active           bool          `bun:"active"`
	Sort             int           `bun:"sort"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// RentalSkipperHours is a row of rental_skipper_hours (weekday 1 = Monday .. 7 = Sunday; to_slot exclusive).
type RentalSkipperHours struct {
	bun.BaseModel `bun:"table:rental_skipper_hours"`

	SkipperID string `bun:"skipper_id,pk"`
	Weekday   int    `bun:"weekday,pk"`
	FromSlot  int    `bun:"from_slot,pk"`
	ToSlot    int    `bun:"to_slot"`
	MarinaID  string `bun:"marina_id"`
}

// RentalFormula is a row of rental_formulas.
type RentalFormula struct {
	bun.BaseModel `bun:"table:rental_formulas"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Code            string    `bun:"code"`
	Name            JSON      `bun:"name"` // I18nText
	Kind            string    `bun:"kind"`
	BandCode        string    `bun:"band_code,nullzero"`
	MinSlots        int       `bun:"min_slots"`
	MaxSlots        *int      `bun:"max_slots"` // nil = the open window
	MinDays         int       `bun:"min_days"`
	MaxDays         int       `bun:"max_days"`
	Skipper         string    `bun:"skipper"`
	Fuel            string    `bun:"fuel"`
	FuelItemID      string    `bun:"fuel_item_id,nullzero"`
	LateItemID      string    `bun:"late_item_id,nullzero"`
	GraceMinutes    int       `bun:"grace_minutes"`
	LateStepMinutes int       `bun:"late_step_minutes"`
	Categories      JSON      `bun:"categories"` // []string, empty = every category
	Active          bool      `bun:"active"`
	Sort            int       `bun:"sort"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// RentalExtra is a row of rental_extras.
type RentalExtra struct {
	bun.BaseModel `bun:"table:rental_extras"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	ItemID          string    `bun:"item_id"`
	ChargeMode      string    `bun:"charge_mode"`
	DefaultQtyMilli int64     `bun:"default_qty_milli"`
	MaxQtyMilli     int64     `bun:"max_qty_milli"`
	Categories      JSON      `bun:"categories"` // []string, empty = every category
	Active          bool      `bun:"active"`
	Sort            int       `bun:"sort"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// GetRentalUnit returns a unit of the marina.
func GetRentalUnit(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalUnit, error) {
	return getRentalRow[RentalUnit](ctx, db, s, id, false)
}

// LockRentalUnit locks a unit row (PostgreSQL FOR UPDATE; SQLite has one writer) and returns it. Lock order:
// booking, then unit, then skipper (m11-plan §0.3).
func LockRentalUnit(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalUnit, error) {
	return getRentalRow[RentalUnit](ctx, db, s, id, true)
}

// GetRentalSkipper returns a skipper of the marina.
func GetRentalSkipper(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalSkipper, error) {
	return getRentalRow[RentalSkipper](ctx, db, s, id, false)
}

// LockRentalSkipper locks a skipper row and returns it.
func LockRentalSkipper(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalSkipper, error) {
	return getRentalRow[RentalSkipper](ctx, db, s, id, true)
}

// GetRentalFormula returns a formula of the marina.
func GetRentalFormula(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalFormula, error) {
	return getRentalRow[RentalFormula](ctx, db, s, id, false)
}

// GetRentalExtra returns an extra of the marina.
func GetRentalExtra(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalExtra, error) {
	return getRentalRow[RentalExtra](ctx, db, s, id, false)
}

// ListRentalSkipperHours returns the weekly hours of a skipper, by weekday and from_slot.
func ListRentalSkipperHours(ctx context.Context, db bun.IDB, s MarinaScope, skipperID string) ([]RentalSkipperHours, error) {
	var rows []RentalSkipperHours
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("skipper_id = ?", skipperID).
		Order("weekday", "from_slot").Scan(ctx)
	return rows, err
}

// LoadRentalSettings returns the rental settings of the marina, or the defaults (Version 0) when never saved.
func LoadRentalSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*RentalSettings, error) {
	row := new(RentalSettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultRentalSettings(s.ID()), nil
		}
		return nil, err
	}
	return row, nil
}

func getRentalRow[T any](ctx context.Context, db bun.IDB, s MarinaScope, id string, lock bool) (*T, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(T)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// Rental tables addressable by id (RentalExists).
const (
	RentalTableUnits     = "rental_units"
	RentalTableSkippers  = "rental_skippers"
	RentalTableFormulas  = "rental_formulas"
	RentalTableExtras    = "rental_extras"
	RentalTableBookings  = "rental_bookings"
	RentalTableBlocks    = "rental_blocks"
	RentalTableDamages   = "rental_damages"
	RentalTableHandovers = "rental_handovers"
)

var rentalTables = map[string]bool{
	RentalTableUnits: true, RentalTableSkippers: true, RentalTableFormulas: true, RentalTableExtras: true,
	RentalTableBookings: true, RentalTableBlocks: true, RentalTableDamages: true, RentalTableHandovers: true,
}

// RentalExists returns ErrNotFound unless table (one of the RentalTable* constants) has a row id of the marina.
// The services use it to answer 404 for another marina's ids before anything else.
func RentalExists(ctx context.Context, db bun.IDB, s MarinaScope, table, id string) error {
	if !rentalTables[table] || !ValidID(id) {
		return ErrNotFound
	}
	n, err := db.NewSelect().Table(table).Where("marina_id = ?", s.ID()).Where("id = ?", id).Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
