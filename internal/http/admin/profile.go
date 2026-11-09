package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// Public marina profile, panel side (M12, m12-plan §12, §13) over profile.Service. If-Match "0" is the version of
// a profile never saved. previewMarinaProfile and the photo bytes answer Cache-Control: no-store and
// X-Robots-Tag: noindex.

func profileText(t profile.Text) *api.ProfileText {
	if len(t) == 0 {
		return nil
	}
	out := &api.ProfileText{}
	if v, ok := t["it"]; ok {
		out.It = &v
	}
	if v, ok := t["en"]; ok {
		out.En = &v
	}
	return out
}

func profileTextIn(t *api.ProfileText) profile.Text {
	out := profile.Text{}
	if t == nil {
		return out
	}
	if t.It != nil {
		out["it"] = *t.It
	}
	if t.En != nil {
		out["en"] = *t.En
	}
	return out
}

func profileHoursIn(hs *[]api.ProfileHours) []profile.Hours {
	out := []profile.Hours{}
	if hs == nil {
		return out
	}
	for _, h := range *hs {
		out = append(out, profile.Hours{Weekday: h.Weekday, From: h.From, To: h.To})
	}
	return out
}

func profilePhotosIn(ps *[]api.ProfilePhoto) []profile.Photo {
	out := []profile.Photo{}
	if ps == nil {
		return out
	}
	for _, p := range *ps {
		out = append(out, profile.Photo{FileID: p.FileId, Alt: profileTextIn(p.Alt)})
	}
	return out
}

func profileBool(b *bool) bool { return b != nil && *b }

// profileInput converts the request body; the typed services go through their JSON form (same names).
func profileInput(in api.MarinaProfileInput) (profile.Input, error) {
	out := profile.Input{
		EntranceLatE7: in.EntranceLatE7, EntranceLonE7: in.EntranceLonE7, Phone: str(in.Phone), Email: str(in.Email),
		Website: str(in.Website), OfficeHours: profileHoursIn(in.OfficeHours), DockHours: profileHoursIn(in.DockHours),
		HoursNote: profileTextIn(in.HoursNote), BerthsTotalOverride: in.BerthsTotalOverride,
		TransitBerthsOverride: in.TransitBerthsOverride, MaxLoaCm: in.MaxLoaCm, DepthEntranceCm: in.DepthEntranceCm,
		DepthQuayCm: in.DepthQuayCm, DepthMinCm: in.DepthMinCm, DepthMaxCm: in.DepthMaxCm,
		AccessNotes: profileTextIn(in.AccessNotes), Lights: profileTextIn(in.Lights), Description: profileTextIn(in.Description),
		AccessImages: profilePhotosIn(in.AccessImages), Photos: profilePhotosIn(in.Photos), ShowMap: profileBool(in.ShowMap),
		ShowPrices: profileBool(in.ShowPrices), ShowOccupancy: profileBool(in.ShowOccupancy), MapID: str(in.MapId),
	}
	if in.Locales != nil {
		for _, l := range *in.Locales {
			out.Locales = append(out.Locales, string(l))
		}
	}
	if a := in.Address; a != nil {
		out.Address = profile.Address{Street: str(a.Street), PostalCode: str(a.PostalCode), City: str(a.City), Province: str(a.Province), Country: str(a.Country)}
	}
	if in.VhfChannels != nil {
		out.VHFChannels = append(out.VHFChannels, *in.VhfChannels...)
	}
	if in.Seabed != nil {
		out.Seabed = string(*in.Seabed)
	}
	if in.Shelter != nil {
		for _, w := range *in.Shelter {
			out.Shelter = append(out.Shelter, string(w))
		}
	}
	if in.Services != nil {
		raw, err := json.Marshal(in.Services)
		if err != nil {
			return out, err
		}
		if err := json.Unmarshal(raw, &out.Services); err != nil {
			return out, err
		}
	}
	if in.OccupancyCategories != nil {
		out.OccupancyCategories = append(out.OccupancyCategories, *in.OccupancyCategories...)
	}
	if in.Poi != nil {
		for _, p := range *in.Poi {
			out.POI = append(out.POI, profile.POI{Kind: string(p.Kind), XCm: p.XCm, YCm: p.YCm})
		}
	}
	return out, nil
}

func jsonInto[T any](raw []byte) *T {
	if len(raw) == 0 {
		return nil
	}
	v := new(T)
	if json.Unmarshal(raw, v) != nil {
		return nil
	}
	return v
}

func textOf(raw []byte) *api.ProfileText {
	t := jsonInto[profile.Text](raw)
	if t == nil {
		return nil
	}
	return profileText(*t)
}

func photosOf(raw []byte) *[]api.ProfilePhoto {
	ps := jsonInto[[]profile.Photo](raw)
	out := []api.ProfilePhoto{}
	if ps != nil {
		for _, p := range *ps {
			sort := p.Sort
			out = append(out, api.ProfilePhoto{FileId: p.FileID, Alt: profileText(p.Alt), Sort: &sort})
		}
	}
	return &out
}

func profileView(v *profile.View) api.MarinaProfile {
	r := v.MarinaProfile
	out := api.MarinaProfile{
		Published: r.Published, PublishedAt: r.PublishedAt, EntranceLatE7: r.EntranceLatE7, EntranceLonE7: r.EntranceLonE7,
		Phone: &r.Phone, Email: &r.Email, Website: &r.Website, BerthsTotal: v.BerthsTotal, TransitBerths: v.TransitBerths,
		BerthsTotalOverride: r.BerthsTotalOverride, TransitBerthsOverride: r.TransitBerthsOverride, MaxLoaCm: r.MaxLoaCm,
		DepthEntranceCm: r.DepthEntranceCm, DepthQuayCm: r.DepthQuayCm, DepthMinCm: r.DepthMinCm, DepthMaxCm: r.DepthMaxCm,
		HoursNote: textOf(r.HoursNote), AccessNotes: textOf(r.AccessNotes), Lights: textOf(r.Lights), Description: textOf(r.Description),
		AccessImages: photosOf(r.AccessImages), Photos: photosOf(r.Photos), ShowMap: &r.ShowMap, ShowPrices: &r.ShowPrices,
		ShowOccupancy: &r.ShowOccupancy, PricesAvailable: v.PricesAvailable, PublicUrl: v.PublicURL, Version: int64(r.Version),
		Address: jsonInto[api.ProfileAddress](r.Address), Services: jsonInto[api.ProfileServices](r.Services),
	}
	if out.Services == nil {
		out.Services = &api.ProfileServices{}
	}
	if !r.UpdatedAt.IsZero() {
		u := r.UpdatedAt.UTC()
		out.UpdatedAt = &u
	}
	if r.MapID != "" {
		out.MapId = &r.MapID
	}
	if r.Seabed != "" {
		s := api.ProfileSeabed(r.Seabed)
		out.Seabed = &s
	}
	missing := append([]string{}, v.Missing...)
	out.Missing = &missing
	locales := []api.Locale{}
	if ls := jsonInto[[]string](r.Locales); ls != nil {
		for _, l := range *ls {
			locales = append(locales, api.Locale(l))
		}
	}
	out.Locales = &locales
	vhf := []int{}
	if p := jsonInto[[]int](r.VHFChannels); p != nil {
		vhf = *p
	}
	out.VhfChannels = &vhf
	for _, dst := range []struct {
		raw []byte
		out **[]api.ProfileHours
	}{{r.OfficeHours, &out.OfficeHours}, {r.DockHours, &out.DockHours}} {
		hs := []api.ProfileHours{}
		if p := jsonInto[[]profile.Hours](dst.raw); p != nil {
			for _, h := range *p {
				hs = append(hs, api.ProfileHours{Weekday: h.Weekday, From: h.From, To: h.To})
			}
		}
		*dst.out = &hs
	}
	shelter := []api.ProfileWind{}
	if p := jsonInto[[]string](r.Shelter); p != nil {
		for _, w := range *p {
			shelter = append(shelter, api.ProfileWind(w))
		}
	}
	out.Shelter = &shelter
	cats := []string{}
	if p := jsonInto[[]string](r.OccupancyCategories); p != nil {
		cats = *p
	}
	out.OccupancyCategories = &cats
	pois := []api.ProfilePoi{}
	if p := jsonInto[[]profile.POI](r.POI); p != nil {
		for _, x := range *p {
			pois = append(pois, api.ProfilePoi{Kind: api.ProfilePoiKind(x.Kind), XCm: x.XCm, YCm: x.YCm})
		}
	}
	out.Poi = &pois
	return out
}

func writeProfile(w http.ResponseWriter, v *profile.View) {
	respond.SetETag(w, v.Version)
	respond.JSON(w, http.StatusOK, profileView(v))
}

// GetMarinaProfile implements api.ServerInterface.
func (h *Handler) GetMarinaProfile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	v, err := h.Profiles.Get(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProfile(w, v)
}

// PutMarinaProfile implements api.ServerInterface.
func (h *Handler) PutMarinaProfile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PutMarinaProfileParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	var body api.MarinaProfileInput
	if !respond.DecodeOrFail(w, r, &body, false) {
		return
	}
	in, err := profileInput(body)
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	v, err := h.Profiles.Put(r.Context(), principal(r), marinaID, ifMatch, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProfile(w, v)
}

// PublishMarinaProfile implements api.ServerInterface.
func (h *Handler) PublishMarinaProfile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PublishMarinaProfileParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Profiles.Publish(r.Context(), principal(r), marinaID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProfile(w, v)
}

// UnpublishMarinaProfile implements api.ServerInterface.
func (h *Handler) UnpublishMarinaProfile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UnpublishMarinaProfileParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Profiles.Unpublish(r.Context(), principal(r), marinaID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProfile(w, v)
}

// PreviewMarinaProfile implements api.ServerInterface.
func (h *Handler) PreviewMarinaProfile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PreviewMarinaProfileParams) {
	lang := "it"
	if params.Lang != nil {
		lang = string(*params.Lang)
	}
	pub, ld, err := h.Profiles.Preview(r.Context(), principal(r), marinaID, lang)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	respond.JSON(w, http.StatusOK, struct {
		Profile *profile.PublicData `json:"profile"`
		JSONLD  map[string]any      `json:"json_ld"`
	}{pub.Data, ld})
}

// UploadProfilePhoto implements api.ServerInterface (raw JPEG/PNG body, at most 8 MiB).
func (h *Handler) UploadProfilePhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.UploadProfilePhotoParams) {
	if _, err := h.Profiles.Get(r.Context(), principal(r), marinaID); err != nil {
		respond.Error(w, r, err) // another marina is 404 before the body is read
		return
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mt != "image/jpeg" && mt != "image/png") {
		respond.Problem(w, r, problem.UnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, profile.MaxPhotoBytes+1))
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	if len(body) > profile.MaxPhotoBytes {
		respond.Problem(w, r, problem.BadRequest, problem.Status(http.StatusRequestEntityTooLarge), problem.Detail("photo larger than 8 MiB"))
		return
	}
	f, err := h.Profiles.UploadPhoto(r.Context(), principal(r), marinaID, mt, bytes.NewReader(body))
	switch {
	case errors.Is(err, profile.ErrUnsupportedMedia):
		respond.Problem(w, r, problem.UnsupportedMediaType)
		return
	case err != nil:
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.ProfilePhotoFile{
		FileId: f.FileID, ContentType: api.ProfilePhotoFileContentType(f.ContentType), Width: f.Width, Height: f.Height, SizeBytes: f.SizeBytes,
	})
}

// GetProfilePhoto implements api.ServerInterface.
func (h *Handler) GetProfilePhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, fileID api.FileId) {
	img, err := h.Profiles.GetPhoto(r.Context(), principal(r), marinaID, fileID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", img.ContentType)
	hd.Set("Cache-Control", "private, no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img.Body) //nolint:gosec // a JPEG/PNG re-encoded by the server, served with its type and nosniff
}

// DeleteProfilePhoto implements api.ServerInterface.
func (h *Handler) DeleteProfilePhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, fileID api.FileId, params api.DeleteProfilePhotoParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Profiles.DeletePhoto(r.Context(), principal(r), marinaID, fileID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProfile(w, v)
}
