package public

import (
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Public pay page of payment requests (m5-plan §5.5): the token is the only credential, never logged; answers are
// never cached and carry no referrer. Unknown, expired and cancelled links are the same 404 link_invalid. The
// return page polls the status: the browser return never confirms a payment (INV-09).

func payHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// GetPayPage implements api.ServerInterface.
func (h *Handler) GetPayPage(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.PayToken) {
	payHeaders(w)
	if h.Collect == nil {
		respond.Problem(w, r, problem.LinkInvalid)
		return
	}
	page, err := h.Collect.GetPayPage(r.Context(), slug, token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PayPage(page))
}

// StartPayCheckout implements api.ServerInterface.
func (h *Handler) StartPayCheckout(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.PayToken) {
	payHeaders(w)
	if h.Collect == nil {
		respond.Problem(w, r, problem.LinkInvalid)
		return
	}
	var in api.PayCheckoutInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	url, err := h.Collect.StartPayCheckout(r.Context(), slug, token, string(in.Method))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PayCheckoutResult{RedirectUrl: url})
}

// GetPayStatus implements api.ServerInterface.
func (h *Handler) GetPayStatus(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.PayToken) {
	payHeaders(w)
	if h.Collect == nil {
		respond.Problem(w, r, problem.LinkInvalid)
		return
	}
	status, verifying, err := h.Collect.PayStatus(r.Context(), slug, token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PayStatus{Status: api.PaymentRequestStatus(status), Verifying: verifying})
}
