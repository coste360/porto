//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIdsM10 (INV-73, M10) extends TestAcceptance_CrossMarinaResourceIds to the work
// order routes: marina B calls every route under its own {marinaId} with the ids of real rows of marina A (work
// order, task, task file, time entry, external cost). Each call is 404 not_found.
func TestAcceptance_CrossMarinaResourceIdsM10(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		classes := jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items")
		item := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "SRV-ALAG", "name": map[string]string{"it": "Alaggio"}, "item_type": "service", "unit": "m", "vat_rate_bp": 2200,
			"revenue_class_id": str(classes[0].(map[string]any), "id"), "price_includes_vat": false,
		})
		wt := o.expect(t, 201, "POST", M+"/yard/work-types", map[string]any{"code": "HULL", "name": map[string]string{"it": "Carena"}})
		order := o.expect(t, 201, "POST", M+"/yard/work-orders", map[string]any{
			"customer_id": w.customer(t, "Bianchi"), "vessel_id": w.vessel(t, "Grecale", 1200, 400), "work_type_id": str(wt, "id"),
			"manual_lines": []map[string]any{{"item_id": str(item, "id"), "qty_milli": 12000, "unit_cents": 1800}},
		})
		W := M + "/yard/work-orders/" + str(order, "id")
		task := o.expect(t, 201, "POST", W+"/tasks", map[string]any{"title": "Ispezione", "estimated_hours_milli": 1000})
		var yardUser string
		for _, m := range jsonList(o.expect(t, 200, "GET", M+"/members", nil), "items") {
			if mm := m.(map[string]any); str(mm, "email") == "yard@porto.test" {
				yardUser = str(mm, "user_id")
			}
		}
		emp := o.expect(t, 201, "POST", M+"/yard/employees", map[string]any{
			"code": "Y1", "first_name": "Marco", "last_name": "Esposito", "user_id": yardUser, "hourly_cost_cents": 2200,
		})
		entry := o.expect(t, 201, "POST", M+"/timesheets", map[string]any{
			"client_ref": "iso-1", "employee_id": str(emp, "id"), "work_order_id": str(order, "id"), "hours_milli": 1000,
		})
		x := o.expect(t, 201, "POST", W+"/external-costs", map[string]any{"supplier_name": "Fornitore", "cost_cents": 1000, "billable": false})
		r := o.raw(t, http.MethodPost, M+"/yard/tasks/"+str(task, "id")+"/photos", "application/pdf", testPDFBytes)
		if r.status != 200 {
			t.Fatalf("upload %d %s", r.status, r.body)
		}
		file := r.json(t)

		ids := map[string]string{
			"{workOrderId}": str(order, "id"), "{taskId}": str(task, "id"), "{timesheetId}": str(entry, "id"),
			"{externalCostId}": str(x, "id"), "{fileId}": str(file, "file_id"),
		}
		bodies := map[string]any{
			// {taskId} also fills the maintenance task routes (M9), which validate their body first.
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/schedule": map[string]any{
				"from_night": "2027-07-01", "to_night": "2027-07-02",
			},
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/complete":       map[string]any{"outcome": "ok"},
			"POST /api/admin/v1/marinas/{marinaId}/maintenance/tasks/{taskId}/cancel":         map[string]any{"reason": "x"},
			"POST /api/admin/v1/marinas/{marinaId}/yard/work-orders/{workOrderId}/transition": map[string]any{"action": "start"},
			"POST /api/admin/v1/marinas/{marinaId}/yard/work-orders/{workOrderId}/tasks":      map[string]any{"title": "x"},
			"POST /api/admin/v1/marinas/{marinaId}/yard/work-orders/{workOrderId}/billings":   map[string]any{"kind": "final"},
			"POST /api/admin/v1/marinas/{marinaId}/yard/work-orders/{workOrderId}/external-costs": map[string]any{
				"supplier_name": "x", "cost_cents": 100,
			},
			"POST /api/admin/v1/marinas/{marinaId}/yard/tasks/{taskId}/progress":          map[string]any{"client_ref": "x1"},
			"PUT /api/admin/v1/marinas/{marinaId}/yard/tasks/{taskId}/dependencies":       map[string]any{"depends_on": []string{}},
			"POST /api/admin/v1/marinas/{marinaId}/timesheets/{timesheetId}/reject":       map[string]any{"reason": "x"},
			"PATCH /api/admin/v1/marinas/{marinaId}/timesheets/{timesheetId}":             map[string]any{"hours_milli": 1000},
			"POST /api/admin/v1/marinas/{marinaId}/timesheets/{timesheetId}/stop":         map[string]any{"client_ref": "x2"},
			"PATCH /api/admin/v1/marinas/{marinaId}/yard/external-costs/{externalCostId}": map[string]any{"cost_cents": 100},
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

var testPDFBytes = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")

// TestAcceptance_CrossOrgUserLinksM10 (INV-73, M10): the user references of the yard (the responsible of a work
// order, the phone identity of an employee) accept only users with a grant on the marina. A user of another
// organization is 422, the same answer as an unknown id, so the check does not reveal users of other tenants.
func TestAcceptance_CrossOrgUserLinksM10(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		b, err := w.h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		})
		if err != nil {
			t.Fatal(err)
		}
		var yardUser string
		for _, m := range jsonList(o.expect(t, 200, "GET", M+"/members", nil), "items") {
			if mm := m.(map[string]any); str(mm, "email") == "yard@porto.test" {
				yardUser = str(mm, "user_id")
			}
		}
		customer, vessel := w.customer(t, "Bianchi"), w.vessel(t, "Grecale", 1200, 400)
		order := func(responsible string) response {
			return o.do(t, "POST", M+"/yard/work-orders", map[string]any{
				"customer_id": customer, "vessel_id": vessel, "responsible_user_id": responsible,
			})
		}
		for _, id := range []string{b.UserID, "0190d6a1-0000-7000-8000-000000000000"} {
			if r := order(id); !fieldError(r, "responsible_user_id") {
				t.Errorf("responsible %s: %d %s, want 400 responsible_user_id", id, r.status, r.body)
			}
		}
		created := order(yardUser)
		if created.status != 201 {
			t.Fatalf("responsible member of the marina: %d %s", created.status, created.body)
		}
		co := created.json(t)
		W := M + "/yard/work-orders/" + str(co, "id")
		if r := o.do(t, "PATCH", W, map[string]any{"responsible_user_id": b.UserID}, "If-Match", fmt.Sprintf(`"%v"`, co["version"])); !fieldError(r, "responsible_user_id") {
			t.Errorf("patch responsible of another org: %d %s, want 400", r.status, r.body)
		}

		r := o.do(t, "POST", M+"/yard/employees", map[string]any{
			"code": "X1", "first_name": "Luca", "last_name": "Neri", "user_id": b.UserID, "hourly_cost_cents": 2000,
		})
		if !fieldError(r, "user_id") {
			t.Errorf("employee linked to a user of another org: %d %s, want 400 user_id", r.status, r.body)
		}
		emp := o.expect(t, 201, "POST", M+"/yard/employees", map[string]any{
			"code": "X3", "first_name": "Luca", "last_name": "Neri", "hourly_cost_cents": 2000,
		})
		if r := o.do(t, "PATCH", M+"/yard/employees/"+str(emp, "id"), map[string]any{"user_id": b.UserID}, "If-Match", `"1"`); !fieldError(r, "user_id") {
			t.Errorf("patch employee to a user of another org: %d %s, want 400", r.status, r.body)
		}
	})
}

// fieldError reports whether r is a 400 validation_failed naming field.
func fieldError(r response, field string) bool {
	var p struct {
		Errors []struct {
			Field string `json:"field"`
		} `json:"errors"`
	}
	if r.status != http.StatusBadRequest || r.problemType() != "validation_failed" || json.Unmarshal(r.body, &p) != nil {
		return false
	}
	for _, e := range p.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}
