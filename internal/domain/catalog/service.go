// Package catalog is the pricing catalogue of a marina (spec 12.1, 07 price_class, 11.1; m2-plan §5): pricing
// settings, revenue classes (spec defaults created lazily), items and services, berth categories and their
// sync from the harbour price-class rule, and the in_use checks that keep referenced rows from being deleted.
//
// Every method authorises first (Principal.Authorize: pricing.configure for writes, pricing.read for reads) and
// scopes every query by the marina. State changes write the change, an audit row and the
// config.pricing_changed outbox event in one transaction (store.RunInTx).
package catalog

import (
	"log/slog"

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

// Service is the catalogue service.
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
