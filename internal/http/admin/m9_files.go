package admin

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Signed-link downloads of M9 files: maintenance task files and meter reading photos. The service checks the
// signature (exp, sig) and the caller's permission; the handler only serves the bytes.

// serveStoredFile writes a decrypted stored file inline with a sandbox CSP and no caching.
func serveStoredFile(w http.ResponseWriter, contentType, fileName string, content []byte) {
	hd := w.Header()
	hd.Set("Content-Type", contentType)
	hd.Set("Content-Length", strconv.Itoa(len(content)))
	hd.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(fileName))
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content) //nolint:gosec // G705: stored bytes served with sandbox CSP, nosniff and a sniffed content type
}

func signedParams(exp *int64, sig *string) (int64, string) {
	var e int64
	if exp != nil {
		e = *exp
	}
	return e, str(sig)
}

// GetMaintenanceFileFile implements api.ServerInterface (GET …/maintenance/tasks/{taskId}/files/{fileId}/file).
func (h *Handler) GetMaintenanceFileFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, fileID api.FileId, params api.GetMaintenanceFileFileParams) {
	exp, sig := signedParams(params.Exp, params.Sig)
	f, err := h.Maintenance.OpenFile(r.Context(), principal(r), marinaID, taskID, fileID, exp, sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	serveStoredFile(w, f.ContentType, f.FileName, f.Content)
}

// GetMeterReadingPhotoFile implements api.ServerInterface (GET …/meter-readings/{readingId}/photo/file).
func (h *Handler) GetMeterReadingPhotoFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, readingID api.ReadingId, params api.GetMeterReadingPhotoFileParams) {
	exp, sig := signedParams(params.Exp, params.Sig)
	f, err := h.Utilities.OpenPhoto(r.Context(), principal(r), marinaID, readingID, exp, sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	serveStoredFile(w, f.ContentType, f.FileName, f.Content)
}
