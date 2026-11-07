//go:build integration

package desktop

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/lan"
	"github.com/coste360/porto/internal/reqctx"
)

const testPassword = "launcher password 2026"

// testCountUsers counts users through the App's database handle (this package never imports the store).
func testCountUsers(ctx context.Context, a *app.App) (int, error) {
	n, err := a.DB.NewSelect().Table("users").Count(ctx)
	return int(n), err
}

type running struct {
	t    *testing.T
	dir  string
	url  string
	ui   *recordUI
	done chan error
	http *http.Client
}

func startLauncher(t *testing.T, dir string, extra map[string]string) *running {
	t.Helper()
	return startLauncherWith(t, dir, extra, nil)
}

func startLauncherWith(t *testing.T, dir string, extra map[string]string, newLAN LANFactory) *running {
	t.Helper()
	env := map[string]string{"PORTO_DESKTOP_KEYRING": "file", "PORTO_UPDATE_CHECK": "false", "PORTO_LOG_LEVEL": "debug"}
	for k, v := range extra {
		env[k] = v
	}
	r := &running{t: t, dir: dir, ui: &recordUI{HeadlessUI: NewHeadlessUI()}, done: make(chan error, 1)}
	ready := make(chan string, 1)
	go func() {
		r.done <- Run(context.Background(), Options{
			Version: "1.0.0", DataDir: dir, Headless: true, GOOS: "linux", GOARCH: "amd64",
			LookupEnv: lookupOf(env), UI: r.ui, CountUsers: testCountUsers, Argon2: auth.TestArgon2Params,
			Opener: func(context.Context, string) error { return nil }, NewLAN: newLAN,
			OnReady: func(u string) { ready <- u },
		})
	}()
	select {
	case r.url = <-ready:
	case err := <-r.done:
		t.Fatalf("launcher exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("launcher not ready")
	}
	jar, _ := cookiejar.New(nil)
	r.http = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	return r
}

func (r *running) stop() {
	r.t.Helper()
	r.ui.Quit()
	select {
	case err := <-r.done:
		if err != nil {
			r.t.Fatalf("launcher stop: %v", err)
		}
	case <-time.After(90 * time.Second):
		r.t.Fatal("launcher did not stop")
	}
}

func (r *running) origin() string {
	u, _ := url.Parse(r.url)
	return u.Scheme + "://" + u.Host
}

func (r *running) do(method, path string, body any, hdr ...string) (int, map[string]any) {
	r.t.Helper()
	var rd io.Reader = http.NoBody
	ctype := ""
	switch b := body.(type) {
	case nil:
	case io.Reader:
		rd, ctype = b, "application/gzip"
	default:
		raw, _ := json.Marshal(b)
		rd, ctype = bytes.NewReader(raw), "application/json"
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, r.origin()+path, rd)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	u, _ := url.Parse(r.origin())
	for _, c := range r.http.Jar.Cookies(u) {
		if c.Name == "porto_csrf" {
			req.Header.Set("X-CSRF-Token", c.Value)
		}
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := r.http.Do(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (r *running) firstRun() {
	r.t.Helper()
	_, frag, ok := strings.Cut(r.url, "#t=")
	if !ok {
		r.t.Fatalf("no first-run token in %q", r.url)
	}
	st, body := r.do(http.MethodPost, "/api/desktop/v1/first-run", map[string]any{
		"token": frag, "organization": "Marina Org", "name": "Ada Owner", "email": "ada@porto.test",
		"password": testPassword, "update_check": false,
	})
	if st != http.StatusCreated {
		r.t.Fatalf("first run %d %v", st, body)
	}
}

func (r *running) waitReady() {
	r.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, r.origin()+"/readyz", http.NoBody)
		if res, err := http.DefaultClient.Do(req); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.t.Fatal("server not ready")
}

func TestLauncher_StartStop(t *testing.T) {
	dir := t.TempDir()
	r := startLauncher(t, dir, map[string]string{"PORTO_DEV": "true", "PORTO_DESKTOP_FIRST_RUN_FILE": "true"})
	if !strings.Contains(r.url, "/admin/desktop/first-run#t=") || !strings.HasPrefix(r.url, "http://127.0.0.1:") {
		t.Fatalf("start url %q", r.url)
	}
	r.waitReady()
	if b, err := os.ReadFile(filepath.Join(dir, "first-run.url")); err != nil || strings.TrimSpace(string(b)) != r.url {
		t.Fatalf("first-run.url %q %v", b, err)
	}
	if st, body := r.do(http.MethodGet, "/api/desktop/v1/status", nil); st != http.StatusOK || body["first_run_pending"] != true || body["secret_store"] != "file" {
		t.Fatalf("status %d %v", st, body)
	}
	// DNS rebinding: another Host is refused on the loopback listener.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, r.origin()+"/api/desktop/v1/status", http.NoBody)
	req.Host = "evil.example:80"
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("host check: %v %v", res, err)
	} else {
		_ = res.Body.Close()
	}
	if st, _ := os.Stat(filepath.Join(dir, "secret.key")); st == nil || st.Mode().Perm() != 0o600 {
		t.Fatal("secret file fallback missing or not 0600")
	}

	r.firstRun()
	if _, err := os.Stat(filepath.Join(dir, "first-run.url")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("first-run.url not deleted once consumed")
	}
	if st, body := r.do(http.MethodGet, "/api/desktop/v1/settings", nil); st != http.StatusOK || body["data_dir"] != dir {
		t.Fatalf("settings %d %v", st, body)
	}
	if st, body := r.do(http.MethodPut, "/api/desktop/v1/lan", map[string]any{"enabled": true}); st != http.StatusConflict {
		t.Logf("lan enable without a server: %d %v", st, body) // LAN not wired in this test: lan_no_address
	}

	// A second launch on the same folder activates the running one and exits.
	second := make(chan error, 1)
	go func() {
		second <- Run(context.Background(), Options{
			Version: "1.0.0", DataDir: dir, Headless: true, LookupEnv: lookupOf(map[string]string{"PORTO_DESKTOP_KEYRING": "file"}),
			CountUsers: testCountUsers,
		})
	}()
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("second launch: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("second launch did not return")
	}
	if r.ui.focus.Load() != 1 {
		t.Fatal("the running window was not brought to the front")
	}

	r.stop()
	ents, _ := os.ReadDir(filepath.Join(dir, "backups"))
	exit := 0
	for _, e := range ents {
		if _, kind, ok := ParseBackupName(e.Name(), time.Local); ok && kind == desktopport.BackupExit {
			exit++
		}
	}
	if exit != 1 {
		t.Fatalf("exit backups: %d (%v)", exit, ents)
	}
	lock, err := AcquireLock(Paths{Root: dir}.Lock())
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	_ = lock.Release()
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("instance.json left behind")
	}
	logs, _ := os.ReadFile(filepath.Join(dir, "logs", LogFileName))
	_, tok, _ := strings.Cut(r.url, "#t=")
	if len(logs) == 0 || strings.Contains(string(logs), tok) || strings.Contains(string(logs), "ada@porto.test") ||
		strings.Contains(string(logs), testPassword) {
		t.Fatal("log missing or leaking the token, email or password")
	}

	// Restart: no first run any more, the panel home opens.
	r2 := startLauncher(t, dir, nil)
	if !strings.HasSuffix(r2.url, "/admin/") {
		t.Fatalf("restart url %q", r2.url)
	}
	r2.stop()
}

func TestLauncher_RestoreSwapsDatabase(t *testing.T) {
	dir := t.TempDir()
	r := startLauncher(t, dir, nil)
	r.waitReady()
	r.firstRun()
	st, body := r.doRetryBusy(http.MethodPost, "/api/desktop/v1/backups")
	if st != http.StatusCreated {
		t.Fatalf("backup %d %v", st, body)
	}
	backupID, _ := body["id"].(string)
	// A change after the backup: the restore must undo it.
	if st, body := r.do(http.MethodPatch, "/api/admin/v1/auth/me", map[string]any{"name": "Changed Name"}); st != http.StatusOK {
		t.Fatalf("update me %d %v", st, body)
	}

	// A corrupt upload fails and leaves the live database untouched.
	if st, body := r.do(http.MethodPost, "/api/desktop/v1/restore-upload", strings.NewReader("not a backup")); st != http.StatusUnprocessableEntity || !strings.HasSuffix(body["type"].(string), "restore_failed") {
		t.Fatalf("corrupt restore %d %v", st, body)
	}
	if st, body := r.do(http.MethodGet, "/api/admin/v1/auth/me", nil); st != http.StatusOK || !strings.Contains(jsonString(body), "Changed Name") {
		t.Fatalf("live database changed by a failed restore: %d %v", st, body)
	}

	if st, body := r.do(http.MethodPost, "/api/desktop/v1/backups/"+backupID+"/restore", nil); st != http.StatusAccepted {
		t.Fatalf("restore %d %v", st, body)
	}
	// The server restarts in place: same port, the window is reloaded on the panel.
	deadline := time.Now().Add(30 * time.Second)
	for r.ui.navigate.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if nav, _ := r.ui.navigate.Load().(string); !strings.HasSuffix(nav, "/admin/") {
		t.Fatalf("window not reloaded: %q", nav)
	}
	r.waitReady()
	if st, _ := r.do(http.MethodGet, "/api/admin/v1/auth/me", nil); st != http.StatusUnauthorized {
		t.Fatalf("sessions survived the restore: %d", st)
	}
	if st, body := r.do(http.MethodPost, "/api/admin/v1/auth/login", map[string]string{"email": "ada@porto.test", "password": testPassword}); st != http.StatusOK || strings.Contains(jsonString(body), "Changed Name") {
		t.Fatalf("login after restore %d %v", st, body)
	}

	ents, _ := os.ReadDir(dir)
	var before bool
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "porto.db.before-") {
			before = true
		}
		if e.Name() == "restore-staging" || strings.HasPrefix(e.Name(), "restore-2") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
	if !before {
		t.Fatal("the previous database was not kept aside")
	}
	files, _ := os.ReadDir(filepath.Join(dir, "backups"))
	pre := 0
	for _, f := range files {
		if _, kind, ok := ParseBackupName(f.Name(), time.Local); ok && kind == desktopport.BackupPreRestore {
			pre++
		}
	}
	if pre != 2 { // one before the failed upload, one before the real restore
		t.Fatalf("pre-restore backups: %d", pre)
	}
	r.stop()
}

// doRetryBusy repeats a request while the scheduler's start-up daily backup holds the backup slot.
func (r *running) doRetryBusy(method, path string) (int, map[string]any) {
	r.t.Helper()
	for range 100 {
		st, body := r.do(method, path, nil)
		if st != http.StatusConflict {
			return st, body
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.t.Fatalf("%s %s: backup slot always busy", method, path)
	return 0, nil
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestLauncher_LANSwitchPersists(t *testing.T) {
	dir := t.TempDir()
	lp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lanPort := strconv.Itoa(lp.Addr().(*net.TCPAddr).Port)
	_ = lp.Close()
	newLAN := func(d LANDeps) (LAN, error) {
		return lan.New(lan.Options{
			Dir: d.Dir, Keys: d.Keys, Clock: d.Clock, Log: d.Log, Port: d.Port, Address: d.Address, Loopback: d.Loopback,
			FallbackOrigin: d.Origin,
			NewServer:      func(hosts []string) *http.Server { return d.App.NewHTTPServer(reqctx.ListenerLAN, hosts) },
		}), nil
	}
	env := map[string]string{"PORTO_DEV": "true", "PORTO_DESKTOP_LAN_LOOPBACK": "true", "PORTO_DESKTOP_LAN_PORT": lanPort}
	r := startLauncherWith(t, dir, env, newLAN)
	r.waitReady()
	r.firstRun()
	st, body := r.do(http.MethodPut, "/api/desktop/v1/lan", map[string]any{"enabled": true})
	if st != http.StatusOK || body["listening"] != true || !strings.HasPrefix(body["url"].(string), "https://127.0.0.1:"+lanPort) {
		t.Fatalf("lan enable %d %v", st, body)
	}
	if s, _ := LoadSettings(Paths{Root: dir}.Settings()); !s.LANEnabled {
		t.Fatal("lan_enabled not persisted")
	}
	// The desktop API is not reachable over the local network, even from a loopback peer.
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // self-signed test certificate
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, body["url"].(string)+"/api/desktop/v1/status", http.NoBody)
	if res, err := (&http.Client{Transport: tr}).Do(req); err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("desktop API over LAN: %v %v", res, err)
	} else {
		_ = res.Body.Close()
	}
	r.stop()

	// A restart turns the local network back on by itself.
	r2 := startLauncherWith(t, dir, env, newLAN)
	r2.waitReady()
	conn, err := tls.Dial("tcp", "127.0.0.1:"+lanPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test
	if err != nil {
		t.Fatalf("lan not restarted: %v", err)
	}
	_ = conn.Close()
	r2.stop()
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+lanPort, time.Second); err == nil {
		_ = c.Close()
		t.Fatal("lan listener still open after stop")
	}
}
