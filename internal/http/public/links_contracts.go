package public

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Customer magic-link operations of quotes and renewals (m4-plan §7.3.6): resolve (with the purpose dispatch),
// document download, quote acceptance, document upload, renewal accept and decline. Every unknown, expired,
// revoked or foreign token is 404 link_invalid (INV-75).

// ViewFiller fills the page of a purpose this file does not own (signature, unsubscribe): signing.go and
// unsubscribe.go register theirs in init. The link was resolved already; an error is answered as it is.
type ViewFiller func(h *Handler, r *http.Request, slug, token string, out *api.PublicLink) error

// publicViews are the fillers by purpose.
var publicViews = map[string]ViewFiller{}

// RegisterPublicView registers the filler of a purpose (called from init of the owning file).
func RegisterPublicView(purpose string, f ViewFiller) { publicViews[purpose] = f }

// linkHeaders are the headers of every magic-link response (m4-plan §6.6).
func linkHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ResolvePublicLink implements api.ServerInterface (GET /api/public/v1/{slug}/links/{token}).
func (h *Handler) ResolvePublicLink(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	v, err := h.Contracts.ResolvePublicLink(r.Context(), slug, token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.PublicLink{Purpose: api.LinkPurpose(v.Link.Purpose), ExpiresAt: v.Link.ExpiresAt.UTC()}
	out.Marina.Name, out.Marina.DefaultLocale = v.Marina.Name, api.Locale(v.Marina.DefaultLocale)
	out.Marina.Locales = view.Locales(v.Marina.Locales)
	if q := v.Quote; q != nil {
		pq := &api.PublicQuote{
			Code: q.Code, Status: api.QuoteStatus(q.Status), ValidUntil: q.ValidUntil.String(), CustomerName: strp(q.CustomerName),
			VesselName: q.VesselName, Options: []api.PublicQuoteOption{}, RequiredFields: []api.RequiredField{}, MissingFields: []api.RequiredField{},
		}
		for _, f := range q.RequiredFields {
			pq.RequiredFields = append(pq.RequiredFields, api.RequiredField(f))
		}
		for _, f := range q.MissingFields {
			pq.MissingFields = append(pq.MissingFields, api.RequiredField(f))
		}
		for _, o := range q.Options {
			po := api.PublicQuoteOption{
				Id: o.ID, Label: o.Label, Seq: o.Seq, StartsOn: o.StartsOn.String(), EndsOn: o.EndsOn.String(), TotalCents: o.Total,
				DepositCents: o.Deposit, EarlyPaymentTotalCents: o.EarlyPaymentTotal,
			}
			if len(o.ResourceCodes) > 0 {
				codes := o.ResourceCodes
				po.ResourceCodes = &codes
			}
			pq.Options = append(pq.Options, po)
		}
		out.Quote = pq
	}
	if rn := v.Renewal; rn != nil {
		pr := &api.PublicRenewal{
			Status: api.RenewalProposalStatus(rn.Status), DeadlineOn: rn.DeadlineOn.String(), NewStartsOn: rn.NewStartsOn.String(),
			NewEndsOn: rn.NewEndsOn.String(), OldTotalCents: rn.OldTotal, NewTotalCents: rn.NewTotal,
			EarlyPaymentTotalCents: rn.EarlyPaymentTotal, CustomerName: strp(rn.CustomerName), VesselName: rn.VesselName,
		}
		if len(rn.ResourceCodes) > 0 {
			codes := rn.ResourceCodes
			pr.ResourceCodes = &codes
		}
		out.Renewal = pr
	}
	if f, ok := publicViews[v.Link.Purpose]; ok {
		if err := f(h, r, slug, token, &out); err != nil {
			respond.Error(w, r, err)
			return
		}
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetPublicLinkDocument implements api.ServerInterface (GET /api/public/v1/{slug}/links/{token}/document).
func (h *Handler) GetPublicLinkDocument(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var b []byte
	var err error
	if l, lerr := h.Links.ResolvePurpose(r.Context(), slug, token, magiclink.PurposeSignature); lerr == nil {
		// A signature link downloads the document to sign (or the signed one once complete).
		b, err = h.Signatures.PublicDocument(r.Context(), l)
	} else {
		b, err = h.Contracts.PublicLinkDocument(r.Context(), slug, token)
	}
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="documento.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func publicDeposit(d *contract.PublicDeposit) *api.PublicDeposit {
	if d == nil {
		return nil
	}
	out := &api.PublicDeposit{AmountCents: d.Amount, DueOn: d.DueOn.String(), Reference: strp(d.Ref)}
	if b := d.Bank; b != nil {
		out.BankDetails = &api.BankDetails{Iban: strp(b.IBAN), Holder: strp(b.Holder), Bank: strp(b.Bank), Bic: strp(b.BIC)}
	}
	return out
}

// AcceptPublicQuote implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/quote/accept).
func (h *Handler) AcceptPublicQuote(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in api.PublicQuoteAccept
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	plan := ""
	if in.Plan != nil {
		plan = string(*in.Plan)
	}
	var data contract.PublicData
	if c := in.Customer; c != nil {
		data.FiscalCode = str(c.FiscalCode)
		if a := c.Address; a != nil {
			data.Address = &customer.Address{
				Line1: str(a.Line1), Line2: str(a.Line2), PostalCode: str(a.PostalCode), City: str(a.City),
				Province: str(a.Province), Country: str(a.Country),
			}
		}
	}
	if v := in.Vessel; v != nil {
		data.VesselRegistration = str(v.Registration)
	}
	res, err := h.Contracts.AcceptPublicQuote(r.Context(), slug, token, in.OptionId, plan, data)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PublicAcceptResult{ContractCode: res.ContractCode, Deposit: publicDeposit(res.Deposit)})
}

// maxPublicUpload bounds a customer upload (m4-plan §7.3.6).
const maxPublicUpload = 15 << 20

// UploadPublicDocument implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/documents).
func (h *Handler) UploadPublicDocument(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token, params api.UploadPublicDocumentParams) {
	linkHeaders(w)
	docType := "insurance_policy"
	if params.Type != nil {
		docType = string(*params.Type)
	}
	var expires calendar.Date
	if params.ExpiresOn != nil {
		d, err := calendar.Parse(*params.ExpiresOn)
		if err != nil {
			respond.Problem(w, r, problem.ValidationFailed)
			return
		}
		expires = d
	}
	name := "documento"
	if params.XFileName != nil {
		name = *params.XFileName
	}
	body := http.MaxBytesReader(w, r.Body, maxPublicUpload)
	id, err := h.Contracts.UploadPublicDocument(r.Context(), slug, token, docType, expires, r.Header.Get("Content-Type"), name, body)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.PublicDocumentUploaded{DocumentId: id})
}

// AcceptPublicRenewal implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/renewal/accept).
func (h *Handler) AcceptPublicRenewal(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in *api.RenewalAccept
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	plan := ""
	if in != nil && in.Plan != nil {
		plan = string(*in.Plan)
	}
	res, err := h.Contracts.AcceptPublicRenewal(r.Context(), slug, token, plan)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PublicRenewalResult{Status: api.RenewalProposalStatus(res.ProposalStatus), Deposit: publicDeposit(res.Deposit)})
}

// DeclinePublicRenewal implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/renewal/decline).
func (h *Handler) DeclinePublicRenewal(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in *api.ReasonInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	reason := ""
	if in != nil && in.Reason != nil {
		reason = *in.Reason
	}
	res, err := h.Contracts.DeclinePublicRenewal(r.Context(), slug, token, reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.PublicRenewalResult{Status: api.RenewalProposalStatus(res.ProposalStatus)})
}

// str dereferences an optional string ("" when absent).
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
