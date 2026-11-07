package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M0 tables (internal/migrations/*/00001_init.sql). Nullable text and uuid columns map to
// string with nullzero ("" = NULL); nullable instants map to time.Time with nullzero.

// Organization is a row of organizations.
type Organization struct {
	bun.BaseModel `bun:"table:organizations"`

	ID             string    `bun:"id,pk"`
	Name           string    `bun:"name"`
	VATNumber      string    `bun:"vat_number,nullzero"`
	FiscalCode     string    `bun:"fiscal_code,nullzero"`
	BillingAddress JSON      `bun:"billing_address"`
	SDICode        string    `bun:"sdi_code,nullzero"`
	PEC            string    `bun:"pec,nullzero"`
	Version        int       `bun:"version"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// Marina is a row of marinas.
type Marina struct {
	bun.BaseModel `bun:"table:marinas"`

	ID                  string        `bun:"id,pk"`
	OrganizationID      string        `bun:"organization_id"`
	Slug                string        `bun:"slug"`
	Name                string        `bun:"name"`
	Kind                string        `bun:"kind"`
	Timezone            string        `bun:"timezone"`
	Currency            string        `bun:"currency"`
	DefaultLocale       string        `bun:"default_locale"`
	Locales             Strings       `bun:"locales"`
	Contacts            JSON          `bun:"contacts"`
	Address             JSON          `bun:"address"`
	Geo                 JSON          `bun:"geo"`
	ConcessionRef       string        `bun:"concession_ref,nullzero"`
	ConcessionExpiresOn calendar.Date `bun:"concession_expires_on,nullzero"`
	Settings            JSON          `bun:"settings"`
	SettingsVersion     int           `bun:"settings_version"`
	Theme               JSON          `bun:"theme"`
	BookingEnabled      bool          `bun:"booking_enabled"`
	AllowedEmbedOrigins Strings       `bun:"allowed_embed_origins"`
	Version             int           `bun:"version"`
	ArchivedAt          time.Time     `bun:"archived_at,nullzero"`
	CreatedAt           time.Time     `bun:"created_at"`
	UpdatedAt           time.Time     `bun:"updated_at"`
}

// User is a row of users.
type User struct {
	bun.BaseModel `bun:"table:users"`

	ID                string    `bun:"id,pk"`
	Email             string    `bun:"email"`
	PasswordHash      string    `bun:"password_hash,nullzero"`
	Name              string    `bun:"name"`
	Locale            string    `bun:"locale"`
	Active            bool      `bun:"active"`
	PasswordChangedAt time.Time `bun:"password_changed_at,nullzero"`
	LastLoginAt       time.Time `bun:"last_login_at,nullzero"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// UserTOTP is a row of user_totp.
type UserTOTP struct {
	bun.BaseModel `bun:"table:user_totp"`

	ID          string    `bun:"id,pk"`
	UserID      string    `bun:"user_id"`
	SecretEnc   string    `bun:"secret_enc"`
	ConfirmedAt time.Time `bun:"confirmed_at,nullzero"`
	LastStep    int64     `bun:"last_step"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// RecoveryCode is a row of recovery_codes.
type RecoveryCode struct {
	bun.BaseModel `bun:"table:recovery_codes"`

	ID        string    `bun:"id,pk"`
	UserID    string    `bun:"user_id"`
	CodeHash  string    `bun:"code_hash"`
	UsedAt    time.Time `bun:"used_at,nullzero"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// Membership is a row of memberships. MarinaID "" is an organization-wide grant.
type Membership struct {
	bun.BaseModel `bun:"table:memberships"`

	ID              string    `bun:"id,pk"`
	UserID          string    `bun:"user_id"`
	OrganizationID  string    `bun:"organization_id"`
	MarinaID        string    `bun:"marina_id,nullzero"`
	Role            string    `bun:"role"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// Session is a row of sessions.
type Session struct {
	bun.BaseModel `bun:"table:sessions"`

	ID            string    `bun:"id,pk"`
	TokenHash     string    `bun:"token_hash"`
	UserID        string    `bun:"user_id"`
	TOTPVerified  bool      `bun:"totp_verified"`
	ExpiresAt     time.Time `bun:"expires_at"`
	IdleExpiresAt time.Time `bun:"idle_expires_at"`
	LastSeenAt    time.Time `bun:"last_seen_at"`
	IP            string    `bun:"ip"`
	UserAgent     string    `bun:"user_agent"`
	// DeviceID binds a device session (browser paired by QR, M6a) to its device; "" for ordinary logins.
	DeviceID  string    `bun:"device_id,nullzero"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// Password reset token purposes.
const (
	PurposeReset  = "reset"
	PurposeInvite = "invite"
)

// PasswordResetToken is a row of password_reset_tokens.
type PasswordResetToken struct {
	bun.BaseModel `bun:"table:password_reset_tokens"`

	ID        string    `bun:"id,pk"`
	UserID    string    `bun:"user_id"`
	TokenHash string    `bun:"token_hash"`
	Purpose   string    `bun:"purpose"`
	ExpiresAt time.Time `bun:"expires_at"`
	UsedAt    time.Time `bun:"used_at,nullzero"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// APIKey is a row of api_keys.
type APIKey struct {
	bun.BaseModel `bun:"table:api_keys"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Name            string    `bun:"name"`
	Prefix          string    `bun:"prefix"`
	KeyHash         string    `bun:"key_hash"`
	Scopes          Strings   `bun:"scopes"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	ExpiresAt       time.Time `bun:"expires_at,nullzero"`
	LastUsedAt      time.Time `bun:"last_used_at,nullzero"`
	RevokedAt       time.Time `bun:"revoked_at,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// Device is a row of devices.
type Device struct {
	bun.BaseModel `bun:"table:devices"`

	ID            string `bun:"id,pk"`
	MarinaID      string `bun:"marina_id"`
	UserID        string `bun:"user_id"`
	Name          string `bun:"name"`
	Platform      string `bun:"platform"`
	Kind          string `bun:"kind"`
	AppVersion    string `bun:"app_version,nullzero"`
	TokenPrefix   string `bun:"token_prefix"`
	TokenHash     string `bun:"token_hash"`
	PairingCodeID string `bun:"pairing_code_id,nullzero"`
	// Access is DeviceAccessToken (po_dev_ token) or DeviceAccessBrowser (device session cookie, M6a).
	Access     string    `bun:"access"`
	LastUsedAt time.Time `bun:"last_used_at,nullzero"`
	RevokedAt  time.Time `bun:"revoked_at,nullzero"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// Device access kinds (devices.access, M6a).
const (
	DeviceAccessToken   = "token"
	DeviceAccessBrowser = "browser"
)

// PairingCode is a row of pairing_codes.
type PairingCode struct {
	bun.BaseModel `bun:"table:pairing_codes"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	UserID          string    `bun:"user_id"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	Kind            string    `bun:"kind,nullzero"`
	CodeHash        string    `bun:"code_hash"`
	ExpiresAt       time.Time `bun:"expires_at"`
	UsedAt          time.Time `bun:"used_at,nullzero"`
	DeviceID        string    `bun:"device_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// Idempotency key states.
const (
	IdempotencyInProgress = "in_progress"
	IdempotencyCompleted  = "completed"
)

// IdempotencyKey is a row of idempotency_keys.
type IdempotencyKey struct {
	bun.BaseModel `bun:"table:idempotency_keys"`

	ID                  string    `bun:"id,pk"`
	Scope               string    `bun:"scope"`
	Key                 string    `bun:"key"`
	RequestHash         string    `bun:"request_hash"`
	State               string    `bun:"state"`
	ResponseStatus      int       `bun:"response_status,nullzero"`
	ResponseContentType string    `bun:"response_content_type,nullzero"`
	ResponseBody        []byte    `bun:"response_body"`
	ExpiresAt           time.Time `bun:"expires_at"`
	CreatedAt           time.Time `bun:"created_at"`
	UpdatedAt           time.Time `bun:"updated_at"`
}

// AuditEntry is a row of audit_log (insert-only).
type AuditEntry struct {
	bun.BaseModel `bun:"table:audit_log"`

	ID             string    `bun:"id,pk"`
	OrganizationID string    `bun:"organization_id,nullzero"`
	MarinaID       string    `bun:"marina_id,nullzero"`
	ActorType      string    `bun:"actor_type"`
	ActorID        string    `bun:"actor_id,nullzero"`
	Action         string    `bun:"action"`
	EntityType     string    `bun:"entity_type"`
	EntityID       string    `bun:"entity_id,nullzero"`
	Diff           JSON      `bun:"diff"`
	RequestID      string    `bun:"request_id"`
	IP             string    `bun:"ip"`
	CreatedAt      time.Time `bun:"created_at"`
}

// OutboxEvent is a row of outbox_events.
type OutboxEvent struct {
	bun.BaseModel `bun:"table:outbox_events"`

	ID             string    `bun:"id,pk"`
	OrganizationID string    `bun:"organization_id,nullzero"`
	MarinaID       string    `bun:"marina_id,nullzero"`
	Type           string    `bun:"type"`
	Version        int       `bun:"version"`
	Payload        JSON      `bun:"payload"`
	OccurredAt     time.Time `bun:"occurred_at"`
	Attempts       int       `bun:"attempts"`
	NextAttemptAt  time.Time `bun:"next_attempt_at,nullzero"`
	LockedUntil    time.Time `bun:"locked_until,nullzero"`
	LastError      string    `bun:"last_error,nullzero"`
	DispatchedAt   time.Time `bun:"dispatched_at,nullzero"`
	CreatedAt      time.Time `bun:"created_at"`
	UpdatedAt      time.Time `bun:"updated_at"`
}

// WebhookEndpoint is a row of webhook_endpoints.
type WebhookEndpoint struct {
	bun.BaseModel `bun:"table:webhook_endpoints"`

	ID                      string    `bun:"id,pk"`
	MarinaID                string    `bun:"marina_id"`
	URL                     string    `bun:"url"`
	Description             string    `bun:"description"`
	SecretEnc               string    `bun:"secret_enc"`
	PreviousSecretEnc       string    `bun:"previous_secret_enc,nullzero"`
	PreviousSecretExpiresAt time.Time `bun:"previous_secret_expires_at,nullzero"`
	Events                  Strings   `bun:"events"`
	Active                  bool      `bun:"active"`
	ConsecutiveFailures     int       `bun:"consecutive_failures"`
	DisabledAt              time.Time `bun:"disabled_at,nullzero"`
	DisabledReason          string    `bun:"disabled_reason,nullzero"`
	Version                 int       `bun:"version"`
	CreatedAt               time.Time `bun:"created_at"`
	UpdatedAt               time.Time `bun:"updated_at"`
}

// Webhook delivery states.
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
)

// WebhookDelivery is a row of webhook_deliveries.
type WebhookDelivery struct {
	bun.BaseModel `bun:"table:webhook_deliveries"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	EndpointID      string    `bun:"endpoint_id"`
	EventID         string    `bun:"event_id"`
	EventType       string    `bun:"event_type"`
	Status          string    `bun:"status"`
	Attempts        int       `bun:"attempts"`
	LastStatusCode  int       `bun:"last_status_code,nullzero"`
	LastError       string    `bun:"last_error,nullzero"`
	ResponseExcerpt string    `bun:"response_excerpt,nullzero"`
	FirstAttemptAt  time.Time `bun:"first_attempt_at,nullzero"`
	NextAttemptAt   time.Time `bun:"next_attempt_at"`
	LockedUntil     time.Time `bun:"locked_until,nullzero"`
	DeliveredAt     time.Time `bun:"delivered_at,nullzero"`
	FailedAt        time.Time `bun:"failed_at,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// Message states.
const (
	MessageQueued    = "queued"
	MessageSending   = "sending"
	MessageSent      = "sent"
	MessageFailed    = "failed"
	MessageCancelled = "cancelled"
)

// Message is a row of message_log (email queue and delivery log).
type Message struct {
	bun.BaseModel `bun:"table:message_log"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id,nullzero"`
	UserID            string    `bun:"user_id,nullzero"`
	Channel           string    `bun:"channel"`
	Template          string    `bun:"template"`
	Locale            string    `bun:"locale"`
	RecipientHash     string    `bun:"recipient_hash"`
	RecipientEnc      string    `bun:"recipient_enc"`
	PayloadEnc        string    `bun:"payload_enc"`
	DedupeKey         string    `bun:"dedupe_key"`
	Status            string    `bun:"status"`
	Attempts          int       `bun:"attempts"`
	NextAttemptAt     time.Time `bun:"next_attempt_at,nullzero"`
	LockedUntil       time.Time `bun:"locked_until,nullzero"`
	LastError         string    `bun:"last_error,nullzero"`
	ProviderMessageID string    `bun:"provider_message_id,nullzero"`
	SentAt            time.Time `bun:"sent_at,nullzero"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}
