//go:build integration

package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func seedOrgMarina(t *testing.T, db *store.DB, slug string) (orgID, marinaID string) {
	t.Helper()
	ctx := context.Background()
	o := &store.Organization{ID: store.NewID(), Name: "Org " + slug, BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: t0, UpdatedAt: t0}
	if err := store.InsertOrganization(ctx, db.DB, o); err != nil {
		t.Fatal(err)
	}
	m := &store.Marina{
		ID: store.NewID(), OrganizationID: o.ID, Slug: slug, Name: "Marina " + slug, Kind: "marina",
		Timezone: "Europe/Rome", Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"},
		Contacts: store.JSON(`{}`), Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1,
		Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertMarina(ctx, db.DB, m); err != nil {
		t.Fatal(err)
	}
	return o.ID, m.ID
}

func TestRunInTx_AfterCommitRunsOnlyOnCommit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		var ran int
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			store.AfterCommit(ctx, func() { ran++ })
			return errors.New("boom")
		})
		if err == nil || ran != 0 {
			t.Fatalf("rollback: err=%v ran=%d", err, ran)
		}
		err = db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			store.AfterCommit(ctx, func() { ran++ })
			return nil
		})
		if err != nil || ran != 1 {
			t.Fatalf("commit: err=%v ran=%d", err, ran)
		}
	})
}

func TestScopes_ArchivedAndMalformed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		orgID, marinaID := seedOrgMarina(t, db, "scope-test")
		s, err := store.ResolveMarinaScope(ctx, db.DB, marinaID)
		if err != nil || s.ID() != marinaID || s.OrgID() != orgID {
			t.Fatalf("resolve: %+v %v", s, err)
		}
		for _, bad := range []string{"", "x", "not-a-uuid-at-all-but-36-characters!", store.NewID()} {
			if _, err := store.ResolveMarinaScope(ctx, db.DB, bad); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("resolve %q: %v", bad, err)
			}
			if _, err := store.ResolveOrgScope(ctx, db.DB, bad); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("resolve org %q: %v", bad, err)
			}
		}
		if err := store.ArchiveMarina(ctx, db.DB, s, t0); err != nil {
			t.Fatal(err)
		}
		if err := store.ArchiveMarina(ctx, db.DB, s, t0); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("archive twice: %v", err)
		}
		if _, err := store.ResolveMarinaScope(ctx, db.DB, marinaID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("archived marina resolved: %v", err)
		}
		if _, err := store.SystemMarinaScope(ctx, db.DB, marinaID); err != nil {
			t.Fatalf("system scope of archived marina: %v", err)
		}
	})
}

func TestUpdateMarina_VersionMismatch(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, marinaID := seedOrgMarina(t, db, "version-test")
		s, _ := store.ResolveMarinaScope(ctx, db.DB, marinaID)
		m, _ := store.GetMarina(ctx, db.DB, s)
		m.Name = "Renamed"
		m.BookingEnabled = true
		if err := store.UpdateMarina(ctx, db.DB, s, 1, m); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateMarina(ctx, db.DB, s, 1, m); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		got, _ := store.GetMarina(ctx, db.DB, s)
		if got.Version != 2 || got.Name != "Renamed" || !got.BookingEnabled {
			t.Fatalf("after update: %+v", got)
		}
	})
}

func TestConcurrency_WriteQueue(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		orgID, _ := seedOrgMarina(t, db, "queue-test")
		const n = 100
		var wg sync.WaitGroup
		errs := make(chan error, 2*n)
		for range n {
			wg.Go(func() {
				errs <- db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
					var v int
					if err := tx.NewSelect().Table("organizations").Column("version").Where("id = ?", orgID).Scan(ctx, &v); err != nil {
						return err
					}
					_, err := tx.NewUpdate().Table("organizations").Set("version = ?", v+1).Where("id = ?", orgID).
						Where("version = ?", v).Exec(ctx)
					return err
				})
			})
		}
		// Autocommit writes and reads interleave with the transactions.
		for range n {
			wg.Go(func() {
				_, err := db.NewUpdate().Table("organizations").Set("updated_at = ?", t0).Where("id = ?", orgID).Exec(ctx)
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var v int
		if err := db.NewSelect().Table("organizations").Column("version").Where("id = ?", orgID).Scan(ctx, &v); err != nil {
			t.Fatal(err)
		}
		if db.IsPostgres() {
			// READ COMMITTED: lost updates are expected without a row lock; the point here is no error.
			return
		}
		if v != n+1 {
			t.Fatalf("version = %d, want %d (lost updates)", v, n+1)
		}
	})
}

func TestConcurrency_IdempotencyKeySingleClaim(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		const n = 100
		var winners atomic.Int32
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for range n {
			wg.Go(func() {
				_, created, err := store.ClaimIdempotencyKey(ctx, db.DB, "user:1 POST /x", "key-123456", "h", t0, t0.Add(24*time.Hour))
				if created {
					winners.Add(1)
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if winners.Load() != 1 {
			t.Fatalf("winners = %d, want 1", winners.Load())
		}
		row, created, err := store.ClaimIdempotencyKey(ctx, db.DB, "user:1 POST /x", "key-123456", "h", t0, t0.Add(time.Hour))
		if err != nil || created || row.State != store.IdempotencyInProgress {
			t.Fatalf("replay claim: %+v %v %v", row, created, err)
		}
		if err := store.CompleteIdempotencyKey(ctx, db.DB, row.ID, 201, "application/json", []byte(`{"ok":true}`), t0); err != nil {
			t.Fatal(err)
		}
		row, _, _ = store.ClaimIdempotencyKey(ctx, db.DB, "user:1 POST /x", "key-123456", "h", t0, t0.Add(time.Hour))
		if row.State != store.IdempotencyCompleted || row.ResponseStatus != 201 || string(row.ResponseBody) != `{"ok":true}` {
			t.Fatalf("completed row: %+v", row)
		}
		// Expired keys are replaced.
		later := t0.Add(25 * time.Hour)
		_, created, err = store.ClaimIdempotencyKey(ctx, db.DB, "user:1 POST /x", "key-123456", "h2", later, later.Add(time.Hour))
		if err != nil || !created {
			t.Fatalf("expired key not replaced: %v %v", created, err)
		}
	})
}

func TestConcurrency_OutboxClaimNoDoubleDispatch(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		const events = 300
		for i := range events {
			e := &store.OutboxEvent{
				ID: store.NewID(), Type: "test.event", Version: 1, Payload: store.JSON(fmt.Sprintf(`{"i":%d}`, i)),
				OccurredAt: t0, CreatedAt: t0, UpdatedAt: t0,
			}
			if err := store.InsertOutboxEvent(ctx, db.DB, e); err != nil {
				t.Fatal(err)
			}
		}
		var mu sync.Mutex
		seen := map[string]int{}
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for range 100 {
			wg.Go(func() {
				for {
					var rows []store.OutboxEvent
					err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
						var err error
						rows, err = store.ClaimOutboxEvents(ctx, tx, t0, time.Minute, 5)
						return err
					})
					if err != nil {
						errs <- err
						return
					}
					if len(rows) == 0 {
						return
					}
					for _, r := range rows {
						mu.Lock()
						seen[r.ID]++
						mu.Unlock()
						if err := store.MarkOutboxDispatched(ctx, db.DB, r.ID, t0); err != nil {
							errs <- err
							return
						}
					}
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if len(seen) != events {
			t.Fatalf("claimed %d distinct events, want %d", len(seen), events)
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("event %s claimed %d times", id, n)
			}
		}
		if n, _ := store.CountPendingOutbox(ctx, db.DB); n != 0 {
			t.Fatalf("pending = %d", n)
		}
	})
}

func TestOutboxClaim_LeaseAndRetry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		e := &store.OutboxEvent{ID: store.NewID(), Type: "t", Version: 1, Payload: store.JSON(`{}`), OccurredAt: t0, CreatedAt: t0, UpdatedAt: t0}
		if err := store.InsertOutboxEvent(ctx, db.DB, e); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ClaimOutboxEvents(ctx, db.DB, t0, time.Minute, 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("claim: %v %v", rows, err)
		}
		if rows, _ := store.ClaimOutboxEvents(ctx, db.DB, t0.Add(30*time.Second), time.Minute, 10); len(rows) != 0 {
			t.Fatal("leased event claimed again")
		}
		if rows, _ := store.ClaimOutboxEvents(ctx, db.DB, t0.Add(61*time.Second), time.Minute, 10); len(rows) != 1 {
			t.Fatal("expired lease not reclaimed")
		}
		if err := store.MarkOutboxRetry(ctx, db.DB, e.ID, t0.Add(10*time.Minute), "sink failed", t0); err != nil {
			t.Fatal(err)
		}
		if rows, _ := store.ClaimOutboxEvents(ctx, db.DB, t0.Add(5*time.Minute), time.Minute, 10); len(rows) != 0 {
			t.Fatal("event claimed before next_attempt_at")
		}
		rows, _ = store.ClaimOutboxEvents(ctx, db.DB, t0.Add(11*time.Minute), time.Minute, 10)
		if len(rows) != 1 || rows[0].Attempts != 1 || rows[0].LastError != "sink failed" {
			t.Fatalf("retry row: %+v", rows)
		}
	})
}

func TestAdvisoryLock_Exclusive(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		unlock, ok, err := db.TryAdvisoryLock(ctx, "job.a")
		if err != nil || !ok {
			t.Fatalf("first lock: %v %v", ok, err)
		}
		if _, ok, _ := db.TryAdvisoryLock(ctx, "job.a"); ok {
			t.Fatal("second lock acquired")
		}
		other, ok, _ := db.TryAdvisoryLock(ctx, "job.b")
		if !ok {
			t.Fatal("other name blocked")
		}
		other()
		unlock()
		again, ok, _ := db.TryAdvisoryLock(ctx, "job.a")
		if !ok {
			t.Fatal("lock not released")
		}
		again()
	})
}

func TestAudit_ListPagination(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, marinaID := seedOrgMarina(t, db, "audit-test")
		s, _ := store.ResolveMarinaScope(ctx, db.DB, marinaID)
		for i := range 5 {
			e := &store.AuditEntry{
				ID: store.NewID(), MarinaID: marinaID, ActorType: "system", Action: "a", EntityType: "x",
				EntityID: fmt.Sprint(i), Diff: store.JSON(`{"k":1}`), CreatedAt: t0.Add(time.Duration(i) * time.Second),
			}
			if err := store.InsertAudit(ctx, db.DB, e); err != nil {
				t.Fatal(err)
			}
		}
		p1, err := store.ListAudit(ctx, db.DB, s, store.AuditFilter{}, "", 2)
		if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" || p1.Items[0].EntityID != "4" {
			t.Fatalf("page 1: %+v %v", p1, err)
		}
		p2, _ := store.ListAudit(ctx, db.DB, s, store.AuditFilter{}, p1.NextCursor, 2)
		p3, _ := store.ListAudit(ctx, db.DB, s, store.AuditFilter{}, p2.NextCursor, 2)
		if len(p2.Items) != 2 || len(p3.Items) != 1 || p3.NextCursor != "" || p3.Items[0].EntityID != "0" {
			t.Fatalf("pages: %+v %+v", p2, p3)
		}
		f, _ := store.ListAudit(ctx, db.DB, s, store.AuditFilter{EntityID: "3"}, "", 10)
		if len(f.Items) != 1 || string(f.Items[0].Diff.Compact()) != `{"k":1}` {
			t.Fatalf("filter: %+v", f)
		}
		if _, err := store.ListAudit(ctx, db.DB, s, store.AuditFilter{}, "garbage", 2); !errors.Is(err, store.ErrInvalidCursor) {
			t.Fatalf("bad cursor: %v", err)
		}
	})
}
