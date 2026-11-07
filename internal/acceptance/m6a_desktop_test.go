//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/lan"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// lanRuntime is the smallest desktopport.Runtime the LAN acceptance needs (status, owner-only settings, the
// local-network controller); everything else is not used here. The real runtime is internal/desktop's.
type lanRuntime struct {
	lan *lan.Server
}

func (r *lanRuntime) Status(context.Context) desktopport.Status {
	return desktopport.Status{Version: "test", Mode: desktopport.ModeLocal, SecretStore: desktopport.SecretStoreFile}
}

func (r *lanRuntime) FirstRun(context.Context, desktopport.FirstRunInput) (*desktopport.FirstRunResult, error) {
	return nil, desktopport.ErrFirstRunDone
}

func (r *lanRuntime) FirstRunRestore(context.Context, string, desktopport.RestoreSource) error {
	return desktopport.ErrFirstRunToken
}

func (r *lanRuntime) Settings(_ context.Context, p *auth.Principal) (desktopport.Settings, error) {
	if !p.IsOwnerAnywhere() {
		return desktopport.Settings{}, auth.ErrForbidden
	}
	st := r.lan.Status()
	return desktopport.Settings{Mode: desktopport.ModeLocal, LANEnabled: st.Enabled, LANPort: st.Port}, nil
}

func (r *lanRuntime) UpdateSettings(ctx context.Context, p *auth.Principal, _ desktopport.SettingsPatch) (desktopport.Settings, error) {
	return r.Settings(ctx, p)
}
func (r *lanRuntime) LAN() desktopport.LANController { return r.lan }
func (r *lanRuntime) Backups(context.Context, *auth.Principal) ([]desktopport.BackupFile, desktopport.BackupStatus, error) {
	return nil, desktopport.BackupStatus{}, auth.ErrForbidden
}

func (r *lanRuntime) BackupNow(context.Context, *auth.Principal) (desktopport.BackupFile, error) {
	return desktopport.BackupFile{}, auth.ErrForbidden
}

func (r *lanRuntime) Restore(context.Context, *auth.Principal, desktopport.RestoreSource) error {
	return auth.ErrForbidden
}

func (r *lanRuntime) RevealSecretKey(context.Context, *auth.Principal, string) (string, error) {
	return "", auth.ErrForbidden
}
func (r *lanRuntime) Activate(context.Context, string) error { return desktopport.ErrInstanceToken }
func (r *lanRuntime) Open(context.Context, *auth.Principal, string) error {
	return auth.ErrForbidden
}

// desktopHarness is an in-process desktop instance: the app on a loopback listener (http, tagged loopback,
// Host-checked) and the real local-network server on 127.0.0.1 (PORTO_DESKTOP_LAN_LOOPBACK).
type desktopHarness struct {
	app      *app.App
	lan      *lan.Server
	loopback string // http://127.0.0.1:<port>
	logs     *syncBuffer
}

func newDesktopHarness(t *testing.T, db *store.DB) *desktopHarness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	secret := base64.StdEncoding.EncodeToString(key)
	dataDir := t.TempDir()
	vars := map[string]string{
		"PORTO_SECRET_KEY": secret, "PORTO_BASE_URL": base, "PORTO_DATA_DIR": dataDir,
		"PORTO_ADDR": "127.0.0.1:" + strconv.Itoa(port), "PORTO_LOG_LEVEL": "debug",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecureCookies() {
		t.Fatal("desktop loopback over http must not mark cookies Secure")
	}
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rt := &lanRuntime{}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, log, app.Options{
		DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Desktop: rt,
		PairTarget: func() desktopport.PairTarget { return rt.lan.PairTarget() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	keys := cfg.Keys
	lanPort, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lp := lanPort.Addr().(*net.TCPAddr).Port
	_ = lanPort.Close()
	rt.lan = lan.New(lan.Options{
		Dir: dataDir + "/lan", Keys: keys, Clock: a.Clock, Log: log, Port: lp, Loopback: true, FallbackOrigin: base,
		NewServer: func(hosts []string) *http.Server { return a.NewHTTPServer(reqctx.ListenerLAN, hosts) },
	})
	srv := a.NewHTTPServer(reqctx.ListenerLoopback, []string{"127.0.0.1:" + strconv.Itoa(port), "localhost:" + strconv.Itoa(port)})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = rt.lan.Disable(context.Background())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return &desktopHarness{app: a, lan: rt.lan, loopback: base, logs: logs}
}

// webClient is a browser-like client: cookie jar, CSRF double submit.
type webClient struct {
	base string
	http *http.Client
}

func newWebClient(base string, tlsFingerprint string) *webClient {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	if tlsFingerprint != "" {
		c.Transport = &http.Transport{TLSClientConfig: lan.PinnedClientConfig(tlsFingerprint)}
	}
	return &webClient{base: base, http: c}
}

func (c *webClient) cookie(name string) *http.Cookie {
	u, _ := url.Parse(c.base)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

func (c *webClient) do(t *testing.T, method, path string, body any) response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ck := c.cookie("porto_csrf"); ck != nil {
		req.Header.Set("X-CSRF-Token", ck.Value)
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: b}
}

func (c *webClient) expect(t *testing.T, want int, method, path string, body any) map[string]any {
	t.Helper()
	r := c.do(t, method, path, body)
	if r.status != want {
		t.Fatalf("%s %s%s: status %d, want %d: %s", method, c.base, path, r.status, want, r.body)
	}
	if len(r.body) == 0 || r.body[0] != '{' {
		return nil
	}
	return r.json(t)
}

func TestAcceptance_M6aLanPairing(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newDesktopHarness(t, db)
		ctx := context.Background()
		o, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "titolare@porto.test", Name: "Titolare", Password: password, Organization: "Porto Srl",
			MarinaName: "Marina", MarinaSlug: "marina-lan",
		})
		if err != nil {
			t.Fatal(err)
		}
		M := A + "/marinas/" + o.MarinaID

		// Owner in the app window (loopback).
		owner := newWebClient(h.loopback, "")
		owner.expect(t, 200, "POST", A+"/auth/login", map[string]string{"email": "titolare@porto.test", "password": password})
		if ck := owner.cookie("porto_session"); ck == nil {
			t.Fatal("no session cookie on loopback")
		}
		owner.expect(t, 200, "GET", "/api/desktop/v1/status", nil)
		meta := owner.expect(t, 200, "GET", A+"/meta", nil)
		if meta["edition"] != "desktop" || meta["local_mode"] != true {
			t.Fatalf("meta %v", meta)
		}

		// With the local network off the pairing dialog has nothing reachable to show.
		dock := owner.expect(t, 201, "POST", M+"/members", map[string]any{"email": "banchina@porto.test", "name": "Banchina", "role": "dock"})
		dockID := str(dock, "user_id")
		if dockID == "" {
			dockID = str(dock, "id")
		}
		off := owner.expect(t, 201, "POST", M+"/devices/pairing-codes", map[string]any{"user_id": dockID, "kind": "mobile"})
		if off["reachable"] != false || off["fingerprint"] != nil {
			t.Fatalf("pairing code with the local network off %v", off)
		}

		// Owner enables the local network (desktop API, else the controller while the handler is a stub).
		var lanURL string
		if r := owner.do(t, "PUT", "/api/desktop/v1/lan", map[string]any{"enabled": true}); r.status == http.StatusOK {
			lanURL = str(r.json(t), "url")
		} else if r.status == http.StatusNotImplemented {
			st, err := h.lan.Enable(ctx, desktopport.LANOptions{})
			if err != nil {
				t.Fatal(err)
			}
			lanURL = st.URL
		} else {
			t.Fatalf("enable LAN: %d %s", r.status, r.body)
		}
		if !strings.HasPrefix(lanURL, "https://127.0.0.1:") {
			t.Fatalf("LAN URL %q", lanURL)
		}

		pc := owner.expect(t, 201, "POST", M+"/devices/pairing-codes", map[string]any{"user_id": dockID, "kind": "mobile"})
		pairURL, _ := pc["pair_url"].(string)
		if pc["reachable"] != true || !strings.HasPrefix(pairURL, lanURL+"/admin/pair#c=") {
			t.Fatalf("pairing code %v", pc)
		}
		frag, err := url.ParseQuery(pairURL[strings.Index(pairURL, "#")+1:])
		if err != nil || frag.Get("c") != str(pc, "code") || frag.Get("fp") != str(pc, "fingerprint") || len(frag.Get("fp")) != 43 {
			t.Fatalf("fragment %v %v", frag, err)
		}

		// The phone opens the QR URL; it pins the fingerprint of the QR (stands for accepting the warning once).
		phone := newWebClient(lanURL, frag.Get("fp"))
		if r := phone.do(t, "GET", "/api/desktop/v1/status", nil); r.status != http.StatusNotFound {
			t.Fatalf("desktop API over the LAN: %d", r.status)
		}
		phone.expect(t, 401, "GET", A+"/auth/me", nil)
		paired := phone.expect(t, 201, "POST", "/api/device/v1/pair-session", map[string]any{
			"code": frag.Get("c"), "name": "Telefono Android", "platform": "android", "kind": "mobile",
		})
		dev, _ := paired["device"].(map[string]any)
		marina, _ := paired["marina"].(map[string]any)
		if dev["access"] != "browser" || marina["id"] != o.MarinaID || paired["token"] != nil || paired["totp_required"] != false {
			t.Fatalf("pair-session %v", paired)
		}
		// Second-factor step: exactly one code, and only for a live device session.
		if r := phone.do(t, "POST", "/api/device/v1/pair-session/totp", map[string]any{}); r.status != 400 {
			t.Fatalf("totp without a code: %d %s", r.status, r.body)
		}
		if r := newWebClient(lanURL, frag.Get("fp")).do(t, "POST", "/api/device/v1/pair-session/totp",
			map[string]any{"totp_code": "123456"}); r.status != 401 || r.problemType() != "unauthorized" {
			t.Fatalf("totp without a device session: %d %s", r.status, r.body)
		}
		session := phone.cookie("porto_session")
		if session == nil {
			t.Fatal("no device session cookie")
		}
		if r := phone.do(t, "POST", "/api/device/v1/pair-session", map[string]any{
			"code": frag.Get("c"), "name": "Altro", "platform": "ios", "kind": "mobile",
		}); r.status != 400 || r.problemType() != "pairing_code_invalid" {
			t.Fatalf("code reused: %d %s", r.status, r.body)
		}
		me := phone.expect(t, 200, "GET", A+"/auth/me", nil)
		user, _ := me["user"].(map[string]any)
		if user["id"] != dockID {
			t.Fatalf("me %v", me)
		}
		phone.expect(t, 200, "GET", M, nil)
		if r := phone.do(t, "GET", "/api/desktop/v1/settings", nil); r.status != http.StatusNotFound {
			t.Fatalf("desktop settings over the LAN: %d", r.status)
		}

		// The device session cookie is Secure over TLS (the loopback base URL is http).
		var setCookie string
		for _, c := range phoneSetCookies(t, lanURL, frag.Get("fp"), owner, M, dockID) {
			if strings.HasPrefix(c, "porto_session=") {
				setCookie = c
			}
		}
		if !strings.Contains(setCookie, "Secure") || !strings.Contains(setCookie, "HttpOnly") {
			t.Fatalf("device session cookie %q", setCookie)
		}

		// The owner sees the browser device and revokes it: the phone is logged out at its next request.
		list := owner.expect(t, 200, "GET", M+"/devices", nil)
		items, _ := list["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("devices %v", list)
		}
		owner.expect(t, 204, "DELETE", M+"/devices/"+str(dev, "id"), nil)
		phone.expect(t, 401, "GET", A+"/auth/me", nil)

		// Local network off: nothing listens any more.
		if err := h.lan.Disable(ctx); err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(lanURL)
		if c, err := net.DialTimeout("tcp", u.Host, 300*time.Millisecond); err == nil {
			_ = c.Close()
			t.Fatal("LAN listener still open after disable")
		}

		assertNoPII(t, h.logs.String(), frag.Get("c"), session.Value, str(pc, "code"))
	})
}

// phoneSetCookies pairs a second phone and returns the raw Set-Cookie headers of pair-session.
func phoneSetCookies(t *testing.T, lanURL, fp string, owner *webClient, marinaPath, userID string) []string {
	t.Helper()
	pc := owner.expect(t, 201, "POST", marinaPath+"/devices/pairing-codes", map[string]any{"user_id": userID, "kind": "mobile"})
	b, _ := json.Marshal(map[string]any{"code": str(pc, "code"), "name": "Secondo", "platform": "ios", "kind": "mobile"})
	req, _ := http.NewRequest("POST", lanURL+"/api/device/v1/pair-session", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: lan.PinnedClientConfig(fp)}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("second pairing %d %s", res.StatusCode, body)
	}
	return res.Header.Values("Set-Cookie")
}
