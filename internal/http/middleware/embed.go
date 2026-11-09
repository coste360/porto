package middleware

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// EmbedOriginsResolver returns the origins allowed to embed the booking page of a marina slug (marinas.
// allowed_embed_origins); ok is false for an unknown slug.
type EmbedOriginsResolver func(ctx context.Context, slug string) (origins []string, ok bool)

// maxFrameOrigins bounds the origins written in one header (the marina form accepts at most 20).
const maxFrameOrigins = 20

var (
	embedSlugPattern   = regexp.MustCompile(`^[a-z0-9-]{3,63}$`)
	embedOriginPattern = regexp.MustCompile(`^https?://[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)
)

// ParseEmbedOrigin normalises an allowed embed origin (lower case, no trailing slash) and reports whether it is a
// bare http(s) origin: scheme, host and optional port, nothing else. Anything that could break out of a CSP source
// list (spaces, ';', ',', quotes, wildcards, paths) is refused.
func ParseEmbedOrigin(s string) (string, bool) {
	o := strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
	if !embedOriginPattern.MatchString(o) {
		return "", false
	}
	return o, true
}

// bookingPageSlug returns the marina slug of a booking HTML page path (/b/{slug} and everything below it). Asset
// and root-file paths (/b/assets/..., /b/embed.js, /b/sw.js) are not pages.
func bookingPageSlug(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, "/b/")
	if !ok {
		// M12: the public profile page /p/{slug} is framed like the booking page (m12-plan §12.3).
		rest, ok = strings.CutPrefix(p, "/p/")
	}
	if !ok || strings.HasPrefix(rest, "assets/") {
		return "", false
	}
	slug, _, _ := strings.Cut(rest, "/")
	if !embedSlugPattern.MatchString(slug) {
		return "", false
	}
	return slug, true
}

// EmbedFraming relaxes the framing headers of the static booking pages /b/{slug}... for the marina's allowed
// origins (m8-plan §10): Content-Security-Policy frame-ancestors 'self' <origins> with X-Frame-Options removed
// when origins exist, frame-ancestors 'self' with X-Frame-Options SAMEORIGIN when none exist or the slug is
// unknown. It runs after SecurityHeaders and only on the booking routes, so every other response keeps
// frame-ancestors 'none' and X-Frame-Options DENY. A nil resolver means no marina allows embedding.
//
// form-action keeps 'self': the checkout leaves the page by a top-level navigation on the user's click
// (window.open(url, '_top')), never by a form post toward the provider.
func EmbedFraming(resolve EmbedOriginsResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug, ok := bookingPageSlug(r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			var origins []string
			if resolve != nil {
				if raw, known := resolve(r.Context(), slug); known {
					for _, o := range raw {
						if n, valid := ParseEmbedOrigin(o); valid && len(origins) < maxFrameOrigins {
							origins = append(origins, n)
						}
					}
				}
			}
			h := w.Header()
			ancestors := "frame-ancestors 'self'"
			if len(origins) > 0 {
				ancestors += " " + strings.Join(origins, " ")
				h.Del("X-Frame-Options")
			} else {
				h.Set("X-Frame-Options", "SAMEORIGIN")
			}
			h.Set("Content-Security-Policy", withDirective(h.Get("Content-Security-Policy"), "frame-ancestors", ancestors))
			next.ServeHTTP(w, r)
		})
	}
}

// withDirective replaces (or appends) the directive name of a CSP header value with full.
func withDirective(csp, name, full string) string {
	parts := strings.Split(csp, ";")
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		d := strings.TrimSpace(p)
		if d == "" {
			continue
		}
		if f := strings.Fields(d); len(f) > 0 && strings.EqualFold(f[0], name) {
			if !replaced {
				out = append(out, full)
				replaced = true
			}
			continue
		}
		out = append(out, d)
	}
	if !replaced {
		out = append(out, full)
	}
	return strings.Join(out, "; ")
}
