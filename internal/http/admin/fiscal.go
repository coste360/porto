package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Fiscal handlers (m5-plan §7.3.3): corrispettivi (JSON, CSV, Excel) and fiscal documents (list, retry, void).

// GetFiscalDailyTotals implements api.ServerInterface. from and to are required (optional in the contract so the
// cross-marina sweep reaches the marina check first).
func (h *Handler) GetFiscalDailyTotals(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetFiscalDailyTotalsParams) {
	from, ok1 := dateParam(w, r, params.From)
	if !ok1 {
		return
	}
	to, ok2 := dateParam(w, r, params.To)
	if !ok2 {
		return
	}
	t, err := h.Fiscal.DailyTotals(r.Context(), principal(r), marinaID, from, to, str(enumPtr(params.Group)))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	name := "corrispettivi_" + from.String() + "_" + to.String()
	switch str(enumPtr(params.Format)) {
	case "csv":
		b := fiscal.TotalsCSV(t)
		cashDownload(w, "text/csv; charset=utf-8", name+".csv", b)
	case "xlsx":
		b, err := fiscal.TotalsXLSX(t)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		cashDownload(w, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", name+".xlsx", b)
	default:
		respond.JSON(w, http.StatusOK, view.FiscalTotals(t))
	}
}

// ListFiscalDocuments implements api.ServerInterface.
func (h *Handler) ListFiscalDocuments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListFiscalDocumentsParams) {
	from, ok1 := dateParam(w, r, params.From)
	if !ok1 {
		return
	}
	to, ok2 := dateParam(w, r, params.To)
	if !ok2 {
		return
	}
	f := fiscal.ListFilter{From: from, To: to, Status: str(enumPtr(params.Status))}
	page, err := h.Fiscal.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.FiscalDocuments(page))
}

// RetryFiscalDocument implements api.ServerInterface.
func (h *Handler) RetryFiscalDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FiscalDocumentId) {
	d, err := h.Fiscal.Retry(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PaymentFiscalDocument(*d))
}

// VoidFiscalDocument implements api.ServerInterface.
func (h *Handler) VoidFiscalDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FiscalDocumentId) {
	d, err := h.Fiscal.Void(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PaymentFiscalDocument(*d))
}
