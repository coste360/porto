package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Template operations (m4-plan §7.3.3).

// ListTemplateKinds implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/template-kinds).
func (h *Handler) ListTemplateKinds(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	ks, err := h.Docs.ListTemplateKinds(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.TemplateKinds(ks))
}

// ListTemplates implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/templates).
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListTemplatesParams) {
	ts, err := h.Docs.ListTemplates(r.Context(), principal(r), marinaID, str(enumPtr(params.Kind)))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Templates(ts))
}

// CreateTemplate implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/templates).
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateTemplateParams) {
	var in api.TemplateInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	t, err := h.Docs.CreateTemplate(r.Context(), principal(r), marinaID, docs.TemplateInput{
		Kind: string(in.Kind), Code: in.Code, Name: in.Name, IsDefault: boolOf(in.IsDefault), FromBuiltin: boolOf(in.FromBuiltin),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, t.Version)
	respond.JSON(w, http.StatusCreated, view.Template(t))
}

func boolOf(p *bool) bool { return p != nil && *p }

// GetTemplate implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/templates/{templateId}).
func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId) {
	t, err := h.Docs.GetTemplate(r.Context(), principal(r), marinaID, templateID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, t.Version)
	respond.JSON(w, http.StatusOK, view.Template(t))
}

// UpdateTemplate implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/templates/{templateId}).
func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId, params api.UpdateTemplateParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.TemplatePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	t, err := h.Docs.UpdateTemplate(r.Context(), principal(r), marinaID, templateID, version, docs.TemplatePatch{
		Name: in.Name, IsDefault: in.IsDefault, Archived: in.Archived,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, t.Version)
	respond.JSON(w, http.StatusOK, view.Template(t))
}

// ListTemplateVersions implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/templates/{templateId}/versions).
func (h *Handler) ListTemplateVersions(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId, params api.ListTemplateVersionsParams) {
	vs, err := h.Docs.ListTemplateVersions(r.Context(), principal(r), marinaID, templateID, str(enumPtr(params.Locale)))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.TemplateVersions(vs))
}

func intOf(p *int) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}

// CreateTemplateVersion implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/templates/{templateId}/versions).
func (h *Handler) CreateTemplateVersion(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId, _ api.CreateTemplateVersionParams) {
	var in api.TemplateVersionInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	vi := docs.VersionInput{Locale: string(in.Locale), Body: in.BodyMd, Header: str(in.HeaderMd), Footer: str(in.FooterMd)}
	if in.Page != nil {
		vi.MarginMM, vi.FontSizePt = intOf(in.Page.MarginsMm), intOf(in.Page.FontSizePt)
	}
	v, err := h.Docs.CreateTemplateVersion(r.Context(), principal(r), marinaID, templateID, vi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.TemplateVersion(v))
}

// PublishTemplateVersion implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/templates/{templateId}/versions/{templateVersion}/publish).
func (h *Handler) PublishTemplateVersion(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId, templateVersion api.TemplateVersionNumber, _ api.PublishTemplateVersionParams) {
	var in api.TemplatePublishInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(templateVersion))
	if err != nil || n < 1 {
		// Authorise first so another marina's template is 404, then reject the malformed version.
		if _, gerr := h.Docs.GetTemplate(r.Context(), principal(r), marinaID, templateID); gerr != nil {
			respond.Error(w, r, gerr)
			return
		}
		v := &auth.ValidationError{}
		v.Add("templateVersion", "invalid")
		respond.Error(w, r, v.Err())
		return
	}
	out, err := h.Docs.PublishTemplateVersion(r.Context(), principal(r), marinaID, templateID, n, string(in.Locale))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.TemplateVersion(out))
}

// PreviewTemplate implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/templates/{templateId}/preview).
func (h *Handler) PreviewTemplate(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, templateID api.TemplateId) {
	var in api.TemplatePreviewInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	pi := docs.PreviewInput{Locale: string(in.Locale), Body: in.BodyMd, Header: in.HeaderMd, Footer: in.FooterMd}
	if in.Version != nil {
		pi.Version = *in.Version
	}
	if rec, err := in.Sample.AsTemplatePreviewSample1(); err == nil && rec.SubjectId != "" {
		pi.SubjectType, pi.SubjectID = string(rec.SubjectType), rec.SubjectId
	}
	out, err := h.Docs.PreviewTemplate(r.Context(), principal(r), marinaID, templateID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePDF(w, out.PDF, "preview.pdf", out.Pages)
}

// writePDF answers a PDF (inline, never cached, sandboxed).
func writePDF(w http.ResponseWriter, b []byte, name string, pages int) {
	hd := w.Header()
	hd.Set("Content-Type", "application/pdf")
	hd.Set("Content-Length", strconv.Itoa(len(b)))
	hd.Set("Content-Disposition", `inline; filename="`+name+`"`)
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	if pages > 0 {
		hd.Set("X-Porto-Pages", strconv.Itoa(pages))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // G705: a PDF rendered by Porto, served with sandbox CSP and nosniff
}
