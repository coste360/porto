package middleware

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
)

// Cookie and header names.
const (
	SessionCookie = "porto_session"
	CSRFCookie    = "porto_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

// Authenticator resolves credentials into principals (implemented by auth.Service).
type Authenticator interface {
	AuthenticateSession(ctx context.Context, token string) (*auth.Principal, error)
	AuthenticateAPIKey(ctx context.Context, key string) (*auth.Principal, error)
	AuthenticateDevice(ctx context.Context, token string) (*auth.Principal, error)
}

// Authenticate reads the credentials of a request: `Authorization: Bearer po_live_…` (API key),
// `Authorization: Bearer po_dev_…` (device) or the porto_session cookie. A bearer token that does not
// authenticate is 401 at once; an invalid cookie leaves the request anonymous (public operations still work).
// Whether the operation accepts the method is decided by Enforce.
func Authenticate(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := reqctx.From(r.Context())
			var (
				p      *auth.Principal
				err    error
				method reqctx.AuthMethod
			)
			if h := r.Header.Get("Authorization"); h != "" {
				scheme, token, _ := strings.Cut(h, " ")
				token = strings.TrimSpace(token)
				switch {
				case !strings.EqualFold(scheme, "Bearer"):
					err = auth.ErrUnauthenticated
				case strings.HasPrefix(token, auth.APIKeyPrefix):
					p, err = a.AuthenticateAPIKey(r.Context(), token)
					method = reqctx.AuthAPIKey
				case strings.HasPrefix(token, auth.DeviceTokenPrefix):
					p, err = a.AuthenticateDevice(r.Context(), token)
					method = reqctx.AuthDevice
				default:
					err = auth.ErrUnauthenticated
				}
				if err != nil {
					if errors.Is(err, auth.ErrUnauthenticated) {
						w.Header().Set("WWW-Authenticate", `Bearer realm="porto"`)
						problem.Write(w, r, problem.Unauthorized)
						return
					}
					respond.Error(w, r, err)
					return
				}
			} else if c, cerr := r.Cookie(SessionCookie); cerr == nil && c.Value != "" {
				p, err = a.AuthenticateSession(r.Context(), c.Value)
				switch {
				case err == nil:
					method = reqctx.AuthSession
				case errors.Is(err, auth.ErrUnauthenticated):
					p = nil
				default:
					respond.Error(w, r, err)
					return
				}
			}
			if p != nil {
				info.AuthMethod = method
				r = r.WithContext(auth.WithPrincipal(r.Context(), p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EnforceOptions configure Enforce.
type EnforceOptions struct {
	// TOTPAllowed are the operation ids a session waiting for TOTP enrolment may call.
	TOTPAllowed []string
}

// Enforce applies the contract policy of the routed operation: public operations pass; the others need one of
// the accepted authentication methods (401 otherwise); cookie-authenticated unsafe requests need X-CSRF-Token
// equal to the porto_csrf cookie (403 csrf_failed); a session that must enrol TOTP only reaches the enrolment
// operations (403 totp_enrolment_required). It must run after Route and Authenticate.
func Enforce(ps Policies, o EnforceOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := reqctx.From(r.Context())
			pol, ok := ps.Lookup(r.Method, info.Route)
			if !ok {
				// Every generated route has a policy (TestPolicies_CoverEveryRoute); refuse rather than guess.
				problem.Write(w, r, problem.Forbidden)
				return
			}
			if pol.Public() {
				next.ServeHTTP(w, r)
				return
			}
			if info.AuthMethod == reqctx.AuthNone || !pol.Accepts(info.AuthMethod) {
				problem.Write(w, r, problem.Unauthorized)
				return
			}
			if info.AuthMethod == reqctx.AuthSession && unsafeMethod(r.Method) && !validCSRF(r) {
				problem.Write(w, r, problem.CSRFFailed)
				return
			}
			if p, ok := auth.PrincipalFrom(r.Context()); ok && p.TOTPEnrolmentRequired &&
				!slices.Contains(o.TOTPAllowed, pol.OperationID) {
				problem.Write(w, r, problem.TOTPEnrolmentRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validCSRF checks the double-submit token (constant time).
func validCSRF(r *http.Request) bool {
	h := r.Header.Get(CSRFHeader)
	c, err := r.Cookie(CSRFCookie)
	if err != nil || h == "" || c.Value == "" {
		return false
	}
	return crypto.ConstantTimeEqual(h, c.Value)
}

// SetSessionCookies sets porto_session (HttpOnly) and a fresh porto_csrf (readable by the SPA). Both expire at
// the absolute session expiry.
func SetSessionCookies(w http.ResponseWriter, token string, expires time.Time, secure bool) error {
	csrf, err := crypto.RandomToken(32)
	if err != nil {
		return err
	}
	//nolint:gosec // G124: Secure is false only in dev mode over plain http (config.SecureCookies)
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode,
	})
	//nolint:gosec // G124: the CSRF cookie is read by the SPA (double submit); Secure as above
	http.SetCookie(w, &http.Cookie{
		Name: CSRFCookie, Value: csrf, Path: "/", Expires: expires, HttpOnly: false, Secure: secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// CookieSecure reports whether session cookies of r carry the Secure attribute: always over TLS (the desktop
// local-network listener serves https while its base URL is the loopback http one), otherwise as configured
// (config.SecureCookies).
func CookieSecure(r *http.Request, configured bool) bool {
	return configured || r.TLS != nil
}

// ClearSessionCookies expires both cookies.
func ClearSessionCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		//nolint:gosec // G124: expiring cookies; same attributes as when they were set
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == SessionCookie, Secure: secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// SessionToken returns the session cookie value of a request ("" when absent).
func SessionToken(r *http.Request) string {
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}
