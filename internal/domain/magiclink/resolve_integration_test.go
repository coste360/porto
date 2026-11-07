//go:build integration

package magiclink_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// A database failure must not be reported as an invalid link: the client would show "link expired" and give up,
// while the request can simply be retried (seen as a flaky TestConcurrency_OTPAttempts under load).
func TestResolve_DatabaseErrorIsNotInvalidLink(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		s := magiclink.NewService(magiclink.Deps{DB: db, Clock: clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))})
		token := strings.Repeat("a", magiclink.TokenChars)

		if _, err := s.Resolve(context.Background(), "demo", token); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatalf("unknown token: got %v, want ErrInvalid", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.Resolve(ctx, "demo", token)
		if err == nil || errors.Is(err, magiclink.ErrInvalid) {
			t.Fatalf("database failure: got %v, want a non-ErrInvalid error", err)
		}
	})
}
