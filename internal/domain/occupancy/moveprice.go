package occupancy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Move price effect (spec 10.5 step 4; m3-plan §3.5 step 6, §4.6): the remaining nights of a moved contract
// allocation are priced on the old and on the new resource before the write transaction. The price is replaced
// only when the contract policy says reprice_on_move or the caller (with contracts.write) asks for it; otherwise
// the difference is only reported and audited. Pricing belongs to the contract service, which this package must
// not import: it is injected as a MovePricer by internal/app.

// PriceEffect is the effect of a move on the stored totals of the contracts involved (summed).
type PriceEffect struct {
	OldTotalCents, NewTotalCents, DeltaCents int64
	Applied                                  bool
}

// MoveLeg is one allocation leaving its resource for Target over the nights [From, To].
type MoveLeg struct {
	AllocationID   string
	ContractLineID string
	ResourceType   string // current resource
	ResourceID     string
	Target         Target
	From, To       calendar.Date
}

// MovePrice is the price effect of a move, computed before its write transaction.
type MovePrice struct {
	Effect *PriceEffect
	// ApplyTx writes the repriced contracts inside the move transaction (nil when nothing is applied). It fails
	// when a contract or its price list changed since the price was computed.
	ApplyTx func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time) error
	// Undo describes what ApplyTx applied (opaque to this package): it is stored in the undo entry of the move
	// and handed back to MovePricer.UndoMovePriceTx when the move is undone (nil when nothing is applied).
	Undo json.RawMessage
}

// MovePricer prices the legs of a move (implemented by the contract service). reprice is the caller's explicit
// choice (nil = the contract policy); the caller has already checked contracts.write for it.
type MovePricer interface {
	PriceMove(ctx context.Context, p *auth.Principal, marinaID string, legs []MoveLeg, reprice *bool) (*MovePrice, error)
	// UndoMovePriceTx reverses, inside the undo transaction, the price a move applied (MovePrice.Undo).
	UndoMovePriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, now time.Time, undo json.RawMessage) error
}

// SetMovePricer wires the move pricer (internal/app, after the contract service exists).
func (s *Service) SetMovePricer(mp MovePricer) { s.pricer = mp }

// authorizeReprice requires contracts.write for an explicit reprice choice (m3-plan §3.5 step 6).
func authorizeReprice(p *auth.Principal, scope store.MarinaScope, reprice *bool) error {
	if reprice != nil && !p.Can(scope.OrgID(), scope.ID(), auth.PermContractsWrite) {
		return ErrRepriceForbidden
	}
	return nil
}

// legOf returns the priced leg of moving a (at the version the client saw) to target from the requested night,
// or false when there is nothing to price: no contract line, same resource, or a request the write transaction
// will refuse anyway.
func legOf(a *store.Allocation, version int, target Target, from calendar.Date, to *calendar.Date) (MoveLeg, bool) {
	if a.ContractLineID == "" || a.Status != store.AllocActive || a.Version != version {
		return MoveLeg{}, false
	}
	if a.ResourceType == string(target.ResourceType) && a.ResourceID == target.ResourceID {
		return MoveLeg{}, false // a new placement on the same resource does not change the price
	}
	last := a.LastNight
	if to != nil && !to.After(last) {
		last = *to
	}
	if last.Before(from) || from.Before(a.FirstNight) {
		return MoveLeg{}, false
	}
	return MoveLeg{
		AllocationID: a.ID, ContractLineID: a.ContractLineID, ResourceType: a.ResourceType, ResourceID: a.ResourceID,
		Target: target, From: from, To: last,
	}, true
}

// priceLegs runs the pricer for legs (nil result when nothing is priced).
func (s *Service) priceLegs(ctx context.Context, p *auth.Principal, marinaID string, legs []MoveLeg, reprice *bool) (*MovePrice, error) {
	if s.pricer == nil || len(legs) == 0 {
		return nil, nil
	}
	return s.pricer.PriceMove(ctx, p, marinaID, legs, reprice)
}

// apply writes the repriced contracts in the move transaction.
func (mp *MovePrice) apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time) error {
	if mp == nil || mp.ApplyTx == nil {
		return nil
	}
	return mp.ApplyTx(ctx, tx, scope, now)
}

// undoData is what the undo of the move must reverse (nil when nothing was applied).
func (mp *MovePrice) undoData() json.RawMessage {
	if mp == nil || mp.ApplyTx == nil {
		return nil
	}
	return mp.Undo
}

func (mp *MovePrice) effect() *PriceEffect {
	if mp == nil {
		return nil
	}
	return mp.Effect
}

// auditDiff adds the price effect to the diff of an allocation.move audit row.
func (e *PriceEffect) auditDiff(diff map[string]any) {
	if e == nil {
		return
	}
	diff["price_delta_cents"] = e.DeltaCents
	diff["old_total_cents"] = e.OldTotalCents
	diff["new_total_cents"] = e.NewTotalCents
	diff["repriced"] = e.Applied
}

// todayIn returns the marina-local today read outside a transaction.
func (s *Service) todayIn(ctx context.Context, scope store.MarinaScope) (calendar.Date, error) {
	loc, err := marinaLocation(ctx, s.d.DB.DB, scope)
	if err != nil {
		return calendar.Date{}, err
	}
	return calendar.In(s.now(), loc), nil
}
