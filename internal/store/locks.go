package store

import (
	"context"
	"fmt"
	"sync"
)

// localLocks are named in-process locks (SQLite: one process owns the database file).
type localLocks struct {
	mu   sync.Mutex
	held map[string]bool
}

func newLocalLocks() *localLocks { return &localLocks{held: map[string]bool{}} }

func (l *localLocks) try(name string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[name] {
		return nil, false
	}
	l.held[name] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.held, name)
			l.mu.Unlock()
		})
	}, true
}

// TryAdvisoryLock takes a named lock without waiting. On PostgreSQL it is a session advisory lock
// (pg_try_advisory_lock(hashtext(name))) held on a dedicated connection, so that several Porto processes on the
// same database do not run the same job at once; on SQLite it is an in-process lock. ok is false when someone
// else holds the lock. unlock must be called exactly once when ok is true.
func (db *DB) TryAdvisoryLock(ctx context.Context, name string) (unlock func(), ok bool, err error) {
	if !db.IsPostgres() {
		unlock, ok = db.locks.try(name)
		return unlock, ok, nil
	}
	// Guard the PostgreSQL lock with the local one too: advisory locks are re-entrant per session, and two
	// goroutines of this process could otherwise both "acquire" through the same pooled connection.
	localUnlock, ok := db.locks.try(name)
	if !ok {
		return nil, false, nil
	}
	conn, err := db.DB.DB.Conn(ctx)
	if err != nil {
		localUnlock()
		return nil, false, fmt.Errorf("store: advisory lock: %w", err)
	}
	var got bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", name).Scan(&got); err != nil {
		_ = conn.Close()
		localUnlock()
		return nil, false, fmt.Errorf("store: advisory lock: %w", err)
	}
	if !got {
		_ = conn.Close()
		localUnlock()
		return nil, false, nil
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtext($1))", name)
			_ = conn.Close()
			localUnlock()
		})
	}, true, nil
}
