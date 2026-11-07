package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Numbering series of billing_sequences used by the ledger.
const (
	seriesPayment = "payment"
	seriesRefund  = "refund"
)

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

// authorizeAny authorises the first permission the principal holds (404 for another marina, 403 when none).
func authorizeAny(ctx context.Context, db bun.IDB, p *auth.Principal, marinaID string, perms ...auth.Permission) (store.MarinaScope, auth.Permission, error) {
	var last error
	for _, perm := range perms {
		scope, err := p.Authorize(ctx, db, marinaID, perm)
		if err == nil {
			return scope, perm, nil
		}
		if !errors.Is(err, auth.ErrForbidden) {
			return store.MarinaScope{}, "", err
		}
		last = err
	}
	return store.MarinaScope{}, "", last
}

// can reports whether the principal holds perm on the marina of scope.
func can(p *auth.Principal, scope store.MarinaScope, perm auth.Permission) bool {
	return p != nil && p.Can(scope.OrgID(), scope.ID(), perm)
}

// actorString is the recorded_by_actor / created_by_actor value: user:<id>, api_key:<id>, system:<job> or
// provider:<code> (system actors carry their name in ID).
func actorString(a audit.Actor) string {
	t := a.Type
	if t == "" {
		t = audit.ActorSystem
	}
	if a.ID == "" {
		return t
	}
	if t == audit.ActorSystem {
		return a.ID // system:<job> and provider:<code> are carried whole
	}
	return t + ":" + a.ID
}

// userOf returns the user id of a user actor ("" otherwise).
func userOf(a audit.Actor) string {
	if a.Type == audit.ActorUser {
		return a.ID
	}
	return ""
}

// principalOf rebuilds the principal of an actor for the contract and rights entry points, which take
// *auth.Principal for their audit attribution (nil = system).
func principalOf(a audit.Actor) *auth.Principal {
	switch a.Type {
	case audit.ActorUser:
		return &auth.Principal{Kind: auth.KindUser, UserID: a.ID}
	case audit.ActorAPIKey:
		return &auth.Principal{Kind: auth.KindAPIKey, APIKeyID: a.ID}
	case audit.ActorDevice:
		return &auth.Principal{Kind: auth.KindDevice, DeviceID: a.ID}
	}
	return nil
}

// ProviderActor is the audit actor of a payment provider webhook (provider:<code>).
func ProviderActor(code string) audit.Actor {
	return audit.Actor{Type: audit.ActorSystem, ID: "provider:" + code}
}

// JobActor is the audit actor of a job (system:<name>).
func JobActor(name string) audit.Actor {
	return audit.Actor{Type: audit.ActorSystem, ID: "system:" + name}
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

func ledgerNumber(prefix string, year int, n int64) string {
	return fmt.Sprintf("%s%04d-%06d", prefix, year, n)
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

func paymentRefAAD(id string) string { return "payment-ref:" + id }
