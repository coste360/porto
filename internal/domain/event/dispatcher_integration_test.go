//go:build integration

package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

type sink struct {
	fail atomic.Int32 // remaining failures
	seen atomic.Int32
}

func (s *sink) Name() string { return "test" }

func (s *sink) Handle(context.Context, ext.Event) error {
	s.seen.Add(1)
	if s.fail.Add(-1) >= 0 {
		return errors.New("downstream unavailable")
	}
	return nil
}

func seedMarina(t *testing.T, db *store.DB) store.MarinaScope {
	t.Helper()
	ctx := context.Background()
	o := &store.Organization{ID: store.NewID(), Name: "O", BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: t0, UpdatedAt: t0}
	if err := store.InsertOrganization(ctx, db.DB, o); err != nil {
		t.Fatal(err)
	}
	m := &store.Marina{
		ID: store.NewID(), OrganizationID: o.ID, Slug: "evt-test", Name: "M", Kind: "marina", Timezone: "Europe/Rome",
		Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"}, Contacts: store.JSON(`{}`), Address: store.JSON(`{}`),
		Settings: store.JSON(`{}`), SettingsVersion: 1, Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1,
		CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertMarina(ctx, db.DB, m); err != nil {
		t.Fatal(err)
	}
	s, err := store.ResolveMarinaScope(ctx, db.DB, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDispatcher_RetryWithBackoffThenPublish(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		c := clock.NewFake(t0)
		scope := seedMarina(t, db)
		ob := event.NewOutbox(c)
		var written ext.Event
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			written, err = ob.Write(ctx, tx, &scope, event.MemberAdded, event.MemberData{UserID: store.NewID(), Role: "dock", Scope: "marina"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-ob.Wake():
		default:
			t.Fatal("outbox did not wake the dispatcher after commit")
		}
		if _, err := ob.Write(ctx, db.DB, &scope, "unknown.type", nil); err == nil {
			t.Fatal("unknown type written")
		}

		s := &sink{}
		s.fail.Store(2)
		bus := event.NewBroker(slog.New(slog.DiscardHandler), 8)
		sub, cancel, _ := bus.Subscribe(ctx, scope.ID())
		defer cancel()
		d := event.NewDispatcher(db, c, []ext.EventSink{s}, bus, slog.New(slog.DiscardHandler))

		if n, err := d.DispatchPending(ctx, 10); err != nil || n != 0 {
			t.Fatalf("1st: %d %v", n, err)
		}
		if n, _ := d.DispatchPending(ctx, 10); n != 0 || s.seen.Load() != 1 {
			t.Fatal("retried before backoff")
		}
		c.Advance(30 * time.Second)
		if n, _ := d.DispatchPending(ctx, 10); n != 0 {
			t.Fatal("2nd attempt should fail")
		}
		c.Advance(time.Minute)
		if n, err := d.DispatchPending(ctx, 10); err != nil || n != 1 {
			t.Fatalf("3rd: %d %v", n, err)
		}
		select {
		case ev := <-sub:
			if ev.ID != written.ID || ev.Type != event.MemberAdded {
				t.Fatalf("published %+v", ev)
			}
			var data map[string]any
			_ = json.Unmarshal(ev.Data, &data)
			if err := event.CheckNoPII(data); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("not published on the bus")
		}
		if n, _ := store.CountPendingOutbox(ctx, db.DB); n != 0 {
			t.Fatal("event still pending")
		}
		row, _ := store.GetOutboxEvent(ctx, db.DB, written.ID)
		if row.Attempts != 2 || row.DispatchedAt.IsZero() || row.LastError != "" {
			t.Fatalf("row %+v", row)
		}
	})
}
