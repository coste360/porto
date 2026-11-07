package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
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

// LockPayment takes a row lock on a payment of the marina (SELECT … FOR UPDATE on PostgreSQL; on SQLite the single
// writer of the write queue serialises transactions) and returns it. Lock order (m5-plan §0.8): contract →
// instalments → payment → charges (ascending id) → cash session → sequences.
func LockPayment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Payment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(Payment)
	q := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// LockChargesSorted locks the charges of the marina with the given ids in ascending id order (so that two
// transactions never deadlock) and returns them in that order. Unknown ids are ErrNotFound.
func LockChargesSorted(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Charge, error) {
	uniq := map[string]bool{}
	var want []string
	for _, id := range ids {
		if !ValidID(id) {
			return nil, ErrNotFound
		}
		if !uniq[id] {
			uniq[id] = true
			want = append(want, id)
		}
	}
	out := []Charge{}
	if len(want) == 0 {
		return out, nil
	}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(want)).OrderExpr("id")
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	if len(out) != len(want) {
		return nil, ErrNotFound
	}
	return out, nil
}

// LockCashSession takes a row lock on a cash session of the marina and returns it.
func LockCashSession(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CashSession, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(CashSession)
	q := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}
