package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/static"
	"github.com/coste360/porto/internal/reqctx"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	h, err := NewRouter(Deps{
		Log: slog.New(slog.DiscardHandler), Clock: clock.System{},
		AdminApp: static.Load("missing-admin", "/admin/"), BookingApp: static.Load("missing-booking", "/b/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Every generated API route has a contract policy, and every policy has a route.
func TestPolicies_CoverEveryRoute(t *testing.T) {
	ps, err := Policies()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = chi.Walk(testRouter(t).(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") && !strings.HasPrefix(route, "/webhooks/") && route != "/healthz" && route != "/readyz" {
			return nil
		}
		if _, ok := ps.Lookup(method, route); !ok {
			t.Errorf("%s %s has no policy", method, route)
		}
		seen[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for k := range ps {
		if !seen[k] {
			t.Errorf("policy %s has no route", k)
		}
	}
	if len(seen) < 50 {
		t.Fatalf("only %d API routes", len(seen))
	}
}

func TestPolicies_SecurityOfKeyOperations(t *testing.T) {
	ps, err := Policies()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		method, route string
		public, csrf  bool
	}{
		{"POST", "/api/admin/v1/auth/login", true, false},
		{"GET", "/api/admin/v1/meta", true, false},
		{"POST", "/api/device/v1/pair", true, false},
		{"GET", "/api/public/v1/{slug}", true, false},
		{"POST", "/api/admin/v1/auth/logout", false, true},
		{"POST", "/api/admin/v1/marinas/{marinaId}/members", false, true},
		{"GET", "/api/admin/v1/marinas/{marinaId}/members", false, false},
		// M6a
		{"POST", "/api/device/v1/pair-session", true, false},
		{"POST", "/api/device/v1/pair-session/totp", true, false},
		{"GET", "/api/desktop/v1/status", true, false},
		{"POST", "/api/desktop/v1/first-run", true, false},
		{"GET", "/api/desktop/v1/settings", false, false},
		{"PUT", "/api/desktop/v1/lan", false, true},
		{"POST", "/api/desktop/v1/secret-key/reveal", false, true},
		{"POST", "/api/desktop/v1/restore-upload", false, true},
		// M8
		{"GET", "/api/public/v1/{slug}/booking/config", true, false},
		{"POST", "/api/public/v1/{slug}/bookings", true, false},
		{"POST", "/api/public/v1/{slug}/account/login", true, false},
		{"POST", "/webhooks/channels/{channel}/{marinaId}", true, false},
		{"POST", "/api/admin/v1/marinas/{marinaId}/online-bookings/{contractId}/approve", false, true},
		{"PUT", "/api/admin/v1/marinas/{marinaId}/online-booking/settings", false, true},
	} {
		p, ok := ps.Lookup(c.method, c.route)
		if !ok || p.Public() != c.public || p.CSRF != c.csrf {
			t.Errorf("%s %s: %+v", c.method, c.route, p)
		}
	}
}

// TestPolicies_DomainIdempotency: createBooking handles Idempotency-Key in the domain (m8-plan §4.3), so the
// middleware never stores a response carrying a booking token; every other operation keeps the middleware.
func TestPolicies_DomainIdempotency(t *testing.T) {
	ps, err := Policies()
	if err != nil {
		t.Fatal(err)
	}
	for key, p := range ps {
		if want := key == "POST /api/public/v1/{slug}/bookings"; p.DomainIdempotency != want {
			t.Errorf("%s: DomainIdempotency %v, want %v", key, p.DomainIdempotency, want)
		}
	}
}

func TestStatic_PlaceholderAndProblems(t *testing.T) {
	h := testRouter(t)
	for _, c := range []struct {
		path string
		code int
		want string
	}{
		{"/admin/", 200, "make build"},
		{"/admin/settings/team", 200, "make build"},
		{"/b/prova", 200, "make build"},
		{"/api/admin/v1/nope", 404, `"not_found"`},
		{"/", 302, ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body)
		}
		if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("%s: missing security headers", c.path)
		}
	}
}

// The desktop operations accept the session cookie only, the activation only the instance token, and the restore
// uploads stream up to 4 GiB.
func TestPolicies_DesktopOperations(t *testing.T) {
	ps, err := Policies()
	if err != nil {
		t.Fatal(err)
	}
	for key, p := range ps {
		if !strings.Contains(key, " /api/desktop/v1/") || p.Public() {
			continue
		}
		if len(p.Methods) != 1 || p.Methods[0] != reqctx.AuthSession {
			t.Errorf("%s accepts %v, want the session only", key, p.Methods)
		}
	}
	if p, _ := ps.Lookup("POST", "/api/desktop/v1/activate"); !p.InstanceToken || !p.Public() {
		t.Errorf("activate: %+v", p)
	}
	streamed := ps.StreamedPaths()
	for _, k := range []string{"POST /api/desktop/v1/restore-upload", "POST /api/desktop/v1/first-run/restore"} {
		if streamed[k] != middleware.MaxStreamCap {
			t.Errorf("%s stream limit %d", k, streamed[k])
		}
	}
	if len(streamed) != 2 {
		t.Errorf("streamed paths %v", streamed)
	}
}

// INV-80 (server side): the desktop API is 404 in the server edition and on any non-loopback listener or peer.
func TestDesktopAPI_LoopbackOnly(t *testing.T) {
	h := testRouter(t)
	for _, c := range []struct {
		name   string
		kind   reqctx.ListenerKind
		remote string
	}{
		{"server listener", reqctx.ListenerServer, "127.0.0.1:5000"},
		{"lan listener, loopback peer", reqctx.ListenerLAN, "127.0.0.1:5000"},
		{"loopback listener, private peer", reqctx.ListenerLoopback, "192.168.1.20:5000"},
		{"loopback listener and peer, server edition (nil runtime)", reqctx.ListenerLoopback, "127.0.0.1:5000"},
	} {
		r := httptest.NewRequest("GET", "/api/desktop/v1/status", nil)
		r.RemoteAddr = c.remote
		r = r.WithContext(reqctx.WithListener(r.Context(), c.kind))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}
