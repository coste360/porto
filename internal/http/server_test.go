package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/http/static"
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
	} {
		p, ok := ps.Lookup(c.method, c.route)
		if !ok || p.Public() != c.public || p.CSRF != c.csrf {
			t.Errorf("%s %s: %+v", c.method, c.route, p)
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
