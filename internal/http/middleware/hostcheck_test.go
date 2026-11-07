package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/reqctx"
)

func TestHostCheck_RejectsRebinding(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	loopback := HostCheck([]string{"127.0.0.1:18090", "localhost:18090"})(ok)
	lan := HostCheck([]string{"192.168.1.20:8443", "[fd12:3456::20]:8443", "marina-pc.local:8443"})(ok)
	for _, c := range []struct {
		name string
		h    http.Handler
		host string
		want int
	}{
		{"loopback ip", loopback, "127.0.0.1:18090", http.StatusNoContent},
		{"loopback name", loopback, "localhost:18090", http.StatusNoContent},
		{"rebinding name", loopback, "evil.example:18090", http.StatusMisdirectedRequest},
		{"rebinding to loopback alias", loopback, "127.1:18090", http.StatusMisdirectedRequest},
		{"no host", loopback, "", http.StatusMisdirectedRequest},
		{"other port", loopback, "localhost:80", http.StatusMisdirectedRequest},
		{"lan ip", lan, "192.168.1.20:8443", http.StatusNoContent},
		{"lan ipv6", lan, "[FD12:3456::20]:8443", http.StatusNoContent},
		{"lan mdns name with trailing dot", lan, "MARINA-PC.local.:8443", http.StatusNoContent},
		{"lan without port", lan, "192.168.1.20", http.StatusMisdirectedRequest},
		{"lan rebinding", lan, "attacker.example:8443", http.StatusMisdirectedRequest},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/admin/v1/me", nil)
		r.Host = c.host
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s (Host %q): %d, want %d", c.name, c.host, rec.Code, c.want)
		}
	}
}

func TestRateLimit_PrivatePeersFullAddress(t *testing.T) {
	for addr, want := range map[string]string{
		"192.168.1.20":          "192.168.1.20",
		"10.0.0.7":              "10.0.0.7",
		"172.16.4.4":            "172.16.4.4",
		"127.0.0.1":             "127.0.0.1",
		"::ffff:192.168.1.21":   "192.168.1.21",
		"fd12:3456::20":         "fd12:3456::20",
		"fe80::1%en0":           "fe80::1",
		"::1":                   "::1",
		"203.0.113.9":           "203.0.113.0/24",
		"100.64.1.1":            "100.64.1.0/24",
		"2001:db8:1:2:3::4":     "2001:db8:1::/56",
		"::ffff:198.51.100.200": "198.51.100.0/24",
	} {
		if got := PeerKey(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PeerKey(%s) = %q, want %q", addr, got, want)
		}
	}

	spec := []byte(`
paths:
  /pair:
    post:
      operationId: pair
      security: []
`)
	ps, err := ParsePolicies(spec)
	if err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	h := RateLimit(ps, RateLimitOptions{ByIP: map[string]*Limiter{"pair": NewLimiter(c, 2, time.Minute)}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(ip string) int {
		r := httptest.NewRequest(http.MethodPost, "/pair", nil)
		r = r.WithContext(reqctx.With(r.Context(), &reqctx.Info{ClientIP: netip.MustParseAddr(ip), Route: "/pair"}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	// Two phones on the marina Wi-Fi each get their own budget.
	for _, ip := range []string{"192.168.1.20", "192.168.1.20", "192.168.1.21", "192.168.1.21"} {
		if got := call(ip); got != http.StatusNoContent {
			t.Fatalf("%s: %d", ip, got)
		}
	}
	if got := call("192.168.1.20"); got != http.StatusTooManyRequests {
		t.Fatalf("third call from the same phone: %d", got)
	}
	// Public peers still share the /24.
	call("203.0.113.1")
	call("203.0.113.2")
	if got := call("203.0.113.3"); got != http.StatusTooManyRequests {
		t.Fatalf("public /24 not grouped: %d", got)
	}
}
