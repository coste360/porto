// Package jobs runs Porto's background jobs in process: each job runs on its period, when its wake channel
// fires, or on demand; runs of the same job never overlap (in process via a mutex, across processes via the
// Locker); a panic is recovered and logged.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/store"
)

// Job is a periodic task.
type Job struct {
	Name       string
	Every      time.Duration
	RunAtStart bool
	Run        func(ctx context.Context) error
	// Wake, when set, triggers a run as soon as it receives (e.g. after a commit that wrote the outbox).
	Wake <-chan struct{}
}

// Locker takes a named lock without waiting.
type Locker interface {
	TryLock(ctx context.Context, name string) (unlock func(), ok bool, err error)
}

// Errors.
var (
	ErrUnknownJob = errors.New("jobs: unknown job")
	ErrBusy       = errors.New("jobs: job already running")
)

type entry struct {
	job Job
	mu  sync.Mutex // serialises runs in this process
}

// Runner runs registered jobs.
type Runner struct {
	log    *slog.Logger
	locker Locker

	mu      sync.Mutex
	jobs    map[string]*entry
	order   []string
	started bool
	wg      sync.WaitGroup
}

// NewRunner returns a Runner. lock may be nil (in-process locking only).
func NewRunner(log *slog.Logger, lock Locker) *Runner {
	if lock == nil {
		lock = NewLocalLocker()
	}
	return &Runner{log: log, locker: lock, jobs: map[string]*entry{}}
}

// Register adds a job. It fails for a duplicate name, a missing Run or a non-positive period, or after Start.
func (r *Runner) Register(j Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.started:
		return errors.New("jobs: register after start")
	case j.Name == "" || j.Run == nil || j.Every <= 0:
		return fmt.Errorf("jobs: invalid job %q", j.Name)
	case r.jobs[j.Name] != nil:
		return fmt.Errorf("jobs: duplicate job %q", j.Name)
	}
	r.jobs[j.Name] = &entry{job: j}
	r.order = append(r.order, j.Name)
	return nil
}

// Jobs returns the registered job names in registration order.
func (r *Runner) Jobs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.order)
}

// Start launches one goroutine per job; they stop when ctx ends. Call Wait to wait for them.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	entries := make([]*entry, 0, len(r.order))
	for _, name := range r.order {
		entries = append(entries, r.jobs[name])
	}
	r.mu.Unlock()
	for _, e := range entries {
		r.wg.Add(1)
		go r.loop(ctx, e)
	}
}

// Wait blocks until every job goroutine stopped (after the Start context ended).
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) loop(ctx context.Context, e *entry) {
	defer r.wg.Done()
	if e.job.RunAtStart {
		r.runOnce(ctx, e, "start")
	}
	t := time.NewTimer(e.job.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.runOnce(ctx, e, "timer")
			t.Reset(e.job.Every)
		case <-e.job.Wake:
			r.runOnce(ctx, e, "wake")
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(e.job.Every)
		}
	}
}

// RunNow runs a job immediately and returns its error. It returns ErrBusy when the job is running (here or,
// with a database locker, in another process).
func (r *Runner) RunNow(ctx context.Context, name string) error {
	r.mu.Lock()
	e := r.jobs[name]
	r.mu.Unlock()
	if e == nil {
		return ErrUnknownJob
	}
	return r.run(ctx, e, "manual")
}

func (r *Runner) runOnce(ctx context.Context, e *entry, trigger string) {
	if err := r.run(ctx, e, trigger); err != nil && !errors.Is(err, ErrBusy) && ctx.Err() == nil {
		r.log.ErrorContext(ctx, "job failed", "job", e.job.Name, "trigger", trigger, "error", err)
	}
}

func (r *Runner) run(ctx context.Context, e *entry, trigger string) (err error) {
	if !e.mu.TryLock() {
		return ErrBusy
	}
	defer e.mu.Unlock()
	unlock, ok, err := r.locker.TryLock(ctx, "porto.job."+e.job.Name)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}
	defer unlock()
	start := clock.System{}.Now() // wall clock for an operational metric (duration), not domain time
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("jobs: %s panicked: %v", e.job.Name, p)
			r.log.ErrorContext(ctx, "job panicked", "job", e.job.Name, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
		}
		r.log.DebugContext(ctx, "job run", "job", e.job.Name, "trigger", trigger,
			"duration_ms", clock.System{}.Now().Sub(start).Milliseconds(), "ok", err == nil)
	}()
	return e.job.Run(ctx)
}

// localLocker is an in-process Locker.
type localLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

// NewLocalLocker returns an in-process Locker.
func NewLocalLocker() Locker { return &localLocker{held: map[string]bool{}} }

func (l *localLocker) TryLock(_ context.Context, name string) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[name] {
		return nil, false, nil
	}
	l.held[name] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.held, name)
			l.mu.Unlock()
		})
	}, true, nil
}

// dbLocker uses the database: PostgreSQL advisory locks (several processes), in-process locks on SQLite.
type dbLocker struct{ db *store.DB }

// NewDBLocker returns a Locker backed by db.
func NewDBLocker(db *store.DB) Locker { return dbLocker{db: db} }

func (l dbLocker) TryLock(ctx context.Context, name string) (func(), bool, error) {
	return l.db.TryAdvisoryLock(ctx, name)
}
