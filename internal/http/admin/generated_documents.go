package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Generated documents and map print (m4-plan §7.3.3).

// ListGeneratedDocuments implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/generated-documents).
func (h *Handler) ListGeneratedDocuments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListGeneratedDocumentsParams) {
	f := docs.DocumentFilter{
		SubjectType: str(enumPtr(params.SubjectType)), SubjectID: str(params.SubjectId), Status: str(enumPtr(params.Status)),
	}
	page, err := h.Docs.ListDocuments(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.GeneratedDocuments(page.Items, page.NextCursor))
}

// GetGeneratedDocument implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/generated-documents/{generatedDocumentId}).
func (h *Handler) GetGeneratedDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.GeneratedDocumentId) {
	d, err := h.Docs.GetDocument(r.Context(), principal(r), marinaID, documentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.GeneratedDocument(d))
}

// GetGeneratedDocumentFile implements api.ServerInterface (GET .../generated-documents/{generatedDocumentId}/file?exp=&sig=):
// the PDF of a download link from getGeneratedDocumentLink.
func (h *Handler) GetGeneratedDocumentFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.GeneratedDocumentId, params api.GetGeneratedDocumentFileParams) {
	var exp int64
	if params.Exp != nil {
		exp = *params.Exp
	}
	b, row, err := h.Docs.OpenDocument(r.Context(), principal(r), marinaID, documentID, exp, str(params.Sig))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePDF(w, b, row.Kind+".pdf", row.Pages)
}

// GetGeneratedDocumentLink implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/generated-documents/{generatedDocumentId}/link).
func (h *Handler) GetGeneratedDocumentLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.GeneratedDocumentId) {
	l, err := h.Docs.DocumentLink(r.Context(), principal(r), marinaID, documentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.FileLink{Url: l.URL, ExpiresAt: l.ExpiresAt.UTC()})
}

// RetryRender implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/generated-documents/{generatedDocumentId}/render).
func (h *Handler) RetryRender(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.GeneratedDocumentId, _ api.RetryRenderParams) {
	if err := h.Docs.RetryRender(r.Context(), principal(r), marinaID, documentID); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// PrintMap implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/maps/{mapId}/print).
func (h *Handler) PrintMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.PrintMapParams) {
	out, err := h.Docs.PrintMap(r.Context(), principal(r), marinaID, mapID, str(params.Date), str(params.Lens), "")
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePDF(w, out.PDF, "map.pdf", out.Pages)
}
