// Package webhooks implements the inbound provider callbacks under /webhooks (m4-plan §7.3.4). They are public
// routes: the registered provider verifies each request with the secret stored for the marina.
package webhooks

import (
	"net/http"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// Handler serves the inbound webhooks. The handler calls the provider's ParseWebhook (it needs *http.Request, which
// the domain may not import) with the secret the signature service returns, then passes the decoded event to the
// service.
type Handler struct {
	Signatures *signature.Service

	// M5 (m5-plan §5.4): payment provider callbacks (payments.go); the signature tolerance is checked against Clock.
	Collect *collect.Service
	Clock   clock.Clock
}

// SignatureWebhook implements api.ServerInterface (POST /webhooks/signature/{provider}/{marinaId}). An unknown
// provider, an unknown marina and a marina not configured with the provider are the same 404; a callback that fails
// verification is 401 invalid_signature; status changes are conditional updates, so replays change nothing.
func (h *Handler) SignatureWebhook(w http.ResponseWriter, r *http.Request, provider api.ProviderCode, marinaID api.MarinaId) {
	if h.Signatures == nil {
		respond.Problem(w, r, problem.NotFound)
		return
	}
	prov, secret, err := h.Signatures.WebhookProvider(r.Context(), provider, marinaID)
	if err != nil {
		respond.Problem(w, r, problem.NotFound)
		return
	}
	ev, err := prov.ParseWebhook(r, secret)
	if err != nil {
		// Any callback the provider cannot verify and decode is refused as unsigned.
		respond.Problem(w, r, problem.InvalidSignature)
		return
	}
	if err := h.Signatures.ApplyProviderEvent(r.Context(), provider, marinaID, ev); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
