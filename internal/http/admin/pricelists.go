package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Price list handlers (m2-plan §4.4).

// dateParam parses an optional YYYY-MM-DD query parameter; an invalid value is a 400.
func dateParam(w http.ResponseWriter, r *http.Request, p *string) (calendar.Date, bool) {
	if p == nil || *p == "" {
		return calendar.Date{}, true
	}
	d, err := calendar.Parse(*p)
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return calendar.Date{}, false
	}
	return d, true
}

// badPricingBody answers a body whose shape the domain refuses (e.g. an invalid date): another marina's id is
// still 404 first, so that the existence of a marina never leaks through a 400.
func (h *Handler) badPricingBody(w http.ResponseWriter, r *http.Request, marinaID string) {
	if _, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.Error(w, r, respond.ErrBadJSON)
}

func (h *Handler) decodePriceListSave(w http.ResponseWriter, r *http.Request, marinaID string, optional bool) (*pricelist.SaveRequest, bool) {
	var in *api.PriceListSave
	if !respond.DecodeOrFail(w, r, &in, optional) {
		return nil, false
	}
	if in == nil {
		return nil, true
	}
	req, err := view.PriceListSaveIn(*in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return nil, false
	}
	return &req, true
}

// ListPriceLists implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/price-lists).
func (h *Handler) ListPriceLists(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListPriceListsParams) {
	on, ok := dateParam(w, r, params.On)
	if !ok {
		return
	}
	f := pricelist.ListFilter{On: on}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.ContractType != nil {
		f.ContractType = string(*params.ContractType)
	}
	list, err := h.PriceLists.List(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PriceListSummaries(list))
}

// CreatePriceList implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/price-lists).
func (h *Handler) CreatePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreatePriceListParams) {
	var in api.PriceListCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	from, err1 := calendar.Parse(in.ValidFrom)
	to, err2 := calendar.Parse(in.ValidTo)
	if err1 != nil || err2 != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	cts := make([]pricing.ContractType, len(in.ContractTypes))
	for i, c := range in.ContractTypes {
		cts[i] = pricing.ContractType(c)
	}
	d, err := h.PriceLists.Create(r.Context(), principal(r), marinaID, pricelist.CreateInput{
		Code: in.Code, Name: catalog.I18nText{It: in.Name.It, En: in.Name.En}, ValidFrom: from, ValidTo: to, ContractTypes: cts,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusCreated, view.PriceListDocument(d))
}

// GetPriceList implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}).
func (h *Handler) GetPriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId) {
	d, err := h.PriceLists.Get(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, view.PriceListDocument(d))
}

// SavePriceList implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}).
func (h *Handler) SavePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.SavePriceListParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	req, ok := h.decodePriceListSave(w, r, marinaID, false)
	if !ok {
		return
	}
	res, err := h.PriceLists.Save(r.Context(), principal(r), marinaID, id, version, *req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Document.Version)
	respond.JSON(w, http.StatusOK, view.PriceListSaveResult(res))
}

// ValidatePriceList implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/validate).
func (h *Handler) ValidatePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId) {
	req, ok := h.decodePriceListSave(w, r, marinaID, true)
	if !ok {
		return
	}
	issues, err := h.PriceLists.Validate(r.Context(), principal(r), marinaID, id, req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PriceListValidation{Issues: view.PriceListIssues(issues)})
}

// DeletePriceList implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}).
func (h *Handler) DeletePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.DeletePriceListParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.PriceLists.Delete(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ActivatePriceList implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/activate).
func (h *Handler) ActivatePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.ActivatePriceListParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PriceListActivate
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	eff, ok := dateParam(w, r, in.EffectiveFrom)
	if !ok {
		return
	}
	res, err := h.PriceLists.Activate(r.Context(), principal(r), marinaID, id, version, eff)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Document.Version)
	respond.JSON(w, http.StatusOK, view.PriceListActivateResult(res))
}

// ArchivePriceList implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/archive).
func (h *Handler) ArchivePriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, params api.ArchivePriceListParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	d, err := h.PriceLists.Archive(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, view.PriceListDocument(d))
}

// RenewPriceList implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/renewal).
func (h *Handler) RenewPriceList(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PriceListId, _ api.RenewPriceListParams) {
	var in api.RenewalRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	req, err := view.RenewalRequestIn(in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	preview, doc, err := h.PriceLists.Renew(r.Context(), principal(r), marinaID, id, req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if preview != nil {
		respond.JSON(w, http.StatusOK, view.RenewalPreview(preview))
		return
	}
	respond.SetETag(w, doc.Version)
	respond.JSON(w, http.StatusCreated, view.PriceListDocument(doc))
}
