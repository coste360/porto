package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/reqctx"
)

func TestLoopbackOnly_ListenerAndPeer(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := LoopbackOnly("/api/desktop/v1/")(ok)
	for _, c := range []struct {
		path, remote string
		kind         reqctx.ListenerKind
		want         int
	}{
		{"/api/desktop/v1/status", "127.0.0.1:1234", reqctx.ListenerLoopback, http.StatusNoContent},
		{"/api/desktop/v1/status", "[::1]:1234", reqctx.ListenerLoopback, http.StatusNoContent},
		{"/api/desktop/v1/status", "[::ffff:127.0.0.1]:1234", reqctx.ListenerLoopback, http.StatusNoContent},
		{"/api/desktop/v1/status", "192.168.1.5:1234", reqctx.ListenerLoopback, http.StatusNotFound},
		{"/api/desktop/v1/status", "127.0.0.1:1234", reqctx.ListenerLAN, http.StatusNotFound},
		{"/api/desktop/v1/status", "127.0.0.1:1234", reqctx.ListenerServer, http.StatusNotFound},
		{"/api/desktop/v1/status", "garbage", reqctx.ListenerLoopback, http.StatusNotFound},
		{"/api/admin/v1/meta", "192.168.1.5:1234", reqctx.ListenerLAN, http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.RemoteAddr = c.remote
		r = r.WithContext(reqctx.WithListener(r.Context(), c.kind))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s from %s on %s: %d, want %d", c.path, c.remote, c.kind, rec.Code, c.want)
		}
	}
}

func TestHostCheck_RejectsUnknownHosts(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := HostCheck([]string{"127.0.0.1:18090", "localhost:18090"})(ok)
	for host, want := range map[string]int{
		"127.0.0.1:18090":    http.StatusNoContent,
		"LOCALHOST:18090":    http.StatusNoContent,
		"evil.example:18090": http.StatusMisdirectedRequest,
		"127.0.0.1:18091":    http.StatusMisdirectedRequest,
		"127.0.0.1":          http.StatusMisdirectedRequest,
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/desktop/v1/status", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("Host %q: %d, want %d", host, rec.Code, want)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "anything"
	rec := httptest.NewRecorder()
	HostCheck(nil)(ok).ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("empty allow-list must not check: %d", rec.Code)
	}
}

func TestParsePolicies_StreamAndInstanceToken(t *testing.T) {
	spec := []byte(`
paths:
  /up:
    post:
      operationId: up
      security: [{cookieAuth: [], csrfToken: []}]
      x-porto-max-stream-bytes: 4294967296
      requestBody: {content: {application/gzip: {}}}
  /act:
    post:
      operationId: act
      security: [{instanceToken: []}]
`)
	ps, err := ParsePolicies(spec)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ps.Lookup("POST", "/up"); p.MaxStreamBytes != MaxStreamCap || !p.CSRF {
		t.Fatalf("up: %+v", p)
	}
	if p, _ := ps.Lookup("POST", "/act"); !p.InstanceToken || !p.Public() {
		t.Fatalf("act: %+v", p)
	}
	for _, bad := range []string{
		`{paths: {/x: {post: {operationId: x, x-porto-max-stream-bytes: 10, requestBody: {content: {application/json: {}}}}}}}`,
		`{paths: {/x/{id}: {post: {operationId: x, x-porto-max-stream-bytes: 10, requestBody: {content: {application/gzip: {}}}}}}}`,
		`{paths: {/x: {post: {operationId: x, x-porto-max-stream-bytes: 8589934592, requestBody: {content: {application/gzip: {}}}}}}}`,
		`{paths: {/x: {post: {operationId: x, security: [{instanceToken: [], cookieAuth: []}]}}}}`,
	} {
		if _, err := ParsePolicies([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestBodyLimitExcept_StreamedPathSkipsCap(t *testing.T) {
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := BodyLimitExcept(8, map[string]int64{"POST /up": 64})(read)
	for _, c := range []struct {
		path string
		size int
		want int
	}{
		{"/up", 32, http.StatusNoContent},
		{"/up", 100, http.StatusRequestEntityTooLarge},
		{"/other", 32, http.StatusRequestEntityTooLarge},
		{"/other", 4, http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(strings.Repeat("a", c.size)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s with %d bytes: %d, want %d", c.path, c.size, rec.Code, c.want)
		}
	}
}
