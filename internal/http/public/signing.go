package public

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
)

// Customer signing pages (m4-plan §6.5, §7.3.6): the OTP "firma elettronica semplice" of a signature link. The token
// is the only credential and is never logged.

func init() {
	RegisterPublicView(magiclink.PurposeSignature, func(h *Handler, r *http.Request, slug, token string, out *api.PublicLink) error {
		l, err := h.Links.ResolvePurpose(r.Context(), slug, token, magiclink.PurposeSignature)
		if err != nil {
			return err
		}
		v, err := h.Signatures.PublicView(r.Context(), l, clientMeta(r))
		if err != nil {
			return err
		}
		kind := api.FirmaElettronicaSemplice
		ps := &api.PublicSignature{
			RequestStatus: api.SignatureRequestStatus(v.RequestStatus), SignerStatus: api.SignerStatus(v.SignerStatus),
			Role: api.SignerRole(v.Role), DocumentTitle: v.DocumentTitle, OtpChannel: api.PublicSignatureOtpChannelEmail,
			AttemptsLeft: v.AttemptsLeft, SignatureKind: &kind,
		}
		ps.Identity.Name, ps.Identity.FiscalCode = strp(v.Name), strp(v.FiscalCode)
		out.Signature = ps
		return nil
	})
}

// clientMeta are the request data recorded in the signature evidence.
func clientMeta(r *http.Request) signature.ClientMeta {
	m := signature.ClientMeta{UserAgent: r.UserAgent()}
	if ip := reqctx.From(r.Context()).ClientIP; ip.IsValid() {
		m.IP = ip.String()
	}
	return m
}

func signatureResult(res *signature.PublicResult) api.PublicSignatureResult {
	out := api.PublicSignatureResult{SignerStatus: api.SignerStatus(res.SignerStatus), RequestStatus: api.SignatureRequestStatus(res.RequestStatus)}
	if res.AttemptsLeft > 0 {
		n := res.AttemptsLeft
		out.AttemptsLeft = &n
	}
	return out
}

// RequestPublicSignatureOtp implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/signature/otp).
func (h *Handler) RequestPublicSignatureOtp(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in api.PublicOtpRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Signatures.RequestOTP(r.Context(), slug, token, in.IdentityConfirmed, clientMeta(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, signatureResult(res))
}

// VerifyPublicSignature implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/signature/verify).
func (h *Handler) VerifyPublicSignature(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in api.PublicOtpVerify
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	// Resolved before the verification: completing the request revokes the link.
	l, _ := h.Links.ResolvePurpose(r.Context(), slug, token, magiclink.PurposeSignature)
	res, err := h.Signatures.VerifyOTP(r.Context(), slug, token, in.Code, clientMeta(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := signatureResult(res)
	if res.RequestStatus == signature.StatusSigned && l != nil {
		// The deposit instructions of a contract just signed (the first instalment is issued at signature).
		if dep, err := h.Contracts.DepositOfSigner(r.Context(), l); err == nil {
			out.Deposit = publicDeposit(dep)
		}
	}
	respond.JSON(w, http.StatusOK, out)
}

// DeclinePublicSignature implements api.ServerInterface (POST /api/public/v1/{slug}/links/{token}/signature/decline).
func (h *Handler) DeclinePublicSignature(w http.ResponseWriter, r *http.Request, slug api.Slug, token api.Token) {
	linkHeaders(w)
	var in api.ReasonInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	reason := ""
	if in.Reason != nil {
		reason = *in.Reason
	}
	res, err := h.Signatures.DeclinePublic(r.Context(), slug, token, reason, clientMeta(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, signatureResult(res))
}
