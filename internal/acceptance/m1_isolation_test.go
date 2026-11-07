//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIds (INV-73) complements TestAcceptance_CrossMarinaIsolation: marina B calls
// every route under its own {marinaId}, with the ids of real resources of marina A (map, berth group,
// background file, customer, vessel, document, deadline; M2 revenue class, item, berth category, price list; M3
// contract and transit, quote, allocation, block, release, inquiry, presence, move undo entry, dynamic area,
// closure). Each call is 404 not_found: the marina in the path is authorised, but no query finds a row of another
// marina.
func TestAcceptance_CrossMarinaResourceIds(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		clk := clock.NewFake(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC))
		h := newRegistryHarness(t, db, clk)
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
			"POST /api/admin/v1/marinas/{marinaId}/contracts/{contractId}/terminate":   map[string]any{"effective_on": "2027-09-01", "reason": "vendita"},
			"POST /api/admin/v1/marinas/{marinaId}/dynamic-areas/{areaId}/compact":     map[string]any{"from": "2027-06-01", "to": "2027-06-02"},
			"POST /api/admin/v1/marinas/{marinaId}/dynamic-areas/{areaId}/layout": map[string]any{
				"date": "2027-06-01", "base": []any{}, "placements": []any{},
			},
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

		// M3 occupancy resources of marina A (2027 price list, stays in June 2027).
		w := &deskWorld{h: h, clock: clk, marina: a.MarinaID, M: MA, owner: ac, clients: map[string]*client{}}
		w.prices(t, false)
		hb := contracttest.InsertHarbour(t, db, a.MarinaID, store.UTC(clk.Now()), 4)
		vessel := func(name string) string {
			return str(ac.expect(t, 201, "POST", MA+"/vessels", map[string]any{"name": name, "loa_cm": 1000, "beam_cm": 350, "hull_type": "monohull_sail"}), "id")
		}
		ct := ac.expect(t, 201, "POST", MA+"/contracts", map[string]any{
			"term": "annual", "customer_id": str(c, "id"), "vessel_id": str(v, "id"), "starts_on": "2027-01-01", "activate": true,
			"placements": []any{berthPlacement(hb.Berths[0], "2027-01-01", "2027-12-31")},
		})
		rel := ac.expect(t, 201, "POST", MA+"/releases", map[string]any{
			"allocation_id": str(firstAllocation(t, ct), "id"), "from_night": "2027-06-10", "to_night": "2027-06-12", "source": "office",
		})
		tr := ac.expect(t, 201, "POST", MA+"/transits", map[string]any{
			"vessel_id": vessel("Maestrale"), "customer_id": str(c, "id"), "arrival": "2027-06-20", "departure": "2027-06-22",
			"placements": []any{berthPlacement(hb.Berths[1], "2027-06-20", "2027-06-21")},
		})
		ta := firstAllocation(t, tr)
		mv := ac.expect(t, 200, "POST", MA+"/occupancies/move", map[string]any{
			"allocation_id": str(ta, "id"), "version": ta["version"], "target": map[string]any{"resource_type": "berth", "resource_id": hb.Berths[2]},
		})
		q := ac.expect(t, 201, "POST", MA+"/quotes", map[string]any{
			"kind": "transit", "customer_id": str(c, "id"), "vessel_id": vessel("Grecale"), "starts_on": "2027-07-01", "ends_on": "2027-07-03",
			"options": []any{map[string]any{"hold": true, "placements": []any{berthPlacement(hb.Berths[3], "2027-07-01", "2027-07-02")}}},
		})
		blk := ac.expect(t, 201, "POST", MA+"/blocks", map[string]any{
			"target": map[string]any{"resource_type": "berth", "resource_id": hb.Berths[3]}, "from": "2027-08-01", "to": "2027-08-02", "reason": "lavori",
		})
		inq := ac.expect(t, 201, "POST", MA+"/inquiries", map[string]any{
			"purpose": "transit", "source": "phone", "arrival": "2027-08-01", "departure": "2027-08-03", "loa_cm": 1000, "beam_cm": 350,
		})
		pr := ac.expect(t, 201, "POST", MA+"/presences", map[string]any{"vessel_id": vessel("Scirocco"), "event": "arrived"})
		cl := ac.expect(t, 201, "POST", MA+"/closures", map[string]any{"scope_type": "marina", "from_date": "2027-09-01", "to_date": "2027-09-02", "reason": "regata"})
		ids["{contractId}"], ids["{quoteId}"], ids["{releaseId}"] = str(ct, "id"), str(q, "id"), str(rel, "id")
		ids["{inquiryId}"], ids["{undoId}"], ids["{areaId}"], ids["{closureId}"] = str(inq, "id"), str(mv, "undo_id"), hb.Area, str(cl, "id")
		ids["{presenceId}"] = str(pr["presence"].(map[string]any), "id")
		// {allocationId} names both allocations and blocks: a block of marina A is an allocation of marina A.
		ids["{allocationId}"] = str(blk, "id")

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
