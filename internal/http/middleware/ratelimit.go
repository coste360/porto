package middleware

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/reqctx"
)

// Limiter is an in-memory fixed-window rate limiter: at most Limit events per key per Window. It is per process,
// which matches Porto's single-binary deployment.
type Limiter struct {
	clock  clock.Clock
	limit  int
	window time.Duration

	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	start time.Time
	count int
}

// NewLimiter returns a limiter of limit events per window.
func NewLimiter(c clock.Clock, limit int, per time.Duration) *Limiter {
	return &Limiter{clock: c, limit: limit, window: per, windows: map[string]*window{}}
}

// Allow records an event for key and reports whether it is within the limit; when it is not, retryAfter is the
// time until the window resets.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.windows) > 50_000 {
		for k, w := range l.windows {
			if now.Sub(w.start) >= l.window {
				delete(l.windows, k)
			}
		}
	}
	w := l.windows[key]
	if w == nil || now.Sub(w.start) >= l.window {
		w = &window{start: now}
		l.windows[key] = w
	}
	if w.count >= l.limit {
		return false, w.start.Add(l.window).Sub(now)
	}
	w.count++
	return true, 0
}

// PeerKey is the rate-limit key of a client address. Public addresses are grouped by prefix (/24, /56) so a
// client cannot dodge a limit by hopping addresses; private, link-local and loopback peers are keyed by full
// address, because every phone of a marina shares one /24 on the office Wi-Fi (m6a-plan §2.11, D11).
func PeerKey(ip netip.Addr) string {
	return auth.PeerKey(ip)
}

// RateLimitOptions configure RateLimit.
type RateLimitOptions struct {
	// ByIP limits operations (by operation id) per client: public peers by IP prefix (/24 IPv4, /56 IPv6),
	// private and loopback peers by full address (PeerKey).
	ByIP map[string]*Limiter
	// APIKey limits every request authenticated with an API key, per key.
	APIKey *Limiter
}

// RateLimit applies the per-IP limits of the routed operation and the per-key API limit; over the limit the
// answer is 429 rate_limited with Retry-After. It must run after Route and Authenticate.
func RateLimit(ps Policies, o RateLimitOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := reqctx.From(r.Context())
			if pol, ok := ps.Lookup(r.Method, info.Route); ok {
				if l := o.ByIP[pol.OperationID]; l != nil {
					if ok, wait := l.Allow(pol.OperationID + "|" + PeerKey(info.ClientIP)); !ok {
						problem.Write(w, r, problem.RateLimited, problem.RetryAfter(durationSeconds(wait)))
						return
					}
				}
			}
			if o.APIKey != nil && info.AuthMethod == reqctx.AuthAPIKey {
				if p, ok := auth.PrincipalFrom(r.Context()); ok {
					if ok, wait := o.APIKey.Allow(p.ID()); !ok {
						problem.Write(w, r, problem.RateLimited, problem.RetryAfter(durationSeconds(wait)))
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
