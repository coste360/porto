// Package workorder is the yard work orders (commesse) with tasks, dependencies, checklists and photos, time entries
// with approval, material issues and returns, external costs, the final account and the billing through the M5
// charges (spec 18.4-18.6, m10-plan §5, §7-§10). It implements yardquote.OrderCreator. Owned by the ORDERS builder.
package workorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB      *store.DB
	Clock   clock.Clock
	Audit   *audit.Writer
	Outbox  *event.Outbox
	Log     *slog.Logger
	Keys    *crypto.KeyRing
	Files   *filestore.Store
	Charges *charge.Service
	Prices  *pricelist.Service
	Docs    *docs.Service
	// Quotes reads the yard settings and prices manual orders with the quote line engine.
	Quotes *yardquote.Service
	// Bookings cancels the future lifting bookings of a cancelled order (lifting.Service, set in internal/app).
	Bookings BookingCanceller
}

// Service is the work order service.
type Service struct {
	d      Deps
	pricer LinePricer
}

// NewService returns the service.
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

// SetBookingCanceller sets the lifting side (internal/app wires lifting.Service after both are built).
func (s *Service) SetBookingCanceller(b BookingCanceller) { s.d.Bookings = b }

var _ yardquote.OrderCreator = (*Service)(nil)

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

func cleanText(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= maxRunes
}

// Invalid is a validation error of one field (400 validation_failed), for the handlers.
func Invalid(field, code string) error { return invalid(field, code) }

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

// readScope authorises a read of orders (yard.read or yard.manage).
func (s *Service) readScope(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardRead, auth.PermYardManage)
	return scope, err
}

// manageScope authorises a change of orders (yard.manage).
func (s *Service) manageScope(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	return p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermYardManage)
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
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

// orderChanged writes work_order.changed (ids and enums only, INV-70).
func (s *Service) orderChanged(ctx context.Context, tx bun.IDB, scope store.MarinaScope, orderID, entity, id string) error {
	return s.emit(ctx, tx, scope, event.WorkOrderChanged, event.WorkOrderChangedData{WorkOrderID: orderID, Entity: entity, ID: id})
}

// Settings are the yard settings the order side reads, with the defaults applied.
type Settings struct {
	DepositBP             int64
	AdvanceOnAccept       bool
	ExternalMarkupBP      int64
	ExternalItemID        string
	FixedIncludesExternal bool
	WorkersIssueMaterials bool
}

// settings reads the yard settings through the shared reader of the quote side (yardquote.Service.SettingsTx).
// Without the quote service (unit tests of this package) the row is read directly with the same defaults.
func (s *Service) settings(ctx context.Context, db bun.IDB, scope store.MarinaScope) (Settings, error) {
	var y store.YardSettings
	if s.d.Quotes != nil {
		got, err := s.d.Quotes.SettingsTx(ctx, db, scope)
		if err != nil {
			return Settings{}, err
		}
		y = got
	} else {
		row, ok, err := store.WorkOrderYardSettings(ctx, db, scope)
		if err != nil {
			return Settings{}, err
		}
		if !ok {
			row = store.YardSettings{
				DepositBP: yardquote.DefaultDepositBP, AdvanceOnAccept: true, ExternalMarkupBP: yardquote.DefaultExternalMarkupBP,
			}
		}
		y = row
	}
	return Settings{
		DepositBP: int64(y.DepositBP), AdvanceOnAccept: y.AdvanceOnAccept, ExternalMarkupBP: int64(y.ExternalMarkupBP),
		ExternalItemID: y.ExternalItemID, FixedIncludesExternal: y.FixedIncludesExternal,
		WorkersIssueMaterials: y.WorkersIssueMaterials,
	}, nil
}

// FormatNumber formats an order number (C<year>-NNNNNN, D12).
func FormatNumber(year int, n int64) string { return fmt.Sprintf("C%04d-%06d", year, n) }

// catalogPrice is the catalogue unit price of an item on a day from its active base rules (highest priority, then
// most recent: unit price, else flat amount); found is false when no rule applies.
//
// It reads the rules of every active price list without the customer category scope; the quote side prices quote
// lines with pricing.UnitPrice, and the order side uses this only for items outside the frozen rate snapshot
// (materials not in the budget, labour items of qualifications added later).
func (s *Service) catalogPrice(ctx context.Context, db bun.IDB, scope store.MarinaScope, itemID string, day calendar.Date) (money.Cents, bool, error) {
	rules, err := store.WOCatalogRules(ctx, db, scope, itemID, day)
	if err != nil {
		return 0, false, err
	}
	for _, rl := range rules {
		var body pricing.Rule
		if json.Unmarshal(rl.Body, &body) != nil {
			continue
		}
		if body.UnitCents != nil {
			return *body.UnitCents, true, nil
		}
		if body.AmountCents != nil {
			return *body.AmountCents, true, nil
		}
	}
	return 0, false, nil
}

// RunTimersGuard is the job yard.timers_guard: running timers older than 16 h are logged for the manager (ids
// only, never closed; m10-plan §7, D24). The phone and the approval page show them through the running status.
func (s *Service) RunTimersGuard(ctx context.Context) error {
	stale, err := store.StaleRunningTimers(ctx, s.d.DB.DB, s.d.Clock.Now().Add(-MaxEntryHoursMilli*time.Hour/1000))
	if err != nil {
		return err
	}
	for _, t := range stale {
		s.d.Log.Warn("yard timer running longer than 16 hours", "marina_id", t.MarinaID, "timesheet_id", t.ID,
			"employee_id", t.EmployeeID)
	}
	return nil
}
