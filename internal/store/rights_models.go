package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M4 rights tables (internal/migrations/*/00008_rights.sql). Conventions as in contract_models.go.
// Owned by the RIGHTS-CHARGES builder after the contract phase (fields may be added only if they exist in the
// migration).

// Concession is the row of concessions of a marina (spec 14.6).
type Concession struct {
	bun.BaseModel `bun:"table:concessions"`

	ID             string        `bun:"id,pk"`
	MarinaID       string        `bun:"marina_id"`
	Authority      string        `bun:"authority"`
	DeedKind       string        `bun:"deed_kind"`
	DeedNumber     string        `bun:"deed_number"`
	GrantedOn      calendar.Date `bun:"granted_on,nullzero"`
	ExpiresOn      calendar.Date `bun:"expires_on,nullzero"`
	AnnualFeeCents int64         `bun:"annual_fee_cents"`
	Surfaces       JSON          `bun:"surfaces"`
	Prescriptions  string        `bun:"prescriptions"`
	Status         string        `bun:"status"` // active, expired, revoked
	Version        int           `bun:"version"`
	CreatedAt      time.Time     `bun:"created_at"`
	UpdatedAt      time.Time     `bun:"updated_at"`
}

// ConcessionObligation is a row of concession_obligations.
type ConcessionObligation struct {
	bun.BaseModel `bun:"table:concession_obligations"`

	ID           string        `bun:"id,pk"`
	MarinaID     string        `bun:"marina_id"`
	ConcessionID string        `bun:"concession_id"`
	Kind         string        `bun:"kind"` // fee_instalment, deposit, insurance, periodic_report, other
	Description  string        `bun:"description"`
	DueOn        calendar.Date `bun:"due_on"`
	Recurrence   string        `bun:"recurrence"` // none, yearly
	AmountCents  *int64        `bun:"amount_cents"`
	DoneOn       calendar.Date `bun:"done_on,nullzero"`
	Version      int           `bun:"version"`
	CreatedAt    time.Time     `bun:"created_at"`
	UpdatedAt    time.Time     `bun:"updated_at"`
}

// BerthRight is a row of berth_rights (spec 14.1).
type BerthRight struct {
	bun.BaseModel `bun:"table:berth_rights"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	BerthID         string        `bun:"berth_id"`
	Kind            string        `bun:"kind"` // ownership_share, long_term_use, membership
	FromOn          calendar.Date `bun:"from_on"`
	ToOn            calendar.Date `bun:"to_on"` // inclusive last night
	Title           JSON          `bun:"title"` // {deed, number, notary, date}
	Status          string        `bun:"status"`
	EndReason       string        `bun:"end_reason"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// BerthRightHolder is a row of berth_right_holders (INV-40: shares of a right sum to 1000).
type BerthRightHolder struct {
	bun.BaseModel `bun:"table:berth_right_holders"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	RightID     string    `bun:"right_id"`
	CustomerID  string    `bun:"customer_id"`
	ShareMillis int64     `bun:"share_millis"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// BerthRightVessel is a row of berth_right_vessels: an authorised vessel of a right.
type BerthRightVessel struct {
	bun.BaseModel `bun:"table:berth_right_vessels"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	RightID     string    `bun:"right_id"`
	VesselID    string    `bun:"vessel_id"`
	IsPrimary   bool      `bun:"is_primary"`
	Fit         JSON      `bun:"fit"`
	FitOverride JSON      `bun:"fit_override"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// BerthRightAllocation links an owner allocation to its right.
type BerthRightAllocation struct {
	bun.BaseModel `bun:"table:berth_right_allocations"`

	AllocationID string    `bun:"allocation_id,pk"`
	MarinaID     string    `bun:"marina_id"`
	RightID      string    `bun:"right_id"`
	CreatedAt    time.Time `bun:"created_at"`
}

// RightTransfer is a row of right_transfers (spec 14.2).
type RightTransfer struct {
	bun.BaseModel `bun:"table:right_transfers"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	RightID            string        `bun:"right_id"`
	EffectiveOn        calendar.Date `bun:"effective_on"`
	ToHolders          JSON          `bun:"to_holders"` // [{customer_id, share_millis}]
	DeclaredPriceCents *int64        `bun:"declared_price_cents"`
	TakeoverItemID     string        `bun:"takeover_item_id,nullzero"`
	TakeoverFeeCents   int64         `bun:"takeover_fee_cents"`
	RequirementPolicy  string        `bun:"requirement_policy"` // block, warn
	RequirementIssues  JSON          `bun:"requirement_issues"`
	FeesPolicy         string        `bun:"fees_policy"` // reassign, prorata
	Status             string        `bun:"status"`      // draft, awaiting_signature, completed, cancelled
	NewRightID         string        `bun:"new_right_id,nullzero"`
	DocumentID         string        `bun:"document_id,nullzero"`
	SignatureRequestID string        `bun:"signature_request_id,nullzero"`
	CompletedAt        time.Time     `bun:"completed_at,nullzero"`
	CancelledAt        time.Time     `bun:"cancelled_at,nullzero"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// ManagementMandate is a row of management_mandates (spec 14.3).
type ManagementMandate struct {
	bun.BaseModel `bun:"table:management_mandates"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	RightID            string        `bun:"right_id"`
	FromOn             calendar.Date `bun:"from_on"`
	ToOn               calendar.Date `bun:"to_on"` // inclusive last night
	OwnerShareBP       int64         `bun:"owner_share_bp"`
	MinGuaranteedCents int64         `bun:"min_guaranteed_cents"`
	Channels           Strings       `bun:"channels"`   // transit, period
	Settlement         string        `bun:"settlement"` // credit, transfer
	Status             string        `bun:"status"`     // active, ended, cancelled
	EndedOn            calendar.Date `bun:"ended_on,nullzero"`
	CreatedByUserID    string        `bun:"created_by_user_id,nullzero"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// MandateRevenue is a row of mandate_revenues: the revenue of one allocation sold inside a mandate.
type MandateRevenue struct {
	bun.BaseModel `bun:"table:mandate_revenues"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	MandateID       string        `bun:"mandate_id"`
	AllocationID    string        `bun:"allocation_id"`
	ContractID      string        `bun:"contract_id,nullzero"`
	Nights          int           `bun:"nights"`
	RevenueCents    int64         `bun:"revenue_cents"`
	OwnerShareCents int64         `bun:"owner_share_cents"`
	ThroughOn       calendar.Date `bun:"through_on"`
	SettledAt       time.Time     `bun:"settled_at,nullzero"` // M5
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// ManagementFeeTable is a row of management_fee_tables (spec 14.4).
type ManagementFeeTable struct {
	bun.BaseModel `bun:"table:management_fee_tables"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	Year      int       `bun:"year"`
	Name      string    `bun:"name"`
	Basis     string    `bun:"basis"` // per_category, per_m2_berth, per_length_band
	IstatBP   int64     `bun:"istat_bp"`
	Status    string    `bun:"status"` // draft, active
	Version   int       `bun:"version"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// ManagementFeeTableRow is a row of management_fee_table_rows.
type ManagementFeeTableRow struct {
	bun.BaseModel `bun:"table:management_fee_table_rows"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	TableID        string    `bun:"table_id"`
	CategoryID     string    `bun:"category_id,nullzero"`
	FromCm         *int64    `bun:"from_cm"`
	ToCm           *int64    `bun:"to_cm"`
	AmountCents    *int64    `bun:"amount_cents"`
	RateCentsPerM2 *int64    `bun:"rate_cents_per_m2"`
	Sort           int       `bun:"sort"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// ManagementFeeRun is a row of management_fee_runs (one issued run per marina, year and table).
type ManagementFeeRun struct {
	bun.BaseModel `bun:"table:management_fee_runs"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Year            int       `bun:"year"`
	TableID         string    `bun:"table_id"`
	Status          string    `bun:"status"` // draft, simulated, issued, discarded
	Plan            JSON      `bun:"plan"`
	Totals          JSON      `bun:"totals"`
	SimulatedAt     time.Time `bun:"simulated_at,nullzero"`
	IssuedAt        time.Time `bun:"issued_at,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// ManagementFeeLine is a row of management_fee_lines (frozen once the run is issued, INV-44).
type ManagementFeeLine struct {
	bun.BaseModel `bun:"table:management_fee_lines"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	RunID           string    `bun:"run_id"`
	RightID         string    `bun:"right_id"`
	BerthID         string    `bun:"berth_id"`
	Category        string    `bun:"category"`
	BaseCents       int64     `bun:"base_cents"`
	Days            int       `bun:"days"`
	AmountCents     int64     `bun:"amount_cents"`
	PreviousCents   *int64    `bun:"previous_cents"`
	Holders         JSON      `bun:"holders"` // [{customer_id, share_millis, amount_cents}]
	Excluded        bool      `bun:"excluded"`
	ExclusionReason string    `bun:"exclusion_reason,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// MillesimalTable is a row of millesimal_tables (INV-41: active only at sum 1000).
type MillesimalTable struct {
	bun.BaseModel `bun:"table:millesimal_tables"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	Name        string    `bun:"name"`
	Code        string    `bun:"code"`
	Status      string    `bun:"status"` // draft, active, archived
	ActivatedAt time.Time `bun:"activated_at,nullzero"`
	ArchivedAt  time.Time `bun:"archived_at,nullzero"`
	Version     int       `bun:"version"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// MillesimalEntry is a row of millesimal_entries.
type MillesimalEntry struct {
	bun.BaseModel `bun:"table:millesimal_entries"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	TableID   string    `bun:"table_id"`
	BerthID   string    `bun:"berth_id"`
	Millis    int64     `bun:"millis"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// CommonChargeBudget is a row of common_charge_budgets (one per marina and year).
type CommonChargeBudget struct {
	bun.BaseModel `bun:"table:common_charge_budgets"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	Year       int       `bun:"year"`
	Name       string    `bun:"name"`
	Status     string    `bun:"status"` // draft, approved, closed
	ApprovedAt time.Time `bun:"approved_at,nullzero"`
	ClosedAt   time.Time `bun:"closed_at,nullzero"`
	Version    int       `bun:"version"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// CommonChargeItem is a row of common_charge_items: a budget item allocated on a millesimal table.
type CommonChargeItem struct {
	bun.BaseModel `bun:"table:common_charge_items"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	BudgetID    string    `bun:"budget_id"`
	Name        string    `bun:"name"`
	TableID     string    `bun:"table_id"`
	BudgetCents int64     `bun:"budget_cents"`
	ActualCents *int64    `bun:"actual_cents"`
	Sort        int       `bun:"sort"`
	Version     int       `bun:"version"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// CommonChargeRun is a row of common_charge_runs (advance or balance).
type CommonChargeRun struct {
	bun.BaseModel `bun:"table:common_charge_runs"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	BudgetID        string        `bun:"budget_id"`
	Kind            string        `bun:"kind"` // advance, balance
	Seq             int           `bun:"seq"`
	ShareBP         *int64        `bun:"share_bp"` // advance only
	ReferenceOn     calendar.Date `bun:"reference_on"`
	Status          string        `bun:"status"` // draft, issued, discarded
	Totals          JSON          `bun:"totals"`
	Plan            JSON          `bun:"plan"`
	IssuedAt        time.Time     `bun:"issued_at,nullzero"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// CommonChargeAllocation is a row of common_charge_allocations (INV-42; frozen once the run is issued, INV-44).
type CommonChargeAllocation struct {
	bun.BaseModel `bun:"table:common_charge_allocations"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	RunID           string    `bun:"run_id"`
	ItemID          string    `bun:"item_id"`
	BerthID         string    `bun:"berth_id"`
	RightID         string    `bun:"right_id,nullzero"`
	CustomerID      string    `bun:"customer_id,nullzero"` // empty: the marina's own share
	BerthShareCents int64     `bun:"berth_share_cents"`
	AmountCents     int64     `bun:"amount_cents"`
	CreatedAt       time.Time `bun:"created_at"`
}

// HolderInstallment is a row of holder_installments: a due amount of a holder (fees, common charges, transfer fee).
type HolderInstallment struct {
	bun.BaseModel `bun:"table:holder_installments"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	SourceType        string        `bun:"source_type"` // management_fee, common_advance, common_balance, transfer_fee
	SourceID          string        `bun:"source_id"`
	RunID             string        `bun:"run_id,nullzero"`
	RightID           string        `bun:"right_id,nullzero"`
	CustomerID        string        `bun:"customer_id"`
	Seq               int           `bun:"seq"`
	Kind              string        `bun:"kind"` // installment, credit
	IssueOn           calendar.Date `bun:"issue_on"`
	DueOn             calendar.Date `bun:"due_on"`
	AmountCents       int64         `bun:"amount_cents"`
	Status            string        `bun:"status"`
	IssuedAt          time.Time     `bun:"issued_at,nullzero"`
	OverdueNotifiedAt time.Time     `bun:"overdue_notified_at,nullzero"`
	ChargeID          string        `bun:"charge_id,nullzero"`
	SettlementSource  string        `bun:"settlement_source,nullzero"`
	SettlementMethod  string        `bun:"settlement_method,nullzero"`
	SettlementRefEnc  string        `bun:"settlement_ref_enc,nullzero"`
	SettledOn         calendar.Date `bun:"settled_on,nullzero"`
	SettledAt         time.Time     `bun:"settled_at,nullzero"`
	SettledByUserID   string        `bun:"settled_by_user_id,nullzero"`
	CancelledAt       time.Time     `bun:"cancelled_at,nullzero"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}
