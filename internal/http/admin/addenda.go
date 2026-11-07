package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Addenda operations (m4-plan §7.3.1): create with price effect, request signature, activate on paper, cancel.

func writeAddendum(w http.ResponseWriter, a *contract.Addendum, status int) {
	respond.SetETag(w, a.Row.Version)
	respond.JSON(w, status, view.Addendum(a))
}

// ListAddenda implements api.ServerInterface (GET .../contracts/{contractId}/addenda).
func (h *Handler) ListAddenda(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	as, err := h.Contracts.ListAddenda(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AddendumList{Items: make([]api.Addendum, 0, len(as))}
	for i := range as {
		out.Items = append(out.Items, view.Addendum(&as[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateAddendum implements api.ServerInterface (POST .../contracts/{contractId}/addenda).
func (h *Handler) CreateAddendum(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, _ api.CreateAddendumParams) {
	var in api.AddendumInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	pl := in.Payload
	ai := contract.AddendumInput{
		Kind: string(in.Kind), EffectiveOn: d.parse(in.EffectiveOn), FitOverride: ctrFit(in.FitOverride),
		Payload: contract.AddendumPayload{
			ItemID: str(pl.ItemId), LineID: str(pl.LineId), LandResourceID: str(pl.LandResourceId), CustomerID: str(pl.CustomerId),
			NewTotalCents: pl.NewTotalCents, Reason: str(pl.Reason), VesselID: str(pl.VesselId),
		},
	}
	if pl.QtyMilli != nil {
		ai.Payload.QtyMilli = *pl.QtyMilli
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	a, err := h.Contracts.CreateAddendum(r.Context(), principal(r), marinaID, contractID, ai)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeAddendum(w, a, http.StatusCreated)
}

// RequestAddendumSignature implements api.ServerInterface (POST .../addenda/{addendumId}/request-signature).
func (h *Handler) RequestAddendumSignature(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, id api.AddendumId, params api.RequestAddendumSignatureParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.SignatureRequestInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	a, err := h.Contracts.RequestAddendumSignature(r.Context(), principal(r), marinaID, contractID, id, ifMatch, ctrSignatureIn(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeAddendum(w, a, http.StatusOK)
}

// ActivateAddendum implements api.ServerInterface (POST .../addenda/{addendumId}/activate).
func (h *Handler) ActivateAddendum(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, id api.AddendumId, params api.ActivateAddendumParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.ContractActivate
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	ai := contract.ActivateInput{}
	if in != nil {
		ai.PaperScanFileID = str(in.PaperScanFileId)
	}
	a, err := h.Contracts.ActivateAddendum(r.Context(), principal(r), marinaID, contractID, id, ifMatch, ai)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeAddendum(w, a, http.StatusOK)
}

// CancelAddendum implements api.ServerInterface (POST .../addenda/{addendumId}/cancel).
func (h *Handler) CancelAddendum(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, id api.AddendumId, params api.CancelAddendumParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	a, err := h.Contracts.CancelAddendum(r.Context(), principal(r), marinaID, contractID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeAddendum(w, a, http.StatusOK)
}
