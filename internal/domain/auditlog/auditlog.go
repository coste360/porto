// Package auditlog is the read side of the audit log for the admin API (audit.read). Writing stays in
// internal/domain/audit, which every service calls inside its own transaction.
package auditlog

import (
	"context"
	"time"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Entry is one audit row as the API shows it (no IP address).
type Entry = store.AuditEntry

// Filter narrows a listing; zero values mean "any".
type Filter struct {
	EntityType, EntityID, ActorID string
	From, To                      time.Time
}

// Service lists audit rows.
type Service struct {
	db     *store.DB
	writer *audit.Writer
}

// NewService returns the audit log reader.
func NewService(db *store.DB, w *audit.Writer) *Service { return &Service{db: db, writer: w} }

// List returns the audit rows of a marina, newest first. limit is clamped to 1..200 (default 50).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[Entry], error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermAuditRead)
	if err != nil {
		return store.Page[Entry]{}, err
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		v := &auth.ValidationError{}
		v.Add("to", "before_from")
		return store.Page[Entry]{}, v
	}
	return s.writer.List(ctx, s.db.DB, scope, audit.Filter{
		EntityType: f.EntityType, EntityID: f.EntityID, ActorID: f.ActorID, From: f.From, To: f.To,
	}, cursor, store.ClampLimit(limit))
}
