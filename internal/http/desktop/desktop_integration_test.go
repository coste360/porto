//go:build integration

package desktop_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/backup"
	"github.com/coste360/porto/internal/config"
	launcher "github.com/coste360/porto/internal/desktop"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: testdb hands the database to the test
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

type harness struct {
	app    *app.App
	rt     *launcher.Runtime
	srv    *httptest.Server
	secret string
	dir    string
	opened atomic.Value
}

func countUsers(ctx context.Context, a *app.App) (int, error) { return store.CountUsers(ctx, a.DB.DB) }

func newHarness(t *testing.T, db *store.DB, desktop bool) *harness {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	h := &harness{secret: base64.StdEncoding.EncodeToString(key)}
	dir := t.TempDir()
	h.dir = dir
	vars := map[string]string{
		"PORTO_SECRET_KEY": h.secret, "PORTO_BASE_URL": "http://127.0.0.1:18090", "PORTO_DATA_DIR": dir,
		"PORTO_ADDR": "127.0.0.1:18090",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	o := app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}}
	if desktop {
		h.rt = launcher.NewRuntime(launcher.RuntimeConfig{
			Version: "1.0.0", Paths: launcher.Paths{Root: dir}, Settings: launcher.DefaultSettings(), SecretKey: h.secret,
			SecretKind: desktopport.SecretStoreFile, CountUsers: countUsers,
			Opener: func(_ context.Context, target string) error { h.opened.Store(target); return nil },
		})
		o.Desktop, o.PairTarget = h.rt, h.rt.PairTarget
	}
	h.app, err = app.NewWithOptions(context.Background(), cfg, "1.0.0", ext.Extensions{}, slog.New(slog.DiscardHandler), o)
	if err != nil {
		t.Fatal(err)
	}
	if desktop {
		h.rt.Attach(h.app, nil, launcher.NewHeadlessUI(), "http://127.0.0.1:18090")
	}
	h.srv = httptest.NewUnstartedServer(nil)
	h.srv.Config = h.app.NewHTTPServer(reqctx.ListenerLoopback, nil)
	h.srv.Start()
	t.Cleanup(h.srv.Close)
	return h
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (h *harness) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: h.srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) csrf() string {
	u, _ := url.Parse(c.base)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == middleware.CSRFCookie {
			return ck.Value
		}
	}
	return ""
}

func (c *client) do(method, path string, body any, hdr ...string) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case io.Reader:
		r = b
	default:
		raw, _ := json.Marshal(b)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, ok := body.(io.Reader); ok {
		req.Header.Set("Content-Type", "application/gzip")
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set(middleware.CSRFHeader, c.csrf())
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (c *client) login(email string) {
	c.t.Helper()
	if st, body := c.do(http.MethodPost, "/api/admin/v1/auth/login", map[string]string{"email": email, "password": fixture.Password}); st != http.StatusOK {
		c.t.Fatalf("login %s: %d %v", email, st, body)
	}
}

func problemType(body map[string]any) string {
	s, _ := body["type"].(string)
	return s[strings.LastIndex(s, "/")+1:]
}

func TestDesktopAPI_OwnerOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		e.AddUser(t, "director@porto.test", auth.RoleDirector, e.OrgID, "")
		h := newHarness(t, db, true)

		anon := h.client(t)
		if st, body := anon.do(http.MethodGet, "/api/desktop/v1/status", nil); st != http.StatusOK ||
			body["first_run_pending"] != false || body["version"] != "1.0.0" || body["secret_store"] != "file" {
			t.Fatalf("status %d %v", st, body)
		}
		if st, _ := anon.do(http.MethodGet, "/api/desktop/v1/settings", nil); st != http.StatusUnauthorized {
			t.Fatalf("anonymous settings %d", st)
		}
		if st, body := anon.do(http.MethodPost, "/api/desktop/v1/activate", nil, launcher.InstanceTokenHeader, "nope"); st != http.StatusUnauthorized {
			t.Fatalf("activate %d %v", st, body)
		}

		dir := h.client(t)
		dir.login("director@porto.test")
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/api/desktop/v1/settings"},
			{http.MethodGet, "/api/desktop/v1/lan"},
			{http.MethodGet, "/api/desktop/v1/backups"},
			{http.MethodPost, "/api/desktop/v1/backups"},
		} {
			if st, body := dir.do(c.method, c.path, nil); st != http.StatusForbidden || problemType(body) != "forbidden" {
				t.Fatalf("director %s %s: %d %v", c.method, c.path, st, body)
			}
		}
		if st, _ := dir.do(http.MethodPut, "/api/desktop/v1/lan", map[string]any{"enabled": true}); st != http.StatusForbidden {
			t.Fatalf("director lan %d", st)
		}
		if st, _ := dir.do(http.MethodPost, "/api/desktop/v1/secret-key/reveal", map[string]string{"password": fixture.Password}); st != http.StatusForbidden {
			t.Fatalf("director reveal %d", st)
		}

		own := h.client(t)
		own.login("owner@porto.test")
		st, body := own.do(http.MethodGet, "/api/desktop/v1/settings", nil)
		if st != http.StatusOK || body["mode"] != "local" || body["backup_keep_daily"] != float64(14) {
			t.Fatalf("owner settings %d %v", st, body)
		}
		st, body = own.do(http.MethodPatch, "/api/desktop/v1/settings", map[string]any{"backup_keep_daily": 3, "update_check": false})
		if st != http.StatusOK || body["backup_keep_daily"] != float64(3) || body["update_check"] != false {
			t.Fatalf("patch %d %v", st, body)
		}
		if st, body := own.do(http.MethodPatch, "/api/desktop/v1/settings", map[string]any{"backup_keep_exit": 0}); st != http.StatusBadRequest || problemType(body) != "validation_failed" {
			t.Fatalf("bad patch %d %v", st, body)
		}
		if st, body := own.do(http.MethodGet, "/api/desktop/v1/lan", nil); st != http.StatusOK || body["listening"] != false {
			t.Fatalf("lan %d %v", st, body)
		}
		if st, body := own.do(http.MethodPost, "/api/desktop/v1/backups", nil); st != http.StatusCreated || body["kind"] != "manual" {
			t.Fatalf("backup %d %v", st, body)
		}
		if st, body := own.do(http.MethodGet, "/api/desktop/v1/backups", nil); st != http.StatusOK || len(body["items"].([]any)) != 1 {
			t.Fatalf("list %d %v", st, body)
		}
		if st, body := own.do(http.MethodPost, "/api/desktop/v1/backups/porto-20200101-000000-manual.tar.gz/restore", nil); st != http.StatusNotFound {
			t.Fatalf("restore unknown %d %v", st, body)
		}
		if st, _ := own.do(http.MethodPost, "/api/desktop/v1/open", map[string]string{"target": "data_dir"}); st != http.StatusNoContent || h.opened.Load() == nil {
			t.Fatalf("open %d", st)
		}
		if st, _ := own.do(http.MethodPost, "/api/desktop/v1/open", map[string]string{"target": "/etc"}); st != http.StatusBadRequest {
			t.Fatalf("open other %d", st)
		}
		if st, body := own.do(http.MethodPost, "/api/desktop/v1/secret-key/reveal", map[string]string{"password": "wrong password"}); st != http.StatusBadRequest || problemType(body) != "invalid_credentials" {
			t.Fatalf("reveal wrong %d %v", st, body)
		}
		st, body = own.do(http.MethodPost, "/api/desktop/v1/secret-key/reveal", map[string]string{"password": fixture.Password})
		if st != http.StatusOK || body["secret_key"] != h.secret {
			t.Fatalf("reveal %d", st)
		}
		for range 3 {
			own.do(http.MethodPost, "/api/desktop/v1/secret-key/reveal", map[string]string{"password": "x"})
		}
		if st, body := own.do(http.MethodPost, "/api/desktop/v1/secret-key/reveal", map[string]string{"password": fixture.Password}); st != http.StatusTooManyRequests {
			t.Fatalf("reveal limit %d %v", st, body)
		}

		var actions []string
		if err := db.NewSelect().Table("audit_log").Column("action").Where("action LIKE 'desktop.%'").Order("action").Scan(context.Background(), &actions); err != nil {
			t.Fatal(err)
		}
		if strings.Join(actions, ",") != "desktop.backup,desktop.secret_reveal,desktop.settings_update" {
			t.Fatalf("audit %v", actions)
		}
	})
}

func TestDesktopAPI_NilRuntime404(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fixture.New(t, db)
		h := newHarness(t, db, false)
		c := h.client(t)
		c.login("owner@porto.test")
		for _, p := range []string{"/api/desktop/v1/status", "/api/desktop/v1/settings", "/api/desktop/v1/backups"} {
			if st, body := c.do(http.MethodGet, p, nil); st != http.StatusNotFound || problemType(body) != "not_found" {
				t.Fatalf("%s: %d %v", p, st, body)
			}
		}
		if st, _ := c.do(http.MethodPost, "/api/desktop/v1/first-run", map[string]string{"token": "x"}); st != http.StatusNotFound {
			t.Fatalf("first run %d", st)
		}
	})
}

func TestConcurrency_FirstRunSingleOwner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db, true)
		tok, err := h.rt.MintFirstRunToken()
		if err != nil {
			t.Fatal(err)
		}
		if !h.rt.Status(context.Background()).FirstRunPending {
			t.Fatal("first run not pending")
		}
		const n = 100
		var ok, tokenErr, done atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := h.rt.FirstRun(fixture.Ctx(), desktopport.FirstRunInput{
					Token: tok, Organization: "Marina Org", Name: "Owner", Email: fmt.Sprintf("owner%d@porto.test", i),
					Password: fixture.Password,
				})
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, desktopport.ErrFirstRunToken):
					tokenErr.Add(1)
				case errors.Is(err, desktopport.ErrFirstRunDone):
					done.Add(1)
				default:
					t.Errorf("first run: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if ok.Load() != 1 || tokenErr.Load()+done.Load() != n-1 {
			t.Fatalf("ok=%d token=%d done=%d", ok.Load(), tokenErr.Load(), done.Load())
		}
		if c, _ := store.CountUsers(context.Background(), db.DB); c != 1 {
			t.Fatalf("%d users", c)
		}
		if h.rt.Status(context.Background()).FirstRunPending {
			t.Fatal("first run still pending")
		}
	})
}

func TestDesktopFirstRun_HTTP(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db, true)
		tok, _ := h.rt.MintFirstRunToken()
		c := h.client(t)
		in := map[string]any{"token": "wrong", "organization": "Marina Org", "name": "Ada Owner", "email": "ada@porto.test", "password": fixture.Password, "update_check": false}
		if st, body := c.do(http.MethodPost, "/api/desktop/v1/first-run", in); st != http.StatusBadRequest || problemType(body) != "first_run_token_invalid" {
			t.Fatalf("wrong token %d %v", st, body)
		}
		in["token"], in["password"] = tok, "short"
		if st, body := c.do(http.MethodPost, "/api/desktop/v1/first-run", in); st/100 != 4 || st == http.StatusBadRequest && problemType(body) == "first_run_token_invalid" {
			t.Fatalf("weak password %d %v", st, body)
		}
		in["password"] = fixture.Password // the token survived the validation error
		st, body := c.do(http.MethodPost, "/api/desktop/v1/first-run", in)
		if st != http.StatusCreated || body["user_id"] == "" {
			t.Fatalf("first run %d %v", st, body)
		}
		if st, body := c.do(http.MethodGet, "/api/desktop/v1/settings", nil); st != http.StatusOK || body["update_check"] != false {
			t.Fatalf("session after first run %d %v", st, body)
		}
		if st, body := c.do(http.MethodPost, "/api/desktop/v1/first-run", in); st != http.StatusConflict || problemType(body) != "first_run_done" {
			t.Fatalf("second first run %d %v", st, body)
		}
		if st, body := c.do(http.MethodPost, "/api/desktop/v1/first-run/restore", strings.NewReader("x"), "Porto-First-Run-Token", tok); st != http.StatusConflict || problemType(body) != "first_run_done" {
			t.Fatalf("first-run restore after use %d %v", st, body)
		}
	})
}

// archiveAtSchema returns a backup of an empty SQLite database migrated up to schema: what an older (or, crafted,
// newer) Porto would have produced.
func archiveAtSchema(t *testing.T, schema int64) []byte {
	t.Helper()
	ctx := context.Background()
	latest, err := migrations.Latest(migrations.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if schema > latest {
		// A future archive: only the manifest is read before it is refused.
		raw, _ := json.Marshal(backup.Manifest{Format: backup.Format, PortoVersion: "99.0.0", SchemaVersion: schema, Dialect: "sqlite"})
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(raw))})
		_, _ = tw.Write(raw)
		_ = tw.Close()
		_ = gz.Close()
		return buf.Bytes()
	}
	db, err := store.Open(ctx, store.Options{SQLitePath: filepath.Join(t.TempDir(), "old.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mig, err := db.Migrator()
	if err != nil {
		t.Fatal(err)
	}
	if err := mig.UpTo(ctx, schema); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m, err := backup.Backup(ctx, db, &buf, "0.9.0", "")
	if err != nil || m.SchemaVersion != schema {
		t.Fatalf("old backup %+v %v", m, err)
	}
	return buf.Bytes()
}

// stagedDB opens the database a restore prepared in the staging folder.
func stagedDB(t *testing.T, h *harness) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{SQLitePath: filepath.Join(h.dir, "restore-staging", "porto.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDesktopRestore_OlderSchemaAndAudit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db, true)
		ctx := context.Background()
		if _, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "ada@porto.test", Name: "Ada Owner", Password: fixture.Password, Organization: "Marina Org",
		}); err != nil {
			t.Fatal(err)
		}
		c := h.client(t)
		c.login("ada@porto.test")
		latest, _ := migrations.Latest(migrations.SQLite)

		// An archive from a newer Porto is refused with its own problem type, before anything is prepared.
		st, body := c.do(http.MethodPost, "/api/desktop/v1/restore-upload", bytes.NewReader(archiveAtSchema(t, latest+1)))
		if st != http.StatusUnprocessableEntity || problemType(body) != "restore_backup_newer" {
			t.Fatalf("newer archive %d %v", st, body)
		}

		// An archive taken before the last migration (an app update since) restores and ends on the current schema.
		st, body = c.do(http.MethodPost, "/api/desktop/v1/restore-upload", bytes.NewReader(archiveAtSchema(t, latest-1)))
		if st != http.StatusAccepted {
			t.Fatalf("older archive %d %v", st, body)
		}
		staged := stagedDB(t, h)
		mig, _ := staged.Migrator()
		if v, err := mig.Version(ctx); err != nil || v != latest {
			t.Fatalf("staged schema %d %v, want %d", v, err, latest)
		}
		// The restored database records its own restore (INV-72); the live one records that it was replaced.
		var n int
		if err := staged.NewRaw("SELECT COUNT(*) FROM audit_log WHERE action = 'desktop.restore'").Scan(ctx, &n); err != nil || n != 1 {
			t.Fatalf("desktop.restore rows in the restored database: %d %v", n, err)
		}
		if err := db.NewRaw("SELECT COUNT(*) FROM audit_log WHERE action = 'desktop.restore'").Scan(ctx, &n); err != nil || n != 1 {
			t.Fatalf("desktop.restore rows in the live database: %d %v", n, err)
		}
	})
}

func TestDesktopFirstRunRestore_TokenAndOwner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		latest, _ := migrations.Latest(migrations.SQLite)
		good := archiveAtSchema(t, latest)

		h := newHarness(t, db, true)
		tok, _ := h.rt.MintFirstRunToken()
		c := h.client(t)
		path := "/api/desktop/v1/first-run/restore"
		// A corrupt archive fails and leaves the token usable.
		if st, body := c.do(http.MethodPost, path, strings.NewReader("not a backup"), "Porto-First-Run-Token", tok); st != http.StatusUnprocessableEntity || problemType(body) != "restore_failed" {
			t.Fatalf("corrupt archive %d %v", st, body)
		}
		if !h.rt.Status(ctx).FirstRunPending {
			t.Fatal("a failed restore burnt the first-run token")
		}
		// An owner created meanwhile (CLI, bootstrap) closes the first run: no restore without authentication.
		if _, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "cli@porto.test", Name: "Cli Owner", Password: fixture.Password, Organization: "Marina Org",
		}); err != nil {
			t.Fatal(err)
		}
		if st, body := c.do(http.MethodPost, path, bytes.NewReader(good), "Porto-First-Run-Token", tok); st != http.StatusConflict || problemType(body) != "first_run_done" {
			t.Fatalf("first-run restore with an owner %d %v", st, body)
		}
	})
}

func TestDesktopFirstRunRestore_SingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		latest, _ := migrations.Latest(migrations.SQLite)
		good := archiveAtSchema(t, latest)
		h := newHarness(t, db, true)
		tok, _ := h.rt.MintFirstRunToken()
		c := h.client(t)
		path := "/api/desktop/v1/first-run/restore"
		if st, body := c.do(http.MethodPost, path, bytes.NewReader(good), "Porto-First-Run-Token", "wrong"); st != http.StatusBadRequest || problemType(body) != "first_run_token_invalid" {
			t.Fatalf("wrong token %d %v", st, body)
		}
		if st, body := c.do(http.MethodPost, path, bytes.NewReader(good), "Porto-First-Run-Token", tok); st != http.StatusAccepted {
			t.Fatalf("first-run restore %d %v", st, body)
		}
		if st, body := c.do(http.MethodPost, path, bytes.NewReader(good), "Porto-First-Run-Token", tok); st != http.StatusBadRequest || problemType(body) != "first_run_token_invalid" {
			t.Fatalf("token reused %d %v", st, body)
		}
	})
}
