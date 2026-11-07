package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M3 contract tables (internal/migrations/*/00005_contracts.sql). Conventions as in
// harbor_models.go: nullable ids, texts and times use nullzero; nullable lengths that may legitimately be 0 are
// pointers. Money is integer cents, lengths centimetres, dates calendar.Date in the marina time zone. Owned by the
// CONTRACTS builder after the contract phase (fields may be added only if they exist in the migration).

// OccupancySettings is the row of occupancy_settings of a marina (created lazily with the defaults).
type OccupancySettings struct {
	bun.BaseModel `bun:"table:occupancy_settings"`

	MarinaID             string    `bun:"marina_id,pk"`
	SafetyClearanceCm    int64     `bun:"safety_clearance_cm"`
	WasteThresholdBP     int64     `bun:"waste_threshold_bp"`
	DraftHoldDays        int       `bun:"draft_hold_days"`
	DeskHoldHours        int       `bun:"desk_hold_hours"`
	QuoteValidityDays    int       `bun:"quote_validity_days"`
	MaxSegments          int       `bun:"max_segments"`
	AbsentDaysThreshold  int       `bun:"absent_days_threshold"`
	ArrivalGraceMinutes  int       `bun:"arrival_grace_minutes"`
	DefaultArrivalTime   string    `bun:"default_arrival_time"`   // HH:MM
	DefaultDepartureTime string    `bun:"default_departure_time"` // HH:MM
	RepriceOnMove        bool      `bun:"reprice_on_move"`
	ReleasePolicy        string    `bun:"release_policy"` // never, with_consent
	Version              int       `bun:"version"`
	CreatedAt            time.Time `bun:"created_at"`
	UpdatedAt            time.Time `bun:"updated_at"`
}

// DefaultOccupancySettings returns the defaults of the migration for a marina (m3-plan §5.1).
func DefaultOccupancySettings(marinaID string, now time.Time) *OccupancySettings {
	return &OccupancySettings{
		MarinaID: marinaID, SafetyClearanceCm: 30, WasteThresholdBP: 6000, DraftHoldDays: 7, DeskHoldHours: 24,
		QuoteValidityDays: 7, MaxSegments: 1, AbsentDaysThreshold: 7, ArrivalGraceMinutes: 120,
		DefaultArrivalTime: "14:00", DefaultDepartureTime: "11:00", ReleasePolicy: "with_consent",
		Version: 1, CreatedAt: UTC(now), UpdatedAt: UTC(now),
	}
}

// NumberSequence is a row of number_sequences: the next number of a series in a year (m3-plan §4.5).
type NumberSequence struct {
	bun.BaseModel `bun:"table:number_sequences"`

	MarinaID   string `bun:"marina_id,pk"`
	Series     string `bun:"series,pk"` // mooring, transit, quote, storage, charter
	Year       int    `bun:"year,pk"`
	NextNumber int64  `bun:"next_number"`
}

// Inquiry is a row of inquiries: a request for a berth, served or not (spec 13.2).
type Inquiry struct {
	bun.BaseModel `bun:"table:inquiries"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	Channel         string        `bun:"channel"` // desk, online, api, external
	Source          string        `bun:"source"`  // phone, email, desk, public, channel
	Purpose         string        `bun:"purpose"` // transit, contract
	Term            string        `bun:"term,nullzero"`
	VesselID        string        `bun:"vessel_id,nullzero"`
	CustomerID      string        `bun:"customer_id,nullzero"`
	LOACm           int64         `bun:"loa_cm"`
	BeamCm          int64         `bun:"beam_cm"`
	DraftCm         int64         `bun:"draft_cm,nullzero"`
	Arrival         calendar.Date `bun:"arrival"`
	Departure       calendar.Date `bun:"departure"`
	Outcome         string        `bun:"outcome"`
	QuoteID         string        `bun:"quote_id,nullzero"`
	ContractID      string        `bun:"contract_id,nullzero"`
	Notes           string        `bun:"notes"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// Quote is a row of quotes (spec 13.2): 1–3 options, each with its placements and price snapshot.
type Quote struct {
	bun.BaseModel `bun:"table:quotes"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	Number           int64         `bun:"number"`
	Year             int           `bun:"year"`
	Status           string        `bun:"status"` // draft, sent, accepted, declined, expired
	Kind             string        `bun:"kind"`   // mooring, transit
	Term             string        `bun:"term,nullzero"`
	CustomerID       string        `bun:"customer_id"`
	VesselID         string        `bun:"vessel_id"`
	StartsOn         calendar.Date `bun:"starts_on"`
	EndsOn           calendar.Date `bun:"ends_on"` // exclusive
	ValidUntil       calendar.Date `bun:"valid_until"`
	ExpiresAt        time.Time     `bun:"expires_at"`
	Channel          string        `bun:"channel"`
	Policy           JSON          `bun:"policy"`
	Notes            string        `bun:"notes"`
	InquiryID        string        `bun:"inquiry_id,nullzero"`
	AcceptedOptionID string        `bun:"accepted_option_id,nullzero"`
	ContractID       string        `bun:"contract_id,nullzero"`
	SentAt           time.Time     `bun:"sent_at,nullzero"`
	DecidedAt        time.Time     `bun:"decided_at,nullzero"`
	DeclineReason    string        `bun:"decline_reason"`
	CreatedByUserID  string        `bun:"created_by_user_id,nullzero"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// QuoteOption is a row of quote_options: one priced alternative of a quote.
type QuoteOption struct {
	bun.BaseModel `bun:"table:quote_options"`

	ID            string    `bun:"id,pk"`
	MarinaID      string    `bun:"marina_id"`
	QuoteID       string    `bun:"quote_id"`
	Seq           int       `bun:"seq"` // 1..3
	Label         string    `bun:"label"`
	Hold          bool      `bun:"hold"`
	PriceListID   string    `bun:"price_list_id,nullzero"`
	PriceSnapshot JSON      `bun:"price_snapshot"` // pricing.Snapshot
	EngineVersion int       `bun:"engine_version"`
	TotalCents    int64     `bun:"total_cents"`
	DepositCents  int64     `bun:"deposit_cents"`
	FitOverride   JSON      `bun:"fit_override"` // {user_id, reason, codes[]}, nil = none
	CreatedAt     time.Time `bun:"created_at"`
	UpdatedAt     time.Time `bun:"updated_at"`
}

// Contract kinds.
const (
	ContractKindMooring = "mooring"
	ContractKindTransit = "transit"
	ContractKindStorage = "storage"
	ContractKindCharter = "charter"
)

// Contract is a row of contracts: a mooring contract or a transit (spec 13; m3-plan §4).
type Contract struct {
	bun.BaseModel `bun:"table:contracts"`

	ID                    string        `bun:"id,pk"`
	MarinaID              string        `bun:"marina_id"`
	Kind                  string        `bun:"kind"`
	Term                  string        `bun:"term,nullzero"`
	Number                int64         `bun:"number"`
	Year                  int           `bun:"year"`
	Status                string        `bun:"status"`
	CustomerID            string        `bun:"customer_id,nullzero"`
	BillingCustomerID     string        `bun:"billing_customer_id,nullzero"`
	VesselID              string        `bun:"vessel_id"`
	StartsOn              calendar.Date `bun:"starts_on"`
	EndsOn                calendar.Date `bun:"ends_on"` // exclusive
	Channel               string        `bun:"channel"`
	QuoteID               string        `bun:"quote_id,nullzero"`
	PriceListID           string        `bun:"price_list_id,nullzero"`
	PriceSnapshot         JSON          `bun:"price_snapshot"` // pricing.Snapshot, never recomputed (INV-20)
	EngineVersion         int           `bun:"engine_version"`
	TotalCents            int64         `bun:"total_cents"`
	DepositCents          int64         `bun:"deposit_cents"`
	Policy                JSON          `bun:"policy"`
	ExpectedArrivalTime   string        `bun:"expected_arrival_time,nullzero"`
	ExpectedDepartureTime string        `bun:"expected_departure_time,nullzero"`
	Notes                 string        `bun:"notes"`
	HoldLost              bool          `bun:"hold_lost"`
	ActivatedAt           time.Time     `bun:"activated_at,nullzero"`
	SignedOnPaper         bool          `bun:"signed_on_paper"`
	ConfirmedAt           time.Time     `bun:"confirmed_at,nullzero"`
	ConfirmedByUserID     string        `bun:"confirmed_by_user_id,nullzero"`
	ConfirmationSource    string        `bun:"confirmation_source,nullzero"` // manual, provider
	ConfirmationRef       string        `bun:"confirmation_ref,nullzero"`    // never logged
	PayAtDeparture        bool          `bun:"pay_at_departure"`
	CancelledAt           time.Time     `bun:"cancelled_at,nullzero"`
	CancelReason          string        `bun:"cancel_reason"`
	TerminatedOn          calendar.Date `bun:"terminated_on,nullzero"`
	TerminateReason       string        `bun:"terminate_reason"`
	CreatedByUserID       string        `bun:"created_by_user_id,nullzero"`
	Version               int           `bun:"version"`
	CreatedAt             time.Time     `bun:"created_at"`
	UpdatedAt             time.Time     `bun:"updated_at"`
}

// ContractLine is a row of contract_lines: a resource line (with allocations) or a service line.
type ContractLine struct {
	bun.BaseModel `bun:"table:contract_lines"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	ContractID   string    `bun:"contract_id"`
	Seq          int       `bun:"seq"`
	LineKind     string    `bun:"line_kind"` // resource, service
	ItemID       string    `bun:"item_id,nullzero"`
	ResourceType string    `bun:"resource_type,nullzero"` // berth, dynamic_area, land_resource
	ResourceID   string    `bun:"resource_id,nullzero"`
	QtyMilli     int64     `bun:"qty_milli"`
	AmountCents  int64     `bun:"amount_cents"`
	CreatedAt    time.Time `bun:"created_at"`
	UpdatedAt    time.Time `bun:"updated_at"`
}
