package auth

import (
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
)

// slidingWindow counts events per key over a sliding window, in memory. It is safe for concurrent use. Keys
// with no events in the window are dropped on the next sweep, so memory stays bounded by recent activity.
type slidingWindow struct {
	clock  clock.Clock
	limit  int
	window time.Duration

	mu        sync.Mutex
	events    map[string][]time.Time
	inflight  map[string]int // reserved attempts not yet resolved (see reserve)
	lastSweep time.Time
}

func newSlidingWindow(c clock.Clock, limit int, window time.Duration) *slidingWindow {
	return &slidingWindow{clock: c, limit: limit, window: window, events: map[string][]time.Time{}, inflight: map[string]int{}}
}

// prune drops events older than the window (caller holds mu).
func (w *slidingWindow) prune(key string, now time.Time) []time.Time {
	ev := w.events[key]
	cut := 0
	for cut < len(ev) && !ev[cut].After(now.Add(-w.window)) {
		cut++
	}
	ev = ev[cut:]
	if len(ev) == 0 {
		delete(w.events, key)
	} else {
		w.events[key] = ev
	}
	return ev
}

func (w *slidingWindow) sweep(now time.Time) {
	if now.Sub(w.lastSweep) < w.window {
		return
	}
	w.lastSweep = now
	for k := range w.events {
		w.prune(k, now)
	}
}

// reserve atomically claims one in-flight attempt for key when recorded events plus attempts still in flight
// stay under the limit. Concurrent attempts therefore cannot all pass before any failure is recorded. Every
// successful reserve must be paired with a release.
func (w *slidingWindow) reserve(key string) (bool, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clock.Now()
	ev := w.prune(key, now)
	if len(ev)+w.inflight[key] >= w.limit {
		if len(ev) >= w.limit {
			return false, retryAfter(ev[len(ev)-w.limit].Add(w.window).Sub(now))
		}
		return false, time.Second // blocked by attempts still in flight: retry shortly
	}
	w.inflight[key]++
	return true, 0
}

// release returns an attempt claimed by reserve.
func (w *slidingWindow) release(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflight[key] <= 1 {
		delete(w.inflight, key)
		return
	}
	w.inflight[key]--
}

// take records an event if the key is under the limit.
func (w *slidingWindow) take(key string) (bool, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clock.Now()
	w.sweep(now)
	ev := w.prune(key, now)
	if len(ev) >= w.limit {
		return false, retryAfter(ev[len(ev)-w.limit].Add(w.window).Sub(now))
	}
	w.events[key] = append(ev, now)
	return true, 0
}

// add records an event unconditionally.
func (w *slidingWindow) add(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clock.Now()
	w.sweep(now)
	w.events[key] = append(w.prune(key, now), now)
}

func (w *slidingWindow) reset(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.events, key)
}

func retryAfter(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	return d.Round(time.Second)
}

// IPPrefixKey groups addresses by network: /24 for IPv4, /56 for IPv6 (one household or office).
func IPPrefixKey(ip netip.Addr) string {
	if !ip.IsValid() {
		return "invalid"
	}
	ip = ip.Unmap()
	bits := 56
	if ip.Is4() {
		bits = 24
	}
	p, err := ip.Prefix(bits)
	if err != nil {
		return ip.String()
	}
	return p.String()
}

// PeerKey is the per-client throttle key: private, loopback and link-local peers by full address (a marina's
// office PCs and phones share one /24 on the local network), public peers by IPPrefixKey.
func PeerKey(ip netip.Addr) string {
	if !ip.IsValid() {
		return IPPrefixKey(ip)
	}
	ip = ip.Unmap().WithZone("")
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return ip.String()
	}
	return IPPrefixKey(ip)
}

// accountKey identifies an account in limiters without keeping the email in memory in clear.
func accountKey(email string) string {
	return crypto.SHA256Hex("account:" + strings.ToLower(strings.TrimSpace(email)))
}

// Login throttling limits (m0-plan §5).
const (
	loginFailuresPerAccount = 5
	loginFailureWindow      = 15 * time.Minute
	loginAttemptsPerIP      = 30
	loginIPWindow           = time.Minute
)

// LoginThrottle limits login attempts: 5 failures per account in 15 minutes, 30 attempts per minute per IP
// prefix. The account limit applies to unknown emails too, so it does not reveal which accounts exist.
type LoginThrottle struct {
	accounts *slidingWindow
	ips      *slidingWindow
}

// NewLoginThrottle returns a LoginThrottle; perIP overrides the attempts per minute per IP prefix (0 = the
// default 30; PORTO_LOGIN_ATTEMPTS_PER_MINUTE, raised only in development and tests). The account limit is fixed.
func NewLoginThrottle(c clock.Clock, perIP int) *LoginThrottle {
	if perIP <= 0 {
		perIP = loginAttemptsPerIP
	}
	return &LoginThrottle{
		accounts: newSlidingWindow(c, loginFailuresPerAccount, loginFailureWindow),
		ips:      newSlidingWindow(c, perIP, loginIPWindow),
	}
}

// Check records an attempt from ip and reports whether it may proceed. account is the email as typed. An
// allowed attempt holds an in-flight slot of the account until Release, so recorded failures plus attempts in
// progress never exceed the account limit, even for concurrent requests. The caller must call Release exactly
// once after an allowed Check, after Failure or Success.
func (t *LoginThrottle) Check(ip netip.Addr, account string) (ok bool, retryAfter time.Duration) {
	key := accountKey(account)
	if ok, wait := t.accounts.reserve(key); !ok {
		return false, wait
	}
	if ok, wait := t.ips.take(PeerKey(ip)); !ok {
		t.accounts.release(key)
		return false, wait
	}
	return true, 0
}

// Release ends an attempt allowed by Check.
func (t *LoginThrottle) Release(account string) {
	t.accounts.release(accountKey(account))
}

// Failure records a failed attempt for the account.
func (t *LoginThrottle) Failure(_ netip.Addr, account string) {
	t.accounts.add(accountKey(account))
}

// Success clears the failures of the account.
func (t *LoginThrottle) Success(account string) {
	t.accounts.reset(accountKey(account))
}

// Password reset request limits (m0-plan §5).
const (
	resetRequestsPerAccount = 3
	resetRequestsPerIP      = 10
	resetWindow             = time.Hour
)

type resetThrottle struct {
	accounts *slidingWindow
	ips      *slidingWindow
}

func newResetThrottle(c clock.Clock) *resetThrottle {
	return &resetThrottle{
		accounts: newSlidingWindow(c, resetRequestsPerAccount, resetWindow),
		ips:      newSlidingWindow(c, resetRequestsPerIP, resetWindow),
	}
}

func (t *resetThrottle) take(ip netip.Addr, email string) (bool, time.Duration) {
	if ok, wait := t.ips.take(PeerKey(ip)); !ok {
		return false, wait
	}
	return t.accounts.take(accountKey(email))
}
