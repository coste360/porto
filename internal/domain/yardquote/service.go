// Package yardquote is the yard catalogue (work types, qualifications, item yard settings, formula variables,
// band tables, work packages with immutable versions) and the guided yard quotes with their lifecycle (spec
// 18.1-18.3, m10-plan §3, §4). Yard quotes are not mooring quotes (D3); accepting one creates the work order through
// OrderCreator in the same transaction (INV-96). Owned by the QUOTE builder.
package yardquote

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
	Keys   *crypto.KeyRing
	Prices *pricelist.Service
	Docs   *docs.Service
	Links  *magiclink.Service
	Notify *notify.Service
	// Orders creates the work order of an accepted quote (workorder.Service, set in internal/app).
	Orders OrderCreator
}

// Service is the yard catalogue and quote service.
type Service struct {
	d Deps
}

// NewService returns the service. With documents configured it registers itself as the data source of the
// yard_quote kind.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d}
	if d.Docs != nil {
		d.Docs.RegisterDataSource(dataSource{s: s})
	}
	return s
}

// SetOrderCreator sets the work order side (internal/app wires workorder.Service after both are built).
func (s *Service) SetOrderCreator(o OrderCreator) { s.d.Orders = o }

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// location returns the marina time zone (UTC when unknown).
func location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// today is the business date in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return calendar.In(s.d.Clock.Now(), location(ctx, db, scope))
}

// endOfDay is the instant the local day d ends (the start of the next day in the marina time zone).
func endOfDay(ctx context.Context, db bun.IDB, scope store.MarinaScope, d calendar.Date) time.Time {
	loc := location(ctx, db, scope)
	n := d.AddDays(1)
	return store.UTC(time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc))
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

// writeErr maps the store errors of a catalogue write.
func writeErr(err error) error {
	switch {
	case err == nil:
		return nil
	case store.IsUniqueViolation(err):
		return ErrCodeTaken
	case store.IsForeignKeyViolation(err):
		v := &auth.ValidationError{}
		v.Add("reference", "not_found")
		return v
	}
	return versionErr(err)
}

func cleanText(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxRunes
}

func i18nJSON(t I18n) store.JSON { return store.MustJSON(t) }

func i18nOf(j store.JSON) I18n {
	var t I18n
	if len(j) > 0 {
		_ = json.Unmarshal(j, &t)
	}
	return t
}

func checkI18n(v *auth.ValidationError, field string, t I18n, maxRunes int) I18n {
	it, ok := cleanText(t.It, maxRunes)
	if !ok || it == "" {
		v.Add(field+".it", "invalid")
	}
	t.It = it
	if t.En != nil {
		en, ok := cleanText(*t.En, maxRunes)
		if !ok {
			v.Add(field+".en", "invalid")
		}
		if en == "" {
			t.En = nil
		} else {
			t.En = &en
		}
	}
	return t
}

// localized returns the text in the locale (en when present and asked), else Italian.
func localized(t I18n, locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "en") && t.En != nil && *t.En != "" {
		return *t.En
	}
	return t.It
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	if s.d.Audit == nil {
		return nil
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, data any) error {
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// catalogChanged writes the audit row and the yard_catalog.changed event of a catalogue write (ids only).
func (s *Service) catalogChanged(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	if err := s.audit(ctx, tx, scope, a, action, entity, id, diff); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.YardCatalogChanged, event.YardCatalogChangedData{Entity: entity, ID: id})
}

// readScope authorises a read (yard.read or yard.manage).
func (s *Service) readScope(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardRead, auth.PermYardManage)
	return scope, err
}

// manageScope authorises a write (yard.manage).
func (s *Service) manageScope(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	return p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermYardManage)
}
