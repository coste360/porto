// Package deadline holds deadlines (spec 11.5; m1-plan §3.2, m4-plan §6.8): deadlines synced from document
// expiries, the M4 engine reconciling contracts, instalments, rights, mandates, concession, proposals, waiting-list
// offers, failed documents and expiring signatures, per-kind lead-day policies with channels, the lead-day
// notifications, the insurance check and the deadlines CSV import.
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
	"github.com/coste360/porto/internal/domain/notify"
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
	// Notify queues the lead-day emails of the engine (M4; nil = panel only, nothing is marked notified for
	// email channels).
	Notify *notify.Service
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
