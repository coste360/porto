package public

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Yard quote magic links (M10, m10-plan §4.3, §13): resolvePublicLink returns PublicLink.yard_quote for
// access_links.subject_type yard_quote (dispatched from links_contracts.go) and the customer accepts here; the work
// order is created in the same transaction. Owned by QUOTE.

func yardIsoDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

// resolveYardQuoteLink answers resolvePublicLink for a yard quote link.
func (h *Handler) resolveYardQuoteLink(w http.ResponseWriter, r *http.Request, slug, token string) {
	q, l, err := h.YardQuotes.PublicLinkQuote(r.Context(), slug, token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	m, err := h.Marinas.PublicBySlug(r.Context(), slug)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.PublicLink{Purpose: api.LinkPurpose(l.Purpose), ExpiresAt: l.ExpiresAt.UTC()}
	out.Marina.Name, out.Marina.DefaultLocale, out.Marina.Locales = m.Name, api.Locale(m.DefaultLocale), view.Locales(m.Locales)
	pq := &api.PublicYardQuote{
		Number: q.Number, Kind: api.YardQuoteKind(q.Kind), Status: api.YardQuoteStatus(q.Status), ValidUntil: q.ValidUntil.String(),
		VesselName: q.VesselName, WorkOrderNumber: view.String(q.WorkOrderNumber), NetCents: int64(q.Net), VatCents: int64(q.VAT),
		GrossCents: int64(q.Gross), DepositAmountCents: int64(q.Deposit), BillingMode: api.YardBillingMode(q.BillingMode),
		Conditions: q.Conditions, PlannedStartOn: yardIsoDate(q.PlannedStartOn), PlannedDeliveryOn: yardIsoDate(q.PlannedDeliveryOn),
		CanAccept: q.CanAccept, Lines: []api.PublicYardQuoteLine{},
	}
	for _, ln := range q.Lines {
		pl := api.PublicYardQuoteLine{
			Description: ln.Description, QtyMilli: int64(ln.Qty), UnitCents: int64(ln.UnitCents), NetCents: int64(ln.Net), VatRateBp: ln.VATRateBP,
		}
		if ln.Unit != "" {
			u := api.ItemUnit(ln.Unit)
			pl.Unit = &u
		}
		if ln.Discount > 0 {
			d := int64(ln.Discount)
			pl.DiscountCents = &d
		}
		pq.Lines = append(pq.Lines, pl)
	}
	out.YardQuote = pq
	respond.JSON(w, http.StatusOK, out)
}

// AcceptPublicYardQuote implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/yard-quote/accept).
func (h *Handler) AcceptPublicYardQuote(w http.ResponseWriter, r *http.Request, slug string, token string) {
	linkHeaders(w)
	var in api.PublicYardQuoteAccept
	if err := respond.Decode(r, &in, true); err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	res, err := h.YardQuotes.AcceptPublic(r.Context(), slug, token, in.AcceptConditions != nil && *in.AcceptConditions)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PublicYardQuoteAcceptResult{QuoteNumber: res.QuoteNumber, WorkOrderNumber: view.String(res.WorkOrderNumber)})
}
