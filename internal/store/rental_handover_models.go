package store

import (
	"time"

	"github.com/uptrace/bun"
)

// Row types of the M11 rental handover tables (internal/migrations/*/00025_rental_handovers.sql, m11-plan §7, §8,
// §15). HANDOVER owns the queries (rental_handovers.go, rental_damages.go).

// Rental handover enums (m11-plan §7, §8).
const (
	RentalPhaseCheckout = "checkout"
	RentalPhaseCheckin  = "checkin"

	RentalFileIDDocument = "id_document"
	RentalFileLicence    = "licence"
	RentalFileBoat       = "boat"
	RentalFileDamage     = "damage"

	RentalDamageOpen    = "open"
	RentalDamageCharged = "charged"
	RentalDamageWaived  = "waived"

	// IntentPurposeRentalDeposit is the payment_intents purpose of a rental deposit pre-authorisation (D15).
	IntentPurposeRentalDeposit = "rental_deposit"
	// SignatureSubjectRental is the signature_requests subject of a rental contract (D14).
	SignatureSubjectRental = "rental"
	// SignatureDriverScreen is the on-screen signature driver (D14).
	SignatureDriverScreen = "screen"
	// SubjectRentalBooking is the generated_documents subject of the rental contract and handover documents.
	SubjectRentalBooking = "rental_booking"
)

// RentalHandover is a row of rental_handovers (one per booking and phase). IdentityEnc holds the renter's document
// and licence data (checkout only; KeyRing, AAD rental-identity:<id>).
type RentalHandover struct {
	bun.BaseModel `bun:"table:rental_handovers"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	BookingID         string    `bun:"booking_id"`
	Phase             string    `bun:"phase"`
	Checklist         JSON      `bun:"checklist"` // [{key, ok, note}]
	FuelEighths       *int      `bun:"fuel_eighths"`
	EngineHoursMilli  *int64    `bun:"engine_hours_milli"`
	RefuelMilli       *int64    `bun:"refuel_milli"`
	IdentityEnc       string    `bun:"identity_enc,nullzero"`
	IdentityPurgedAt  time.Time `bun:"identity_purged_at,nullzero"`
	Notes             string    `bun:"notes"`
	ActualAt          time.Time `bun:"actual_at,nullzero"`
	ClientRef         string    `bun:"client_ref,nullzero"`
	DeviceAt          time.Time `bun:"device_at,nullzero"`
	CompletedAt       time.Time `bun:"completed_at,nullzero"`
	CompletedByUserID string    `bun:"completed_by_user_id,nullzero"`
	Version           int       `bun:"version"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// RentalHandoverFile is a row of rental_handover_files (DamageID set iff kind damage).
type RentalHandoverFile struct {
	bun.BaseModel `bun:"table:rental_handover_files"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	HandoverID      string    `bun:"handover_id"`
	DamageID        string    `bun:"damage_id,nullzero"`
	Kind            string    `bun:"kind"`
	FileID          string    `bun:"file_id"`
	ClientRef       string    `bun:"client_ref,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
}

// RentalDamage is a row of rental_damages.
type RentalDamage struct {
	bun.BaseModel `bun:"table:rental_damages"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	BookingID       string    `bun:"booking_id"`
	Description     string    `bun:"description"`
	EstimateCents   int64     `bun:"estimate_cents"`
	Status          string    `bun:"status"`
	RetainedCents   int64     `bun:"retained_cents"`
	ChargedCents    int64     `bun:"charged_cents"`
	WaiveReason     string    `bun:"waive_reason"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}
