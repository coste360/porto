package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// FanOutSink is the ext.EventSink that creates one pending delivery per subscribed active endpoint. It is
// idempotent: a retried event finds its deliveries already there (unique endpoint/event).
type FanOutSink struct {
	db    *store.DB
	clock clock.Clock
	svc   *Service
}

// NewFanOutSink returns the sink. svc is woken when deliveries were created.
func NewFanOutSink(db *store.DB, c clock.Clock, svc *Service) *FanOutSink {
	return &FanOutSink{db: db, clock: c, svc: svc}
}

// Name implements ext.EventSink.
func (f *FanOutSink) Name() string { return "webhooks" }

func subscribed(events []string, typ string) bool {
	return slices.Contains(events, allEvents) || slices.Contains(events, typ)
}

// Handle implements ext.EventSink. Marina events go to the marina's endpoints; organization events (no marina)
// go to the endpoints of every active marina of the organization.
func (f *FanOutSink) Handle(ctx context.Context, ev ext.Event) error {
	if !event.IsPublic(ev.Type) {
		return nil
	}
	return f.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var scopes []store.MarinaScope
		switch {
		case ev.MarinaID != "":
			s, err := store.SystemMarinaScope(ctx, tx, ev.MarinaID)
			if err != nil {
				return nil // marina gone: nothing to deliver
			}
			scopes = append(scopes, s)
		case ev.OrganizationID != "":
			org, err := store.ResolveOrgScope(ctx, tx, ev.OrganizationID)
			if err != nil {
				return nil
			}
			marinas, err := store.ListMarinas(ctx, tx, org, nil)
			if err != nil {
				return err
			}
			for _, m := range marinas {
				s, err := store.ResolveMarinaScope(ctx, tx, m.ID)
				if err == nil {
					scopes = append(scopes, s)
				}
			}
		}
		now := store.UTC(f.clock.Now())
		created := false
		for _, s := range scopes {
			endpoints, err := store.ActiveWebhookEndpoints(ctx, tx, s)
			if err != nil {
				return err
			}
			for _, ep := range endpoints {
				if !subscribed(ep.Events, ev.Type) {
					continue
				}
				ok, err := store.InsertWebhookDelivery(ctx, tx, &store.WebhookDelivery{
					ID: store.NewID(), MarinaID: s.ID(), EndpointID: ep.ID, EventID: ev.ID, EventType: ev.Type,
					Status: store.DeliveryPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
				})
				if err != nil {
					return err
				}
				created = created || ok
			}
		}
		if created && f.svc != nil {
			store.AfterCommit(ctx, f.svc.kick)
		}
		return nil
	})
}

// Deliverer sends due deliveries.
type Deliverer struct {
	db      *store.DB
	clock   clock.Clock
	keys    *crypto.KeyRing
	egress  *event.Egress
	audit   *audit.Writer
	log     *slog.Logger
	version string
}

// NewDeliverer returns a Deliverer. version goes into the User-Agent.
func NewDeliverer(db *store.DB, c clock.Clock, keys *crypto.KeyRing, egress *event.Egress, aw *audit.Writer, version string, log *slog.Logger) *Deliverer {
	return &Deliverer{db: db, clock: c, keys: keys, egress: egress, audit: aw, log: log, version: version}
}

// DeliverDue claims up to limit due deliveries, sends them and records the outcome. It returns how many were
// delivered.
func (d *Deliverer) DeliverDue(ctx context.Context, limit int) (int, error) {
	now := store.UTC(d.clock.Now())
	rows, err := store.ClaimWebhookDeliveries(ctx, d.db.DB, now, deliveryLease, limit)
	if err != nil {
		return 0, fmt.Errorf("webhook: claim: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	var endpointIDs, eventIDs []string
	for _, r := range rows {
		endpointIDs = append(endpointIDs, r.EndpointID)
		eventIDs = append(eventIDs, r.EventID)
	}
	endpoints, err := store.WebhookEndpointsByIDs(ctx, d.db.DB, endpointIDs)
	if err != nil {
		return 0, err
	}
	events, err := store.OutboxEventsByIDs(ctx, d.db.DB, eventIDs)
	if err != nil {
		return 0, err
	}
	epByID := map[string]*store.WebhookEndpoint{}
	for i := range endpoints {
		epByID[endpoints[i].ID] = &endpoints[i]
	}
	evByID := map[string]*store.OutboxEvent{}
	for i := range events {
		evByID[events[i].ID] = &events[i]
	}
	delivered := 0
	for i := range rows {
		ok, err := d.deliverOne(ctx, &rows[i], epByID[rows[i].EndpointID], evByID[rows[i].EventID])
		if err != nil {
			return delivered, err
		}
		if ok {
			delivered++
		}
	}
	return delivered, nil
}

// outcome of one attempt
type attempt struct {
	status  int
	excerpt string
	err     string
	final   bool // permanent failure: do not retry
}

func (d *Deliverer) deliverOne(ctx context.Context, row *store.WebhookDelivery, ep *store.WebhookEndpoint, ev *store.OutboxEvent) (bool, error) {
	now := store.UTC(d.clock.Now())
	if row.FirstAttemptAt.IsZero() {
		row.FirstAttemptAt = now
	}
	var a attempt
	switch {
	case ep == nil || ev == nil:
		a = attempt{err: "endpoint or event no longer exists", final: true}
	case !ep.Active:
		a = attempt{err: "endpoint disabled", final: true}
	default:
		a = d.send(ctx, row, ep, ev)
	}
	row.Attempts++
	row.LastStatusCode = a.status
	row.ResponseExcerpt = a.excerpt
	row.UpdatedAt = store.UTC(d.clock.Now())
	success := a.err == "" && a.status >= 200 && a.status < 300
	switch {
	case success:
		row.Status = store.DeliveryDelivered
		row.DeliveredAt = row.UpdatedAt
		row.LastError = ""
	case a.final || row.UpdatedAt.Sub(row.FirstAttemptAt) >= GiveUpAfter:
		row.Status = store.DeliveryFailed
		row.FailedAt = row.UpdatedAt
		row.LastError = failureText(a)
	default:
		row.LastError = failureText(a)
		row.NextAttemptAt = store.UTC(row.UpdatedAt.Add(event.Backoff(row.Attempts)))
	}
	err := d.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.SaveWebhookAttempt(ctx, tx, row); err != nil {
			return err
		}
		if ep == nil {
			return nil
		}
		if success {
			return store.RecordWebhookSuccess(ctx, tx, ep.ID, row.UpdatedAt)
		}
		if !ep.Active {
			return nil
		}
		n, err := store.RecordWebhookFailure(ctx, tx, ep.ID, row.UpdatedAt)
		if err != nil {
			return err
		}
		if n < DisableAfterFailures {
			return nil
		}
		disabled, err := store.DisableWebhookEndpoint(ctx, tx, ep.ID, "consecutive_failures", row.UpdatedAt)
		if err != nil || !disabled {
			return err
		}
		ep.Active = false
		return d.audit.Write(ctx, tx, audit.Entry{
			MarinaID: ep.MarinaID, Actor: audit.System, Action: "webhook.disable", EntityType: "webhook_endpoint",
			EntityID: ep.ID, Diff: map[string]any{"reason": "consecutive_failures", "failures": n},
		})
	})
	if err != nil {
		return false, fmt.Errorf("webhook: save attempt: %w", err)
	}
	if !success {
		d.log.InfoContext(ctx, "webhook delivery failed", "delivery_id", row.ID, "endpoint_id", row.EndpointID,
			"status_code", a.status, "attempts", row.Attempts, "final", row.Status == store.DeliveryFailed)
	}
	return success, nil
}

func failureText(a attempt) string {
	if a.err != "" {
		return a.err
	}
	return fmt.Sprintf("HTTP %d", a.status)
}

func (d *Deliverer) send(ctx context.Context, row *store.WebhookDelivery, ep *store.WebhookEndpoint, ev *store.OutboxEvent) attempt {
	body, err := json.Marshal(event.EnvelopeOf(event.FromRow(ev)))
	if err != nil {
		return attempt{err: "cannot encode event", final: true}
	}
	if len(body) > MaxPayloadBytes {
		return attempt{err: "payload too large", final: true}
	}
	secrets, err := d.secrets(ep)
	if err != nil {
		return attempt{err: "cannot decrypt endpoint secret", final: true}
	}
	headers := map[string]string{
		"Porto-Signature": event.SignAll(secrets, body, d.clock.Now()),
		"Porto-Event":     ev.Type,
		"Porto-Delivery":  row.ID,
		"User-Agent":      "Porto-Webhooks/" + d.version,
	}
	ctx, cancel := context.WithTimeout(ctx, DeliveryTimeout)
	defer cancel()
	res, err := d.egress.PostJSON(ctx, ep.URL, headers, body, ResponseExcerptBytes)
	if err != nil {
		return attempt{err: event.Sanitize([]byte(err.Error()), 300)}
	}
	return attempt{status: res.Status, excerpt: res.Excerpt}
}

// secrets returns the current secret and, during rotation, the previous one.
func (d *Deliverer) secrets(ep *store.WebhookEndpoint) ([]string, error) {
	cur, err := d.keys.DecryptString(ep.SecretEnc, secretAAD(ep.ID))
	if err != nil {
		return nil, err
	}
	out := []string{cur}
	if ep.PreviousSecretEnc != "" && d.clock.Now().Before(ep.PreviousSecretExpiresAt) {
		prev, err := d.keys.DecryptString(ep.PreviousSecretEnc, previousSecretAAD(ep.ID))
		if err == nil {
			out = append(out, prev)
		}
	}
	return out, nil
}
