//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIdsM9 (INV-73, M9) extends TestAcceptance_CrossMarinaResourceIds to the M9
// routes: marina B calls every route under its own {marinaId} with the ids of real rows of marina A (storage
// scenario and movement, utility meter, reading and consumption, equipment, maintenance plan and task). Each call is
// 404 not_found.
func TestAcceptance_CrossMarinaResourceIdsM9(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w, meter := newUtilityWorld(t, db)
		o, M := w.owner, w.M
		berth := w.harbour.Berths[0]

		// Storage: a contract in a shed, a scenario that moves it, applied (one movement).
		w.storagePrices(t)
		area := w.storageShed(t)
		cust := w.customer(t, "Rossi")
		c := o.expect(t, 201, "POST", M+"/storage/contracts", map[string]any{
			"term": "period", "customer_id": cust, "vessel_id": w.vessel(t, "Libeccio", 800, 280), "starts_on": "2027-06-05",
			"ends_on": "2027-09-01", "area_id": area, "support": "cradle", "activate": true,
		})
		alloc := str(c["placement"].(map[string]any), "allocation_id")
		sc := o.expect(t, 201, "POST", M+"/storage/scenarios", map[string]any{
			"area_id": area, "name": "Estate", "from_on": "2027-06-05", "to_on": "2027-08-31",
		})
		scn := sc["scenario"].(map[string]any)
		S := M + "/storage/scenarios/" + str(scn, "id")
		sd := o.expect(t, 200, "PUT", S+"/items", map[string]any{"items": []any{
			map[string]any{"allocation_id": alloc, "x_cm": 3000, "y_cm": 3000, "rotation_dd": 0},
		}}, "If-Match", etagOf(scn["version"]))
		ap := o.expect(t, 200, "POST", S+"/apply", nil, "If-Match", etagOf(sd["scenario"].(map[string]any)["version"]))
		movements := jsonList(ap, "movements")
		if len(movements) == 0 {
			t.Fatalf("apply %v", ap)
		}

		// Utilities: two readings (an unassigned consumption between them).
		w.setClock(t, time.Date(2027, 6, 8, 10, 0, 0, 0, time.UTC))
		w.reading(t, o, meter, "100", "2027-06-02T07:30:00Z")
		rd := w.reading(t, o, meter, "160", "2027-06-04T07:30:00Z")
		cons := jsonList(o.expect(t, 200, "GET", M+"/utility-consumptions?meter_id="+meter, nil), "items")
		if len(cons) == 0 {
			t.Fatal("no consumption")
		}

		// Maintenance: equipment, plan, task.
		eq := o.expect(t, 201, "POST", M+"/equipment", map[string]any{"code": "TL-1", "name": "Travel lift", "kind": "travel_lift"})
		plan := o.expect(t, 201, "POST", M+"/maintenance/plans", map[string]any{
			"name": "Ispezione", "intervention": "underwater_inspection", "lead_days": 10,
			"targets":     []map[string]any{{"element_type": "berth", "element_id": berth}},
			"periodicity": map[string]any{"kind": "months", "every": 6, "starts_on": "2027-12-10"},
		})
		task := o.expect(t, 201, "POST", M+"/maintenance/tasks", map[string]any{
			"title": "Verifica", "element": map[string]any{"element_type": "berth", "element_id": berth},
		})

		ids := map[string]string{
			"{scenarioId}": str(scn, "id"), "{movementId}": str(movements[0].(map[string]any), "id"), "{meterId}": meter,
			"{readingId}": str(rd, "id"), "{consumptionId}": str(cons[0].(map[string]any), "id"), "{equipmentId}": str(eq, "id"),
			"{planId}": str(plan, "id"), "{taskId}": str(task, "id"),
		}
		// Well-formed bodies for the routes that validate the body before loading the resource.
		bodies := map[string]any{
			"PUT /api/admin/v1/marinas/{marinaId}/storage/scenarios/{scenarioId}/items":  map[string]any{"items": []any{}},
			"POST /api/admin/v1/marinas/{marinaId}/storage/scenarios/{scenarioId}/items": map[string]any{"allocation_id": alloc},
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/schedule": map[string]any{
				"from_night": "2027-07-01", "to_night": "2027-07-02",
			},
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/complete":          map[string]any{"outcome": "ok"},
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/cancel":            map[string]any{"reason": "x"},
			"POST /api/admin/v1/marinas/{marinaId}/meter-readings/{readingId}/void":              map[string]any{"reason": "x"},
			"POST /api/admin/v1/marinas/{marinaId}/utility-consumptions/{consumptionId}/exclude": map[string]any{"reason": "x"},
			"POST /api/admin/v1/marinas/{marinaId}/utility-consumptions/{consumptionId}/assign": map[string]any{
				"contract_id": str(c["contract"].(map[string]any), "id"),
			},
		}

		b, err := w.h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		})
		if err != nil {
			t.Fatal(err)
		}
		bc := w.h.cookieClient()
		bc.login(t, "b@porto.test")
		router, ok := w.h.app.Router.(chi.Routes)
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
				return nil
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
