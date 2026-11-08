// Package static serves the embedded single-page apps: the admin panel under /admin/ and the booking pages under
// /b/{slug}. The builds are copied here by `make web-build`; while a build is missing, a placeholder page is
// served instead, so that a fresh checkout still runs.
package static

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:admin all:booking
var files embed.FS

// App is one embedded SPA.
type App struct {
	name   string
	prefix string // URL prefix of the assets, e.g. "/admin/"
	fsys   fs.FS
	index  []byte // nil when the build is missing
}

// Load returns the app embedded under dir ("admin" or "booking").
func Load(dir, prefix string) *App {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		sub = emptyFS{}
	}
	a := &App{name: dir, prefix: prefix, fsys: sub}
	if b, err := fs.ReadFile(sub, "index.html"); err == nil {
		a.index = b
	}
	return a
}

// Built reports whether the SPA build is embedded.
func (a *App) Built() bool { return a.index != nil }

// ServeIndex writes index.html (never cached, so a deploy is picked up at once) or the placeholder.
func (a *App) ServeIndex(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	if a.index == nil {
		w.WriteHeader(http.StatusOK)
		_ = placeholder.Execute(w, a.name)
		return
	}
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(a.index))
}

// ServeFile serves the file at rel (relative to the build root). Hashed assets under assets/ are immutable;
// unknown paths fall back to index.html when fallback is true (client-side routes), else 404.
func (a *App) ServeFile(w http.ResponseWriter, r *http.Request, rel string, fallback bool) {
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if rel == "" || rel == "index.html" {
		a.ServeIndex(w, r)
		return
	}
	if strings.HasPrefix(rel, ".vite/") {
		http.NotFound(w, r) // the build manifest is an input of sw-precache, never served
		return
	}
	f, err := a.fsys.Open(rel)
	if err == nil {
		defer f.Close()
		st, err := f.Stat()
		if err == nil && !st.IsDir() {
			if rs, ok := f.(readSeeker); ok {
				switch {
				case rel == ServiceWorkerFile:
					// The service worker (M6b) is revalidated on every check, so that a deploy is picked up, and
					// may control the whole prefix.
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Service-Worker-Allowed", a.prefix)
					w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
				case strings.HasPrefix(rel, "assets/"):
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				default:
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				http.ServeContent(w, r, st.Name(), time.Time{}, rs)
				return
			}
		}
	}
	if fallback && path.Ext(rel) == "" {
		a.ServeIndex(w, r)
		return
	}
	http.NotFound(w, r)
}

// ServiceWorkerFile is the service worker of the admin panel (m6b-plan §3.1), built without a hash.
const ServiceWorkerFile = "sw.js"

type readSeeker interface {
	Read(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
}

// Handler returns the handler of an SPA mounted at a.prefix, with client-side routing fallback.
func (a *App) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.ServeFile(w, r, strings.TrimPrefix(r.URL.Path, a.prefix), true)
	})
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// The placeholder carries no inline style or script, so that it passes the CSP.
var placeholder = template.Must(template.New("placeholder").Parse(`<!doctype html>
<html lang="it">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Porto</title>
</head>
<body>
<main>
<h1>Porto</h1>
<p>L'interfaccia web ({{.}}) non è inclusa in questo binario. Esegui <code>make build</code> per incorporarla.</p>
<p lang="en">The web interface ({{.}}) is not embedded in this binary. Run <code>make build</code> to embed it.</p>
</main>
</body>
</html>
`))
