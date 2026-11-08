//go:build integration

package onlineconf_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func newSvc(e *fixture.Env) *onlineconf.Service {
	return onlineconf.NewService(onlineconf.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox})
}

func TestOnlineSettings_IfMatchAndValidation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		svc, ctx := newSvc(e), fixture.Ctx()
		s, err := svc.Get(ctx, e.Owner, e.MarinaID)
		if err != nil || s.Saved() || s.MinNights != 1 {
			t.Fatalf("defaults %+v %v", s, err)
		}
		in := onlineconf.Defaults()
		in.MinNights, in.PaymentMode = 2, onlineconf.PaymentDeposit
		if _, err := svc.Update(ctx, e.Owner, e.MarinaID, 1, in); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("If-Match on a never-saved row: %v", err)
		}
		bad := in
		bad.MaxNights = 0
		var ve *auth.ValidationError
		if _, err := svc.Update(ctx, e.Owner, e.MarinaID, 0, bad); !errors.As(err, &ve) {
			t.Fatalf("invalid settings: %v", err)
		}
		out, err := svc.Update(ctx, e.Owner, e.MarinaID, 0, in)
		if err != nil || out.Version != 1 || out.MinNights != 2 {
			t.Fatalf("first save %+v %v", out, err)
		}
		if _, err := svc.Update(ctx, e.Owner, e.MarinaID, 0, in); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("second create: %v", err)
		}
		in.MaxLiveHolds = 7
		out, err = svc.Update(ctx, e.Owner, e.MarinaID, 1, in)
		if err != nil || out.Version != 2 || out.MaxLiveHolds != 7 {
			t.Fatalf("second save %+v %v", out, err)
		}
		if e.AuditCount(t, "online_booking_settings.update") != 2 {
			t.Fatal("audit rows missing")
		}
		loaded, err := onlineconf.Load(ctx, db.DB, scopeOf(t, db, e.MarinaID))
		if err != nil || loaded.Version != 2 || loaded.PaymentMode != onlineconf.PaymentDeposit {
			t.Fatalf("load %+v %v", loaded, err)
		}

		// Concurrent writers with the same If-Match: exactly one wins.
		var wg sync.WaitGroup
		wins := make(chan int, 20)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				x := in
				x.MaxLiveHolds = 10 + i
				if _, err := svc.Update(ctx, e.Owner, e.MarinaID, 2, x); err == nil {
					wins <- i
				}
			}(i)
		}
		wg.Wait()
		close(wins)
		if n := len(wins); n != 1 {
			t.Fatalf("%d concurrent winners", n)
		}

		// Isolation and permissions.
		_, _, other := e.OtherOrganization(t)
		if _, err := svc.Get(ctx, other, e.MarinaID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other organization: %v", err)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := svc.Update(ctx, dock, e.MarinaID, 3, in); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock: %v", err)
		}
		// Approval groups must belong to the marina.
		in.Approval.GroupIDs = []string{store.NewID()}
		if _, err := svc.Update(ctx, e.Owner, e.MarinaID, 3, in); !errors.As(err, &ve) {
			t.Fatalf("unknown group: %v", err)
		}
	})
}

func TestBookingTexts_Immutable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		svc, ctx := newSvc(e), fixture.Ctx()
		scope := scopeOf(t, db, e.MarinaID)
		cur, err := onlineconf.CurrentTexts(ctx, db.DB, scope)
		if err != nil || len(cur) != 0 {
			t.Fatalf("current %+v %v", cur, err)
		}
		v1, err := svc.PublishText(ctx, e.Owner, e.MarinaID, store.BookingTextTerms, onlineconf.LongText{It: "Condizioni 1"})
		if err != nil || v1.Version != 1 || v1.SHA256 != onlineconf.TextSHA256(onlineconf.LongText{It: "Condizioni 1"}) {
			t.Fatalf("v1 %+v %v", v1, err)
		}

		// Concurrent publishes of the same kind get distinct versions.
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := svc.PublishText(ctx, e.Owner, e.MarinaID, store.BookingTextTerms, onlineconf.LongText{It: "Condizioni N"}); err != nil &&
					!store.IsSerializationFailure(err) {
					t.Errorf("concurrent publish: %v", err)
				}
			}()
		}
		wg.Wait()
		all, err := svc.ListTexts(ctx, e.Owner, e.MarinaID, store.BookingTextTerms)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int]bool{}
		for _, x := range all {
			if seen[x.Version] {
				t.Fatalf("duplicate version %d", x.Version)
			}
			seen[x.Version] = true
		}
		if all[0].Version != len(all) {
			t.Fatalf("newest first %+v", all)
		}
		if _, err := svc.PublishText(ctx, e.Owner, e.MarinaID, store.BookingTextPrivacy, onlineconf.LongText{It: "Privacy"}); err != nil {
			t.Fatal(err)
		}
		cur, err = onlineconf.CurrentTexts(ctx, db.DB, scope)
		if err != nil || len(cur) != 2 {
			t.Fatalf("current %+v %v", cur, err)
		}
		for _, c := range cur {
			if c.Kind == store.BookingTextTerms && c.Version != len(all) {
				t.Fatalf("current terms %+v", c)
			}
		}

		// The database refuses any change of a published text.
		_, err = db.NewRaw("UPDATE booking_texts SET texts = ? WHERE id = ?", `{"it":"changed"}`, v1.ID).Exec(context.Background())
		if err == nil {
			t.Fatal("published text changed")
		}
		if _, err := svc.PublishText(ctx, e.Owner, e.MarinaID, "cookies", onlineconf.LongText{It: "x"}); err == nil {
			t.Fatal("unknown kind accepted")
		}
		if _, err := svc.PublishText(ctx, e.Owner, e.MarinaID, store.BookingTextTerms, onlineconf.LongText{En: "only english"}); err == nil {
			t.Fatal("text without Italian accepted")
		}
		if e.AuditCount(t, "booking_text.publish") < 3 {
			t.Fatal("publish audit missing")
		}
	})
}

func scopeOf(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
