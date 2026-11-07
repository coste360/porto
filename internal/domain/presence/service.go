// Package presence records arrivals, departures (and, for M9, haul-outs and launches) of vessels, keeps the
// per-vessel state vessel_presence in the same transaction, handles unexpected arrivals (a held transit plus the
// arrival) and computes the movements of the day (spec 10.6, 9.5; m3-plan §4.4).
//
// Transit state changes caused by presences go through internal/domain/contract (INV-30). Notes and photos are
// never logged or emitted (INV-70). Every exported method authorises first (Principal.Authorize).
package presence

import (
	"log/slog"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Presence events.
const (
	EventArrived   = "arrived"
	EventDeparted  = "departed"
	EventHauledOut = "hauled_out" // accepted by the API for M9, not offered by the UI
	EventLaunched  = "launched"   // accepted by the API for M9, not offered by the UI
)

// Vessel presence states (vessel_presence.state).
const (
	StateInPort = "in_port"
	StateOut    = "out"
	StateAshore = "ashore"
	StateYard   = "yard"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Keys      *crypto.KeyRing
	Files     *filestore.Store
	Log       *slog.Logger
	Contracts *contract.Service
	Occupancy *occupancy.Service
}

// Service is the presence service.
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
