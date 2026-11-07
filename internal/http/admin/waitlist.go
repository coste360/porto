package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Waiting list operations (m4-plan §7.3.1).

func writeWaitlist(w http.ResponseWriter, e *contract.WaitlistRow, status int) {
	respond.SetETag(w, e.Version)
	respond.JSON(w, status, view.WaitlistEntry(e))
}

// ListWaitlist implements api.ServerInterface (GET .../waitlist).
func (h *Handler) ListWaitlist(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListWaitlistParams) {
	rows, err := h.Contracts.ListWaitlist(r.Context(), principal(r), marinaID, str(enumPtr(params.Status)), str(enumPtr(params.Term)), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.WaitlistPage{Items: make([]api.WaitlistEntry, 0, len(rows))}
	for i := range rows {
		out.Items = append(out.Items, view.WaitlistEntry(&rows[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateWaitlistEntry implements api.ServerInterface (POST .../waitlist).
func (h *Handler) CreateWaitlistEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateWaitlistEntryParams) {
	var in api.WaitlistInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	wi := contract.WaitlistInput{
		CustomerID: in.CustomerId, VesselID: str(in.VesselId), Term: string(in.Term), RequestedFrom: d.parse(in.RequestedFrom),
		Priority: intOr(in.Priority, 0), Notes: str(in.Notes),
	}
	if in.LoaCm != nil {
		wi.LOA = *in.LoaCm
	}
	if in.BeamCm != nil {
		wi.Beam = *in.BeamCm
	}
	if in.DraftCm != nil {
		wi.Dra = *in.DraftCm
	}
	if in.GroupIds != nil {
		wi.GroupIDs = *in.GroupIds
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	e, err := h.Contracts.CreateWaitlistEntry(r.Context(), principal(r), marinaID, wi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeWaitlist(w, e, http.StatusCreated)
}

// UpdateWaitlistEntry implements api.ServerInterface (PATCH .../waitlist/{waitlistId}).
func (h *Handler) UpdateWaitlistEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.WaitlistId, params api.UpdateWaitlistEntryParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.WaitlistPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	e, err := h.Contracts.UpdateWaitlistEntry(r.Context(), principal(r), marinaID, id, ifMatch, contract.WaitlistPatch{
		GroupIDs: in.GroupIds, Notes: in.Notes, Priority: in.Priority, Status: enumPtr(in.Status),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeWaitlist(w, e, http.StatusOK)
}

// QuoteFromWaitlist implements api.ServerInterface (POST .../waitlist/{waitlistId}/quote).
func (h *Handler) QuoteFromWaitlist(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.WaitlistId, _ api.QuoteFromWaitlistParams) {
	ifMatch := 0
	var in api.QuoteInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	qi := ctrQuoteInput(d, in)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	q, err := h.Contracts.QuoteFromWaitlist(r.Context(), principal(r), marinaID, id, ifMatch, qi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeQuote(w, q, http.StatusCreated)
}
