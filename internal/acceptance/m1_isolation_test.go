//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIds (INV-73) complements TestAcceptance_CrossMarinaIsolation: marina B calls
// every route under its own {marinaId}, with the ids of real resources of marina A (map, berth group,
// background file, customer, vessel, document, deadline; M2 revenue class, item, berth category, price list).
// Each call is 404 not_found: the marina in the path is
// authorised, but no query finds a row of another marina.
func TestAcceptance_CrossMarinaResourceIds(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarness(t, db)
		ctx := context.Background()
		a, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "a@porto.test", Name: "Owner A", Password: password, Organization: "Org A", MarinaName: "Marina A", MarinaSlug: "marina-a",
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		})
		if err != nil {
			t.Fatal(err)
		}
		ac := h.cookieClient()
		ac.login(t, "a@porto.test")
		MA := A + "/marinas/" + a.MarinaID

		// Resources of marina A.
		m := ac.expect(t, 201, "POST", MA+"/maps", map[string]any{"kind": "water", "name": map[string]string{"it": "Porto"}, "width_cm": 20000, "height_cm": 20000})
		img, err := os.ReadFile("testdata/harbor/orthophoto.png")
		if err != nil {
			t.Fatal(err)
		}
		up := ac.harborUpload(t, "POST", MA+"/maps/"+str(m, "id")+"/background", "image/png", img)
		if up.status != 201 {
			t.Fatalf("upload: %d %s", up.status, up.body)
		}
		g := ac.expect(t, 201, "POST", MA+"/berth-groups", map[string]any{"code": "PONT-A", "name": map[string]string{"it": "Pontile A"}})
		c, _ := ac.expect(t, 201, "POST", MA+"/customers", map[string]any{"kind": "person", "last_name": "Rossi"})["customer"].(map[string]any)
		v := ac.expect(t, 201, "POST", MA+"/vessels", map[string]any{
			"name": "Libeccio", "loa_cm": 1190, "beam_cm": 395, "hull_type": "monohull_sail",
			"parties": []map[string]any{{"customer_id": str(c, "id"), "role": "owner", "share_millis": 1000}},
		})
		d := ac.expect(t, 201, "POST", MA+"/documents", map[string]any{"owner_type": "vessel", "vessel_id": str(v, "id"), "type": "insurance_policy", "expires_on": "2027-01-15"})
		dl := ac.expect(t, 200, "GET", MA+"/deadlines?vessel_id="+str(v, "id"), nil)
		items, _ := dl["items"].([]any)
		if len(items) == 0 {
			t.Fatal("no deadline for the document")
		}
		// M2 pricing resources of marina A.
		rc := ac.expect(t, 201, "POST", MA+"/revenue-classes", map[string]any{"code": "iso_a", "name": map[string]string{"it": "Classe A"}})
		it := ac.expect(t, 201, "POST", MA+"/items", map[string]any{
			"code": "ISO-A", "name": map[string]string{"it": "Articolo A"}, "item_type": "berth_fee", "unit": "night",
			"revenue_class_id": str(rc, "id"),
		})
		bcat := ac.expect(t, 201, "POST", MA+"/berth-categories", map[string]any{
			"code": "ISO-A", "name": map[string]string{"it": "Categoria A"}, "applies_to": []string{"berth"},
		})
		pl := ac.expect(t, 201, "POST", MA+"/price-lists", map[string]any{
			"code": "ISO-A", "name": map[string]string{"it": "Listino A"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
			"contract_types": []string{"transit"},
		})
		// Well-formed bodies for the routes that validate the body before loading the resource, so the 404 is
		// what is checked. The draft document of marina A is a valid PriceListSave.
		draft := ac.expect(t, 200, "GET", MA+"/price-lists/"+str(pl, "id"), nil)
		save := map[string]any{}
		for _, k := range []string{"code", "name", "valid_from", "valid_to", "contract_types", "settings", "periods", "bands", "rules"} {
			save[k] = draft[k]
		}
		bodies := map[string]any{
			"PUT /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}":           save,
			"POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/validate": save,
			"POST /api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/renewal": map[string]any{
				"dry_run": true, "code": "ISO-B", "name": map[string]string{"it": "Rinnovo"}, "shift_years": 1, "amount_rounding": "none",
			},
		}
		ids := map[string]string{
			"{mapId}": str(m, "id"), "{fileId}": str(up.json(t), "file_id"), "{groupId}": str(g, "id"),
			"{customerId}": str(c, "id"), "{vesselId}": str(v, "id"), "{documentId}": str(d, "id"),
			"{deadlineId}":     str(items[0].(map[string]any), "id"),
			"{revenueClassId}": str(rc, "id"), "{itemId}": str(it, "id"), "{categoryId}": str(bcat, "id"),
			"{priceListId}": str(pl, "id"),
		}

		bc := h.cookieClient()
		bc.login(t, "b@porto.test")
		router, ok := h.app.Router.(chi.Routes)
		if !ok {
			t.Fatal("router is not a chi router")
		}
		placeholder := regexp.MustCompile(`\{[A-Za-z]+\}`)
		checked := map[string]bool{}
		err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !strings.Contains(route, "{marinaId}") {
				return nil
			}
			path, uses := strings.ReplaceAll(route, "{marinaId}", b.MarinaID), false
			for p, id := range ids {
				if strings.Contains(path, p) {
					path, uses = strings.ReplaceAll(path, p, id), true
					checked[p] = true
				}
			}
			if !uses || placeholder.MatchString(path) {
				return nil // no resource id of marina A in this route
			}
			var body any
			if method != "GET" && method != "DELETE" {
				body = map[string]any{}
				if v, ok := bodies[method+" "+route]; ok {
					body = v
				}
			}
			r := bc.do(t, method, path, body, "If-Match", `"1"`)
			if r.status != http.StatusNotFound || r.problemType() != "not_found" {
				t.Errorf("%s %s with a resource of marina A: status %d %s, want 404 not_found", method, route, r.status, r.body)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for p := range ids {
			if !checked[p] {
				t.Errorf("no route checked with %s", p)
			}
		}
	})
}
