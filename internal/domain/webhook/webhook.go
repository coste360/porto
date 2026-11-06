// Package webhook manages outgoing webhooks: endpoints with signing secrets (rotation keeps the previous secret
// valid for 24 h), the fan-out sink that turns outbox events into deliveries, and the deliverer that sends them
// with HMAC signatures, exponential backoff and the anti-SSRF egress client.
package webhook

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Errors.
var (
	ErrInvalidURL       = errors.New("webhook: invalid endpoint URL")
	ErrUnknownEventType = errors.New("webhook: unknown event type")
	ErrEndpointDisabled = errors.New("webhook: endpoint disabled")
	ErrVersionMismatch  = errors.New("webhook: version mismatch")
)

// Policy constants.
const (
	SecretPrefix          = "whsec_"
	PreviousSecretTTL     = 24 * time.Hour
	MaxPayloadBytes       = 256 << 10
	DeliveryTimeout       = 10 * time.Second
	GiveUpAfter           = 24 * time.Hour
	DisableAfterFailures  = 20
	ResponseExcerptBytes  = 1 << 10
	Retention             = 30 * 24 * time.Hour
	deliveryLease         = time.Minute
	maxEndpointsPerMarina = 20
	allEvents             = "*"
)

// Endpoint is the public view of an endpoint (never the secrets).
type Endpoint struct {
	ID                  string
	MarinaID            string
	URL                 string
	Description         string
	Events              []string
	Active              bool
	ConsecutiveFailures int
	DisabledAt          time.Time
	DisabledReason      string
	HasPreviousSecret   bool
	Version             int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Delivery is the public view of a delivery.
type Delivery = store.WebhookDelivery

// CreateInput describes a new endpoint.
type CreateInput struct {
	URL, Description string
	Events           []string
}

// UpdateInput lists the fields to change (nil = unchanged). Setting Active to true re-enables a disabled
// endpoint and resets its failure counter.
type UpdateInput struct {
	URL         *string
	Description *string
	Events      *[]string
	Active      *bool
}

// Service manages webhook endpoints.
type Service struct {
	db     *store.DB
	clock  clock.Clock
	keys   *crypto.KeyRing
	audit  *audit.Writer
	outbox *event.Outbox
	policy event.EgressPolicy
	log    *slog.Logger
	wake   chan struct{}
}

// NewService returns the webhook service. policy validates endpoint URLs.
func NewService(db *store.DB, c clock.Clock, keys *crypto.KeyRing, aw *audit.Writer, ob *event.Outbox, policy event.EgressPolicy, log *slog.Logger) *Service {
	return &Service{db: db, clock: c, keys: keys, audit: aw, outbox: ob, policy: policy, log: log, wake: make(chan struct{}, 1)}
}

// Wake is signalled when new deliveries are due (test sends, redeliveries, fan-out).
func (s *Service) Wake() <-chan struct{} { return s.wake }

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) now() time.Time { return store.UTC(s.clock.Now()) }

func toEndpoint(e *store.WebhookEndpoint, now time.Time) Endpoint {
	return Endpoint{
		ID: e.ID, MarinaID: e.MarinaID, URL: e.URL, Description: e.Description, Events: slices.Clone([]string(e.Events)),
		Active: e.Active, ConsecutiveFailures: e.ConsecutiveFailures, DisabledAt: e.DisabledAt,
		DisabledReason: e.DisabledReason, Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		HasPreviousSecret: e.PreviousSecretEnc != "" && now.Before(e.PreviousSecretExpiresAt),
	}
}

func secretAAD(endpointID string) string         { return "webhook:" + endpointID }
func previousSecretAAD(endpointID string) string { return "webhook:" + endpointID + ":previous" }

func newSecret() (string, error) {
	t, err := crypto.RandomToken(32)
	if err != nil {
		return "", err
	}
	return SecretPrefix + t, nil
}

func (s *Service) validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2000 {
		return "", ErrInvalidURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidURL
	}
	if err := s.policy.CheckURL(u); err != nil {
		return "", ErrInvalidURL
	}
	return u.String(), nil
}

func validateEvents(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, ErrUnknownEventType
	}
	var out []string
	for _, t := range in {
		if t != allEvents && !event.IsPublic(t) {
			return nil, ErrUnknownEventType
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if slices.Contains(out, allEvents) {
		return []string{allEvents}, nil
	}
	slices.Sort(out)
	return out, nil
}

func validateDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > 500 {
		return "", &auth.ValidationError{Fields: []auth.FieldError{{Field: "description", Code: "too_long"}}}
	}
	return d, nil
}

// List returns the endpoints of a marina (users.manage).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string) ([]Endpoint, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListWebhookEndpoints(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Endpoint, 0, len(rows))
	for i := range rows {
		out = append(out, toEndpoint(&rows[i], now))
	}
	return out, nil
}

// Get returns one endpoint.
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, endpointID string) (*Endpoint, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	row, err := store.GetWebhookEndpoint(ctx, s.db.DB, scope, endpointID)
	if err != nil {
		return nil, notFound(err)
	}
	ep := toEndpoint(row, s.now())
	return &ep, nil
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// Create adds an endpoint and returns its signing secret (shown once).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in CreateInput) (string, *Endpoint, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return "", nil, err
	}
	u, err := s.validateURL(in.URL)
	if err != nil {
		return "", nil, err
	}
	events, err := validateEvents(in.Events)
	if err != nil {
		return "", nil, err
	}
	desc, err := validateDescription(in.Description)
	if err != nil {
		return "", nil, err
	}
	secret, err := newSecret()
	if err != nil {
		return "", nil, err
	}
	var out *Endpoint
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		existing, err := store.ListWebhookEndpoints(ctx, tx, scope)
		if err != nil {
			return err
		}
		if len(existing) >= maxEndpointsPerMarina {
			return &auth.ValidationError{Fields: []auth.FieldError{{Field: "url", Code: "too_many_endpoints"}}}
		}
		now := s.now()
		id := store.NewID()
		enc, err := s.keys.EncryptString(secret, secretAAD(id))
		if err != nil {
			return err
		}
		row := &store.WebhookEndpoint{
			ID: id, MarinaID: scope.ID(), URL: u, Description: desc, SecretEnc: enc, Events: events, Active: true,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertWebhookEndpoint(ctx, tx, row); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.create",
			EntityType: "webhook_endpoint", EntityID: id, Diff: map[string]any{"url": u, "events": events},
		}); err != nil {
			return err
		}
		ep := toEndpoint(row, now)
		out = &ep
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return secret, out, nil
}

// Update changes an endpoint when its version equals ifMatch.
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, endpointID string, ifMatch int, in UpdateInput) (*Endpoint, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	var out *Endpoint
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetWebhookEndpoint(ctx, tx, scope, endpointID)
		if err != nil {
			return notFound(err)
		}
		var changed []string
		if in.URL != nil {
			u, err := s.validateURL(*in.URL)
			if err != nil {
				return err
			}
			row.URL = u
			changed = append(changed, "url")
		}
		if in.Description != nil {
			d, err := validateDescription(*in.Description)
			if err != nil {
				return err
			}
			row.Description = d
			changed = append(changed, "description")
		}
		if in.Events != nil {
			ev, err := validateEvents(*in.Events)
			if err != nil {
				return err
			}
			row.Events = ev
			changed = append(changed, "events")
		}
		if in.Active != nil {
			row.Active = *in.Active
			if row.Active {
				row.ConsecutiveFailures = 0
				row.DisabledAt = time.Time{}
				row.DisabledReason = ""
			} else if row.DisabledAt.IsZero() {
				row.DisabledAt = s.now()
				row.DisabledReason = "manual"
			}
			changed = append(changed, "active")
		}
		row.UpdatedAt = s.now()
		if err := store.UpdateWebhookEndpoint(ctx, tx, scope, ifMatch, row); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return notFound(err)
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.update",
			EntityType: "webhook_endpoint", EntityID: row.ID, Diff: map[string]any{"fields": changed, "active": row.Active},
		}); err != nil {
			return err
		}
		ep := toEndpoint(row, s.now())
		out = &ep
		return nil
	})
	return out, err
}

// Delete removes an endpoint and its deliveries.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, marinaID, endpointID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.DeleteWebhookEndpoint(ctx, tx, scope, endpointID); err != nil {
			return notFound(err)
		}
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.delete",
			EntityType: "webhook_endpoint", EntityID: endpointID,
		})
	})
}

// RotateSecret replaces the signing secret; the previous one stays valid (and is sent as a second signature)
// for 24 hours.
func (s *Service) RotateSecret(ctx context.Context, p *auth.Principal, marinaID, endpointID string) (string, error) {
	secret, _, err := s.RotateSecretUntil(ctx, p, marinaID, endpointID)
	return secret, err
}

// RotateSecretUntil is RotateSecret that also returns until when the previous secret is accepted.
func (s *Service) RotateSecretUntil(ctx context.Context, p *auth.Principal, marinaID, endpointID string) (string, time.Time, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return "", time.Time{}, err
	}
	secret, err := newSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	var until time.Time
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetWebhookEndpoint(ctx, tx, scope, endpointID)
		if err != nil {
			return notFound(err)
		}
		current, err := s.keys.DecryptString(row.SecretEnc, secretAAD(row.ID))
		if err != nil {
			return err
		}
		prevEnc, err := s.keys.EncryptString(current, previousSecretAAD(row.ID))
		if err != nil {
			return err
		}
		newEnc, err := s.keys.EncryptString(secret, secretAAD(row.ID))
		if err != nil {
			return err
		}
		now := s.now()
		until = now.Add(PreviousSecretTTL)
		if err := store.RotateWebhookSecret(ctx, tx, scope, row.ID, newEnc, prevEnc, until, now); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.rotate_secret",
			EntityType: "webhook_endpoint", EntityID: row.ID,
		})
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return secret, until, nil
}

// SendTest queues a webhook.test event for one endpoint and returns the delivery id.
func (s *Service) SendTest(ctx context.Context, p *auth.Principal, marinaID, endpointID string) (string, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return "", err
	}
	var deliveryID string
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetWebhookEndpoint(ctx, tx, scope, endpointID)
		if err != nil {
			return notFound(err)
		}
		if !row.Active {
			return ErrEndpointDisabled
		}
		ev, err := s.outbox.Write(ctx, tx, &scope, event.WebhookTest, event.WebhookTestData{EndpointID: row.ID})
		if err != nil {
			return err
		}
		now := s.now()
		d := &store.WebhookDelivery{
			ID: store.NewID(), MarinaID: scope.ID(), EndpointID: row.ID, EventID: ev.ID, EventType: ev.Type,
			Status: store.DeliveryPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := store.InsertWebhookDelivery(ctx, tx, d); err != nil {
			return err
		}
		deliveryID = d.ID
		store.AfterCommit(ctx, s.kick)
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.test",
			EntityType: "webhook_endpoint", EntityID: row.ID,
		})
	})
	return deliveryID, err
}

// ListDeliveries returns the deliveries of an endpoint, newest first.
func (s *Service) ListDeliveries(ctx context.Context, p *auth.Principal, marinaID, endpointID, status, cursor string, limit int) (store.Page[Delivery], error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return store.Page[Delivery]{}, err
	}
	if status != "" && status != store.DeliveryPending && status != store.DeliveryDelivered && status != store.DeliveryFailed {
		return store.Page[Delivery]{}, &auth.ValidationError{Fields: []auth.FieldError{{Field: "status", Code: "invalid"}}}
	}
	if _, err := store.GetWebhookEndpoint(ctx, s.db.DB, scope, endpointID); err != nil {
		return store.Page[Delivery]{}, notFound(err)
	}
	page, err := store.ListWebhookDeliveries(ctx, s.db.DB, scope, endpointID, status, cursor, limit)
	if errors.Is(err, store.ErrInvalidCursor) {
		return page, &auth.ValidationError{Fields: []auth.FieldError{{Field: "cursor", Code: "invalid"}}}
	}
	return page, err
}

// Redeliver makes a delivery pending again, due now, with a fresh attempt budget.
func (s *Service) Redeliver(ctx context.Context, p *auth.Principal, marinaID, endpointID, deliveryID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d, err := store.GetWebhookDelivery(ctx, tx, scope, endpointID, deliveryID)
		if err != nil {
			return notFound(err)
		}
		if err := store.ResetWebhookDelivery(ctx, tx, scope, d.ID, s.now()); err != nil {
			return err
		}
		store.AfterCommit(ctx, s.kick)
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "webhook.redeliver",
			EntityType: "webhook_delivery", EntityID: d.ID,
		})
	})
}

// PurgeOld deletes finished deliveries older than olderThan (retention, default 30 days).
func (s *Service) PurgeOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	return store.PurgeWebhookDeliveries(ctx, s.db.DB, s.now().Add(-olderThan))
}
