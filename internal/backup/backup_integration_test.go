//go:build integration

package backup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func seeded(t *testing.T, db *store.DB) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_DEV": "true", "PORTO_DATA_DIR": t.TempDir(),
		"PORTO_WEBHOOKS_ALLOW_PRIVATE": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler),
		app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := fixture.Ctx()
	res, err := a.SeedDemo(ctx, "demo-password-123")
	if err != nil {
		t.Fatal(err)
	}
	owner := &auth.Principal{Kind: auth.KindUser, UserID: "", Grants: []auth.Grant{{OrganizationID: res.OrganizationID, Role: auth.RoleOwner}}}
	me, err := store.UserByEmail(ctx, db.DB, res.OwnerEmail)
	if err != nil {
		t.Fatal(err)
	}
	owner.UserID = me.ID
	if _, _, err := a.APIKeys.Create(ctx, owner, res.MarinaID, apikey.CreateInput{Name: "k", Scopes: []auth.Permission{auth.PermMarinaRead}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Webhooks.Create(ctx, owner, res.MarinaID, webhook.CreateInput{URL: "https://example.com/hook", Events: []string{"member.added"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dispatcher.DispatchPending(ctx, 500); err != nil {
		t.Fatal(err)
	}
}

// tables returns every table of a backup as decoded rows (JSON values compared semantically).
func tables(t *testing.T, archive []byte) (*Manifest, map[string][]map[string]any) {
	t.Helper()
	dir := t.TempDir()
	m, err := extract(bytes.NewReader(archive), dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]map[string]any{}
	for _, tb := range m.Tables {
		b := readFile(t, dir+"/"+tb.Name+".jsonl")
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(nil, 16<<20)
		for sc.Scan() {
			var row map[string]any
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			out[tb.Name] = append(out[tb.Name], row)
		}
	}
	return m, out
}

func roundTrip(t *testing.T, src, dst *store.DB) {
	t.Helper()
	ctx := context.Background()
	seeded(t, src)
	srcFiles := t.TempDir()
	blob := []byte("encrypted-blob-bytes")
	if err := os.MkdirAll(filepath.Join(srcFiles, "marina-1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcFiles, "marina-1", "file-1.bin"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcFiles, "marina-1", ".tmp-partial"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m1, err := Backup(ctx, src, &buf, "test", srcFiles)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Rows("users") < 9 || m1.Rows("audit_log") == 0 || m1.Rows("webhook_endpoints") != 1 {
		t.Fatalf("unexpected counts: %+v", m1.Tables)
	}
	if len(m1.Files) != 1 || m1.Files[0].Path != "files/marina-1/file-1.bin" {
		t.Fatalf("files in manifest: %+v", m1.Files)
	}
	if _, err := Restore(ctx, dst, bytes.NewReader(buf.Bytes()), false, ""); !errors.Is(err, ErrNoFilesDir) {
		t.Fatalf("restore without files dir: %v", err)
	}
	dstFiles := t.TempDir()
	if _, err := Restore(ctx, dst, bytes.NewReader(buf.Bytes()), false, dstFiles); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dstFiles, "marina-1", "file-1.bin")); err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("restored blob = %q, %v", got, err)
	}
	var again bytes.Buffer
	if _, err := Backup(ctx, dst, &again, "test", ""); err != nil {
		t.Fatal(err)
	}
	_, want := tables(t, buf.Bytes())
	_, got := tables(t, again.Bytes())
	for name, rows := range want {
		if !reflect.DeepEqual(rows, got[name]) {
			t.Errorf("table %s differs after the round trip:\nwant %v\ngot  %v", name, rows, got[name])
		}
	}
	// The restored database works: a second restore without --force refuses.
	if _, err := Restore(ctx, dst, bytes.NewReader(buf.Bytes()), false, t.TempDir()); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("restore into a non-empty database: %v", err)
	}
	if _, err := Restore(ctx, dst, bytes.NewReader(buf.Bytes()), true, t.TempDir()); err != nil {
		t.Fatalf("forced restore: %v", err)
	}
}

func TestBackup_RoundTripSQLiteToSQLite(t *testing.T) {
	roundTrip(t, testdb.SQLite(t), testdb.SQLite(t))
}

func TestBackup_RoundTripSQLiteToPostgres(t *testing.T) {
	roundTrip(t, testdb.SQLite(t), testdb.Postgres(t))
}

func TestBackup_RoundTripPostgresToSQLite(t *testing.T) {
	roundTrip(t, testdb.Postgres(t), testdb.SQLite(t))
}

func TestRestore_RejectsSchemaMismatch(t *testing.T) {
	src := testdb.SQLite(t)
	seeded(t, src)
	var buf bytes.Buffer
	if _, err := Backup(context.Background(), src, &buf, "test", ""); err != nil {
		t.Fatal(err)
	}
	testdb.RunUnmigrated(t, func(t *testing.T, db *store.DB) {
		if _, err := Restore(context.Background(), db, bytes.NewReader(buf.Bytes()), false, t.TempDir()); !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("err = %v, want ErrSchemaMismatch", err)
		}
	})
}

func TestRestore_RejectsCorruptArchive(t *testing.T) {
	src := testdb.SQLite(t)
	seeded(t, src)
	var buf bytes.Buffer
	if _, err := Backup(context.Background(), src, &buf, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), testdb.SQLite(t), bytes.NewReader([]byte("not a backup")), false, t.TempDir()); !errors.Is(err, ErrFormat) {
		t.Fatalf("garbage: %v", err)
	}
	b := buf.Bytes()
	if _, err := Restore(context.Background(), testdb.SQLite(t), bytes.NewReader(b[:len(b)/2]), false, t.TempDir()); err == nil {
		t.Fatal("truncated archive accepted")
	}
}
