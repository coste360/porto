package admin

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// Rights, transfers, holder chain and rights lens handlers (m4-plan §7.3.2). Shared helpers of the rights area
// (rights.go, mandates.go, fees.go, common_charges.go, concession.go) are prefixed rc.

// rcInvalid answers a body the handler could not convert (bad date): another marina is still 404 first, so the
// existence of a marina never leaks through a 422.
func (h *Handler) rcInvalid(w http.ResponseWriter, r *http.Request, marinaID string, v *auth.ValidationError) {
	if _, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.Error(w, r, v)
}

// rcDate parses a required date into v.
func rcDate(v *auth.ValidationError, field, s string) calendar.Date {
	d, err := calendar.Parse(s)
	if err != nil {
		v.Add(field, "invalid")
	}
	return d
}

// rcOptDate parses an optional date into v.
func rcOptDate(v *auth.ValidationError, field string, s *string) calendar.Date {
	if s == nil || *s == "" {
		return calendar.Date{}
	}
	return rcDate(v, field, *s)
}

func rcShares(in []api.HolderShare) []allot.Share {
	out := make([]allot.Share, len(in))
	for i, h := range in {
		out[i] = allot.Share{CustomerID: h.CustomerId, Millis: h.ShareMillis}
	}
	return out
}

func rcVessels(in []api.RightVesselInput) []rights.VesselInput {
	out := make([]rights.VesselInput, len(in))
	for i, v := range in {
		out[i] = rights.VesselInput{VesselID: v.VesselId, Primary: v.Primary}
	}
	return out
}

func rcTitle(v *auth.ValidationError, t api.RightTitle) rights.Title {
	return rights.Title{Deed: str(t.Deed), Number: str(t.Number), Notary: str(t.Notary), Date: rcOptDate(v, "title.date", t.Date)}
}

// rcCSV writes a CSV attachment (UTF-8 with BOM for spreadsheets).
func rcCSV(w http.ResponseWriter, name string, header []string, rows [][]string) {
	var b bytes.Buffer
	cw := csv.NewWriter(&b)
	cw.Comma = ';'
	_ = cw.Write(header)
	_ = cw.WriteAll(rows)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("\ufeff"))
	_, _ = w.Write(b.Bytes())
}

// rcPDF renders a view of a rights kind and writes it as a PDF attachment.
func (h *Handler) rcPDF(w http.ResponseWriter, r *http.Request, marinaID, name string, kind docs.Kind, v any) {
	b, err := h.Rights.RenderPDF(r.Context(), principal(r), marinaID, kind, v)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // application/pdf attachment rendered by the service, not HTML
}

// rcCents formats cents for CSV exports ("1234,56").
func rcCents(c int64) string { return strings.Replace(money.Cents(c).String(), ".", ",", 1) }

// ListRights implements api.ServerInterface (GET .../rights).
func (h *Handler) ListRights(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRightsParams) {
	v := &auth.ValidationError{}
	f := rights.RightFilter{BerthID: str(params.BerthId), CustomerID: str(params.CustomerId), ActiveOn: rcOptDate(v, "active_on", params.ActiveOn)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	page, err := h.Rights.ListRights(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.RightPage(page.Items, page.NextCursor))
}

// CreateRight implements api.ServerInterface (POST .../rights).
func (h *Handler) CreateRight(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRightParams) {
	var in api.RightInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ri := rights.RightInput{
		BerthID: in.BerthId, Kind: string(in.Kind), FromOn: rcDate(v, "from_on", in.FromOn), ToOn: rcOptDate(v, "to_on", in.ToOn),
		Title: rcTitle(v, in.Title), Holders: rcShares(in.Holders),
	}
	if in.Vessels != nil {
		ri.Vessels = rcVessels(*in.Vessels)
	}
	if in.FitOverride != nil {
		ri.FitOverride = in.FitOverride.Reason
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.CreateRight(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Right(out))
}

// GetRight implements api.ServerInterface (GET .../rights/{rightId}).
func (h *Handler) GetRight(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RightId) {
	out, err := h.Rights.GetRight(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Right(out))
}

// UpdateRight implements api.ServerInterface (PATCH .../rights/{rightId}).
func (h *Handler) UpdateRight(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RightId, params api.UpdateRightParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RightPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	var patch rights.RightPatch
	if in.Title != nil {
		t := rcTitle(v, *in.Title)
		patch.Title = &t
	}
	if in.Holders != nil {
		patch.Holders = rcShares(*in.Holders)
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.UpdateRight(r.Context(), principal(r), marinaID, id, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Right(out))
}

// EndRight implements api.ServerInterface (POST .../rights/{rightId}/end).
func (h *Handler) EndRight(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RightId, params api.EndRightParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RightEndInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	on := rcDate(v, "on", in.On)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.EndRight(r.Context(), principal(r), marinaID, id, version, on, in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Right(out))
}

// CancelRight implements api.ServerInterface (POST .../rights/{rightId}/cancel).
func (h *Handler) CancelRight(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RightId, params api.CancelRightParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RightEndInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.CancelRight(r.Context(), principal(r), marinaID, id, version, in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Right(out))
}

// SetRightVessels implements api.ServerInterface (PUT .../rights/{rightId}/vessels).
func (h *Handler) SetRightVessels(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RightId, params api.SetRightVesselsParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RightVesselsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ri := rights.RightVesselsInput{Vessels: rcVessels(in.Vessels), FromNight: rcOptDate(v, "from_night", in.FromNight)}
	if in.FitOverride != nil {
		ri.FitOverride = in.FitOverride.Reason
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.SetRightVessels(r.Context(), principal(r), marinaID, id, version, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Right(out))
}

// GetRightHistory implements api.ServerInterface (GET .../berths/{berthId}/right-history).
func (h *Handler) GetRightHistory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, berthID api.BerthId, params api.GetRightHistoryParams) {
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format == api.GetRightHistoryParamsFormatPdf {
		hv, err := h.Rights.HolderChainView(ctx, p, marinaID, berthID)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		h.rcPDF(w, r, marinaID, "holder-chain-"+hv.Berth.Code+".pdf", docs.KindHolderChain, hv)
		return
	}
	out, err := h.Rights.RightHistory(ctx, p, marinaID, berthID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.RightHistory(out))
}

// GetMapRights implements api.ServerInterface (GET .../maps/{mapId}/rights).
func (h *Handler) GetMapRights(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.GetMapRightsParams) {
	v := &auth.ValidationError{}
	on := rcOptDate(v, "date", params.Date)
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	date, items, err := h.Rights.MapRights(r.Context(), principal(r), marinaID, mapID, on)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MapRights(mapID, date, items))
}

// ---- transfers ----

// ListRightTransfers implements api.ServerInterface (GET .../right-transfers).
func (h *Handler) ListRightTransfers(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRightTransfersParams) {
	f := rights.TransferFilter{RightID: str(params.RightId)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	page, err := h.Rights.ListTransfers(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.TransferPage(page.Items, page.NextCursor))
}

// CreateRightTransfer implements api.ServerInterface (POST .../right-transfers).
func (h *Handler) CreateRightTransfer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRightTransferParams) {
	var in api.RightTransferInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ti := rights.TransferInput{
		RightID: in.RightId, EffectiveOn: rcDate(v, "effective_on", in.EffectiveOn), ToHolders: rcShares(in.ToHolders),
		DeclaredPriceCents: in.DeclaredPriceCents, TakeoverItemID: str(in.TakeoverItemId), FeesPolicy: string(in.FeesPolicy),
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.CreateTransfer(r.Context(), principal(r), marinaID, ti)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Transfer(out))
}

// GetRightTransfer implements api.ServerInterface (GET .../right-transfers/{transferId}).
func (h *Handler) GetRightTransfer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TransferId) {
	out, err := h.Rights.GetTransfer(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Transfer(out))
}

// RequestTransferSignature implements api.ServerInterface (POST .../right-transfers/{transferId}/request-signature).
func (h *Handler) RequestTransferSignature(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TransferId, params api.RequestTransferSignatureParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.SignatureRequestInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	si := rights.SignatureInput{}
	if in.Driver != nil {
		si.Driver = string(*in.Driver)
	}
	out, err := h.Rights.RequestTransferSignature(r.Context(), principal(r), marinaID, id, version, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Transfer(out))
}

// CompleteRightTransfer implements api.ServerInterface (POST .../right-transfers/{transferId}/complete).
func (h *Handler) CompleteRightTransfer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TransferId, params api.CompleteRightTransferParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PaperConfirmInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	out, err := h.Rights.CompleteTransfer(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Transfer(out))
}

// CancelRightTransfer implements api.ServerInterface (POST .../right-transfers/{transferId}/cancel).
func (h *Handler) CancelRightTransfer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TransferId, params api.CancelRightTransferParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ReasonInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	out, err := h.Rights.CancelTransfer(r.Context(), principal(r), marinaID, id, version, str(in.Reason))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Transfer(out))
}
