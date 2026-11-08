package webhooks

import (
	"errors"
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/pkg/ext"
)

// ReceiveChannelWebhook implements api.ServerInterface (POST /webhooks/channels/{channel}/{marinaId}, m8-plan
// §8.2). The raw body (≤ 256 KiB) reaches the driver's ParseWebhook untouched, verified with the connection's
// webhook secret against the host clock. An unknown channel, marina or connection is 404; a callback that fails
// verification is 401 invalid_signature; events Porto does not use are acknowledged. Each reservation is applied
// once (INV-89), so a redelivery changes nothing. Guest data are never logged.
func (h *Handler) ReceiveChannelWebhook(w http.ResponseWriter, r *http.Request, code api.ChannelPathCode, marinaID api.MarinaId) {
	if h.Channels == nil || h.Clock == nil {
		respond.Problem(w, r, problem.NotFound)
		return
	}
	drv, secret, err := h.Channels.WebhookChannel(r.Context(), code, marinaID)
	if err != nil || len(secret) == 0 {
		respond.Problem(w, r, problem.NotFound)
		return
	}
	res, err := drv.ParseWebhook(r, secret, h.Clock.Now())
	switch {
	case errors.Is(err, ext.ErrIgnoredEvent):
		w.WriteHeader(http.StatusOK)
		return
	case err != nil:
		respond.Problem(w, r, problem.InvalidSignature)
		return
	}
	if err := h.Channels.ApplyWebhook(r.Context(), code, marinaID, res); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
