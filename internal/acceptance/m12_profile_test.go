//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M12_PublicProfile (m12-plan §17.4): the owner fills and publishes the profile through the API; an
// anonymous visitor fetches /p/{slug} and every public operation: JSON-LD parses, the aggregate occupancy counts
// match the seeded rows, the price matrix equals getPriceMatrix, no response sets cookies and no customer, vessel
// or berth code ever appears (INV-103, TestPublicProfile_NoPII).
func TestAcceptance_M12_PublicProfile(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
		h := newRegistryHarness(t, db, c)
		own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Vetrina SRL", MarinaName: "Marina Vetrina", MarinaSlug: "vetrina",
		})
		if err != nil {
			t.Fatal(err)
		}
		M := A + "/marinas/" + own.MarinaID
		o := h.cookieClient()
		o.login(t, "owner@porto.test")
		harbour := contracttest.InsertHarbour(t, db, own.MarinaID, store.UTC(c.Now()), 20)
		o.expect(t, 201, "POST", M+"/berth-categories", map[string]any{"code": "L12", "name": map[string]string{"it": "Fino a 12 m", "en": "Up to 12 m"}, "applies_to": []string{"berth"}})

		// Price list with publish_matrix: transits 3 € per metre and night on three bands.
		classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
		transitClass := ""
		for _, rc := range jsonList(classes, "items") {
			if str(rc.(map[string]any), "code") == "transit" {
				transitClass = str(rc.(map[string]any), "id")
			}
		}
		trn := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night",
			"revenue_class_id": transitClass, "price_includes_vat": true,
		})
		doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
			"code": "PUB2027", "name": map[string]string{"it": "Pubblico"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"transit"},
		})
		saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), map[string]any{
			"code": "PUB2027", "name": map[string]string{"it": "Pubblico"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
			"contract_types": []string{"transit"}, "periods": []any{},
			"settings": map[string]any{
				"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
				"primary_items": map[string]string{"transit": str(trn, "id")}, "publish_matrix": true,
			},
			"bands": []map[string]any{{"key": "b10", "code": "L10", "max_cm": 1000}, {"key": "b12", "code": "L12", "max_cm": 1200, "category": "L12"}, {"key": "b14", "code": "L14"}},
			"rules": []map[string]any{{"key": "r1", "stage": "base", "name": "Transito", "item_id": str(trn, "id"), "basis": "per_meter", "unit": "night", "unit_cents": 300}},
		}, "If-Match", etagOf(doc["version"]))
		listID := str(doc, "id")
		o.expect(t, 200, "POST", M+"/price-lists/"+listID+"/activate", map[string]any{}, "If-Match", etagOf(saved["document"].(map[string]any)["version"]))

		// Personal data that must never reach a public response.
		cust := o.expect(t, 201, "POST", M+"/customers", map[string]any{
			"kind": "person", "first_name": "Ermenegildo", "last_name": "Zanzibar",
			"emails": []map[string]string{{"label": "main", "value": "ermenegildo.zanzibar@example.test"}},
			"phones": []map[string]string{{"label": "mobile", "value": "+39 333 9876543"}},
		})
		custID := str(cust["customer"].(map[string]any), "id")
		ves := o.expect(t, 201, "POST", M+"/vessels", map[string]any{"name": "Quintessenza", "loa_cm": 1100, "beam_cm": 380, "hull_type": "monohull_sail"})
		tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": str(ves, "id"), "customer_id": custID, "arrival": "2027-06-01", "departure": "2027-06-04",
			"placements": []any{berthPlacement(harbour.Berths[0], "2027-06-01", "2027-06-03")},
		}, "Idempotency-Key", "acc-m12-transit-0001")
		o.expect(t, 200, "POST", M+"/transits/"+str(tr, "id")+"/confirm", map[string]any{"reference": "POS 1"}, "If-Match", etagOf(tr["version"]))

		// The owner fills the profile, uploads a photo and publishes.
		anon := h.cookieClient()
		if r := anon.do(t, "GET", "/p/vetrina", nil); r.status != 404 {
			t.Fatalf("unpublished page %d", r.status)
		}
		var pb bytes.Buffer
		_ = png.Encode(&pb, image.NewNRGBA(image.Rect(0, 0, 40, 30)))
		req, _ := http.NewRequest("POST", h.srv.URL+M+"/profile/photos", bytes.NewReader(pb.Bytes()))
		req.Header.Set("Content-Type", "image/png")
		req.Header.Set("X-CSRF-Token", o.csrf())
		res, err := o.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		pbody, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 201 {
			t.Fatalf("upload %d %s", res.StatusCode, pbody)
		}
		var photo map[string]any
		_ = json.Unmarshal(pbody, &photo)
		prof := o.expect(t, 200, "PUT", M+"/profile", map[string]any{
			"entrance_lat_e7": 437101234, "entrance_lon_e7": -104012345, "phone": "+39 050 1112222", "email": "ufficio@vetrina.example",
			"website": "https://vetrina.example", "vhf_channels": []int{9}, "description": map[string]string{"it": "La marina vetrina"},
			"office_hours": []map[string]any{{"weekday": 1, "from": "08:00", "to": "18:00"}},
			"services":     map[string]any{"water": true, "wifi": true}, "photos": []map[string]any{{"file_id": str(photo, "file_id"), "alt": map[string]string{"it": "Ingresso"}}},
			"show_map": true, "show_prices": true, "show_occupancy": true, "occupancy_categories": []string{"L12"},
		}, "If-Match", `"0"`)
		if prof["prices_available"] != true {
			t.Fatalf("prices not available: %v", prof)
		}
		o.expect(t, 200, "POST", M+"/profile/publish", nil, "If-Match", etagOf(prof["version"]))

		// Anonymous visitor.
		bodies := map[string][]byte{}
		get := func(path string, want int) response {
			t.Helper()
			r := anon.do(t, "GET", path, nil)
			if r.status != want {
				t.Fatalf("GET %s: %d %s", path, r.status, r.body)
			}
			if v := r.header.Values("Set-Cookie"); len(v) > 0 {
				t.Fatalf("GET %s sets cookies: %v", path, v)
			}
			bodies[path] = r.body
			return r
		}
		page := get("/p/vetrina", 200)
		html := string(page.body)
		if page.header.Get("Cache-Control") != "public, max-age=300" || !strings.Contains(html, `<link rel="canonical" href="http://localhost:8080/p/vetrina">`) ||
			!strings.Contains(html, `hreflang="it"`) || strings.Count(html, "application/ld+json") != 1 {
			t.Fatalf("page %v\n%s", page.header, html)
		}
		m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(html)
		var ld map[string]any
		if len(m) < 2 || json.Unmarshal([]byte(m[1]), &ld) != nil || ld["name"] != "Marina Vetrina" {
			t.Fatalf("json-ld %v", m)
		}
		if g := ld["geo"].(map[string]any); g["latitude"] != "43.7101234" || g["longitude"] != "-10.4012345" {
			t.Fatalf("geo %v", g)
		}
		pp := get("/api/public/v1/vetrina/profile?lang=en", 200).json(t)
		sec := pp["sections"].(map[string]any)
		if sec["map"] != true || sec["prices"] != true || sec["occupancy"] != true || pp["lang"] != "it" {
			t.Fatalf("public profile %v", pp)
		}
		get("/api/public/v1/vetrina/profile/map", 200)
		occ := get("/api/public/v1/vetrina/occupancy", 200).json(t)
		cats := jsonList(occ, "categories")
		if len(cats) != 1 || cats[0].(map[string]any)["total_berths"] != float64(20) || cats[0].(map[string]any)["free_berths"] != float64(19) || occ["night"] != "2027-06-01" {
			t.Fatalf("occupancy %v", occ)
		}
		prices := get("/api/public/v1/vetrina/profile/prices", 200).json(t)
		matrix := o.expect(t, 200, "GET", M+"/price-lists/"+listID+"/matrix?contract_type=transit&rows=bands&channel=online&quoted_on=2027-06-01", nil)
		pc, mc := jsonList(prices, "cells"), jsonList(matrix, "cells")
		if len(pc) == 0 || len(pc) != len(mc) {
			t.Fatalf("cells %d vs %d", len(pc), len(mc))
		}
		for i := range pc {
			a, b := pc[i].(map[string]any), mc[i].(map[string]any)
			if a["row"] != b["row"] || a["col"] != b["col"] || a["total_cents"] != b["total_cents"] {
				t.Fatalf("cell %d: public %v matrix %v", i, a, b)
			}
		}
		if strings.Contains(string(bodies["/api/public/v1/vetrina/profile/prices"]), listID) {
			t.Fatal("public prices carry the list id")
		}
		get("/api/public/v1/vetrina/profile/photos/"+str(photo, "file_id"), 200)
		get("/api/public/v1/vetrina", 200)
		bodies["/p/vetrina?lang=en"] = get("/p/vetrina?lang=en", 200).body

		// INV-103: no personal data, no berth codes.
		codes := []string{}
		for i := range 20 {
			codes = append(codes, `"A`+strconv.Itoa(i+1)+`"`, `>A`+strconv.Itoa(i+1)+`<`)
		}
		for path, b := range bodies {
			s := string(b)
			for _, leak := range append([]string{"Ermenegildo", "Zanzibar", "ermenegildo.zanzibar@example.test", "9876543", "Quintessenza", custID, harbour.Berths[0]}, codes...) {
				if strings.Contains(s, leak) {
					t.Fatalf("%s leaks %q", path, leak)
				}
			}
		}

		// Unpublish: every public operation is 404 again.
		cur := o.expect(t, 200, "GET", M+"/profile", nil)
		o.expect(t, 200, "POST", M+"/profile/unpublish", nil, "If-Match", etagOf(cur["version"]))
		for _, p := range []string{"/p/vetrina", "/api/public/v1/vetrina/profile", "/api/public/v1/vetrina/profile/map", "/api/public/v1/vetrina/profile/prices", "/api/public/v1/vetrina/profile/photos/" + str(photo, "file_id")} {
			get(p, 404)
		}
	})
}
