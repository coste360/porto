package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Customer statement of account and dashboard (m5-plan §7.3.2).

// GetCustomerStatement implements api.ServerInterface (GET .../customers/{customerId}/statement).
func (h *Handler) GetCustomerStatement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, params api.GetCustomerStatementParams) {
	d := newCtrDates()
	from, to := d.parse(str(params.From)), d.parse(str(params.To))
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format == api.GetCustomerStatementParamsFormatPdf {
		b, err := h.Payments.StatementPDF(ctx, p, marinaID, customerID, from, to)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeAttachment(w, "application/pdf", "estratto-conto.pdf", b)
		return
	}
	st, err := h.Payments.CustomerStatement(ctx, p, marinaID, customerID, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if params.Format != nil && *params.Format == api.GetCustomerStatementParamsFormatCsv {
		writeCSV(w, "estratto-conto.csv", payment.StatementCSV(st))
		return
	}
	respond.JSON(w, http.StatusOK, view.Statement(st))
}

// GetCustomerDashboard implements api.ServerInterface (GET .../customers/{customerId}/dashboard).
func (h *Handler) GetCustomerDashboard(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	d, err := h.Payments.CustomerDashboard(r.Context(), principal(r), marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Dashboard(d))
}
