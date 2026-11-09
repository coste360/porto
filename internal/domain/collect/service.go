// Package collect collects money (spec 16.2, 16.4, 28.1; m5-plan §5.4, §5.5): payment intents through the
// payment drivers, provider events (signed webhooks, deduplicated, INV-56) and reconciliation, payment requests
// with the public pay page, SEPA mandates and collections, reminders with levels and exclusions, and the arrears
// policy. It works fully offline with the manual driver alone; no provider call ever runs inside a database
// transaction (intents are written first, the provider is called after commit).
package collect

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Keys      *crypto.KeyRing
	Payments  *payment.Service
	Charges   *charge.Service
	Contracts *contract.Service
	Drivers   *paydriver.Registry
	Docs      *docs.Service
	Notify    *notify.Service
	BaseURL   string
	Log       *slog.Logger
}

// Service is the collection service.
type Service struct {
	d Deps
	// rental receives the provider events of rental deposit pre-authorisations (M11, SetRentalDepositHooks).
	rental RentalDepositHooks
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	d.BaseURL = strings.TrimRight(d.BaseURL, "/")
	return &Service{d: d}
}

var (
	_ charge.PaymentRequester  = (*Service)(nil)
	_ payment.ProviderRefunder = (*Service)(nil)
)

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			return l
		}
	}
	return time.UTC
}

func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return occupancy.Today(s.d.Clock, s.location(ctx, db, scope))
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, data any) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// jobActor and providerActor are the audit actors of jobs and webhooks (system:<job>, provider:<code>).
func jobActor(name string) audit.Actor {
	return audit.Actor{Type: audit.ActorSystem, ID: "system:" + name}
}

func providerActor(code string) audit.Actor {
	return audit.Actor{Type: audit.ActorSystem, ID: "provider:" + code}
}

func userOf(a audit.Actor) string {
	if a.Type == audit.ActorUser {
		return a.ID
	}
	return ""
}

func actorString(a audit.Actor) string {
	t := a.Type
	if t == "" {
		t = audit.ActorSystem
	}
	if a.ID == "" {
		return t
	}
	if t == audit.ActorSystem {
		return a.ID
	}
	return t + ":" + a.ID
}

// customerContact returns the first email, the locale and the display name of a customer ("" email when none).
func customerContact(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID string) (email, locale, name string, err error) {
	cs, err := store.CustomersByIDs(ctx, db, scope, []string{customerID})
	if err != nil {
		return "", "", "", err
	}
	if len(cs) == 0 {
		return "", "", "", store.ErrNotFound
	}
	c := &cs[0]
	name = strings.TrimSpace(c.FirstName + " " + c.LastName)
	if c.Kind == "company" && c.CompanyName != "" {
		name = c.CompanyName
	}
	var emails []struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(c.Emails, &emails)
	for _, e := range emails {
		if v := strings.TrimSpace(e.Value); v != "" {
			return v, c.Locale, name, nil
		}
	}
	return "", c.Locale, name, nil
}

func euro(c int64) string {
	v := strings.Replace(moneyString(c), ".", ",", 1)
	return v + " €"
}

func italianDate(d calendar.Date) string {
	s := d.String()
	if len(s) != 10 {
		return s
	}
	return s[8:10] + "/" + s[5:7] + "/" + s[0:4]
}

// CustomerNames returns the display names of customers of the marina for a principal with customers.read (an
// empty map otherwise); used by the views of requests, reminders, exclusions and counter sales.
func (s *Service) CustomerNames(ctx context.Context, p *auth.Principal, marinaID string, ids []string) map[string]string {
	out := map[string]string{}
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return out
	}
	var want []string
	for _, id := range ids {
		if store.ValidID(id) {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return out
	}
	cs, err := store.CustomersByIDs(ctx, s.d.DB.DB, scope, uniq(want))
	if err != nil {
		return out
	}
	for i := range cs {
		out[cs[i].ID] = customer.DisplayName(&cs[i]) // same name as everywhere else, empty once anonymised
	}
	return out
}

// RecordIgnoredEvent records a verified provider event that Porto does not use (dedupe and audit trail of the
// webhook; nothing else changes).
func (s *Service) RecordIgnoredEvent(ctx context.Context, providerCode, marinaID string, ev *ext.PaymentEvent) error {
	if ev == nil || ev.EventID == "" {
		return nil
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return ErrNotFound
	}
	now := s.now()
	_, err = store.InsertProviderEvent(ctx, s.d.DB.DB, &store.PaymentProviderEvent{
		ID: store.NewID(), MarinaID: scope.ID(), ProviderCode: providerCode, EventID: limit(ev.EventID, 200),
		Status: EventIgnored, ReceivedAt: now, CreatedAt: now,
	})
	return err
}
