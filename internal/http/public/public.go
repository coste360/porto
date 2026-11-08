// Package public implements /api/public/v1: what anyone may read about a marina (M0: name and locales) and, from
// M4, the customer magic-link pages (/api/public/v1/{slug}/links/{token}; the token is the only credential and is
// never logged).
package public

import (
	"net/http"
	"slices"

	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/portal"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Handler serves the public API.
type Handler struct {
	Marinas *marina.Service

	// M4 customer magic-link pages (m4-plan §7.3.6): quote and renewal (CONTRACTS), signing and unsubscribe
	// (DOCUMENTS).
	Contracts  *contract.Service
	Links      *magiclink.Service
	Signatures *signature.Service
	Comms      *comms.Service
	Docs       *docs.Service

	// M5 public pay page of payment requests (m5-plan §5.5, CASH-DRIVERS).
	Collect *collect.Service

	// M8 public booking page (booking.go, ENGINE/PUBLIC) and customer area (account.go, ACCOUNT) (m8-plan §11).
	Booking *booking.Service
	Portal  *portal.Service

	// M10 yard quote links (yard_quote_links.go, QUOTE; m10-plan §13).
	YardQuotes *yardquote.Service
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
	if len(m.EmbedOrigins) > 0 {
		origins := slices.Clone(m.EmbedOrigins)
		body.EmbedOrigins = &origins
	}
	respond.JSON(w, http.StatusOK, body)
}
