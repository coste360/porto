package admin

import (
	"encoding/json"
	"net/http"

	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// M1 harbour operations (m1-plan §4.3). Handlers decode, call the harbour service (which authorises first)
// and encode.

func i18nIn(t api.I18nText) harbor.I18nText { return harbor.I18nText{It: t.It, En: t.En} }

// ListMaps implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/maps).
func (h *Handler) ListMaps(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Harbor.ListMaps(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MapSummaries(list))
}

// CreateMap implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps).
func (h *Handler) CreateMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateMapParams) {
	var in api.MapCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d, err := h.Harbor.CreateMap(r.Context(), principal(r), marinaID, harbor.MapCreate{
		Kind: string(in.Kind), Name: i18nIn(in.Name), Icon: str(in.Icon), WidthCm: in.WidthCm, HeightCm: in.HeightCm, GridCm: int64Or(in.GridCm),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusCreated, view.MapDocument(d))
}

func int64Or(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// ReorderMaps implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/reorder).
func (h *Handler) ReorderMaps(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.MapReorder
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Harbor.ReorderMaps(r.Context(), principal(r), marinaID, in.MapIds); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ImportMap implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/import).
func (h *Handler) ImportMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.ImportMapParams) {
	var in api.MapExport
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	exp, err := view.MapExportIn(in)
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	s, err := h.Harbor.ImportMap(r.Context(), principal(r), marinaID, exp)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.MapSummary(s))
}

// GetMap implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/maps/{mapId}).
func (h *Handler) GetMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId) {
	d, err := h.Harbor.GetMap(r.Context(), principal(r), marinaID, mapID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, view.MapDocument(d))
}

func decodeDocument(w http.ResponseWriter, r *http.Request) (harbor.SaveRequest, bool) {
	var in api.MapSaveRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return harbor.SaveRequest{}, false
	}
	req, err := view.SaveRequestIn(in)
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return harbor.SaveRequest{}, false
	}
	return req, true
}

// SaveMap implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/maps/{mapId}).
func (h *Handler) SaveMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.SaveMapParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	req, ok := decodeDocument(w, r)
	if !ok {
		return
	}
	res, err := h.Harbor.SaveMap(r.Context(), principal(r), marinaID, mapID, version, req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Document.Version)
	respond.JSON(w, http.StatusOK, view.MapSaveResult(res))
}

// DeleteMap implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/maps/{mapId}).
func (h *Handler) DeleteMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.DeleteMapParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Harbor.DeleteMap(r.Context(), principal(r), marinaID, mapID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ValidateMap implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/{mapId}/validate).
func (h *Handler) ValidateMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId) {
	req, ok := decodeDocument(w, r)
	if !ok {
		return
	}
	rep, err := h.Harbor.ValidateMap(r.Context(), principal(r), marinaID, mapID, req)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MapValidationReport(rep))
}

// GenerateMapElements implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/{mapId}/generate).
func (h *Handler) GenerateMapElements(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId) {
	var in api.GenerateRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	spec, err := view.GeneratorSpecIn(in.Generator)
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	gi := harbor.GenerateInput{Generator: spec}
	if in.Document != nil {
		doc, err := view.SaveRequestIn(*in.Document)
		if err != nil {
			respond.Error(w, r, respond.ErrBadJSON)
			return
		}
		gi.Document = &doc
	}
	res, err := h.Harbor.Generate(r.Context(), principal(r), marinaID, mapID, gi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.GenerateResult(res))
}

// UploadMapBackground implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/{mapId}/background).
// The body is the raw image; the service authorises before looking at the content type.
func (h *Handler) UploadMapBackground(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId) {
	f, err := h.Harbor.UploadBackground(r.Context(), principal(r), marinaID, mapID, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.MapBackgroundFile{
		FileId: f.FileID, ContentType: api.MapBackgroundFileContentType(f.ContentType), WidthPx: f.WidthPx, HeightPx: f.HeightPx, SizeBytes: f.SizeBytes,
	})
}

// GetMapBackground implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/maps/{mapId}/background/{fileId}).
func (h *Handler) GetMapBackground(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, fileID api.FileId) {
	b, ct, err := h.Harbor.Background(r.Context(), principal(r), marinaID, mapID, fileID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", ct)
	hd.Set("Cache-Control", "private, max-age=31536000, immutable")
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // sniffed image served with a sandbox CSP and nosniff, never rendered as HTML
}

// ExportMap implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/maps/{mapId}/export).
func (h *Handler) ExportMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.ExportMapParams) {
	if params.Format != nil && *params.Format == api.ExportMapParamsFormat("geojson") {
		b, err := h.Harbor.ExportGeoJSON(r.Context(), principal(r), marinaID, mapID)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		hd := w.Header()
		hd.Set("Content-Type", "application/geo+json")
		hd.Set("Content-Disposition", `attachment; filename="map-`+mapID+`.geojson"`)
		hd.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b) //nolint:gosec // GeoJSON built by the service from integers, served as an attachment
		return
	}
	e, err := h.Harbor.ExportMap(r.Context(), principal(r), marinaID, mapID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="map-`+mapID+`.json"`)
	respond.JSON(w, http.StatusOK, view.MapExport(e))
}

// ConvertConcessionGeojson implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/{mapId}/concession/geojson).
// The GeoJSON is kept raw, so that its numbers are parsed as exact decimals (no floats).
func (h *Handler) ConvertConcessionGeojson(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId) {
	var in struct {
		Geojson json.RawMessage `json:"geojson"`
		Georef  *harbor.Georef  `json:"georef,omitempty"`
	}
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Harbor.ConvertConcession(r.Context(), principal(r), marinaID, mapID, in.Geojson, in.Georef)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	poly := make(api.Polygon, 0, len(out))
	for _, p := range out {
		poly = append(poly, api.Point{X: int64(p.X), Y: int64(p.Y)})
	}
	respond.JSON(w, http.StatusOK, api.ConcessionGeojsonResult{Outline: poly})
}

// ImportBerthsCsv implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/maps/{mapId}/berth-import).
// If-Match is required to apply (dry_run = false) and ignored for a dry run.
func (h *Handler) ImportBerthsCsv(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.ImportBerthsCsvParams) {
	var in api.BerthImportRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	version := 0
	if !in.DryRun {
		v, ok := respond.IfMatch(w, r, params.IfMatch)
		if !ok {
			return
		}
		version = v
	}
	bi := harbor.BerthImport{
		CSV: in.Csv, HasHeader: in.HasHeader, Units: string(in.Units), Start: string(in.Placement.Start),
		SidePolicy: string(in.Placement.SidePolicy), DryRun: in.DryRun,
		Mapping: harbor.ImportMapping{
			Code: in.Mapping.Code, Pier: in.Mapping.Pier, Length: in.Mapping.Length, Width: in.Mapping.Width,
			Depth: in.Mapping.Depth, PriceClass: in.Mapping.PriceClass, Side: in.Mapping.Side, MooringType: in.Mapping.MooringType,
		},
	}
	if in.Delimiter != nil {
		bi.Delimiter = string(*in.Delimiter)
	}
	rep, err := h.Harbor.ImportBerths(r.Context(), principal(r), marinaID, mapID, version, bi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if rep.Document != nil {
		respond.SetETag(w, rep.Document.Document.Version)
	}
	respond.JSON(w, http.StatusOK, view.BerthImportReport(rep))
}

// ListBerths implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/berths).
func (h *Handler) ListBerths(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListBerthsParams) {
	page, err := h.Harbor.ListBerths(r.Context(), principal(r), marinaID,
		harbor.BerthFilter{MapID: str(params.MapId), GroupID: str(params.GroupId), Q: str(params.Q)}, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.BerthPage{Items: make([]api.BerthSummary, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Items = append(out.Items, view.BerthSummary(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// ListBerthGroups implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/berth-groups).
func (h *Handler) ListBerthGroups(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	gs, err := h.Harbor.ListGroups(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.BerthGroupList{Items: make([]api.BerthGroup, 0, len(gs))}
	for i := range gs {
		out.Items = append(out.Items, view.BerthGroup(&gs[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateBerthGroup implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/berth-groups).
func (h *Handler) CreateBerthGroup(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateBerthGroupParams) {
	var in api.BerthGroupCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	gi := harbor.GroupInput{Code: in.Code, Name: i18nIn(in.Name)}
	if in.Kind != nil {
		gi.Kind = string(*in.Kind)
	}
	g, err := h.Harbor.CreateGroup(r.Context(), principal(r), marinaID, gi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, g.Version)
	respond.JSON(w, http.StatusCreated, view.BerthGroup(g))
}

// UpdateBerthGroup implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/berth-groups/{groupId}).
func (h *Handler) UpdateBerthGroup(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, groupID api.GroupId, params api.UpdateBerthGroupParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.BerthGroupPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	patch := harbor.GroupPatch{Code: in.Code, SortOrder: in.SortOrder}
	if in.Name != nil {
		n := i18nIn(*in.Name)
		patch.Name = &n
	}
	if in.Kind != nil {
		k := string(*in.Kind)
		patch.Kind = &k
	}
	g, err := h.Harbor.UpdateGroup(r.Context(), principal(r), marinaID, groupID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, g.Version)
	respond.JSON(w, http.StatusOK, view.BerthGroup(g))
}

// DeleteBerthGroup implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/berth-groups/{groupId}).
func (h *Handler) DeleteBerthGroup(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, groupID api.GroupId, params api.DeleteBerthGroupParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Harbor.DeleteGroup(r.Context(), principal(r), marinaID, groupID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// GetHarborSettings implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/harbor-settings).
func (h *Handler) GetHarborSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Harbor.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.HarborSettings(s))
}

// UpdateHarborSettings implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/harbor-settings).
func (h *Handler) UpdateHarborSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateHarborSettingsParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.HarborSettingsPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	patch := harbor.SettingsPatch{LOAToleranceCm: in.LoaToleranceCm, DefaultGridCm: in.DefaultGridCm}
	if in.PriceClassRule != nil {
		rule := view.PriceClassRuleIn(*in.PriceClassRule)
		patch.PriceClassRule = &rule
	}
	s, err := h.Harbor.UpdateSettings(r.Context(), principal(r), marinaID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.HarborSettings(s))
}
