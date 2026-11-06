//go:build integration

package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/httpsink"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

type harness struct {
	e    *fixture.Env
	svc  *webhook.Service
	sink *webhook.FanOutSink
	del  *webhook.Deliverer
	disp *event.Dispatcher
}

func newHarness(t *testing.T, db *store.DB) *harness {
	e := fixture.New(t, db)
	policy := event.EgressPolicy{AllowPrivate: true, AllowHTTP: true}
	svc := webhook.NewService(e.DB, e.Clock, e.Keys, e.Audit, e.Outbox, policy, e.Log)
	sink := webhook.NewFanOutSink(e.DB, e.Clock, svc)
	return &harness{
		e: e, svc: svc, sink: sink,
		del:  webhook.NewDeliverer(e.DB, e.Clock, e.Keys, event.NewEgress(policy, 5*time.Second), e.Audit, "test", e.Log),
		disp: event.NewDispatcher(e.DB, e.Clock, []ext.EventSink{sink}, nil, e.Log),
	}
}

func TestWebhook_MemberAddedIsSignedAndDelivered(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := fixture.Ctx()
		rcv := httpsink.New(t)
		secret, ep, err := h.svc.Create(ctx, h.e.Owner, h.e.MarinaID, webhook.CreateInput{URL: rcv.URL + "/hook", Events: []string{event.MemberAdded}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(secret, "whsec_") || ep.Version != 1 || ep.HasPreviousSecret {
			t.Fatalf("created %+v", ep)
		}
		tm := team.NewService(h.e.DB, h.e.Clock, h.e.Auth, h.e.Audit, h.e.Outbox, h.e.Log)
		if _, err := tm.AddMember(ctx, h.e.Owner, h.e.MarinaID, team.AddMemberInput{Email: "m@porto.test", Name: "M", Role: auth.RoleDock}); err != nil {
			t.Fatal(err)
		}
		if n, err := h.disp.DispatchPending(context.Background(), 100); err != nil || n != 1 {
			t.Fatalf("dispatch: %d %v", n, err)
		}
		select {
		case <-h.svc.Wake():
		default:
			t.Fatal("deliverer not woken by fan-out")
		}
		if n, err := h.del.DeliverDue(context.Background(), 10); err != nil || n != 1 {
			t.Fatalf("deliver: %d %v", n, err)
		}
		req := rcv.Requests()[0]
		body := req.Body
		if req.Header["Porto-Event"] != event.MemberAdded || req.Header["User-Agent"] != "Porto-Webhooks/test" ||
			req.Header["Content-Type"] != "application/json" || req.Header["Porto-Delivery"] == "" || req.Method != "POST" {
			t.Fatalf("headers %v", req.Header)
		}
		if err := event.VerifySignature(secret, req.Header["Porto-Signature"], body, h.e.Clock.Now(), 5*time.Minute); err != nil {
			t.Fatalf("signature: %v", err)
		}
		var env event.Envelope
		if err := json.Unmarshal(body, &env); err != nil || !strings.HasPrefix(env.ID, "evt_") || env.MarinaID != h.e.MarinaID ||
			strings.Contains(string(env.Data), "m@porto.test") {
			t.Fatalf("envelope %s", body)
		}
		page, _ := h.svc.ListDeliveries(ctx, h.e.Owner, h.e.MarinaID, ep.ID, "", "", 10)
		if len(page.Items) != 1 || page.Items[0].Status != store.DeliveryDelivered || page.Items[0].LastStatusCode != 200 ||
			strings.ContainsRune(page.Items[0].ResponseExcerpt, 0) {
			t.Fatalf("deliveries %+v", page.Items)
		}
		// A retried dispatch of the same event does not duplicate deliveries.
		evs := h.e.Events(t, event.MemberAdded)
		if err := h.sink.Handle(context.Background(), event.FromRow(&evs[0])); err != nil {
			t.Fatal(err)
		}
		page, _ = h.svc.ListDeliveries(ctx, h.e.Owner, h.e.MarinaID, ep.ID, "", "", 10)
		if len(page.Items) != 1 {
			t.Fatalf("duplicate deliveries: %d", len(page.Items))
		}
		// Not subscribed: a role change produces no delivery.
		members, _ := tm.ListMembers(ctx, h.e.Owner, h.e.MarinaID)
		for _, m := range members {
			if m.Scope == team.ScopeMarina {
				if _, err := tm.ChangeRole(ctx, h.e.Owner, h.e.MarinaID, m.UserID, auth.RoleOffice); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err := h.disp.DispatchPending(context.Background(), 100); err != nil {
			t.Fatal(err)
		}
		if page, _ = h.svc.ListDeliveries(ctx, h.e.Owner, h.e.MarinaID, ep.ID, "", "", 10); len(page.Items) != 1 {
			t.Fatalf("unsubscribed event delivered: %d", len(page.Items))
		}
	})
}

func TestDeliverer_BackoffAndDisable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := fixture.Ctx()
		rcv := httpsink.New(t)
		rcv.SetStatus(500)
		_, ep, err := h.svc.Create(ctx, h.e.Owner, h.e.MarinaID, webhook.CreateInput{URL: rcv.URL, Events: []string{"*"}})
		if err != nil {
			t.Fatal(err)
		}
		id, err := h.svc.SendTest(ctx, h.e.Owner, h.e.MarinaID, ep.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The dispatcher must not fan webhook.test out to "*" endpoints.
		if _, err := h.disp.DispatchPending(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		get := func() store.WebhookDelivery {
			d, err := store.GetWebhookDelivery(context.Background(), db.DB, mustScope(t, h), ep.ID, id)
			if err != nil {
				t.Fatal(err)
			}
			return *d
		}
		if n, _ := h.del.DeliverDue(context.Background(), 10); n != 0 {
			t.Fatal("500 counted as delivered")
		}
		d := get()
		if d.Status != store.DeliveryPending || d.Attempts != 1 || d.LastStatusCode != 500 || !d.NextAttemptAt.Equal(fixture.T0.Add(30*time.Second)) {
			t.Fatalf("after 1st failure %+v", d)
		}
		if n, _ := h.del.DeliverDue(context.Background(), 10); n != 0 || rcv.Count() != 1 {
			t.Fatal("retried before backoff")
		}
		h.e.Clock.Advance(30 * time.Second)
		_, _ = h.del.DeliverDue(context.Background(), 10)
		if d = get(); d.Attempts != 2 || !d.NextAttemptAt.Equal(h.e.Clock.Now().Add(time.Minute)) {
			t.Fatalf("after 2nd failure %+v", d)
		}
		// Keep failing: after 24 h from the first attempt the delivery is failed.
		for d.Status == store.DeliveryPending {
			h.e.Clock.Set(d.NextAttemptAt)
			if _, err := h.del.DeliverDue(context.Background(), 10); err != nil {
				t.Fatal(err)
			}
			d = get()
		}
		if d.Status != store.DeliveryFailed || d.FailedAt.Sub(d.FirstAttemptAt) < webhook.GiveUpAfter {
			t.Fatalf("give up %+v", d)
		}
		// Redeliver resets the budget; more failures reach the disable threshold (20 consecutive).
		got, _ := h.svc.Get(ctx, h.e.Owner, h.e.MarinaID, ep.ID)
		failures := got.ConsecutiveFailures
		if err := h.svc.Redeliver(ctx, h.e.Owner, h.e.MarinaID, ep.ID, id); err != nil {
			t.Fatal(err)
		}
		for range webhook.DisableAfterFailures - failures {
			d = get()
			h.e.Clock.Set(d.NextAttemptAt)
			if _, err := h.del.DeliverDue(context.Background(), 10); err != nil {
				t.Fatal(err)
			}
		}
		got, _ = h.svc.Get(ctx, h.e.Owner, h.e.MarinaID, ep.ID)
		if got.Active || got.DisabledReason != "consecutive_failures" || got.DisabledAt.IsZero() {
			t.Fatalf("endpoint not disabled: %+v", got)
		}
		if h.e.AuditCount(t, "webhook.disable") != 1 {
			t.Fatal("disable not audited")
		}
		if _, err := h.svc.SendTest(ctx, h.e.Owner, h.e.MarinaID, ep.ID); !errors.Is(err, webhook.ErrEndpointDisabled) {
			t.Fatalf("test on disabled endpoint: %v", err)
		}
		// Re-enabling resets the counter; a success then works.
		rcv.SetStatus(204)
		got, err = h.svc.Update(ctx, h.e.Owner, h.e.MarinaID, ep.ID, got.Version, webhook.UpdateInput{Active: ptr(true)})
		if err != nil || !got.Active || got.ConsecutiveFailures != 0 {
			t.Fatalf("re-enable %+v %v", got, err)
		}
		if _, err := h.svc.Update(ctx, h.e.Owner, h.e.MarinaID, ep.ID, 1, webhook.UpdateInput{Active: ptr(true)}); !errors.Is(err, webhook.ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		if _, err := h.svc.SendTest(ctx, h.e.Owner, h.e.MarinaID, ep.ID); err != nil {
			t.Fatal(err)
		}
		if n, _ := h.del.DeliverDue(context.Background(), 10); n != 1 {
			t.Fatal("delivery after re-enable failed")
		}
		// Retention removes finished deliveries older than 30 days.
		h.e.Clock.Advance(webhook.Retention + time.Hour)
		if n, err := h.svc.PurgeOld(context.Background(), webhook.Retention); err != nil || n < 1 {
			t.Fatalf("purge: %d %v", n, err)
		}
	})
}

func ptr[T any](v T) *T { return &v }

func mustScope(t *testing.T, h *harness) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), h.e.DB.DB, h.e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDeliverer_RotationSendsBothSignatures(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := fixture.Ctx()
		rcv := httpsink.New(t)
		oldSecret, ep, err := h.svc.Create(ctx, h.e.Owner, h.e.MarinaID, webhook.CreateInput{URL: rcv.URL, Events: []string{event.MarinaUpdated}})
		if err != nil {
			t.Fatal(err)
		}
		newSecret, until, err := h.svc.RotateSecretUntil(ctx, h.e.Owner, h.e.MarinaID, ep.ID)
		if err != nil || newSecret == oldSecret || !until.Equal(fixture.T0.Add(webhook.PreviousSecretTTL)) {
			t.Fatalf("rotate: %v", err)
		}
		if got, _ := h.svc.Get(ctx, h.e.Owner, h.e.MarinaID, ep.ID); !got.HasPreviousSecret {
			t.Fatal("previous secret not reported")
		}
		send := func() (httpsink.Request, []byte) {
			if _, err := h.svc.SendTest(ctx, h.e.Owner, h.e.MarinaID, ep.ID); err != nil {
				t.Fatal(err)
			}
			if n, err := h.del.DeliverDue(context.Background(), 10); err != nil || n != 1 {
				t.Fatalf("deliver %d %v", n, err)
			}
			last := rcv.Last()
			return last, last.Body
		}
		req, body := send()
		sig := req.Header["Porto-Signature"]
		if strings.Count(sig, "v1=") != 2 {
			t.Fatalf("want two signatures during rotation: %s", sig)
		}
		for _, s := range []string{oldSecret, newSecret} {
			if err := event.VerifySignature(s, sig, body, h.e.Clock.Now(), time.Minute); err != nil {
				t.Fatalf("verify: %v", err)
			}
		}
		h.e.Clock.Advance(webhook.PreviousSecretTTL + time.Second)
		req, body = send()
		sig = req.Header["Porto-Signature"]
		if strings.Count(sig, "v1=") != 1 || event.VerifySignature(oldSecret, sig, body, h.e.Clock.Now(), time.Minute) == nil {
			t.Fatalf("old secret still signing after 24h: %s", sig)
		}
	})
}

func TestWebhook_Validation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		strict := webhook.NewService(e.DB, e.Clock, e.Keys, e.Audit, e.Outbox, event.EgressPolicy{}, e.Log)
		ctx := fixture.Ctx()
		for _, u := range []string{"http://hooks.example.com", "https://127.0.0.1/x", "https://169.254.169.254/", "ftp://x", "", "https://user:pw@hooks.example.com"} {
			if _, _, err := strict.Create(ctx, e.Owner, e.MarinaID, webhook.CreateInput{URL: u, Events: []string{"*"}}); !errors.Is(err, webhook.ErrInvalidURL) {
				t.Errorf("%q: %v", u, err)
			}
		}
		for _, evs := range [][]string{{}, {"nope"}, {event.WebhookTest}} {
			if _, _, err := strict.Create(ctx, e.Owner, e.MarinaID, webhook.CreateInput{URL: "https://hooks.example.com", Events: evs}); !errors.Is(err, webhook.ErrUnknownEventType) {
				t.Errorf("events %v: %v", evs, err)
			}
		}
		_, ep, err := strict.Create(ctx, e.Owner, e.MarinaID, webhook.CreateInput{URL: "https://hooks.example.com/porto", Events: []string{event.MemberAdded, "*"}})
		if err != nil || len(ep.Events) != 1 || ep.Events[0] != "*" {
			t.Fatalf("create %+v %v", ep, err)
		}
		dir := e.AddUser(t, "dir@porto.test", auth.RoleDirector, e.OrgID, e.MarinaID)
		if _, err := strict.List(ctx, dir, e.MarinaID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("director lists webhooks: %v", err)
		}
		if err := strict.Delete(ctx, e.Owner, e.MarinaID, ep.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := strict.Get(ctx, e.Owner, e.MarinaID, ep.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("deleted endpoint: %v", err)
		}
	})
}
