package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// ListCollectibles implements api.ServerInterface (GET .../collectibles, m6b-plan §5.4): the open charges the dock
// can collect on a date. Customer names only with customers.read.
func (h *Handler) ListCollectibles(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListCollectiblesParams) {
	d := newCtrDates()
	date := d.ptr(params.Date)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Payments.Collectibles(r.Context(), principal(r), marinaID, date, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	items := make([]api.Collectible, 0, len(page.Items))
	for _, c := range page.Items {
		it := api.Collectible{
			ChargeId: c.ChargeID, Number: c.Number, ContractId: c.ContractID, ContractCode: c.ContractCode,
			CustomerId: c.CustomerID, Description: c.Description, DueOn: api.IsoDate(c.DueOn.String()),
			FiscalMode: api.FiscalMode(c.FiscalMode), ResidualCents: api.Cents(c.Residual),
			Vessel: api.CollectibleVessel{Id: c.VesselID, Name: c.VesselName},
		}
		if c.CustomerName != "" {
			name := c.CustomerName
			it.CustomerDisplayName = &name
		}
		items = append(items, it)
	}
	respond.JSON(w, http.StatusOK, api.CollectiblePage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}
