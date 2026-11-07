package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// SEPA mandate handlers (m5-plan §7.3.3): list, request (setup link) and revoke.

// ListSepaMandates implements api.ServerInterface.
func (h *Handler) ListSepaMandates(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	ms, err := h.Collect.ListMandates(r.Context(), principal(r), marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.SepaMandates(ms))
}

// CreateSepaMandate implements api.ServerInterface.
func (h *Handler) CreateSepaMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, _ api.CreateSepaMandateParams) {
	var in api.SepaMandateCreate
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	m, err := h.Collect.RequestMandate(r.Context(), principal(r), marinaID, customerID, str(in.ProviderCode), boolOr(in.Send, false))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.SepaMandateCreated{Mandate: view.SepaMandate(&m.Mandate), SetupUrl: m.SetupURL})
}

// RevokeSepaMandate implements api.ServerInterface.
func (h *Handler) RevokeSepaMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, id api.SepaMandateId) {
	m, err := h.Collect.RevokeMandate(r.Context(), principal(r), marinaID, customerID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.SepaMandate(m))
}
