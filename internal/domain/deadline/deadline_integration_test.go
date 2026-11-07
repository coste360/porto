//go:build integration

package deadline_test

import (
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	cust *customer.Service
	ves  *vessel.Service
	docs *document.Service
	dl   *deadline.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	return &env{
		Env:  e,
		cust: customer.NewService(customer.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		ves:  vessel.NewService(vessel.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		docs: document.NewService(document.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		dl:   deadline.NewService(deadline.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
	}
}

func ptr[T any](v T) *T { return &v }

func (e *env) customer(t *testing.T) string {
	t.Helper()
	res, err := e.cust.Create(fixture.Ctx(), e.Owner, e.MarinaID, customer.Patch{Kind: ptr(customer.KindPerson), LastName: ptr("Rossi")})
	if err != nil {
		t.Fatal(err)
	}
	return res.Customer.ID
}

func (e *env) all(t *testing.T, f deadline.Filter) []deadline.Deadline {
	t.Helper()
	page, err := e.dl.List(fixture.Ctx(), e.Owner, e.MarinaID, f, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func TestDeadline_SyncedWithDocument(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid := e.customer(t)
		exp := calendar.MustParse("2027-03-01")
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: cid, Type: "passport", ExpiresOn: exp})
		if err != nil {
			t.Fatal(err)
		}
		got := e.all(t, deadline.Filter{})
		if len(got) != 1 || got[0].Kind != deadline.KindIdentityDocument || !got[0].DueOn.Equal(exp) || got[0].SourceID != d.Row.ID ||
			got[0].Customer == nil || got[0].Customer.Name != "Rossi" || got[0].DocumentType != "passport" || got[0].Status != "open" {
			t.Fatalf("created %+v", got)
		}
		// Mark done, then change the expiry: the deadline reopens with the new date.
		done, err := e.dl.Update(ctx, e.Owner, e.MarinaID, got[0].ID, got[0].Version, deadline.Patch{Status: ptr("done"), Note: ptr("rinnovato")})
		if err != nil || done.Status != "done" {
			t.Fatalf("done %+v %v", done, err)
		}
		if _, err := e.dl.Update(ctx, e.Owner, e.MarinaID, got[0].ID, got[0].Version, deadline.Patch{Status: ptr("open")}); !errors.Is(err, deadline.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if len(e.all(t, deadline.Filter{})) != 0 || len(e.all(t, deadline.Filter{Status: "done"})) != 1 {
			t.Fatal("status filter")
		}
		exp2 := calendar.MustParse("2028-03-01")
		d2, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, 1, document.Patch{ExpiresOn: &exp2})
		if err != nil {
			t.Fatal(err)
		}
		got = e.all(t, deadline.Filter{})
		if len(got) != 1 || !got[0].DueOn.Equal(exp2) || got[0].Status != "open" || got[0].Note != "rinnovato" {
			t.Fatalf("after expiry change %+v", got)
		}
		// A type without deadline removes it; back to a typed document recreates it with the new kind.
		d3, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, d2.Row.Version, document.Patch{Type: ptr("photo")})
		if err != nil || len(e.all(t, deadline.Filter{Status: deadline.StatusAny})) != 0 {
			t.Fatalf("photo: %v", err)
		}
		d4, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, d3.Row.Version, document.Patch{Type: ptr("insurance_policy")})
		if err != nil {
			t.Fatal(err)
		}
		if got = e.all(t, deadline.Filter{}); len(got) != 1 || got[0].Kind != deadline.KindInsurance {
			t.Fatalf("insurance %+v", got)
		}
		// Removing the expiry removes the deadline.
		d5, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, d4.Row.Version, document.Patch{ExpiresOn: &calendar.Date{}})
		if err != nil || len(e.all(t, deadline.Filter{Status: deadline.StatusAny})) != 0 {
			t.Fatalf("no expiry: %v", err)
		}
		d6, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, d5.Row.Version, document.Patch{ExpiresOn: &exp})
		if err != nil || len(e.all(t, deadline.Filter{})) != 1 {
			t.Fatalf("expiry again: %v", err)
		}
		// Deleting the document deletes the deadline.
		if err := e.docs.Delete(ctx, e.Owner, e.MarinaID, d.Row.ID, d6.Row.Version-1); !errors.Is(err, document.ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
		if err := e.docs.Delete(ctx, e.Owner, e.MarinaID, d.Row.ID, d6.Row.Version); err != nil {
			t.Fatal(err)
		}
		if len(e.all(t, deadline.Filter{Status: deadline.StatusAny})) != 0 {
			t.Fatal("deadline left after delete")
		}
		// Vessel documents feed vessel deadlines.
		v, err := e.ves.Create(ctx, e.Owner, e.MarinaID, vessel.Patch{Name: ptr("Aurora"), LOACm: ptr(int64(1000)), BeamCm: ptr(int64(350))}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "vessel", VesselID: v.Row.ID, Type: "insurance_policy", ExpiresOn: exp}); err != nil {
			t.Fatal(err)
		}
		got = e.all(t, deadline.Filter{VesselID: v.Row.ID})
		if len(got) != 1 || got[0].Vessel == nil || got[0].Vessel.Name != "Aurora" || got[0].Customer != nil {
			t.Fatalf("vessel deadline %+v", got)
		}
		if len(e.Events(t, event.DeadlineChanged)) != 1 {
			t.Fatal("deadline.changed missing")
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.dl.Update(ctx, dock, e.MarinaID, got[0].ID, got[0].Version, deadline.Patch{Status: ptr("done")}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock: %v", err)
		}
	})
}

func TestDeadline_Urgency(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		// 23:30 UTC on 2026-12-31 is already 2027-01-01 in Rome (marina timezone).
		e.Clock.Set(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))
		cid := e.customer(t)
		mk := func(typ, due string) {
			t.Helper()
			if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: cid, Type: typ, ExpiresOn: calendar.MustParse(due)}); err != nil {
				t.Fatal(err)
			}
		}
		mk("passport", "2026-12-31")         // overdue (yesterday in Rome)
		mk("passport", "2027-01-01")         // due soon (today)
		mk("passport", "2027-01-31")         // due soon (30 days)
		mk("passport", "2027-02-01")         // later
		mk("insurance_policy", "2027-03-02") // due soon (60 days)
		mk("insurance_policy", "2027-03-03") // later
		urg := map[string]string{}
		for _, d := range e.all(t, deadline.Filter{}) {
			urg[d.Kind+" "+d.DueOn.String()] = d.Urgency
		}
		want := map[string]string{
			"identity_document 2026-12-31": "overdue", "identity_document 2027-01-01": "due_soon",
			"identity_document 2027-01-31": "due_soon", "identity_document 2027-02-01": "later",
			"insurance 2027-03-02": "due_soon", "insurance 2027-03-03": "later",
		}
		for k, w := range want {
			if urg[k] != w {
				t.Errorf("%s: %s, want %s", k, urg[k], w)
			}
		}
		// The urgency filter selects in SQL what the view computes.
		for _, u := range []string{"overdue", "due_soon", "later"} {
			items := e.all(t, deadline.Filter{Urgency: u})
			n := 0
			for _, w := range want {
				if w == u {
					n++
				}
			}
			if len(items) != n {
				t.Errorf("urgency %s: %d items, want %d", u, len(items), n)
			}
			for _, it := range items {
				if it.Urgency != u {
					t.Errorf("urgency %s returned %s", u, it.Urgency)
				}
			}
		}
		// Longer lead days for identity documents turn "later" into "due soon".
		if _, err := e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, []deadline.Policy{{Kind: deadline.KindIdentityDocument, LeadDays: []int{45}}}); err != nil {
			t.Fatal(err)
		}
		if items := e.all(t, deadline.Filter{Urgency: "later"}); len(items) != 1 || items[0].Kind != deadline.KindInsurance {
			t.Fatalf("later after policy %+v", items)
		}
		// Ordered by due date with a working cursor.
		p1, _ := e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{}, "", 4)
		p2, _ := e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{}, p1.NextCursor, 4)
		all := append(append([]deadline.Deadline{}, p1.Items...), p2.Items...)
		if len(p1.Items) != 4 || len(p2.Items) != 2 || p2.NextCursor != "" {
			t.Fatalf("pages %d %d", len(p1.Items), len(p2.Items))
		}
		for i := 1; i < len(all); i++ {
			if all[i].DueOn.Before(all[i-1].DueOn) {
				t.Fatalf("not ordered by due date at %d", i)
			}
		}
		if _, err := e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{}, "garbage", 4); !errors.Is(err, store.ErrInvalidCursor) {
			t.Fatalf("bad cursor: %v", err)
		}
		if _, err := e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{Urgency: "soon"}, "", 0); err == nil {
			t.Fatal("bad urgency accepted")
		}
	})
}

func TestDeadline_PoliciesDefaults(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		ps, err := e.dl.ListPolicies(ctx, e.Owner, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != len(deadline.AllKinds()) {
			t.Fatalf("policies %d", len(ps))
		}
		for _, p := range ps {
			want := deadline.DefaultLeadDays(p.Kind)
			if p.Kind == deadline.KindInsurance && want[0] != 60 {
				t.Fatal("insurance default")
			}
			if len(p.LeadDays) != len(want) || p.LeadDays[0] != want[0] {
				t.Errorf("%s: %v", p.Kind, p.LeadDays)
			}
		}
		out, err := e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, []deadline.Policy{{Kind: deadline.KindInsurance, LeadDays: []int{7, 90, 30, 30}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range out {
			if p.Kind == deadline.KindInsurance && (len(p.LeadDays) != 3 || p.LeadDays[0] != 90 || p.LeadDays[2] != 7) {
				t.Fatalf("sorted desc and deduplicated: %v", p.LeadDays)
			}
		}
		for name, in := range map[string][]deadline.Policy{
			"unknown kind": {{Kind: "berth_cleaning", LeadDays: []int{30}}},
			"zero":         {{Kind: deadline.KindInsurance, LeadDays: []int{0}}},
			"too far":      {{Kind: deadline.KindInsurance, LeadDays: []int{731}}},
			"empty":        {{Kind: deadline.KindInsurance}},
			"twice":        {{Kind: deadline.KindInsurance, LeadDays: []int{1}}, {Kind: deadline.KindInsurance, LeadDays: []int{2}}},
		} {
			if _, err := e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, in); !errors.Is(err, auth.ErrInvalidInput) {
				t.Errorf("%s: %v", name, err)
			}
		}
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		if _, err := e.dl.ReplacePolicies(ctx, office, e.MarinaID, nil); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office: %v", err)
		}
		if _, err := e.dl.ListPolicies(ctx, office, e.MarinaID); err != nil {
			t.Fatalf("office read: %v", err)
		}
		// Replacing with nothing restores the defaults.
		out, _ = e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, nil)
		for _, p := range out {
			if p.Kind == deadline.KindInsurance && len(p.LeadDays) != 2 {
				t.Fatalf("defaults back: %v", p.LeadDays)
			}
		}
	})
}
