package static

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func testApp() *App {
	return &App{name: "admin", prefix: "/admin/", index: []byte("<!doctype html>"), fsys: fstest.MapFS{
		"sw.js":                  {Data: []byte("self.addEventListener('fetch',()=>{})")},
		"assets/index-abc.js":    {Data: []byte("console.log(1)")},
		".vite/manifest.json":    {Data: []byte("{}")},
		"site.webmanifest":       {Data: []byte("{}")},
		"android-chrome-192.png": {Data: []byte("png")},
	}}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestStatic_ServiceWorkerHeaders(t *testing.T) {
	h := testApp().Handler()
	rec := get(h, "/admin/sw.js")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("Service-Worker-Allowed") != "/admin/" ||
		rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("sw.js %d %v", rec.Code, rec.Header())
	}
	if rec := get(h, "/admin/assets/index-abc.js"); rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset %v", rec.Header())
	}
	if rec := get(h, "/admin/site.webmanifest"); rec.Header().Get("Service-Worker-Allowed") != "" {
		t.Fatalf("manifest %v", rec.Header())
	}
}

func TestStatic_ViteManifestNotServed(t *testing.T) {
	h := testApp().Handler()
	if rec := get(h, "/admin/.vite/manifest.json"); rec.Code != http.StatusNotFound {
		t.Fatalf(".vite served: %d", rec.Code)
	}
}

// TestEmbedJS_Served checks the widget loader (M8, m8-plan §10): public cache for an hour, readable cross-origin,
// a JavaScript content type (nosniff is global) and under 3 KB gzip.
func TestEmbedJS_Served(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "booking", "public", EmbedScriptFile))
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	_, _ = zw.Write(src)
	_ = zw.Close()
	if gz.Len() >= 3*1024 {
		t.Fatalf("embed.js is %d bytes gzip, limit 3072", gz.Len())
	}

	a := &App{name: "booking", prefix: "/b/", index: []byte("<!doctype html>"), fsys: fstest.MapFS{
		EmbedScriptFile:        {Data: src},
		"manifest.webmanifest": {Data: []byte("{}")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
	}}
	rec := httptest.NewRecorder()
	a.ServeFile(rec, httptest.NewRequest("GET", "/embed.js", nil), EmbedScriptFile, false)
	h := rec.Header()
	if rec.Code != 200 || h.Get("Cache-Control") != "public, max-age=3600" ||
		h.Get("Cross-Origin-Resource-Policy") != "cross-origin" || h.Get("Access-Control-Allow-Origin") != "*" ||
		h.Get("Content-Type") != "text/javascript; charset=utf-8" || rec.Body.Len() != len(src) {
		t.Fatalf("embed.js %d %v", rec.Code, h)
	}
	if rec := get(a.Handler(), "/b/assets/index-abc.js"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("asset is CORS-readable: %v", rec.Header())
	}
	if rec := get(a.Handler(), "/b/manifest.webmanifest"); rec.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest %v", rec.Header())
	}
}
