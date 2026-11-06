//go:build integration

package notify

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type flakyTransport struct {
	mu    sync.Mutex
	fails int
	sent  []Message
}

func (f *flakyTransport) Send(_ context.Context, m Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails > 0 {
		f.fails--
		return "", errors.New("421 try later")
	}
	f.sent = append(f.sent, m)
	return "p-" + m.ID, nil
}

func newTestService(t *testing.T, db *store.DB, c clock.Clock, tr Transport) *Service {
	t.Helper()
	k, _ := crypto.GenerateKey()
	keys, err := crypto.ParseKeyRing(k)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, c, keys, tr, "http://localhost:8080/", slog.New(slog.DiscardHandler))
}

func seedUser(t *testing.T, db *store.DB) string {
	t.Helper()
	u := &store.User{ID: store.NewID(), Email: "anna@example.test", Name: "Anna", Locale: "it", Active: true, CreatedAt: t0, UpdatedAt: t0}
	if err := store.InsertUser(context.Background(), db.DB, u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestDeliver_DedupeAndRetry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		c := clock.NewFake(t0)
		tr := &flakyTransport{fails: 2}
		s := newTestService(t, db, c, tr)
		userID := seedUser(t, db)
		mail := PasswordResetMail{UserID: userID, Email: "Anna@Example.test", Name: "Anna", Locale: "en", Token: "tok-1", ExpiresAt: t0.Add(time.Hour)}
		for range 2 { // same token twice: one message
			err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error { return s.EnqueuePasswordReset(ctx, tx, mail) })
			if err != nil {
				t.Fatal(err)
			}
		}
		select {
		case <-s.Wake():
		default:
			t.Fatal("not woken after commit")
		}
		rows, _ := store.ListMessages(ctx, db.DB)
		if len(rows) != 1 {
			t.Fatalf("messages = %d, want 1 (dedupe)", len(rows))
		}
		if strings.Contains(rows[0].RecipientEnc, "anna") || strings.Contains(rows[0].PayloadEnc, "tok-1") ||
			rows[0].RecipientHash != s.RecipientHash("anna@example.test") {
			t.Fatal("recipient or payload stored in clear")
		}

		// Attempt 1 fails, retry after 1 min; attempt 2 fails, retry after 2 min; attempt 3 succeeds.
		if n, err := s.DeliverDue(ctx, 10); err != nil || n != 0 {
			t.Fatalf("first run: %d %v", n, err)
		}
		if n, _ := s.DeliverDue(ctx, 10); n != 0 {
			t.Fatal("retried before backoff")
		}
		c.Advance(time.Minute)
		if n, _ := s.DeliverDue(ctx, 10); n != 0 {
			t.Fatal("second attempt should fail")
		}
		c.Advance(time.Minute)
		if n, _ := s.DeliverDue(ctx, 10); n != 0 {
			t.Fatal("retried before second backoff (2 min)")
		}
		c.Advance(time.Minute)
		if n, err := s.DeliverDue(ctx, 10); err != nil || n != 1 {
			t.Fatalf("third run: %d %v", n, err)
		}
		m, _ := store.GetMessage(ctx, db.DB, rows[0].ID)
		if m.Status != store.MessageSent || m.Attempts != 3 || m.ProviderMessageID != "p-"+m.ID || m.LastError != "" {
			t.Fatalf("final row: %+v", m)
		}
		if len(tr.sent) != 1 || tr.sent[0].To != "Anna@Example.test" || !strings.Contains(tr.sent[0].Text, "#token=tok-1") ||
			!strings.Contains(tr.sent[0].Subject, "Porto") || tr.sent[0].Locale != "en" {
			t.Fatalf("sent: %+v", tr.sent)
		}
		if n, _ := s.DeliverDue(ctx, 10); n != 0 {
			t.Fatal("sent twice")
		}
	})
}

func TestDeliver_GivesUpAfterMaxAttempts(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		c := clock.NewFake(t0)
		tr := &flakyTransport{fails: 100}
		s := newTestService(t, db, c, tr)
		userID := seedUser(t, db)
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			return s.EnqueueInvitation(ctx, tx, InvitationMail{UserID: userID, Email: "anna@example.test", Name: "Anna", Token: "inv", ExpiresAt: t0, MarinaName: "M"})
		})
		if err != nil {
			t.Fatal(err)
		}
		for range maxAttempts + 2 {
			if _, err := s.DeliverDue(ctx, 10); err != nil {
				t.Fatal(err)
			}
			c.Advance(2 * time.Hour)
		}
		rows, _ := store.ListMessages(ctx, db.DB)
		if rows[0].Status != store.MessageFailed || rows[0].Attempts != maxAttempts || rows[0].LastError == "" {
			t.Fatalf("row: %+v", rows[0])
		}
	})
}

func TestDeliver_ReclaimsStuckSending(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		c := clock.NewFake(t0)
		tr := &flakyTransport{}
		s := newTestService(t, db, c, tr)
		userID := seedUser(t, db)
		_ = db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			return s.EnqueuePasswordReset(ctx, tx, PasswordResetMail{UserID: userID, Email: "a@b.test", Token: "x", ExpiresAt: t0})
		})
		// A crashed worker claimed it and never finished.
		if rows, _ := store.ClaimMessages(ctx, db.DB, t0, deliveryLease, 10); len(rows) != 1 {
			t.Fatal("claim")
		}
		if n, _ := s.DeliverDue(ctx, 10); n != 0 {
			t.Fatal("leased message delivered")
		}
		c.Advance(deliveryLease + time.Second)
		if n, _ := s.DeliverDue(ctx, 10); n != 1 {
			t.Fatal("stuck message not reclaimed")
		}
	})
}
