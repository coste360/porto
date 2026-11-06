// Package customer is the customer registry (spec 11.1; m1-plan §4.4): customers, consents, search,
// duplicates, merge, anonymisation, GDPR export and fiscal checks.
//
// Every method authorises first (Principal.Authorize: customers.write for writes, customers.read for reads) and scopes every query by the marina. State
// changes write the change, an audit row and the outbox event in one transaction (store.RunInTx).
package customer

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

// Service is the customer service.
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
