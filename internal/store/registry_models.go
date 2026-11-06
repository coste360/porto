package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M1 registry tables (internal/migrations/*/00003_registry.sql). Conventions as in models.go
// and harbor_models.go. Encrypted columns (*_enc) hold KeyRing ciphertext, never clear text.

// Customer is a row of customers.
type Customer struct {
	bun.BaseModel `bun:"table:customers"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	Kind           string    `bun:"kind"` // person, company
	FirstName      string    `bun:"first_name"`
	LastName       string    `bun:"last_name"`
	CompanyName    string    `bun:"company_name"`
	FiscalCode     string    `bun:"fiscal_code,nullzero"`
	VATNumber      string    `bun:"vat_number,nullzero"`
	SDICode        string    `bun:"sdi_code,nullzero"`
	PEC            string    `bun:"pec,nullzero"`
	BillingAddress JSON      `bun:"billing_address"`
	Nationality    string    `bun:"nationality,nullzero"`
	Locale         string    `bun:"locale"`
	Emails         JSON      `bun:"emails"` // [{label, value}]
	Phones         JSON      `bun:"phones"` // [{label, value}]
	Category       string    `bun:"category,nullzero"`
	Tags           Strings   `bun:"tags"`
	PaymentTerms   JSON      `bun:"payment_terms"`
	IBANEnc        string    `bun:"iban_enc,nullzero"` // KeyRing, AAD "iban:<id>"
	IBANLast4      string    `bun:"iban_last4,nullzero"`
	Notes          string    `bun:"notes"`
	SearchText     string    `bun:"search_text"`
	MergedIntoID   string    `bun:"merged_into_id,nullzero"`
	AnonymizedAt   time.Time `bun:"anonymized_at,nullzero"`
	ArchivedAt     time.Time `bun:"archived_at,nullzero"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// Consent is a row of consents (insert-only through the API).
type Consent struct {
	bun.BaseModel `bun:"table:consents"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	CustomerID       string    `bun:"customer_id"`
	Kind             string    `bun:"kind"` // privacy, marketing, third_party
	PolicyVersion    string    `bun:"policy_version"`
	Granted          bool      `bun:"granted"`
	Source           string    `bun:"source"` // desk, online, import, api
	RecordedByUserID string    `bun:"recorded_by_user_id,nullzero"`
	RecordedAt       time.Time `bun:"recorded_at"`
	CreatedAt        time.Time `bun:"created_at"`
}

// Vessel is a row of vessels.
type Vessel struct {
	bun.BaseModel `bun:"table:vessels"`

	ID                 string    `bun:"id,pk"`
	MarinaID           string    `bun:"marina_id"`
	Name               string    `bun:"name"`
	Registration       string    `bun:"registration,nullzero"`
	RegistryOffice     string    `bun:"registry_office,nullzero"`
	Flag               string    `bun:"flag,nullzero"`
	HomePort           string    `bun:"home_port,nullzero"`
	HullType           string    `bun:"hull_type"`
	Model              string    `bun:"model,nullzero"`
	Builder            string    `bun:"builder,nullzero"`
	YearBuilt          int64     `bun:"year_built,nullzero"`
	HullColor          string    `bun:"hull_color,nullzero"`
	HullMaterial       string    `bun:"hull_material,nullzero"`
	LOACm              int64     `bun:"loa_cm"`
	LWLCm              int64     `bun:"lwl_cm,nullzero"`
	BeamCm             int64     `bun:"beam_cm"`
	DraftCm            int64     `bun:"draft_cm,nullzero"`
	AirDraftCm         int64     `bun:"air_draft_cm,nullzero"`
	DisplacementKg     int64     `bun:"displacement_kg,nullzero"`
	MeasuredLOACm      int64     `bun:"measured_loa_cm,nullzero"`
	MeasuredBeamCm     int64     `bun:"measured_beam_cm,nullzero"`
	MeasuredDraftCm    int64     `bun:"measured_draft_cm,nullzero"`
	MeasuredAirDraftCm int64     `bun:"measured_air_draft_cm,nullzero"`
	MeasuredAt         time.Time `bun:"measured_at,nullzero"`
	MeasuredByUserID   string    `bun:"measured_by_user_id,nullzero"`
	DimensionsSource   string    `bun:"dimensions_source"` // declared, measured
	Engines            JSON      `bun:"engines"`
	Fuel               string    `bun:"fuel,nullzero"`
	PowerRequiredA     int64     `bun:"power_required_a,nullzero"`
	Notes              string    `bun:"notes"`
	SearchText         string    `bun:"search_text"`
	ArchivedAt         time.Time `bun:"archived_at,nullzero"`
	Version            int       `bun:"version"`
	CreatedAt          time.Time `bun:"created_at"`
	UpdatedAt          time.Time `bun:"updated_at"`
}

// VesselParty is a row of vessel_parties (owner with share in millis, operator, skipper, billing, contact).
type VesselParty struct {
	bun.BaseModel `bun:"table:vessel_parties"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	VesselID    string    `bun:"vessel_id"`
	CustomerID  string    `bun:"customer_id"`
	Role        string    `bun:"role"`
	ShareMillis int64     `bun:"share_millis,nullzero"` // owners only, 1..1000
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// Document is a row of documents (customer or vessel document, optional encrypted file).
type Document struct {
	bun.BaseModel `bun:"table:documents"`

	ID             string        `bun:"id,pk"`
	MarinaID       string        `bun:"marina_id"`
	OwnerType      string        `bun:"owner_type"` // customer, vessel
	CustomerID     string        `bun:"customer_id,nullzero"`
	VesselID       string        `bun:"vessel_id,nullzero"`
	Type           string        `bun:"type"`
	NumberEnc      string        `bun:"number_enc,nullzero"` // KeyRing, AAD "document-number:<id>"
	Issuer         string        `bun:"issuer,nullzero"`
	IssuedOn       calendar.Date `bun:"issued_on,nullzero"`
	ExpiresOn      calendar.Date `bun:"expires_on,nullzero"`
	FileID         string        `bun:"file_id,nullzero"`
	FileName       string        `bun:"file_name,nullzero"`
	Notes          string        `bun:"notes"`
	RetentionUntil calendar.Date `bun:"retention_until,nullzero"`
	Version        int           `bun:"version"`
	CreatedAt      time.Time     `bun:"created_at"`
	UpdatedAt      time.Time     `bun:"updated_at"`
}

// Deadline is a row of deadlines (fed by document expiries in M1).
type Deadline struct {
	bun.BaseModel `bun:"table:deadlines"`

	ID         string        `bun:"id,pk"`
	MarinaID   string        `bun:"marina_id"`
	SourceType string        `bun:"source_type"` // document (M1)
	SourceID   string        `bun:"source_id"`
	Kind       string        `bun:"kind"`
	CustomerID string        `bun:"customer_id,nullzero"`
	VesselID   string        `bun:"vessel_id,nullzero"`
	DueOn      calendar.Date `bun:"due_on"`
	Status     string        `bun:"status"` // open, done, dismissed
	Note       string        `bun:"note"`
	NotifiedAt JSON          `bun:"notified_at"` // M4
	Version    int           `bun:"version"`
	CreatedAt  time.Time     `bun:"created_at"`
	UpdatedAt  time.Time     `bun:"updated_at"`
}

// DeadlinePolicy is a row of deadline_policies (lead days per kind, created lazily with defaults).
type DeadlinePolicy struct {
	bun.BaseModel `bun:"table:deadline_policies"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Kind      string    `bun:"kind"`
	LeadDays  JSON      `bun:"lead_days"` // []int, sorted desc
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}
