//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/coste360/porto/internal/jobs"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestDBLocker_TwoRunnersDoNotOverlap(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		release := make(chan struct{})
		entered := make(chan struct{})
		job := func(r *jobs.Runner) {
			_ = r.Register(jobs.Job{Name: "shared", Every: time.Hour, Run: func(context.Context) error {
				entered <- struct{}{}
				<-release
				return nil
			}})
		}
		a := jobs.NewRunner(slog.New(slog.DiscardHandler), jobs.NewDBLocker(db))
		b := jobs.NewRunner(slog.New(slog.DiscardHandler), jobs.NewDBLocker(db))
		job(a)
		job(b)
		done := make(chan error)
		go func() { done <- a.RunNow(context.Background(), "shared") }()
		<-entered
		if err := b.RunNow(context.Background(), "shared"); !errors.Is(err, jobs.ErrBusy) {
			t.Fatalf("second runner: %v", err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		go func() { <-entered }()
		if err := b.RunNow(context.Background(), "shared"); err != nil {
			t.Fatalf("after release: %v", err)
		}
	})
}
