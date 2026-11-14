package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRunner_RegisterValidation(t *testing.T) {
	r := NewRunner(quiet(), nil)
	run := func(context.Context) error { return nil }
	if err := r.Register(Job{Name: "a", Every: time.Second, Run: run}); err != nil {
		t.Fatal(err)
	}
	for _, j := range []Job{{Name: "a", Every: time.Second, Run: run}, {Name: "", Every: time.Second, Run: run}, {Name: "b", Run: run}, {Name: "c", Every: time.Second}} {
		if err := r.Register(j); err == nil {
			t.Errorf("%+v accepted", j)
		}
	}
	if got := r.Jobs(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("jobs %v", got)
	}
	if err := r.RunNow(context.Background(), "zzz"); !errors.Is(err, ErrUnknownJob) {
		t.Fatal(err)
	}
}

func TestRunner_StartWakeAndStop(t *testing.T) {
	r := NewRunner(quiet(), nil)
	var starts, runs atomic.Int32
	wake := make(chan struct{}, 1)
	done := make(chan struct{}, 10)
	_ = r.Register(Job{Name: "woken", Every: time.Hour, RunAtStart: true, Wake: wake, Run: func(context.Context) error {
		if runs.Add(1) == 1 {
			starts.Add(1)
		}
		done <- struct{}{}
		return nil
	}})
	var ticks atomic.Int32
	_ = r.Register(Job{Name: "ticker", Every: 10 * time.Millisecond, Run: func(context.Context) error {
		ticks.Add(1)
		return errors.New("logged, not fatal")
	}})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	<-done // RunAtStart
	wake <- struct{}{}
	<-done // woken
	deadline := time.After(2 * time.Second)
	for ticks.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("ticker did not run")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	r.Wait()
	if runs.Load() != 2 || starts.Load() != 1 {
		t.Fatalf("runs=%d", runs.Load())
	}
	if err := r.Register(Job{Name: "late", Every: time.Second, Run: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("register after start accepted")
	}
}

func TestRunner_NoOverlapAndPanic(t *testing.T) {
	r := NewRunner(quiet(), nil)
	release := make(chan struct{})
	entered := make(chan struct{})
	var active, maxActive atomic.Int32
	_ = r.Register(Job{Name: "slow", Every: time.Hour, Run: func(context.Context) error {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return nil
	}})
	_ = r.Register(Job{Name: "boom", Every: time.Hour, Run: func(context.Context) error { panic("kaboom") }})

	var wg sync.WaitGroup
	wg.Go(func() { _ = r.RunNow(context.Background(), "slow") })
	<-entered
	// While the first run holds the job, every other RunNow returns at once: wait for all of them (not for a
	// fixed time), then release the first run.
	var busy atomic.Int32
	var others sync.WaitGroup
	for range 100 {
		others.Go(func() {
			if errors.Is(r.RunNow(context.Background(), "slow"), ErrBusy) {
				busy.Add(1)
			}
		})
	}
	others.Wait()
	close(release)
	wg.Wait()
	if maxActive.Load() != 1 {
		t.Fatalf("overlapping runs: %d", maxActive.Load())
	}
	if busy.Load() != 100 {
		t.Fatalf("%d of 100 concurrent runs refused while the first one was running", busy.Load())
	}
	if err := r.RunNow(context.Background(), "boom"); err == nil {
		t.Fatal("panic not turned into an error")
	}
}

func TestLocalLocker(t *testing.T) {
	l := NewLocalLocker()
	unlock, ok, _ := l.TryLock(context.Background(), "x")
	if !ok {
		t.Fatal("first lock")
	}
	if _, ok, _ := l.TryLock(context.Background(), "x"); ok {
		t.Fatal("second lock")
	}
	unlock()
	unlock()
	if _, ok, _ := l.TryLock(context.Background(), "x"); !ok {
		t.Fatal("after unlock")
	}
}
