package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	apispec "github.com/coste360/porto/api"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	mw "github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/reqctx"
)

func contractPolicies(t *testing.T) mw.Policies {
	t.Helper()
	ps, err := mw.ParsePolicies(apispec.OpenAPI)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// TestPolicies_OfflineSet pins the operations the offline queue may replay (m6b-plan §4.2, m9-plan §10, INV-71).
func TestPolicies_OfflineSet(t *testing.T) {
	got := contractPolicies(t).OfflineOperations()
	want := []string{
		"completeMaintenanceTask", "recordMeterReading", "recordPayment", "recordPresence", "reportMaintenance",
		"uploadMaintenancePhoto", "uploadMeterReadingPhoto", "uploadPresencePhoto",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("x-porto-offline operations = %v, want %v", got, want)
	}
}

// guardHandler runs OfflineGuard for a routed request of the contract (route pattern set as the router would).
func guardHandler(ps mw.Policies, c clock.Clock, route string, seen *time.Time) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if at, ok := reqctx.OfflineReplay(r.Context()); ok && seen != nil {
			*seen = at
		}
		w.WriteHeader(http.StatusNoContent)
	})
	g := mw.OfflineGuard(ps, c)(inner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &reqctx.Info{Route: route, Method: r.Method}
		g.ServeHTTP(w, r.WithContext(reqctx.With(r.Context(), info)))
	})
}

func problemType(rec *httptest.ResponseRecorder) string {
	b := rec.Body.String()
	i := strings.Index(b, `"type":"`)
	if i < 0 {
		return ""
	}
	rest := b[i+8:]
	return rest[:strings.IndexByte(rest, '"')]
}

// TestOfflineGuard_RefusesNonOfflineOps sweeps the contract: every operation without x-porto-offline answers 422
// offline_not_allowed to a replay header, the flagged ones pass.
func TestOfflineGuard_RefusesNonOfflineOps(t *testing.T) {
	ps := contractPolicies(t)
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	queued := c.Now().Add(-time.Hour).Format(time.RFC3339)
	named := map[string]bool{}
	for key, p := range ps {
		method, route, _ := strings.Cut(key, " ")
		req := httptest.NewRequest(method, "/x", nil)
		req.Header.Set(mw.OfflineQueuedAtHeader, queued)
		req.Header.Set(mw.IdempotencyHeader, "offline-key-0001")
		rec := httptest.NewRecorder()
		guardHandler(ps, c, route, nil).ServeHTTP(rec, req)
		if p.Offline {
			if rec.Code != http.StatusNoContent {
				t.Errorf("%s (%s): offline operation refused: %d %s", p.OperationID, key, rec.Code, rec.Body)
			}
			continue
		}
		if rec.Code != http.StatusUnprocessableEntity || problemType(rec) != "offline_not_allowed" {
			t.Errorf("%s (%s): %d %s, want 422 offline_not_allowed", p.OperationID, key, rec.Code, rec.Body)
		}
		named[p.OperationID] = true
	}
	for _, op := range []string{"recordUnexpectedArrival", "createBlock", "createRelease", "createTransit", "confirmTransit", "createCounterSale", "recordVesselMeasurement"} {
		if !named[op] {
			t.Errorf("operation %s not in the sweep (renamed?)", op)
		}
	}
}

// TestOfflineGuard_RequiresKeyAndValidInstant: a flagged operation needs an RFC 3339 instant not after now + 10
// minutes and an Idempotency-Key; the instant reaches reqctx in UTC.
func TestOfflineGuard_RequiresKeyAndValidInstant(t *testing.T) {
	ps := contractPolicies(t)
	route := ""
	for key, p := range ps {
		if p.OperationID == "recordPresence" {
			_, route, _ = strings.Cut(key, " ")
		}
	}
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	cases := []struct {
		name, at, key string
		want          int
	}{
		{"valid", "2027-06-01T08:30:00+02:00", "offline-key-0001", http.StatusNoContent},
		{"skew allowed", c.Now().Add(9 * time.Minute).Format(time.RFC3339), "offline-key-0001", http.StatusNoContent},
		{"future", c.Now().Add(11 * time.Minute).Format(time.RFC3339), "offline-key-0001", http.StatusUnprocessableEntity},
		{"not rfc3339", "2027-06-01 08:30", "offline-key-0001", http.StatusUnprocessableEntity},
		{"no key", "2027-06-01T06:00:00Z", "", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen time.Time
			req := httptest.NewRequest("POST", "/x", nil)
			req.Header.Set(mw.OfflineQueuedAtHeader, tc.at)
			if tc.key != "" {
				req.Header.Set(mw.IdempotencyHeader, tc.key)
			}
			rec := httptest.NewRecorder()
			guardHandler(ps, c, route, &seen).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
			if tc.want == http.StatusUnprocessableEntity && problemType(rec) != "validation_failed" {
				t.Fatalf("problem %s", rec.Body)
			}
			if tc.name == "valid" && !seen.Equal(time.Date(2027, 6, 1, 6, 30, 0, 0, time.UTC)) || (tc.want == http.StatusNoContent && seen.Location() != time.UTC) {
				t.Fatalf("queued at %v", seen)
			}
		})
	}
	// Without the header nothing changes, even on a flagged operation.
	rec := httptest.NewRecorder()
	guardHandler(ps, c, route, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("plain request %d", rec.Code)
	}
}

// memIdem is an in-memory IdempotencyStore with the semantics of the database one (tests of the middleware).
type memIdem struct {
	mu   sync.Mutex
	rows map[string]*memRow
	n    int
}

type memRow struct {
	rec     mw.IdempotencyRecord
	scope   string
	expires time.Time
}

func newMemIdem() *memIdem { return &memIdem{rows: map[string]*memRow{}} }

func (m *memIdem) Claim(_ context.Context, scope, key, hash string, now, exp time.Time) (mw.IdempotencyRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := scope + "|" + key
	if r, ok := m.rows[k]; ok && r.expires.After(now) {
		return r.rec, false, nil
	}
	m.n++
	r := &memRow{rec: mw.IdempotencyRecord{ID: k, RequestHash: hash, UpdatedAt: now}, scope: scope, expires: exp}
	m.rows[k] = r
	return r.rec, true, nil
}

func (m *memIdem) Complete(_ context.Context, id string, status int, ct string, body []byte, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	r.rec.Completed, r.rec.Status, r.rec.ContentType, r.rec.Body, r.rec.UpdatedAt = true, status, ct, body, at
	return nil
}

func (m *memIdem) Release(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok && !r.rec.Completed {
		delete(m.rows, id)
	}
	return nil
}

func (m *memIdem) TakeOver(_ context.Context, id string, prev, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.rec.Completed || !r.rec.UpdatedAt.Equal(prev) {
		return false, nil
	}
	r.rec.UpdatedAt = now
	return true, nil
}

func (m *memIdem) expiry(id string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[id].expires
}

// offlineStack is the router fragment OfflineGuard → Idempotency → handler on two routes of the contract: one
// flagged x-porto-offline (recordPresence), one not (createBlock).
func offlineStack(t *testing.T, st mw.IdempotencyStore, c clock.Clock, h http.HandlerFunc) http.Handler {
	t.Helper()
	ps := contractPolicies(t)
	r := chi.NewRouter()
	r.Use(mw.RequestID())
	stack := func(next http.Handler) http.Handler {
		return mw.Route()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := &auth.Principal{Kind: auth.KindUser, UserID: "u1"}
			r = r.WithContext(auth.WithPrincipal(r.Context(), p))
			mw.OfflineGuard(ps, c)(mw.Idempotency(mw.IdempotencyOptions{Store: st, Clock: c})(next)).ServeHTTP(w, r)
		}))
	}
	r.With(stack).Post("/api/admin/v1/marinas/{marinaId}/presences", h)
	r.With(stack).Post("/api/admin/v1/marinas/{marinaId}/blocks", h)
	return r
}

func send(h http.Handler, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(`{"a":1}`))
	req.Header.Set(mw.IdempotencyHeader, key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const (
	presencesPath = "/api/admin/v1/marinas/m1/presences"
	blocksPath    = "/api/admin/v1/marinas/m1/blocks"
)

// TestIdempotency_OfflineTTLSevenDays: keys of offline operations live 7 days, the others 24 hours.
func TestIdempotency_OfflineTTLSevenDays(t *testing.T) {
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	st := newMemIdem()
	h := offlineStack(t, st, c, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	send(h, presencesPath, "ttl-key-offline")
	send(h, blocksPath, "ttl-key-online1")
	var offline, online time.Time
	for id := range st.rows {
		switch {
		case strings.HasSuffix(id, "ttl-key-offline"):
			offline = st.expiry(id)
		case strings.HasSuffix(id, "ttl-key-online1"):
			online = st.expiry(id)
		}
	}
	if !offline.Equal(c.Now().Add(7*24*time.Hour)) || !online.Equal(c.Now().Add(24*time.Hour)) {
		t.Fatalf("expiry offline %v online %v", offline, online)
	}
}

// TestIdempotency_StaleClaimTakeover: an in-progress claim older than 2 minutes is taken over on offline
// operations only; a fresh claim is still 409 idempotency_in_progress.
func TestIdempotency_StaleClaimTakeover(t *testing.T) {
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	st := newMemIdem()
	runs := 0
	h := offlineStack(t, st, c, func(w http.ResponseWriter, _ *http.Request) {
		runs++
		w.WriteHeader(http.StatusCreated)
	})
	// Crashed claimers: run each key once, then put the rows back in progress (Complete never ran).
	send(h, presencesPath, "stale-key-offl1")
	send(h, blocksPath, "stale-key-onln1")
	st.mu.Lock()
	for _, r := range st.rows {
		r.rec.Completed, r.rec.Status, r.rec.Body = false, 0, nil
	}
	st.mu.Unlock()
	runs = 0

	c.Advance(time.Minute)
	if rec := send(h, presencesPath, "stale-key-offl1"); rec.Code != http.StatusConflict || problemType(rec) != "idempotency_in_progress" {
		t.Fatalf("fresh claim: %d %s", rec.Code, rec.Body)
	}
	c.Advance(90 * time.Second)
	if rec := send(h, presencesPath, "stale-key-offl1"); rec.Code != http.StatusCreated || runs != 1 {
		t.Fatalf("takeover: %d runs %d", rec.Code, runs)
	}
	if rec := send(h, presencesPath, "stale-key-offl1"); rec.Code != http.StatusCreated || rec.Header().Get(mw.IdempotentReplayedHeader) != "true" || runs != 1 {
		t.Fatalf("replay after takeover: %d runs %d", rec.Code, runs)
	}
	if rec := send(h, blocksPath, "stale-key-onln1"); rec.Code != http.StatusConflict || runs != 1 {
		t.Fatalf("non-offline stale claim taken over: %d", rec.Code)
	}
}

// TestIdempotency_RetryAfterNotStored: a response with Retry-After is not replayed; the retry runs again.
func TestIdempotency_RetryAfterNotStored(t *testing.T) {
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	runs := 0
	h := offlineStack(t, newMemIdem(), c, func(w http.ResponseWriter, _ *http.Request) {
		runs++
		if runs == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	if rec := send(h, presencesPath, "retry-after-key1"); rec.Code != http.StatusConflict {
		t.Fatalf("first %d", rec.Code)
	}
	if rec := send(h, presencesPath, "retry-after-key1"); rec.Code != http.StatusCreated || runs != 2 {
		t.Fatalf("retry %d runs %d", rec.Code, runs)
	}
}
