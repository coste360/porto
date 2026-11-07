package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Inquiry operations (m3-plan §7.3).

func writeInquiry(w http.ResponseWriter, v *contract.InquiryView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, view.Inquiry(v))
}

// ListInquiries implements api.ServerInterface (GET .../inquiries).
func (h *Handler) ListInquiries(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListInquiriesParams) {
	d := newCtrDates()
	f := contract.InquiryFilter{Outcome: str(enumPtr(params.Outcome)), From: d.ptr(params.From), To: d.ptr(params.To)}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Contracts.ListInquiries(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	items := make([]api.Inquiry, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, view.Inquiry(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, api.InquiryPage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}

// CreateInquiry implements api.ServerInterface (POST .../inquiries).
func (h *Handler) CreateInquiry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateInquiryParams) {
	var in api.InquiryCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ii := contract.InquiryInput{
		Purpose: string(in.Purpose), Term: str(enumPtr(in.Term)), Source: string(in.Source), Channel: str(enumPtr(in.Channel)),
		VesselID: str(in.VesselId), Arrival: d.parse(in.Arrival), Departure: d.parse(in.Departure),
		CustomerID: str(in.CustomerId), Outcome: str(enumPtr(in.Outcome)), Notes: str(in.Notes),
	}
	if in.LoaCm != nil {
		ii.LOACm = *in.LoaCm
	}
	if in.BeamCm != nil {
		ii.BeamCm = *in.BeamCm
	}
	if in.DraftCm != nil {
		ii.DraftCm = *in.DraftCm
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	v, err := h.Contracts.CreateInquiry(r.Context(), principal(r), marinaID, ii)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeInquiry(w, v, http.StatusCreated)
}

// UpdateInquiry implements api.ServerInterface (PATCH .../inquiries/{inquiryId}).
func (h *Handler) UpdateInquiry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, inquiryID api.InquiryId, params api.UpdateInquiryParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.InquiryPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v, err := h.Contracts.UpdateInquiry(r.Context(), principal(r), marinaID, inquiryID, ifMatch, contract.InquiryPatch{
		Outcome: enumPtr(in.Outcome), QuoteID: in.QuoteId, ContractID: in.ContractId, Notes: in.Notes,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeInquiry(w, v, http.StatusOK)
}
