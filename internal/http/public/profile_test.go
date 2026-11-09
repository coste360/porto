//go:build integration

package public

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: fixtures and assertions read the database directly
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type profileEnv struct {
	*fixture.Env
	svc  *profile.Service
	h    *Handler
	page *ProfilePage
}

func newProfileEnv(t *testing.T, db *store.DB) *profileEnv {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	prices := pricelist.NewService(pricelist.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	svc := profile.NewService(profile.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Files: files, Prices: prices, BaseURL: "https://porto.example"})
	mar := marina.NewService(db, e.Clock, e.Audit, e.Outbox, e.Log)
	index := []byte(`<!doctype html><html lang="it"><head><meta charset="UTF-8" /><title>Porto</title><script type="module" src="/b/assets/x.js"></script></head><body><div id="root"></div></body></html>`)
	return &profileEnv{Env: e, svc: svc, h: &Handler{Marinas: mar, Profiles: svc}, page: &ProfilePage{Profiles: svc, Index: func() []byte { return index }}}
}

func p64(v int64) *int64 { return &v }

func (e *profileEnv) publish(t *testing.T) string {
	t.Helper()
	ctx := fixture.Ctx()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 10, 10)))
	f, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/png", &buf)
	if err != nil {
		t.Fatal(err)
	}
	in := profile.Input{
		EntranceLatE7: p64(437101234), EntranceLonE7: p64(104012345), Phone: "+39 050 000000",
		Description: profile.Text{"it": "Un porto sicuro"}, Photos: []profile.Photo{{FileID: f.FileID, Alt: profile.Text{"it": "Vista"}}},
		ShowPrices: true,
	}
	if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
		t.Fatal(err)
	}
	return f.FileID
}

func serve(fn func(w http.ResponseWriter, r *http.Request), path string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	fn(w, r)
	return w
}

func assertNoCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if v := w.Header().Values("Set-Cookie"); len(v) > 0 {
		t.Fatalf("public response sets cookies: %v", v)
	}
}

func TestPublicProfileHandlers_NotFoundWhenUnpublished(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newProfileEnv(t, db)
		unknown := serve(func(w http.ResponseWriter, r *http.Request) {
			e.h.GetPublicProfile(w, r, "nessun-porto", api.GetPublicProfileParams{})
		}, "/api/public/v1/nessun-porto/profile")
		known := serve(func(w http.ResponseWriter, r *http.Request) {
			e.h.GetPublicProfile(w, r, "marina-test", api.GetPublicProfileParams{})
		}, "/api/public/v1/marina-test/profile")
		if unknown.Code != 404 || known.Code != 404 {
			t.Fatalf("status %d %d", unknown.Code, known.Code)
		}
		strip := func(b []byte) string {
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			delete(m, "instance")
			delete(m, "request_id")
			out, _ := json.Marshal(m)
			return string(out)
		}
		if strip(unknown.Body.Bytes()) != strip(known.Body.Bytes()) {
			t.Fatalf("unpublished differs from unknown:\n%s\n%s", unknown.Body, known.Body)
		}
		for name, fn := range map[string]func(w http.ResponseWriter, r *http.Request){
			"map": func(w http.ResponseWriter, r *http.Request) {
				e.h.GetPublicProfileMap(w, r, "marina-test", api.GetPublicProfileMapParams{})
			},
			"prices": func(w http.ResponseWriter, r *http.Request) {
				e.h.GetPublicProfilePrices(w, r, "marina-test", api.GetPublicProfilePricesParams{})
			},
			"occupancy": func(w http.ResponseWriter, r *http.Request) {
				e.h.GetPublicOccupancy(w, r, "marina-test", api.GetPublicOccupancyParams{})
			},
			"photo": func(w http.ResponseWriter, r *http.Request) {
				e.h.GetPublicProfilePhoto(w, r, "marina-test", store.NewID())
			},
			"page": e.page.ServeHTTP,
		} {
			w := serve(fn, "/p/marina-test")
			if w.Code != 404 {
				t.Errorf("%s: %d", name, w.Code)
			}
			assertNoCookie(t, w)
		}
		w := serve(e.page.ServeHTTP, "/p/marina-test")
		if w.Header().Get("X-Robots-Tag") != "noindex" {
			t.Fatal("unpublished page must be noindex")
		}
	})
}

func TestPublicProfileHandlers_CacheAndETag(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newProfileEnv(t, db)
		fileID := e.publish(t)
		get := func(headers ...string) *httptest.ResponseRecorder {
			return serve(func(w http.ResponseWriter, r *http.Request) {
				e.h.GetPublicProfile(w, r, "marina-test", api.GetPublicProfileParams{})
			}, "/api/public/v1/marina-test/profile", headers...)
		}
		w := get()
		if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("ETag") == "" {
			t.Fatalf("profile %d %v", w.Code, w.Header())
		}
		assertNoCookie(t, w)
		var pp api.PublicProfile
		if err := json.Unmarshal(w.Body.Bytes(), &pp); err != nil {
			t.Fatal(err)
		}
		if pp.Name != "Marina Test" || pp.Latitude == nil || *pp.Latitude != "43.7101234" || pp.Photos == nil || len(*pp.Photos) != 1 ||
			pp.PageUrl != "https://porto.example/p/marina-test" || pp.Sections.Prices || pp.BookingUrl != nil {
			t.Fatalf("public profile %+v", pp)
		}
		if w2 := get("If-None-Match", w.Header().Get("ETag")); w2.Code != 304 || w2.Body.Len() != 0 {
			t.Fatalf("304 expected, got %d", w2.Code)
		}
		// Prices on, but no list publishes the matrix: 404.
		wp := serve(func(w http.ResponseWriter, r *http.Request) {
			e.h.GetPublicProfilePrices(w, r, "marina-test", api.GetPublicProfilePricesParams{})
		}, "/x")
		if wp.Code != 404 {
			t.Fatalf("prices %d", wp.Code)
		}
		ph := serve(func(w http.ResponseWriter, r *http.Request) { e.h.GetPublicProfilePhoto(w, r, "marina-test", fileID) }, "/x")
		if ph.Code != 200 || ph.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || ph.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("photo %d %v", ph.Code, ph.Header())
		}
		if ph2 := serve(func(w http.ResponseWriter, r *http.Request) { e.h.GetPublicProfilePhoto(w, r, "marina-test", fileID) }, "/x", "If-None-Match", ph.Header().Get("ETag")); ph2.Code != 304 {
			t.Fatalf("photo 304: %d", ph2.Code)
		}
		mc := serve(func(w http.ResponseWriter, r *http.Request) { e.h.GetPublicMarina(w, r, "marina-test") }, "/x")
		var m api.PublicMarina
		_ = json.Unmarshal(mc.Body.Bytes(), &m)
		if m.ProfilePublished == nil || !*m.ProfilePublished {
			t.Fatalf("profile_published %s", mc.Body)
		}
	})
}

func TestProfilePage_Head(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newProfileEnv(t, db)
		e.publish(t)
		w := serve(e.page.ServeHTTP, "/p/marina-test")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("ETag") == "" ||
			!strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("page %d %v", w.Code, w.Header())
		}
		assertNoCookie(t, w)
		body := w.Body.String()
		for _, want := range []string{
			`<link rel="canonical" href="https://porto.example/p/marina-test">`, `hreflang="it"`, `<title>Marina Test</title>`,
			`<meta name="description" content="Un porto sicuro">`, `og:image`, `<div id="root"></div>`, `/b/assets/x.js`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("page misses %s:\n%s", want, body)
			}
		}
		if strings.Count(body, `application/ld+json`) != 1 || strings.Count(body, "<title>") != 1 {
			t.Fatalf("head injected wrongly:\n%s", body)
		}
		i := strings.Index(body, `<script type="application/ld+json">`) + len(`<script type="application/ld+json">`)
		j := strings.Index(body[i:], "</script>")
		var ld map[string]any
		if err := json.Unmarshal([]byte(body[i:i+j]), &ld); err != nil || ld["name"] != "Marina Test" {
			t.Fatalf("json-ld %v %v", err, ld)
		}
		if w2 := serve(e.page.ServeHTTP, "/p/marina-test", "If-None-Match", w.Header().Get("ETag")); w2.Code != 304 {
			t.Fatalf("page 304: %d", w2.Code)
		}
		pv := serve(e.page.ServeHTTP, "/p/marina-test?preview=1")
		if pv.Code != 200 || pv.Header().Get("Cache-Control") != "no-store" || pv.Header().Get("X-Robots-Tag") != "noindex" ||
			strings.Contains(pv.Body.String(), "ld+json") {
			t.Fatalf("preview %d %v", pv.Code, pv.Header())
		}
		if bad := serve(e.page.ServeHTTP, "/p/Marina_Test"); bad.Code != 404 {
			t.Fatalf("bad slug %d", bad.Code)
		}
	})
}

func TestProfilePage_InjectHead(t *testing.T) {
	idx := []byte(`<html lang="it"><head><title>Porto</title></head><body></body></html>`)
	got := string(injectHead(idx, "<title>X</title>\n", "en"))
	if got != `<html lang="en"><head><title>X</title>`+"\n"+`</head><body></body></html>` {
		t.Fatalf("inject: %s", got)
	}
	if !etagMatch(`W/"a", "b"`, `"b"`) || etagMatch(`"a"`, `"b"`) || !etagMatch("*", `"x"`) || etagMatch("", `"x"`) {
		t.Fatal("etagMatch")
	}
}
