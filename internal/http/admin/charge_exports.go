package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// "Da fatturare" exports, invoice reconciliations and the payments export (m5-plan §7.3.1).

func exportFilterIn(d *ctrDates, from, to string, customerID *string, origins *[]api.ChargeOriginType, class *string) charge.ExportFilter {
	f := charge.ExportFilter{PeriodFrom: d.parse(from), PeriodTo: d.parse(to), CustomerID: str(customerID), RevenueClass: str(class)}
	if origins != nil {
		for _, o := range *origins {
			f.OriginTypes = append(f.OriginTypes, string(o))
		}
	}
	return f
}

// ListChargeExports implements api.ServerInterface (GET .../charge-exports).
func (h *Handler) ListChargeExports(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListChargeExportsParams) {
	page, err := h.Charges.ListExports(r.Context(), principal(r), marinaID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ChargeExportPage(page))
}

// PreviewChargeExport implements api.ServerInterface (POST .../charge-exports/preview).
func (h *Handler) PreviewChargeExport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.ExportFilter
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	f := exportFilterIn(d, in.PeriodFrom, in.PeriodTo, in.CustomerId, in.OriginTypes, in.RevenueClass)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	p, err := h.Charges.PreviewExport(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ExportPreview(p))
}

// CreateChargeExport implements api.ServerInterface (POST .../charge-exports).
func (h *Handler) CreateChargeExport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateChargeExportParams) {
	var in api.ExportCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	f := exportFilterIn(d, in.PeriodFrom, in.PeriodTo, in.CustomerId, in.OriginTypes, in.RevenueClass)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	e, err := h.Charges.CreateExport(r.Context(), principal(r), marinaID, f, str(in.ExporterCode))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, e.Version)
	respond.JSON(w, http.StatusCreated, view.ChargeExport(e))
}

// ReexportCharges implements api.ServerInterface (POST .../charge-exports/reexport).
func (h *Handler) ReexportCharges(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.ReexportChargesParams) {
	var in api.ReexportInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ri := charge.ReexportInput{ExportID: str(in.ExportId)}
	if in.ChargeIds != nil {
		ri.ChargeIDs = *in.ChargeIds
	}
	e, err := h.Charges.Reexport(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, e.Version)
	respond.JSON(w, http.StatusCreated, view.ChargeExport(e))
}

// GetChargeExport implements api.ServerInterface (GET .../charge-exports/{chargeExportId}).
func (h *Handler) GetChargeExport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeExportId, params api.GetChargeExportParams) {
	d, err := h.Charges.GetExport(r.Context(), principal(r), marinaID, id, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Export.Version)
	respond.JSON(w, http.StatusOK, view.ChargeExportDetail(d))
}

// DownloadChargeExport implements api.ServerInterface (GET .../charge-exports/{chargeExportId}/file). The format
// is required: it is checked by the service after authorisation (so another marina is 404 first).
func (h *Handler) DownloadChargeExport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeExportId, params api.DownloadChargeExportParams) {
	format := str(enumPtr(params.Format))
	b, number, err := h.Charges.DownloadExport(r.Context(), principal(r), marinaID, id, format)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDownload(w, "esportazione-"+number+"."+format, contentTypes[format], b)
}

// ListInvoiceReconciliations implements api.ServerInterface (GET .../invoice-reconciliations).
func (h *Handler) ListInvoiceReconciliations(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListInvoiceReconciliationsParams) {
	page, err := h.Charges.ListReconciliations(r.Context(), principal(r), marinaID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.InvoiceReconciliationPage(page))
}

// CreateInvoiceReconciliation implements api.ServerInterface (POST .../invoice-reconciliations).
func (h *Handler) CreateInvoiceReconciliation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateInvoiceReconciliationParams) {
	var in api.InvoiceReconciliationInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ri := charge.ReconcileInput{CSV: str(in.Csv), DryRun: in.DryRun}
	if in.Rows != nil {
		for i, row := range *in.Rows {
			ri.Rows = append(ri.Rows, charge.ReconcileRow{
				Row: i + 1, ChargeID: str(row.ChargeId), ChargeNumber: str(row.ChargeNumber), InvoiceNumber: row.InvoiceNumber,
				InvoiceDate: row.InvoiceDate,
			})
		}
	}
	rec, report, err := h.Charges.Reconcile(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.InvoiceReconciliation(rec, report))
}

// ExportPayments implements api.ServerInterface (GET .../payments/export). from, to and format are required and
// checked by the service after authorisation.
func (h *Handler) ExportPayments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ExportPaymentsParams) {
	d := newCtrDates()
	from, to := dateOr(d.ptr(params.From)), dateOr(d.ptr(params.To))
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	format := str(enumPtr(params.Format))
	b, err := h.Charges.PaymentsFile(r.Context(), principal(r), marinaID, from, to, str(enumPtr(params.Method)), format)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDownload(w, "incassi-"+from.String()+"-"+to.String()+"."+format, contentTypes[format], b)
}
