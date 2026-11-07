package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Pricing tool handlers (m2-plan §4.5): simulator, matrix, coverage.

// SimulatePrice implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/pricing/simulate).
func (h *Handler) SimulatePrice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.PricingSimulateRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	req, err := view.SimulateRequestIn(in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	b, res, err := h.PriceLists.Quote(r.Context(), principal(r), marinaID, req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PriceBreakdown(b, &res))
}

// GetPriceMatrix implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/matrix).
func (h *Handler) GetPriceMatrix(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.GetPriceMatrixParams) {
	quotedOn, ok := dateParam(w, r, params.QuotedOn)
	if !ok {
		return
	}
	mp := pricelist.MatrixParams{ItemID: str(params.ItemId), QuotedOn: quotedOn}
	if params.ContractType != nil {
		mp.ContractType = pricing.ContractType(*params.ContractType)
	}
	if params.Rows != nil {
		mp.Rows = string(*params.Rows)
	}
	if params.Channel != nil {
		mp.Channel = pricing.Channel(*params.Channel)
	}
	if params.CustomerCategory != nil {
		mp.CustomerCategory = string(*params.CustomerCategory)
	}
	m, err := h.PriceLists.Matrix(r.Context(), principal(r), marinaID, id, mp)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PriceMatrix(m))
}

// GetPriceCoverage implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/coverage).
func (h *Handler) GetPriceCoverage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.GetPriceCoverageParams) {
	quotedOn, ok := dateParam(w, r, params.QuotedOn)
	if !ok {
		return
	}
	c, err := h.PriceLists.Coverage(r.Context(), principal(r), marinaID, id, quotedOn)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PriceCoverage(c))
}
