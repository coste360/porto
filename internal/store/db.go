// Package store is the only package of Porto that speaks SQL. It opens the database (SQLite or PostgreSQL through
// Bun), runs transactions and holds one repository file per area. Every operational function takes a MarinaScope
// (or OrgScope) obtained from an authorised principal, never a raw marina id from a request.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"modernc.org/sqlite"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/migrations"
)

// Dialect is the database flavour of an open DB.
type Dialect = migrations.Dialect

// Options selects the database. PostgresURL wins when set; otherwise SQLite opens SQLitePath, or a private
// in-memory database when SQLiteMemory is true (tests).
type Options struct {
	PostgresURL  string
	SQLitePath   string
	SQLiteMemory bool
}

// DB is an open database.
type DB struct {
	*bun.DB
	Kind Dialect
	// locks backs the in-process advisory locks used on SQLite (one process owns the file).
	locks *localLocks
}

// sqliteBusyTimeout covers contention with other processes (CLI, backup). Writers of this process wait in the
// in-process write queue instead (sqlitequeue.go).
const sqliteBusyTimeout = 10000

// Open opens the database described by o. It does not run migrations.
func Open(ctx context.Context, o Options) (*DB, error) {
	if o.PostgresURL != "" {
		return openPostgres(ctx, o.PostgresURL)
	}
	return openSQLite(ctx, o)
}

func openPostgres(ctx context.Context, dsn string) (*DB, error) {
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn))
	sqldb := sql.OpenDB(connector)
	sqldb.SetMaxOpenConns(25)
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxIdleTime(5 * time.Minute)
	db := &DB{DB: bun.NewDB(sqldb, pgdialect.New()), Kind: migrations.Postgres, locks: newLocalLocks()}
	if err := pingWithRetry(ctx, sqldb); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect to postgres: %w", err)
	}
	return db, nil
}

func pingWithRetry(ctx context.Context, sqldb *sql.DB) error {
	var err error
	for i := range 5 {
		if err = sqldb.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 200 * time.Millisecond):
		}
	}
	return err
}

func openSQLite(ctx context.Context, o Options) (*DB, error) {
	var dsn string
	params := url.Values{}
	params.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeout))
	params.Add("_pragma", "foreign_keys(1)")
	params.Set("_txlock", "immediate")
	switch {
	case o.SQLiteMemory:
		// memdb VFS: an in-memory database shared by the connections of this pool (and only them), with real
		// locking, so tests exercise the same concurrency as a file.
		name, err := crypto.RandomBase32(16)
		if err != nil {
			return nil, err
		}
		params.Set("vfs", "memdb")
		dsn = "file:/porto-" + strings.ToLower(name) + "?" + params.Encode()
	case o.SQLitePath != "":
		if dir := filepath.Dir(o.SQLitePath); dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("store: create data dir: %w", err)
			}
		}
		params.Add("_pragma", "journal_mode(WAL)")
		params.Add("_pragma", "synchronous(NORMAL)")
		dsn = "file:" + filepath.ToSlash(o.SQLitePath) + "?" + params.Encode()
	default:
		return nil, errors.New("store: no database configured")
	}
	queue := newWriteQueue()
	sqldb := sql.OpenDB(&queuedConnector{driver: &sqlite.Driver{}, dsn: dsn, queue: queue})
	sqldb.SetMaxOpenConns(8)
	sqldb.SetMaxIdleConns(8)
	sqldb.SetConnMaxIdleTime(0)
	sqldb.SetConnMaxLifetime(0)
	db := &DB{DB: bun.NewDB(sqldb, sqlitedialect.New()), Kind: migrations.SQLite, locks: newLocalLocks()}
	if err := sqldb.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	return db, nil
}

// Migrator returns a migration runner bound to this database.
func (db *DB) Migrator() (*migrations.Runner, error) {
	return migrations.NewRunner(db.DB.DB, db.Kind)
}

// Migrate applies every pending migration.
func (db *DB) Migrate(ctx context.Context) error {
	r, err := db.Migrator()
	if err != nil {
		return err
	}
	_, err = r.Up(ctx)
	return err
}

// IsPostgres reports whether the database is PostgreSQL.
func (db *DB) IsPostgres() bool { return db.Kind == migrations.Postgres }

// IsPostgres reports whether the bun handle (DB or Tx) talks to PostgreSQL. Repositories that need
// dialect-specific SQL use it on the bun.IDB they receive.
func IsPostgres(idb bun.IDB) bool {
	return idb.Dialect().Name().String() == "pg"
}

// maxTxAttempts bounds the retries of a transaction that failed on a serialization failure, a deadlock or a
// busy database.
const maxTxAttempts = 4

// RunInTx runs fn in a read-write transaction and commits when fn returns nil. On SQLite the transaction starts
// with BEGIN IMMEDIATE after waiting its turn in the in-process write queue. Serialization failures, deadlocks
// and busy errors are retried (up to 3 retries) with a short pause. Functions registered with AfterCommit run
// once the commit succeeded.
//
// Inside fn, every statement must go through tx: writing through db would wait for the queue slot that fn holds.
func (db *DB) RunInTx(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error {
	var err error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		hooks := &commitHooks{}
		txCtx := context.WithValue(ctx, commitHooksKey{}, hooks)
		err = db.DB.RunInTx(txCtx, nil, func(ctx context.Context, tx bun.Tx) error { return fn(ctx, tx) })
		if err == nil {
			hooks.run()
			return nil
		}
		if !IsSerializationFailure(err) || attempt == maxTxAttempts || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt*attempt) * 10 * time.Millisecond):
		}
	}
	return err
}

// ReadTx runs fn in a read-only transaction: one consistent snapshot (SQLite WAL / PostgreSQL REPEATABLE READ).
// It never waits for the write queue.
func (db *DB) ReadTx(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error {
	opts := &sql.TxOptions{ReadOnly: true}
	if db.IsPostgres() {
		opts.Isolation = sql.LevelRepeatableRead
	}
	return db.DB.RunInTx(ctx, opts, fn)
}

type commitHooksKey struct{}

type commitHooks struct{ fns []func() }

func (h *commitHooks) run() {
	for _, fn := range h.fns {
		fn()
	}
}

// AfterCommit registers fn to run after the transaction carried by ctx commits (ctx must be the one RunInTx
// passed to its callback). Outside RunInTx fn runs immediately. It is used to wake background jobs.
func AfterCommit(ctx context.Context, fn func()) {
	if h, ok := ctx.Value(commitHooksKey{}).(*commitHooks); ok {
		h.fns = append(h.fns, fn)
		return
	}
	fn()
}

// LockOrganization serialises transactions that check an organization-wide rule (last owner). On PostgreSQL it
// takes a row lock on the organization; on SQLite every write transaction is already exclusive.
func LockOrganization(ctx context.Context, tx bun.IDB, orgID string) error {
	if !IsPostgres(tx) {
		return nil
	}
	if !ValidID(orgID) {
		return ErrNotFound
	}
	var id string
	err := tx.NewRaw("SELECT id FROM organizations WHERE id = ? FOR UPDATE", orgID).Scan(ctx, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Common errors.
var (
	// ErrNotFound means the row does not exist or is not visible in the given scope.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means a unique constraint or a state precondition was violated.
	ErrConflict = errors.New("store: conflict")
	// ErrVersionMismatch means an optimistic concurrency check (If-Match) failed.
	ErrVersionMismatch = errors.New("store: version mismatch")
)

// IsUniqueViolation reports whether err comes from a unique (or primary key) constraint.
func IsUniqueViolation(err error) bool {
	if pe, ok := errors.AsType[pgdriver.Error](err); ok {
		return pe.Field('C') == "23505"
	}
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		switch se.Code() {
		case 2067, 1555: // SQLITE_CONSTRAINT_UNIQUE, SQLITE_CONSTRAINT_PRIMARYKEY
			return true
		}
	}
	return false
}

// IsForeignKeyViolation reports whether err comes from a foreign key constraint.
func IsForeignKeyViolation(err error) bool {
	if pe, ok := errors.AsType[pgdriver.Error](err); ok {
		return pe.Field('C') == "23503"
	}
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		return se.Code() == 787 // SQLITE_CONSTRAINT_FOREIGNKEY
	}
	return false
}

// IsSerializationFailure reports whether err is transient and the transaction can be retried: PostgreSQL
// serialization failure (40001) or deadlock (40P01), SQLite busy or locked.
func IsSerializationFailure(err error) bool {
	if pe, ok := errors.AsType[pgdriver.Error](err); ok {
		c := pe.Field('C')
		return c == "40001" || c == "40P01"
	}
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		switch se.Code() & 0xff {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED (and their extended codes)
			return true
		}
	}
	return false
}

// affected returns the number of rows a statement changed.
func affected(res sql.Result) int64 {
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}

// expectOne turns "no row changed" into ErrNotFound.
func expectOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrNotFound
	}
	return nil
}

// notFound maps sql.ErrNoRows to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
