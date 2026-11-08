//go:build integration

package portal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/portal"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	svc  *portal.Service
	slug string
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	b := booking.NewService(booking.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys})
	svc := portal.NewService(portal.Deps{
		DB: db, Clock: e.Clock, Booking: b, Notify: e.Notify, BaseURL: "https://porto.test/",
	})
	return &env{Env: e, svc: svc, slug: "marina-test"}
}

func scopeOf(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// seedCustomer inserts a customer with email (and, with transit, a transit contract and its vessel).
func seedCustomer(t *testing.T, db *store.DB, marinaID, email string, transit bool) (customerID, contractID, vesselID string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(fixture.T0)
	c := &store.Customer{
		ID: store.NewID(), MarinaID: marinaID, Kind: "person", FirstName: "Anna", LastName: "Bianchi",
		BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.MustJSON([]map[string]string{{"label": "main", "value": email}}),
		Phones: store.JSON(`[]`), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	c.SearchText = customer.SearchText(c)
	if err := store.InsertCustomer(ctx, db.DB, c); err != nil {
		t.Fatal(err)
	}
	if !transit {
		return c.ID, "", ""
	}
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: marinaID, Name: "Libeccio", HullType: "monohull_sail", LOACm: 1200, BeamCm: 400,
		DraftCm: 180, DimensionsSource: "declared", Engines: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(v).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.NewRaw("SELECT COUNT(*) FROM contracts WHERE marina_id = ?", marinaID).Scan(ctx, &n); err != nil {
		t.Fatal(err)
	}
	k := &store.Contract{
		ID: store.NewID(), MarinaID: marinaID, Kind: store.ContractKindTransit, Number: int64(n + 1), Year: 2026,
		Status: "confirmed", CustomerID: c.ID, VesselID: v.ID, StartsOn: calendar.New(2026, 12, 1),
		EndsOn: calendar.New(2026, 12, 4), Channel: "online", PriceSnapshot: store.JSON(`{}`), Policy: store.JSON(`{}`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(k).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return c.ID, k.ID, v.ID
}

// seedLink inserts a link with a known token and returns the token.
func seedLink(t *testing.T, db *store.DB, marinaID, customerID, kind, contractID string, expires time.Time) string {
	t.Helper()
	token, err := crypto.RandomBase32(portal.TokenChars)
	if err != nil {
		t.Fatal(err)
	}
	now := store.UTC(fixture.T0)
	l := &store.CustomerPortalLink{
		ID: store.NewID(), MarinaID: marinaID, CustomerID: customerID, Kind: kind, ContractID: contractID,
		TokenHash: crypto.SHA256Hex(token), ExpiresAt: store.UTC(expires), CreatedAt: now,
	}
	if err := store.InsertPortalLink(context.Background(), db.DB, l); err != nil {
		t.Fatal(err)
	}
	return token
}

func linkCount(t *testing.T, db *store.DB) int {
	t.Helper()
	n, err := db.NewSelect().Table("customer_portal_links").Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func templateLoaded() bool {
	_, err := notify.Render(portal.TemplatePortalLogin, "it", notify.RenderData{})
	return err == nil
}

func TestPortal_LoginNoEnumeration(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		seedCustomer(t, db, e.MarinaID, "nobooking@example.test", false)

		known := e.svc.RequestLink(ctx, e.slug, "Anna@Example.test", "it", "", "203.0.113.10")
		unknown := e.svc.RequestLink(ctx, e.slug, "ghost@example.test", "it", "", "203.0.113.10")
		noTransit := e.svc.RequestLink(ctx, e.slug, "nobooking@example.test", "it", "", "203.0.113.10")
		if known != nil || unknown != nil || noTransit != nil {
			t.Fatalf("answers differ or fail: known=%v unknown=%v no transit=%v", known, unknown, noTransit)
		}
		if err := e.svc.RequestLink(ctx, e.slug, "not an email", "it", "", ""); err == nil {
			t.Fatal("invalid email accepted")
		}
		if err := e.svc.RequestLink(ctx, "unknown-marina", "anna@example.test", "it", "", ""); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("unknown slug: %v", err)
		}
		if !templateLoaded() {
			t.Log("portal_login template not registered in notify yet: email assertions skipped")
			return
		}
		if n := linkCount(t, db); n != 1 {
			t.Fatalf("links = %d, want 1 (known email only)", n)
		}
		// A repeated request in the same 10-minute bucket does not send a second email nor leave a link behind.
		if err := e.svc.RequestLink(ctx, e.slug, "anna@example.test", "it", "", ""); err != nil {
			t.Fatal(err)
		}
		if n := linkCount(t, db); n != 1 {
			t.Fatalf("links after repeat = %d, want 1", n)
		}
	})
}

func TestPortal_LinkSingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid, _, _ := seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		tok := seedLink(t, db, e.MarinaID, cid, store.PortalLinkLogin, "", fixture.T0.Add(portal.LoginLinkTTL))

		s, err := e.svc.OpenSession(ctx, e.slug, tok)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Token) != portal.TokenChars || !s.ExpiresAt.Equal(store.UTC(fixture.T0.Add(portal.SessionAbsoluteTTL))) {
			t.Fatalf("session %+v", s)
		}
		if _, err := e.svc.OpenSession(ctx, e.slug, tok); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("second use: %v", err)
		}
		p, err := e.svc.Resolve(ctx, e.slug, s.Token)
		if err != nil || p.CustomerID != cid || p.Scope.ID() != e.MarinaID {
			t.Fatalf("resolve: %+v %v", p, err)
		}

		// Expired login link.
		old := seedLink(t, db, e.MarinaID, cid, store.PortalLinkLogin, "", fixture.T0.Add(time.Minute))
		e.Clock.Advance(2 * time.Minute)
		if _, err := e.svc.OpenSession(ctx, e.slug, old); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("expired link: %v", err)
		}
		// Garbage and unknown tokens.
		for _, bad := range []string{"", "short", strings.Repeat("Z", portal.TokenChars), strings.Repeat("!", portal.TokenChars)} {
			if _, err := e.svc.OpenSession(ctx, e.slug, bad); !errors.Is(err, portal.ErrLinkInvalid) {
				t.Fatalf("token %q: %v", bad, err)
			}
		}
	})
}

func TestPortal_BookingLinkReusable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid, kid, _ := seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		scope := scopeOf(t, db, e.MarinaID)
		var tok string
		err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			tok, err = e.svc.IssueBookingLinkTx(ctx, tx, scope, cid, kid)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := e.svc.LinkURL(e.slug, tok); got != "https://porto.test/b/marina-test/a#t="+tok {
			t.Fatalf("url %s", got)
		}
		for i := 0; i < 3; i++ {
			if _, err := e.svc.OpenSession(ctx, e.slug, tok); err != nil {
				t.Fatalf("use %d: %v", i, err)
			}
		}
		// Valid until departure (2026-12-04, Europe/Rome) + 30 days.
		e.Clock.Set(time.Date(2027, 1, 2, 23, 0, 0, 0, time.UTC))
		if _, err := e.svc.OpenSession(ctx, e.slug, tok); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("after grace: %v", err)
		}
		// A link for a transit of another customer is refused.
		other, _, _ := seedCustomer(t, db, e.MarinaID, "other@example.test", true)
		err = db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			_, err := e.svc.IssueBookingLinkTx(ctx, tx, scope, other, kid)
			return err
		})
		if err == nil {
			t.Fatal("booking link for another customer's transit accepted")
		}
	})
}

func TestPortal_TokenScoped(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		otherMarina := e.AddMarina(t, "marina-due")
		cid, _, _ := seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		cid2, _, _ := seedCustomer(t, db, otherMarina, "anna@example.test", true)

		// A link of marina A does not open a session on marina B.
		tok := seedLink(t, db, e.MarinaID, cid, store.PortalLinkLogin, "", fixture.T0.Add(time.Hour))
		if _, err := e.svc.OpenSession(ctx, "marina-due", tok); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("foreign slug link: %v", err)
		}
		s, err := e.svc.OpenSession(ctx, e.slug, tok)
		if err != nil {
			t.Fatal(err)
		}
		// A session of marina A does not resolve on marina B, and resolves only its own customer.
		if _, err := e.svc.Resolve(ctx, "marina-due", s.Token); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("foreign slug session: %v", err)
		}
		if _, err := e.svc.Vessels(ctx, "marina-due", s.Token); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("foreign slug vessels: %v", err)
		}
		tok2 := seedLink(t, db, otherMarina, cid2, store.PortalLinkLogin, "", fixture.T0.Add(time.Hour))
		s2, err := e.svc.OpenSession(ctx, "marina-due", tok2)
		if err != nil {
			t.Fatal(err)
		}
		p2, err := e.svc.Resolve(ctx, "marina-due", s2.Token)
		if err != nil || p2.CustomerID != cid2 || p2.Scope.ID() != otherMarina {
			t.Fatalf("second marina principal %+v %v", p2, err)
		}
		vs, err := e.svc.Vessels(ctx, e.slug, s.Token)
		if err != nil || len(vs) != 1 || vs[0].Name != "Libeccio" || vs[0].LOACm != 1200 {
			t.Fatalf("vessels %+v %v", vs, err)
		}

		// An anonymised customer loses access.
		if _, err := db.NewRaw("UPDATE customers SET anonymized_at = ? WHERE id = ?", store.UTC(fixture.T0), cid).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Resolve(ctx, e.slug, s.Token); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("anonymised customer: %v", err)
		}
	})
}

func TestPortal_SessionExpiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid, _, _ := seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		open := func() string {
			tok := seedLink(t, db, e.MarinaID, cid, store.PortalLinkLogin, "", e.Clock.Now().Add(time.Hour))
			s, err := e.svc.OpenSession(ctx, e.slug, tok)
			if err != nil {
				t.Fatal(err)
			}
			return s.Token
		}

		// Idle: 8 hours without use.
		idle := open()
		e.Clock.Advance(portal.SessionIdleTTL + time.Second)
		if _, err := e.svc.Resolve(ctx, e.slug, idle); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("idle session: %v", err)
		}

		// Sliding: used every 7 hours it lives until the absolute expiry (7 days).
		live := open()
		start := e.Clock.Now()
		for e.Clock.Now().Before(start.Add(portal.SessionAbsoluteTTL - 7*time.Hour)) {
			e.Clock.Advance(7 * time.Hour)
			if _, err := e.svc.Resolve(ctx, e.slug, live); err != nil {
				t.Fatalf("sliding session at %s: %v", e.Clock.Now().Sub(start), err)
			}
		}
		e.Clock.Set(start.Add(portal.SessionAbsoluteTTL + time.Second))
		if _, err := e.svc.Resolve(ctx, e.slug, live); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("absolute expiry: %v", err)
		}

		// Logout revokes.
		out := open()
		if err := e.svc.CloseSession(ctx, e.slug, out); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Resolve(ctx, e.slug, out); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("revoked session: %v", err)
		}
		if err := e.svc.CloseSession(ctx, e.slug, out); !errors.Is(err, portal.ErrLinkInvalid) {
			t.Fatalf("second logout: %v", err)
		}

		// Cleanup removes rows expired long ago.
		e.Clock.Advance(portal.SessionAbsoluteTTL + portal.Retention + time.Hour)
		if err := e.svc.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		n, err := db.NewSelect().Table("customer_portal_sessions").Count(ctx)
		if err != nil || n != 0 {
			t.Fatalf("sessions after cleanup = %d, %v", n, err)
		}
	})
}

// The customer-area operations delegate to booking with the session's scope and customer; an invalid session never
// reaches booking.
func TestPortal_OperationsRequireSession(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		_, kid, _ := seedCustomer(t, db, e.MarinaID, "anna@example.test", true)
		bad := strings.Repeat("B", portal.TokenChars)
		checks := map[string]error{}
		_, checks["bookings"] = e.svc.Bookings(ctx, e.slug, bad)
		_, checks["booking"] = e.svc.Booking(ctx, e.slug, bad, kid)
		_, checks["upload"] = e.svc.UploadDocument(ctx, e.slug, bad, kid, booking.DocumentUpload{})
		_, checks["document_url"] = e.svc.DocumentURL(ctx, e.slug, bad, kid)
		_, checks["pay_balance"] = e.svc.PayBalance(ctx, e.slug, bad, kid)
		_, checks["preview_cancel"] = e.svc.PreviewCancellation(ctx, e.slug, bad, kid)
		_, checks["cancel"] = e.svc.Cancel(ctx, e.slug, bad, kid, 0)
		_, checks["preview_dates"] = e.svc.PreviewDateChange(ctx, e.slug, bad, kid, booking.DateChange{})
		_, checks["change_dates"] = e.svc.ChangeDates(ctx, e.slug, bad, kid, booking.DateChange{})
		for name, err := range checks {
			if !errors.Is(err, portal.ErrLinkInvalid) {
				t.Errorf("%s without a session: %v", name, err)
			}
		}
	})
}
