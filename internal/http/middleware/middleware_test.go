package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/reqctx"
)

const testSpec = `
openapi: 3.1.0
paths:
  /api/admin/v1/things:
    get:
      operationId: listThings
      security: &read
        - cookieAuth: []
        - apiKeyAuth: []
    post:
      operationId: createThing
      security:
        - cookieAuth: []
          csrfToken: []
        - apiKeyAuth: []
  /api/admin/v1/auth/login:
    post:
      operationId: login
      security: []
  /api/admin/v1/auth/me:
    get:
      operationId: getMe
      security:
        - cookieAuth: []
`

type fakeAuth struct{}

func (fakeAuth) AuthenticateSession(_ context.Context, token string) (*auth.Principal, error) {
	switch token {
	case "good":
		return &auth.Principal{Kind: auth.KindUser, UserID: "u1"}, nil
	case "enrol":
		return &auth.Principal{Kind: auth.KindUser, UserID: "u2", TOTPEnrolmentRequired: true}, nil
	}
	return nil, auth.ErrUnauthenticated
}

func (fakeAuth) AuthenticateAPIKey(_ context.Context, key string) (*auth.Principal, error) {
	if key == "po_live_abcdefgh_secret" {
		return &auth.Principal{Kind: auth.KindAPIKey, APIKeyID: "k1"}, nil
	}
	return nil, auth.ErrUnauthenticated
}

func (fakeAuth) AuthenticateDevice(context.Context, string) (*auth.Principal, error) {
	return nil, auth.ErrUnauthenticated
}

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	ps, err := ParsePolicies([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(RequestID())
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	r.Group(func(r chi.Router) {
		r.Use(Route(), Authenticate(fakeAuth{}), Enforce(ps, EnforceOptions{TOTPAllowed: []string{"getMe"}}))
		r.Get("/api/admin/v1/things", ok)
		r.Post("/api/admin/v1/things", ok)
		r.Post("/api/admin/v1/auth/login", ok)
		r.Get("/api/admin/v1/auth/me", ok)
	})
	return r
}

func do(h http.Handler, method, path string, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookies(session, csrf string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: session})
		if csrf != "" {
			r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: csrf})
		}
	}
}

func TestParsePolicies_Contract(t *testing.T) {
	ps, err := ParsePolicies([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ps.Lookup("POST", "/api/admin/v1/things"); !p.CSRF || !p.Accepts(reqctx.AuthAPIKey) || p.Accepts(reqctx.AuthDevice) {
		t.Fatalf("createThing policy = %+v", p)
	}
	if p, _ := ps.Lookup("POST", "/api/admin/v1/auth/login"); !p.Public() {
		t.Fatalf("login must be public: %+v", p)
	}
	if _, err := ParsePolicies([]byte("paths:\n  /x:\n    get:\n      security:\n        - bogus: []\n")); err == nil {
		t.Fatal("unknown scheme accepted")
	}
}

func TestCSRF_RequiredForCookieWrites(t *testing.T) {
	h := testRouter(t)
	cases := []struct {
		name   string
		method string
		mod    func(*http.Request)
		want   int
	}{
		{"cookie write without header", "POST", cookies("good", "tok"), http.StatusForbidden},
		{"cookie write without cookie", "POST", func(r *http.Request) { cookies("good", "")(r); r.Header.Set(CSRFHeader, "tok") }, http.StatusForbidden},
		{"cookie write with wrong header", "POST", func(r *http.Request) { cookies("good", "tok")(r); r.Header.Set(CSRFHeader, "nope") }, http.StatusForbidden},
		{"cookie write with matching header", "POST", func(r *http.Request) { cookies("good", "tok")(r); r.Header.Set(CSRFHeader, "tok") }, http.StatusNoContent},
		{"cookie read needs no header", "GET", cookies("good", ""), http.StatusNoContent},
		{"bearer write is exempt", "POST", func(r *http.Request) { r.Header.Set("Authorization", "Bearer po_live_abcdefgh_secret") }, http.StatusNoContent},
		{"anonymous write", "POST", nil, http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(h, c.method, "/api/admin/v1/things", c.mod)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
			if c.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), `"csrf_failed"`) {
				t.Fatalf("problem type: %s", rec.Body)
			}
		})
	}
}

func TestEnforce_MethodsAndTOTPGate(t *testing.T) {
	h := testRouter(t)
	if rec := do(h, "POST", "/api/admin/v1/auth/login", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("public login: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/admin/v1/auth/me", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer po_live_abcdefgh_secret")
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api key on cookie-only op: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/admin/v1/things", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer po_live_wrong")
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/admin/v1/things", cookies("enrol", "")); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "totp_enrolment_required") {
		t.Fatalf("enrolment gate: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/admin/v1/auth/me", cookies("enrol", "")); rec.Code != http.StatusNoContent {
		t.Fatalf("enrolment allowed op: %d", rec.Code)
	}
}

func TestLimiter_Window(t *testing.T) {
	c := clock.NewFake(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	l := NewLimiter(c, 2, time.Minute)
	for i := range 2 {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("event %d refused", i)
		}
	}
	ok, wait := l.Allow("k")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("third event: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("keys must be independent")
	}
	c.Advance(time.Minute)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("window did not reset")
	}
}

func TestRequestID_EchoesValidAndReplacesInvalid(t *testing.T) {
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reqctx.From(r.Context()).RequestID))
	}))
	rec := do(h, "GET", "/", func(r *http.Request) { r.Header.Set(RequestIDHeader, "client-id-123") })
	if rec.Body.String() != "client-id-123" {
		t.Fatalf("got %q", rec.Body)
	}
	rec = do(h, "GET", "/", func(r *http.Request) { r.Header.Set(RequestIDHeader, "bad id\n") })
	if got := rec.Body.String(); got == "" || strings.Contains(got, "bad") {
		t.Fatalf("got %q", got)
	}
}
