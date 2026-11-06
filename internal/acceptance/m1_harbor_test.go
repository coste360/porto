//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/harborcheck"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M1 harbour acceptance (spec 32.3, m1-plan §8.3): a 6-pier, 400-berth harbour configured through the API from an
// orthophoto, with the editor's own calls (generator previews on the current document, then one save).

type harborWorld struct {
	h       *harness
	owner   *client
	marina  string
	M, P    string // marina and map paths
	mapID   string
	version int
	doc     map[string]any // the current document as a save request
}

// harborUpload sends a non-JSON body (image upload).
func (c *client) harborUpload(t *testing.T, method, path, contentType string, body []byte, headers ...string) response {
	t.Helper()
	req, err := http.NewRequest(method, c.h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if tok := c.csrf(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: buf.Bytes()}
}

func harborRequest(doc map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range doc {
		switch k {
		case "id", "version", "updated_at":
		default:
			out[k] = v
		}
	}
	return out
}

func (w *harborWorld) save(t *testing.T) map[string]any {
	t.Helper()
	res := w.owner.expect(t, 200, "PUT", w.P, w.doc, "If-Match", fmt.Sprintf(`"%d"`, w.version))
	d := res["document"].(map[string]any)
	w.version = int(d["version"].(float64))
	w.doc = harborRequest(d)
	return res
}

func jsonList(m map[string]any, k string) []any {
	l, _ := m[k].([]any)
	return l
}

// pierGenerator is the editor's pier generator form for pier i (A–F).
func pierGenerator(i int) map[string]any {
	code := string(rune('A' + i))
	perSide, length := 34, 12000
	if i >= 4 {
		perSide, length = 32, 13000
	}
	return map[string]any{
		"type": "pier", "key_prefix": fmt.Sprintf("g%d-", i+1), "origin": map[string]int{"x": 6000 + i*4500, "y": 2000},
		"rotation_dd": 900, "length_cm": length, "width_cm": 300, "code": code, "root_id": "quay-1", "sides": []string{"left", "right"},
		"berths_per_side": perSide, "berth_width_cm": 350, "berth_length_cm": 1000, "depth_cm": 350,
		"numbering": map[string]any{"scheme": "continuous", "prefix": code + "-"},
	}
}

func buildHarbor400(t *testing.T, db *store.DB) *harborWorld {
	t.Helper()
	h := newHarness(t, db)
	ctx := context.Background()
	o, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto SRL", MarinaName: "Marina Porto", MarinaSlug: "porto",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &harborWorld{h: h, owner: h.cookieClient(), marina: o.MarinaID, M: A + "/marinas/" + o.MarinaID}
	w.owner.login(t, "owner@porto.test")

	// 1. A water map of 800 × 500 m and the orthophoto.
	d := w.owner.expect(t, 201, "POST", w.M+"/maps", map[string]any{
		"kind": "water", "name": map[string]string{"it": "Porto", "en": "Harbour"},
		"width_cm": 80000, "height_cm": 50000,
	}, "Idempotency-Key", "create-map-porto-1")
	w.mapID, w.P, w.version, w.doc = d["id"].(string), w.M+"/maps/"+d["id"].(string), 1, harborRequest(d)
	img, err := os.ReadFile("testdata/harbor/orthophoto.png")
	if err != nil {
		t.Fatal(err)
	}
	up := w.owner.harborUpload(t, "POST", w.P+"/background", "image/png", img)
	if up.status != 201 {
		t.Fatalf("upload: %d %s", up.status, up.body)
	}
	bg := up.json(t)

	// 2. Calibration (1 px = 5 m) and georeference.
	w.doc["background"] = map[string]any{
		"file_id": bg["file_id"], "content_type": "image/png", "width_px": bg["width_px"], "height_px": bg["height_px"],
		"origin": map[string]int{"x": 0, "y": 0}, "scale_mcm_per_px": 500000, "rotation_dd": 0, "opacity_pct": 60, "visible": true,
	}
	w.doc["georef"] = map[string]any{"points": []map[string]int64{
		{"x": 0, "y": 0, "lat_e7": 409_000_000, "lon_e7": 96_000_000},
		{"x": 80000, "y": 0, "lat_e7": 409_000_000, "lon_e7": 96_093_680},
		{"x": 0, "y": 50000, "lat_e7": 408_955_085, "lon_e7": 96_000_000},
	}}
	w.save(t)

	// 3. The quay.
	w.doc["structures"] = []any{map[string]any{
		"key": "quay-1", "kind": "quay", "code": "BANCHINA", "points": []map[string]int{{"x": 0, "y": 2000}, {"x": 80000, "y": 2000}},
		"sides": []string{"right"},
	}}
	w.doc["water_polygons"] = []any{[]map[string]int{{"x": 0, "y": 2000}, {"x": 80000, "y": 2000}, {"x": 80000, "y": 50000}, {"x": 0, "y": 50000}}}
	w.save(t)
	// The quay key is now an id: the generators refer to the saved quay.
	quayID := jsonList(w.doc, "structures")[0].(map[string]any)["id"].(string)

	// 4. Six pier generator runs, each previewed on the current (unsaved) document and inserted.
	for i := range 6 {
		g := pierGenerator(i)
		g["root_id"] = quayID
		res := w.owner.expect(t, 200, "POST", w.P+"/generate", map[string]any{"document": w.doc, "generator": g})
		if rep := res["report"].(map[string]any); len(jsonList(rep, "errors")) != 0 {
			t.Fatalf("pier %d preview errors: %v", i, rep["errors"])
		}
		el := res["elements"].(map[string]any)
		w.doc["structures"] = append(jsonList(w.doc, "structures"), jsonList(el, "structures")...)
		w.doc["berths"] = append(jsonList(w.doc, "berths"), jsonList(el, "berths")...)
	}

	// 5. Save and reload: 400 berths, 6 piers, unique codes.
	w.save(t)
	got := w.owner.expect(t, 200, "GET", w.P, nil)
	berths := jsonList(got, "berths")
	piers, codes := 0, map[string]bool{}
	for _, s := range jsonList(got, "structures") {
		if s.(map[string]any)["kind"] == "pier" {
			piers++
		}
	}
	for _, b := range berths {
		codes[b.(map[string]any)["code"].(string)] = true
	}
	if len(berths) != 400 || piers != 6 || len(codes) != 400 {
		t.Fatalf("%d berths, %d piers, %d codes", len(berths), piers, len(codes))
	}
	return w
}

func TestAcceptance_M1_Harbor(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := buildHarbor400(t, db)
		h, ctx := w.h, context.Background()

		// 6. A dock user reads the map but cannot save it.
		mem := w.owner.expect(t, 201, "POST", w.M+"/members", map[string]any{"email": "dock@porto.test", "name": "Dock", "role": "dock"})
		if err := h.app.Auth.SetPassword(ctx, str(mem, "user_id"), password); err != nil {
			t.Fatal(err)
		}
		dock := h.cookieClient()
		dock.login(t, "dock@porto.test")
		dock.expect(t, 200, "GET", w.P, nil)
		if r := dock.do(t, "PUT", w.P, w.doc, "If-Match", fmt.Sprintf(`"%d"`, w.version)); r.status != 403 || r.problemType() != "forbidden" {
			t.Fatalf("dock save: %d %s", r.status, r.body)
		}

		// 7. Another marina: 404.
		if _, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "other@porto.test", Name: "Other", Password: password,
			Organization: "Altro", MarinaName: "Altro", MarinaSlug: "altro",
		}); err != nil {
			t.Fatal(err)
		}
		other := h.cookieClient()
		other.login(t, "other@porto.test")
		if r := other.do(t, "GET", w.P, nil); r.status != 404 || r.problemType() != "not_found" {
			t.Fatalf("other marina: %d", r.status)
		}

		// 8. JSON export, imported as a second map.
		exp := w.owner.expect(t, 200, "GET", w.P+"/export", nil)
		if exp["format"] != "porto-map/1" {
			t.Fatalf("export format %v", exp["format"])
		}
		imp := w.owner.expect(t, 201, "POST", w.M+"/maps/import", exp, "Idempotency-Key", "import-map-porto-1")
		counts := imp["counts"].(map[string]any)
		if counts["berths"].(float64) != 400 || counts["structures"].(float64) != 7 || imp["georeferenced"] != true {
			t.Fatalf("imported counts %v", counts)
		}
		gj := w.owner.do(t, "GET", w.P+"/export?format=geojson", nil)
		if gj.status != 200 || gj.header.Get("Content-Type") != "application/geo+json" {
			t.Fatalf("geojson %d", gj.status)
		}

		// 9. CSV import on pier F: dry run, then apply.
		csv, err := os.ReadFile("testdata/harbor/berths.csv")
		if err != nil {
			t.Fatal(err)
		}
		req := map[string]any{
			"csv": string(csv), "delimiter": "semicolon", "has_header": true, "units": "m", "dry_run": true,
			"mapping":   map[string]int{"code": 0, "pier": 1, "length": 2, "width": 3, "depth": 4},
			"placement": map[string]string{"start": "after_existing", "side_policy": "alternate"},
		}
		dry := w.owner.expect(t, 200, "POST", w.P+"/berth-import", req)
		if dry["valid"].(float64) != 8 || len(jsonList(dry, "rejected")) != 2 || dry["document"] != nil {
			t.Fatalf("dry run %v", dry)
		}
		req["dry_run"] = false
		applied := w.owner.expect(t, 200, "POST", w.P+"/berth-import", req, "If-Match", fmt.Sprintf(`"%d"`, w.version))
		if n := len(jsonList(applied["document"].(map[string]any)["document"].(map[string]any), "berths")); n != 408 {
			t.Fatalf("%d berths after the import", n)
		}

		// 10. Events and audit.
		var evs []store.OutboxEvent
		if err := db.NewSelect().Model(&evs).Where("type = ?", event.ConfigMapChanged).Where("marina_id = ?", w.marina).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if len(evs) < 6 {
			t.Fatalf("%d config.map_changed events", len(evs))
		}
		for _, action := range []string{"map.create", "map.save", "map.import", "map.berth_import", "map.background_upload"} {
			if n, _ := store.CountAudit(ctx, db.DB, action); n == 0 {
				t.Errorf("no audit row %s", action)
			}
		}

		// 11. INV-04 on the stored rows.
		harborcheck.AssertNoOverlappingBerths(t, db)
		assertNoPII(t, h.logs.String(), password)
	})
}

func berthByCode(doc map[string]any, code string) map[string]any {
	for _, b := range jsonList(doc, "berths") {
		if m := b.(map[string]any); m["code"] == code {
			return m
		}
	}
	return nil
}

func TestAcceptance_M1_RejectsOverlappingSave(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := buildHarbor400(t, db)
		ctx := context.Background()
		before := w.owner.do(t, "GET", w.P, nil)
		var rowsBefore []store.Berth
		if err := db.NewSelect().Model(&rowsBefore).OrderExpr("id").Scan(ctx); err != nil {
			t.Fatal(err)
		}
		eventsBefore, _ := db.NewSelect().Table("outbox_events").Count(ctx)
		auditBefore, _ := db.NewSelect().Table("audit_log").Count(ctx)

		// Move B-12 onto B-13.
		b12, b13 := berthByCode(w.doc, "B-12"), berthByCode(w.doc, "B-13")
		b12["anchor"] = b13["anchor"]
		r := w.owner.do(t, "PUT", w.P, w.doc, "If-Match", fmt.Sprintf(`"%d"`, w.version))
		if r.status != 422 || r.problemType() != "map_invalid" {
			t.Fatalf("overlapping save: %d %s", r.status, r.body)
		}
		var prob struct {
			Issues []struct {
				Code string   `json:"code"`
				Refs []string `json:"refs"`
			} `json:"issues"`
		}
		_ = json.Unmarshal(r.body, &prob)
		ids := []string{b12["id"].(string), b13["id"].(string)}
		found := slices.ContainsFunc(prob.Issues, func(is struct {
			Code string   `json:"code"`
			Refs []string `json:"refs"`
		},
		) bool {
			return is.Code == "berth_overlap" && slices.Contains(is.Refs, ids[0]) && slices.Contains(is.Refs, ids[1])
		})
		if !found {
			t.Fatalf("issues %s", r.body)
		}

		// Nothing changed: version, updated_at, rows, outbox and audit.
		after := w.owner.do(t, "GET", w.P, nil)
		if !bytes.Equal(before.body, after.body) || before.header.Get("ETag") != after.header.Get("ETag") {
			t.Fatal("the refused save changed the map")
		}
		var rowsAfter []store.Berth
		if err := db.NewSelect().Model(&rowsAfter).OrderExpr("id").Scan(ctx); err != nil {
			t.Fatal(err)
		}
		bb, _ := json.Marshal(rowsBefore)
		ab, _ := json.Marshal(rowsAfter)
		if !bytes.Equal(bb, ab) {
			t.Fatal("berth rows changed")
		}
		eventsAfter, _ := db.NewSelect().Table("outbox_events").Count(ctx)
		auditAfter, _ := db.NewSelect().Table("audit_log").Count(ctx)
		if eventsAfter != eventsBefore || auditAfter != auditBefore {
			t.Fatalf("events %d→%d, audit %d→%d", eventsBefore, eventsAfter, auditBefore, auditAfter)
		}

		// validateMap reports the same issue.
		rep := w.owner.expect(t, 200, "POST", w.P+"/validate", w.doc)
		if !bytes.Contains(harborJSON(rep["errors"]), []byte("berth_overlap")) {
			t.Fatalf("validate %v", rep["errors"])
		}

		// A generator preview overlapping pier A reports it.
		b12["anchor"] = berthByCode(harborRequest(after.json(t)), "B-12")["anchor"]
		g := pierGenerator(0)
		g["key_prefix"], g["code"], g["root_id"] = "g9-", "Z", nil
		g["numbering"] = map[string]any{"scheme": "continuous", "prefix": "Z-"}
		delete(g, "root_id")
		res := w.owner.expect(t, 200, "POST", w.P+"/generate", map[string]any{"document": w.doc, "generator": g})
		if !bytes.Contains(harborJSON(res["report"].(map[string]any)["errors"]), []byte("berth_overlap")) {
			t.Fatalf("preview report %v", res["report"])
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func harborJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
