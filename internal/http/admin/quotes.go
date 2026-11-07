package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Quote operations (m3-plan §7.3).

func ctrQuoteInput(d *ctrDates, in api.QuoteInput) contract.QuoteInput {
	q := contract.QuoteInput{
		Kind: string(in.Kind), Term: str(enumPtr(in.Term)), CustomerID: in.CustomerId, VesselID: in.VesselId,
		StartsOn: d.parse(in.StartsOn), EndsOn: d.parse(in.EndsOn), ValidUntil: d.ptr(in.ValidUntil),
		Channel: str(enumPtr(in.Channel)), Policy: ctrPolicyIn(in.Policy), Notes: str(in.Notes), InquiryID: str(in.InquiryId),
	}
	for _, o := range in.Options {
		q.Options = append(q.Options, contract.OptionInput{
			Label: str(o.Label), Hold: o.Hold, Placements: ctrSegments(d, o.Placements), Services: ctrServices(o.Services),
			FitOverride: ctrFit(o.FitOverride),
		})
	}
	return q
}

func writeQuote(w http.ResponseWriter, q *contract.QuoteDetail, status int) {
	respond.SetETag(w, q.Row.Version)
	respond.JSON(w, status, view.Quote(q))
}

// ListQuotes implements api.ServerInterface (GET .../quotes).
func (h *Handler) ListQuotes(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListQuotesParams) {
	f := contract.QuoteFilter{
		Status: str(enumPtr(params.Status)), Kind: str(enumPtr(params.Kind)), CustomerID: str(params.CustomerId), Search: str(params.Q),
	}
	page, err := h.Contracts.ListQuotes(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.QuotePage{Items: view.QuoteSummaries(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateQuote implements api.ServerInterface (POST .../quotes).
func (h *Handler) CreateQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateQuoteParams) {
	var in api.QuoteInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	qi := ctrQuoteInput(d, in)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	q, err := h.Contracts.CreateQuote(r.Context(), principal(r), marinaID, qi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeQuote(w, q, http.StatusCreated)
}

// GetQuote implements api.ServerInterface (GET .../quotes/{quoteId}).
func (h *Handler) GetQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, quoteID api.QuoteId) {
	q, err := h.Contracts.GetQuote(r.Context(), principal(r), marinaID, quoteID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeQuote(w, q, http.StatusOK)
}

// UpdateQuote implements api.ServerInterface (PATCH .../quotes/{quoteId}): replaces a draft quote.
func (h *Handler) UpdateQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, quoteID api.QuoteId, params api.UpdateQuoteParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.QuoteInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	qi := ctrQuoteInput(d, in)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	q, err := h.Contracts.UpdateQuote(r.Context(), principal(r), marinaID, quoteID, ifMatch, qi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeQuote(w, q, http.StatusOK)
}

// SendQuote implements api.ServerInterface (POST .../quotes/{quoteId}/send).
//
// M4 contract phase: every call still runs the M3 manual mark (no PDF, no magic link, no email); CONTRACTS replaces
// the non-manual path with the 13.2 send (m4-plan §3.2). The response already has the M4 shape.
func (h *Handler) SendQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, quoteID api.QuoteId, params api.SendQuoteParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	res, err := h.Contracts.SendQuote(r.Context(), principal(r), marinaID, quoteID, ifMatch, boolOr(params.Manual, false))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Quote.Row.Version)
	respond.JSON(w, http.StatusOK, api.QuoteSendResult{Quote: view.Quote(res.Quote), DocumentId: view.String(res.DocumentID), MessageId: view.String(res.MessageID)})
}

// AcceptQuote implements api.ServerInterface (POST .../quotes/{quoteId}/accept).
func (h *Handler) AcceptQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, quoteID api.QuoteId, params api.AcceptQuoteParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.QuoteAccept
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ai := contract.AcceptInput{OptionID: in.OptionId, Confirm: ctrManual(in.Confirm), Plan: str(enumPtr(in.Plan))}
	if in.Activate != nil {
		ai.Activate = *in.Activate
	}
	if in.Signers != nil {
		ai.Signers = ctrSignatureIn(&api.SignatureRequestInput{Signers: in.Signers}).Signers
	}
	res, err := h.Contracts.AcceptQuoteM4(r.Context(), principal(r), marinaID, quoteID, ifMatch, ai)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.QuoteAcceptResult{
		Quote: view.Quote(res.Quote), Contract: view.ContractM4(res.Contract), SignatureRequestId: view.String(res.SignatureRequestID),
		DocumentId: view.String(res.DocumentID),
	}
	if len(res.Warnings) > 0 {
		ws := make([]api.WriteWarning, 0, len(res.Warnings))
		for _, w := range res.Warnings {
			ws = append(ws, api.WriteWarning(w))
		}
		out.Warnings = &ws
	}
	respond.SetETag(w, res.Quote.Row.Version)
	respond.JSON(w, http.StatusOK, out)
}

// DeclineQuote implements api.ServerInterface (POST .../quotes/{quoteId}/decline).
func (h *Handler) DeclineQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, quoteID api.QuoteId, params api.DeclineQuoteParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason, ok := ctrReason(r, w)
	if !ok {
		return
	}
	q, err := h.Contracts.DeclineQuote(r.Context(), principal(r), marinaID, quoteID, ifMatch, reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeQuote(w, q, http.StatusOK)
}
