package admin

import (
	"net/http"
	"strconv"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Management mandate handlers (m4-plan §7.3.2).

// ListMandates implements api.ServerInterface (GET .../mandates).
func (h *Handler) ListMandates(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListMandatesParams) {
	f := rights.MandateFilter{RightID: str(params.RightId)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	page, err := h.Rights.ListMandates(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MandatePage(page.Items, page.NextCursor))
}

// CreateMandate implements api.ServerInterface (POST .../mandates).
func (h *Handler) CreateMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateMandateParams) {
	var in api.MandateInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	mi := rights.MandateInput{
		RightID: in.RightId, FromOn: rcDate(v, "from_on", in.FromOn), ToOn: rcDate(v, "to_on", in.ToOn), OwnerShareBP: in.OwnerShareBp,
		Settlement: string(in.Settlement),
	}
	if in.MinGuaranteedCents != nil {
		mi.MinGuaranteedCents = *in.MinGuaranteedCents
	}
	for _, c := range in.Channels {
		mi.Channels = append(mi.Channels, string(c))
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.CreateMandate(r.Context(), principal(r), marinaID, mi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Mandate(out))
}

// GetMandate implements api.ServerInterface (GET .../mandates/{mandateId}).
func (h *Handler) GetMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MandateId) {
	out, err := h.Rights.GetMandate(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Mandate(out))
}

// EndMandate implements api.ServerInterface (POST .../mandates/{mandateId}/end).
func (h *Handler) EndMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MandateId, params api.EndMandateParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.MandateEndInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	on := rcDate(v, "on", in.On)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.EndMandate(r.Context(), principal(r), marinaID, id, version, on, str(in.Reason))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Mandate(out))
}

// CancelMandate implements api.ServerInterface (POST .../mandates/{mandateId}/cancel).
func (h *Handler) CancelMandate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MandateId, params api.CancelMandateParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ReasonInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	out, err := h.Rights.CancelMandate(r.Context(), principal(r), marinaID, id, version, str(in.Reason))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Mandate(out))
}

// GetMandateStatement implements api.ServerInterface (GET .../mandates/{mandateId}/statement).
func (h *Handler) GetMandateStatement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MandateId, params api.GetMandateStatementParams) {
	v := &auth.ValidationError{}
	from, to := rcOptDate(v, "from", params.From), rcOptDate(v, "to", params.To)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format == api.GetMandateStatementParamsFormatPdf {
		mv, err := h.Rights.MandateStatementView(ctx, p, marinaID, id, from, to)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		h.rcPDF(w, r, marinaID, "mandate-statement.pdf", docs.KindMandateStatement, mv)
		return
	}
	st, err := h.Rights.MandateStatement(ctx, p, marinaID, id, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if params.Format != nil && *params.Format == api.GetMandateStatementParamsFormatCsv {
		rows := make([][]string, 0, len(st.Lines)+1)
		for _, l := range st.Lines {
			rows = append(rows, []string{
				l.ContractCode, l.FirstNight.String(), l.LastNight.String(), strconv.Itoa(l.Nights),
				rcCents(l.RevenueCents), rcCents(l.OwnerShareCents),
			})
		}
		rows = append(rows, []string{"top_up", "", "", "", "", rcCents(st.GuaranteedTopUpCents)})
		rcCSV(w, "mandate-statement.csv", []string{"contract", "first_night", "last_night", "nights", "revenue", "owner_share"}, rows)
		return
	}
	respond.JSON(w, http.StatusOK, view.MandateStatement(st))
}

// AccrueMandates implements api.ServerInterface (POST .../mandates/accrue).
func (h *Handler) AccrueMandates(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.AccrueMandatesParams) {
	n, err := h.Rights.AccrueMandates(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.AccrueResult{Rows: n})
}
