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
	"github.com/coste360/porto/internal/http/bridge"
	"github.com/coste360/porto/internal/http/desktop"
	"github.com/coste360/porto/internal/http/device"
	"github.com/coste360/porto/internal/http/ingest"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/public"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/static"
	"github.com/coste360/porto/internal/http/webhooks"
	"github.com/coste360/porto/internal/reqctx"
)

// MaxBodyBytes is the hard cap of every request body (m1-plan §4.1); each operation has its own, smaller or
// equal limit from the contract (x-porto-max-body-bytes, default 1 MiB), applied per operation.
const MaxBodyBytes = middleware.MaxBodyCap

// Fixed limits (m0-plan §5).
const (
	pairingPerMinute = 10
	apiKeyPerMinute  = 600
)

// totpEnrolmentOperations are what a session that must enrol TOTP may still call.
var totpEnrolmentOperations = []string{"getMe", "logout", "enrollTotp", "confirmTotp"}

// linkRequestsPerMinute is the rate limit of the customer link pages per IP prefix (m4-plan §6.6).
const linkRequestsPerMinute = 30

// publicPayOperations are the operations of the public pay page /api/public/v1/{slug}/pay/{payToken} (M5,
// m5-plan §5.5): 30 requests per minute per IP prefix, like the customer links.
var publicPayOperations = []string{"getPayPage", "startPayCheckout", "getPayStatus"}

// paymentWebhooksPerMinute is the rate limit of the payment provider callbacks per IP prefix (m5-plan §5.4).
const paymentWebhooksPerMinute = 600

// publicLinkOperations are the operations of /api/public/v1/{slug}/links/{token} (m4-plan §7.3.6).
var publicLinkOperations = []string{
	"resolvePublicLink", "getPublicLinkDocument", "acceptPublicQuote", "uploadPublicDocument", "acceptPublicRenewal",
	"declinePublicRenewal", "requestPublicSignatureOtp", "verifyPublicSignature", "declinePublicSignature",
	"publicUnsubscribe", "acceptPublicYardQuote",
}

// bookingRateLimits are the per-IP-prefix limits of the M8 public booking, customer-area and channel operations
// (m8-plan §10): requests per window. Operations listed together share one bucket.
var bookingRateLimits = []struct {
	ops    []string
	n      int
	window time.Duration
}{
	{[]string{"getBookingConfig"}, 120, time.Minute},
	{[]string{"getBookingAvailability"}, 60, time.Minute},
	{[]string{"quoteBooking"}, 60, time.Minute},
	{[]string{"createBooking"}, 10, time.Minute},
	{[]string{"getBooking", "abandonBooking", "submitBookingDetails", "uploadBookingDocument", "startBookingPayment"}, 20, time.Minute},
	{[]string{"getBookingStatus"}, 60, time.Minute},
	{[]string{"requestAccountLink"}, 5, 15 * time.Minute},
	{[]string{"openAccountSession", "closeAccountSession"}, 10, time.Minute},
	{[]string{"listAccountVessels", "listAccountBookings", "getAccountBooking", "getAccountDocumentURL", "getAccountDocumentFile", "previewAccountCancellation"}, 60, time.Minute},
	{[]string{"uploadAccountDocument", "payAccountBalance", "cancelAccountBooking", "previewAccountDateChange", "changeAccountDates"}, 20, time.Minute},
	{[]string{"receiveChannelWebhook"}, 600, time.Minute},
}

// m12RateLimits are the per-IP-prefix limits of the M12 ingestion (before authentication; the per-key limits are
// in the sensor service) and of the public profile operations (m12-plan §4.6, §12.2).
var m12RateLimits = []struct {
	op string
	n  int
}{
	{"ingestSensorReadings", 3000},
	{"getPublicProfile", 120},
	{"getPublicProfileMap", 60},
	{"getPublicProfilePrices", 60},
	{"getPublicOccupancy", 120},
	{"getPublicProfilePhoto", 300},
}

// profilePagePerMinute is the per-IP-prefix limit of the /p/{slug} page (the getPublicProfile limit).
const profilePagePerMinute = 120

// Deps are the collaborators of the router.
type Deps struct {
	Log   *slog.Logger
	Clock clock.Clock

	Admin  *admin.Handler
	Device *device.Handler
	Public *public.Handler
	// Webhooks serves the inbound provider callbacks (M4); nil means an empty handler.
	Webhooks *webhooks.Handler
	// Desktop serves /api/desktop/v1 (M6a); nil or a nil Runtime (server edition) answers 404 everywhere.
	Desktop *desktop.Handler
	// Bridge serves /api/bridge/v1 (M6b, local peripherals); nil or a nil Runtime answers 404 everywhere.
	Bridge *bridge.Handler
	// Ingest serves /api/ingest/v1 (M12, sensor readings); nil answers 404.
	Ingest *ingest.Handler
	// ProfilePage serves /p/{slug} (M12, public marina profile page, framed like /b/{slug}); nil answers 404. Its
	// Index defaults to the booking SPA index.html.
	ProfilePage *public.ProfilePage

	Authenticator middleware.Authenticator
	Idempotency   middleware.IdempotencyStore
	// Ready reports the applied schema version, or an error when the instance cannot serve (readyz).
	Ready func(ctx context.Context) (int64, error)

	TrustedProxies       []netip.Prefix
	HSTS                 bool
	PublicReadsPerMinute int

	// EmbedOrigins resolves the allowed embed origins of a booking slug for the framing headers of /b/{slug} (M8,
	// m8-plan §10); nil = no marina allows embedding.
	EmbedOrigins middleware.EmbedOriginsResolver

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
	webhooksAPI
	desktopAPI
	bridgeAPI
	ingestAPI
	ready func(ctx context.Context) (int64, error)
}

type (
	adminAPI    struct{ *admin.Handler }
	deviceAPI   struct{ *device.Handler }
	publicAPI   struct{ *public.Handler }
	webhooksAPI struct{ *webhooks.Handler }
	desktopAPI  struct{ *desktop.Handler }
	bridgeAPI   struct{ *bridge.Handler }
	ingestAPI   struct{ *ingest.Handler }
)

// DesktopPrefix is the path prefix of the desktop API, served only on the desktop loopback listener (INV-80).
const DesktopPrefix = "/api/desktop/v1/"

// BridgePrefix is the path prefix of the bridge API (M6b), loopback only like the desktop API (INV-85).
const BridgePrefix = bridge.Prefix

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
		middleware.BodyLimitExcept(MaxBodyBytes, policies.StreamedPaths()),
		// M6a: the desktop API exists only on the loopback listener from a loopback peer (404 elsewhere).
		middleware.LoopbackOnly(DesktopPrefix),
		// M6b: the bridge to local peripherals, same rule (INV-85).
		middleware.LoopbackOnly(BridgePrefix),
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

	embedFraming := middleware.EmbedFraming(d.EmbedOrigins)
	mountStatic(r, d.AdminApp, d.BookingApp, embedFraming)
	mountProfilePage(r, d.ProfilePage, d.BookingApp, embedFraming, middleware.NewLimiter(d.Clock, profilePagePerMinute, time.Minute))

	// Customer magic-link pages (M4, m4-plan §6.6): one limiter per IP prefix shared by every link operation. The
	// per-token limit is applied by the link services.
	byIP := map[string]*middleware.Limiter{
		"pairDevice":     middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
		"demoPairDevice": middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
		// M6a browser pairing (m6a-plan §3.3): the pairing limit, its own bucket.
		"pairDeviceSession": middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
		// Second factor of a device session: per peer, its own bucket (the per-user budget is in auth).
		"verifyDeviceSessionTotp": middleware.NewLimiter(d.Clock, pairingPerMinute, time.Minute),
		"getPublicMarina":         middleware.NewLimiter(d.Clock, d.PublicReadsPerMinute, time.Minute),
	}
	links := middleware.NewLimiter(d.Clock, linkRequestsPerMinute, time.Minute)
	for _, op := range publicLinkOperations {
		byIP[op] = links
	}
	pay := middleware.NewLimiter(d.Clock, linkRequestsPerMinute, time.Minute)
	for _, op := range publicPayOperations {
		byIP[op] = pay
	}
	byIP["paymentWebhook"] = middleware.NewLimiter(d.Clock, paymentWebhooksPerMinute, time.Minute)
	for _, rl := range bookingRateLimits {
		l := middleware.NewLimiter(d.Clock, rl.n, rl.window)
		for _, op := range rl.ops {
			byIP[op] = l
		}
	}
	for _, rl := range m12RateLimits {
		byIP[rl.op] = middleware.NewLimiter(d.Clock, rl.n, time.Minute)
	}

	// Per-operation middleware, outermost first; the generated wrapper applies the list in reverse.
	perOp := []func(http.Handler) http.Handler{
		middleware.Route(),
		middleware.Authenticate(d.Authenticator),
		middleware.BodyLimitByPolicy(policies),
		middleware.Enforce(policies, middleware.EnforceOptions{TOTPAllowed: totpEnrolmentOperations}),
		middleware.RateLimit(policies, middleware.RateLimitOptions{
			ByIP:   byIP,
			APIKey: middleware.NewLimiter(d.Clock, apiKeyPerMinute, time.Minute),
		}),
		middleware.RequireJSON(policies),
		// M6b: replays of the offline queue only on x-porto-offline operations (INV-71).
		middleware.OfflineGuard(policies, d.Clock),
		middleware.Idempotency(middleware.IdempotencyOptions{Store: d.Idempotency, Clock: d.Clock, Policies: policies}),
	}
	slices.Reverse(perOp)

	if d.Webhooks == nil {
		d.Webhooks = &webhooks.Handler{}
	}
	if d.Desktop == nil {
		d.Desktop = &desktop.Handler{}
	}
	if d.Bridge == nil {
		d.Bridge = &bridge.Handler{}
	}
	if d.Ingest == nil {
		d.Ingest = &ingest.Handler{}
	}
	srv := &server{
		adminAPI{d.Admin},
		deviceAPI{d.Device},
		publicAPI{d.Public},
		webhooksAPI{d.Webhooks},
		desktopAPI{d.Desktop},
		bridgeAPI{d.Bridge},
		ingestAPI{d.Ingest},
		d.Ready,
	}
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

// mountStatic serves the SPAs: /admin/* (client-side routing), /b/{slug} (booking, behind embedFraming), favicon
// and the widget loader /embed.js (M8; its caching and CORS headers are set by PUBLIC).
func mountStatic(r chi.Router, adminApp, bookingApp *static.App, embedFraming func(http.Handler) http.Handler) {
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
	framed := embedFraming(http.HandlerFunc(booking))
	r.Get("/b/*", framed.ServeHTTP)
	r.Head("/b/*", framed.ServeHTTP)
	embedJS := func(w http.ResponseWriter, r *http.Request) { bookingApp.ServeFile(w, r, "embed.js", false) }
	r.Get("/embed.js", embedJS)
	r.Head("/embed.js", embedJS)
}

// mountProfilePage serves /p/{slug} (M12, m12-plan §12.3) behind the booking framing headers: the profile page
// handler injects its head into the booking SPA index. Without a handler the path is 404 noindex.
// The page is limited per IP prefix like getPublicProfile: each request resolves the slug in the database.
func mountProfilePage(r chi.Router, page *public.ProfilePage, bookingApp *static.App, embedFraming func(http.Handler) http.Handler, limit *middleware.Limiter) {
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex")
		http.NotFound(w, r)
	})
	if page != nil {
		if page.Index == nil {
			page.Index = bookingApp.Index
		}
		h = page
	}
	framed := embedFraming(h)
	limited := func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := limit.Allow("profilePage|" + middleware.PeerKey(reqctx.From(r.Context()).ClientIP)); !ok {
			problem.Write(w, r, problem.RateLimited, problem.RetryAfter(wait))
			return
		}
		framed.ServeHTTP(w, r)
	}
	r.Get("/p/*", limited)
	r.Head("/p/*", limited)
}
