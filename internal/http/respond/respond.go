// Package respond holds the helpers shared by the HTTP handlers: JSON encoding and decoding, ETag/If-Match and
// the mapping of domain errors to problem types (errmap.go).
package respond

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/http/problem"
)

type loggerKey struct{}

// WithLogger stores the request logger in ctx (set by the server for every request).
func WithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

// Logger returns the logger stored in ctx, or slog.Default.
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// JSON writes v with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Error writes err as a problem, through the domain error mapper.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	problem.WriteError(w, r, err, MapError, Logger(r.Context()))
}

// Problem writes a problem of type t.
func Problem(w http.ResponseWriter, r *http.Request, t problem.Type, opts ...problem.Option) {
	problem.Write(w, r, t, opts...)
}

// ErrBadJSON is a malformed request body.
var ErrBadJSON = errors.New("respond: malformed JSON body")

// errUnsupportedMedia is a body that is not JSON.
var errUnsupportedMedia = errors.New("respond: unsupported media type")

// Decode reads a JSON body into v. Unknown fields are rejected, so that typos do not silently do nothing. An
// empty body is allowed only when optional is true (v stays zero).
func Decode(r *http.Request, v any, optional bool) error {
	if r.Body == nil || r.Body == http.NoBody {
		if optional {
			return nil
		}
		return ErrBadJSON
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
			return errUnsupportedMedia
		}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) && optional {
			return nil
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errBodyTooLarge
		}
		return ErrBadJSON
	}
	if dec.More() {
		return ErrBadJSON
	}
	return nil
}

var errBodyTooLarge = errors.New("respond: body too large")

// DecodeWithFields decodes a required JSON object body into v and also returns its top-level members, so that a
// handler can tell an explicit null from an absent field.
func DecodeWithFields(r *http.Request, v any) (map[string]json.RawMessage, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, ErrBadJSON
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errBodyTooLarge
		}
		return nil, ErrBadJSON
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err := Decode(r, v, false); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, ErrBadJSON
	}
	return fields, nil
}

// IsNull reports whether fields has key set to JSON null.
func IsNull(fields map[string]json.RawMessage, key string) bool {
	v, ok := fields[key]
	return ok && strings.TrimSpace(string(v)) == "null"
}

// DecodeOrFail decodes the body and writes the problem on failure; it reports whether decoding succeeded.
func DecodeOrFail(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	if err := Decode(r, v, optional); err != nil {
		Error(w, r, err)
		return false
	}
	return true
}

// SetETag writes ETag: "<version>".
func SetETag(w http.ResponseWriter, version int) {
	w.Header().Set("ETag", `"`+strconv.Itoa(version)+`"`)
}

// IfMatch parses an If-Match header value. A missing header is 428 precondition_required; a value that is not a
// version is 412 version_conflict. It writes the problem and reports false in both cases.
func IfMatch(w http.ResponseWriter, r *http.Request, header *string) (int, bool) {
	if header == nil || strings.TrimSpace(*header) == "" {
		Problem(w, r, problem.PreconditionRequired, problem.Detail("If-Match header required"))
		return 0, false
	}
	v := strings.TrimSpace(*header)
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		Problem(w, r, problem.VersionConflict)
		return 0, false
	}
	return n, true
}
