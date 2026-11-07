package admin

import (
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Charges (m5-plan §7.3.1): list (JSON, CSV, XLSX), create, detail, edit, cancel, adjust, invoice reference and
// the termination charge of a contract.

// writeDownload answers a file download (no-store; the bytes are built by the service).
func writeDownload(w http.ResponseWriter, name, contentType string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // attachment built by the service, not HTML
}

// contentTypes of the downloads by format.
var contentTypes = map[string]string{
	charge.FormatCSV:  "text/csv; charset=utf-8",
	charge.FormatXLSX: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	charge.FormatPDF:  "application/pdf",
}

func chargeItemLines(d *ctrDates, in []api.ChargeLineInput) []charge.ItemLine {
	out := make([]charge.ItemLine, 0, len(in))
	for _, l := range in {
		il := charge.ItemLine{ItemID: l.ItemId, Qty: qty.Milli(l.QtyMilli), Description: str(l.Description)}
		if l.UnitCents != nil {
			u := money.Cents(*l.UnitCents)
			il.Unit = &u
		}
		if l.DiscountCents != nil {
			il.Discount = money.Cents(*l.DiscountCents)
		}
		if p := d.ptr(l.ServiceFrom); p != nil {
			il.ServiceFrom = *p
		}
		if p := d.ptr(l.ServiceTo); p != nil {
			il.ServiceTo = *p
		}
		out = append(out, il)
	}
	return out
}

func dateOr(p *calendar.Date) calendar.Date {
	if p == nil {
		return calendar.Date{}
	}
	return *p
}

// ListCharges implements api.ServerInterface (GET .../charges).
func (h *Handler) ListCharges(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListChargesParams) {
	d := newCtrDates()
	f := charge.ListFilter{
		PaymentStatus: str(enumPtr(params.PaymentStatus)), InvoicingStatus: str(enumPtr(params.InvoicingStatus)),
		OriginType: str(enumPtr(params.OriginType)), CustomerID: str(params.CustomerId), ContractID: str(params.ContractId),
		IssuedFrom: dateOr(d.ptr(params.IssuedFrom)), IssuedTo: dateOr(d.ptr(params.IssuedTo)), DueFrom: dateOr(d.ptr(params.DueFrom)),
		DueTo: dateOr(d.ptr(params.DueTo)), RevenueClass: str(params.RevenueClass), Number: strings.TrimSpace(str(params.Q)),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	overdue := boolOr(params.Overdue, false)
	if params.Format != nil && *params.Format != api.ListChargesParamsFormatJson {
		format := string(*params.Format)
		b, err := h.Charges.ListFile(r.Context(), principal(r), marinaID, f, overdue, format)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeDownload(w, "addebiti."+format, contentTypes[format], b)
		return
	}
	page, err := h.Charges.List(r.Context(), principal(r), marinaID, f, overdue, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ChargePage(page.Items, page.NextCursor))
}

// CreateCharge implements api.ServerInterface (POST .../charges).
func (h *Handler) CreateCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateChargeParams) {
	var in api.ChargeCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	mi := charge.ManualInput{
		OriginType: string(in.OriginType), CustomerID: str(in.CustomerId), ContractID: str(in.ContractId), VesselID: str(in.VesselId),
		DueOn: dateOr(d.ptr(in.DueOn)), Description: str(in.Description), Lines: chargeItemLines(d, in.Lines),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	if mi.OriginType == "counter_sale" && mi.CustomerID == "" {
		mi.FiscalMode = charge.FiscalReceipt
	}
	det, err := h.Charges.Create(r.Context(), principal(r), marinaID, mi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusCreated, view.ChargeDetail(det))
}

// GetCharge implements api.ServerInterface (GET .../charges/{chargeId}).
func (h *Handler) GetCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeId) {
	det, err := h.Charges.Get(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusOK, view.ChargeDetail(det))
}

// UpdateCharge implements api.ServerInterface (PATCH .../charges/{chargeId}).
func (h *Handler) UpdateCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeId, params api.UpdateChargeParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ChargePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	e := charge.EditInput{CustomerID: in.CustomerId, Description: in.Description, DueOn: d.ptr(in.DueOn)}
	if in.Lines != nil {
		e.Lines = chargeItemLines(d, *in.Lines)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	det, err := h.Charges.Update(r.Context(), principal(r), marinaID, id, ifMatch, e)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusOK, view.ChargeDetail(det))
}

// CancelCharge implements api.ServerInterface (POST .../charges/{chargeId}/cancel).
func (h *Handler) CancelCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeId, params api.CancelChargeParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ChargeCancel
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	det, err := h.Charges.Cancel(r.Context(), principal(r), marinaID, id, ifMatch, in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusOK, view.ChargeDetail(det))
}

// AdjustCharge implements api.ServerInterface (POST .../charges/{chargeId}/adjust).
func (h *Handler) AdjustCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeId, params api.AdjustChargeParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ChargeAdjust
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	lines := make([]charge.ItemLine, 0, len(in.Lines))
	for _, l := range in.Lines {
		il := charge.ItemLine{ItemID: l.ItemId, Qty: qty.Milli(l.QtyMilli), Description: str(l.Description)}
		if l.UnitCents != nil {
			u := money.Cents(*l.UnitCents)
			il.Unit = &u
		}
		il.ServiceFrom, il.ServiceTo = dateOr(d.ptr(l.ServiceFrom)), dateOr(d.ptr(l.ServiceTo))
		lines = append(lines, il)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	out, err := h.Charges.Adjust(r.Context(), principal(r), marinaID, id, ifMatch, charge.AdjustInput{
		Mode: string(in.Mode), Lines: lines, Reason: in.Reason,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	res := api.AdjustResult{Adjustments: make([]api.Charge, 0, len(out))}
	for i := range out {
		res.Adjustments = append(res.Adjustments, view.ChargeDetail(&out[i]))
	}
	respond.JSON(w, http.StatusCreated, res)
}

// SetChargeInvoice implements api.ServerInterface (PUT .../charges/{chargeId}/invoice).
func (h *Handler) SetChargeInvoice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ChargeId) {
	var in api.InvoiceRefInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	date := d.parse(in.Date)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	det, err := h.Charges.SetInvoice(r.Context(), principal(r), marinaID, id, in.Number, date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusOK, view.ChargeDetail(det))
}

// CreateTerminationCharge implements api.ServerInterface (POST .../contracts/{contractId}/termination-charge).
func (h *Handler) CreateTerminationCharge(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.CreateTerminationChargeParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	det, err := h.Charges.CreateTermination(r.Context(), principal(r), marinaID, contractID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, det.Charge.Version)
	respond.JSON(w, http.StatusCreated, view.ChargeDetail(det))
}
