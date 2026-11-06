//go:build integration

package admin_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/pkg/ext"
)

const harborPW = "handler-password-1"

type harborSrv struct {
	t   *testing.T
	app *app.App
	url string
}

func newHarborSrv(t *testing.T) *harborSrv {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler), app.Options{Argon2: auth.TestArgon2Params})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Router)
	t.Cleanup(ts.Close)
	return &harborSrv{t: t, app: a, url: ts.URL}
}

type harborClient struct {
	s    *harborSrv
	http *http.Client
}

type harborResp struct {
	status int
	header http.Header
	body   []byte
}

func (r harborResp) problem() string {
	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r.body, &p)
	return p.Type
}

func (s *harborSrv) login(t *testing.T, email string) *harborClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &harborClient{s: s, http: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
	if r := c.do(t, "POST", "/api/admin/v1/auth/login", map[string]string{"email": email, "password": harborPW}); r.status != 200 {
		t.Fatalf("login %s: %d %s", email, r.status, r.body)
	}
	return c
}

func (c *harborClient) raw(t *testing.T, method, path, ct string, body io.Reader, headers ...string) harborResp {
	t.Helper()
	req, err := http.NewRequest(method, c.s.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	u, _ := url.Parse(c.s.url)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "porto_csrf" {
			req.Header.Set("X-CSRF-Token", ck.Value)
		}
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return harborResp{status: res.StatusCode, header: res.Header, body: b}
}

func (c *harborClient) do(t *testing.T, method, path string, body any, headers ...string) harborResp {
	t.Helper()
	var rd io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd, ct = bytes.NewReader(b), "application/json"
	}
	return c.raw(t, method, path, ct, rd, headers...)
}

// expect checks the status and, for errors, the problem type; it decodes a JSON body into out when given.
func (c *harborClient) expect(t *testing.T, status int, problem, method, path string, body any, out any, headers ...string) harborResp {
	t.Helper()
	r := c.do(t, method, path, body, headers...)
	if r.status != status || (problem != "" && r.problem() != problem) {
		t.Fatalf("%s %s: %d %s, want %d %s", method, path, r.status, r.body, status, problem)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			t.Fatalf("decode %s: %v", r.body, err)
		}
	}
	return r
}

func harborPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 20)))
	return buf.Bytes()
}

func harborPier(t *testing.T, c *harborClient, marinaPath, mapID string, doc map[string]any) map[string]any {
	t.Helper()
	var gen map[string]any
	c.expect(t, 200, "", "POST", marinaPath+"/maps/"+mapID+"/generate", map[string]any{"document": doc, "generator": map[string]any{
		"type": "pier", "key_prefix": "g1-", "origin": map[string]int{"x": 5000, "y": 1000}, "rotation_dd": 900, "length_cm": 4000,
		"width_cm": 300, "code": "A", "sides": []string{"left", "right"}, "berths_per_side": 8, "berth_width_cm": 400,
		"berth_length_cm": 1200, "numbering": map[string]any{"scheme": "continuous", "prefix": "A-"},
	}}, &gen)
	return gen
}

func TestHarborHandlers_Scenario(t *testing.T) {
	{
		s := newHarborSrv(t)
		ctx := context.Background()
		o, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "owner@porto.test", Name: "Owner", Password: harborPW, Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-h"})
		if err != nil {
			t.Fatal(err)
		}
		c := s.login(t, "owner@porto.test")
		M := "/api/admin/v1/marinas/" + o.MarinaID

		// Maps.
		c.expect(t, 400, "validation_failed", "POST", M+"/maps", map[string]any{"kind": "lake", "name": map[string]string{"it": ""}, "width_cm": 1, "height_cm": 1}, nil)
		var doc map[string]any
		r := c.expect(t, 201, "", "POST", M+"/maps", map[string]any{"kind": "water", "name": map[string]string{"it": "Porto"}, "width_cm": 80000, "height_cm": 50000}, &doc)
		if r.header.Get("ETag") != `"1"` {
			t.Fatalf("etag %q", r.header.Get("ETag"))
		}
		mapID := doc["id"].(string)
		P := M + "/maps/" + mapID
		var list api.MapSummaryList
		c.expect(t, 200, "", "GET", M+"/maps", nil, &list)
		if len(list.Items) != 1 || list.Items[0].Version != 1 {
			t.Fatalf("list %+v", list)
		}
		c.expect(t, 200, "", "GET", P, nil, nil)
		for _, k := range []string{"id", "version", "updated_at"} {
			delete(doc, k)
		}
		gen := harborPier(t, c, M, mapID, doc)
		if gen["counts"].(map[string]any)["berths"].(float64) != 16 || gen["report"] == nil {
			t.Fatalf("generate %v", gen["counts"])
		}
		c.expect(t, 400, "validation_failed", "POST", P+"/generate", map[string]any{"generator": map[string]any{"type": "pier", "key_prefix": "x"}}, nil)
		els := gen["elements"].(map[string]any)
		doc["structures"], doc["berths"] = els["structures"], els["berths"]

		c.expect(t, 428, "precondition_required", "PUT", P, doc, nil)
		c.expect(t, 412, "version_conflict", "PUT", P, doc, nil, "If-Match", `"7"`)
		var rep api.MapValidationReport
		c.expect(t, 200, "", "POST", P+"/validate", doc, &rep)
		if len(rep.Errors) != 0 || len(rep.Warnings) == 0 {
			t.Fatalf("validate %+v", rep)
		}
		var saved api.MapSaveResult
		r = c.expect(t, 200, "", "PUT", P, doc, &saved, "If-Match", `"1"`)
		if r.header.Get("ETag") != `"2"` || len(saved.Document.Berths) != 16 || len(saved.IdMap) != 17 {
			t.Fatalf("save etag %q, %d berths", r.header.Get("ETag"), len(saved.Document.Berths))
		}
		// Overlap: 422 map_invalid with issues, nothing written.
		bad, _ := json.Marshal(saved.Document)
		var badDoc map[string]any
		_ = json.Unmarshal(bad, &badDoc)
		for _, k := range []string{"id", "version", "updated_at"} {
			delete(badDoc, k)
		}
		berths := badDoc["berths"].([]any)
		berths[1].(map[string]any)["anchor"] = berths[0].(map[string]any)["anchor"]
		r = c.expect(t, 422, "map_invalid", "PUT", P, badDoc, nil, "If-Match", `"2"`)
		var prob api.Problem
		_ = json.Unmarshal(r.body, &prob)
		if prob.Issues == nil || (*prob.Issues)[0].Code != "berth_overlap" || len((*prob.Issues)[0].Refs) != 2 {
			t.Fatalf("issues %s", r.body)
		}
		c.expect(t, 200, "", "GET", P, nil, nil, "If-None-Match", "")

		// Background.
		img := harborPNG(t)
		if r := c.raw(t, "POST", P+"/background", "image/jpeg", bytes.NewReader(img)); r.status != 400 || r.problem() != "invalid_file" {
			t.Fatalf("png as jpeg: %d %s", r.status, r.body)
		}
		if r := c.raw(t, "POST", P+"/background", "text/plain", strings.NewReader("hello")); r.status != 415 || r.problem() != "unsupported_media_type" {
			t.Fatalf("text: %d %s", r.status, r.body)
		}
		if r := c.raw(t, "POST", P+"/background", "image/png", bytes.NewReader(append(img, make([]byte, 15<<20)...))); r.status != 413 || r.problem() != "file_too_large" {
			t.Fatalf("too large: %d %.200s", r.status, r.body)
		}
		up := c.raw(t, "POST", P+"/background", "image/png", bytes.NewReader(img))
		var bf api.MapBackgroundFile
		if up.status != 201 || json.Unmarshal(up.body, &bf) != nil || bf.WidthPx != 40 {
			t.Fatalf("upload %d %s", up.status, up.body)
		}
		got := c.raw(t, "GET", P+"/background/"+bf.FileId, "", nil)
		if got.status != 200 || !bytes.Equal(got.body, img) || got.header.Get("Content-Type") != "image/png" ||
			!strings.Contains(got.header.Get("Content-Security-Policy"), "sandbox") || got.header.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(got.header.Get("Cache-Control"), "immutable") {
			t.Fatalf("download %d %v", got.status, got.header)
		}
		c.expect(t, 404, "not_found", "GET", P+"/background/"+"01900000-0000-7000-8000-00000000abcd", nil, nil)
		// A background of another map of the same marina is not found through this map (m1-plan §0 rule 10).
		var other map[string]any
		c.expect(t, 201, "", "POST", M+"/maps", map[string]any{"kind": "yard", "name": map[string]string{"it": "Cantiere"}, "width_cm": 5000, "height_cm": 5000}, &other)
		c.expect(t, 404, "not_found", "GET", M+"/maps/"+other["id"].(string)+"/background/"+bf.FileId, nil, nil)
		c.expect(t, 204, "", "DELETE", M+"/maps/"+other["id"].(string), nil, nil, "If-Match", `"1"`)

		// Export, GeoJSON, concession.
		r = c.expect(t, 200, "", "GET", P+"/export", nil, nil)
		if !strings.HasPrefix(r.header.Get("Content-Disposition"), "attachment") {
			t.Fatalf("disposition %q", r.header.Get("Content-Disposition"))
		}
		var exp map[string]any
		_ = json.Unmarshal(r.body, &exp)
		c.expect(t, 409, "map_not_georeferenced", "GET", P+"/export?format=geojson", nil, nil)
		c.expect(t, 409, "map_not_georeferenced", "POST", P+"/concession/geojson", map[string]any{"geojson": map[string]any{"type": "Polygon", "coordinates": []any{}}}, nil)
		var conv api.ConcessionGeojsonResult
		c.expect(t, 200, "", "POST", P+"/concession/geojson", map[string]any{
			"geojson": map[string]any{"type": "Polygon", "coordinates": json.RawMessage(`[[[9.6,40.9],[9.601,40.9],[9.601,40.899],[9.6,40.9]]]`)},
			"georef":  map[string]any{"points": []map[string]int64{{"x": 0, "y": 0, "lat_e7": 409000000, "lon_e7": 96000000}, {"x": 60000, "y": 0, "lat_e7": 409000000, "lon_e7": 96070260}, {"x": 0, "y": 40000, "lat_e7": 408964068, "lon_e7": 96000000}}},
		}, &conv)
		if len(conv.Outline) != 3 || conv.Outline[0].X != 0 || conv.Outline[0].Y != 0 {
			t.Fatalf("concession %+v", conv)
		}
		var imported api.MapSummary
		c.expect(t, 201, "", "POST", M+"/maps/import", exp, &imported)
		if imported.Counts.Berths != 16 {
			t.Fatalf("imported %+v", imported)
		}
		c.expect(t, 400, "validation_failed", "POST", M+"/maps/import", map[string]any{"format": "x", "exported_at": time.Now(), "map": doc, "groups": []any{}}, nil)

		// Berth import.
		imp := map[string]any{
			"csv": "Z-1;A;10;3\n", "delimiter": "semicolon", "has_header": false, "units": "m", "dry_run": true,
			"mapping": map[string]int{"code": 0, "pier": 1, "length": 2, "width": 3}, "placement": map[string]string{"start": "after_existing", "side_policy": "left"},
		}
		var irep api.BerthImportReport
		c.expect(t, 200, "", "POST", P+"/berth-import", imp, &irep)
		if irep.Valid != 1 || irep.Document != nil {
			t.Fatalf("dry run %+v", irep)
		}
		imp["dry_run"] = false
		c.expect(t, 428, "precondition_required", "POST", P+"/berth-import", imp, nil)
		r = c.expect(t, 200, "", "POST", P+"/berth-import", imp, &irep, "If-Match", `"2"`)
		if irep.Document == nil || r.header.Get("ETag") != `"3"` {
			t.Fatalf("apply %+v %q", irep, r.header.Get("ETag"))
		}
		imp["csv"] = "a,\"b"
		imp["dry_run"] = true
		c.expect(t, 400, "csv_invalid", "POST", P+"/berth-import", imp, nil)

		// Berths list.
		var bp api.BerthPage
		c.expect(t, 200, "", "GET", M+"/berths?map_id="+mapID+"&limit=5", nil, &bp)
		if len(bp.Items) != 5 || bp.NextCursor == nil {
			t.Fatalf("berths %+v", bp)
		}
		c.expect(t, 400, "bad_request", "GET", M+"/berths?cursor=nope", nil, nil)

		// Groups.
		var g api.BerthGroup
		r = c.expect(t, 201, "", "POST", M+"/berth-groups", map[string]any{"code": "PONT-A", "name": map[string]string{"it": "Pontile A"}}, &g)
		if r.header.Get("ETag") != `"1"` || g.Kind != "custom" {
			t.Fatalf("group %+v", g)
		}
		c.expect(t, 409, "conflict", "POST", M+"/berth-groups", map[string]any{"code": "PONT-A", "name": map[string]string{"it": "Dup"}}, nil)
		c.expect(t, 200, "", "PATCH", M+"/berth-groups/"+g.Id, map[string]any{"sort_order": 3}, nil, "If-Match", `"1"`)
		c.expect(t, 412, "version_conflict", "PATCH", M+"/berth-groups/"+g.Id, map[string]any{"sort_order": 4}, nil, "If-Match", `"1"`)
		var gl api.BerthGroupList
		c.expect(t, 200, "", "GET", M+"/berth-groups", nil, &gl)
		if len(gl.Items) != 1 || gl.Items[0].SortOrder != 3 {
			t.Fatalf("groups %+v", gl)
		}
		c.expect(t, 204, "", "DELETE", M+"/berth-groups/"+g.Id, nil, nil, "If-Match", `"2"`)

		// Settings.
		var hs api.HarborSettings
		r = c.expect(t, 200, "", "GET", M+"/harbor-settings", nil, &hs)
		if r.header.Get("ETag") != `"1"` || hs.LoaToleranceCm != 50 || len(hs.PriceClassRule.Bands) != 7 {
			t.Fatalf("settings %+v", hs)
		}
		c.expect(t, 200, "", "PATCH", M+"/harbor-settings", map[string]any{"loa_tolerance_cm": 80}, &hs, "If-Match", `"1"`)
		c.expect(t, 412, "version_conflict", "PATCH", M+"/harbor-settings", map[string]any{"loa_tolerance_cm": 90}, nil, "If-Match", `"1"`)
		c.expect(t, 400, "validation_failed", "PATCH", M+"/harbor-settings", map[string]any{"loa_tolerance_cm": 900}, nil, "If-Match", `"2"`)

		// Reorder and delete.
		c.expect(t, 400, "validation_failed", "POST", M+"/maps/reorder", map[string]any{"map_ids": []string{mapID}}, nil)
		c.expect(t, 204, "", "POST", M+"/maps/reorder", map[string]any{"map_ids": []string{imported.Id, mapID}}, nil)
		c.expect(t, 409, "map_not_empty", "DELETE", P, nil, nil, "If-Match", `"3"`)
		c.expect(t, 428, "precondition_required", "DELETE", P, nil, nil)

		// Roles: a dock user reads but cannot configure; another organization sees nothing.
		var mem struct {
			UserID string `json:"user_id"`
		}
		c.expect(t, 201, "", "POST", M+"/members", map[string]any{"email": "dock@porto.test", "name": "Dock", "role": "dock"}, &mem)
		if err := s.app.Auth.SetPassword(ctx, mem.UserID, harborPW); err != nil {
			t.Fatal(err)
		}
		dc := s.login(t, "dock@porto.test")
		dc.expect(t, 200, "", "GET", P, nil, nil)
		dc.expect(t, 403, "forbidden", "PUT", P, doc, nil, "If-Match", `"3"`)
		dc.expect(t, 403, "forbidden", "GET", P+"/export", nil, nil)
		if _, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "other@porto.test", Name: "Other", Password: harborPW, Organization: "Other", MarinaName: "Other", MarinaSlug: "other-h"}); err != nil {
			t.Fatal(err)
		}
		oc := s.login(t, "other@porto.test")
		oc.expect(t, 404, "not_found", "GET", P, nil, nil)
		oc.expect(t, 404, "not_found", "GET", M+"/maps", nil, nil)
		if r := oc.raw(t, "POST", P+"/background", "image/png", bytes.NewReader(img)); r.status != 404 {
			t.Fatalf("other marina upload: %d", r.status)
		}
	}
}

// TestHarborView_RoundTrip: the harbour document types and the contract types carry the same JSON fields (a
// strict round trip both ways keeps every value).
func TestHarborView_RoundTrip(t *testing.T) {
	en := "Harbour"
	doc := harbor.Document{ID: "01900000-0000-7000-8000-00000000abcd", Version: 3, UpdatedAt: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Meta: harbor.Meta{
		Kind: "water", Name: harbor.I18nText{It: "Porto", En: &en}, Icon: "map", WidthCm: 1000, HeightCm: 2000, GridCm: 50, SortOrder: 2,
		Background:        &harbor.Background{FileID: "f", ContentType: "image/png", WidthPx: 10, HeightPx: 20, Origin: geo.Point{X: 1, Y: 2}, ScaleMcmPerPx: 3, RotationDd: 4, OpacityPct: 5, Visible: true},
		Georef:            &harbor.Georef{Points: []harbor.ControlPoint{{X: 1, Y: 2, LatE7: 3, LonE7: 4}}},
		ConcessionOutline: []geo.Point{{X: 1, Y: 1}}, WaterPolygons: [][]geo.Point{{{X: 2, Y: 2}}}, LandPolygons: [][]geo.Point{{{X: 3, Y: 3}}},
	}, Elements: harbor.Elements{
		Structures: []harbor.Structure{{
			ID: "s", Kind: "pier", Code: "A", Name: "Pontile", Points: []geo.Point{{X: 1, Y: 2}}, Closed: true,
			Rect: &harbor.Rect{Anchor: geo.Point{X: 1}, WidthCm: 2, LengthCm: 3, RotationDd: 4}, WidthCm: 300, ParentID: "q", Sides: []string{"left"},
			Floating: true, DepthProfile: []harbor.DepthSpan{{FromCm: 1, ToCm: 2, DepthCm: 3}}, ElevationCm: new(int64), BollardSpacingCm: 5,
		}},
		Berths: []harbor.Berth{{
			ID: "b", Code: "A-1", StructureID: "s", Side: "left", Position: 10, Anchor: geo.Point{X: 1, Y: 2}, RotationDd: 3, WidthCm: 4,
			LengthCm: 5, MaxLOACm: 6, MaxBeamCm: 7, DepthCm: 8, MaxAirDraftCm: 9, MaxDisplacementKg: 10, MooringType: "buoy", SwingRadiusCm: 11,
			OverhangAllowed: true, PedestalID: "p", PedestalSocket: "S1", PowerA: 16, PowerPhase: "single", Water: true, Wifi: true, PriceClass: "L8",
			Attributes: []string{"transit_only"}, OnlineSellable: true, Active: true, GroupIDs: []string{"g"},
		}},
		DynamicAreas: []harbor.DynamicArea{{
			ID: "a", Kind: "linear", Code: "T", StructureID: "s", Side: "left", FromCm: new(int64), ToCm: new(int64),
			Outline: []geo.Point{{X: 1}}, MooringMode: "stern_to", CellCm: new(int64), MarginCm: new(int64), MaxLOACm: 1, MaxBeamCm: 2, MaxHeightCm: 3,
			MaxWeightKg: 4, Supports: []string{"cradle"}, DepthProfile: []harbor.DepthSpan{{FromCm: 1, ToCm: 2, DepthCm: 3}}, BlockedCells: []int{1},
			Exits: []int{2}, GridRotationDd: 900, CellCount: new(int64), PriceClass: "X", Active: true, GroupIDs: []string{"g"},
		}},
		LandResources: []harbor.LandResource{{
			ID: "l", Kind: "box", Code: "B1", Anchor: geo.Point{X: 1}, RotationDd: 2, WidthCm: 3, LengthCm: 4,
			PriceClass: "Y", Attributes: []string{"x"}, Active: true, GroupIDs: []string{},
		}},
		Pedestals: []harbor.Pedestal{{
			ID: "p", Code: "C1", Position: geo.Point{X: 1}, Sockets: []harbor.Socket{{Code: "S1", Kind: "power", Amps: 16, Phase: "single"}},
			Meters: []harbor.Meter{{Kind: "water", Serial: "123"}},
		}},
		Decorations: []harbor.Decoration{{
			ID: "d", Kind: "symbol", Asset: "crane", Variant: "v", Position: &geo.Point{X: 1}, RotationDd: new(int64), SizeCm: 2,
			Points: []geo.Point{{X: 3}}, Label: &harbor.I18nText{It: "x"}, DepthCm: new(int64),
		}},
	}}
	out := view.MapDocument(&doc)
	var back harbor.Document
	b, _ := json.Marshal(out)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, doc) {
		t.Fatalf("round trip lost data:\n%+v\n%+v", back, doc)
	}
	req, err := view.SaveRequestIn(harborSaveRequest(t, b))
	if err != nil || !reflect.DeepEqual(req, doc.Request()) {
		t.Fatalf("save request %v", err)
	}
}

func harborSaveRequest(t *testing.T, docJSON []byte) api.MapSaveRequest {
	t.Helper()
	var m map[string]any
	_ = json.Unmarshal(docJSON, &m)
	for _, k := range []string{"id", "version", "updated_at"} {
		delete(m, k)
	}
	b, _ := json.Marshal(m)
	var out api.MapSaveRequest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestAdmin_HarborHasNoStubs: every harbour operation is implemented (the CONTRACT stub marker is gone).
func TestAdmin_HarborHasNoStubs(t *testing.T) {
	b, err := os.ReadFile("harbor.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "STUB(M1)") || strings.Contains(string(b), "notImplemented") {
		t.Fatal("harbor.go still has stubs")
	}
}
