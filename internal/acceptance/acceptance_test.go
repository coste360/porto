//go:build integration

// Package acceptance drives the M0 API end to end through the real router (spec 32.3), on SQLite and, when
// PORTO_TEST_POSTGRES_URL is set, on PostgreSQL.
package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/httpsink"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

const password = "acceptance-password-1"

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	t    *testing.T
	app  *app.App
	srv  *httptest.Server
	logs *syncBuffer
	mail *fixture.Mailbox
}

func newHarness(t *testing.T, db *store.DB) *harness {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY":             base64.StdEncoding.EncodeToString(key),
		"PORTO_BASE_URL":               "http://localhost:8080",
		"PORTO_DATA_DIR":               t.TempDir(),
		"PORTO_DEV":                    "true",
		"PORTO_WEBHOOKS_ALLOW_PRIVATE": "true",
		"PORTO_LOG_LEVEL":              "debug",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mail := &fixture.Mailbox{}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, log, app.Options{
		DB: db, Argon2: auth.TestArgon2Params, Transport: mail,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	return &harness{t: t, app: a, srv: srv, logs: logs, mail: mail}
}

// client is an API client: cookie session (with CSRF) or bearer token.
type client struct {
	h      *harness
	http   *http.Client
	bearer string
}

func (h *harness) cookieClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

func (h *harness) bearerClient(token string) *client {
	return &client{h: h, http: &http.Client{Timeout: 10 * time.Second}, bearer: token}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode %d %s: %v", r.status, r.body, err)
	}
	return m
}

func (r response) problemType() string {
	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r.body, &p)
	return p.Type
}

func (c *client) csrf() string {
	u, _ := url.Parse(c.h.srv.URL)
	if c.http.Jar == nil {
		return ""
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "porto_csrf" {
			return ck.Value
		}
	}
	return ""
}

func (c *client) do(t *testing.T, method, path string, body any, headers ...string) response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.h.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if tok := c.csrf(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: b}
}

func (c *client) expect(t *testing.T, want int, method, path string, body any, headers ...string) map[string]any {
	t.Helper()
	r := c.do(t, method, path, body, headers...)
	if r.status != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.status, want, r.body)
	}
	if len(r.body) == 0 || r.body[0] != '{' {
		return nil
	}
	return r.json(t)
}

func (c *client) login(t *testing.T, email string) map[string]any {
	t.Helper()
	return c.expect(t, http.StatusOK, "POST", "/api/admin/v1/auth/login", map[string]string{"email": email, "password": password})
}

func (h *harness) runJobs(names ...string) {
	h.t.Helper()
	for _, n := range names {
		if err := h.app.Jobs.RunNow(context.Background(), n); err != nil {
			h.t.Fatalf("job %s: %v", n, err)
		}
	}
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

const (
	A = "/api/admin/v1"
)

func TestAcceptance_M0(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		owner, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Prova SRL",
			MarinaName: "Marina Alpha", MarinaSlug: "alpha",
		})
		if err != nil {
			t.Fatal(err)
		}
		alpha := owner.MarinaID
		M := A + "/marinas/" + alpha

		// Health and meta.
		pub := h.bearerClient("")
		pub.expect(t, 200, "GET", "/healthz", nil)
		if r := pub.expect(t, 200, "GET", "/readyz", nil); r["schema_version"].(float64) < 1 {
			t.Fatalf("readyz %v", r)
		}
		pub.expect(t, 200, "GET", A+"/meta", nil)
		if r := pub.expect(t, 200, "GET", "/api/public/v1/alpha", nil); str(r, "name") != "Marina Alpha" {
			t.Fatalf("public %v", r)
		}
		pub.expect(t, 401, "GET", A+"/auth/me", nil)

		// Login and me.
		oc := h.cookieClient()
		if r := oc.do(t, "POST", A+"/auth/login", map[string]string{"email": "owner@porto.test", "password": "wrong-password-x"}); r.status != 401 || r.problemType() != "invalid_credentials" {
			t.Fatalf("bad login: %d %s", r.status, r.body)
		}
		oc.login(t, "owner@porto.test")
		me := oc.expect(t, 200, "GET", A+"/auth/me", nil)
		marinas := me["marinas"].([]any)
		if len(marinas) != 1 || str(marinas[0].(map[string]any), "slug") != "alpha" {
			t.Fatalf("me.marinas = %v", marinas)
		}
		// Cookie write without CSRF header fails.
		req, _ := http.NewRequest("POST", h.srv.URL+A+"/auth/logout", nil)
		res, err := oc.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("logout without CSRF: %d", res.StatusCode)
		}

		// SSE stream of alpha, opened before the writes.
		sse := openStream(t, h, oc, alpha)

		// A second marina, with an idempotent replay.
		orgs := oc.expect(t, 200, "GET", A+"/organizations", nil)["items"].([]any)
		orgID := str(orgs[0].(map[string]any), "id")
		body := map[string]string{"name": "Marina Beta", "slug": "beta"}
		r1 := oc.do(t, "POST", A+"/organizations/"+orgID+"/marinas", body, "Idempotency-Key", "create-beta-0001")
		r2 := oc.do(t, "POST", A+"/organizations/"+orgID+"/marinas", body, "Idempotency-Key", "create-beta-0001")
		if r1.status != 201 || r2.status != 201 || r2.header.Get("Idempotent-Replayed") != "true" || !bytes.Equal(r1.body, r2.body) {
			t.Fatalf("idempotent create: %d %d %q", r1.status, r2.status, r2.header.Get("Idempotent-Replayed"))
		}
		if r3 := oc.do(t, "POST", A+"/organizations/"+orgID+"/marinas", map[string]string{"name": "X", "slug": "xxx"}, "Idempotency-Key", "create-beta-0001"); r3.status != 422 || r3.problemType() != "idempotency_key_reused" {
			t.Fatalf("reused key: %d %s", r3.status, r3.body)
		}
		if r := oc.do(t, "POST", A+"/organizations/"+orgID+"/marinas", map[string]string{"name": "Dup", "slug": "beta"}); r.status != 409 || r.problemType() != "slug_taken" {
			t.Fatalf("slug taken: %d %s", r.status, r.body)
		}

		// PATCH needs If-Match.
		if r := oc.do(t, "PATCH", M, map[string]any{"name": "Marina Alpha 2"}); r.status != 428 {
			t.Fatalf("patch without If-Match: %d", r.status)
		}
		if r := oc.do(t, "PATCH", M, map[string]any{"name": "Marina Alpha 2"}, "If-Match", `"9"`); r.status != 412 || r.problemType() != "version_conflict" {
			t.Fatalf("patch stale: %d %s", r.status, r.body)
		}
		upd := oc.do(t, "PATCH", M, map[string]any{"name": "Marina Alpha 2", "geo": map[string]int64{"lat_e7": 409000000, "lon_e7": 140000000}}, "If-Match", `"1"`)
		if upd.status != 200 || upd.header.Get("ETag") != `"2"` {
			t.Fatalf("patch: %d %s", upd.status, upd.body)
		}
		cleared := oc.do(t, "PATCH", M, map[string]any{"geo": nil}, "If-Match", `"2"`).json(t)
		if cleared["geo"] != nil {
			t.Fatalf("geo not cleared: %v", cleared["geo"])
		}

		// Webhook endpoint on a local receiver.
		sink := httpsink.New(t)
		wh := oc.expect(t, 201, "POST", M+"/webhook-endpoints", map[string]any{"url": sink.URL + "/hook", "events": []string{"member.added"}})
		secret := str(wh, "secret")

		// Add a member; the member logs in but cannot manage users.
		mem := oc.expect(t, 201, "POST", M+"/members", map[string]any{"email": "office@porto.test", "name": "Office", "role": "office"})
		if err := h.app.Auth.SetPassword(ctx, str(mem, "user_id"), password); err != nil {
			t.Fatal(err)
		}
		mc := h.cookieClient()
		mc.login(t, "office@porto.test")
		mc.expect(t, 200, "GET", M, nil)
		if r := mc.do(t, "GET", M+"/members", nil); r.status != 403 || r.problemType() != "forbidden" {
			t.Fatalf("office lists members: %d %s", r.status, r.body)
		}
		if r := mc.do(t, "POST", M+"/api-keys", map[string]any{"name": "x", "scopes": []string{"marina.read"}}); r.status != 403 {
			t.Fatalf("office creates key: %d", r.status)
		}

		// API key: reads the marina, cannot manage members, cannot carry users.manage.
		if r := oc.do(t, "POST", M+"/api-keys", map[string]any{"name": "bad", "scopes": []string{"users.manage"}}); r.status != 400 || r.problemType() != "invalid_scope" {
			t.Fatalf("users.manage key: %d %s", r.status, r.body)
		}
		key := oc.expect(t, 201, "POST", M+"/api-keys", map[string]any{"name": "integration", "scopes": []string{"marina.read"}})
		kc := h.bearerClient(str(key, "secret"))
		kc.expect(t, 200, "GET", M, nil)
		if r := kc.do(t, "GET", M+"/members", nil); r.status != 403 {
			t.Fatalf("api key lists members: %d", r.status)
		}
		if r := kc.do(t, "GET", A+"/auth/me", nil); r.status != 401 {
			t.Fatalf("api key on account op: %d", r.status)
		}

		// Device pairing.
		pc := oc.expect(t, 201, "POST", M+"/devices/pairing-codes", map[string]any{}, "Idempotency-Key", "pairing-code-0001")
		pair := pub.expect(t, 201, "POST", "/api/device/v1/pair", map[string]any{"code": str(pc, "code"), "name": "Tablet", "platform": "android", "kind": "mobile"})
		if r := pub.do(t, "POST", "/api/device/v1/pair", map[string]any{"code": str(pc, "code"), "name": "Tablet", "platform": "android", "kind": "mobile"}); r.status != 400 || r.problemType() != "pairing_code_invalid" {
			t.Fatalf("code reused: %d %s", r.status, r.body)
		}
		dc := h.bearerClient(str(pair, "token"))
		self := dc.expect(t, 200, "GET", "/api/device/v1/self", nil)
		if str(self["marina"].(map[string]any), "id") != alpha {
			t.Fatalf("device self: %v", self)
		}
		dc.expect(t, 200, "GET", M, nil)

		// Events: outbox -> webhook + SSE.
		h.runJobs("outbox.dispatch", "webhooks.deliver")
		if sink.Count() == 0 {
			t.Fatal("webhook not delivered")
		}
		last := sink.Last()
		if last.Header["Porto-Event"] != event.MemberAdded {
			t.Fatalf("webhook event %q", last.Header["Porto-Event"])
		}
		if err := event.VerifySignature(secret, last.Header["Porto-Signature"], last.Body, time.Now(), 5*time.Minute); err != nil {
			t.Fatalf("signature: %v", err)
		}
		sse.waitFor(t, "event: team.changed")

		// Audit rows for the writes.
		audit := oc.expect(t, 200, "GET", M+"/audit-log?limit=200", nil)["items"].([]any)
		actions := map[string]bool{}
		for _, it := range audit {
			actions[str(it.(map[string]any), "action")] = true
		}
		for _, want := range []string{"marina.update", "member.add", "api_key.create", "device.pairing_code_create", "device.pair", "webhook.create"} {
			if !actions[want] {
				t.Errorf("no audit row %q in %v", want, actions)
			}
		}

		// Logout.
		oc.expect(t, 204, "POST", A+"/auth/logout", nil)
		oc.expect(t, 401, "GET", A+"/auth/me", nil)

		assertNoPII(t, h.logs.String(), str(key, "secret"), str(pair, "token"), secret, str(pc, "code"))
	})
}

// assertNoPII is TestLogs_NoPII: the logs of a whole acceptance run carry no email, password or token.
func assertNoPII(t *testing.T, logs string, secrets ...string) {
	t.Helper()
	if logs == "" {
		t.Fatal("no logs captured")
	}
	if m := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[a-z]{2,}`).FindString(logs); m != "" {
		t.Errorf("logs contain an email address: %q", m)
	}
	for _, s := range append(secrets, password, "po_live_", "po_dev_", "whsec_") {
		if s != "" && strings.Contains(logs, s) {
			t.Errorf("logs contain a secret (%d chars)", len(s))
		}
	}
}

func TestLogs_NoPII(t *testing.T) {
	h := newHarness(t, testdb.SQLite(t))
	ctx := context.Background()
	o, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: "mario.rossi@porto.test", Name: "Mario Rossi", Password: password, Organization: "Org",
		MarinaName: "Marina", MarinaSlug: "marina-logs",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := h.cookieClient()
	c.do(t, "POST", A+"/auth/login", map[string]string{"email": "mario.rossi@porto.test", "password": "wrong-password-1"})
	c.do(t, "POST", A+"/auth/login", map[string]string{"email": "nobody@porto.test", "password": "wrong-password-1"})
	c.login(t, "mario.rossi@porto.test")
	c.expect(t, 201, "POST", A+"/marinas/"+o.MarinaID+"/members", map[string]any{"email": "anna.bianchi@porto.test", "name": "Anna Bianchi", "role": "dock"})
	c.expect(t, 204, "POST", A+"/auth/password-reset/request", map[string]string{"email": "mario.rossi@porto.test"})
	key := c.expect(t, 201, "POST", A+"/marinas/"+o.MarinaID+"/api-keys", map[string]any{"name": "k", "scopes": []string{"marina.read"}})
	h.bearerClient(str(key, "secret")).expect(t, 200, "GET", A+"/marinas/"+o.MarinaID, nil)
	h.bearerClient("po_live_abcdefgh_forged").expect(t, 401, "GET", A+"/marinas/"+o.MarinaID, nil)
	h.runJobs("outbox.dispatch", "email.deliver")
	logs := h.logs.String()
	for _, name := range []string{"Mario Rossi", "Anna Bianchi"} {
		if strings.Contains(logs, name) {
			t.Errorf("logs contain the name %q", name)
		}
	}
	assertNoPII(t, logs, str(key, "secret"))
}

type stream struct {
	mu    sync.Mutex
	lines []string
	cond  chan struct{}
}

func openStream(t *testing.T, h *harness, c *client, marinaID string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", h.srv.URL+A+"/stream?marina_id="+marinaID, nil)
	hc := &http.Client{Jar: c.http.Jar}
	res, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("stream: %d", res.StatusCode)
	}
	s := &stream{cond: make(chan struct{}, 1)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			s.mu.Lock()
			s.lines = append(s.lines, sc.Text())
			s.mu.Unlock()
			select {
			case s.cond <- struct{}{}:
			default:
			}
		}
	}()
	return s
}

func (s *stream) waitFor(t *testing.T, prefix string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		for _, l := range s.lines {
			if strings.HasPrefix(l, prefix) {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
		select {
		case <-s.cond:
		case <-deadline:
			t.Fatalf("SSE: no line %q", prefix)
		}
	}
}

func TestAcceptance_CrossMarinaIsolation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		a, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "a@porto.test", Name: "Owner A", Password: password, Organization: "Org A", MarinaName: "Marina A", MarinaSlug: "marina-a",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		}); err != nil {
			t.Fatal(err)
		}
		bc := h.cookieClient()
		bc.login(t, "b@porto.test")

		router, ok := h.app.Router.(chi.Routes)
		if !ok {
			t.Fatal("router is not a chi router")
		}
		other := "01900000-0000-7000-8000-000000000001"
		checked := 0
		err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !strings.Contains(route, "{marinaId}") && !strings.Contains(route, "{organizationId}") {
				return nil
			}
			path := strings.ReplaceAll(route, "{marinaId}", a.MarinaID)
			path = strings.ReplaceAll(path, "{organizationId}", a.OrganizationID)
			path = regexp.MustCompile(`\{[A-Za-z]+\}`).ReplaceAllString(path, other)
			var body any
			if method != "GET" && method != "DELETE" {
				body = map[string]any{}
			}
			r := bc.do(t, method, path, body, "If-Match", `"1"`)
			if r.status != http.StatusNotFound || r.problemType() != "not_found" {
				t.Errorf("%s %s: status %d %s, want 404 not_found", method, route, r.status, r.body)
			}
			checked++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if checked < 100 { // M0 + M1 + M2 routes (m1-plan §8.2, m2-plan §8.2)
			t.Fatalf("only %d routes checked", checked)
		}
		if r := bc.do(t, "GET", A+"/stream?marina_id="+a.MarinaID, nil); r.status != 404 {
			t.Errorf("stream of another marina: %d", r.status)
		}
	})
}
