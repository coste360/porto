// Package pricelist manages price lists (spec 12.2-12.6; m2-plan §5): documents (create, save diff, validate,
// delete), activation with the one-active-list-per-night rule (INV-24), archive, renewal (dry run and apply),
// and the pricing tools (simulate with input resolution, matrix, coverage) on top of the pure engine
// internal/domain/pricing. Service.Quote is the single entry point that later milestones call to price a stay.
//
// Every method authorises first (Principal.Authorize: pricing.configure for writes, pricing.read for reads) and
// scopes every query by the marina. State changes write the change, an audit row and the
// config.pricing_changed outbox event in one transaction (store.RunInTx).
package pricelist

import (
	"log/slog"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
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

// Service is the price list service.
type Service struct {
	d     Deps
	quote quoter
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d, quote: pricing.Quote}
}
