// Package migrations embeds Porto's SQL migrations (one directory per dialect) and runs them with goose.
//
// Both directories hold the same numbered files. A migration already on main is never edited: a schema change is a
// new file in both directories (AGENTS.md).
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/pressly/goose/v3"
)

// Dialect names a supported database.
type Dialect string

const (
	// SQLite is the default, single-file database (modernc.org/sqlite, no CGO).
	SQLite Dialect = "sqlite"
	// Postgres is PostgreSQL 15+.
	Postgres Dialect = "postgres"
)

//go:embed sqlite/*.sql postgres/*.sql
var files embed.FS

// FS returns the migration files of one dialect.
func FS(d Dialect) (fs.FS, error) {
	switch d {
	case SQLite, Postgres:
		return fs.Sub(files, string(d))
	default:
		return nil, fmt.Errorf("migrations: unknown dialect %q", d)
	}
}

// Status describes one migration file.
type Status struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt string // RFC 3339 UTC, empty when pending
}

// Runner applies the migrations of one dialect to a database.
type Runner struct {
	provider *goose.Provider
}

// NewRunner returns a Runner for db. db must already be open on the given dialect.
func NewRunner(db *sql.DB, d Dialect) (*Runner, error) {
	fsys, err := FS(d)
	if err != nil {
		return nil, err
	}
	gd := goose.DialectSQLite3
	if d == Postgres {
		gd = goose.DialectPostgres
	}
	p, err := goose.NewProvider(gd, db, fsys, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Runner{provider: p}, nil
}

// Up applies every pending migration and returns the versions applied.
func (r *Runner) Up(ctx context.Context) ([]int64, error) {
	res, err := r.provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: up: %w", err)
	}
	out := make([]int64, 0, len(res))
	for _, m := range res {
		out = append(out, m.Source.Version)
	}
	return out, nil
}

// UpTo applies the pending migrations up to and including version (restoring an archive taken on an older
// schema: the database is brought to the archive's version, loaded, then migrated up).
func (r *Runner) UpTo(ctx context.Context, version int64) error {
	if _, err := r.provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("migrations: up to %d: %w", version, err)
	}
	return nil
}

// Reset rolls every migration back. Tests use it to check the Down sections; operators never need it.
func (r *Runner) Reset(ctx context.Context) error {
	if _, err := r.provider.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("migrations: down: %w", err)
	}
	return nil
}

// Status lists every known migration with its state.
func (r *Runner) Status(ctx context.Context) ([]Status, error) {
	st, err := r.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: status: %w", err)
	}
	out := make([]Status, 0, len(st))
	for _, s := range st {
		row := Status{Version: s.Source.Version, Name: path.Base(s.Source.Path), Applied: s.State == goose.StateApplied}
		if row.Applied && !s.AppliedAt.IsZero() {
			row.AppliedAt = s.AppliedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out, nil
}

// Aligned reports whether the database has every migration applied and nothing unknown.
func (r *Runner) Aligned(ctx context.Context) (bool, error) {
	pending, err := r.provider.HasPending(ctx)
	if err != nil {
		return false, fmt.Errorf("migrations: %w", err)
	}
	if pending {
		return false, nil
	}
	current, target, err := r.provider.GetVersions(ctx)
	if err != nil {
		return false, fmt.Errorf("migrations: %w", err)
	}
	return current == target, nil
}

// Version returns the highest applied version (0 on an empty database).
func (r *Runner) Version(ctx context.Context) (int64, error) {
	v, err := r.provider.GetDBVersion(ctx)
	if err != nil {
		if errors.Is(err, goose.ErrNoMigrations) {
			return 0, nil
		}
		return 0, fmt.Errorf("migrations: %w", err)
	}
	return v, nil
}

// Latest returns the highest version shipped in the binary for a dialect.
func Latest(d Dialect) (int64, error) {
	fsys, err := FS(d)
	if err != nil {
		return 0, err
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, n := range names {
		v, err := versionOf(n)
		if err != nil {
			return 0, err
		}
		latest = max(latest, v)
	}
	return latest, nil
}

func versionOf(name string) (int64, error) {
	var v int64
	if _, err := fmt.Sscanf(name, "%05d_", &v); err != nil {
		return 0, fmt.Errorf("migrations: bad file name %q", name)
	}
	return v, nil
}
