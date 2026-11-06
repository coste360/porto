package event

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Egress sends outgoing requests through the anti-SSRF client. Packages that must not import net/http (domain
// services such as webhook) use it instead of an http.Client.
type Egress struct {
	policy EgressPolicy
	client *http.Client
}

// NewEgress returns an Egress with the given policy and per-request timeout.
func NewEgress(p EgressPolicy, timeout time.Duration) *Egress {
	return &Egress{policy: p, client: NewEgressClient(p, timeout)}
}

// Policy returns the destination policy.
func (e *Egress) Policy() EgressPolicy { return e.policy }

// PostResult is the outcome of a POST that reached the server.
type PostResult struct {
	Status  int
	Excerpt string // first bytes of the response body, sanitised
}

// PostJSON sends body as application/json with the given headers. It checks the URL against the policy first;
// the resolved address is checked again when dialling. Redirects are not followed (a 3xx is a result).
func (e *Egress) PostJSON(ctx context.Context, rawURL string, headers map[string]string, body []byte, maxExcerpt int) (PostResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return PostResult{}, fmt.Errorf("%w: unparsable", ErrEgressURL)
	}
	if err := e.policy.CheckURL(u); err != nil {
		return PostResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return PostResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return PostResult{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxExcerpt)))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return PostResult{Status: resp.StatusCode, Excerpt: Sanitize(raw, maxExcerpt)}, nil
}

// Sanitize makes a response excerpt safe to store and show: valid UTF-8, no control characters except newline
// and tab, at most n bytes.
func Sanitize(b []byte, n int) string {
	var out strings.Builder
	for len(b) > 0 && out.Len() < n {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		switch {
		case r == utf8.RuneError && size <= 1:
			r = '?'
		case r == '\n' || r == '\t':
		case unicode.IsControl(r):
			r = ' '
		}
		if out.Len()+utf8.RuneLen(r) > n {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}
