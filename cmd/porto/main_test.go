package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/migrations"
)

type result struct {
	code           int
	stdout, stderr string
}

func testEnv(t *testing.T, vars map[string]string) map[string]string {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	base := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key),
		"PORTO_DATA_DIR":   t.TempDir(),
		"PORTO_LOG_LEVEL":  "error",
	}
	for k, v := range vars {
		base[k] = v
	}
	return base
}

func runCLI(t *testing.T, vars map[string]string, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, env{
		stdin: strings.NewReader(stdin), stdout: &out, stderr: &errb,
		lookup: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
	})
	return result{code, out.String(), errb.String()}
}

func latestSchema(t *testing.T) int64 {
	t.Helper()
	v, err := migrations.Latest(migrations.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCLI_Version(t *testing.T) {
	r := runCLI(t, nil, "", "version")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "porto dev (go") || !strings.Contains(r.stdout, fmt.Sprintf("schema %d)", latestSchema(t))) {
		t.Fatalf("%+v", r)
	}
}

func TestCLI_Usage(t *testing.T) {
	for _, args := range [][]string{{}, {"nope"}, {"migrate"}, {"migrate", "sideways"}, {"admin", "create"}, {"backup"}} {
		if r := runCLI(t, testEnv(t, nil), "", args...); r.code != 2 || !strings.Contains(r.stderr, "usage:") {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestCLI_MigrateStatus(t *testing.T) {
	vars := testEnv(t, nil)
	r := runCLI(t, vars, "", "migrate", "status", "--check")
	if r.code != 1 || !strings.Contains(r.stdout, "00001_init.sql") || !strings.Contains(r.stderr, "pending") {
		t.Fatalf("before up: %+v", r)
	}
	if r := runCLI(t, vars, "", "migrate", "up"); r.code != 0 || !strings.Contains(r.stdout, fmt.Sprintf("schema version %d", latestSchema(t))) {
		t.Fatalf("up: %+v", r)
	}
	r = runCLI(t, vars, "", "migrate", "status", "--check")
	if r.code != 0 || !strings.Contains(r.stdout, "true") {
		t.Fatalf("after up: %+v", r)
	}
}

func TestCLI_AdminCreate(t *testing.T) {
	vars := testEnv(t, nil)
	args := []string{
		"admin", "create", "--email", "owner@porto.test", "--name", "Owner", "--org", "Porto SRL",
		"--marina", "Marina Prova", "--slug", "prova", "--password-stdin",
	}
	r := runCLI(t, vars, "prova-password-123\n", args...)
	if r.code != 0 || !strings.Contains(r.stdout, "created owner user") || !strings.Contains(r.stdout, "(prova)") {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.stdout+r.stderr, "prova-password-123") || strings.Contains(r.stdout+r.stderr, "owner@porto.test") {
		t.Fatal("password or email echoed")
	}
	if r := runCLI(t, vars, "prova-password-123\n", args...); r.code != 1 || !strings.Contains(r.stderr, "already exists") {
		t.Fatalf("duplicate: %+v", r)
	}
	weak := []string{"admin", "create", "--email", "x@porto.test", "--name", "X", "--org", "X SRL", "--password-stdin"}
	if r := runCLI(t, vars, "short\n", weak...); r.code != 1 || !strings.Contains(r.stderr, "too weak") {
		t.Fatalf("weak: %+v", r)
	}
	// Without a password source and without a terminal it fails rather than hang.
	if r := runCLI(t, vars, "", weak[:len(weak)-1]...); r.code != 1 || !strings.Contains(r.stderr, "no password") {
		t.Fatalf("no password: %+v", r)
	}
}

func TestCLI_SeedBackupRestore(t *testing.T) {
	vars := testEnv(t, map[string]string{"PORTO_ADMIN_PASSWORD": "demo-password-123"})
	r := runCLI(t, vars, "", "seed", "demo")
	if r.code != 0 || !strings.Contains(r.stdout, "owner@demo.porto.test") || strings.Contains(r.stdout, "demo-password-123") {
		t.Fatalf("seed: %+v", r)
	}
	if r := runCLI(t, vars, "", "seed", "demo"); r.code != 0 || !strings.Contains(r.stdout, "already exists") {
		t.Fatalf("seed again: %+v", r)
	}
	file := filepath.Join(t.TempDir(), "b.porto-backup")
	if r := runCLI(t, vars, "", "backup", "--out", file); r.code != 0 {
		t.Fatalf("backup: %+v", r)
	}
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v %v", st, err)
	}
	if r := runCLI(t, vars, "", "restore", "--in", file); r.code != 1 || !strings.Contains(r.stderr, "not empty") {
		t.Fatalf("restore without force: %+v", r)
	}
	if r := runCLI(t, vars, "", "restore", "--in", file, "--force"); r.code != 0 {
		t.Fatalf("restore --force: %+v", r)
	}
	other := testEnv(t, nil)
	if r := runCLI(t, other, "", "restore", "--in", file); r.code != 0 {
		t.Fatalf("restore into a new instance: %+v", r)
	}
}
