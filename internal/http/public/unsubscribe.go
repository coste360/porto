package public

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// One-click unsubscribe from promotional emails (m4-plan §6.7, RFC 8058): the page of the link and the POST of the
// List-Unsubscribe-Post header (form body "List-Unsubscribe=One-Click", accepted as is).

func init() {
	RegisterPublicView(magiclink.PurposeUnsubscribe, func(h *Handler, r *http.Request, slug, token string, out *api.PublicLink) error {
		l, err := h.Links.ResolvePurpose(r.Context(), slug, token, magiclink.PurposeUnsubscribe)
		if err != nil {
			return err
		}
		sub, err := h.Comms.PublicView(r.Context(), l)
		if err != nil {
			return err
		}
		out.Unsubscribe = &api.PublicUnsubscribe{Subscribed: sub}
		return nil
	})
}

// PublicUnsubscribe implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/unsubscribe).
func (h *Handler) PublicUnsubscribe(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	if err := h.Comms.Unsubscribe(r.Context(), slug, token); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
