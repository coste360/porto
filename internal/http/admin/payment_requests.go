package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Payment request handlers (m5-plan §7.3.3): requests with their public link, bulk requests, send, cancel and the
// payment link of a held transit.

func (h *Handler) requestNames(r *http.Request, marinaID string, views ...collect.RequestView) map[string]string {
	ids := make([]string, 0, len(views))
	for _, v := range views {
		ids = append(ids, v.Request.CustomerID)
	}
	return h.Collect.CustomerNames(r.Context(), principal(r), marinaID, ids)
}

// ListPaymentRequests implements api.ServerInterface.
func (h *Handler) ListPaymentRequests(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListPaymentRequestsParams) {
	f := collect.RequestFilter{Status: str(enumPtr(params.Status)), CustomerID: str(params.CustomerId)}
	page, err := h.Collect.ListRequests(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PaymentRequestPage(page, h.requestNames(r, marinaID, page.Items...)))
}

// CreatePaymentRequest implements api.ServerInterface.
func (h *Handler) CreatePaymentRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreatePaymentRequestParams) {
	var in api.PaymentRequestCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	var exp calendar.Date
	if in.ExpiresOn != nil {
		d, ok := dateParam(w, r, in.ExpiresOn)
		if !ok {
			return
		}
		exp = d
	}
	v, err := h.Collect.CreateRequest(r.Context(), principal(r), marinaID, collect.CreateRequestInput{
		CustomerID: in.CustomerId, ChargeIDs: in.ChargeIds, ExpiresOn: exp, Send: boolOr(in.Send, false),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, v.Request.Version)
	respond.JSON(w, http.StatusCreated, view.PaymentRequestCreated(v, h.requestNames(r, marinaID, *v)))
}

// CreatePaymentRequestsBulk implements api.ServerInterface.
func (h *Handler) CreatePaymentRequestsBulk(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreatePaymentRequestsBulkParams) {
	var in api.PaymentRequestBulk
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Collect.CreateRequestsBulk(r.Context(), principal(r), marinaID, in.ChargeIds, in.Send)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	ids := res.RequestIDs
	if ids == nil {
		ids = []string{}
	}
	respond.JSON(w, http.StatusCreated, api.PaymentRequestBulkResult{Created: res.Created, Skipped: res.Skipped, RequestIds: &ids})
}

// GetPaymentRequest implements api.ServerInterface.
func (h *Handler) GetPaymentRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PaymentRequestId) {
	v, err := h.Collect.GetRequest(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, v.Request.Version)
	respond.JSON(w, http.StatusOK, view.PaymentRequest(*v, h.requestNames(r, marinaID, *v)))
}

// SendPaymentRequest implements api.ServerInterface.
func (h *Handler) SendPaymentRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PaymentRequestId, params api.SendPaymentRequestParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PaymentRequestSend
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	v, err := h.Collect.SendRequest(r.Context(), principal(r), marinaID, id, ifMatch, boolOr(in.Rotate, false))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, v.Request.Version)
	respond.JSON(w, http.StatusOK, view.PaymentRequestCreated(v, h.requestNames(r, marinaID, *v)))
}

// CancelPaymentRequest implements api.ServerInterface.
func (h *Handler) CancelPaymentRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PaymentRequestId, params api.CancelPaymentRequestParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Collect.CancelRequest(r.Context(), principal(r), marinaID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, v.Request.Version)
	respond.JSON(w, http.StatusOK, view.PaymentRequest(*v, h.requestNames(r, marinaID, *v)))
}

// CreateTransitPaymentLink implements api.ServerInterface.
func (h *Handler) CreateTransitPaymentLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.CreateTransitPaymentLinkParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.TransitPaymentLinkInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	v, err := h.Collect.CreateTransitLink(r.Context(), principal(r), marinaID, contractID, ifMatch, collect.TransitLinkInput{
		ExpiresInHours: intOr(in.ExpiresInHours, 0), Send: boolOr(in.Send, false),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.TransitPaymentLink{Request: view.PaymentRequest(*v, h.requestNames(r, marinaID, *v)), Url: v.URL})
}
