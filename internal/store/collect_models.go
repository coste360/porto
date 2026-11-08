package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M5 collection tables (internal/migrations/*/00012_payments.sql): provider settings, intents and
// provider events, SEPA mandates, payment requests, reminders and dunning exclusions. Owned by the CASH-DRIVERS
// builder after the contract phase.

// Payment intent purposes and statuses (m5-plan §5.4).
const (
	IntentPurposePaymentRequest = "payment_request"
	IntentPurposeTransit        = "transit"
	IntentPurposeSEPACollect    = "sepa_collect"
	IntentPurposeMandateSetup   = "mandate_setup"

	IntentCreated    = "created"
	IntentOpen       = "open"
	IntentProcessing = "processing"
	IntentSucceeded  = "succeeded"
	IntentFailed     = "failed"
	IntentExpired    = "expired"
	IntentCancelled  = "cancelled"
)

// Reminder levels (m5-plan §5.5, §16 D13).
const (
	ReminderPreDue      = "pre_due"
	ReminderDue         = "due"
	ReminderCourteous   = "courteous"
	ReminderFormal      = "formal"
	ReminderFinalNotice = "final_notice"
)

// PaymentProviderSettings is a row of payment_provider_settings. Secrets are write-only through the API.
type PaymentProviderSettings struct {
	bun.BaseModel `bun:"table:payment_provider_settings"`

	ID                  string    `bun:"id,pk"`
	MarinaID            string    `bun:"marina_id"`
	ProviderCode        string    `bun:"provider_code"`
	Enabled             bool      `bun:"enabled"`
	Mode                string    `bun:"mode"`                        // test, live
	CredentialsEnc      string    `bun:"credentials_enc,nullzero"`    // KeyRing, AAD paycreds:<marina>:<provider>
	WebhookSecretEnc    string    `bun:"webhook_secret_enc,nullzero"` // KeyRing, AAD paycreds:<marina>:<provider>
	PublishableKeyLast4 string    `bun:"publishable_key_last4"`
	SEPAEnabled         bool      `bun:"sepa_enabled"`
	Methods             JSON      `bun:"methods"`
	StatementDescriptor string    `bun:"statement_descriptor"`
	Version             int       `bun:"version"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
}

// PaymentIntent is a row of payment_intents: an online collection attempt through a provider.
type PaymentIntent struct {
	bun.BaseModel `bun:"table:payment_intents"`

	ID                 string    `bun:"id,pk"`
	MarinaID           string    `bun:"marina_id"`
	Purpose            string    `bun:"purpose"`
	ProviderCode       string    `bun:"provider_code"`
	CustomerID         string    `bun:"customer_id"`
	AmountCents        int64     `bun:"amount_cents"`
	Currency           string    `bun:"currency"`
	Method             string    `bun:"method,nullzero"`
	RequestID          string    `bun:"request_id,nullzero"`
	ContractID         string    `bun:"contract_id,nullzero"`
	SEPAMandateID      string    `bun:"sepa_mandate_id,nullzero"`
	ProviderSessionRef string    `bun:"provider_session_ref,nullzero"`
	ProviderPaymentRef string    `bun:"provider_payment_ref,nullzero"`
	CheckoutURL        string    `bun:"checkout_url,nullzero"`
	Status             string    `bun:"status"`
	Attempt            int       `bun:"attempt"`
	IdempotencyKey     string    `bun:"idempotency_key"`
	ExpiresAt          time.Time `bun:"expires_at,nullzero"`
	LastCheckedAt      time.Time `bun:"last_checked_at,nullzero"`
	FailureCode        string    `bun:"failure_code,nullzero"`
	SucceededAt        time.Time `bun:"succeeded_at,nullzero"`
	// M8 manual capture (m8-plan §6, §16 D6): an authorised intent stays processing with AuthorizedAt set.
	CaptureMethod  string    `bun:"capture_method,default:'automatic'"` // automatic, manual
	AuthorizedAt   time.Time `bun:"authorized_at,nullzero"`
	CapturedAt     time.Time `bun:"captured_at,nullzero"`
	VoidedAt       time.Time `bun:"voided_at,nullzero"`
	CreatedByActor string    `bun:"created_by_actor"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// PaymentIntentCharge is a row of payment_intent_charges. OpenFlag is true while the intent is open or processing
// (at most one per charge, §16 D16).
type PaymentIntentCharge struct {
	bun.BaseModel `bun:"table:payment_intent_charges"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	IntentID    string    `bun:"intent_id"`
	ChargeID    string    `bun:"charge_id"`
	AmountCents int64     `bun:"amount_cents"`
	OpenFlag    bool      `bun:"open_flag"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// PaymentProviderEvent is a row of payment_provider_events: the dedupe record of a provider event (INV-56). Not
// backed up.
type PaymentProviderEvent struct {
	bun.BaseModel `bun:"table:payment_provider_events"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	ProviderCode string    `bun:"provider_code"`
	EventID      string    `bun:"event_id"`
	Kind         string    `bun:"kind"`
	IntentID     string    `bun:"intent_id,nullzero"`
	Status       string    `bun:"status"` // applied, ignored, deferred
	ReceivedAt   time.Time `bun:"received_at"`
	CreatedAt    time.Time `bun:"created_at"`
}

// SEPAMandate is a row of sepa_mandates.
type SEPAMandate struct {
	bun.BaseModel `bun:"table:sepa_mandates"`

	ID                     string    `bun:"id,pk"`
	MarinaID               string    `bun:"marina_id"`
	CustomerID             string    `bun:"customer_id"`
	ProviderCode           string    `bun:"provider_code"`
	ProviderCustomerRefEnc string    `bun:"provider_customer_ref_enc,nullzero"` // KeyRing, AAD sepa-customer:<id>
	PaymentMethodRefEnc    string    `bun:"payment_method_ref_enc,nullzero"`    // KeyRing, AAD sepa-pm:<id>
	MandateRef             string    `bun:"mandate_ref,nullzero"`
	IBANLast4              string    `bun:"iban_last4"`
	Status                 string    `bun:"status"` // pending, active, revoked, failed
	SetupIntentID          string    `bun:"setup_intent_id,nullzero"`
	ActivatedAt            time.Time `bun:"activated_at,nullzero"`
	RevokedAt              time.Time `bun:"revoked_at,nullzero"`
	CreatedByUserID        string    `bun:"created_by_user_id,nullzero"`
	Version                int       `bun:"version"`
	CreatedAt              time.Time `bun:"created_at"`
	UpdatedAt              time.Time `bun:"updated_at"`
}

// PaymentRequest is a row of payment_requests (public pay page token stored as SHA-256 only).
type PaymentRequest struct {
	bun.BaseModel `bun:"table:payment_requests"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	CustomerID      string    `bun:"customer_id"`
	TokenHash       string    `bun:"token_hash"`
	AmountCents     int64     `bun:"amount_cents"`
	Currency        string    `bun:"currency"`
	Status          string    `bun:"status"` // open, paid, cancelled, expired
	Source          string    `bun:"source"` // manual, auto, reminder, transit
	ExpiresAt       time.Time `bun:"expires_at,nullzero"`
	SentAt          time.Time `bun:"sent_at,nullzero"`
	SentCount       int       `bun:"sent_count"`
	LastMessageID   string    `bun:"last_message_id,nullzero"`
	PaidAt          time.Time `bun:"paid_at,nullzero"`
	CancelledAt     time.Time `bun:"cancelled_at,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedByActor  string    `bun:"created_by_actor"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// PaymentRequestCharge is a row of payment_request_charges.
type PaymentRequestCharge struct {
	bun.BaseModel `bun:"table:payment_request_charges"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	RequestID   string    `bun:"request_id"`
	ChargeID    string    `bun:"charge_id"`
	AmountCents int64     `bun:"amount_cents"`
	CreatedAt   time.Time `bun:"created_at"`
}

// ReminderRun is a row of reminder_runs (one per marina and business day).
type ReminderRun struct {
	bun.BaseModel `bun:"table:reminder_runs"`

	ID             string        `bun:"id,pk"`
	MarinaID       string        `bun:"marina_id"`
	RunOn          calendar.Date `bun:"run_on"`
	Status         string        `bun:"status"` // running, done, failed
	Counts         JSON          `bun:"counts"`
	StartedAt      time.Time     `bun:"started_at"`
	FinishedAt     time.Time     `bun:"finished_at,nullzero"`
	CreatedByActor string        `bun:"created_by_actor"`
	CreatedAt      time.Time     `bun:"created_at"`
	UpdatedAt      time.Time     `bun:"updated_at"`
}

// ReminderItem is a row of reminder_items (each (charge, level) at most once, INV-58).
type ReminderItem struct {
	bun.BaseModel `bun:"table:reminder_items"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	RunID           string    `bun:"run_id,nullzero"`
	ChargeID        string    `bun:"charge_id"`
	CustomerID      string    `bun:"customer_id"`
	Level           string    `bun:"level"`
	Status          string    `bun:"status"`
	RequestID       string    `bun:"request_id,nullzero"`
	MessageID       string    `bun:"message_id,nullzero"`
	DecidedByUserID string    `bun:"decided_by_user_id,nullzero"`
	DecidedAt       time.Time `bun:"decided_at,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// DunningExclusion is a row of dunning_exclusions (exactly one of customer and charge).
type DunningExclusion struct {
	bun.BaseModel `bun:"table:dunning_exclusions"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	CustomerID      string        `bun:"customer_id,nullzero"`
	ChargeID        string        `bun:"charge_id,nullzero"`
	Kind            string        `bun:"kind"` // payment_plan, disputed, other
	UntilOn         calendar.Date `bun:"until_on,nullzero"`
	Note            string        `bun:"note"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	RemovedAt       time.Time     `bun:"removed_at,nullzero"`
	RemovedByUserID string        `bun:"removed_by_user_id,nullzero"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}
