package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// Customer credits (m5-plan §7.3.2).

// ListCredits implements api.ServerInterface (GET .../credits).
func (h *Handler) ListCredits(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListCreditsParams) {
	page, err := h.Payments.ListCredits(r.Context(), principal(r), marinaID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.CreditPage{Items: view.CreditRows(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// GetCustomerCredit implements api.ServerInterface (GET .../customers/{customerId}/credits).
func (h *Handler) GetCustomerCredit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	row, pays, err := h.Payments.CustomerCredit(r.Context(), principal(r), marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.CustomerCredit(row, pays))
}

// GrantCustomerCredit implements api.ServerInterface (POST .../customers/{customerId}/credits).
func (h *Handler) GrantCustomerCredit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, params api.GrantCustomerCreditParams) {
	var in api.CreditGrantInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p, err := h.Payments.GrantCredit(r.Context(), principal(r), marinaID, customerID, payment.CreditInput{
		Amount: money.Cents(in.AmountCents), Reason: in.Reason, Source: string(in.Source), ReleaseID: str(in.ReleaseId),
	}, str(params.IdempotencyKey))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePayment(w, p, http.StatusCreated)
}

// ApplyCustomerCredit implements api.ServerInterface (POST .../customers/{customerId}/credits/apply).
func (h *Handler) ApplyCustomerCredit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, _ api.ApplyCustomerCreditParams) {
	var in *api.ApplyCreditInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	var ids []string
	if in != nil && in.ChargeIds != nil {
		ids = *in.ChargeIds
	}
	total, err := h.Payments.ApplyCredit(r.Context(), principal(r), marinaID, customerID, ids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ApplyCreditResult{MatchedCents: int64(total)})
}
