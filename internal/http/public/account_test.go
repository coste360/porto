//go:build integration

package public

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/portal"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: fixtures and assertions read the database directly
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type accountEnv struct {
	*fixture.Env
	h          *Handler
	customerID string
	contractID string
}

func newAccountEnv(t *testing.T, db *store.DB) *accountEnv {
	t.Helper()
	e := fixture.New(t, db)
	b := booking.NewService(booking.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys})
	p := portal.NewService(portal.Deps{DB: db, Clock: e.Clock, Booking: b, Notify: e.Notify, BaseURL: "https://porto.test"})
	ae := &accountEnv{Env: e, h: &Handler{Portal: p, Booking: b}}
	ctx := context.Background()
	now := store.UTC(fixture.T0)
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: "Bianchi", BillingAddress: store.JSON(`{}`),
		Locale: "it", Emails: store.JSON(`[{"label":"main","value":"anna@example.test"}]`), Phones: store.JSON(`[]`),
		Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	c.SearchText = customer.SearchText(c)
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: e.MarinaID, Name: "Libeccio", HullType: "monohull_sail", LOACm: 1200, BeamCm: 400,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	k := &store.Contract{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: store.ContractKindTransit, Number: 1, Year: 2026, Status: "confirmed",
		CustomerID: c.ID, VesselID: v.ID, StartsOn: calendar.New(2026, 12, 1), EndsOn: calendar.New(2026, 12, 4),
		Channel: "online", PriceSnapshot: store.JSON(`{}`), Policy: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []any{c, v, k} {
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ae.customerID, ae.contractID = c.ID, k.ID
	return ae
}

func (e *accountEnv) loginToken(t *testing.T) string {
	t.Helper()
	tok, err := crypto.RandomBase32(portal.TokenChars)
	if err != nil {
		t.Fatal(err)
	}
	l := &store.CustomerPortalLink{
		ID: store.NewID(), MarinaID: e.MarinaID, CustomerID: e.customerID, Kind: store.PortalLinkLogin,
		TokenHash: crypto.SHA256Hex(tok), ExpiresAt: store.UTC(e.Clock.Now().Add(time.Hour)), CreatedAt: store.UTC(e.Clock.Now()),
	}
	if err := store.InsertPortalLink(context.Background(), e.DB.DB, l); err != nil {
		t.Fatal(err)
	}
	return tok
}

func call(t *testing.T, fn func(w http.ResponseWriter, r *http.Request), method, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/api/public/v1/marina-test/account", strings.NewReader(body)).WithContext(fixture.Ctx())
	r.Header.Set("Content-Type", "application/json")
	fn(w, r)
	return w
}

func wantStatus(t *testing.T, name string, w *httptest.ResponseRecorder, status int, problemType string) {
	t.Helper()
	if w.Code != status || (problemType != "" && !strings.Contains(w.Body.String(), `"type":"`+problemType+`"`)) {
		t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, problemType)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%s: Cache-Control %q", name, w.Header().Get("Cache-Control"))
	}
}

func TestAccountHandlers_LoginAndSession(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newAccountEnv(t, db)
		h, slug := e.h, "marina-test"

		// Known and unknown emails get the same answer.
		known := call(t, func(w http.ResponseWriter, r *http.Request) {
			h.RequestAccountLink(w, r, slug, api.RequestAccountLinkParams{})
		},
			"POST", `{"email":"anna@example.test","locale":"it"}`)
		unknown := call(t, func(w http.ResponseWriter, r *http.Request) {
			h.RequestAccountLink(w, r, slug, api.RequestAccountLinkParams{})
		},
			"POST", `{"email":"ghost@example.test","locale":"it"}`)
		wantStatus(t, "known", known, 202, "")
		wantStatus(t, "unknown", unknown, 202, "")
		if known.Body.String() != unknown.Body.String() {
			t.Fatalf("bodies differ: %s / %s", known.Body.String(), unknown.Body.String())
		}
		wantStatus(t, "bad email", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.RequestAccountLink(w, r, slug, api.RequestAccountLinkParams{})
		}, "POST", `{"email":"nope","locale":"it"}`), 400, "bad_request")
		wantStatus(t, "unknown slug", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.RequestAccountLink(w, r, "nowhere", api.RequestAccountLinkParams{})
		}, "POST", `{"email":"anna@example.test","locale":"it"}`), 404, "link_invalid")

		// Session exchange.
		tok := e.loginToken(t)
		open := func(token string) *httptest.ResponseRecorder {
			return call(t, func(w http.ResponseWriter, r *http.Request) { h.OpenAccountSession(w, r, slug) }, "POST", `{"token":"`+token+`"}`)
		}
		w := open(tok)
		wantStatus(t, "open", w, 201, "")
		var s api.AccountSession
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || len(s.SessionToken) != portal.TokenChars {
			t.Fatalf("session %s %v", w.Body.String(), err)
		}
		wantStatus(t, "reuse", open(tok), 404, "link_invalid")
		wantStatus(t, "garbage", open("x"), 404, "link_invalid")

		sess := &s.SessionToken
		w = call(t, func(w http.ResponseWriter, r *http.Request) {
			h.ListAccountVessels(w, r, slug, api.ListAccountVesselsParams{PortoCustomerSession: sess})
		}, "GET", "")
		wantStatus(t, "vessels", w, 200, "")
		var vs api.AccountVesselList
		if err := json.Unmarshal(w.Body.Bytes(), &vs); err != nil || len(vs.Items) != 1 || vs.Items[0].LoaCm != 1200 {
			t.Fatalf("vessels %s", w.Body.String())
		}
		// Another marina's slug, a missing or a wrong session: 404.
		for name, fn := range map[string]func(w http.ResponseWriter, r *http.Request){
			"foreign slug": func(w http.ResponseWriter, r *http.Request) {
				h.ListAccountVessels(w, r, "marina-altra", api.ListAccountVesselsParams{PortoCustomerSession: sess})
			},
			"no session": func(w http.ResponseWriter, r *http.Request) {
				h.ListAccountBookings(w, r, slug, api.ListAccountBookingsParams{})
			},
			"no session2": func(w http.ResponseWriter, r *http.Request) {
				h.GetAccountBooking(w, r, slug, e.contractID, api.GetAccountBookingParams{})
			},
			"doc url": func(w http.ResponseWriter, r *http.Request) {
				h.GetAccountDocumentURL(w, r, slug, store.NewID(), api.GetAccountDocumentURLParams{PortoCustomerSession: sess})
			},
		} {
			wantStatus(t, name, call(t, fn, "GET", ""), 404, "link_invalid")
		}

		// Bad bodies are 400 before anything is touched.
		wantStatus(t, "dates", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.PreviewAccountDateChange(w, r, slug, e.contractID, api.PreviewAccountDateChangeParams{PortoCustomerSession: sess})
		}, "POST", `{"arrival":"2026-12-05","departure":"2026-12-01"}`), 400, "validation_failed")
		wantStatus(t, "expected total", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.ChangeAccountDates(w, r, slug, e.contractID, api.ChangeAccountDatesParams{PortoCustomerSession: sess})
		}, "POST", `{"arrival":"2026-12-02","departure":"2026-12-05"}`), 400, "validation_failed")

		// Logout, then the session is gone.
		wantStatus(t, "logout", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.CloseAccountSession(w, r, slug, api.CloseAccountSessionParams{PortoCustomerSession: sess})
		}, "DELETE", ""), 204, "")
		wantStatus(t, "after logout", call(t, func(w http.ResponseWriter, r *http.Request) {
			h.ListAccountVessels(w, r, slug, api.ListAccountVesselsParams{PortoCustomerSession: sess})
		}, "GET", ""), 404, "link_invalid")
	})
}
