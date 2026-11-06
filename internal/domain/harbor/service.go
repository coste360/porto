// Package harbor is the harbour map service (spec 07, 08; m1-plan §2.1): maps, map documents (load, save,
// validate), generator previews, backgrounds, export and import, berth CSV import, berth groups and harbour
// settings. Geometry and validation live in the pure package internal/domain/layout; every write goes through
// applyDocument (one write path, INV-04).
//
// Every method authorises first (Principal.Authorize: harbor.configure for writes, map.view for reads) and scopes every query by the marina. State
// changes write the change, an audit row and the outbox event in one transaction (store.RunInTx).
package harbor

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Keys   *crypto.KeyRing
	Files  *filestore.Store
	Log    *slog.Logger
	// Guard checks element changes against occupancies (spec 8.5); nil = no-op (M1). M3 implements it.
	Guard OccupancyGuard
}

// ElementChange is one change of a saved element in a map save, as seen by the OccupancyGuard.
type ElementChange struct {
	Kind            string // berth, dynamic_area, land_resource
	ID              string
	Removed         bool
	GeometryChanged bool // position, rotation or size changed
}

// OccupancyGuard refuses map changes that would break existing occupancies (spec 8.5). It runs inside the save
// transaction, after validation and before the write. M1 has no occupancies: nil means no-op.
type OccupancyGuard interface {
	CheckChanges(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mapID string, changes []ElementChange) error
}

type noGuard struct{}

func (noGuard) CheckChanges(context.Context, bun.IDB, store.MarinaScope, string, []ElementChange) error {
	return nil
}

// Service is the harbor service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Guard == nil {
		d.Guard = noGuard{}
	}
	return &Service{d: d}
}
