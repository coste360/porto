// Package admin implements the /api/admin/v1 operations of the generated contract. Handlers are thin: decode,
// call the domain service (which authorises), map errors, encode.
package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auditlog"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/meta"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/sse"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/reqctx"
)

// Handler serves the admin API.
type Handler struct {
	Clock         clock.Clock
	Auth          *auth.Service
	Team          *team.Service
	Marinas       *marina.Service
	APIKeys       *apikey.Service
	Devices       *device.Service
	Webhooks      *webhook.Service
	Audit         *auditlog.Service
	SSE           *sse.Server
	Meta          meta.Meta
	SecureCookies bool

	// M1
	Harbor    *harbor.Service
	Customers *customer.Service
	Vessels   *vessel.Service
	Documents *document.Service
	Deadlines *deadline.Service

	// M2
	Catalog    *catalog.Service
	PriceLists *pricelist.Service

	// M3
	Occupancy *occupancy.Service
	Contracts *contract.Service
	Presence  *presence.Service

	// M4
	Rights     *rights.Service
	Docs       *docs.Service
	Signatures *signature.Service
	Links      *magiclink.Service
	Comms      *comms.Service
	Imports    *csvimport.Registry

	// M5 (m5-plan §7.3): charges and exports (CHARGES-EXPORT), ledger (PAYMENTS), cash, fiscal and collection
	// (CASH-DRIVERS).
	Charges    *charge.Service
	Payments   *payment.Service
	Cash       *cash.Service
	Fiscal     *fiscal.Service
	Collect    *collect.Service
	PayDrivers *paydriver.Registry

	// M7 (m7-plan §8): reports, dashboard and comparison.
	Reports *report.Service
}

func principal(r *http.Request) *auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetMeta implements api.ServerInterface.
func (h *Handler) GetMeta(w http.ResponseWriter, _ *http.Request) {
	caps := h.Meta.AllCapabilities()
	edition := h.Meta.Edition
	if edition == "" {
		edition = meta.EditionServer
	}
	respond.JSON(w, http.StatusOK, api.Meta{
		Version: h.Meta.Version, ApiVersion: h.Meta.APIVersion, Edition: api.MetaEdition(edition),
		Capabilities: caps, DemoPairing: h.Meta.DemoPairing, LocalMode: h.Meta.LocalMode,
		PricingEngineVersion: h.Meta.PricingEngineVersion,
	})
}

// ---- auth ----

func (h *Handler) writeMe(w http.ResponseWriter, r *http.Request, p *auth.Principal, method reqctx.AuthMethod) {
	me, err := h.Auth.Me(r.Context(), p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Me(me, method))
}

// Login implements api.ServerInterface.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in api.LoginRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Auth.Login(r.Context(), auth.LoginInput{
		Email: in.Email, Password: in.Password, TOTPCode: str(in.TotpCode), RecoveryCode: str(in.RecoveryCode),
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidTOTP) {
			respond.Problem(w, r, problem.InvalidTOTP, problem.Status(http.StatusUnauthorized))
			return
		}
		respond.Error(w, r, err)
		return
	}
	if err := middleware.SetSessionCookies(w, res.Token, res.ExpiresAt, middleware.CookieSecure(r, h.SecureCookies)); err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeMe(w, r, res.Principal, reqctx.AuthSession)
}

// Logout implements api.ServerInterface.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Auth.Logout(r.Context(), middleware.SessionToken(r)); err != nil {
		respond.Error(w, r, err)
		return
	}
	middleware.ClearSessionCookies(w, middleware.CookieSecure(r, h.SecureCookies))
	respond.NoContent(w)
}

// GetMe implements api.ServerInterface.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	h.writeMe(w, r, principal(r), reqctx.From(r.Context()).AuthMethod)
}

// UpdateMe implements api.ServerInterface.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var in api.MePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	var locale *string
	if in.Locale != nil {
		l := string(*in.Locale)
		locale = &l
	}
	p := principal(r)
	if err := h.Auth.UpdateProfile(r.Context(), p, in.Name, locale); err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeMe(w, r, p, reqctx.From(r.Context()).AuthMethod)
}

// ChangePassword implements api.ServerInterface.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var in api.ChangePasswordRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Auth.ChangePassword(r.Context(), principal(r), in.CurrentPassword, in.NewPassword); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// RequestPasswordReset implements api.ServerInterface.
func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in api.PasswordResetRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Auth.RequestPasswordReset(r.Context(), in.Email); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ConfirmPasswordReset implements api.ServerInterface.
func (h *Handler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in api.PasswordResetConfirm
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Auth.ConfirmPasswordReset(r.Context(), in.Token, in.NewPassword); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// EnrollTotp implements api.ServerInterface.
func (h *Handler) EnrollTotp(w http.ResponseWriter, r *http.Request) {
	e, err := h.Auth.BeginTOTPEnrolment(r.Context(), principal(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.TotpEnrollment{Secret: e.Secret, OtpauthUrl: e.OTPAuthURL, QrSvg: e.QRSVG})
}

// ConfirmTotp implements api.ServerInterface.
func (h *Handler) ConfirmTotp(w http.ResponseWriter, r *http.Request) {
	var in api.TotpCode
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	codes, token, err := h.Auth.ConfirmTOTP(r.Context(), principal(r), in.Code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if token != "" {
		if err := middleware.SetSessionCookies(w, token, h.Clock.Now().Add(auth.SessionAbsolute), middleware.CookieSecure(r, h.SecureCookies)); err != nil {
			respond.Error(w, r, err)
			return
		}
	}
	respond.JSON(w, http.StatusOK, api.RecoveryCodes{Codes: codes})
}

// DisableTotp implements api.ServerInterface.
func (h *Handler) DisableTotp(w http.ResponseWriter, r *http.Request) {
	var in api.TotpDisableRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Auth.DisableTOTP(r.Context(), principal(r), in.Password, in.Code); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// RegenerateRecoveryCodes implements api.ServerInterface.
func (h *Handler) RegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var in api.TotpCode
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	codes, err := h.Auth.RegenerateRecoveryCodes(r.Context(), principal(r), in.Code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RecoveryCodes{Codes: codes})
}

// ---- event types and stream ----

// ListEventTypes implements api.ServerInterface.
func (h *Handler) ListEventTypes(w http.ResponseWriter, _ *http.Request) {
	types := event.Types()
	out := api.EventTypeList{Items: make([]api.EventType, 0, len(types))}
	for _, t := range types {
		out.Items = append(out.Items, api.EventType{Type: t.Type, Topic: t.Topic, Public: t.Public})
	}
	respond.JSON(w, http.StatusOK, out)
}

// Stream implements api.ServerInterface.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request, params api.StreamParams) {
	p := principal(r)
	// GetMarina authorises marina.read; another marina is 404.
	if _, err := h.Marinas.GetMarina(r.Context(), p, params.MarinaId); err != nil {
		respond.Error(w, r, err)
		return
	}
	err := h.SSE.Serve(w, r, p.ID(), params.MarinaId, str(params.LastEventID))
	if errors.Is(err, sse.ErrTooManyStreams) {
		respond.Problem(w, r, problem.RateLimited, problem.RetryAfter(10*time.Second),
			problem.Detail("too many concurrent streams"))
		return
	}
	if err != nil {
		respond.Error(w, r, err)
	}
}
