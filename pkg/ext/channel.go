package ext

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// BookingChannel connects an external berth marketplace to Porto (spec 23.2, 27.5; m8-plan §8). Inbound
// reservations go through the same booking service and occupancy rules as the public page; a channel never writes
// occupancy itself. Calls never run inside a database transaction. Credentials arrive per call in the context
// (WithChannelCredentials); outbound HTTP goes only through the client the host gives the driver at construction.
// Guest data are personal data: drivers must never log them.
type BookingChannel interface {
	// Code is the stable channel code (lower case, 1..32 characters), used in URLs and settings.
	Code() string
	Capabilities() ChannelCapabilities
	// PushAvailability sends free units per category and night (ErrNotSupported without Push).
	PushAvailability(ctx context.Context, a ChannelAvailability) error
	// PushRates sends the price per category and night for a reference size (ErrNotSupported without Push).
	PushRates(ctx context.Context, r ChannelRates) error
	// PullReservations returns the reservations changed after the opaque cursor since ("" = from the start) and
	// the cursor to store for the next call (ErrNotSupported without Pull).
	PullReservations(ctx context.Context, since string) (res []ChannelReservation, next string, err error)
	// ParseWebhook verifies a callback with the connection's webhook secret against now (the host's clock) and
	// decodes it: ErrInvalidSignature, or ErrIgnoredEvent for events Porto does not use.
	ParseWebhook(r *http.Request, secret []byte, now time.Time) ([]ChannelReservation, error)
	// Acknowledge tells the channel the outcome of one reservation (ErrNotSupported without Ack).
	Acknowledge(ctx context.Context, ack ChannelAck) error
}

// ErrUnknownChannel is returned by the host for a channel code no extension provides.
var ErrUnknownChannel = errors.New("ext: unknown booking channel")

// ChannelCapabilities says what a channel driver can do.
type ChannelCapabilities struct {
	Push    bool // availability and rates
	Pull    bool // PullReservations
	Webhook bool // ParseWebhook
	Ack     bool // Acknowledge
}

// ChannelNight is the availability or the rate of one category on one night.
type ChannelNight struct {
	Night     string // YYYY-MM-DD, marina time zone
	Available bool
	Units     int   // free units, already capped by the online quota
	Cents     int64 // price of the night for the reference size (rates only)
}

// ChannelCategory is one category (an external code from the connection's category map) and its nights.
type ChannelCategory struct {
	Code   string // external category code
	Nights []ChannelNight
}

// ChannelAvailability is a push of free units.
type ChannelAvailability struct {
	MarinaRef  string // the marina's reference at the channel (from the credentials)
	Categories []ChannelCategory
}

// ChannelRates is a push of prices for a reference vessel size.
type ChannelRates struct {
	MarinaRef  string
	Currency   string // ISO 4217
	RefLOACm   int64
	RefBeamCm  int64
	Categories []ChannelCategory
}

// Reservation kinds.
const (
	ChannelReservationNew    = "new"
	ChannelReservationModify = "modify"
	ChannelReservationCancel = "cancel"
)

// ChannelGuest is the guest of a reservation. Every field is personal data: never logged, never in events.
type ChannelGuest struct {
	FirstName string
	LastName  string
	Email     string
	Phone     string
	Country   string // ISO 3166-1 alpha-2
	Locale    string
}

// ChannelReservation is one reservation event received from a channel. EventID identifies the event (applied
// once), ExternalRef the reservation (stable across its events).
type ChannelReservation struct {
	EventID       string
	ExternalRef   string
	Kind          string // new, modify, cancel
	MarinaRef     string
	CategoryCode  string // external category code
	Arrival       string // YYYY-MM-DD
	Departure     string // YYYY-MM-DD, exclusive
	LOACm         int64
	BeamCm        int64
	DraftCm       int64 // 0 = unknown
	HullType      string
	Guest         ChannelGuest
	AmountCents   int64
	Currency      string
	PaidToChannel bool // the channel collected the payment (merchant of record)
	ReceivedAt    time.Time
}

// Acknowledgement outcomes and reject reasons.
const (
	ChannelAckAccepted = "accepted"
	ChannelAckRejected = "rejected"

	ChannelRejectNoSpace           = "no_space"
	ChannelRejectRules             = "rules"
	ChannelRejectUnmappedCategory  = "unmapped_category"
	ChannelRejectModifyUnsupported = "modify_unsupported"
	ChannelRejectInvalid           = "invalid"
)

// ChannelAck is the outcome of one reservation sent back to the channel.
type ChannelAck struct {
	ExternalRef string
	EventID     string
	Outcome     string // accepted, rejected
	Reference   string // Porto booking reference when accepted
	Reason      string // reject reason when rejected
}

type channelCredentialsKey struct{}

// WithChannelCredentials returns a context carrying the decrypted credentials of a channel connection for one call.
func WithChannelCredentials(ctx context.Context, creds map[string]string) context.Context {
	return context.WithValue(ctx, channelCredentialsKey{}, creds)
}

// ChannelCredentials returns the credentials put in ctx by WithChannelCredentials (nil when absent).
func ChannelCredentials(ctx context.Context) map[string]string {
	creds, _ := ctx.Value(channelCredentialsKey{}).(map[string]string)
	return creds
}
