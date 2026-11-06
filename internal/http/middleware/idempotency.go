package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// IdempotencyHeader is the request header; IdempotentReplayedHeader marks a replayed response.
const (
	IdempotencyHeader        = "Idempotency-Key"
	IdempotentReplayedHeader = "Idempotent-Replayed"
	idempotencyTTL           = 24 * time.Hour
	maxStoredResponse        = 1 << 20
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_\-:.]{8,128}$`)

// IdempotencyRecord is a stored key.
type IdempotencyRecord struct {
	ID, RequestHash string
	Completed       bool
	Status          int
	ContentType     string
	Body            []byte
}

// IdempotencyStore persists idempotency keys. The unique (scope, key) constraint of the store makes exactly one
// concurrent claimer win.
type IdempotencyStore interface {
	// Claim returns (record, true) when the key is new for scope, or the existing record and false.
	Claim(ctx context.Context, scope, key, requestHash string, now, expiresAt time.Time) (IdempotencyRecord, bool, error)
	Complete(ctx context.Context, id string, status int, contentType string, body []byte, at time.Time) error
	Release(ctx context.Context, id string) error
}

// StoreIdempotency implements IdempotencyStore on the database.
type StoreIdempotency struct{ DB bun.IDB }

// Claim implements IdempotencyStore.
func (s StoreIdempotency) Claim(ctx context.Context, scope, key, hash string, now, exp time.Time) (IdempotencyRecord, bool, error) {
	row, created, err := store.ClaimIdempotencyKey(ctx, s.DB, scope, key, hash, now, exp)
	if err != nil {
		return IdempotencyRecord{}, false, err
	}
	return IdempotencyRecord{
		ID: row.ID, RequestHash: row.RequestHash, Completed: row.State == store.IdempotencyCompleted,
		Status: row.ResponseStatus, ContentType: row.ResponseContentType, Body: row.ResponseBody,
	}, created, nil
}

// Complete implements IdempotencyStore.
func (s StoreIdempotency) Complete(ctx context.Context, id string, status int, ct string, body []byte, at time.Time) error {
	return store.CompleteIdempotencyKey(ctx, s.DB, id, status, ct, body, at)
}

// Release implements IdempotencyStore.
func (s StoreIdempotency) Release(ctx context.Context, id string) error {
	return store.ReleaseIdempotencyKey(ctx, s.DB, id)
}

// IdempotencyOptions configure Idempotency.
type IdempotencyOptions struct {
	Store IdempotencyStore
	Clock clock.Clock
}

// Idempotency makes POSTs carrying Idempotency-Key safe to retry. The scope is the principal (or, for anonymous
// requests, the client IP prefix) plus method and route; the request hash covers method, route and body. The
// first request runs and its response (up to 1 MiB, status < 500) is stored for 24 hours; a retry with the
// same hash replays it (Idempotent-Replayed: true); a different hash is 422 idempotency_key_reused; a retry
// while the first is still running is 409 idempotency_in_progress. /auth/* (cookies) and SSE are skipped.
func Idempotency(o IdempotencyOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(IdempotencyHeader)
			info := reqctx.From(r.Context())
			if key == "" || r.Method != http.MethodPost || strings.Contains(info.Route, "/auth/") {
				next.ServeHTTP(w, r)
				return
			}
			if !idempotencyKeyPattern.MatchString(key) {
				problem.Write(w, r, problem.ValidationFailed,
					problem.Fields(problem.FieldError{Field: IdempotencyHeader, Code: "invalid"}))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				respond.Error(w, r, respond.ErrBadJSON)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			scope := "anon:" + auth.IPPrefixKey(info.ClientIP)
			if p, ok := auth.PrincipalFrom(r.Context()); ok {
				scope = p.ID()
			}
			scope += "|" + r.Method + " " + info.Route
			sum := sha256.New()
			sum.Write([]byte(r.Method + "\n" + info.Route + "\n" + r.URL.Path + "\n"))
			sum.Write(body)
			hash := hex.EncodeToString(sum.Sum(nil))

			now := o.Clock.Now()
			rec, created, err := o.Store.Claim(r.Context(), scope, key, hash, now, now.Add(idempotencyTTL))
			if err != nil {
				respond.Error(w, r, err)
				return
			}
			if !created {
				switch {
				case rec.RequestHash != hash:
					problem.Write(w, r, problem.IdempotencyKeyReused)
				case !rec.Completed:
					problem.Write(w, r, problem.IdempotencyInProgress)
				default:
					h := w.Header()
					if rec.ContentType != "" {
						h.Set("Content-Type", rec.ContentType)
					}
					h.Set("Cache-Control", "no-store")
					h.Set(IdempotentReplayedHeader, "true")
					w.WriteHeader(rec.Status)
					_, _ = w.Write(rec.Body)
				}
				return
			}

			rw := &recordingWriter{ResponseWriter: w}
			completed := false
			defer func() {
				if completed {
					return
				}
				// Panic or failure: forget the claim so the client can retry with the same key. A detached
				// context, because the request context may be cancelled already.
				ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
				defer cancel()
				if err := o.Store.Release(ctx, rec.ID); err != nil {
					respond.Logger(r.Context()).WarnContext(ctx, "release idempotency key", "error", err,
						"request_id", info.RequestID)
				}
			}()
			next.ServeHTTP(rw, r)
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 500 || rw.overflow {
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			if err := o.Store.Complete(ctx, rec.ID, status, rw.Header().Get("Content-Type"), rw.buf.Bytes(), o.Clock.Now()); err != nil {
				respond.Logger(r.Context()).WarnContext(ctx, "store idempotent response", "error", err,
					"request_id", info.RequestID)
				return
			}
			completed = true
		})
	}
}

// recordingWriter passes the response through and keeps a copy of the body (up to maxStoredResponse).
type recordingWriter struct {
	http.ResponseWriter
	status   int
	buf      bytes.Buffer
	overflow bool
}

func (rw *recordingWriter) WriteHeader(code int) {
	if rw.status == 0 {
		rw.status = code
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recordingWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	if !rw.overflow {
		if rw.buf.Len()+len(b) > maxStoredResponse {
			rw.overflow = true
			rw.buf.Reset()
		} else {
			rw.buf.Write(b)
		}
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (rw *recordingWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
