package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Scadenzario, receivables overview and the payment lens (m5-plan §7.3.2).

// xlsxType is the media type of Excel workbooks.
const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// writeAttachment answers a binary download.
func writeAttachment(w http.ResponseWriter, contentType, name string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // attachment built by the service, not HTML
}

// ListReceivables implements api.ServerInterface (GET .../receivables).
func (h *Handler) ListReceivables(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListReceivablesParams) {
	d := newCtrDates()
	f := payment.ReceivableFilter{
		Bucket: str(enumPtr(params.Bucket)), Status: str(enumPtr(params.Status)), CustomerID: str(params.CustomerId),
		ContractID: str(params.ContractId), GroupID: str(params.GroupId), OriginType: str(enumPtr(params.OriginType)),
	}
	if params.DueFrom != nil {
		f.DueFrom = d.parse(*params.DueFrom)
	}
	if params.DueTo != nil {
		f.DueTo = d.parse(*params.DueTo)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format != api.ListReceivablesParamsFormatJson {
		rows, _, err := h.Payments.AllReceivables(ctx, p, marinaID, f)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		if *params.Format == api.ListReceivablesParamsFormatCsv {
			writeCSV(w, "scadenzario.csv", payment.ReceivablesCSV(rows))
			return
		}
		b, err := payment.ReceivablesXLSX(rows)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeAttachment(w, xlsxType, "scadenzario.xlsx", b)
		return
	}
	page, err := h.Payments.ListReceivables(ctx, p, marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ReceivablePage{
		Items: view.Receivables(page.Items), NextCursor: nextCursor(page.NextCursor), Totals: view.AgeingAmounts(page.Totals),
	})
}

// GetReceivablesAgeing implements api.ServerInterface (GET .../receivables/ageing).
func (h *Handler) GetReceivablesAgeing(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetReceivablesAgeingParams) {
	rep, err := h.Payments.ReceivablesAgeing(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	switch {
	case params.Format != nil && *params.Format == api.GetReceivablesAgeingParamsFormatCsv:
		writeCSV(w, "sospesi.csv", payment.AgeingCSV(rep))
	case params.Format != nil && *params.Format == api.GetReceivablesAgeingParamsFormatXlsx:
		b, err := payment.AgeingXLSX(rep)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeAttachment(w, xlsxType, "sospesi.xlsx", b)
	default:
		respond.JSON(w, http.StatusOK, view.AgeingReport(rep))
	}
}

// GetMapPaymentStatus implements api.ServerInterface (GET .../maps/{mapId}/payment-status).
func (h *Handler) GetMapPaymentStatus(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.GetMapPaymentStatusParams) {
	d := newCtrDates()
	night := d.parse(str(params.Date))
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	on, rs, err := h.Payments.MapPaymentStatus(r.Context(), principal(r), marinaID, mapID, night)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MapPaymentStatus(on, rs))
}
