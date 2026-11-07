package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"modernc.org/sqlite"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M4 contract tables (internal/migrations/*/00007_contracts_m4.sql). Conventions as in
// contract_models.go. Owned by the CONTRACTS builder after the contract phase (fields may be added only if they
// exist in the migration).

// Messages raised by the M4 immutability triggers (00008_rights, 00009_documents).
const (
	MillesimalImmutableMessage      = "millesimal_table_immutable"
	AllocationsImmutableMessage     = "allocations_immutable"
	TemplateVersionImmutableMessage = "template_version_immutable"
	DocumentImmutableMessage        = "document_immutable"
)

// IsTriggerViolation reports whether err was raised by an immutability trigger with the given message (both
// dialects).
func IsTriggerViolation(err error, message string) bool {
	if pe, ok := errors.AsType[pgdriver.Error](err); ok {
		return pe.Field('C') == "P0001" && strings.Contains(pe.Field('M'), message)
	}
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		return se.Code() == 1811 && strings.Contains(se.Error(), message) // SQLITE_CONSTRAINT_TRIGGER
	}
	return false
}

// Installment statuses (m4-plan §3.3). Overdue is computed (issued and due_on < today).
const (
	InstallmentScheduled     = "scheduled"
	InstallmentIssued        = "issued"
	InstallmentPartiallyPaid = "partially_paid" // M5
	InstallmentPaid          = "paid"           // set only by the settlement entry points (INV-32)
	InstallmentCancelled     = "cancelled"
)

// Settlement sources.
const (
	SettlementManual  = "manual"
	SettlementPayment = "payment" // M5 payment matching
)

// ContractSettings is the row of contract_settings of a marina (created lazily with the defaults).
type ContractSettings struct {
	bun.BaseModel `bun:"table:contract_settings"`

	MarinaID              string    `bun:"marina_id,pk"`
	DefaultPolicy         JSON      `bun:"default_policy"`
	SignatureDriver       string    `bun:"signature_driver"` // paper, otp, provider
	ProviderCode          string    `bun:"provider_code,nullzero"`
	MarinaCountersign     bool      `bun:"marina_countersign"`
	RequirePaperScan      bool      `bun:"require_paper_scan"`
	SignatureDays         int       `bun:"signature_days"`
	SignatureReminderDays int       `bun:"signature_reminder_days"`
	RenewalDeadlineDays   int       `bun:"renewal_deadline_days"`
	ArrearsDays           int       `bun:"arrears_days"`
	InsurancePolicy       string    `bun:"insurance_policy"` // warn, block
	RequiredFields        JSON      `bun:"required_fields"`  // []string
	BankDetailsEnc        string    `bun:"bank_details_enc,nullzero"`
	OfficeEmailEnc        string    `bun:"office_email_enc,nullzero"`
	Version               int       `bun:"version"`
	CreatedAt             time.Time `bun:"created_at"`
	UpdatedAt             time.Time `bun:"updated_at"`

	// Stored is true when the row exists (false = the migration defaults of a marina never configured).
	Stored bool `bun:"-"`
}

// DefaultContractSettings returns the defaults of the migration for a marina (m4-plan §5.1).
func DefaultContractSettings(marinaID string, now time.Time) *ContractSettings {
	return &ContractSettings{
		MarinaID: marinaID, DefaultPolicy: JSON(`{}`), SignatureDriver: "paper", RequirePaperScan: true,
		SignatureDays: 14, SignatureReminderDays: 3, RenewalDeadlineDays: 30, ArrearsDays: 30, InsurancePolicy: "warn",
		RequiredFields: JSON(`[]`), Version: 1, CreatedAt: UTC(now), UpdatedAt: UTC(now),
	}
}

// Installment is a row of installments: one due amount of a contract's plan (spec 13.7, INV-31).
type Installment struct {
	bun.BaseModel `bun:"table:installments"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	ContractID        string        `bun:"contract_id"`
	PlanVersion       int           `bun:"plan_version"`
	Seq               int           `bun:"seq"`
	Kind              string        `bun:"kind"` // deposit, installment, single
	IssueOn           calendar.Date `bun:"issue_on"`
	DueOn             calendar.Date `bun:"due_on"`
	AmountCents       int64         `bun:"amount_cents"`
	Status            string        `bun:"status"`
	IssuedAt          time.Time     `bun:"issued_at,nullzero"`
	OverdueNotifiedAt time.Time     `bun:"overdue_notified_at,nullzero"`
	ChargeID          string        `bun:"charge_id,nullzero"`         // M5
	SettlementSource  string        `bun:"settlement_source,nullzero"` // manual, payment
	SettlementMethod  string        `bun:"settlement_method,nullzero"` // bank_transfer, cash, card_pos, cheque, other
	SettlementRefEnc  string        `bun:"settlement_ref_enc,nullzero"`
	SettledOn         calendar.Date `bun:"settled_on,nullzero"`
	SettledAt         time.Time     `bun:"settled_at,nullzero"`
	SettledByUserID   string        `bun:"settled_by_user_id,nullzero"`
	ReplacedByPlan    *int          `bun:"replaced_by_plan"`
	CancelledAt       time.Time     `bun:"cancelled_at,nullzero"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}

// RenewalCampaign is a row of renewal_campaigns (spec 13.8).
type RenewalCampaign struct {
	bun.BaseModel `bun:"table:renewal_campaigns"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	Name             string        `bun:"name"`
	Filter           JSON          `bun:"filter"`
	PriceListID      string        `bun:"price_list_id,nullzero"`
	TargetStartsRule string        `bun:"target_starts_rule"` // next_term, fixed
	TargetStartsOn   calendar.Date `bun:"target_starts_on,nullzero"`
	DeadlineOn       calendar.Date `bun:"deadline_on"`
	Status           string        `bun:"status"` // draft, sending, open, closed, discarded
	Message          string        `bun:"message"`
	Counts           JSON          `bun:"counts"`
	LaunchedAt       time.Time     `bun:"launched_at,nullzero"`
	ClosedAt         time.Time     `bun:"closed_at,nullzero"`
	CreatedByUserID  string        `bun:"created_by_user_id,nullzero"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// RenewalProposal is a row of renewal_proposals: the renewal of one source contract (INV-33).
type RenewalProposal struct {
	bun.BaseModel `bun:"table:renewal_proposals"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	CampaignID       string        `bun:"campaign_id,nullzero"`
	SourceContractID string        `bun:"source_contract_id"`
	NewContractID    string        `bun:"new_contract_id,nullzero"`
	Status           string        `bun:"status"`
	ExclusionReason  string        `bun:"exclusion_reason,nullzero"`
	IncludedReason   string        `bun:"included_reason,nullzero"`
	OldTotalCents    int64         `bun:"old_total_cents"`
	NewTotalCents    *int64        `bun:"new_total_cents"`
	DeadlineOn       calendar.Date `bun:"deadline_on"`
	SentAt           time.Time     `bun:"sent_at,nullzero"`
	SentCount        int           `bun:"sent_count"`
	AcceptedAt       time.Time     `bun:"accepted_at,nullzero"`
	AcceptedVia      string        `bun:"accepted_via,nullzero"`  // panel, link
	AcceptedPlan     string        `bun:"accepted_plan,nullzero"` // standard, early_payment
	DecidedAt        time.Time     `bun:"decided_at,nullzero"`
	DeclineReason    string        `bun:"decline_reason"`
	LinkID           string        `bun:"link_id,nullzero"`
	CreatedByUserID  string        `bun:"created_by_user_id,nullzero"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// ContractAddendum is a row of contract_addenda (spec 13.4).
type ContractAddendum struct {
	bun.BaseModel `bun:"table:contract_addenda"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	ContractID         string        `bun:"contract_id"`
	Seq                int           `bun:"seq"`
	Kind               string        `bun:"kind"`
	EffectiveOn        calendar.Date `bun:"effective_on"`
	Payload            JSON          `bun:"payload"`
	AmountDeltaCents   int64         `bun:"amount_delta_cents"`
	Status             string        `bun:"status"` // draft, awaiting_signature, active, cancelled
	DocumentID         string        `bun:"document_id,nullzero"`
	SignatureRequestID string        `bun:"signature_request_id,nullzero"`
	ActivatedAt        time.Time     `bun:"activated_at,nullzero"`
	CancelledAt        time.Time     `bun:"cancelled_at,nullzero"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// CharterFleet is a row of charter_fleets: the registered vessels of a charter company.
type CharterFleet struct {
	bun.BaseModel `bun:"table:charter_fleets"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	CustomerID string    `bun:"customer_id"`
	Name       string    `bun:"name"`
	Version    int       `bun:"version"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// CharterFleetVessel is a row of charter_fleet_vessels (a vessel is in at most one active fleet).
type CharterFleetVessel struct {
	bun.BaseModel `bun:"table:charter_fleet_vessels"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	FleetID   string    `bun:"fleet_id"`
	VesselID  string    `bun:"vessel_id"`
	Active    bool      `bun:"active"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// CharterContract is a row of charter_contracts: the charter fields of a contract of kind charter.
type CharterContract struct {
	bun.BaseModel `bun:"table:charter_contracts"`

	ContractID        string    `bun:"contract_id,pk"`
	MarinaID          string    `bun:"marina_id"`
	FleetID           string    `bun:"fleet_id"`
	ChangeoverWeekday int       `bun:"changeover_weekday"` // 1 = Monday .. 7 = Sunday
	PricingMode       string    `bun:"pricing_mode"`       // per_berth, per_vessel, flat
	OutNights         JSON      `bun:"out_nights"`         // [{from, to, berth_id?}]
	Version           int       `bun:"version"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// WaitlistEntry is a row of waitlist_entries (spec 13.9).
type WaitlistEntry struct {
	bun.BaseModel `bun:"table:waitlist_entries"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	CustomerID      string        `bun:"customer_id"`
	VesselID        string        `bun:"vessel_id,nullzero"`
	LOACm           int64         `bun:"loa_cm"`
	BeamCm          int64         `bun:"beam_cm"`
	DraftCm         int64         `bun:"draft_cm,nullzero"`
	Term            string        `bun:"term"`
	GroupIDs        Strings       `bun:"group_ids"`
	RequestedFrom   calendar.Date `bun:"requested_from"`
	Priority        int           `bun:"priority"`
	Status          string        `bun:"status"` // waiting, offered, converted, withdrawn
	OfferedQuoteID  string        `bun:"offered_quote_id,nullzero"`
	OfferedAt       time.Time     `bun:"offered_at,nullzero"`
	Notes           string        `bun:"notes"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// contractM4Columns are the columns of contracts added by 00007 (and vessel_id, changed by a change of vessel)
// that UpdateContract (M3) does not write.
var contractM4Columns = []string{
	"vessel_id", "signed_at", "signed_document_id", "signature_request_id", "suspended_at", "suspend_reason",
	"renewed_at", "termination_amount_cents", "installment_plan_version", "charter",
}

// SetContractM4 writes the M4 columns of a contract in the transaction of the state change that updated it
// (UpdateContract has bumped the version already).
func SetContractM4(ctx context.Context, db bun.IDB, s MarinaScope, c *Contract) error {
	_, err := db.NewUpdate().Model(c).Column(contractM4Columns...).
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Exec(ctx)
	return err
}

// SetQuoteM4 writes the M4 columns of a quote (sending and acceptance channel) after UpdateQuote.
func SetQuoteM4(ctx context.Context, db bun.IDB, s MarinaScope, q *Quote) error {
	_, err := db.NewUpdate().Model(q).Column("link_rotated_at", "sent_count", "accepted_via").
		Where("marina_id = ?", s.ID()).Where("id = ?", q.ID).Exec(ctx)
	return err
}
