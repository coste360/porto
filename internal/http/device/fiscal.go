package device

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// Fiscal bridge claims of a paired desktop in remote mode (m6b-plan §8.4).

func refundFlag(refundID string) bool { return refundID != "" }

// ClaimFiscalDocuments implements api.ServerInterface.
func (h *Handler) ClaimFiscalDocuments(w http.ResponseWriter, r *http.Request) {
	if h.Fiscal == nil {
		problem.Write(w, r, problem.NotFound)
		return
	}
	var in api.FiscalClaimRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if in.Max < 1 || in.Max > fiscal.MaxBridgeClaim {
		problem.Write(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "max", Code: "range"}))
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	claims, err := h.Fiscal.ClaimBridge(r.Context(), p, in.Max)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.FiscalClaim{Items: make([]api.FiscalClaimDocument, 0, len(claims))}
	for _, c := range claims {
		d := api.FiscalClaimDocument{
			DocumentId: c.Document.ID, Number: c.Document.Number, BusinessDate: c.Document.BusinessDate.String(),
			ClaimId: c.ClaimID, ClaimUntil: c.ClaimUntil, Method: c.Method, TotalCents: c.Document.TotalCents,
			Refund: refundFlag(c.Document.RefundID), Lines: []api.ReceiptDataLine{}, Vat: []api.ReceiptDataVat{},
		}
		for _, l := range c.Lines {
			d.Lines = append(d.Lines, api.ReceiptDataLine{
				Description: l.Description, QtyMilli: l.QtyMilli, GrossCents: int64(l.Gross), VatRateBp: int(l.VATRateBP),
			})
		}
		for _, v := range c.VAT {
			d.Vat = append(d.Vat, api.ReceiptDataVat{
				VatRateBp: int(v.RateBP), VatNature: v.Nature, NetCents: int64(v.Net), VatCents: int64(v.VAT), GrossCents: int64(v.Gross),
			})
		}
		out.Items = append(out.Items, d)
	}
	respond.JSON(w, http.StatusOK, out)
}

// ReportFiscalOutcome implements api.ServerInterface (Idempotency-Key <documentId>:<claim_id>, replayed by the
// idempotency middleware).
func (h *Handler) ReportFiscalOutcome(w http.ResponseWriter, r *http.Request, documentID string, _ api.ReportFiscalOutcomeParams) {
	if h.Fiscal == nil {
		problem.Write(w, r, problem.NotFound)
		return
	}
	var in api.FiscalOutcome
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	o := fiscal.BridgeOutcomeInput{ClaimID: in.ClaimId, Status: string(in.Status), ExternalRef: str(in.ExternalRef), ErrorCode: str(in.ErrorCode)}
	if in.IssuedAt != nil {
		o.IssuedAt = *in.IssuedAt
	}
	p, _ := auth.PrincipalFrom(r.Context())
	if err := h.Fiscal.BridgeOutcome(r.Context(), p, documentID, o); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
