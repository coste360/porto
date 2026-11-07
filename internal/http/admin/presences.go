package admin

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Presence and movement operations (m3-plan §7.3).

func (h *Handler) presenceResult(w http.ResponseWriter, res *presence.Result) {
	// The service blanks the notes for callers without customers.read.
	out := api.PresenceResult{Presence: view.Presence(&res.Presence, true)}
	if res.Contract != nil {
		c := view.Contract(res.Contract)
		out.Contract = &c
	}
	respond.JSON(w, http.StatusCreated, out)
}

// ListPresences implements api.ServerInterface (GET .../presences).
func (h *Handler) ListPresences(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListPresencesParams) {
	d := newCtrDates()
	f := presence.Filter{Night: d.ptr(params.Date), VesselID: str(params.VesselId)}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Presence.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	items := make([]api.Presence, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, view.Presence(&page.Items[i], true))
	}
	respond.JSON(w, http.StatusOK, api.PresencePage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}

// RecordPresence implements api.ServerInterface (POST .../presences).
func (h *Handler) RecordPresence(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.RecordPresenceParams) {
	var in api.PresenceCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	pi := presence.Input{
		VesselID: in.VesselId, Event: string(in.Event), At: in.At, AllocationID: str(in.AllocationId), Notes: str(in.Notes),
		ExpectedReturnOn: d.ptr(in.ExpectedReturnOn),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Presence.Record(r.Context(), principal(r), marinaID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.presenceResult(w, res)
}

// RecordUnexpectedArrival implements api.ServerInterface (POST .../presences/unexpected-arrival).
func (h *Handler) RecordUnexpectedArrival(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.RecordUnexpectedArrivalParams) {
	var in api.UnexpectedArrival
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ui := presence.UnexpectedInput{
		VesselID: str(in.VesselId), NewVessel: ctrNewVessel(in.NewVessel), Departure: d.ptr(in.Departure), Notes: str(in.Notes),
		FitOverride: ctrFit(in.FitOverride),
		Target: contract.Segment{
			ResourceType: string(in.Target.ResourceType), ResourceID: in.Target.ResourceId, X: in.Target.XCm, Y: in.Target.YCm,
			RotationDd: in.Target.RotationDd,
		},
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Presence.UnexpectedArrival(r.Context(), principal(r), marinaID, ui)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.presenceResult(w, res)
}

// UploadPresencePhoto implements api.ServerInterface (POST .../presences/{presenceId}/photo): raw image body.
func (h *Handler) UploadPresencePhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, presenceID api.PresenceId) {
	id, err := h.Presence.UploadPhoto(r.Context(), principal(r), marinaID, presenceID, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PresencePhoto{FileId: id})
}

// GetPresencePhotoLink implements api.ServerInterface (GET .../presences/{presenceId}/photo).
func (h *Handler) GetPresencePhotoLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, presenceID api.PresenceId) {
	l, err := h.Presence.PhotoLink(r.Context(), principal(r), marinaID, presenceID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.DocumentFileUrl{Url: l.URL, ExpiresAt: l.ExpiresAt.UTC()})
}

// GetPresencePhotoFile implements api.ServerInterface (GET .../presences/{presenceId}/photo/file?exp=&sig=).
func (h *Handler) GetPresencePhotoFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, presenceID api.PresenceId, params api.GetPresencePhotoFileParams) {
	var exp int64
	if params.Exp != nil {
		exp = *params.Exp
	}
	f, err := h.Presence.OpenPhoto(r.Context(), principal(r), marinaID, presenceID, exp, str(params.Sig))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", f.ContentType)
	hd.Set("Content-Length", strconv.Itoa(len(f.Content)))
	hd.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(f.FileName))
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Content) //nolint:gosec // G705: stored photo bytes served with sandbox CSP, nosniff and a sniffed content type
}

// GetMovements implements api.ServerInterface (GET .../movements).
func (h *Handler) GetMovements(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetMovementsParams) {
	d := newCtrDates()
	date := d.ptr(params.Date)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	m, err := h.Presence.Movements(r.Context(), principal(r), marinaID, date, str(params.MapId))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Movements(m))
}
