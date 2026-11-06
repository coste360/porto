// Package public implements /api/public/v1: what anyone may read about a marina (M0: name and locales).
package public

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Handler serves the public API.
type Handler struct {
	Marinas *marina.Service
}

// GetPublicMarina implements api.ServerInterface.
func (h *Handler) GetPublicMarina(w http.ResponseWriter, r *http.Request, slug string) {
	m, err := h.Marinas.PublicBySlug(r.Context(), slug)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	body := api.PublicMarina{
		Slug: m.Slug, Name: m.Name, DefaultLocale: api.Locale(m.DefaultLocale), Locales: view.Locales(m.Locales),
		BookingEnabled: m.BookingEnabled,
	}
	respond.JSON(w, http.StatusOK, body)
}
