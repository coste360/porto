// Package contract manages quotes with options, mooring contracts (base) and transits (spec 13 base, 10.6;
// m3-plan §4): numbering, the state machines of states.go, price snapshots from pricelist.Service.Quote
// (INV-20), inquiries and the contracts.expire job.
//
// Every state change of a contract, transit or quote goes through this package (INV-30) inside store.RunInTx,
// with an audit row and its outbox event in the same transaction. Allocation changes go through the tx
// primitives of internal/domain/occupancy. Transit confirmation happens only in ConfirmTransit (manual driver
// in M3, payment webhook later: INV-09). Every exported method authorises first (Principal.Authorize).
package contract

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Log       *slog.Logger
	Occupancy *occupancy.Service
	Prices    *pricelist.Service
}

// Service is the contract service.
type Service struct {
	d     Deps
	alloc allocator

	// Test hooks: afterPrice runs between pricing and the write transaction, beforeCommit at the end of the
	// write transaction of transit creation and confirmation (its error rolls the transaction back).
	afterPrice   func()
	beforeCommit func() error
}

func (s *Service) priced() {
	if s.afterPrice != nil {
		s.afterPrice()
	}
}

func (s *Service) commitHook() error {
	if s.beforeCommit != nil {
		return s.beforeCommit()
	}
	return nil
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d}
	if d.Occupancy != nil {
		s.alloc = d.Occupancy
	}
	return s
}

// actor is who performs a state change: a principal or the system (jobs).
type actor struct {
	p *auth.Principal
}

func (a actor) audit() audit.Actor { return a.p.Actor() }

func (a actor) userID() string {
	if a.p == nil || a.p.Kind != auth.KindUser {
		return ""
	}
	return a.p.UserID
}

var system = actor{}

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

// today returns the current night in the marina time zone (occupancy.Today, m3-plan §0.12).
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return occupancy.Today(s.d.Clock, location(ctx, db, scope))
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

// authorizeOverride checks occupancy.override_fit when a fit override is requested (m3-plan §7.2).
func (s *Service) authorizeOverride(ctx context.Context, p *auth.Principal, marinaID string, reason *string) error {
	if reason == nil {
		return nil
	}
	if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyOverrideFit); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return occupancy.ErrOverrideForbidden
		}
		return err
	}
	return nil
}

// writeAudit records one audit row of a contract, transit, quote or inquiry.
func (s *Service) writeAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, action, entityType, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, auditEntry(scope, a, action, entityType, id, diff))
}

func auditEntry(scope store.MarinaScope, a actor, action, entityType, id string, diff any) audit.Entry {
	return audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a.audit(), Action: action,
		EntityType: entityType, EntityID: id, Diff: diff,
	}
}

// emit writes an outbox event in tx.
func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, data any) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// requireAllocator fails when the occupancy engine is not wired (tests without it).
func (s *Service) requireAllocator() error {
	if s.alloc == nil {
		return errors.New("contract: occupancy service not wired")
	}
	return nil
}

// withAlternatives adds search alternatives for the taken nights to a resource_taken error of a stay change or
// a quote acceptance (m3-plan §4.2, §4.3); the write transaction has already rolled back.
func (s *Service) withAlternatives(ctx context.Context, p *auth.Principal, marinaID, vesselID string, err error) error {
	ce, ok := errors.AsType[*occupancy.ConflictError](err)
	if !ok || s.d.Occupancy == nil || len(ce.Alternatives) > 0 {
		return err
	}
	ce.Alternatives = s.d.Occupancy.Alternatives(ctx, p, marinaID, vesselID, ce.From, ce.To)
	return err
}
