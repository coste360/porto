// Package ext holds the public extension interfaces of Porto (spec 27.5). M0 ships the subset needed by the
// event pipeline and the HTTP router; the other interfaces arrive with their milestones.
//
// The package must not import anything under internal/: extensions are compiled against it from outside.
package ext

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Event is a domain event as it leaves the outbox. ID is the outbox row id (a UUIDv7, so ids sort by creation);
// the public envelope prefixes it with "evt_". Data holds ids and enums only, never personal data.
type Event struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Version        int             `json:"version"`
	OrganizationID string          `json:"organization_id,omitempty"`
	MarinaID       string          `json:"marina_id,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Data           json.RawMessage `json:"data"`
}

// EventSink receives every event dispatched from the outbox, at least once. Handle must be idempotent: a failing
// sink makes the dispatcher retry the event later, for every sink.
type EventSink interface {
	Name() string
	Handle(ctx context.Context, ev Event) error
}

// EventBus fans events out to live subscribers (SSE). Delivery is best effort.
type EventBus interface {
	Publish(ctx context.Context, ev Event) error
	// Subscribe returns a channel of the events of one marina and a function that ends the subscription.
	Subscribe(ctx context.Context, marinaID string) (<-chan Event, func(), error)
}

// AuthProvider is an external identity provider (SSO). Not used in M0.
type AuthProvider interface {
	Name() string
}

// TenantResolver maps a request host to a marina slug (custom domains for the public pages).
type TenantResolver interface {
	ResolveSlug(ctx context.Context, host string) (slug string, ok bool)
}

// Notification is a message for a channel Porto does not ship itself (SMS, push). Data holds template
// variables; To is the channel address and must never be logged.
type Notification struct {
	Channel, Template, Locale, To string
	Data                          map[string]string
}

// Notifier delivers notifications on an extra channel. Not used in M0 (email is built in).
type Notifier interface {
	Name() string
	Channel() string
	Notify(ctx context.Context, n Notification) error
}

// BotProtection verifies the challenge token of a public form (captcha). Not used in M0.
type BotProtection interface {
	Verify(ctx context.Context, token, remoteIP string) (ok bool, err error)
}

// SignatureProvider is an external electronic signature service (spec 15.4). Porto ships none: the InfoCert driver
// is an extension whose contract and API are still to be agreed (spec 33 D5). Outbound calls must go through the
// egress client the host passes in the context (see WithProviderCredentials for the marina's credentials).
type SignatureProvider interface {
	Code() string
	// Request starts a signature request for one or more signers on a PDF.
	Request(ctx context.Context, req SignatureRequest) (*SignatureEnvelope, error)
	Status(ctx context.Context, envelopeRef string) (*SignatureEnvelope, error)
	// Download returns the signed PDF and the provider's evidence (audit trail), if any.
	Download(ctx context.Context, envelopeRef string) (*SignedDocument, error)
	Cancel(ctx context.Context, envelopeRef string) error
	// ParseWebhook verifies a provider callback with the marina's webhook secret and decodes it.
	ParseWebhook(r *http.Request, secret []byte) (*SignatureEvent, error)
}

// Signer is one signer of a SignatureRequest. Name, Email and Phone are personal data: providers must not log them.
type Signer struct {
	Seq    int
	Role   string // customer, co_holder, outgoing_holder, incoming_holder, legal_representative, marina
	Name   string
	Email  string
	Phone  string
	Locale string
}

// SignatureRequest is what Porto asks a provider to sign.
type SignatureRequest struct {
	MarinaID       string
	RequestID      string
	Document       []byte // the PDF/A to sign
	DocumentSHA256 string
	Title          string
	Signers        []Signer
	Sequential     bool
	ExpiresAt      time.Time
	CallbackURL    string
}

// Envelope statuses reported by providers.
const (
	EnvelopeSent            = "sent"
	EnvelopePartiallySigned = "partially_signed"
	EnvelopeSigned          = "signed"
	EnvelopeDeclined        = "declined"
	EnvelopeExpired         = "expired"
	EnvelopeCancelled       = "cancelled"
)

// SignerStatus is the state of one signer at the provider.
type SignerStatus struct {
	Seq      int
	Status   string // waiting, invited, signed, declined
	SignedAt time.Time
}

// SignatureEnvelope is the provider's view of a request.
type SignatureEnvelope struct {
	Ref     string
	Status  string
	Signers []SignerStatus
}

// SignedDocument is the signed PDF and the provider's evidence.
type SignedDocument struct {
	PDF      []byte
	Evidence []byte
}

// SignatureEvent is a decoded provider callback. SignerSeq is 0 when the event concerns the whole envelope.
type SignatureEvent struct {
	EnvelopeRef string
	Status      string
	SignerSeq   int
	OccurredAt  time.Time
}

type providerCredentialsKey struct{}

// WithProviderCredentials returns a context carrying the marina's decrypted provider credentials for one call.
// The host decrypts them from its settings only in memory; providers read them with ProviderCredentials.
func WithProviderCredentials(ctx context.Context, creds map[string]string) context.Context {
	return context.WithValue(ctx, providerCredentialsKey{}, creds)
}

// ProviderCredentials returns the credentials put in ctx by WithProviderCredentials (nil when absent).
func ProviderCredentials(ctx context.Context) map[string]string {
	creds, _ := ctx.Value(providerCredentialsKey{}).(map[string]string)
	return creds
}

// Extensions is what an embedding binary passes to the application.
type Extensions struct {
	EventSinks     []EventSink
	EventBus       EventBus
	AuthProviders  []AuthProvider
	TenantResolver TenantResolver
	Notifiers      []Notifier
	BotProtection  BotProtection
	// SignatureProviders are the registered signature drivers (M4, spec 15.4); none ships with Porto.
	SignatureProviders []SignatureProvider
	// PaymentProviders are extra payment drivers (M5, spec 16.2); manual and stripe are in the core.
	PaymentProviders []PaymentProvider
	// FiscalDrivers are fiscal device drivers (M5, spec 16.3); none and export are in the core.
	FiscalDrivers []FiscalDriver
	// ChargeExporters send charges to an external invoicing software (M5, spec 16.5); none ships with Porto.
	ChargeExporters []ChargeExporter
	// BookingChannels connect external berth marketplaces (M8, spec 23.2, 27.5); none ships with Porto (the
	// fake channel of internal/domain/channel/fakechannel is for tests and development only).
	BookingChannels []BookingChannel
	// SensorSources are polled sources of occupancy readings (M12, spec 24.3, 27.5): vendor APIs or bridges. None
	// ships with Porto (the fake source of internal/domain/sensor/fakesensor is for tests, development and demo).
	SensorSources []SensorSource
	// HTTPRoutes mounts extra routes on the root router.
	HTTPRoutes func(r chi.Router)
}
