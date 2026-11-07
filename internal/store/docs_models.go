package store

import (
	"time"

	"github.com/uptrace/bun"
)

// Row types of the M4 documents tables (internal/migrations/*/00009_documents.sql). Conventions as in
// contract_models.go. Owned by the DOCUMENTS builder after the contract phase (fields may be added only if they
// exist in the migration).

// Template is a row of templates: a marina print template of a catalogue kind (spec 15.1).
type Template struct {
	bun.BaseModel `bun:"table:templates"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Kind            string    `bun:"kind"`
	Code            string    `bun:"code"`
	Name            string    `bun:"name"`
	IsDefault       bool      `bun:"is_default"`
	ArchivedAt      time.Time `bun:"archived_at,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// TemplateVersion is a row of template_versions (published rows are immutable).
type TemplateVersion struct {
	bun.BaseModel `bun:"table:template_versions"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	TemplateID        string    `bun:"template_id"`
	Version           int       `bun:"version"`
	Locale            string    `bun:"locale"`
	BodyMD            string    `bun:"body_md"`
	HeaderMD          string    `bun:"header_md"`
	FooterMD          string    `bun:"footer_md"`
	Page              JSON      `bun:"page"`   // {margins_mm, font_size_pt}
	Status            string    `bun:"status"` // draft, published, superseded
	SHA256            string    `bun:"sha256,nullzero"`
	PublishedAt       time.Time `bun:"published_at,nullzero"`
	PublishedByUserID string    `bun:"published_by_user_id,nullzero"`
	CreatedByUserID   string    `bun:"created_by_user_id,nullzero"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// GeneratedDocument is a row of generated_documents: a composed (and, once ready, rendered) PDF (spec 15.2).
type GeneratedDocument struct {
	bun.BaseModel `bun:"table:generated_documents"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	Kind              string    `bun:"kind"`
	TemplateVersionID string    `bun:"template_version_id,nullzero"`
	BuiltinRef        string    `bun:"builtin_ref,nullzero"`
	Locale            string    `bun:"locale"`
	SubjectType       string    `bun:"subject_type"`
	SubjectID         string    `bun:"subject_id"`
	Status            string    `bun:"status"`            // pending, rendering, ready, failed, superseded
	DataEnc           string    `bun:"data_enc,nullzero"` // KeyRing, AAD docdata:<id>
	FileID            string    `bun:"file_id,nullzero"`  // stored_files
	SHA256            string    `bun:"sha256,nullzero"`   // of the plaintext PDF
	Pages             int       `bun:"pages"`
	PDFA              bool      `bun:"pdfa"`
	Signed            bool      `bun:"signed"`
	SignedFromID      string    `bun:"signed_from_id,nullzero"`
	Attempts          int       `bun:"attempts"`
	NextAttemptAt     time.Time `bun:"next_attempt_at,nullzero"`
	LeaseUntil        time.Time `bun:"lease_until,nullzero"`
	ErrorCode         string    `bun:"error_code,nullzero"`
	RenderedAt        time.Time `bun:"rendered_at,nullzero"`
	CreatedByUserID   string    `bun:"created_by_user_id,nullzero"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// SignatureRequest is a row of signature_requests (one open request per subject).
type SignatureRequest struct {
	bun.BaseModel `bun:"table:signature_requests"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	SubjectType      string    `bun:"subject_type"` // contract, addendum, right_transfer
	SubjectID        string    `bun:"subject_id"`
	DocumentID       string    `bun:"document_id"`
	Driver           string    `bun:"driver"` // paper, otp, provider
	ProviderCode     string    `bun:"provider_code,nullzero"`
	EnvelopeRef      string    `bun:"envelope_ref,nullzero"`
	OrderMode        string    `bun:"order_mode"` // sequential, parallel
	Status           string    `bun:"status"`
	ExpiresAt        time.Time `bun:"expires_at"`
	LastReminderAt   time.Time `bun:"last_reminder_at,nullzero"`
	RemindersSent    int       `bun:"reminders_sent"`
	SignedDocumentID string    `bun:"signed_document_id,nullzero"`
	CompletedAt      time.Time `bun:"completed_at,nullzero"`
	CreatedByUserID  string    `bun:"created_by_user_id,nullzero"`
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// SignatureSigner is a row of signature_signers. Names and emails are KeyRing ciphertexts.
type SignatureSigner struct {
	bun.BaseModel `bun:"table:signature_signers"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	RequestID       string    `bun:"request_id"`
	Seq             int       `bun:"seq"`
	Role            string    `bun:"role"`
	CustomerID      string    `bun:"customer_id,nullzero"`
	UserID          string    `bun:"user_id,nullzero"`
	DisplayNameEnc  string    `bun:"display_name_enc"`
	EmailEnc        string    `bun:"email_enc,nullzero"`
	EmailHash       string    `bun:"email_hash,nullzero"`
	Locale          string    `bun:"locale"`
	Status          string    `bun:"status"` // waiting, invited, signed, declined
	LinkID          string    `bun:"link_id,nullzero"`
	InvitedAt       time.Time `bun:"invited_at,nullzero"`
	OpenedAt        time.Time `bun:"opened_at,nullzero"`
	SignedAt        time.Time `bun:"signed_at,nullzero"`
	DeclinedAt      time.Time `bun:"declined_at,nullzero"`
	EvidenceEnc     string    `bun:"evidence_enc,nullzero"`
	PaperScanFileID string    `bun:"paper_scan_file_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// SignatureOTP is a row of signature_otps (never backed up).
type SignatureOTP struct {
	bun.BaseModel `bun:"table:signature_otps"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	SignerID   string    `bun:"signer_id"`
	CodeHMAC   string    `bun:"code_hmac"`
	ExpiresAt  time.Time `bun:"expires_at"`
	Attempts   int       `bun:"attempts"`
	ConsumedAt time.Time `bun:"consumed_at,nullzero"`
	SentAt     time.Time `bun:"sent_at,nullzero"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// AccessLink is a row of access_links: a customer magic link, stored by the SHA-256 of its token (INV-75).
type AccessLink struct {
	bun.BaseModel `bun:"table:access_links"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	Purpose     string    `bun:"purpose"` // quote, renewal, signature, unsubscribe
	SubjectType string    `bun:"subject_type"`
	SubjectID   string    `bun:"subject_id"`
	CustomerID  string    `bun:"customer_id,nullzero"`
	TokenHash   string    `bun:"token_hash"`
	ExpiresAt   time.Time `bun:"expires_at"`
	RevokedAt   time.Time `bun:"revoked_at,nullzero"`
	LastUsedAt  time.Time `bun:"last_used_at,nullzero"`
	UseCount    int       `bun:"use_count"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// CommCampaign is a row of comm_campaigns: a mass customer communication (spec 28.2).
type CommCampaign struct {
	bun.BaseModel `bun:"table:comm_campaigns"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Name            string    `bun:"name"`
	Category        string    `bun:"category"` // service, promotional
	Audience        JSON      `bun:"audience"`
	Channels        Strings   `bun:"channels"`
	TemplateID      string    `bun:"template_id,nullzero"`
	Subject         string    `bun:"subject"`
	BodyMD          string    `bun:"body_md"`
	Attachments     JSON      `bun:"attachments"`
	ScheduledAt     time.Time `bun:"scheduled_at,nullzero"`
	Status          string    `bun:"status"` // draft, scheduled, sending, sent, cancelled
	Counts          JSON      `bun:"counts"`
	SentAt          time.Time `bun:"sent_at,nullzero"`
	CancelledAt     time.Time `bun:"cancelled_at,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// CommRecipient is a row of comm_recipients.
type CommRecipient struct {
	bun.BaseModel `bun:"table:comm_recipients"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	CampaignID string    `bun:"campaign_id"`
	CustomerID string    `bun:"customer_id"`
	Status     string    `bun:"status"` // pending, queued, skipped_no_consent, skipped_no_email, failed
	MessageID  string    `bun:"message_id,nullzero"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// CustomerMessage is a row of customer_messages: the per-customer registry of emails (status from message_log).
type CustomerMessage struct {
	bun.BaseModel `bun:"table:customer_messages"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	CustomerID string    `bun:"customer_id"`
	MessageID  string    `bun:"message_id"`
	Category   string    `bun:"category"` // service, promotional, transactional
	SourceType string    `bun:"source_type"`
	SourceID   string    `bun:"source_id,nullzero"`
	Template   string    `bun:"template"`
	CreatedAt  time.Time `bun:"created_at"`
}

// SignatureSettings is the row of signature_settings of a marina: provider credentials (KeyRing ciphertexts).
type SignatureSettings struct {
	bun.BaseModel `bun:"table:signature_settings"`

	MarinaID         string    `bun:"marina_id,pk"`
	ProviderCode     string    `bun:"provider_code,nullzero"`
	CredentialsEnc   string    `bun:"credentials_enc,nullzero"`
	WebhookSecretEnc string    `bun:"webhook_secret_enc,nullzero"`
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// ImportRun is a row of import_runs: one CSV import (dry run or applied).
type ImportRun struct {
	bun.BaseModel `bun:"table:import_runs"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Entity          string    `bun:"entity"` // customers_vessels, contracts, rights, deadlines
	Status          string    `bun:"status"` // dry_run, applied, undone, failed
	Mapping         JSON      `bun:"mapping"`
	Report          JSON      `bun:"report"`
	FileSHA256      string    `bun:"file_sha256"`
	RowCount        int       `bun:"row_count"`
	AppliedAt       time.Time `bun:"applied_at,nullzero"`
	UndoneAt        time.Time `bun:"undone_at,nullzero"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// ImportRunItem is a row of import_run_items: a record created by an applied import (for undo).
type ImportRunItem struct {
	bun.BaseModel `bun:"table:import_run_items"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	RunID      string    `bun:"run_id"`
	EntityType string    `bun:"entity_type"`
	EntityID   string    `bun:"entity_id"`
	CreatedAt  time.Time `bun:"created_at"`
}
