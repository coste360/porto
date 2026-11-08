package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M5 ledger tables (internal/migrations/*/00012_payments.sql): payments, matches, refunds and the
// office worklist states. Owned by the PAYMENTS builder after the contract phase.

// Payment kinds, methods and sources (m5-plan §4.1).
const (
	PaymentKindPayment = "payment"
	PaymentKindDeposit = "deposit"
	PaymentKindCredit  = "credit"

	PaymentMethodCash         = "cash"
	PaymentMethodCardPOS      = "card_pos"
	PaymentMethodBankTransfer = "bank_transfer"
	PaymentMethodCheque       = "cheque"
	PaymentMethodOther        = "other"
	PaymentMethodCardOnline   = "card_online"
	PaymentMethodWallet       = "wallet"
	PaymentMethodSEPADebit    = "sepa_debit"
	PaymentMethodCreditNote   = "credit_note"

	PaymentSourceManual         = "manual"
	PaymentSourceProvider       = "provider"
	PaymentSourceBackfill       = "backfill"
	PaymentSourceAdjustment     = "adjustment"
	PaymentSourceGoodwill       = "goodwill"
	PaymentSourceRelease        = "release"
	PaymentSourceMandate        = "mandate"
	PaymentSourceOpeningBalance = "opening_balance"
)

// Refund kinds and statuses (m5-plan §4.3).
const (
	RefundKindRefund = "refund"
	RefundKindPayout = "payout"

	RefundPending   = "pending"
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// Payment is a row of payments (spec 16.2). Credit of a customer = Σ (amount − allocated − refunded) of the
// customer's payment and credit rows.
type Payment struct {
	bun.BaseModel `bun:"table:payments"`

	ID                 string        `bun:"id,pk"`
	MarinaID           string        `bun:"marina_id"`
	Number             string        `bun:"number"`
	CustomerID         string        `bun:"customer_id,nullzero"`
	Kind               string        `bun:"kind"`
	Method             string        `bun:"method"`
	Source             string        `bun:"source"`
	ProviderCode       string        `bun:"provider_code,nullzero"`
	ProviderRef        string        `bun:"provider_ref,nullzero"`
	IntentID           string        `bun:"intent_id,nullzero"`
	AdjustmentChargeID string        `bun:"adjustment_charge_id,nullzero"`
	ReleaseID          string        `bun:"release_id,nullzero"`
	MandateID          string        `bun:"mandate_id,nullzero"`
	AmountCents        int64         `bun:"amount_cents"`
	AllocatedCents     int64         `bun:"allocated_cents"`
	RefundedCents      int64         `bun:"refunded_cents"`
	FeeCents           int64         `bun:"fee_cents"`
	Currency           string        `bun:"currency"`
	ReceivedOn         calendar.Date `bun:"received_on"`
	CashSessionID      string        `bun:"cash_session_id,nullzero"`
	VATSplit           JSON          `bun:"vat_split"`
	ReferenceEnc       string        `bun:"reference_enc,nullzero"` // KeyRing, AAD payment-ref:<id>
	Notes              string        `bun:"notes"`
	IdempotencyKey     string        `bun:"idempotency_key,nullzero"`
	IdempotencyHash    string        `bun:"idempotency_hash,nullzero"`
	RecordedByUserID   string        `bun:"recorded_by_user_id,nullzero"`
	RecordedByActor    string        `bun:"recorded_by_actor"`
	Version            int           `bun:"version"`
	CreatedAt          time.Time     `bun:"created_at"`
	UpdatedAt          time.Time     `bun:"updated_at"`
}

// UnallocatedCents is the part of the payment neither matched nor refunded.
func (p *Payment) UnallocatedCents() int64 { return p.AmountCents - p.AllocatedCents - p.RefundedCents }

// PaymentMatch is a row of payment_matches (insert-only but for reversed_cents; a reversal is a row with the
// opposite amount, and a match may have several partial reversals up to its amount).
type PaymentMatch struct {
	bun.BaseModel `bun:"table:payment_matches"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	PaymentID       string    `bun:"payment_id"`
	ChargeID        string    `bun:"charge_id"`
	AmountCents     int64     `bun:"amount_cents"`
	ReversesMatchID string    `bun:"reverses_match_id,nullzero"`
	ReversedCents   int64     `bun:"reversed_cents"` // Σ of its reversals, only via AddMatchReversed; 0 on reversal rows
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedByActor  string    `bun:"created_by_actor"`
	CreatedAt       time.Time `bun:"created_at"`
}

// Refund is a row of refunds: a refund of a payment or a payout (mandate compensation).
type Refund struct {
	bun.BaseModel `bun:"table:refunds"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	Number           string        `bun:"number"`
	PaymentID        string        `bun:"payment_id,nullzero"`
	Kind             string        `bun:"kind"`
	Method           string        `bun:"method"`
	Source           string        `bun:"source"` // manual, provider
	ProviderCode     string        `bun:"provider_code,nullzero"`
	ProviderRef      string        `bun:"provider_ref,nullzero"`
	IntentID         string        `bun:"intent_id,nullzero"`
	Status           string        `bun:"status"`
	AmountCents      int64         `bun:"amount_cents"`
	Reason           string        `bun:"reason"`
	BusinessDate     calendar.Date `bun:"business_date"`
	CashSessionID    string        `bun:"cash_session_id,nullzero"`
	MandateID        string        `bun:"mandate_id,nullzero"`
	CustomerID       string        `bun:"customer_id,nullzero"`
	FailureCode      string        `bun:"failure_code,nullzero"`
	CompletedAt      time.Time     `bun:"completed_at,nullzero"`
	IdempotencyKey   string        `bun:"idempotency_key,nullzero"`
	RecordedByUserID string        `bun:"recorded_by_user_id,nullzero"`
	RecordedByActor  string        `bun:"recorded_by_actor"`
	Version          int           `bun:"version"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// WorkItemState is a row of work_item_states: assignee and status of an office worklist item (spec 28.3).
type WorkItemState struct {
	bun.BaseModel `bun:"table:work_item_states"`

	ID             string    `bun:"id,pk"`
	MarinaID       string    `bun:"marina_id"`
	SourceType     string    `bun:"source_type"`
	SourceID       string    `bun:"source_id"`
	AssigneeUserID string    `bun:"assignee_user_id,nullzero"`
	Status         string    `bun:"status"` // open, in_progress, done, dismissed
	Note           string    `bun:"note"`
	Occurrence     string    `bun:"occurrence"` // occurrence key of a raised item (RaiseWorkItem)
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}
