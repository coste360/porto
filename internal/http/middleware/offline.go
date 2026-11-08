package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/reqctx"
)

// OfflineQueuedAtHeader marks a replay from the offline queue of the panel (M6b, m6b-plan §4.4) with the instant
// the operation was enqueued.
const OfflineQueuedAtHeader = "Porto-Offline-Queued-At"

// offlineClockSkew is how far in the future a queued instant may be (client clocks drift).
const offlineClockSkew = 10 * time.Minute

// OfflineGuard enforces INV-71 on the server: a request carrying Porto-Offline-Queued-At on an operation without
// x-porto-offline is 422 offline_not_allowed; on a flagged operation the header must be an RFC 3339 instant not
// after now + 10 minutes and must come with an Idempotency-Key (else 422 validation_failed). The instant is stored
// in reqctx (OfflineQueuedAt) for the services and the audit rows. It runs after Enforce and before Idempotency.
func OfflineGuard(ps Policies, c clock.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(OfflineQueuedAtHeader)
			info := reqctx.From(r.Context())
			pol, ok := ps.Lookup(r.Method, info.Route)
			flagged := ok && pol.Offline
			if flagged {
				r = r.WithContext(context.WithValue(r.Context(), offlineRouteKey{}, true))
			}
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !flagged {
				problem.Write(w, r, problem.OfflineNotAllowed)
				return
			}
			at, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil || at.After(c.Now().Add(offlineClockSkew)) || r.Header.Get(IdempotencyHeader) == "" {
				problem.Write(w, r, problem.ValidationFailed, problem.Status(http.StatusUnprocessableEntity),
					problem.Fields(problem.FieldError{Field: OfflineQueuedAtHeader, Code: "invalid"}))
				return
			}
			info.OfflineQueuedAt = at.UTC()
			next.ServeHTTP(w, r)
		})
	}
}

type offlineRouteKey struct{}

// isOfflineRoute reports whether OfflineGuard marked the request as one of an operation flagged x-porto-offline
// (with or without the replay header): Idempotency keeps those keys longer and takes over stale claims.
func isOfflineRoute(ctx context.Context) bool {
	v, _ := ctx.Value(offlineRouteKey{}).(bool)
	return v
}
