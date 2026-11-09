package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func lookupOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDataDir_PerOS(t *testing.T) {
	cases := []struct {
		name, goos string
		env        map[string]string
		home, want string
		err        bool
	}{
		{"darwin", "darwin", nil, "/Users/ann", "/Users/ann/Library/Application Support/Porto", false},
		{"windows localappdata", "windows", map[string]string{"LOCALAPPDATA": `C:\Users\ann\AppData\Local`}, `C:\Users\ann`, `C:\Users\ann\AppData\Local\Porto`, false},
		{"windows no localappdata", "windows", nil, `C:\Users\ann`, `C:\Users\ann\AppData\Local\Porto`, false},
		{"linux xdg", "linux", map[string]string{"XDG_DATA_HOME": "/data/x"}, "/home/ann", "/data/x/porto", false},
		{"linux relative xdg ignored", "linux", map[string]string{"XDG_DATA_HOME": "rel"}, "/home/ann", "/home/ann/.local/share/porto", false},
		{"linux default", "linux", nil, "/home/ann", "/home/ann/.local/share/porto", false},
		{"override", "darwin", map[string]string{"PORTO_DATA_DIR": "/srv/porto/"}, "/Users/ann", "/srv/porto", false},
		{"no home", "darwin", nil, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DataDir(c.goos, envOf(c.env), c.home)
			if c.err {
				if !errors.Is(err, ErrNoDataDir) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("DataDir = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestDesktopConfig_Load(t *testing.T) {
	e, err := LoadEnv(lookupOf(map[string]string{"PORTO_UPDATE_CHECK": "false", "PORTO_DESKTOP_PORT": "18090", "PORTO_DESKTOP_KEYRING": "file"}))
	if err != nil {
		t.Fatal(err)
	}
	if !e.UpdateCheckOff || e.Port != 18090 || e.Keyring != KeyringFile || e.LANLoopback {
		t.Fatalf("env %+v", e)
	}
	e, err = LoadEnv(lookupOf(nil))
	if err != nil || e.UpdateCheckOff || e.Keyring != KeyringAuto {
		t.Fatalf("defaults %+v %v", e, err)
	}

	// Dev-only variables are refused outside dev; errors name the variable, never the value.
	secretish := "https://secret-host.example/x?token=s3cr3t"
	_, err = LoadEnv(lookupOf(map[string]string{
		"PORTO_UPDATE_URL": secretish, "PORTO_DESKTOP_LAN_LOOPBACK": "true", "PORTO_DESKTOP_FIRST_RUN_FILE": "true",
		"PORTO_DESKTOP_LAN_PORT": "99999", "PORTO_DESKTOP_KEYRING": "vault-s3cr3t",
	}))
	if err == nil {
		t.Fatal("dev-only variables accepted outside dev")
	}
	for _, name := range []string{"PORTO_UPDATE_URL", "PORTO_DESKTOP_LAN_LOOPBACK", "PORTO_DESKTOP_FIRST_RUN_FILE", "PORTO_DESKTOP_LAN_PORT", "PORTO_DESKTOP_KEYRING"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name %s: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "99999") {
		t.Fatalf("error echoes a value: %v", err)
	}

	e, err = LoadEnv(lookupOf(map[string]string{
		"PORTO_DEV": "true", "PORTO_UPDATE_URL": "http://127.0.0.1:9/latest.json", "PORTO_DESKTOP_LAN_LOOPBACK": "true",
		"PORTO_DESKTOP_FIRST_RUN_FILE": "true",
	}))
	if err != nil || !e.LANLoopback || !e.FirstRunFile || e.UpdateURL == "" {
		t.Fatalf("dev %+v %v", e, err)
	}
}

func TestServerEnv_Build(t *testing.T) {
	proc := map[string]string{
		"PORTO_LOG_LEVEL": "debug", "DATABASE_URL": "postgres://x/y", "PORTO_ADDR": ":80", "PORTO_SECRET_KEY": "other",
		"PORTO_BOOTSTRAP_OWNER_EMAIL": "a@b.it",
	}
	get := ServerEnv(ServerEnvInput{Port: 4321, DataDir: "/d", SecretKey: "k", MailDir: "/d/mail", Lookup: lookupOf(proc)})
	want := map[string]string{
		"PORTO_ADDR": "127.0.0.1:4321", "PORTO_BASE_URL": "http://127.0.0.1:4321", "PORTO_DATA_DIR": "/d",
		"PORTO_SECRET_KEY": "k", "PORTO_AUTO_MIGRATE": "true", "PORTO_MAIL_DIR": "/d/mail", "PORTO_LOG_LEVEL": "debug",
		"PORTO_BOOTSTRAP_OWNER_EMAIL": "a@b.it",
	}
	for k, v := range want {
		if got, ok := get(k); !ok || got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, ok := get("DATABASE_URL"); ok {
		t.Error("DATABASE_URL passed through (desktop is SQLite only)")
	}
	withSMTP := ServerEnv(ServerEnvInput{Port: 1, MailDir: "/m", Lookup: lookupOf(map[string]string{"PORTO_SMTP_URL": "smtp://h"})})
	if _, ok := withSMTP("PORTO_MAIL_DIR"); ok {
		t.Error("file mail transport set although PORTO_SMTP_URL is set")
	}
}

func TestSettings_RoundTripPreservesUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"mode":"local","port":5000,"future_flag":{"a":1},"backup_keep_daily":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 5000 || s.BackupKeepDaily != 7 || s.BackupKeepExit != DefaultBackupKeepExit || s.LANPort != DefaultLANPort || !s.UpdateCheck {
		t.Fatalf("settings %+v", s)
	}
	s.LANEnabled = true
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(m["future_flag"]) != "map[a:1]" || m["lan_enabled"] != true {
		t.Fatalf("written %s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	missing, err := LoadSettings(filepath.Join(dir, "none.json"))
	if err != nil || missing.Mode != desktopport.ModeLocal {
		t.Fatalf("defaults %+v %v", missing, err)
	}
}

func TestSettings_RejectsRemote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	_ = os.WriteFile(path, []byte(`{"mode":"remote"}`), 0o600)
	_, err := LoadSettings(path)
	var se *desktopport.SettingsError
	if !errors.As(err, &se) || !slices.Contains(se.Fields, "mode") || !errors.Is(err, desktopport.ErrInvalidSettings) {
		t.Fatalf("err = %v", err)
	}
	s := DefaultSettings()
	bad := 0
	rel := "relative/dir"
	if _, _, err := s.Apply(desktopport.SettingsPatch{BackupKeepDaily: &bad, BackupDir: &rel}); !errors.As(err, &se) ||
		!slices.Contains(se.Fields, "backup_keep_daily") || !slices.Contains(se.Fields, "backup_dir") {
		t.Fatalf("apply err = %v", err)
	}
}

type memStore struct {
	mu   sync.Mutex
	v    string
	fail bool
}

func (m *memStore) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return "", errors.New("keychain locked")
	}
	if m.v == "" {
		return "", ErrSecretNotFound
	}
	return m.v, nil
}

func (m *memStore) Set(v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("keychain locked")
	}
	m.v = v
	return nil
}

func (m *memStore) Kind() string { return desktopport.SecretStoreKeyring }

func genKey(t *testing.T) string {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSecret_ResolveOrder(t *testing.T) {
	dir := t.TempDir()
	file := FileStore{Path: filepath.Join(dir, "secret.key")}
	envKey, krKey, fileKey := genKey(t), genKey(t), genKey(t)

	kr := &memStore{v: krKey}
	_ = file.Set(fileKey)
	if r, err := ResolveSecretKey(envKey, kr, file, true); err != nil || r.Key != envKey {
		t.Fatalf("env first: %+v %v", r, err)
	}
	if r, err := ResolveSecretKey("", kr, file, true); err != nil || r.Key != krKey || r.Kind != desktopport.SecretStoreKeyring {
		t.Fatalf("keychain second: %+v %v", r, err)
	}
	// Only in the file: used and moved into the keychain, the file is removed after the read back.
	kr = &memStore{}
	r, err := ResolveSecretKey("", kr, file, true)
	if err != nil || r.Key != fileKey || r.Kind != desktopport.SecretStoreKeyring || kr.v != fileKey {
		t.Fatalf("file migrated: %+v %v", r, err)
	}
	if _, err := os.Stat(file.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file kept after migration")
	}
	// Keychain unavailable: the file stays and is used.
	_ = file.Set(fileKey)
	r, err = ResolveSecretKey("", &memStore{fail: true}, file, true)
	if err != nil || r.Key != fileKey || r.Kind != desktopport.SecretStoreFile {
		t.Fatalf("locked keychain: %+v %v", r, err)
	}
	// New install without keychain: generated into the file, 0600.
	file2 := FileStore{Path: filepath.Join(dir, "secret2.key")}
	r, err = ResolveSecretKey("", nil, file2, false)
	if err != nil || !r.Generated || r.Kind != desktopport.SecretStoreFile {
		t.Fatalf("generated: %+v %v", r, err)
	}
	if st, _ := os.Stat(file2.Path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if _, err := ResolveSecretKey("not-a-key", nil, file2, false); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("invalid env key: %v", err)
	}

	// The real keychain store over go-keyring's in-memory provider.
	keyring.MockInit()
	ks := NewKeyringStore(dir)
	if !strings.HasPrefix(ks.Account, "secret-key/") || len(ks.Account) != len("secret-key/")+12 {
		t.Fatalf("account %q", ks.Account)
	}
	if _, err := ks.Get(); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("empty keychain: %v", err)
	}
	r, err = ResolveSecretKey("", ks, FileStore{Path: filepath.Join(dir, "none.key")}, false)
	if err != nil || r.Kind != desktopport.SecretStoreKeyring {
		t.Fatalf("keychain generate: %+v %v", r, err)
	}
	if v, _ := ks.Get(); v != r.Key {
		t.Fatal("generated key not in the keychain")
	}
	if NewKeyringStore(t.TempDir()).Account == ks.Account {
		t.Fatal("two data folders share a keychain account")
	}
}

func TestSecret_NeverRegeneratesForExistingDB(t *testing.T) {
	dir := t.TempDir()
	kr := &memStore{}
	_, err := ResolveSecretKey("", kr, FileStore{Path: filepath.Join(dir, "secret.key")}, true)
	if !errors.Is(err, ErrSecretMissing) {
		t.Fatalf("err = %v", err)
	}
	if kr.v != "" {
		t.Fatal("a key was generated for an existing database")
	}
	if _, err := os.Stat(filepath.Join(dir, "secret.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a key file was written for an existing database")
	}
}

func TestInstanceLock_SecondLaunchActivates(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	first, err := AcquireLock(p.Lock())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(p.Lock()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second lock: %v", err)
	}

	var got atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/desktop/v1/activate" {
			http.NotFound(w, r)
			return
		}
		got.Store(r.Header.Get(InstanceTokenHeader))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	hash, err := WriteInstance(p, os.Getpid(), port)
	if err != nil {
		t.Fatal(err)
	}
	if err := ActivateRunning(context.Background(), p, srv.Client()); err != nil {
		t.Fatal(err)
	}
	tok, _ := got.Load().(string)
	if tok == "" || crypto.SHA256Hex(tok) != hash {
		t.Fatal("activation did not carry the instance token")
	}
	b, _ := os.ReadFile(p.Instance())
	if strings.Contains(string(b), tok) {
		t.Fatal("instance.json contains the raw token")
	}
	for _, f := range []string{p.Instance(), p.InstanceToken()} {
		if st, _ := os.Stat(f); st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", f, st.Mode())
		}
	}

	// A runtime accepts only the right token and then focuses the UI.
	rt := NewRuntime(RuntimeConfig{InstanceTok: hash})
	ui := &recordUI{HeadlessUI: NewHeadlessUI()}
	rt.Attach(nil, nil, ui, "")
	if err := rt.Activate(context.Background(), "wrong"); !errors.Is(err, desktopport.ErrInstanceToken) {
		t.Fatalf("wrong token: %v", err)
	}
	if err := rt.Activate(context.Background(), tok); err != nil || ui.focus.Load() != 1 {
		t.Fatalf("activate: %v focus=%d", err, ui.focus.Load())
	}

	// The OS releases the lock: after Release a new launch takes it.
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := AcquireLock(p.Lock())
	if err != nil {
		t.Fatalf("relock: %v", err)
	}
	_ = again.Release()
}

type recordUI struct {
	*HeadlessUI
	focus    atomic.Int32
	navigate atomic.Value
}

func (r *recordUI) Focus()            { r.focus.Add(1) }
func (r *recordUI) Navigate(u string) { r.navigate.Store(u) }

func TestFirstRunToken_SingleUse(t *testing.T) {
	var tk firstRunToken
	if _, ok := tk.begin(""); tk.pending() || ok {
		t.Fatal("token before mint")
	}
	tok, err := tk.mint()
	if err != nil || len(tok) < 40 {
		t.Fatalf("mint %q %v", tok, err)
	}
	if _, ok := tk.begin("wrong"); !tk.pending() || ok {
		t.Fatal("wrong token accepted")
	}
	finish, ok := tk.begin(tok)
	if !ok {
		t.Fatal("begin")
	}
	if _, ok := tk.begin(tok); ok {
		t.Fatal("two attempts at once")
	}
	finish(false) // validation error: still usable
	finish, ok = tk.begin(tok)
	if !ok {
		t.Fatal("token not usable after a failed attempt")
	}
	finish(true)
	if _, ok := tk.begin(tok); ok || tk.pending() {
		t.Fatal("token used twice")
	}
	if _, ok := tk.begin(tok); ok {
		t.Fatal("begin after use")
	}
}

func TestBackupName_ParseFormat(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	at := time.Date(2026, 7, 1, 21, 5, 9, 0, time.UTC)
	for _, kind := range backupKinds {
		name := BackupName(at, loc, kind)
		if kind == desktopport.BackupDaily && name != "porto-20260701-220509-daily.tar.gz" {
			t.Fatalf("name %q", name)
		}
		got, k, ok := ParseBackupName(name, loc)
		if !ok || k != kind || !got.Equal(at) {
			t.Fatalf("parse %q = %v %q %v", name, got, k, ok)
		}
	}
	for _, bad := range []string{
		"porto-20260701-220509-weekly.tar.gz", "porto-20260701-220509-daily.tar.gz.part", "../porto-20260701-220509-daily.tar.gz",
		"x-porto-20260701-220509-daily.tar.gz", "porto-20261301-220509-daily.tar.gz", "notes.txt",
	} {
		if _, _, ok := ParseBackupName(bad, loc); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBackup_PruneCandidates(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 60).Draw(t, "n")
		keepDaily := rapid.IntRange(1, 20).Draw(t, "keepDaily")
		keepExit := rapid.IntRange(1, 10).Draw(t, "keepExit")
		var files []backupEntry
		seen := map[string]bool{}
		for i := 0; i < n; i++ {
			kind := rapid.SampledFrom(backupKinds).Draw(t, "kind")
			at := base.Add(time.Duration(rapid.IntRange(0, 100000).Draw(t, "min")) * time.Minute)
			name := BackupName(at, time.UTC, kind)
			if seen[name] {
				continue
			}
			seen[name] = true
			files = append(files, backupEntry{
				BackupFile: desktopport.BackupFile{ID: name, Kind: kind, CreatedAt: at},
				Valid:      rapid.IntRange(0, 9).Draw(t, "valid") > 0,
			})
		}
		out := PruneCandidates(files, keepDaily, keepExit)
		del := map[string]bool{}
		for _, id := range out {
			del[id] = true
		}
		var newest *backupEntry
		count := map[string]int{}
		kept := map[string]int{}
		for i := range files {
			f := &files[i]
			count[f.Kind]++
			if !del[f.ID] {
				kept[f.Kind]++
			}
			if f.Valid && (newest == nil || f.CreatedAt.After(newest.CreatedAt) || (f.CreatedAt.Equal(newest.CreatedAt) && f.ID > newest.ID)) {
				newest = f
			}
			if del[f.ID] && (f.Kind == desktopport.BackupManual || f.Kind == desktopport.BackupPreRestore) {
				t.Fatalf("pruned %s", f.ID)
			}
		}
		if newest != nil && del[newest.ID] {
			t.Fatalf("pruned the newest valid backup %s", newest.ID)
		}
		for kind, keep := range map[string]int{desktopport.BackupDaily: keepDaily, desktopport.BackupExit: keepExit} {
			var of []backupEntry
			for _, f := range files {
				if f.Kind == kind {
					of = append(of, f)
				}
			}
			sortNewestFirst(of)
			want := min(keep, len(of))
			// The newest valid backup is kept even when newer invalid files of its kind fill the N slots.
			if newest != nil && newest.Kind == kind && !slices.ContainsFunc(of[:want], func(f backupEntry) bool { return f.ID == newest.ID }) {
				want++
			}
			if kept[kind] != want {
				t.Fatalf("%s: kept %d of %d, want %d", kind, kept[kind], count[kind], want)
			}
		}
	})
}

func newTestBackups(t *testing.T, clk clock.Clock, settings *FileSettings) *Backups {
	t.Helper()
	return &Backups{Paths: Paths{Root: t.TempDir()}, Clock: clk, Loc: time.UTC, Settings: func() FileSettings { return *settings }}
}

func fakeSnapshot(calls *atomic.Int32) Snapshot {
	return func(_ context.Context, w io.Writer) error {
		calls.Add(1)
		_, err := w.Write([]byte("archive"))
		return err
	}
}

func TestBackupScheduler_DailyDueAcrossRestarts(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	s := DefaultSettings()
	b := newTestBackups(t, clk, &s)
	var calls atomic.Int32
	snap := fakeSnapshot(&calls)
	ctx := context.Background()

	b.Tick(ctx, snap)
	if calls.Load() != 1 {
		t.Fatalf("first start: %d backups", calls.Load())
	}
	clk.Advance(time.Hour)
	// A restart reads the folder: the daily backup of an hour ago is still fresh.
	b2 := &Backups{Paths: b.Paths, Clock: clk, Loc: time.UTC, Settings: b.Settings}
	b2.Tick(ctx, snap)
	if calls.Load() != 1 {
		t.Fatal("daily backup repeated after a restart")
	}
	clk.Advance(23 * time.Hour) // asleep: 24 h since the last daily
	b2.Tick(ctx, snap)
	if calls.Load() != 2 {
		t.Fatalf("due after 24 h: %d", calls.Load())
	}
	files, st, err := b2.List()
	if err != nil || len(files) != 2 || st.LastDailyAt.IsZero() || !st.NextDueAt.Equal(st.LastDailyAt.Add(DailyInterval)) {
		t.Fatalf("list %+v %+v %v", files, st, err)
	}
	if files[0].SHA256 == "" || files[0].SizeBytes != int64(len("archive")) {
		t.Fatalf("file %+v", files[0])
	}

	// A failing snapshot leaves no file, records the error and is retried on the next tick.
	clk.Advance(25 * time.Hour)
	b2.Tick(ctx, func(context.Context, io.Writer) error { return errors.New("disk full") })
	if _, st, _ := b2.List(); st.LastError == "" {
		t.Fatal("failure not reported")
	}
	ents, _ := os.ReadDir(st.Dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatal("partial file left behind")
		}
	}
	b2.Tick(ctx, snap)
	if _, st, _ := b2.List(); st.LastError != "" || calls.Load() != 3 {
		t.Fatalf("retry: %+v calls=%d", st, calls.Load())
	}

	// Retention: only N daily files remain.
	s.BackupKeepDaily = 2
	for i := 0; i < 4; i++ {
		clk.Advance(25 * time.Hour)
		b2.Tick(ctx, snap)
	}
	files, _, _ = b2.List()
	if len(files) != 2 {
		t.Fatalf("retention kept %d", len(files))
	}
	// Manual backups are never pruned and a second one in the same second gets the next name.
	m1, err := b2.Take(ctx, desktopport.BackupManual, snap)
	m2, err2 := b2.Take(ctx, desktopport.BackupManual, snap)
	if err != nil || err2 != nil || m1.ID == m2.ID {
		t.Fatalf("manual %v %v %s %s", err, err2, m1.ID, m2.ID)
	}
	if !b2.TryLock() {
		t.Fatal("lock")
	}
	if _, err := b2.Take(ctx, desktopport.BackupManual, snap); !errors.Is(err, desktopport.ErrBackupInProgress) {
		t.Fatalf("concurrent backup: %v", err)
	}
	b2.Unlock()
	if _, err := b2.Find("../desktop.json"); !errors.Is(err, desktopport.ErrNotFound) {
		t.Fatalf("find traversal: %v", err)
	}
	if p, err := b2.Find(m1.ID); err != nil || filepath.Base(p) != m1.ID {
		t.Fatalf("find: %q %v", p, err)
	}
}

func TestBackup_FallbackFolder(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	blocker := filepath.Join(t.TempDir(), "usb")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil { // a file where the folder should be
		t.Fatal(err)
	}
	s := DefaultSettings()
	s.BackupDir = filepath.Join(blocker, "porto")
	b := newTestBackups(t, clk, &s)
	var calls atomic.Int32
	f, err := b.Take(context.Background(), desktopport.BackupManual, fakeSnapshot(&calls))
	if err != nil {
		t.Fatal(err)
	}
	_, st, _ := b.List()
	if !st.Fallback || st.Dir != b.Paths.Backups() {
		t.Fatalf("status %+v", st)
	}
	if _, err := os.Stat(filepath.Join(b.Paths.Backups(), f.ID)); err != nil {
		t.Fatal(err)
	}
	ok := t.TempDir()
	s.BackupDir = ok
	if _, st, _ := b.List(); st.Fallback || st.Dir != ok {
		t.Fatalf("configured folder %+v", st)
	}
}

func updaterFor(srv *httptest.Server, version string, on *atomic.Bool) *Updater {
	return &Updater{
		URL: srv.URL + "/latest.json", Client: srv.Client(), Version: version, GOOS: "darwin", GOARCH: "arm64",
		Enabled: on.Load,
	}
}

func TestUpdate_DisabledMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"version":"9.9.9","url":"https://example.com/d"}`))
	}))
	defer srv.Close()
	var on atomic.Bool
	u := updaterFor(srv, "1.0.0", &on)
	ctx, cancel := context.WithCancel(context.Background())
	recheck := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() { u.Run(ctx, recheck); close(done) }()
	recheck <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if hits.Load() != 0 || u.Latest() != nil {
		t.Fatalf("requests with the check off: %d", hits.Load())
	}
	on.Store(true)
	u.Check(context.Background())
	if hits.Load() != 1 || u.Latest() == nil {
		t.Fatal("no request with the check on")
	}
	on.Store(false)
	if u.Latest() != nil {
		t.Fatal("banner shown with the check off")
	}
}

func TestUpdate_FailureIsSilent(t *testing.T) {
	for _, body := range []string{"", "{", `{"version":"x"}`, `{"version":"2.0.0","url":"http://insecure.example"}`, strings.Repeat("a", 70<<10)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if body == "" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		var on atomic.Bool
		on.Store(true)
		u := updaterFor(srv, "1.0.0", &on)
		u.Check(context.Background())
		if u.Latest() != nil {
			t.Fatalf("banner after a bad manifest %.20q", body)
		}
		srv.Close()
	}
	var on atomic.Bool
	on.Store(true)
	u := &Updater{URL: "http://127.0.0.1:1/none", Client: &http.Client{Timeout: time.Second}, Version: "1.0.0", Enabled: on.Load}
	u.Check(context.Background()) // unreachable: no panic, no banner
	if u.Latest() != nil {
		t.Fatal("banner without a server")
	}
}

func TestUpdate_UserAgentHasNoIdentifiers(t *testing.T) {
	var req *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req = r.Clone(context.Background())
		_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
	}))
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := updaterFor(srv, "1.2.3", &on)
	u.Check(context.Background())
	if req == nil {
		t.Fatal("no request")
	}
	// The form the Coste360 updates service counts: product/version (edition; goos/goarch).
	if ua := req.Header.Get("User-Agent"); ua != "porto/1.2.3 (desktop; darwin/arm64)" {
		t.Fatalf("User-Agent %q", ua)
	}
	for v, want := range map[string]string{
		"v1.2.3": "porto/1.2.3 (desktop; darwin/arm64)", "": "porto/dev (desktop; darwin/arm64)",
		"dev": "porto/dev (desktop; darwin/arm64)", "1.0.0 (x)": "porto/dev (desktop; darwin/arm64)",
	} {
		if got := (&Updater{Version: v, GOOS: "darwin", GOARCH: "arm64"}).UserAgent(); got != want {
			t.Errorf("UserAgent(%q) = %q", v, got)
		}
	}
	if req.URL.RawQuery != "" || len(req.Cookies()) != 0 || req.Header.Get("Authorization") != "" {
		t.Fatalf("identifying request: %v %v", req.URL, req.Header)
	}
	for k := range req.Header {
		if !slices.Contains([]string{"User-Agent", "Accept", "Accept-Encoding"}, k) {
			t.Errorf("unexpected header %s", k)
		}
	}
}

func TestUpdate_NewerVersionOnly(t *testing.T) {
	cases := []struct {
		running, latest string
		banner          bool
	}{
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "v1.10.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.2", false},
		{"dev", "9.0.0", false},
		{"1.2.3", "1.3.0-rc1", false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"version":%q,"url":"https://download.example/porto"}`, c.latest)
		}))
		var on atomic.Bool
		on.Store(true)
		u := updaterFor(srv, c.running, &on)
		u.Check(context.Background())
		if got := u.Latest() != nil; got != c.banner {
			t.Errorf("%s -> %s: banner %v", c.running, c.latest, got)
		}
		srv.Close()
	}
}

// releaseManifest is a latest.json as the private build pipeline writes it (updates service format plus the
// top-level url of the banner).
const releaseManifest = `{
  "version": "1.4.0",
  "url": "https://github.com/coste360/porto/releases/tag/v1.4.0",
  "tag": "v1.4.0",
  "published_at": "2027-03-01T10:00:00Z",
  "release_notes": "https://github.com/coste360/porto/releases/tag/v1.4.0",
  "checksums": "https://assets.coste360.it/updates/porto/1.4.0/SHA256SUMS",
  "files": [
    {"name": "Porto_1.4.0_macos_universal.dmg", "edition": "desktop", "os": "macos", "arch": "universal",
     "size": 41234567, "sha256": "aa", "url": "https://assets.coste360.it/updates/porto/1.4.0/Porto_1.4.0_macos_universal.dmg"},
    {"name": "porto_1.4.0_linux_amd64.tar.gz", "edition": "server", "os": "linux", "arch": "amd64",
     "size": 21234567, "sha256": "bb", "url": "https://assets.coste360.it/updates/porto/1.4.0/porto_1.4.0_linux_amd64.tar.gz"}
  ],
  "future_field": {"ignored": true}
}`

func TestUpdate_ReleaseManifest(t *testing.T) {
	serve := func(body string) *Updater {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		var on atomic.Bool
		on.Store(true)
		u := updaterFor(srv, "1.3.2", &on)
		u.Check(context.Background())
		return u
	}
	got := serve(releaseManifest).Latest()
	if got == nil || got.Version != "1.4.0" || got.URL != "https://github.com/coste360/porto/releases/tag/v1.4.0" {
		t.Fatalf("release manifest: %+v", got)
	}
	// Without a top-level url the banner opens the release notes; without version the tag is used.
	got = serve(`{"tag":"v1.5.0","release_notes":"https://github.com/coste360/porto/releases/tag/v1.5.0","files":[]}`).Latest()
	if got == nil || got.Version != "1.5.0" || got.URL != "https://github.com/coste360/porto/releases/tag/v1.5.0" {
		t.Fatalf("fallbacks: %+v", got)
	}
	for _, body := range []string{
		`{"version":"1.5.0","files":[]}`,
		`{"version":"1.5.0","url":"https://user:pw@example.com/x"}`,
		`{"version":"1.5.0","url":"","release_notes":"http://example.com/notes"}`,
		`{"version":"1.5.0","url":"https://example.com/x","files":"` + strings.Repeat("a", 65<<10) + `"}`,
	} {
		if got := serve(body).Latest(); got != nil {
			t.Errorf("banner from %.60q: %+v", body, got)
		}
	}
}

func TestUpdate_DefaultURL(t *testing.T) {
	if DefaultUpdateURL != "https://updates.coste360.it/porto/latest.json" {
		t.Fatalf("DefaultUpdateURL %q", DefaultUpdateURL)
	}
}

func TestBridge_SaveDownload(t *testing.T) {
	dir := t.TempDir()
	p1, err := SaveDownload(dir, "../../etc/passwd", "aGVsbG8=")
	if err != nil || filepath.Dir(p1) != dir || filepath.Base(p1) != "passwd" {
		t.Fatalf("%q %v", p1, err)
	}
	p2, err := SaveDownload(dir, "passwd", "aGVsbG8=")
	if err != nil || filepath.Base(p2) != "passwd (1)" {
		t.Fatalf("%q %v", p2, err)
	}
	if got := SanitizeFileName(`C:\x\Contratto 2026/27.pdf`); got != "27.pdf" {
		t.Fatalf("sanitize %q", got)
	}
	if got := SanitizeFileName("..."); got != "porto-file" {
		t.Fatalf("sanitize %q", got)
	}
	if _, err := SaveDownload(dir, "a", "!!"); !errors.Is(err, ErrSaveRefused) {
		t.Fatalf("bad base64: %v", err)
	}
	if _, err := SaveDownload(dir, "a", strings.Repeat("A", (MaxSaveFileBytes/3+10)*4)); !errors.Is(err, ErrSaveRefused) {
		t.Fatalf("too large: %v", err)
	}
	for u, ok := range map[string]bool{
		"https://coste360.it": true, "mailto:info@example.com": true, "http://x": false, "file:///etc": false,
		"javascript:alert(1)": false, "https://u:p@x": false,
	} {
		if ExternalURLAllowed(u) != ok {
			t.Errorf("%s allowed=%v", u, !ok)
		}
	}
	if s := BridgeScript(`1.0"</script>`); !strings.Contains(s, BindSaveFile) || strings.Contains(s, `1.0"<`) {
		t.Fatalf("script %s", s)
	}
}

type refreshingLAN struct {
	unavailableLAN
	calls atomic.Int32
}

func (r *refreshingLAN) PairTarget() desktopport.PairTarget { return desktopport.PairTarget{} }

func (r *refreshingLAN) Refresh(context.Context) (desktopport.LANStatus, error) {
	r.calls.Add(1)
	return desktopport.LANStatus{}, nil
}

func TestRefreshLAN_FollowsInjectedClock(t *testing.T) {
	lan := &refreshingLAN{}
	clk := clock.NewFake(time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		refreshLAN(ctx, lan, clk, LANRefreshEvery, time.Millisecond, slog.New(slog.DiscardHandler))
		close(done)
	}()
	waitCalls := func(want int32) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for lan.calls.Load() < want {
			select {
			case <-deadline:
				t.Fatalf("Refresh called %d times, want %d", lan.calls.Load(), want)
			case <-time.After(time.Millisecond):
			}
		}
	}
	// Many polls go by on wall time, but the injected clock has not moved: no refresh.
	time.Sleep(30 * time.Millisecond)
	if n := lan.calls.Load(); n != 0 {
		t.Fatalf("refreshed %d times without the clock moving", n)
	}
	clk.Advance(LANRefreshEvery - time.Second)
	time.Sleep(10 * time.Millisecond)
	if n := lan.calls.Load(); n != 0 {
		t.Fatalf("refreshed %d times before the interval", n)
	}
	clk.Advance(time.Second)
	waitCalls(1)
	clk.Advance(LANRefreshEvery)
	waitCalls(2)
	cancel()
	<-done
	if n := lan.calls.Load(); n != 2 {
		t.Fatalf("refreshed %d times, want 2", n)
	}
	// A controller without Refresh returns at once.
	refreshLAN(context.Background(), &unavailableLAN{}, clk, LANRefreshEvery, time.Millisecond, slog.New(slog.DiscardHandler))
}
