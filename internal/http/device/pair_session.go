package device

import (
	"errors"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// PairDeviceSession implements api.ServerInterface: a browser that opened the pairing QR exchanges the code for
// a device session (m6a-plan §3.2). The session token only travels in the porto_session cookie.
func (h *Handler) PairDeviceSession(w http.ResponseWriter, r *http.Request) {
	var in api.PairSessionRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Devices.PairSession(r.Context(), device.PairInput{
		Code: in.Code, Name: in.Name, Platform: string(in.Platform), Kind: string(in.Kind),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if err := middleware.SetSessionCookies(w, res.SessionToken, res.ExpiresAt, h.secureCookies(r)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.PairSessionResult{
		Device:      view.Device(&res.Device),
		Marina:      api.MarinaRef{Id: res.MarinaID, Slug: res.MarinaSlug, Name: res.MarinaName},
		User:        api.UserRef{Id: res.UserID, Name: res.UserName},
		Permissions: view.Permissions(res.Permissions), TotpRequired: res.TOTPRequired,
	})
}

// VerifyDeviceSessionTotp implements api.ServerInterface: the second factor of a device session just created by
// PairDeviceSession for a user with TOTP, entered on the phone. The session comes from the porto_session cookie.
func (h *Handler) VerifyDeviceSessionTotp(w http.ResponseWriter, r *http.Request) {
	var in api.DeviceSessionTotpRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	code, recovery := deref(in.TotpCode), deref(in.RecoveryCode)
	if (code == "") == (recovery == "") {
		respond.Problem(w, r, problem.BadRequest, problem.Detail("exactly one of totp_code and recovery_code"))
		return
	}
	sess, err := h.Auth.VerifyDeviceSessionTOTP(r.Context(), middleware.SessionToken(r), code, recovery)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidTOTP) {
			respond.Problem(w, r, problem.InvalidTOTP, problem.Status(http.StatusUnauthorized))
			return
		}
		respond.Error(w, r, err)
		return
	}
	// The verified session replaces the pending one: new porto_session and porto_csrf cookies.
	if sess != nil {
		if err := middleware.SetSessionCookies(w, sess.Token, sess.ExpiresAt, h.secureCookies(r)); err != nil {
			respond.Error(w, r, err)
			return
		}
	}
	respond.NoContent(w)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// secureCookies: Secure over TLS, when configured, and whenever the base URL is https (a TLS-terminating proxy).
func (h *Handler) secureCookies(r *http.Request) bool {
	return middleware.CookieSecure(r, h.SecureCookies || strings.HasPrefix(h.BaseURL, "https://"))
}
