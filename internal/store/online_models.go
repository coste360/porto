package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M8 online booking tables (internal/migrations/*/00016_online_booking.sql, m8-plan §13).
// Frozen after the CONTRACT phase: builders add queries in their own files (online_quota.go, online_bookings.go,
// channels.go, inquiries_online.go, portal.go, online_settings.go, documents_verify.go), never columns.

// Message raised by the booking_texts immutability trigger.
const MsgBookingTextImmutable = "booking_text_immutable"

// Online quota kinds of berth_groups.online_quota_kind (m8-plan §3.3).
const (
	QuotaNone    = "none"
	QuotaCount   = "count"
	QuotaPercent = "percent"
)

// Booking text kinds (booking_texts.kind, consents.kind).
const (
	BookingTextTerms     = "terms"
	BookingTextPrivacy   = "privacy"
	BookingTextMarketing = "marketing"
)

// Online booking channels (online_bookings.channel, a subset of contracts.channel).
const (
	OnlineChannelOnline   = "online"
	OnlineChannelExternal = "external"
)

// Approval reasons (online_bookings.approval_reason, event transit.approval_requested).
const (
	ApprovalReasonLOA        = "loa"
	ApprovalReasonGroup      = "group"
	ApprovalReasonAlways     = "always"
	ApprovalReasonNoProvider = "no_provider"
)

// Payment intent capture methods (payment_intents.capture_method).
const (
	CaptureAutomatic = "automatic"
	CaptureManual    = "manual"
)

// Customer portal link kinds (customer_portal_links.kind).
const (
	PortalLinkLogin   = "login"
	PortalLinkBooking = "booking"
)

// Channel reservation statuses and reject reasons (channel_reservations).
const (
	ChannelResReceived  = "received"
	ChannelResBooked    = "booked"
	ChannelResRejected  = "rejected"
	ChannelResCancelled = "cancelled"

	ChannelRejectNoSpace           = "no_space"
	ChannelRejectRules             = "rules"
	ChannelRejectUnmappedCategory  = "unmapped_category"
	ChannelRejectModifyUnsupported = "modify_unsupported"
	ChannelRejectInvalid           = "invalid"
	ChannelRejectNotFound          = "not_found"
)

// Channel event statuses (channel_events.status).
const (
	ChannelEventApplied = "applied"
	ChannelEventIgnored = "ignored"
)

// OnlineBookingSettings is the row of online_booking_settings of one marina (m8-plan §3.1). A missing row means
// the defaults of onlineconf.Defaults(). Booleans with a true default are written explicitly (no default tag).
type OnlineBookingSettings struct {
	bun.BaseModel `bun:"table:online_booking_settings"`

	MarinaID             string    `bun:"marina_id,pk"`
	MinNights            int       `bun:"min_nights"`
	MaxNights            int       `bun:"max_nights"`
	MinAdvanceHours      int       `bun:"min_advance_hours"`
	MaxAdvanceDays       int       `bun:"max_advance_days"`
	ArrivalFrom          string    `bun:"arrival_from"` // HH:MM marina time
	ArrivalTo            string    `bun:"arrival_to"`
	ClosedWeekdays       JSON      `bun:"closed_weekdays"`    // [0..6], 0 = Sunday
	PaymentMode          string    `bun:"payment_mode"`       // deposit, full
	DepositBP            int       `bun:"deposit_bp"`         // basis points of the total
	CancellationTiers    JSON      `bun:"cancellation_tiers"` // [{min_days_before, refund_bp}]
	RefundAs             string    `bun:"refund_as"`          // refund, credit
	Approval             JSON      `bun:"approval"`           // {loa_over_cm, group_ids, always, hold_hours}
	RequiredFields       JSON      `bun:"required_fields"`    // {documents, people_on_board, phone, documents_later}
	RevealBerth          string    `bun:"reveal_berth"`       // never, after_confirmation
	LatePaymentRecovery  bool      `bun:"late_payment_recovery"`
	AllowWithoutProvider bool      `bun:"allow_without_provider"`
	MaxLiveHolds         int       `bun:"max_live_holds"`
	ArrivalInfo          JSON      `bun:"arrival_info"` // {vhf_channel, phone, rules_url, text: I18nText}
	ThemePreset          string    `bun:"theme_preset"`
	HideHeaderInEmbed    bool      `bun:"hide_header_in_embed"`
	ShowFeet             bool      `bun:"show_feet"`
	Version              int       `bun:"version"`
	CreatedAt            time.Time `bun:"created_at"`
	UpdatedAt            time.Time `bun:"updated_at"`
}

// BookingText is a row of booking_texts: one published, immutable version of a terms, privacy or marketing text.
type BookingText struct {
	bun.BaseModel `bun:"table:booking_texts"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	Kind              string    `bun:"kind"` // terms, privacy, marketing
	Version           int       `bun:"version"`
	Texts             JSON      `bun:"texts"`  // I18nText
	SHA256            string    `bun:"sha256"` // hex of the canonical texts JSON
	PublishedAt       time.Time `bun:"published_at"`
	PublishedByUserID string    `bun:"published_by_user_id,nullzero"`
	CreatedAt         time.Time `bun:"created_at"`
}

// OnlineBooking is a row of online_bookings: the online side of a transit created by the public page (channel
// online) or a booking channel (channel external). The transit state lives on the contract (INV-30).
type OnlineBooking struct {
	bun.BaseModel `bun:"table:online_bookings"`

	ID                string    `bun:"id,pk"`
	MarinaID          string    `bun:"marina_id"`
	ContractID        string    `bun:"contract_id"`
	Channel           string    `bun:"channel,default:'online'"` // online, external
	Reference         string    `bun:"reference"`                // 8 Crockford base32 characters
	TokenNonce        string    `bun:"token_nonce"`              // the booking token is HMAC(nonce), never stored
	IdempotencyHash   string    `bun:"idempotency_hash,nullzero"`
	BodySHA256        string    `bun:"body_sha256,nullzero"`
	ContactEnc        string    `bun:"contact_enc,nullzero"` // KeyRing, AAD booking-contact:<id>
	PeopleOnBoard     int       `bun:"people_on_board,nullzero"`
	ApprovalReason    string    `bun:"approval_reason,nullzero"` // loa, group, always, no_provider
	ApprovalDeadline  time.Time `bun:"approval_deadline,nullzero"`
	ApprovedAt        time.Time `bun:"approved_at,nullzero"`
	ApprovedByUserID  string    `bun:"approved_by_user_id,nullzero"`
	RejectedAt        time.Time `bun:"rejected_at,nullzero"`
	RejectReason      string    `bun:"reject_reason,nullzero"`
	EmbedParentOrigin string    `bun:"embed_parent_origin,nullzero"`
	Locale            string    `bun:"locale,default:'it'"`
	InquiryID         string    `bun:"inquiry_id,nullzero"`
	Version           int       `bun:"version"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

// OnlineQuotaUsage is a row of online_quota_usage: the online counter of one group and night (INV-12).
type OnlineQuotaUsage struct {
	bun.BaseModel `bun:"table:online_quota_usage"`

	MarinaID  string        `bun:"marina_id"`
	GroupID   string        `bun:"group_id,pk"`
	Night     calendar.Date `bun:"night,pk"`
	Cap       int           `bun:"cap"`
	Used      int           `bun:"used"`
	UpdatedAt time.Time     `bun:"updated_at"`
}

// OnlineQuotaClaim is a row of online_quota_claims: one unit of a group and night held by a contract.
type OnlineQuotaClaim struct {
	bun.BaseModel `bun:"table:online_quota_claims"`

	MarinaID   string        `bun:"marina_id"`
	ContractID string        `bun:"contract_id,pk"`
	GroupID    string        `bun:"group_id,pk"`
	Night      calendar.Date `bun:"night,pk"`
	CreatedAt  time.Time     `bun:"created_at"`
}

// CustomerPortalLink is a row of customer_portal_links: a login link (single use, 30 min) or the booking link of
// a confirmation email (reusable until departure + 30 days). Only the SHA-256 of the token is stored.
type CustomerPortalLink struct {
	bun.BaseModel `bun:"table:customer_portal_links"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	CustomerID string    `bun:"customer_id"`
	Kind       string    `bun:"kind"` // login, booking
	ContractID string    `bun:"contract_id,nullzero"`
	TokenHash  string    `bun:"token_hash"`
	ExpiresAt  time.Time `bun:"expires_at"`
	UsedAt     time.Time `bun:"used_at,nullzero"`
	CreatedAt  time.Time `bun:"created_at"`
}

// CustomerPortalSession is a row of customer_portal_sessions (8 h idle from LastSeenAt, absolute ExpiresAt).
type CustomerPortalSession struct {
	bun.BaseModel `bun:"table:customer_portal_sessions"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	CustomerID string    `bun:"customer_id"`
	TokenHash  string    `bun:"token_hash"`
	LastSeenAt time.Time `bun:"last_seen_at"`
	ExpiresAt  time.Time `bun:"expires_at"`
	RevokedAt  time.Time `bun:"revoked_at,nullzero"`
	CreatedAt  time.Time `bun:"created_at"`
}

// ChannelConnection is a row of channel_connections. Secrets are write-only through the API.
type ChannelConnection struct {
	bun.BaseModel `bun:"table:channel_connections"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	ChannelCode      string    `bun:"channel_code"`
	Enabled          bool      `bun:"enabled"`
	CredentialsEnc   string    `bun:"credentials_enc,nullzero"`    // KeyRing, AAD channel:<marina>:<code>
	WebhookSecretEnc string    `bun:"webhook_secret_enc,nullzero"` // KeyRing, AAD channel:<marina>:<code>
	CategoryMap      JSON      `bun:"category_map"`                // {external code: Porto category code}
	PushEnabled      bool      `bun:"push_enabled"`
	PushDirty        bool      `bun:"push_dirty"`
	PullCursor       string    `bun:"pull_cursor"`
	LastPushAt       time.Time `bun:"last_push_at,nullzero"`
	LastFullPushAt   time.Time `bun:"last_full_push_at,nullzero"`
	LastPullAt       time.Time `bun:"last_pull_at,nullzero"`
	LastError        string    `bun:"last_error"` // error code only
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// ChannelReservation is a row of channel_reservations: one external reservation, applied once (INV-89).
type ChannelReservation struct {
	bun.BaseModel `bun:"table:channel_reservations"`

	ID            string    `bun:"id,pk"`
	MarinaID      string    `bun:"marina_id"`
	ChannelCode   string    `bun:"channel_code"`
	ExternalRef   string    `bun:"external_ref"`
	ContractID    string    `bun:"contract_id,nullzero"`
	Status        string    `bun:"status"`
	RejectReason  string    `bun:"reject_reason,nullzero"`
	PayloadSHA256 string    `bun:"payload_sha256"`
	CreatedAt     time.Time `bun:"created_at"`
	UpdatedAt     time.Time `bun:"updated_at"`
}

// ChannelEvent is a row of channel_events: the dedupe record of one channel event (INV-89).
type ChannelEvent struct {
	bun.BaseModel `bun:"table:channel_events"`

	ID            string    `bun:"id,pk"`
	MarinaID      string    `bun:"marina_id"`
	ChannelCode   string    `bun:"channel_code"`
	EventID       string    `bun:"event_id"`
	ReservationID string    `bun:"reservation_id,nullzero"`
	Status        string    `bun:"status"` // applied, ignored
	ReceivedAt    time.Time `bun:"received_at"`
	CreatedAt     time.Time `bun:"created_at"`
}
