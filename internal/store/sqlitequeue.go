package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
)

// SQLite allows one writer at a time. When two connections of the same pool want to write, SQLite does not queue
// them: the loser sleeps and polls under busy_timeout and can lose again and again under a burst, failing with
// SQLITE_BUSY even though the total work would have fitted. Porto therefore queues the writers of its own process
// in arrival order before they ask SQLite for the lock:
//
//   - a read-write transaction (BeginTx without ReadOnly, which the DSN turns into BEGIN IMMEDIATE) takes its turn
//     in BeginTx and gives it back on Commit/Rollback;
//   - an autocommit Exec outside a transaction takes a turn for the statement;
//   - an autocommit Query whose statement writes (INSERT/UPDATE/DELETE ... RETURNING) keeps its turn until the
//     rows are closed.
//
// Reads and read-only transactions use WAL snapshots and never queue. busy_timeout stays in place for contention
// with other processes (CLI commands, backup).
//
// Rule: inside a write transaction never write through the *DB pool, always through the transaction; the pool
// write would wait for the turn held by the same goroutine.

// writeQueue is a FIFO mutex: waiters are served strictly in arrival order and can give up when their context
// ends.
type writeQueue struct {
	mu      sync.Mutex
	busy    bool
	waiters []chan struct{}
}

func newWriteQueue() *writeQueue { return &writeQueue{} }

// acquire waits for the turn.
func (q *writeQueue) acquire(ctx context.Context) error {
	q.mu.Lock()
	if !q.busy {
		q.busy = true
		q.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	q.waiters = append(q.waiters, ch)
	q.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		q.mu.Lock()
		for i, w := range q.waiters {
			if w == ch {
				q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
				q.mu.Unlock()
				return ctx.Err()
			}
		}
		q.mu.Unlock()
		// The turn was handed over while the context ended: pass it on.
		q.release()
		return ctx.Err()
	}
}

// release hands the turn to the oldest waiter, or frees it.
func (q *writeQueue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.waiters) == 0 {
		q.busy = false
		return
	}
	next := q.waiters[0]
	q.waiters = q.waiters[1:]
	close(next) // busy stays true: ownership moves to next
}

// queuedConnector opens SQLite connections that share one writeQueue.
type queuedConnector struct {
	driver driver.Driver
	dsn    string
	queue  *writeQueue
}

func (c *queuedConnector) Connect(context.Context) (driver.Conn, error) {
	raw, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	inner, ok := raw.(fullConn)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("store: sqlite connection does not implement the context interfaces")
	}
	return &queuedConn{inner: inner, queue: c.queue}, nil
}

func (c *queuedConnector) Driver() driver.Driver { return c.driver }

// fullConn lists the driver interfaces database/sql uses on a modernc connection.
type fullConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

// queuedConn wraps one connection. database/sql uses a driver.Conn from one goroutine at a time, but it may call
// Tx.Rollback from its context watcher, so the transaction pointer is guarded.
type queuedConn struct {
	inner fullConn
	queue *writeQueue

	mu sync.Mutex
	tx *queuedTx // open read-write transaction holding the queue turn
}

var _ fullConn = (*queuedConn)(nil)

func (c *queuedConn) inWriteTx() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tx != nil
}

func (c *queuedConn) Prepare(query string) (driver.Stmt, error) { return c.inner.Prepare(query) }

func (c *queuedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.inner.PrepareContext(ctx, query)
}

func (c *queuedConn) Ping(ctx context.Context) error { return c.inner.Ping(ctx) }

func (c *queuedConn) ResetSession(ctx context.Context) error { return c.inner.ResetSession(ctx) }

func (c *queuedConn) IsValid() bool { return c.inner.IsValid() }

// Close closes the connection; if a write transaction was still open (a broken connection discarded by
// database/sql) its turn is released after SQLite dropped the lock.
func (c *queuedConn) Close() error {
	err := c.inner.Close()
	c.mu.Lock()
	tx := c.tx
	c.mu.Unlock()
	if tx != nil {
		tx.finish()
	}
	return err
}

func (c *queuedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *queuedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if opts.ReadOnly {
		return c.inner.BeginTx(ctx, opts)
	}
	if err := c.queue.acquire(ctx); err != nil {
		return nil, err
	}
	tx, err := c.inner.BeginTx(ctx, opts)
	if err != nil {
		c.queue.release()
		return nil, err
	}
	qt := &queuedTx{inner: tx, conn: c}
	c.mu.Lock()
	c.tx = qt
	c.mu.Unlock()
	return qt, nil
}

func (c *queuedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.inWriteTx() {
		return c.inner.ExecContext(ctx, query, args)
	}
	if err := c.queue.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.queue.release()
	return c.inner.ExecContext(ctx, query, args)
}

func (c *queuedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.inWriteTx() || !writesWithReturning(query) {
		return c.inner.QueryContext(ctx, query, args)
	}
	if err := c.queue.acquire(ctx); err != nil {
		return nil, err
	}
	rows, err := c.inner.QueryContext(ctx, query, args)
	if err != nil {
		c.queue.release()
		return nil, err
	}
	return &queuedRows{Rows: rows, queue: c.queue}, nil
}

// writesWithReturning reports whether a statement sent through Query writes. Bun sends every other write through
// Exec, so a RETURNING clause is the marker.
func writesWithReturning(query string) bool {
	return strings.Contains(strings.ToUpper(query), "RETURNING")
}

// queuedTx gives the turn back when the transaction ends.
type queuedTx struct {
	inner driver.Tx
	conn  *queuedConn
	once  sync.Once
}

func (t *queuedTx) finish() {
	t.once.Do(func() {
		t.conn.mu.Lock()
		if t.conn.tx == t {
			t.conn.tx = nil
		}
		t.conn.mu.Unlock()
		t.conn.queue.release()
	})
}

func (t *queuedTx) Commit() error {
	err := t.inner.Commit()
	t.finish()
	return err
}

func (t *queuedTx) Rollback() error {
	err := t.inner.Rollback()
	t.finish()
	return err
}

// queuedRows holds the turn of an autocommit write until its rows are closed. It forwards the column type
// interfaces that bun uses when scanning into maps.
type queuedRows struct {
	driver.Rows
	queue *writeQueue
	once  sync.Once
}

func (r *queuedRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(r.queue.release)
	return err
}

func (r *queuedRows) ColumnTypeDatabaseTypeName(i int) string {
	if ct, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return ct.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}
