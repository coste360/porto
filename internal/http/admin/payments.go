package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// Payments, matches, refunds and deposits (m5-plan §7.3.2). Thin: decode, call internal/domain/payment, encode.

func matchesIn(in []api.MatchInput) []payment.MatchInput {
	out := make([]payment.MatchInput, 0, len(in))
	for _, m := range in {
		out = append(out, payment.MatchInput{ChargeID: m.ChargeId, Amount: money.Cents(m.AmountCents)})
	}
	return out
}

func writePayment(w http.ResponseWriter, d *payment.Detail, status int) {
	respond.SetETag(w, d.Version)
	respond.JSON(w, status, view.Payment(*d, true))
}

// ListPayments implements api.ServerInterface (GET .../payments).
func (h *Handler) ListPayments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListPaymentsParams) {
	d := newCtrDates()
	f := payment.Filter{
		Method: str(enumPtr(params.Method)), Kind: str(enumPtr(params.Kind)), Source: str(enumPtr(params.Source)),
		CustomerID: str(params.CustomerId), SessionID: str(params.SessionId), Unallocated: params.Unallocated != nil && *params.Unallocated,
	}
	if params.From != nil {
		f.From = d.parse(*params.From)
	}
	if params.To != nil {
		f.To = d.parse(*params.To)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Payments.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PaymentPage{Items: view.Payments(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// RecordPayment implements api.ServerInterface (POST .../payments).
func (h *Handler) RecordPayment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.RecordPaymentParams) {
	var in api.RecordPayment
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	rec := payment.RecordInput{
		CustomerID: str(in.CustomerId), Kind: string(in.Kind), Method: string(in.Method), Amount: money.Cents(in.AmountCents),
		Reference: str(in.Reference), Notes: str(in.Notes), AutoMatch: str(enumPtr(in.AutoMatch)),
		CashRegisterID: str(in.CashRegisterId), IdempotencyKey: str(params.IdempotencyKey),
		SendReceipt: in.SendReceipt != nil && *in.SendReceipt,
	}
	if in.ReceivedOn != nil {
		rec.ReceivedOn = d.parse(*in.ReceivedOn)
	}
	if in.Matches != nil {
		rec.Matches = matchesIn(*in.Matches)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	p, err := h.Payments.Record(r.Context(), principal(r), marinaID, rec)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePayment(w, p, http.StatusCreated)
}

// GetPayment implements api.ServerInterface (GET .../payments/{paymentId}).
func (h *Handler) GetPayment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, paymentID api.PaymentId) {
	p, err := h.Payments.Get(r.Context(), principal(r), marinaID, paymentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePayment(w, p, http.StatusOK)
}

// MatchPayment implements api.ServerInterface (POST .../payments/{paymentId}/matches).
func (h *Handler) MatchPayment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, paymentID api.PaymentId, _ api.MatchPaymentParams) {
	var in api.MatchPaymentInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p, err := h.Payments.Match(r.Context(), principal(r), marinaID, paymentID, matchesIn(in.Matches))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePayment(w, p, http.StatusOK)
}

// ReverseMatch implements api.ServerInterface (POST .../payments/{paymentId}/matches/{matchId}/reverse).
func (h *Handler) ReverseMatch(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, paymentID api.PaymentId, matchID api.PaymentMatchId, _ api.ReverseMatchParams) {
	p, err := h.Payments.ReverseMatch(r.Context(), principal(r), marinaID, paymentID, matchID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePayment(w, p, http.StatusOK)
}

// RefundPayment implements api.ServerInterface (POST .../payments/{paymentId}/refunds).
func (h *Handler) RefundPayment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, paymentID api.PaymentId, params api.RefundPaymentParams) {
	var in api.RefundInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	rin := payment.RefundInput{
		Amount: money.Cents(in.AmountCents), ViaProvider: in.ViaProvider != nil && *in.ViaProvider, Method: str(enumPtr(in.Method)),
		Reason: in.Reason, CashRegisterID: str(in.CashRegisterId), IdempotencyKey: str(params.IdempotencyKey),
	}
	if in.Unmatch != nil {
		for _, u := range *in.Unmatch {
			rin.Unmatch = append(rin.Unmatch, payment.UnmatchInput{MatchID: u.MatchId, Amount: money.Cents(u.AmountCents)})
		}
	}
	ref, err := h.Payments.Refund(r.Context(), principal(r), marinaID, paymentID, rin)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.PaymentRefund(*ref))
}

// RetainDeposit implements api.ServerInterface (POST .../payments/{paymentId}/retain).
func (h *Handler) RetainDeposit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, paymentID api.PaymentId, _ api.RetainDepositParams) {
	var in api.RetainInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ch, err := h.Payments.Retain(r.Context(), principal(r), marinaID, paymentID, payment.RetainInput{
		Amount: money.Cents(in.AmountCents), ItemID: str(in.ItemId), Reason: in.Reason,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, ch.Version)
	respond.JSON(w, http.StatusCreated, view.LedgerCharge(ch, ""))
}

// ListRefunds implements api.ServerInterface (GET .../refunds).
func (h *Handler) ListRefunds(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRefundsParams) {
	d := newCtrDates()
	f := payment.RefundFilter{PaymentID: str(params.PaymentId), Status: str(enumPtr(params.Status))}
	if params.From != nil {
		f.From = d.parse(*params.From)
	}
	if params.To != nil {
		f.To = d.parse(*params.To)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Payments.ListRefunds(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RefundPage{Items: view.PaymentRefunds(page.Items), NextCursor: nextCursor(page.NextCursor)})
}
