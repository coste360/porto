//go:build integration

package comms_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	cust  *customer.Service
	links *magiclink.Service
	svc   *comms.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	d := docs.NewService(docs.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Notify: e.Notify, Log: e.Log})
	l := magiclink.NewService(magiclink.Deps{DB: db, Clock: e.Clock, BaseURL: "https://porto.test"})
	svc := comms.NewService(comms.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Docs: d, Links: l, Notify: e.Notify, Log: e.Log})
	return &env{
		Env: e, links: l, svc: svc,
		cust: customer.NewService(customer.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
	}
}

func ptr[T any](v T) *T { return &v }

var contractNumber atomic.Int64

func nextNumber() int64 { return contractNumber.Add(1) }

// customer creates a customer with an email (optional), a marketing consent (optional) and an active contract
// (optional).
func (e *env) customer(t *testing.T, name, email string, marketing, contract bool) string {
	t.Helper()
	ctx := fixture.Ctx()
	p := customer.Patch{Kind: ptr(customer.KindPerson), FirstName: ptr(name), LastName: ptr("Test")}
	if email != "" {
		p.Emails = &[]customer.Contact{{Label: "home", Value: email}}
	}
	res, err := e.cust.Create(ctx, e.Owner, e.MarinaID, p)
	if err != nil {
		t.Fatal(err)
	}
	id := res.Customer.ID
	if marketing {
		if _, err := e.cust.RecordConsent(ctx, e.Owner, e.MarinaID, id, customer.ConsentInput{Kind: "marketing", Granted: true, Source: "desk", PolicyVersion: "v1"}); err != nil {
			t.Fatal(err)
		}
	}
	if contract {
		now := store.UTC(e.Clock.Now())
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: e.MarinaID, Name: "V-" + name, HullType: "other", LOACm: 900, BeamCm: 300,
			DimensionsSource: "declared", Engines: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertVessel(context.Background(), e.DB.DB, v); err != nil {
			t.Fatal(err)
		}
		_, err := e.DB.NewRaw(`INSERT INTO contracts (id, marina_id, kind, term, number, year, status, customer_id, vessel_id, starts_on, ends_on, created_at, updated_at)
			VALUES (?, ?, 'mooring', 'annual', ?, 2026, 'active', ?, ?, ?, ?, ?, ?)`,
			store.NewID(), e.MarinaID, nextNumber(), id, v.ID, "2026-01-01", "2027-01-01", now, now).Exec(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func (e *env) campaign(t *testing.T, category string, ids []string) *comms.Communication {
	t.Helper()
	c, err := e.svc.Create(fixture.Ctx(), e.Owner, e.MarinaID, comms.Input{
		Name: "Avviso", Category: category, Audience: comms.Audience{CustomerIDs: ids},
		Subject: "Novità per {{customer.first_name}}", BodyMD: "# Ciao {{customer.display_name}}\n\nIl porto **{{marina.name}}** ti informa.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) sendNow(t *testing.T, c *comms.Communication) *comms.Communication {
	t.Helper()
	ctx := fixture.Ctx()
	out, err := e.svc.Send(ctx, e.Owner, e.MarinaID, c.ID, c.Version)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := e.svc.RunSend(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.svc.Get(ctx, e.Owner, e.MarinaID, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestComms_ConsentRules(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		withConsent := e.customer(t, "Anna", "anna@example.test", true, false)
		noConsent := e.customer(t, "Bruno", "bruno@example.test", false, true)
		noEmail := e.customer(t, "Carla", "", true, true)
		ids := []string{withConsent, noConsent, noEmail}

		promo := e.campaign(t, comms.CategoryPromotional, ids)
		pv, err := e.svc.PreviewCommunication(ctx, e.Owner, e.MarinaID, promo.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if pv.Count != 3 || pv.SkippedNoConsent != 1 || pv.SkippedNoEmail != 1 || pv.Sample == nil ||
			pv.Sample.Subject != "Novità per Anna" || !strings.Contains(pv.Sample.HTML, "<strong>Porto Test</strong>") && !strings.Contains(pv.Sample.HTML, "<strong>") {
			t.Fatalf("preview %+v", pv)
		}
		got := e.sendNow(t, promo)
		if got.Status != store.CommSent || got.CountsOf["queued"] != 1 || got.CountsOf["skipped_no_consent"] != 1 || got.CountsOf["skipped_no_email"] != 1 {
			t.Fatalf("promotional %+v", got.CountsOf)
		}
		// Service: active relation needed, no marketing consent needed.
		svc := e.sendNow(t, e.campaign(t, comms.CategoryService, ids))
		if svc.CountsOf["queued"] != 1 || svc.CountsOf["skipped_no_consent"] != 1 || svc.CountsOf["skipped_no_email"] != 1 {
			t.Fatalf("service %+v", svc.CountsOf)
		}
		page, err := e.svc.CustomerMessages(ctx, e.Owner, e.MarinaID, withConsent, "", 50)
		if err != nil || len(page.Items) != 1 || page.Items[0].Category != "promotional" {
			t.Fatalf("registry %+v %v", page.Items, err)
		}
		// A body with an unknown field is template_invalid.
		_, err = e.svc.Create(ctx, e.Owner, e.MarinaID, comms.Input{Name: "x", Category: comms.CategoryService, BodyMD: "{{customer.nope}}"})
		if !errors.Is(err, docs.ErrTemplateInvalid) {
			t.Fatalf("invalid body: %v", err)
		}
	})
}

var reUnsub = regexp.MustCompile(`/m/unsubscribe/([0-9A-Z]{52})`)

func TestComms_Unsubscribe(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		id := e.customer(t, "Dario", "dario@example.test", true, true)
		e.sendNow(t, e.campaign(t, comms.CategoryPromotional, []string{id}))
		if _, err := e.Notify.DeliverDue(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		msgs := e.Mailbox.Messages()
		if len(msgs) != 1 || msgs[0].Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" ||
			!strings.Contains(msgs[0].Headers["List-Unsubscribe"], "/api/public/v1/marina-test/links/") {
			t.Fatalf("promotional headers %+v", msgs)
		}
		m := reUnsub.FindStringSubmatch(msgs[0].Text)
		if m == nil {
			t.Fatal("no unsubscribe link")
		}
		l, err := e.links.Resolve(ctx, "marina-test", m[1])
		if err != nil {
			t.Fatal(err)
		}
		if sub, _ := e.svc.PublicView(ctx, l); !sub {
			t.Fatal("subscribed customer shown as unsubscribed")
		}
		for range 2 { // idempotent
			if err := e.svc.Unsubscribe(ctx, "marina-test", m[1]); err != nil {
				t.Fatal(err)
			}
		}
		if sub, _ := e.svc.PublicView(ctx, l); sub {
			t.Fatal("still subscribed")
		}
		if e.AuditCount(t, "consent.unsubscribe") != 1 {
			t.Fatal("unsubscription not recorded once")
		}
		if err := e.svc.Unsubscribe(ctx, "marina-altra", m[1]); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatalf("other slug: %v", err)
		}
		// The next promotional send skips the customer, a service send still reaches them.
		promo := e.sendNow(t, e.campaign(t, comms.CategoryPromotional, []string{id}))
		svc := e.sendNow(t, e.campaign(t, comms.CategoryService, []string{id}))
		if promo.CountsOf["skipped_no_consent"] != 1 || svc.CountsOf["queued"] != 1 {
			t.Fatalf("after unsubscribe: promo %v service %v", promo.CountsOf, svc.CountsOf)
		}
	})
}

func TestComms_SendJobIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		var ids []string
		for _, n := range []string{"E", "F", "G", "H"} {
			ids = append(ids, e.customer(t, n, strings.ToLower(n)+"@example.test", true, false))
		}
		c := e.campaign(t, comms.CategoryPromotional, ids)
		sched, err := e.svc.Schedule(ctx, e.Owner, e.MarinaID, c.ID, c.Version, e.Clock.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err := e.svc.RunSend(ctx); err != nil {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().Table("comm_recipients").Count(ctx); n != 0 {
			t.Fatal("scheduled communication sent early")
		}
		if _, err := e.svc.Update(ctx, e.Owner, e.MarinaID, c.ID, sched.Version, comms.Patch{Name: ptr("x")}); err == nil {
			t.Fatal("scheduled communication edited")
		}
		e.Clock.Advance(2 * time.Hour)
		for range 4 {
			if err := e.svc.RunSend(ctx); err != nil {
				t.Fatal(err)
			}
		}
		n, _ := db.NewSelect().Table("message_log").Where("template = ?", "communication").Count(ctx)
		if n != 4 {
			t.Fatalf("%d messages, want 4", n)
		}
		page, _ := e.svc.Recipients(ctx, e.Owner, e.MarinaID, c.ID, "", "", 50)
		if len(page.Items) != 4 || page.Items[0].Status != store.RecipientQueued || page.Items[0].DisplayName == "" {
			t.Fatalf("recipients %+v", page.Items)
		}
		if _, err := e.svc.Cancel(ctx, e.Owner, e.MarinaID, c.ID, 0); err == nil {
			t.Fatal("sent communication cancelled")
		}
	})
}
