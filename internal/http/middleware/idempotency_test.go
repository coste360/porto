package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	mw "github.com/coste360/porto/internal/http/middleware"
)

// TestIdempotency_AnonymousScopeBindsCredential: on the token-authenticated public writes a replay is answered
// only to the holder of the same booking token or customer session; the same key from the same IP prefix with
// another (or no) credential runs the handler, which checks its own credential, and never receives the stored
// response.
func TestIdempotency_AnonymousScopeBindsCredential(t *testing.T) {
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	st := newMemIdem()
	runs := 0
	r := chi.NewRouter()
	r.Use(mw.RequestID())
	r.With(mw.Route(), mw.Idempotency(mw.IdempotencyOptions{Store: st, Clock: c})).
		Post("/api/public/v1/{slug}/bookings/{bookingId}/pay", func(w http.ResponseWriter, r *http.Request) {
			runs++
			if r.Header.Get("Porto-Booking-Token") != "token-a" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status_token":"secret"}`))
		})
	post := func(header, value string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/public/v1/m/bookings/b1/pay", strings.NewReader(`{}`))
		req.RemoteAddr = "203.0.113.7:4000"
		req.Header.Set(mw.IdempotencyHeader, "pay-key-0001")
		if header != "" {
			req.Header.Set(header, value)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("Porto-Booking-Token", "token-a"); rec.Code != http.StatusOK || runs != 1 {
		t.Fatalf("first %d runs %d", rec.Code, runs)
	}
	if rec := post("Porto-Booking-Token", "token-a"); rec.Header().Get(mw.IdempotentReplayedHeader) != "true" || runs != 1 {
		t.Fatalf("same credential not replayed: %d runs %d", rec.Code, runs)
	}
	for _, h := range [][2]string{{"Porto-Booking-Token", "token-b"}, {"", ""}, {"Porto-Customer-Session", "token-a"}} {
		before := runs
		rec := post(h[0], h[1])
		if rec.Header().Get(mw.IdempotentReplayedHeader) != "" || strings.Contains(rec.Body.String(), "secret") || runs != before+1 {
			t.Fatalf("%v got the stored response: %d %s", h, rec.Code, rec.Body)
		}
	}
	for scope := range st.rows {
		if strings.Contains(scope, "token-a") {
			t.Fatalf("credential stored in clear in the scope %q", scope)
		}
	}
}
