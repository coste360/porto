// Package rights manages berth rights of use with their holders and authorised vessels, the owner occupancy
// (through internal/domain/occupancy), transfers, management mandates and their revenues, management fee tables
// and runs, millesimal tables, budgets and common charge runs, holder instalments with their manual settlement
// and the concession (spec 14; m4-plan §4).
//
// Every state change runs in store.RunInTx with an audit row and the outbox event in the same transaction;
// every exported method authorises first (rights.manage, payments.collect for settlement, marina.configure for
// concession writes). Allocation changes go through the occupancy tx primitives (INV-01 stays a DB guarantee).
package rights

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB         *store.DB
	Clock      clock.Clock
	Audit      *audit.Writer
	Outbox     *event.Outbox
	Keys       *crypto.KeyRing
	Occupancy  *occupancy.Service
	Prices     *pricelist.Service
	Docs       *docs.Service
	Signatures *signature.Service
	Notify     *notify.Service
	Log        *slog.Logger
}

// Service is the rights service.
type Service struct {
	d Deps

	// accrued remembers, per marina, the marina day of the last scheduled accrual (job mandates.accrue runs
	// hourly and accrues once per marina day after 02:00; a restart runs it again, which is idempotent).
	mu      sync.Mutex
	accrued map[string]calendar.Date

	// billing creates the charges of issued holder instalments (M5, billing.go); nil = M4 behaviour.
	billing BillingHooks
	// settler records the payment of a holder instalment's charge (M5, payments.go); set once at wiring.
	settler ChargeSettler
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d, accrued: map[string]calendar.Date{}}
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// location returns the time zone of the marina (UTC when unknown).
func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
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

// today returns the current night in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return occupancy.Today(s.d.Clock, s.location(ctx, db, scope))
}

// can reports whether the principal holds perm on the marina of scope.
func can(p *auth.Principal, scope store.MarinaScope, perm auth.Permission) bool {
	return p != nil && p.Can(scope.OrgID(), scope.ID(), perm)
}

func actorOf(p *auth.Principal) audit.Actor {
	if p == nil {
		return audit.System
	}
	return p.Actor()
}

func userOf(p *auth.Principal) string {
	if p == nil {
		return ""
	}
	return p.UserID
}

// write stores the audit row and the outbox events of a change in tx (INV-30 for rights, m4-plan §0.10).
func (s *Service) write(ctx context.Context, tx bun.IDB, actor audit.Actor, scope store.MarinaScope, action, entityType, id string, diff any, events ...ev) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: actor, Action: action, EntityType: entityType, EntityID: id, Diff: diff,
	}); err != nil {
		return err
	}
	for _, e := range events {
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, e.typ, e.data); err != nil {
			return fmt.Errorf("%s event: %w", e.typ, err)
		}
	}
	return nil
}

// ev is an outbox event to write with a change.
type ev struct {
	typ  string
	data any
}

// changed is the internal rights.changed event of an entity.
func changed(entity, id, change string) ev {
	return ev{event.RightsChanged, event.RightsChangedData{Entity: entity, ID: id, Change: change}}
}

// occupancyEvents are the occupancy.allocated / occupancy.released events of owner allocation changes, so the
// map views refresh (allocations carry no personal data).
func allocatedEvent(a *store.Allocation) ev {
	return ev{event.OccupancyAllocated, event.OccupancyAllocatedData{
		AllocationIDs: []string{a.ID}, ResourceType: a.ResourceType, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID,
		Kind: a.Kind, From: a.FirstNight, To: a.LastNight,
	}}
}

func releasedEvent(a *store.Allocation, from, to calendar.Date, reason string) ev {
	return ev{event.OccupancyReleased, event.OccupancyReleasedData{
		AllocationIDs: []string{a.ID}, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID, From: from, To: to, Reason: reason,
	}}
}

// IssueDueTx issues the scheduled holder instalments of the marina whose issue date is today or earlier (at most
// limit rows), inside the caller's transaction. It is called by the installments.issue job of contract (m4-plan
// §13.3) and returns the number of issued rows. Issuing is a conditional update, so concurrent runs issue each
// row once; the payment request email is queued with a dedupe key per instalment.
func (s *Service) IssueDueTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, today calendar.Date, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	due, err := store.DueHolderInstallments(ctx, tx, scope, today, limit)
	if err != nil {
		return 0, err
	}
	// Lock order right → holder instalments, rights in id order (lockHolderRights); a row moved or cancelled by a
	// transfer while we waited fails the conditional update below and is skipped.
	slices.SortStableFunc(due, func(a, b store.HolderInstallment) int { return strings.Compare(a.RightID, b.RightID) })
	if err := lockHolderRights(ctx, tx, scope, due); err != nil {
		return 0, err
	}
	now := s.now()
	n := 0
	for i := range due {
		h := &due[i]
		ok, err := store.IssueHolderInstallment(ctx, tx, scope, h.ID, now)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		n++
		if err := s.write(ctx, tx, audit.System, scope, "holder_installment.issue", "holder_installment", h.ID,
			map[string]any{"source_type": h.SourceType, "due_on": h.DueOn, "amount_cents": h.AmountCents},
			changed("holder_installment", h.ID, event.ChangeIssued)); err != nil {
			return n, err
		}
		if s.billing != nil {
			if err := s.billing.OnHolderInstallmentIssuedTx(ctx, tx, scope, audit.System, h); err != nil {
				return n, err
			}
			continue
		}
		if err := s.queuePaymentRequest(ctx, tx, scope, h); err != nil {
			return n, err
		}
	}
	return n, nil
}
