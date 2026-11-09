package public

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"

	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/http/respond"
)

// ProfilePage serves /p/{slug} (M12, m12-plan §12.3): the booking SPA index.html with a server-injected head (title,
// description, canonical, hreflang, Open Graph, JSON-LD Marina/Place). Unpublished or unknown slugs are 404 with
// X-Robots-Tag: noindex; published pages are Cache-Control public, max-age=300 with an ETag, framed like the
// booking page. ?preview=1 serves the bare SPA (no-store, noindex) for every slug: the SPA then loads the draft
// through the authenticated previewMarinaProfile, so the page itself reveals nothing. No cookie is ever set.
type ProfilePage struct {
	Profiles *profile.Service
	// Index returns the booking SPA index.html (static.App of the booking build).
	Index func() []byte
}

var (
	profilePath = regexp.MustCompile(`^/p/([a-z0-9-]{3,63})/?$`)
	titleTag    = regexp.MustCompile(`(?is)<title>.*?</title>\s*`)
)

// fallbackIndex is served when the booking SPA is not built (tests, `go run` without the web build).
const fallbackIndex = `<!doctype html><html lang="it"><head><meta charset="UTF-8"><title>Porto</title></head><body><div id="root"></div></body></html>`

func (p *ProfilePage) index() []byte {
	if p.Index != nil {
		if b := p.Index(); len(b) > 0 {
			return b
		}
	}
	return []byte(fallbackIndex)
}

func profileNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}

// injectHead replaces the <title> of the index with the head and sets the document language.
func injectHead(index []byte, head, lang string) []byte {
	out := titleTag.ReplaceAll(index, nil)
	i := bytes.Index(bytes.ToLower(out), []byte("</head>"))
	if i < 0 {
		return out
	}
	var b bytes.Buffer
	b.Grow(len(out) + len(head))
	b.Write(out[:i])
	b.WriteString(head)
	b.Write(out[i:])
	res := b.Bytes()
	if lang == "en" {
		res = bytes.Replace(res, []byte(`<html lang="it"`), []byte(`<html lang="en"`), 1)
	}
	return res
}

// ServeHTTP implements http.Handler.
func (p *ProfilePage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m := profilePath.FindStringSubmatch(r.URL.Path)
	if m == nil || p.Profiles == nil {
		profileNotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	if r.URL.Query().Get("preview") != "" {
		h.Set("Cache-Control", "no-store")
		h.Set("X-Robots-Tag", "noindex")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(p.index())
		}
		return
	}
	lang := strings.ToLower(r.URL.Query().Get("lang"))
	head, err := p.Profiles.PageHead(r.Context(), m[1], lang)
	if err != nil {
		if isProfileNotFound(err) {
			profileNotFound(w, r)
			return
		}
		respond.Error(w, r, err)
		return
	}
	body := injectHead(p.index(), head.HTML, lang)
	etag := `"` + strings.Trim(head.ETag, `"`) + "-" + shortETag(body) + `"`
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("ETag", etag)
	h.Set("Vary", "Accept-Encoding")
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
