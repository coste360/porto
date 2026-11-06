// Package httpserver assembles Porto's HTTP router: the global middleware chain, the embedded SPAs and the
// generated API routes with their per-operation middleware (contract policies, rate limits, idempotency).
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apispec "github.com/coste360/porto/api"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/http/admin"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/device"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/public"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/static"
)

// MaxBodyBytes is the request body limit (m0-plan §5).
const MaxBodyBytes = 1 << 20

// Fixed limits (m0-plan §5).
const (
	pairingPerMinute = 10
	apiKeyPerMinute  = 600
)

// totpEnrolmentOperations are what a session that must enrol TOTP may still call.
var totpEnrolmentOperations = []string{"getMe", "logout", "enrollTotp", "confirmTotp"}

// Deps are the collaborators of the router.
type Deps struct {
	Log   *slog.Logger
	Clock clock.Clock

	Admin  *admin.Handler
	Device *device.Handler
	Public *public.Handler

	Authenticator middleware.Authenticator
	Idempotency   middleware.IdempotencyStore
	// Ready reports the applied schema version, or an error when the instance cannot serve (readyz).
	Ready func(ctx context.Context) (int64, error)

	TrustedProxies       []netip.Prefix
	HSTS                 bool
	PublicReadsPerMinute int

	// AdminApp and BookingApp default to the embedded builds.
	AdminApp, BookingApp *static.App
	// ExtraRoutes mounts extension routes (pkg/ext.Extensions.HTTPRoutes).
	ExtraRoutes func(r chi.Router)
}

// server is the full api.ServerInterface: the three handler groups plus the probes.
type server struct {
	adminAPI
	deviceAPI
	publicAPI
	ready func(ctx context.Context) (int64, error)
}

type (
	adminAPI  struct{ *admin.Handler }
	deviceAPI struct{ *device.Handler }
	publicAPI struct{ *public.Handler }
)

var _ api.ServerInterface = (*server)(nil)

// Healthz implements api.ServerInterface.
func (s *server) Healthz(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, api.HealthStatus{Status: api.HealthStatusStatusOk})
}

// Readyz implements api.ServerInterface.
func (s *server) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	v, err := s.ready(ctx)
	if err != nil {
		respond.Logger(r.Context()).WarnContext(r.Context(), "not ready", "error", err)
		respond.Problem(w, r, problem.NotReady)
		return
	}
	respond.JSON(w, http.StatusOK, api.ReadyStatus{Status: api.ReadyStatusStatusOk, SchemaVersion: v})
}

// Policies returns the per-operation policies of the embedded contract.
func Policies() (middleware.Policies, error) { return middleware.ParsePolicies(apispec.OpenAPI) }

// NewRouter builds the root handler.
func NewRouter(d Deps) (http.Handler, error) {
	policies, err := Policies()
	if err != nil {
		return nil, err
	}
	if d.AdminApp == nil {
		d.AdminApp = static.Load("admin", "/admin/")
	}
	if d.BookingApp == nil {
		d.BookingApp = static.Load("booking", "/b/")
	}
	if d.PublicReadsPerMinute <= 0 {
		d.PublicReadsPerMinute = 120
	}

	r := chi.NewRouter()
	r.Use(
		middleware.RequestID(),
		middleware.RealIP(d.TrustedProxies),
		middleware.AccessLog(d.Log, d.Clock),
		middleware.Recover(d.Log),
		middleware.SecurityHeaders(middleware.SecurityOptions{HSTS: d.HSTS}),
		middleware.BodyLimit(MaxBodyBytes),
	)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			problem.Write(w, r, problem.NotFound)
			return
		}
		http.NotFound(w, r)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.MethodNotAllowed)
	})

	mountStatic(r, d.AdminApp, d.BookingApp)

	// Per-operation middleware, outermost first; the generated wrapper applies the list in reverse.
	perOp := []func(http.Handler) http.Handler{
		middleware.Route(),
		middleware.Authenticate(d.Authenticator),
		middleware.Enforce(policies, middleware.EnforceOptions{TOTPAllowed: totpEnrolmentOperations}),
		middleware.RateLimit(policies, middleware.RateLimitOptions{
			ByIP: map[string]*middleware.Limiter{
				"pairDevice":      middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
				"demoPairDevice":  middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
				"getPublicMarina": middleware.NewLimiter(d.Clock, d.PublicReadsPerMinute, time.Minute),
			},
			APIKey: middleware.NewLimiter(d.Clock, apiKeyPerMinute, time.Minute),
		}),
		middleware.RequireJSON(),
		middleware.Idempotency(middleware.IdempotencyOptions{Store: d.Idempotency, Clock: d.Clock}),
	}
	slices.Reverse(perOp)

	srv := &server{adminAPI{d.Admin}, deviceAPI{d.Device}, publicAPI{d.Public}, d.Ready}
	api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:  r,
		Middlewares: toMiddlewares(perOp),
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			problem.Write(w, r, problem.BadRequest, problem.Detail(err.Error()))
		},
	})
	if d.ExtraRoutes != nil {
		d.ExtraRoutes(r)
	}
	return r, nil
}

func toMiddlewares(in []func(http.Handler) http.Handler) []api.MiddlewareFunc {
	out := make([]api.MiddlewareFunc, 0, len(in))
	for _, m := range in {
		out = append(out, m)
	}
	return out
}

// mountStatic serves the SPAs: /admin/* (client-side routing), /b/{slug} (booking), favicon.
func mountStatic(r chi.Router, adminApp, bookingApp *static.App) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Get("/admin/*", adminApp.Handler().ServeHTTP)
	r.Head("/admin/*", adminApp.Handler().ServeHTTP)
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		adminApp.ServeFile(w, r, "favicon.ico", false)
	})
	booking := func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/b/")
		if strings.HasPrefix(rest, "assets/") || strings.Contains(rest, ".") && !strings.Contains(rest, "/") {
			bookingApp.ServeFile(w, r, rest, false)
			return
		}
		bookingApp.ServeIndex(w, r)
	}
	r.Get("/b/*", booking)
	r.Head("/b/*", booking)
}
