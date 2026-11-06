// Package testdb runs database tests on every available dialect: SQLite (in memory, always) and PostgreSQL when
// PORTO_TEST_POSTGRES_URL is set. Each test gets a fresh, migrated database (a private schema on PostgreSQL).
package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/internal/store"
)

// EnvPostgresURL names the variable with the URL of a PostgreSQL server for tests.
const EnvPostgresURL = "PORTO_TEST_POSTGRES_URL"

// Run runs fn as one subtest per available dialect ("sqlite", "postgres").
func Run(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		fn(t, SQLite(t))
	})
	t.Run("postgres", func(t *testing.T) {
		fn(t, Postgres(t))
	})
}

// RunUnmigrated is Run without migrations (for migration tests).
func RunUnmigrated(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		fn(t, open(t, migrations.SQLite, false))
	})
	t.Run("postgres", func(t *testing.T) {
		fn(t, open(t, migrations.Postgres, false))
	})
}

// SQLite returns a fresh migrated in-memory SQLite database.
func SQLite(t *testing.T) *store.DB {
	t.Helper()
	return open(t, migrations.SQLite, true)
}

// Postgres returns a fresh migrated PostgreSQL database, or skips the test when no server is configured.
func Postgres(t *testing.T) *store.DB {
	t.Helper()
	return open(t, migrations.Postgres, true)
}

func open(t *testing.T, d migrations.Dialect, migrate bool) *store.DB {
	t.Helper()
	ctx := context.Background()
	var opts store.Options
	switch d {
	case migrations.SQLite:
		opts.SQLiteMemory = true
	case migrations.Postgres:
		base := os.Getenv(EnvPostgresURL)
		if base == "" {
			t.Skip(EnvPostgresURL + " not set: PostgreSQL tests skipped")
		}
		opts.PostgresURL = privateSchema(t, base)
	}
	db, err := store.Open(ctx, opts)
	if err != nil {
		t.Fatalf("testdb: open %s: %v", d, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if migrate {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("testdb: migrate %s: %v", d, err)
		}
	}
	return db
}

// privateSchema creates an empty schema for the test and returns base with search_path set to it. The schema is
// dropped when the test ends.
func privateSchema(t *testing.T, base string) string {
	t.Helper()
	suffix, err := crypto.RandomBase32(10)
	if err != nil {
		t.Fatal(err)
	}
	schema := "t_" + strings.ToLower(suffix)
	admin := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(base)))
	if _, err := admin.ExecContext(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("testdb: create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("testdb: parse %s: %v", EnvPostgresURL, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
