// Package ext holds the public extension interfaces of Porto (spec 27.5). M0 ships the subset needed by the
// event pipeline and the HTTP router; the other interfaces arrive with their milestones.
//
// The package must not import anything under internal/: extensions are compiled against it from outside.
package ext

import (
	"context"
	"encoding/json"
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

// Extensions is what an embedding binary passes to the application.
type Extensions struct {
	EventSinks     []EventSink
	EventBus       EventBus
	AuthProviders  []AuthProvider
	TenantResolver TenantResolver
	Notifiers      []Notifier
	BotProtection  BotProtection
	// HTTPRoutes mounts extra routes on the root router.
	HTTPRoutes func(r chi.Router)
}
