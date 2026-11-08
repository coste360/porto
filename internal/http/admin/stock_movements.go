package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Material issues and returns (M10, m10-plan §8, §13). Owned by ORDERS.

func woMovement(v *workorder.MovementView) api.StockMovement {
	m := v.Row
	return api.StockMovement{
		Id: m.ID, WorkOrderId: m.WorkOrderID, TaskId: view.String(m.TaskID), ItemId: m.ItemID, ItemCode: view.String(v.ItemCode),
		Kind: api.StockMovementKind(m.Kind), QtyMilli: m.QtyMilli, UnitCostCents: m.UnitCostCents, UnitPriceCents: m.UnitPriceCents,
		PriceOverridden: m.PriceOverridden, MovedOn: m.MovedOn.String(), Note: m.Note, Late: m.Late, CreatedAt: m.CreatedAt.UTC(),
	}
}

// ListStockMovements implements api.ServerInterface.
func (h *Handler) ListStockMovements(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListStockMovementsParams) {
	d := newCtrDates()
	f := workorder.MovementFilter{
		WorkOrderID: str(params.WorkOrderId), ItemID: str(params.ItemId), From: d.parse(str(params.From)), To: d.parse(str(params.To)),
		Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	items, next, err := h.WorkOrders.ListMovements(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.StockMovement, 0, len(items))
	for i := range items {
		out = append(out, woMovement(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.StockMovementPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateStockMovement implements api.ServerInterface.
func (h *Handler) CreateStockMovement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateStockMovementParams) {
	var in api.StockMovementInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	mi := workorder.MovementInput{
		WorkOrderID: in.WorkOrderId, TaskID: str(in.TaskId), ItemID: in.ItemId, Kind: string(in.Kind), Qty: qty.Milli(in.QtyMilli),
		MovedOn: d.parse(str(in.MovedOn)), Note: str(in.Note),
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.UnitCostCents != nil {
		c := money.Cents(*in.UnitCostCents)
		mi.UnitCost = &c
	}
	if in.UnitPriceCents != nil {
		p := money.Cents(*in.UnitPriceCents)
		mi.UnitPrice = &p
	}
	v, err := h.WorkOrders.CreateMovement(r.Context(), principal(r), marinaID, mi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, woMovement(v))
}
