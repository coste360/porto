package webhooks

import (
	"errors"
	"net/http"

	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/pkg/ext"
)

// PaymentWebhook implements api.ServerInterface (POST /webhooks/payments/{provider}/{marinaId}, m5-plan §5.4). The
// raw body (≤ 256 KiB, x-porto-max-body-bytes) reaches the provider's ParseWebhook untouched, verified against the
// host clock. Unknown provider or marina is 404; a provider configured without a webhook secret, or a callback that
// fails verification, is 401 invalid_signature; events Porto does not use are recorded as ignored and acknowledged;
// duplicates are no-ops (INV-56); an event for an intent not visible yet is 503 so the provider retries.
func (h *Handler) PaymentWebhook(w http.ResponseWriter, r *http.Request, provider api.ProviderCode, marinaID api.MarinaId) {
	if h.Collect == nil || h.Clock == nil {
		respond.Problem(w, r, problem.NotFound)
		return
	}
	prov, secret, err := h.Collect.WebhookProvider(r.Context(), provider, marinaID)
	switch {
	case errors.Is(err, collect.ErrWebhookSecretMissing) || (err == nil && len(secret) == 0):
		respond.Problem(w, r, problem.InvalidSignature)
		return
	case err != nil:
		respond.Problem(w, r, problem.NotFound)
		return
	}
	ev, err := prov.ParseWebhook(r, secret, h.Clock.Now())
	switch {
	case errors.Is(err, ext.ErrIgnoredEvent):
		if err := h.Collect.RecordIgnoredEvent(r.Context(), provider, marinaID, ev); err != nil {
			respond.Error(w, r, err)
			return
		}
		respond.NoContent(w)
		return
	case err != nil:
		respond.Problem(w, r, problem.InvalidSignature)
		return
	}
	if err := h.Collect.ApplyProviderEvent(r.Context(), provider, marinaID, ev); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
