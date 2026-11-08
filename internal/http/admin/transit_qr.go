package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/qrsvg"
)

// GetTransitQr implements api.ServerInterface (GET .../transits/{contractId}/qr, m6b-plan §6.4): the SVG of the
// transit QR (payload PT1:<contract_id>) for the dock worker's scanner.
func (h *Handler) GetTransitQr(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	payload, err := h.Contracts.TransitQrPayload(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	svg, err := qrsvg.Render(payload)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(svg)) //nolint:gosec // G705: SVG built by qrsvg from a UUID payload (one path, no text, no scripts)
}
