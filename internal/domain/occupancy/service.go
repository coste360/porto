// Package occupancy is the occupancy engine service (spec 10; m3-plan §2.3, §3): tx primitives that write
// allocations and their occupancy rows (Allocate, Convert, Truncate, Cancel, Resize, ExpireHolds, Move, Layout,
// Release, Revoke, Block), search and placement check, day view and map occupancy, closures, settings, move undo
// and the harbour guard.
//
// No double occupancy is the primary key of the three row tables (INV-01): rows are inserted with
// ON CONFLICT DO NOTHING and a shortfall is a *ConflictError that rolls the caller's transaction back. Never
// "check then insert". The pure parts live in internal/domain/occupancy/engine and internal/domain/fit.
//
// Exported service methods authorise first (Principal.Authorize) and scope every query by the marina; tx-level
// primitives (...Tx) take an already-authorised store.MarinaScope and are never called from handlers. This
// package never imports internal/domain/contract.
package occupancy

import (
	"log/slog"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
}

// Service is the occupancy service.
type Service struct {
	d      Deps
	pricer MovePricer // set by internal/app (SetMovePricer): the contract service
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

// Today returns the current night in the marina time zone tz (the single helper for "today", m3-plan §0.12).
func Today(c clock.Clock, tz *time.Location) calendar.Date {
	return calendar.In(c.Now(), tz)
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }
