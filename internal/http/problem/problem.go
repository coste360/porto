// Package problem writes RFC 9457 application/problem+json error responses.
//
// The "type" member is a stable slug (see types.go) rather than a URI, so clients can switch on it and the
// web app can translate it (errors.<slug>).
package problem

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coste360/porto/internal/reqctx"
)

// ContentType is the media type of every error response.
const ContentType = "application/problem+json"

// FieldError describes one invalid input field of a validation_failed problem.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Problem is the response body.
type Problem struct {
	Type              Type         `json:"type"`
	Title             string       `json:"title"`
	Status            int          `json:"status"`
	Detail            string       `json:"detail,omitempty"`
	Instance          string       `json:"instance,omitempty"`
	RequestID         string       `json:"request_id,omitempty"`
	Errors            []FieldError `json:"errors,omitempty"`
	RetryAfterSeconds int          `json:"retry_after_seconds,omitempty"`
	// Issues carries the map validation issues of map_invalid (M1).
	Issues any `json:"issues,omitempty"`
	// PriceListIssues carries the price list validation issues of price_list_invalid (M2).
	PriceListIssues any `json:"price_list_issues,omitempty"`
	// Details carries machine-readable details (no_price, price_list_overlap, in_use; M2). Ids, codes and
	// integers only, never personal data.
	Details any `json:"details,omitempty"`
}

// Option customizes a Problem.
type Option func(*Problem)

// Detail sets a human-readable explanation. It must not contain personal data or secrets.
func Detail(s string) Option { return func(p *Problem) { p.Detail = s } }

// Fields appends field errors.
func Fields(f ...FieldError) Option { return func(p *Problem) { p.Errors = append(p.Errors, f...) } }

// RetryAfter sets retry_after_seconds (rounded up, at least 1) and the Retry-After header.
func RetryAfter(d time.Duration) Option {
	return func(p *Problem) {
		secs := int((d + time.Second - 1) / time.Second)
		p.RetryAfterSeconds = max(secs, 1)
	}
}

// Status overrides the default status (for example invalid_totp is 401 on login).
func Status(code int) Option { return func(p *Problem) { p.Status = code } }

// WithIssues sets the issues member (map_invalid). v must marshal to a JSON array of MapIssue.
func WithIssues(v any) Option { return func(p *Problem) { p.Issues = v } }

// WithPriceListIssues sets the price_list_issues member (price_list_invalid). v must marshal to a JSON array of
// PriceListIssue.
func WithPriceListIssues(v any) Option { return func(p *Problem) { p.PriceListIssues = v } }

// WithDetails sets the details member. v must marshal to a JSON object without personal data.
func WithDetails(v any) Option { return func(p *Problem) { p.Details = v } }

// Instance sets the instance member.
func Instance(s string) Option { return func(p *Problem) { p.Instance = s } }

// New builds a Problem of type t with its default status and title.
func New(t Type, opts ...Option) Problem {
	p := Problem{Type: t, Title: TitleOf(t), Status: StatusOf(t)}
	for _, o := range opts {
		o(&p)
	}
	return p
}

// Write sends a problem response. The request id comes from reqctx. It sets Content-Type
// application/problem+json, Cache-Control no-store and Retry-After when RetryAfter is used.
func Write(w http.ResponseWriter, r *http.Request, t Type, opts ...Option) {
	WriteProblem(w, r, New(t, opts...))
}

// WriteProblem sends an already built Problem.
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.RequestID == "" && r != nil {
		p.RequestID = reqctx.From(r.Context()).RequestID
	}
	if p.Status < 400 || p.Status > 599 {
		p.Status = StatusOf(p.Type)
	}
	body, err := json.Marshal(p)
	if err != nil { // cannot happen: Problem has only plain fields
		body = []byte(`{"type":"internal_error","title":"Internal error","status":500}`)
		p.Status = http.StatusInternalServerError
	}
	h := w.Header()
	h.Set("Content-Type", ContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if p.RetryAfterSeconds > 0 {
		h.Set("Retry-After", strconv.Itoa(p.RetryAfterSeconds))
	}
	w.WriteHeader(p.Status)
	if r != nil && r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// Mapper translates a domain error into a problem type and options. ok is false for errors it does not know.
type Mapper func(err error) (t Type, opts []Option, ok bool)

// WriteError writes err through m. Errors that m does not map become internal_error, and only those are
// logged (with request id and route; never the request body). The client never sees the error text of an
// unmapped error.
func WriteError(w http.ResponseWriter, r *http.Request, err error, m Mapper, log *slog.Logger) {
	if m != nil {
		if t, opts, ok := m(err); ok {
			Write(w, r, t, opts...)
			return
		}
	}
	if log == nil {
		log = slog.Default()
	}
	info := reqctx.From(r.Context())
	log.ErrorContext(r.Context(), "unhandled error",
		"request_id", info.RequestID,
		"method", info.Method,
		"route", info.Route,
		"error", err,
	)
	Write(w, r, InternalError)
}
