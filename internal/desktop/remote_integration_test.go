//go:build integration

package desktop

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/pkg/ext"
)

// onlineInstance is a Porto server (server edition) with an owner and a marina, for the remote mode.
type onlineInstance struct {
	app      *app.App
	srv      *httptest.Server
	owner    *auth.Principal
	ownerID  string
	marinaID string
}

func newOnlineInstance(t *testing.T) *onlineInstance {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://127.0.0.1:1",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_AUTO_MIGRATE": "true", "PORTO_DEV": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := app.NewWithOptions(ctx, cfg, "1.0.0", ext.Extensions{}, slog.New(slog.DiscardHandler), app.Options{Argon2: auth.TestArgon2Params})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: "online@porto.test", Name: "Online", Password: testPassword, Organization: "Online SRL",
		MarinaName: "Marina Online", MarinaSlug: "online",
	})
	if err != nil {
		t.Fatal(err)
	}
	login, err := a.Auth.Login(ctx, auth.LoginInput{Email: "online@porto.test", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	return &onlineInstance{app: a, srv: srv, owner: login.Principal, ownerID: res.UserID, marinaID: res.MarinaID}
}

func (o *onlineInstance) pairingCode(t *testing.T) string {
	t.Helper()
	pc, err := o.app.Devices.CreatePairingCode(context.Background(), o.owner, o.marinaID, device.PairingInput{UserID: o.ownerID, Kind: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	return pc.Code
}

// TestLauncher_PairRemoteAndUnpair: first run → "Collega a un Porto online" → the app restarts in remote mode on
// the same port, the panel API goes through the proxy with the device token, unpairing with the app PIN brings
// the app back to the first run of the local mode.
func TestLauncher_PairRemoteAndUnpair(t *testing.T) {
	online := newOnlineInstance(t)
	dir := t.TempDir()
	env := map[string]string{
		"PORTO_DESKTOP_KEYRING": "file", "PORTO_UPDATE_CHECK": "false", "PORTO_DEV": "true",
		"PORTO_DESKTOP_FIRST_RUN_FILE": "true",
	}
	ui := &recordUI{HeadlessUI: NewHeadlessUI()}
	ready, nav, done := make(chan string, 1), make(chan string, 4), make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Version: "1.0.0", DataDir: dir, Headless: true, GOOS: "linux", GOARCH: "amd64", LookupEnv: lookupOf(env),
			UI: ui, CountUsers: testCountUsers, Argon2: auth.TestArgon2Params,
			Opener:  func(context.Context, string) error { return nil },
			OnReady: func(u string) { ready <- u }, OnNavigate: func(u string) { nav <- u },
		})
	}()
	var first string
	select {
	case first = <-ready:
	case err := <-done:
		t.Fatalf("launcher exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("launcher not ready")
	}
	defer func() {
		ui.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop: %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Error("launcher did not stop")
		}
	}()
	jar, _ := cookiejar.New(nil)
	r := &running{t: t, dir: dir, url: first, ui: ui, done: done, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	r.waitReady()
	_, tok, _ := strings.Cut(first, "#t=")

	// Wrong PIN format and wrong code keep the first-run token usable.
	if st, body := r.do(http.MethodPost, "/api/desktop/v1/remote/pair", map[string]any{
		"first_run_token": tok, "url": online.srv.URL, "code": online.pairingCode(t), "pin": "12",
	}); st != http.StatusBadRequest {
		t.Fatalf("short pin %d %v", st, body)
	}
	if st, body := r.do(http.MethodPost, "/api/desktop/v1/remote/pair", map[string]any{
		"first_run_token": tok, "url": online.srv.URL, "code": "WRONG-CODE", "pin": "246810",
	}); st != http.StatusBadRequest {
		t.Fatalf("wrong code %d %v", st, body)
	}
	link := "porto://pair?v=1&url=" + url.QueryEscape(online.srv.URL) + "&code=" + online.pairingCode(t)
	if st, body := r.do(http.MethodPost, "/api/desktop/v1/remote/pair", map[string]any{
		"first_run_token": tok, "link": link, "pin": "246810", "name": "Banco",
	}); st != http.StatusAccepted {
		t.Fatalf("pair %d %v", st, body)
	}
	var enter string
	select {
	case enter = <-nav:
	case <-time.After(30 * time.Second):
		t.Fatal("no navigation after pairing")
	}
	if !strings.Contains(enter, "/__porto/enter#k=") || !strings.HasPrefix(enter, r.origin()) {
		t.Fatalf("enter url %q", enter)
	}
	if b, err := os.ReadFile(Paths{Root: dir}.RemoteEnterURL()); err != nil || strings.TrimSpace(string(b)) != enter {
		t.Fatalf("remote-enter.url %q %v", b, err)
	}
	s, err := LoadSettings(Paths{Root: dir}.Settings())
	if err != nil || s.Mode != "remote" || s.Remote == nil || s.Remote.MarinaID != online.marinaID {
		t.Fatalf("settings %+v %v", s.Remote, err)
	}
	raw, _ := os.ReadFile(Paths{Root: dir}.Settings())
	if strings.Contains(string(raw), "po_dev_") {
		t.Fatal("device token in desktop.json")
	}
	if st, _ := os.Stat(Paths{Root: dir}.DeviceTokenFile()); st == nil || st.Mode().Perm() != 0o600 {
		t.Fatal("device token file missing or not 0600")
	}

	// Through the proxy.
	frag, _ := url.ParseQuery(enter[strings.Index(enter, "#")+1:])
	k := frag.Get("k")
	if frag.Get("next") != "/admin/m/"+online.marinaID {
		t.Fatalf("start page %q", frag.Get("next"))
	}
	if st, _ := r.do(http.MethodGet, "/api/admin/v1/organizations", nil); st != http.StatusUnauthorized {
		t.Fatalf("before enter %d", st)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, r.origin()+"/__porto/enter", strings.NewReader("k="+url.QueryEscape(k)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", r.origin())
	if res, err := r.http.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("enter %v %v", res, err)
	} else {
		_ = res.Body.Close()
	}
	if st, body := r.do(http.MethodGet, "/api/admin/v1/organizations", nil); st != http.StatusOK || !strings.Contains(fmtAny(body), "Online SRL") {
		t.Fatalf("organizations through the proxy %d %v", st, body)
	}
	if st, body := r.do(http.MethodGet, "/api/desktop/v1/status", nil); st != http.StatusOK || body["mode"] != "remote" {
		t.Fatalf("status %d %v", st, body)
	}

	// Unpair with the PIN: the device is revoked upstream and the app is back on the first run.
	if st, _ := r.do(http.MethodDelete, "/api/desktop/v1/remote", nil, "Origin", r.origin(), "Porto-App-Pin", "000000"); st != http.StatusUnauthorized {
		t.Fatalf("unpair with a wrong pin %d", st)
	}
	if st, body := r.do(http.MethodDelete, "/api/desktop/v1/remote", nil, "Origin", r.origin(), "Porto-App-Pin", "246810"); st != http.StatusAccepted {
		t.Fatalf("unpair %d %v", st, body)
	}
	select {
	case back := <-nav:
		if !strings.Contains(back, "/admin/desktop/first-run#t=") {
			t.Fatalf("after unpair %q", back)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no navigation after unpairing")
	}
	r.waitReady()
	if st, body := r.do(http.MethodGet, "/api/desktop/v1/status", nil); st != http.StatusOK || body["mode"] != "local" || body["first_run_pending"] != true {
		t.Fatalf("status after unpair %d %v", st, body)
	}
	devs, err := online.app.Devices.List(context.Background(), online.owner, online.marinaID)
	if err != nil || len(devs) != 1 || devs[0].RevokedAt.IsZero() {
		t.Fatalf("device not revoked upstream: %+v %v", devs, err)
	}
}

func fmtAny(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

var jsonMarshal = json.Marshal
