package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbed_OriginParsing(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"https://marina.example", "https://marina.example", true},
		{" HTTPS://Marina.Example/ ", "https://marina.example", true},
		{"http://localhost:18081", "http://localhost:18081", true},
		{"https://a.b-c.example:8443", "https://a.b-c.example:8443", true},
		{"https://marina.example/path", "", false},
		{"https://marina.example?x=1", "", false},
		{"ftp://marina.example", "", false},
		{"https://*.example", "", false},
		{"*", "", false},
		{"https://a.example https://b.example", "", false},
		{"https://a.example;script-src *", "", false},
		{"https://a.example,https://b.example", "", false},
		{"'self'", "", false},
		{"https://", "", false},
		{"https://-bad.example", "", false},
		{"https://user@a.example", "", false},
		{"", "", false},
	} {
		got, ok := ParseEmbedOrigin(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseEmbedOrigin(%q) = %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestEmbed_BookingPageSlug(t *testing.T) {
	for _, c := range []struct {
		path, slug string
		ok         bool
	}{
		{"/b/porto-a", "porto-a", true},
		{"/b/porto-a/", "porto-a", true},
		{"/b/porto-a/r/0190a1b2-c3d4", "porto-a", true},
		{"/b/porto-a/account", "porto-a", true},
		{"/b/assets/index-abc.js", "", false},
		{"/b/embed.js", "", false},
		{"/b/sw.js", "", false},
		{"/b/manifest.webmanifest", "", false},
		{"/b/", "", false},
		{"/admin/", "", false},
		{"/api/public/v1/porto-a", "", false},
	} {
		slug, ok := bookingPageSlug(c.path)
		if slug != c.slug || ok != c.ok {
			t.Errorf("bookingPageSlug(%q) = %q %v", c.path, slug, ok)
		}
	}
}

// TestEmbedFraming_Headers runs the global SecurityHeaders and, on the booking routes only, EmbedFraming, as the
// router does: allowed origins relax frame-ancestors, a marina without origins or an unknown slug allows only
// 'self', and every other response (API, admin, assets) keeps frame-ancestors 'none' and DENY.
func TestEmbedFraming_Headers(t *testing.T) {
	resolve := func(_ context.Context, slug string) ([]string, bool) {
		switch slug {
		case "porto-a":
			return []string{"https://marina-a.example", "http://localhost:18081", "https://bad.example; script-src *"}, true
		case "porto-b":
			return nil, true
		}
		return nil, false
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	sec := SecurityHeaders(SecurityOptions{})
	booking := sec(EmbedFraming(resolve)(ok))
	other := sec(ok)

	serve := func(h http.Handler, path string) http.Header {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Header()
	}
	ancestors := func(h http.Header) string {
		for _, d := range strings.Split(h.Get("Content-Security-Policy"), ";") {
			if d = strings.TrimSpace(d); strings.HasPrefix(d, "frame-ancestors") {
				return d
			}
		}
		return ""
	}

	t.Run("allowed origins", func(t *testing.T) {
		for _, p := range []string{"/b/porto-a", "/b/porto-a/r/0190a1b2-c3d4-7e8f"} {
			h := serve(booking, p)
			if got := ancestors(h); got != "frame-ancestors 'self' https://marina-a.example http://localhost:18081" {
				t.Fatalf("%s frame-ancestors %q", p, got)
			}
			if h.Get("X-Frame-Options") != "" {
				t.Fatalf("%s X-Frame-Options %q", p, h.Get("X-Frame-Options"))
			}
			csp := h.Get("Content-Security-Policy")
			if strings.Count(csp, "frame-ancestors") != 1 || !strings.Contains(csp, "default-src 'self'") ||
				!strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "script-src *") {
				t.Fatalf("%s csp %q", p, csp)
			}
		}
	})
	t.Run("no origins", func(t *testing.T) {
		h := serve(booking, "/b/porto-b")
		if ancestors(h) != "frame-ancestors 'self'" || h.Get("X-Frame-Options") != "SAMEORIGIN" {
			t.Fatalf("headers %v", h)
		}
	})
	t.Run("unknown slug", func(t *testing.T) {
		h := serve(booking, "/b/nope-nope")
		if ancestors(h) != "frame-ancestors 'self'" || h.Get("X-Frame-Options") != "SAMEORIGIN" {
			t.Fatalf("headers %v", h)
		}
	})
	t.Run("nil resolver", func(t *testing.T) {
		h := serve(sec(EmbedFraming(nil)(ok)), "/b/porto-a")
		if ancestors(h) != "frame-ancestors 'self'" || h.Get("X-Frame-Options") != "SAMEORIGIN" {
			t.Fatalf("headers %v", h)
		}
	})
	t.Run("assets keep deny", func(t *testing.T) {
		h := serve(booking, "/b/assets/index-abc.js")
		if ancestors(h) != "frame-ancestors 'none'" || h.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("headers %v", h)
		}
	})
	t.Run("api stays deny", func(t *testing.T) {
		for _, p := range []string{"/api/public/v1/porto-a", "/api/public/v1/porto-a/booking/config", "/admin/"} {
			h := serve(other, p)
			if ancestors(h) != "frame-ancestors 'none'" || h.Get("X-Frame-Options") != "DENY" {
				t.Fatalf("%s headers %v", p, h)
			}
		}
	})
}

func TestEmbed_WithDirective(t *testing.T) {
	if got := withDirective("default-src 'self'; frame-ancestors 'none'", "frame-ancestors", "frame-ancestors 'self'"); got != "default-src 'self'; frame-ancestors 'self'" {
		t.Fatalf("replace: %q", got)
	}
	if got := withDirective("default-src 'self'", "frame-ancestors", "frame-ancestors 'self'"); got != "default-src 'self'; frame-ancestors 'self'" {
		t.Fatalf("append: %q", got)
	}
	if got := withDirective("", "frame-ancestors", "frame-ancestors 'self'"); got != "frame-ancestors 'self'" {
		t.Fatalf("empty: %q", got)
	}
}
