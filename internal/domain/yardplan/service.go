// Package yardplan is the yard planner: employees (distinct from users), qualifications, weekly calendars and
// absences, task assignments with qualification, dependency and capacity checks, workload by employee, department,
// qualification and bay, at-risk orders and the greedy auto-plan proposal (spec 18.5, m10-plan §6). Employee names
// never reach events or logs (INV-70).
package yardplan

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
}

// Service is the planner service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// today is the business date in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	loc := time.UTC
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return calendar.In(s.d.Clock.Now(), loc)
}

func (s *Service) read(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardRead, auth.PermYardManage)
	return scope, err
}

func (s *Service) manage(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	return p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermYardManage)
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

func cleanText(v string, maxRunes int) (string, bool) {
	v = strings.TrimSpace(v)
	return v, utf8.RuneCountInString(v) <= maxRunes
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

// changed writes yard_plan.changed: entity, id and dates only (INV-70).
func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, entity, id string, dates []calendar.Date) error {
	if s.d.Outbox == nil {
		return nil
	}
	var ds []string
	seen := map[string]bool{}
	for _, d := range dates {
		if v := d.String(); !d.IsZero() && !seen[v] {
			seen[v] = true
			ds = append(ds, v)
		}
	}
	sort.Strings(ds)
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.YardPlanChanged, event.YardPlanChangedData{Entity: entity, ID: id, Dates: ds})
	return err
}

// rangeOrWeek returns [from, to] defaulting to the ISO week of today; to before from or a span above MaxRangeDays is
// a validation error.
func rangeOrWeek(today, from, to calendar.Date) (calendar.Date, calendar.Date, error) {
	if from.IsZero() && to.IsZero() {
		from = today.AddDays(1 - today.ISOWeekday())
		return from, from.AddDays(6), nil
	}
	if from.IsZero() {
		from = to.AddDays(-6)
	}
	if to.IsZero() {
		to = from.AddDays(6)
	}
	v := &auth.ValidationError{}
	if to.Before(from) {
		v.Add("to", "before_from")
	} else if from.DaysUntil(to)+1 > MaxRangeDays {
		v.Add("to", "range_too_long")
	}
	return from, to, v.Err()
}
