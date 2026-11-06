// Package middleware holds the HTTP middleware of Porto: request context, access log, panic recovery, security
// headers, body limit, authentication, contract policies (auth method, CSRF), TOTP gate, rate limits and
// idempotency. Every constructor returns a func(http.Handler) http.Handler.
package middleware

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
)

// RequestIDHeader carries the request id in both directions.
const RequestIDHeader = "X-Request-ID"

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID creates the request context (reqctx.Info) with a request id: the client's X-Request-ID when it is
// well formed, a new random one otherwise. The id is echoed in the response.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !requestIDPattern.MatchString(id) {
				id, _ = crypto.RandomToken(12)
			}
			info := &reqctx.Info{
				RequestID: id, Method: r.Method, Scheme: "http", AuthMethod: reqctx.AuthNone,
				UserAgent: reqctx.TruncateUserAgent(r.UserAgent()),
			}
			if r.TLS != nil {
				info.Scheme = "https"
			}
			if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
				info.ClientIP = ap.Addr().Unmap()
			}
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(reqctx.With(r.Context(), info)))
		})
	}
}

// RealIP honours X-Forwarded-For and X-Forwarded-Proto only when the direct peer is a trusted proxy. The client
// address is the right-most untrusted entry of X-Forwarded-For.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := reqctx.From(r.Context())
			if len(trusted) > 0 && info.ClientIP.IsValid() && isTrusted(info.ClientIP) {
				if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
					parts := strings.Split(strings.Join(xff, ","), ",")
					for i := len(parts) - 1; i >= 0; i-- {
						a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
						if err != nil {
							break
						}
						a = a.Unmap()
						info.ClientIP = a
						if !isTrusted(a) {
							break
						}
					}
				}
				if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
					info.Scheme = p
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter records the status and size of a response.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// Flush supports streaming responses (SSE).
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack is not used by Porto but keeps the interface of the underlying writer.
func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("middleware: hijack not supported")
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog logs one line per request: method, route pattern (never the raw path, which may carry tokens),
// status, duration, size, auth method and request id. It also stores log in the context for the handlers.
func AccessLog(log *slog.Logger, c clock.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := c.Now()
			sw := &statusWriter{ResponseWriter: w}
			r = r.WithContext(respond.WithLogger(r.Context(), log))
			next.ServeHTTP(sw, r)
			info := reqctx.From(r.Context())
			route := info.Route
			if route == "" {
				route = "unmatched"
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "http request",
				"request_id", info.RequestID,
				"method", r.Method,
				"route", route,
				"status", status,
				"duration_ms", c.Now().Sub(start).Milliseconds(),
				"bytes", sw.bytes,
				"auth_method", string(info.AuthMethod),
			)
		})
	}
}

// Recover turns a panic into a 500 problem and logs the stack (never the request body).
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared as the stdlib does
						panic(v)
					}
					info := reqctx.From(r.Context())
					log.ErrorContext(r.Context(), "panic in handler",
						"request_id", info.RequestID, "route", info.Route, "panic", v, "stack", string(debug.Stack()))
					problem.Write(w, r, problem.InternalError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityOptions configure SecurityHeaders.
type SecurityOptions struct {
	// HSTS adds Strict-Transport-Security (only when the public URL is https).
	HSTS bool
}

// SecurityHeaders sets the headers every response carries: a strict CSP (the SPAs load only same-origin
// scripts), no sniffing, no framing, no referrer leakage, and HSTS behind https.
func SecurityHeaders(o SecurityOptions) func(http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
		"font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			if o.HSTS {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps request bodies at n bytes.
func BodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				problem.Write(w, r, problem.BadRequest, problem.Status(http.StatusRequestEntityTooLarge),
					problem.Detail("request body too large"))
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Route records the chi route pattern of the matched operation in reqctx (logs, idempotency scope, policies).
func Route() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rc := chi.RouteContext(r.Context()); rc != nil {
				reqctx.From(r.Context()).Route = rc.RoutePattern()
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireJSON rejects request bodies that are not JSON with 415 (the API only speaks JSON).
func RequireJSON() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength != 0 && unsafeMethod(r.Method) {
				ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
				if ct != "" && ct != "application/json" && !strings.HasSuffix(ct, "+json") {
					problem.Write(w, r, problem.UnsupportedMediaType)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// durationSeconds rounds d up to whole seconds (at least 1), for Retry-After.
func durationSeconds(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	return d.Round(time.Second)
}
