//go:build integration

package admin_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: fixtures and assertions read the database directly
)

func profileBody() map[string]any {
	return map[string]any{
		"locales": []string{"it"}, "entrance_lat_e7": 437101234, "entrance_lon_e7": 104012345,
		"address": map[string]any{"street": "Via del Porto 1", "city": "Pisa", "country": "IT"},
		"phone":   "+39 050 000000", "email": "info@porto.example", "website": "https://porto.example",
		"vhf_channels": []int{9}, "office_hours": []map[string]any{{"weekday": 1, "from": "08:00", "to": "19:00"}},
		"description": map[string]string{"it": "Un porto"}, "depth_min_cm": 250, "depth_max_cm": 600, "seabed": "sand",
		"shelter": []string{"N"}, "services": map[string]any{"water": true, "power_amps": []int{16}, "fuel": map[string]any{"diesel": true}},
		"show_map": true, "poi": []map[string]any{{"kind": "fuel", "x_cm": 100, "y_cm": 200}},
	}
}

func profileJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProfileHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var p api.MarinaProfile
	r := c.expect(t, 200, "", "GET", M+"/profile", nil, &p)
	if p.Version != 0 || p.Published || r.header.Get("ETag") != etag(0) || p.Missing == nil || len(*p.Missing) != 2 ||
		!strings.HasSuffix(p.PublicUrl, "/p/marina-p") {
		t.Fatalf("defaults %+v", p)
	}
	ps.dock.expect(t, 200, "", "GET", M+"/profile", nil, nil) // marina.read
	ps.other.expect(t, 404, "not_found", "GET", M+"/profile", nil, nil)

	body := profileBody()
	c.expect(t, 428, "precondition_required", "PUT", M+"/profile", body, nil)
	c.expect(t, 412, "version_conflict", "PUT", M+"/profile", body, nil, "If-Match", etag(4))
	ps.dock.expect(t, 403, "forbidden", "PUT", M+"/profile", body, nil, "If-Match", etag(0))
	ps.other.expect(t, 404, "not_found", "PUT", M+"/profile", body, nil, "If-Match", etag(0))
	bad := profileBody()
	bad["website"] = "http://insecure.example"
	c.expect(t, 400, "validation_failed", "PUT", M+"/profile", bad, nil, "If-Match", etag(0))
	c.expect(t, 422, "profile_incomplete", "POST", M+"/profile/publish", nil, nil, "If-Match", etag(0))

	r = c.expect(t, 200, "", "PUT", M+"/profile", body, &p, "If-Match", etag(0))
	if p.Version != 1 || r.header.Get("ETag") != etag(1) || p.Services == nil || p.Services.Fuel == nil || p.Services.Fuel.Diesel == nil ||
		p.Poi == nil || len(*p.Poi) != 1 || p.Seabed == nil || *p.Seabed != "sand" || len(*p.Missing) != 0 {
		t.Fatalf("saved %+v", p)
	}

	// Photos: raw body, re-encoded; another marina is 404, wrong types 415.
	img := profileJPEG(t)
	if r := ps.other.raw(t, "POST", M+"/profile/photos", "image/jpeg", bytes.NewReader(img)); r.status != 404 {
		t.Fatalf("cross-marina upload %d", r.status)
	}
	if r := c.raw(t, "POST", M+"/profile/photos", "image/gif", bytes.NewReader(img)); r.status != 415 || r.problem() != "unsupported_media_type" {
		t.Fatalf("gif %d %s", r.status, r.body)
	}
	if r := c.raw(t, "POST", M+"/profile/photos", "image/jpeg", strings.NewReader("garbage")); r.status != 422 || r.problem() != "photo_invalid" {
		t.Fatalf("garbage %d %s", r.status, r.body)
	}
	up := c.raw(t, "POST", M+"/profile/photos", "image/jpeg", bytes.NewReader(img))
	if up.status != 201 {
		t.Fatalf("upload %d %s", up.status, up.body)
	}
	var f api.ProfilePhotoFile
	if err := json.Unmarshal(up.body, &f); err != nil {
		t.Fatal(err)
	}
	if f.Width != 32 || f.Height != 24 || f.ContentType != "image/jpeg" {
		t.Fatalf("photo %+v", f)
	}
	if r := c.raw(t, "GET", M+"/profile/photos/"+f.FileId, "", nil); r.status != 200 || r.header.Get("Content-Type") != "image/jpeg" ||
		r.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("get photo %d %v", r.status, r.header)
	}
	ps.other.expect(t, 404, "not_found", "GET", M+"/profile/photos/"+f.FileId, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/profile/photos/"+f.FileId, nil, nil)
	c.expect(t, 404, "not_found", "GET", M+"/profile/photos/"+store.NewID(), nil, nil)

	body["photos"] = []map[string]any{{"file_id": f.FileId, "alt": map[string]string{"it": "Banchina"}}}
	c.expect(t, 200, "", "PUT", M+"/profile", body, &p, "If-Match", etag(1))
	if p.Photos == nil || len(*p.Photos) != 1 || (*p.Photos)[0].FileId != f.FileId {
		t.Fatalf("photos %+v", p.Photos)
	}

	// Preview: draft JSON + JSON-LD, never cached.
	var prev api.MarinaProfilePreview
	r = c.expect(t, 200, "", "GET", M+"/profile/preview?lang=it", nil, &prev)
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Robots-Tag") != "noindex" || prev.Profile.Name != "Marina" ||
		prev.JsonLd["@type"] == nil || len(*prev.Profile.Photos) != 1 {
		t.Fatalf("preview %v %+v", r.header, prev)
	}
	ps.dock.expect(t, 403, "forbidden", "GET", M+"/profile/preview", nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", M+"/profile/preview", nil, nil)

	ps.other.expect(t, 404, "not_found", "POST", M+"/profile/publish", nil, nil, "If-Match", etag(2))
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/profile/publish", nil, nil, "If-Match", etag(2))
	c.expect(t, 412, "version_conflict", "POST", M+"/profile/publish", nil, nil, "If-Match", etag(1))
	c.expect(t, 200, "", "POST", M+"/profile/publish", nil, &p, "If-Match", etag(2))
	if !p.Published || p.Version != 3 || p.PublishedAt == nil {
		t.Fatalf("published %+v", p)
	}

	// The public marina card links the profile.
	var pm api.PublicMarina
	c.expect(t, 200, "", "GET", "/api/public/v1/marina-p", nil, &pm)
	if pm.ProfilePublished == nil || !*pm.ProfilePublished {
		t.Fatalf("public marina %+v", pm)
	}

	ps.other.expect(t, 404, "not_found", "DELETE", M+"/profile/photos/"+f.FileId, nil, nil, "If-Match", etag(3))
	c.expect(t, 412, "version_conflict", "DELETE", M+"/profile/photos/"+f.FileId, nil, nil, "If-Match", etag(2))
	c.expect(t, 200, "", "DELETE", M+"/profile/photos/"+f.FileId, nil, &p, "If-Match", etag(3))
	if p.Version != 4 || len(*p.Photos) != 0 {
		t.Fatalf("after delete %+v", p)
	}
	c.expect(t, 404, "not_found", "GET", M+"/profile/photos/"+f.FileId, nil, nil)

	ps.other.expect(t, 404, "not_found", "POST", M+"/profile/unpublish", nil, nil, "If-Match", etag(4))
	c.expect(t, 200, "", "POST", M+"/profile/unpublish", nil, &p, "If-Match", etag(4))
	if p.Published {
		t.Fatal("still published")
	}
	c.expect(t, 404, "not_found", "GET", "/api/public/v1/marina-p/profile", nil, nil)
}
