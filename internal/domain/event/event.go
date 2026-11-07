// Package event holds Porto's domain events: the catalogue of types, the transactional outbox, the dispatcher
// that moves events to sinks and to the live bus, the in-memory broker, and the anti-SSRF egress client and
// signature scheme used for outgoing webhooks.
package event

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// M0 event types (version 1).
const (
	OrganizationUpdated = "organization.updated"
	MarinaCreated       = "marina.created"
	MarinaUpdated       = "marina.updated"
	MarinaArchived      = "marina.archived"
	MemberAdded         = "member.added"
	MemberRoleChanged   = "member.role_changed"
	MemberRemoved       = "member.removed"
	APIKeyCreated       = "api_key.created"
	APIKeyRevoked       = "api_key.revoked"
	DevicePaired        = "device.paired"
	DeviceRevoked       = "device.revoked"
	WebhookTest         = "webhook.test"
)

// M1 event types (version 1, m1-plan §4.6). Only config.map_changed, customer.created and customer.anonymized
// are public (spec 27.3); the others feed SSE and cache invalidation only.
const (
	ConfigMapChanged   = "config.map_changed"
	CustomerCreated    = "customer.created"
	CustomerAnonymized = "customer.anonymized"
	CustomerUpdated    = "customer.updated"
	CustomerMerged     = "customer.merged"
	VesselChanged      = "vessel.changed"
	DocumentChanged    = "document.changed"
	DeadlineChanged    = "deadline.changed"
)

// M2 event types (version 1, m2-plan §4.7). config.pricing_changed is public (spec 27.3).
const (
	ConfigPricingChanged = "config.pricing_changed"
)

// M3 event types (version 1, m3-plan §7.5). Data carry ids, enums, dates and counts only (INV-70).
const (
	OccupancyAllocated    = "occupancy.allocated"
	OccupancyMoved        = "occupancy.moved"
	OccupancyReleased     = "occupancy.released"
	OccupancyHoldExpired  = "occupancy.hold_expired"
	ReleaseCreated        = "release.created"
	ReleaseResold         = "release.resold"
	PresenceArrived       = "presence.arrived"
	PresenceDeparted      = "presence.departed"
	QuoteSent             = "quote.sent"
	QuoteAccepted         = "quote.accepted"
	QuoteExpired          = "quote.expired"
	ContractCreated       = "contract.created"
	ContractActivated     = "contract.activated"
	ContractTerminated    = "contract.terminated"
	TransitHeld           = "transit.held"
	TransitConfirmed      = "transit.confirmed"
	TransitCancelled      = "transit.cancelled"
	TransitArrived        = "transit.arrived"
	TransitDeparted       = "transit.departed"
	QuoteChanged          = "quote.changed"
	ContractChanged       = "contract.changed"
	TransitChanged        = "transit.changed"
	InquiryChanged        = "inquiry.changed"
	ConfigOccupancyChange = "config.occupancy_changed"
)

// SSE topics.
const (
	TopicMarina = "marina.changed"
	TopicTeam   = "team.changed"
	// TopicMap carries harbour map, berth group and harbour settings changes (M1).
	TopicMap = "map.changed"
	// TopicRegistry carries customer, vessel, document and deadline changes (M1).
	TopicRegistry = "registry.changed"
	// TopicPricing carries pricing settings, revenue class, item, berth category and price list changes (M2).
	TopicPricing = "pricing.changed"
	// TopicOccupancy carries allocations, moves, releases, holds, blocks, closures and occupancy settings (M3).
	TopicOccupancy = "occupancy.changed"
	// TopicPresence carries arrivals and departures (M3).
	TopicPresence = "presence.changed"
	// TopicContract carries quotes, contracts, transits and inquiries (M3).
	TopicContract = "contract.changed"
)

// TypeInfo describes an event type. Public types can be subscribed by webhook endpoints; Topic is the SSE event
// name ("" = not published to SSE).
type TypeInfo struct {
	Type   string `json:"type"`
	Topic  string `json:"topic"`
	Public bool   `json:"public"`
}

var catalogue = []TypeInfo{
	{OrganizationUpdated, TopicMarina, true},
	{MarinaCreated, TopicMarina, true},
	{MarinaUpdated, TopicMarina, true},
	{MarinaArchived, TopicMarina, true},
	{MemberAdded, TopicTeam, true},
	{MemberRoleChanged, TopicTeam, true},
	{MemberRemoved, TopicTeam, true},
	{APIKeyCreated, TopicTeam, true},
	{APIKeyRevoked, TopicTeam, true},
	{DevicePaired, TopicTeam, true},
	{DeviceRevoked, TopicTeam, true},
	{WebhookTest, "", false},
	{ConfigMapChanged, TopicMap, true},
	{CustomerCreated, TopicRegistry, true},
	{CustomerAnonymized, TopicRegistry, true},
	{CustomerUpdated, TopicRegistry, false},
	{CustomerMerged, TopicRegistry, false},
	{VesselChanged, TopicRegistry, false},
	{DocumentChanged, TopicRegistry, false},
	{DeadlineChanged, TopicRegistry, false},
	{ConfigPricingChanged, TopicPricing, true},
	{OccupancyAllocated, TopicOccupancy, true},
	{OccupancyMoved, TopicOccupancy, true},
	{OccupancyReleased, TopicOccupancy, true},
	{OccupancyHoldExpired, TopicOccupancy, true},
	{ReleaseCreated, TopicOccupancy, true},
	{ReleaseResold, TopicOccupancy, true},
	{PresenceArrived, TopicPresence, true},
	{PresenceDeparted, TopicPresence, true},
	{QuoteSent, TopicContract, true},
	{QuoteAccepted, TopicContract, true},
	{QuoteExpired, TopicContract, true},
	{ContractCreated, TopicContract, true},
	{ContractActivated, TopicContract, true},
	{ContractTerminated, TopicContract, true},
	{TransitHeld, TopicContract, true},
	{TransitConfirmed, TopicContract, true},
	{TransitCancelled, TopicContract, true},
	{TransitArrived, TopicContract, true},
	{TransitDeparted, TopicContract, true},
	{QuoteChanged, TopicContract, false},
	{ContractChanged, TopicContract, false},
	{TransitChanged, TopicContract, false},
	{InquiryChanged, TopicContract, false},
	{ConfigOccupancyChange, TopicOccupancy, false},
}

// Types returns the catalogue of event types.
func Types() []TypeInfo { return slices.Clone(catalogue) }

// Lookup returns the description of a type.
func Lookup(typ string) (TypeInfo, bool) {
	for _, t := range catalogue {
		if t.Type == typ {
			return t, true
		}
	}
	return TypeInfo{}, false
}

// IsPublic reports whether webhooks may subscribe to typ.
func IsPublic(typ string) bool {
	t, ok := Lookup(typ)
	return ok && t.Public
}

// TopicOf returns the SSE topic of a type ("" when not published).
func TopicOf(typ string) string {
	t, _ := Lookup(typ)
	return t.Topic
}

// EnvelopeIDPrefix prefixes outbox ids in public envelopes.
const EnvelopeIDPrefix = "evt_"

// Envelope is the public JSON form of an event (webhook body).
type Envelope struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	MarinaID   string          `json:"marina_id,omitempty"`
	OccurredAt string          `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

// EnvelopeOf builds the public envelope of ev.
func EnvelopeOf(ev ext.Event) Envelope {
	data := ev.Data
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return Envelope{
		ID:         EnvelopeIDPrefix + ev.ID,
		Type:       ev.Type,
		Version:    ev.Version,
		MarinaID:   ev.MarinaID,
		OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339),
		Data:       data,
	}
}

// FromRow converts an outbox row to an ext.Event.
func FromRow(r *store.OutboxEvent) ext.Event {
	return ext.Event{
		ID:             r.ID,
		Type:           r.Type,
		Version:        r.Version,
		OrganizationID: r.OrganizationID,
		MarinaID:       r.MarinaID,
		OccurredAt:     r.OccurredAt.UTC(),
		Data:           json.RawMessage(r.Payload.Compact()),
	}
}

// Outbox writes events in the caller's transaction.
type Outbox struct {
	clock clock.Clock
	wake  chan struct{}
}

// NewOutbox returns an Outbox.
func NewOutbox(c clock.Clock) *Outbox {
	return &Outbox{clock: c, wake: make(chan struct{}, 1)}
}

// Wake is signalled after each commit that wrote an event (the dispatch job listens on it).
func (o *Outbox) Wake() <-chan struct{} { return o.wake }

func (o *Outbox) kick() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// Write stores an event of a marina in tx. data must hold ids and enums only. ctx must be the context RunInTx
// passed to the callback, so that the dispatcher is woken after commit.
func (o *Outbox) Write(ctx context.Context, tx bun.IDB, marina *store.MarinaScope, typ string, data any) (ext.Event, error) {
	var orgID, marinaID string
	if marina != nil {
		orgID, marinaID = marina.OrgID(), marina.ID()
	}
	return o.write(ctx, tx, orgID, marinaID, typ, data)
}

// WriteOrg stores an organization-level event (no marina) in tx.
func (o *Outbox) WriteOrg(ctx context.Context, tx bun.IDB, org store.OrgScope, typ string, data any) (ext.Event, error) {
	return o.write(ctx, tx, org.ID(), "", typ, data)
}

func (o *Outbox) write(ctx context.Context, tx bun.IDB, orgID, marinaID, typ string, data any) (ext.Event, error) {
	if _, ok := Lookup(typ); !ok {
		return ext.Event{}, fmt.Errorf("event: unknown type %q", typ)
	}
	if data == nil {
		data = struct{}{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return ext.Event{}, fmt.Errorf("event: marshal %s: %w", typ, err)
	}
	now := store.UTC(o.clock.Now())
	row := &store.OutboxEvent{
		ID:             store.NewID(),
		OrganizationID: orgID,
		MarinaID:       marinaID,
		Type:           typ,
		Version:        1,
		Payload:        payload,
		OccurredAt:     now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.InsertOutboxEvent(ctx, tx, row); err != nil {
		return ext.Event{}, fmt.Errorf("event: write %s: %w", typ, err)
	}
	store.AfterCommit(ctx, o.kick)
	return FromRow(row), nil
}

// Backoff returns the delay before retry number failedAttempts (1-based): 30 s · 2^(n−1), capped at 6 h.
func Backoff(failedAttempts int) time.Duration {
	const (
		base    = 30 * time.Second
		ceiling = 6 * time.Hour
	)
	if failedAttempts < 1 {
		failedAttempts = 1
	}
	d := base
	for i := 1; i < failedAttempts; i++ {
		d *= 2
		if d >= ceiling {
			return ceiling
		}
	}
	return d
}
