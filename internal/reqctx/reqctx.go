// Package reqctx carries per-request metadata (request id, client IP, route, auth method) through a context.
//
// The middleware stores a *Info early in the chain and later middleware (authentication) fills more fields.
// It is consumed by the audit writer, the access log and problem responses.
package reqctx

import (
	"context"
	"net/netip"
	"time"
	"unicode/utf8"
)

// AuthMethod is how the request was authenticated.
type AuthMethod string

// Authentication methods.
const (
	AuthNone    AuthMethod = "none"
	AuthSession AuthMethod = "session"
	AuthAPIKey  AuthMethod = "api_key"
	AuthDevice  AuthMethod = "device"
)

// MaxUserAgent is the maximum stored length of the User-Agent header, in bytes.
const MaxUserAgent = 256

// Info is the request metadata.
type Info struct {
	RequestID  string
	ClientIP   netip.Addr
	Scheme     string // http | https
	Method     string
	Route      string // chi route pattern, never the raw path (no ids or tokens in logs)
	UserAgent  string // truncated to MaxUserAgent bytes
	AuthMethod AuthMethod
	// OfflineQueuedAt is the Porto-Offline-Queued-At instant of a replay from the offline queue (M6b), set by
	// middleware.OfflineGuard only on operations flagged x-porto-offline; zero for every other request. Audit rows
	// of the request record it (offline_queued_at).
	OfflineQueuedAt time.Time
}

// OfflineReplay reports whether the request is a replay from the offline queue (M6b) and its queued instant.
func OfflineReplay(ctx context.Context) (time.Time, bool) {
	at := From(ctx).OfflineQueuedAt
	return at, !at.IsZero()
}

type ctxKey struct{}

// With returns a copy of ctx carrying info.
func With(ctx context.Context, info *Info) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

// From returns the Info stored in ctx. It never returns nil: without an Info it returns a fresh zero Info
// with AuthMethod none (writes to it are not visible to other callers).
func From(ctx context.Context) *Info {
	if ctx != nil {
		if info, ok := ctx.Value(ctxKey{}).(*Info); ok && info != nil {
			return info
		}
	}
	return &Info{AuthMethod: AuthNone}
}

// TruncateUserAgent cuts s to at most MaxUserAgent bytes without splitting a UTF-8 sequence and replaces
// invalid UTF-8.
func TruncateUserAgent(s string) string {
	if !utf8.ValidString(s) {
		s = toValidUTF8(s)
	}
	if len(s) <= MaxUserAgent {
		return s
	}
	cut := MaxUserAgent
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func toValidUTF8(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s { // invalid bytes decode as utf8.RuneError
		out = append(out, r)
	}
	return string(out)
}
