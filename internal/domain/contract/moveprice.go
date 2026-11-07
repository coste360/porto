package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Move repricing (spec 10.5 step 4; m3-plan §3.5 step 6, §4.6, §13 D10). The remaining nights of each moved leg
// are quoted on the old and on the new resource; the difference is the price effect. It replaces the stored
// price only when the contract policy has reprice_on_move or the caller asked for it (contracts.write, checked
// by the occupancy service); then the total moves by the difference and the snapshot records the adjustment
// (its pinned request and breakdown stay as they were, so the original quote is still reproducible, INV-20).

// PriceAdjustment is one repriced move stored in PriceSnapshot.Adjustments.
type PriceAdjustment struct {
	Kind          string        `json:"kind"` // move
	AllocationID  string        `json:"allocation_id"`
	From          calendar.Date `json:"from"`
	To            calendar.Date `json:"to"`
	OldResourceID string        `json:"old_resource_id"`
	NewResourceID string        `json:"new_resource_id"`
	OldCents      int64         `json:"old_cents"`
	NewCents      int64         `json:"new_cents"`
	PriceListID   string        `json:"price_list_id"`
}

// PriceAdjustment.Kind: a repriced move, and its reversal by the undo of the move (old and new swapped).
const (
	AdjustmentMove     = "move"
	AdjustmentMoveUndo = "move_undo"
)

// movePriceUndo is one repriced contract in the undo entry of a move (occupancy.MovePrice.Undo).
type movePriceUndo struct {
	ContractID  string            `json:"contract_id"`
	Transit     bool              `json:"transit"`
	Adjustments []PriceAdjustment `json:"adjustments"`
}

// movePriced is the price effect of a move on one contract.
type movePriced struct {
	contractID string
	version    int
	transit    bool
	oldTotal   int64
	delta      int64
	apply      bool
	adjust     []PriceAdjustment
	prices     []*priced // the new-resource quotes, whose lists must still be active at write time
}

// livePriced are the statuses whose price a move can still change.
var livePriced = []Status{StatusDraft, StatusActive, StatusHeld, StatusPendingPayment, StatusConfirmed, StatusArrived}

// PriceMove implements occupancy.MovePricer: it prices every leg of a contract allocation before the move
// transaction (occupancy.move or map.view; pricing.read through pricelist.Quote). A leg that cannot be priced (no_price, no
// pricing.read) makes the contract unreported, unless its price must be replaced: then the move fails.
func (s *Service) PriceMove(ctx context.Context, p *auth.Principal, marinaID string, legs []occupancy.MoveLeg, reprice *bool) (*occupancy.MovePrice, error) {
	// The caller is the move (occupancy.move) or the placement check (map.view).
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	}
	if err != nil {
		return nil, err
	}
	byContract := map[string]*movePriced{}
	var order []string
	for _, leg := range legs {
		c, err := store.ContractByLine(ctx, s.d.DB.DB, scope, leg.ContractLineID)
		if err != nil || !slices.Contains(livePriced, Status(c.Status)) {
			continue
		}
		mp := byContract[c.ID]
		if mp == nil {
			pol, err := DecodePolicy(c.Policy)
			if err != nil {
				return nil, err
			}
			apply := pol.RepriceOnMove
			if reprice != nil {
				apply = *reprice
			}
			mp = &movePriced{contractID: c.ID, version: c.Version, transit: c.Kind == KindTransit, oldTotal: c.TotalCents, apply: apply}
			byContract[c.ID] = mp
			order = append(order, c.ID)
		}
		req := priceRequest{
			contractType: PricingType(c.Kind, c.Term), arrival: leg.From, departure: leg.To.AddDays(1),
			vesselID: c.VesselID, customerID: c.CustomerID, channel: c.Channel,
		}
		req.first = Segment{ResourceType: leg.ResourceType, ResourceID: leg.ResourceID}
		oldP, errOld := s.price(ctx, p, marinaID, req)
		req.first = Segment{ResourceType: string(leg.Target.ResourceType), ResourceID: leg.Target.ResourceID}
		newP, errNew := s.price(ctx, p, marinaID, req)
		if err := errors.Join(errOld, errNew); err != nil {
			if mp.apply {
				return nil, err
			}
			delete(byContract, c.ID) // reported only, and not priceable: no effect for this contract
			continue
		}
		mp.delta += newP.total() - oldP.total()
		mp.prices = append(mp.prices, newP)
		mp.adjust = append(mp.adjust, PriceAdjustment{
			Kind: AdjustmentMove, AllocationID: leg.AllocationID, From: leg.From, To: leg.To,
			OldResourceID: leg.ResourceID, NewResourceID: leg.Target.ResourceID,
			OldCents: oldP.total(), NewCents: newP.total(), PriceListID: newP.listID(),
		})
	}
	out := &occupancy.MovePrice{}
	var toApply []*movePriced
	for _, id := range order {
		mp := byContract[id]
		if mp == nil {
			continue
		}
		if out.Effect == nil {
			out.Effect = &occupancy.PriceEffect{}
		}
		out.Effect.OldTotalCents += mp.oldTotal
		out.Effect.NewTotalCents += mp.oldTotal + mp.delta
		out.Effect.DeltaCents += mp.delta
		if mp.apply {
			out.Effect.Applied = true
			if mp.delta != 0 {
				toApply = append(toApply, mp)
			}
		}
	}
	if out.Effect == nil {
		return nil, nil
	}
	if len(toApply) > 0 {
		undo := make([]movePriceUndo, 0, len(toApply))
		for _, mp := range toApply {
			undo = append(undo, movePriceUndo{ContractID: mp.contractID, Transit: mp.transit, Adjustments: mp.adjust})
		}
		raw, err := json.Marshal(undo)
		if err != nil {
			return nil, err
		}
		out.Undo = raw
		out.ApplyTx = func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time) error {
			for _, mp := range toApply {
				if err := s.applyMovePriceTx(ctx, tx, scope, actor{p}, now, mp); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return out, nil
}

// applyMovePriceTx replaces the price of one contract in the move transaction: the contract must be at the
// version it was priced at and the new quotes' lists still active (else 409 conflict, the client retries).
func (s *Service) applyMovePriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, now time.Time, mp *movePriced) error {
	c, err := lockContract(ctx, tx, scope, mp.contractID, "", 0)
	if err != nil {
		return err
	}
	if c.Version != mp.version {
		return ErrPriceListChanged
	}
	for _, pr := range mp.prices {
		if err := checkListStillActive(ctx, tx, scope, pr); err != nil {
			return err
		}
	}
	return s.adjustPriceTx(ctx, tx, scope, a, now, c, mp.transit, mp.adjust, false)
}

// adjustPriceTx adds adjustments to the snapshot of a locked contract and moves its total by their deltas
// (INV-27: total_cents = Breakdown.Total + Σ(new − old) of the adjustments), with the reprice audit row and
// *.changed (repriced). A contract without a snapshot, or a total that would go below zero, is refused
// (ErrRepriceRefused): the move or its undo rolls back.
func (s *Service) adjustPriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, now time.Time,
	c *store.Contract, transit bool, adjust []PriceAdjustment, undo bool,
) error {
	snap := DecodeSnapshot(c.PriceSnapshot)
	if snap == nil {
		return ErrRepriceRefused
	}
	var delta int64
	ids := make([]string, 0, len(adjust))
	for _, ad := range adjust {
		delta += ad.NewCents - ad.OldCents
		ids = append(ids, ad.AllocationID)
	}
	old := c.TotalCents
	if old+delta < 0 {
		return ErrRepriceRefused
	}
	snap.Adjustments = append(snap.Adjustments, adjust...)
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	c.PriceSnapshot = raw
	c.TotalCents = old + delta
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	entity, action, typ := entityContract, "contract.reprice", event.ContractChanged
	var data any = event.ContractChangedData{ContractID: c.ID, Change: event.ChangeRepriced}
	if transit {
		entity, action, typ = entityTransit, "transit.reprice", event.TransitChanged
		data = event.TransitChangedData{ContractID: c.ID, Change: event.ChangeRepriced}
	}
	diff := map[string]any{
		"old_total_cents": old, "new_total_cents": c.TotalCents, "price_delta_cents": delta, "allocation_ids": ids,
	}
	if undo {
		diff["undo"] = true
	}
	if err := s.writeAudit(ctx, tx, scope, a, action, entity, c.ID, diff); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, typ, data)
}

// UndoMovePriceTx implements occupancy.MovePricer: in the undo transaction of a repriced move, every adjustment
// the move applied that is still in the contract's snapshot (a stay change re-prices from scratch and drops
// them) gets an opposite move_undo adjustment, so the total is back to what it was before the move (INV-27).
func (s *Service) UndoMovePriceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, now time.Time, raw json.RawMessage) error {
	var undo []movePriceUndo
	if err := json.Unmarshal(raw, &undo); err != nil {
		return fmt.Errorf("decode move price undo: %w", err)
	}
	for _, u := range undo {
		c, err := lockContract(ctx, tx, scope, u.ContractID, "", 0)
		if err != nil {
			return err
		}
		if !slices.Contains(livePriced, Status(c.Status)) {
			continue
		}
		snap := DecodeSnapshot(c.PriceSnapshot)
		if snap == nil {
			continue
		}
		var reverse []PriceAdjustment
		for _, ad := range u.Adjustments {
			if !slices.ContainsFunc(snap.Adjustments, func(x PriceAdjustment) bool { return sameAdjustment(x, ad) }) {
				continue
			}
			reverse = append(reverse, PriceAdjustment{
				Kind: AdjustmentMoveUndo, AllocationID: ad.AllocationID, From: ad.From, To: ad.To,
				OldResourceID: ad.NewResourceID, NewResourceID: ad.OldResourceID, OldCents: ad.NewCents, NewCents: ad.OldCents,
				PriceListID: ad.PriceListID,
			})
		}
		if len(reverse) == 0 {
			continue
		}
		if err := s.adjustPriceTx(ctx, tx, scope, actor{p}, now, c, u.Transit, reverse, true); err != nil {
			return err
		}
	}
	return nil
}

func sameAdjustment(x, y PriceAdjustment) bool {
	return x.Kind == y.Kind && x.AllocationID == y.AllocationID && x.From.Equal(y.From) && x.To.Equal(y.To) &&
		x.OldResourceID == y.OldResourceID && x.NewResourceID == y.NewResourceID && x.OldCents == y.OldCents &&
		x.NewCents == y.NewCents && x.PriceListID == y.PriceListID
}

var _ occupancy.MovePricer = (*Service)(nil)
