package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Signature requests and provider settings (m4-plan §7.3.4).

// ListSignatureRequests implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/signature-requests).
func (h *Handler) ListSignatureRequests(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListSignatureRequestsParams) {
	f := signature.RequestFilter{
		Status: str(enumPtr(params.Status)), SubjectType: str(enumPtr(params.SubjectType)), SubjectID: str(params.SubjectId),
	}
	page, err := h.Signatures.ListRequests(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.SignatureRequests(page.Items, page.NextCursor))
}

func (h *Handler) signatureReply(w http.ResponseWriter, r *http.Request, req *signature.Request, err error) {
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, req.Version)
	respond.JSON(w, http.StatusOK, view.SignatureRequest(req))
}

// GetSignatureRequest implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/signature-requests/{signatureRequestId}).
func (h *Handler) GetSignatureRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.SignatureRequestId) {
	req, err := h.Signatures.GetRequest(r.Context(), principal(r), marinaID, id)
	h.signatureReply(w, r, req, err)
}

// CancelSignatureRequest implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/signature-requests/{signatureRequestId}/cancel).
func (h *Handler) CancelSignatureRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.SignatureRequestId, params api.CancelSignatureRequestParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	req, err := h.Signatures.CancelRequest(r.Context(), principal(r), marinaID, id, version)
	h.signatureReply(w, r, req, err)
}

// RemindSignatureRequest implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/signature-requests/{signatureRequestId}/remind).
func (h *Handler) RemindSignatureRequest(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.SignatureRequestId, params api.RemindSignatureRequestParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	req, err := h.Signatures.RemindRequest(r.Context(), principal(r), marinaID, id, version)
	h.signatureReply(w, r, req, err)
}

// UploadPaperScan implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/signature-requests/{signatureRequestId}/paper-scan).
func (h *Handler) UploadPaperScan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.SignatureRequestId, _ api.UploadPaperScanParams) {
	fileID, err := h.Signatures.UploadPaperScan(r.Context(), principal(r), marinaID, id, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.PaperScanResult{FileId: fileID})
}

// ConfirmPaperSignature implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/signature-requests/{signatureRequestId}/confirm-paper).
func (h *Handler) ConfirmPaperSignature(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.SignatureRequestId, params api.ConfirmPaperSignatureParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PaperConfirmInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	var ids []string
	if in.SignerIds != nil {
		ids = *in.SignerIds
	}
	req, err := h.Signatures.ConfirmPaper(r.Context(), principal(r), marinaID, id, version, ids, str(in.PaperScanFileId))
	h.signatureReply(w, r, req, err)
}

// GetSignatureSettings implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/signature-settings).
func (h *Handler) GetSignatureSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	st, err := h.Signatures.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, st.Version)
	respond.JSON(w, http.StatusOK, view.SignatureSettings(st))
}

// UpdateSignatureSettings implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/signature-settings).
// The first save of a marina takes no version: If-Match "0" (or the version of GET, which is 0) creates the row.
func (h *Handler) UpdateSignatureSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateSignatureSettingsParams) {
	version := 0
	if params.IfMatch == nil || *params.IfMatch != `"0"` && *params.IfMatch != "0" {
		v, ok := respond.IfMatch(w, r, params.IfMatch)
		if !ok {
			return
		}
		version = v
	}
	var in api.SignatureSettingsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	si := signature.SettingsInput{ProviderCode: in.ProviderCode, WebhookSecret: in.WebhookSecret}
	if in.Credentials != nil {
		si.Credentials = *in.Credentials
	}
	st, err := h.Signatures.UpdateSettings(r.Context(), principal(r), marinaID, version, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, st.Version)
	respond.JSON(w, http.StatusOK, view.SignatureSettings(st))
}
