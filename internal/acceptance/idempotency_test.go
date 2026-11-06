//go:build integration

package acceptance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	mw "github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func idempotentRouter(db *store.DB, c clock.Clock, handler http.HandlerFunc) http.Handler {
	r := chi.NewRouter()
	r.Use(mw.RequestID())
	r.With(mw.Route(), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := &auth.Principal{Kind: auth.KindUser, UserID: r.Header.Get("X-Test-User")}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}, mw.Idempotency(mw.IdempotencyOptions{Store: mw.StoreIdempotency{DB: db.DB}, Clock: c})).Post("/things", handler)
	return r
}

func post(h http.Handler, user, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/things", strings.NewReader(body))
	req.Header.Set("X-Test-User", user)
	if key != "" {
		req.Header.Set(mw.IdempotencyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestConcurrency_IdempotencyKeySingleExecution(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		var executed atomic.Int32
		h := idempotentRouter(db, clock.System{}, func(w http.ResponseWriter, _ *http.Request) {
			executed.Add(1)
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"1"}`))
		})
		const n = 100
		var wg sync.WaitGroup
		codes := make([]int, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = post(h, "u1", "same-key-0001", `{"a":1}`).Code
			}()
		}
		wg.Wait()
		if got := executed.Load(); got != 1 {
			t.Fatalf("handler executed %d times, want 1", got)
		}
		created := 0
		for _, c := range codes {
			switch c {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
			default:
				t.Fatalf("unexpected status %d", c)
			}
		}
		if created < 1 {
			t.Fatal("no request succeeded")
		}
		// After completion every retry replays.
		rec := post(h, "u1", "same-key-0001", `{"a":1}`)
		if rec.Code != http.StatusCreated || rec.Header().Get(mw.IdempotentReplayedHeader) != "true" || rec.Body.String() != `{"id":"1"}` {
			t.Fatalf("replay: %d %q %s", rec.Code, rec.Header().Get(mw.IdempotentReplayedHeader), rec.Body)
		}
		if executed.Load() != 1 {
			t.Fatal("replay executed the handler")
		}
	})
}

func TestIdempotency_ScopeHashAndFailures(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		var executed atomic.Int32
		fail := atomic.Bool{}
		fail.Store(true)
		h := idempotentRouter(db, clock.System{}, func(w http.ResponseWriter, _ *http.Request) {
			executed.Add(1)
			if fail.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		})
		// A 5xx is not stored: the retry runs again.
		if rec := post(h, "u1", "key-00000001", `{}`); rec.Code != 500 {
			t.Fatalf("first: %d", rec.Code)
		}
		fail.Store(false)
		if rec := post(h, "u1", "key-00000001", `{}`); rec.Code != 201 || executed.Load() != 2 {
			t.Fatalf("retry after 5xx: %d executed=%d", rec.Code, executed.Load())
		}
		// Different body: 422.
		if rec := post(h, "u1", "key-00000001", `{"b":2}`); rec.Code != 422 || !strings.Contains(rec.Body.String(), "idempotency_key_reused") {
			t.Fatalf("reuse: %d %s", rec.Code, rec.Body)
		}
		// Another principal has its own scope.
		if rec := post(h, "u2", "key-00000001", `{}`); rec.Code != 201 || rec.Header().Get(mw.IdempotentReplayedHeader) != "" {
			t.Fatalf("other principal: %d", rec.Code)
		}
		// Malformed key.
		if rec := post(h, "u1", "short", `{}`); rec.Code != 400 {
			t.Fatalf("bad key: %d", rec.Code)
		}
		// No key: no idempotency.
		before := executed.Load()
		post(h, "u1", "", `{}`)
		post(h, "u1", "", `{}`)
		if executed.Load() != before+2 {
			t.Fatal("requests without a key must always run")
		}
	})
}
