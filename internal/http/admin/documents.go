package admin

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Document operations (m1-plan §4.4). Files are raw binary bodies; downloads go through short-lived signed
// links (INV-74).

func dateOrZero(d *calendar.Date) calendar.Date {
	if d == nil {
		return calendar.Date{}
	}
	return *d
}

// ListDocuments implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/documents).
func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListDocumentsParams) {
	v := &auth.ValidationError{}
	f := document.Filter{CustomerID: str(params.CustomerId), VesselID: str(params.VesselId), ExpiresBefore: dateOrZero(parseDate(v, "expires_before", params.ExpiresBefore))}
	if params.Type != nil {
		f.Type = string(*params.Type)
	}
	if err := v.Err(); err != nil {
		respond.Error(w, r, err)
		return
	}
	page, err := h.Documents.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.DocumentPage{Items: view.Documents(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateDocument implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/documents).
func (h *Handler) CreateDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateDocumentParams) {
	var in api.DocumentInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	di := document.Input{
		OwnerType: string(in.OwnerType), CustomerID: str(in.CustomerId), VesselID: str(in.VesselId), Type: string(in.Type),
		Number: str(in.Number), Issuer: str(in.Issuer), Notes: str(in.Notes),
		IssuedOn: dateOrZero(parseDate(v, "issued_on", in.IssuedOn)), ExpiresOn: dateOrZero(parseDate(v, "expires_on", in.ExpiresOn)),
	}
	// Reject malformed input before the service runs: a request reported as failed must not change state.
	if err := v.Err(); err != nil {
		respond.Error(w, r, err)
		return
	}
	d, err := h.Documents.Create(r.Context(), principal(r), marinaID, di)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Row.Version)
	respond.JSON(w, http.StatusCreated, view.Document(d))
}

// GetDocument implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/documents/{documentId}).
func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId) {
	d, err := h.Documents.Get(r.Context(), principal(r), marinaID, documentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Row.Version)
	respond.JSON(w, http.StatusOK, view.Document(d))
}

// UpdateDocument implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/documents/{documentId}).
func (h *Handler) UpdateDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId, params api.UpdateDocumentParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.DocumentPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	v := &auth.ValidationError{}
	p := document.Patch{
		Type: enumPtr(in.Type), Number: nullable(fields, "number", in.Number), Issuer: nullable(fields, "issuer", in.Issuer),
		Notes: in.Notes, IssuedOn: parseDate(v, "issued_on", nullable(fields, "issued_on", in.IssuedOn)),
		ExpiresOn: parseDate(v, "expires_on", nullable(fields, "expires_on", in.ExpiresOn)),
	}
	if err := v.Err(); err != nil {
		respond.Error(w, r, err)
		return
	}
	d, err := h.Documents.Update(r.Context(), principal(r), marinaID, documentID, version, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Row.Version)
	respond.JSON(w, http.StatusOK, view.Document(d))
}

// DeleteDocument implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/documents/{documentId}).
func (h *Handler) DeleteDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId, params api.DeleteDocumentParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Documents.Delete(r.Context(), principal(r), marinaID, documentID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// UploadDocumentFile implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/documents/{documentId}/file).
// The service authorises before it looks at the content type or reads the body.
func (h *Handler) UploadDocumentFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId, params api.UploadDocumentFileParams) {
	d, err := h.Documents.UploadFile(r.Context(), principal(r), marinaID, documentID, r.Header.Get("Content-Type"), str(params.XFileName), r.Body)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Row.Version)
	respond.JSON(w, http.StatusOK, view.Document(d))
}

// DeleteDocumentFile implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/documents/{documentId}/file).
func (h *Handler) DeleteDocumentFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId) {
	if err := h.Documents.DeleteFile(r.Context(), principal(r), marinaID, documentID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// CreateDocumentFileURL implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/documents/{documentId}/file-url).
func (h *Handler) CreateDocumentFileURL(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId) {
	u, err := h.Documents.FileURL(r.Context(), principal(r), marinaID, documentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.DocumentFileUrl{Url: u.URL, ExpiresAt: u.ExpiresAt.UTC()})
}

// GetDocumentFile implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/documents/{documentId}/file).
func (h *Handler) GetDocumentFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId, params api.GetDocumentFileParams) {
	var exp int64
	if params.Exp != nil {
		exp = *params.Exp
	}
	f, err := h.Documents.OpenFile(r.Context(), principal(r), marinaID, documentID, exp, str(params.Sig))
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
	_, _ = w.Write(f.Content) //nolint:gosec // G705: stored file bytes served with sandbox CSP, nosniff and a sniffed content type
}
