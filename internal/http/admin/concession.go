package admin

import (
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Concession card and obligation handlers (m4-plan §7.3.2, §4.7).

// GetConcession implements api.ServerInterface (GET .../concession).
func (h *Handler) GetConcession(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	out, err := h.Rights.GetConcession(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Concession(out))
}

// UpdateConcession implements api.ServerInterface (PUT .../concession). If-Match "0" creates the card.
func (h *Handler) UpdateConcession(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateConcessionParams) {
	version := 0
	if params.IfMatch == nil || strings.Trim(strings.TrimPrefix(strings.TrimSpace(*params.IfMatch), "W/"), `"`) != "0" {
		v, ok := respond.IfMatch(w, r, params.IfMatch)
		if !ok {
			return
		}
		version = v
	}
	var in api.ConcessionInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ci := rights.ConcessionInput{
		Authority: in.Authority, DeedKind: in.DeedKind, DeedNumber: in.DeedNumber, AnnualFeeCents: in.AnnualFeeCents,
		Prescriptions: in.Prescriptions,
	}
	if in.GrantedOn != nil {
		d := rcOptDate(v, "granted_on", in.GrantedOn)
		ci.GrantedOn = &d
	}
	if in.ExpiresOn != nil {
		d := rcOptDate(v, "expires_on", in.ExpiresOn)
		ci.ExpiresOn = &d
	}
	if in.Status != nil {
		st := string(*in.Status)
		ci.Status = &st
	}
	if in.Surfaces != nil {
		ss := make([]rights.Surface, len(*in.Surfaces))
		for i, s := range *in.Surfaces {
			ss[i] = rights.Surface{Kind: string(s.Kind), AreaCm: s.AreaCm2, Note: str(s.Note)}
		}
		ci.Surfaces = &ss
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.UpdateConcession(r.Context(), principal(r), marinaID, version, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Concession(out))
}

// ListConcessionObligations implements api.ServerInterface (GET .../concession/obligations).
func (h *Handler) ListConcessionObligations(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Rights.ListObligations(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.ConcessionObligationList{Items: make([]api.ConcessionObligation, len(list))}
	for i := range list {
		out.Items[i] = view.Obligation(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

func obligationIn(v *auth.ValidationError, in api.ConcessionObligationInput) rights.ObligationInput {
	oi := rights.ObligationInput{Kind: string(in.Kind), Description: str(in.Description), DueOn: rcDate(v, "due_on", in.DueOn), AmountCents: in.AmountCents}
	if in.Recurrence != nil {
		oi.Recurrence = string(*in.Recurrence)
	}
	return oi
}

// CreateConcessionObligation implements api.ServerInterface (POST .../concession/obligations).
func (h *Handler) CreateConcessionObligation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateConcessionObligationParams) {
	var in api.ConcessionObligationInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	oi := obligationIn(v, in)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.CreateObligation(r.Context(), principal(r), marinaID, oi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Obligation(out))
}

// UpdateConcessionObligation implements api.ServerInterface (PATCH .../concession/obligations/{obligationId}).
func (h *Handler) UpdateConcessionObligation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ObligationId, params api.UpdateConcessionObligationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ConcessionObligationInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	oi := obligationIn(v, in)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.UpdateObligation(r.Context(), principal(r), marinaID, id, version, oi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Obligation(out))
}

// DeleteConcessionObligation implements api.ServerInterface (DELETE .../concession/obligations/{obligationId}).
func (h *Handler) DeleteConcessionObligation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ObligationId, params api.DeleteConcessionObligationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Rights.DeleteObligation(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// MarkObligationDone implements api.ServerInterface (POST .../concession/obligations/{obligationId}/done).
func (h *Handler) MarkObligationDone(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ObligationId, params api.MarkObligationDoneParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ObligationDoneInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	v := &auth.ValidationError{}
	done := rcOptDate(v, "done_on", in.DoneOn)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.MarkObligationDone(r.Context(), principal(r), marinaID, id, version, done)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Obligation(out))
}
