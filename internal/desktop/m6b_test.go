package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/desktop/remote"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/peripheral"
)

func validRemoteSettings() FileSettings {
	s := DefaultSettings()
	s.Mode, s.AppPinHash = desktopport.ModeRemote, "$argon2id$x"
	s.Remote = &RemoteFile{URL: "https://porto.example.it", DeviceID: "dev", MarinaID: "m1", MarinaName: "Marina"}
	return s
}

func TestRemoteSettings_Validate(t *testing.T) {
	s := validRemoteSettings()
	if err := s.Validate(); err != nil {
		t.Fatalf("valid remote settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSettings(path)
	if err != nil || back.Remote == nil || back.Remote.MarinaID != "m1" || back.Mode != desktopport.ModeRemote {
		t.Fatalf("round trip %+v %v", back.Remote, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "po_dev_") || strings.Contains(string(raw), "token") {
		t.Fatal("desktop.json carries a token")
	}
	for name, change := range map[string]func(*FileSettings){
		"mode":                       func(s *FileSettings) { s.AppPinHash = "" },
		"remote.url":                 func(s *FileSettings) { s.Remote.URL = "ftp://x" },
		"remote.device_id":           func(s *FileSettings) { s.Remote.DeviceID = "" },
		"remote.leaf_fp":             func(s *FileSettings) { s.Remote.LeafFP = "zz" },
		"peripherals.receipt.driver": func(s *FileSettings) { s.Peripherals.Receipt.Driver = "laser" },
		"peripherals.receipt.target": func(s *FileSettings) {
			s.Peripherals.Receipt.Driver, s.Peripherals.Receipt.Target = "escpos", "http://x"
		},
		"peripherals.fiscal.driver":    func(s *FileSettings) { s.Peripherals.Fiscal.Driver = "epson" },
		"peripherals.reader.driver":    func(s *FileSettings) { s.Peripherals.Reader.Driver = "serial" },
		"peripherals.label.driver":     func(s *FileSettings) { s.Peripherals.Label.Driver = "laser" },
		"peripherals.fiscal.fake_mode": func(s *FileSettings) { s.Peripherals.Fiscal.Driver, s.Peripherals.Fiscal.FakeMode = "fake", "x" },
	} {
		s := validRemoteSettings()
		r := *s.Remote
		s.Remote = &r
		change(&s)
		var se *desktopport.SettingsError
		if err := s.Validate(); !errors.As(err, &se) || !slices.Contains(se.Fields, name) {
			t.Errorf("%s: %v", name, err)
		}
	}
	m := DefaultSettings()
	m.Mode = desktopport.ModeRemote
	if err := m.Validate(); err == nil {
		t.Fatal("remote mode without the remote instance accepted")
	}
}

func TestRemotePin_RateLimitAndHash(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC))
	g := newPinGuard(auth.TestArgon2Params, clk)
	for _, bad := range []string{"", "12345", "1234567890123", "12a456"} {
		if _, err := g.hash(bad); err == nil {
			t.Errorf("pin %q accepted", bad)
		}
	}
	h, err := g.hash("246810")
	if err != nil || !strings.HasPrefix(h, "$argon2id$") || strings.Contains(h, "246810") {
		t.Fatalf("hash %q %v", h, err)
	}
	if err := g.check("246810", h); err != nil {
		t.Fatalf("right pin: %v", err)
	}
	for range 5 {
		if err := g.check("111111", h); !errors.Is(err, desktopport.ErrAppPinInvalid) {
			t.Fatalf("wrong pin: %v", err)
		}
	}
	var rl *auth.RateLimitError
	if err := g.check("246810", h); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("sixth attempt not rate limited: %v", err)
	}
	clk.Advance(16 * time.Minute)
	if err := g.check("246810", h); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestKioskPolicy(t *testing.T) {
	for _, c := range []struct {
		in   KioskInput
		want KioskDecision
	}{
		{KioskInput{Enabled: true, Window: true}, KioskDecision{Kiosk: true, StartPath: "/admin/"}},
		{KioskInput{Enabled: true, Window: false}, KioskDecision{StartPath: "/admin/"}},
		{KioskInput{Enabled: false, Window: true}, KioskDecision{StartPath: "/admin/"}},
		{KioskInput{Enabled: true, Window: true, FirstRun: true}, KioskDecision{StartPath: "/admin/"}},
		{KioskInput{Enabled: true, Window: true, Remote: true, MarinaID: "m-1"}, KioskDecision{Kiosk: true, StartPath: "/admin/m/m-1"}},
	} {
		if got := KioskPolicy(c.in); got != c.want {
			t.Errorf("KioskPolicy(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
	o := "http://127.0.0.1:4100"
	for target, ok := range map[string]bool{
		"/admin/m/1": true, "http://127.0.0.1:4100/admin/": true, "https://evil.example/": false,
		"http://127.0.0.1:4101/": false, "//evil.example/x": false, "javascript:alert(1)": false,
	} {
		if KioskNavigationAllowed(o, target) != ok {
			t.Errorf("navigation to %q", target)
		}
	}
	if !KioskShortcutExit("darwin", "K", true, false, true) || KioskShortcutExit("darwin", "k", false, true, true) ||
		!KioskShortcutExit("windows", "k", false, true, true) || KioskShortcutExit("linux", "k", false, true, false) {
		t.Error("exit shortcut")
	}
	if s := KioskScript(o, "darwin"); !strings.Contains(s, KioskExitPath) || !strings.Contains(s, "window.open") {
		t.Error("kiosk script")
	}
}

type memToken struct {
	mu sync.Mutex
	v  string
}

func (m *memToken) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.v == "" {
		return "", ErrSecretNotFound
	}
	return m.v, nil
}

func (m *memToken) Set(v string) error { m.mu.Lock(); m.v = v; m.mu.Unlock(); return nil }
func (m *memToken) Delete() error      { m.mu.Lock(); m.v = ""; m.mu.Unlock(); return nil }

// remoteEnv is a RemoteRuntime behind its proxy, with a fake online instance.
type remoteEnv struct {
	rr       *RemoteRuntime
	proxy    *remote.Proxy
	base     string
	client   *http.Client
	paths    Paths
	resets   chan bool
	upstream *httptest.Server
	unpaired chan struct{}
}

func newRemoteEnv(t *testing.T) *remoteEnv {
	t.Helper()
	e := &remoteEnv{resets: make(chan bool, 1), unpaired: make(chan struct{}, 1)}
	e.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/device/v1/self":
			e.unpaired <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/receipt") && r.URL.Query().Get("format") == "json":
			if !strings.Contains(r.URL.Path, "/marinas/m1/payments/pay-1/") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(api.ReceiptData{
				Number: "2026-000001", Date: "2026-11-08", Method: "cash", AmountCents: 2500, NotFiscal: true,
				Marina: api.ReceiptMarina{Name: "Marina"}, Lines: []api.ReceiptDataLine{{Description: "Ormeggio", QtyMilli: 1000, GrossCents: 2500}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(e.upstream.Close)
	e.paths = Paths{Root: t.TempDir()}
	s := validRemoteSettings()
	s.Remote.URL = e.upstream.URL
	g := newPinGuard(auth.TestArgon2Params, clock.System{})
	s.AppPinHash, _ = g.hash("135790")
	s.Peripherals.Receipt.Driver, s.Peripherals.Reader.Driver, s.Peripherals.Fiscal.Driver = "fake", "fake", "fake"
	if err := SaveSettings(e.paths.Settings(), s); err != nil {
		t.Fatal(err)
	}
	tokens := &memToken{v: "po_dev_x_secret"}
	e.rr = NewRemoteRuntime(RemoteRuntimeConfig{
		Version: "1.2.0", Paths: e.paths, Env: Env{Dev: true}, Settings: s, SecretKind: "keyring",
		Argon2: auth.TestArgon2Params, Tokens: tokens,
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	origin, _ := url.Parse(e.upstream.URL)
	local := remote.NewLocalAPI(e.rr, "m1", true)
	e.proxy = remote.New(remote.Options{Origin: origin, Token: e.rr.Token, Port: port, CacheDir: e.paths.RemoteCache(), Dev: true, Local: local})
	local.Proxy = e.proxy
	e.rr.Attach(e.proxy, nil, func(revoked bool) { e.resets <- revoked })
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: e.proxy, ReadHeaderTimeout: 5 * time.Second}}
	srv.Start()
	t.Cleanup(srv.Close)
	e.base = "http://127.0.0.1:" + strconv.Itoa(port)
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	enter, _ := e.proxy.EnterURL("")
	k := enter[strings.Index(enter, "#k=")+3:]
	if r := e.do(t, http.MethodPost, remote.EnterPath, "k="+url.QueryEscape(k), "", "application/x-www-form-urlencoded"); r.code != http.StatusNoContent {
		t.Fatalf("enter %d", r.code)
	}
	return e
}

type resp struct {
	code int
	body string
}

func (e *remoteEnv) do(t *testing.T, method, path, body, pin, ct string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, e.base+path, rd)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if ct == "" && body != "" {
		ct = "application/json"
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if pin != "" {
		req.Header.Set(remote.AppPinHeader, pin)
	}
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, string(b)}
}

func TestRemoteRuntime_LocalAPIThroughProxy(t *testing.T) {
	e := newRemoteEnv(t)
	st := e.do(t, http.MethodGet, "/api/desktop/v1/status", "", "", "")
	var status api.DesktopStatus
	_ = json.Unmarshal([]byte(st.body), &status)
	if st.code != http.StatusOK || status.Mode != "remote" || status.Remote == nil || status.Remote.Marina.Id != "m1" || !status.Remote.Reachable {
		t.Fatalf("status %d %s", st.code, st.body)
	}
	if strings.Contains(st.body, "po_dev_") || strings.Contains(st.body, "argon2") {
		t.Fatal("status leaks a secret")
	}
	// M6a operations do not exist in remote mode.
	if r := e.do(t, http.MethodGet, "/api/desktop/v1/settings", "", "", ""); r.code != http.StatusNotFound {
		t.Fatalf("settings in remote mode %d", r.code)
	}

	// Bridge: status, receipt through the instance, scan, test with the PIN.
	b := e.do(t, http.MethodGet, "/api/bridge/v1/status", "", "", "")
	if b.code != http.StatusOK || !strings.Contains(b.body, `"receipt":{"driver":"fake","message":"ready","ready":true}`) {
		t.Fatalf("bridge status %d %s", b.code, b.body)
	}
	if r := e.do(t, http.MethodPost, "/api/bridge/v1/receipts", `{"payment_id":"pay-1"}`, "", ""); r.code != http.StatusAccepted {
		t.Fatalf("print %d %s", r.code, r.body)
	}
	lines, _ := peripheral.ReadFake(e.paths.Peripherals(), "receipt")
	if len(lines) != 1 || !strings.Contains(string(lines[0]), "2026-000001") {
		t.Fatalf("fake receipt %s", lines)
	}
	if r := e.do(t, http.MethodPost, "/api/bridge/v1/receipts", `{"payment_id":"other"}`, "", ""); r.code != http.StatusNotFound {
		t.Fatalf("unknown payment %d", r.code)
	}
	e.rr.FakeReader().Push("PT1:0190aaaa-0000-7000-8000-000000000001")
	if r := e.do(t, http.MethodGet, "/api/bridge/v1/scanner/next?timeout=2", "", "", ""); r.code != http.StatusOK || !strings.Contains(r.body, "PT1:") {
		t.Fatalf("scan %d %s", r.code, r.body)
	}
	if r := e.do(t, http.MethodGet, "/api/bridge/v1/scanner/next?timeout=1", "", "", ""); r.code != http.StatusNoContent {
		t.Fatalf("no scan %d", r.code)
	}
	if r := e.do(t, http.MethodPost, "/api/bridge/v1/test", `{"device":"receipt"}`, "", ""); r.code != http.StatusUnauthorized || !strings.Contains(r.body, "app_pin_invalid") {
		t.Fatalf("test without pin %d %s", r.code, r.body)
	}
	if r := e.do(t, http.MethodPost, "/api/bridge/v1/test", `{"device":"receipt"}`, "135790", ""); r.code != http.StatusNoContent {
		t.Fatalf("test with pin %d %s", r.code, r.body)
	}
	if r := e.do(t, http.MethodPost, "/api/bridge/v1/test", `{"device":"label"}`, "135790", ""); r.code != http.StatusServiceUnavailable {
		t.Fatalf("test of a missing label printer %d", r.code)
	}

	// Peripheral settings and kiosk need the PIN.
	ps := `{"receipt":{"driver":"escpos","target":"tcp://192.168.1.50:9100","width":58},"label":{"driver":"none"},"reader":{"driver":"keyboard"},"fiscal":{"driver":"none"}}`
	if r := e.do(t, http.MethodPut, "/api/desktop/v1/peripherals", ps, "", ""); r.code != http.StatusUnauthorized {
		t.Fatalf("peripherals without pin %d", r.code)
	}
	if r := e.do(t, http.MethodPut, "/api/desktop/v1/peripherals", strings.Replace(ps, "192.168.1.50", "8.8.8.8", 1), "135790", ""); r.code != http.StatusBadRequest {
		t.Fatalf("public printer address %d %s", r.code, r.body)
	}
	if r := e.do(t, http.MethodPut, "/api/desktop/v1/peripherals", ps, "135790", ""); r.code != http.StatusOK || !strings.Contains(r.body, `"width":58`) {
		t.Fatalf("peripherals %d %s", r.code, r.body)
	}
	if r := e.do(t, http.MethodGet, "/api/desktop/v1/peripherals", "", "", ""); !strings.Contains(r.body, "escpos") {
		t.Fatalf("read back %s", r.body)
	}
	if e.rr.Peripherals().Receipt.Code() != "escpos" || e.rr.Peripherals().Reader != nil {
		t.Fatal("drivers not reloaded")
	}
	if r := e.do(t, http.MethodPut, "/api/desktop/v1/kiosk", `{"enabled":true}`, "135790", ""); r.code != http.StatusOK {
		t.Fatalf("kiosk %d", r.code)
	}
	e.rr.SetKioskActive(true)
	if r := e.do(t, http.MethodPost, "/api/desktop/v1/kiosk/exit", "", "000000", ""); r.code != http.StatusUnauthorized {
		t.Fatalf("kiosk exit with a wrong pin %d", r.code)
	}
	if r := e.do(t, http.MethodPost, "/api/desktop/v1/kiosk/exit", "", "135790", ""); r.code != http.StatusNoContent {
		t.Fatalf("kiosk exit %d %s", r.code, r.body)
	}
	if e.rr.Status(context.Background()).Kiosk {
		t.Fatal("still in kiosk")
	}

	// Dev offline switch.
	if r := e.do(t, http.MethodPost, "/api/desktop/v1/dev/remote-offline", `{"offline":true}`, "", ""); r.code != http.StatusNoContent {
		t.Fatalf("dev offline %d", r.code)
	}
	if r := e.do(t, http.MethodGet, "/api/admin/v1/meta", "", "", ""); r.code != http.StatusServiceUnavailable {
		t.Fatalf("offline api %d", r.code)
	}
	_ = e.do(t, http.MethodPost, "/api/desktop/v1/dev/remote-offline", `{"offline":false}`, "", "")

	// Unpair: revoke upstream, delete token, cache and remote settings, reset to the first run.
	if r := e.do(t, http.MethodDelete, "/api/desktop/v1/remote", "", "135790", ""); r.code != http.StatusAccepted {
		t.Fatalf("unpair %d %s", r.code, r.body)
	}
	select {
	case <-e.unpaired:
	default:
		t.Fatal("device not revoked upstream")
	}
	if revoked := <-e.resets; revoked {
		t.Fatal("unpair reported as revoked")
	}
	if e.rr.Token() != "" {
		t.Fatal("token kept")
	}
	s, err := LoadSettings(e.paths.Settings())
	if err != nil || s.Mode != desktopport.ModeLocal || s.Remote != nil || s.AppPinHash != "" {
		t.Fatalf("settings after unpair %+v %v", s, err)
	}
	if s.Peripherals.Receipt.Driver != "escpos" {
		t.Fatal("peripheral settings lost on unpair")
	}
}
