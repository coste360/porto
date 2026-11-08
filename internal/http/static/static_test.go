package static

import (
	"net/http"
	"net/http/httptest"
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
