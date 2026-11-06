// Package deadline holds deadlines (spec 11.5 basic; m1-plan §3.2): deadlines synced from document expiries,
// per-kind lead-day policies and the list with urgency.
//
// Every method authorises first (Principal.Authorize: customers.write for writes, customers.read for reads, marina.configure for policies) and scopes every query by the marina. State
// changes write the change, an audit row and the outbox event in one transaction (store.RunInTx).
package deadline

import (
	"log/slog"

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
}

// Service is the deadline service.
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
