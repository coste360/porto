//go:build integration

package acceptance

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M10 acceptance criterion 2 (spec 32, m10-plan §9.4, §16.4): the final account reconciles hours, materials and
// external costs. Through the API only: the §4.4 quote accepted from the panel creates the order and the advance
// (623.15); hours of three employees (one from the phone, replayed offline twice), approved; materials issued and
// returned; an external cost; the final account is the §9.4 table exactly and the equations of §9.3 hold on the API
// numbers; advance + final = 2,319.10; the charges are exported; a late entry is billed as a conguaglio and the
// exported charge does not change (INV-51, INV-93, INV-94).

func TestAcceptance_M10_FinalAccount(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		yard := w.clients["yard"]
		pkg, items := w.yardCatalogue(t)
		classes := jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items")
		ext := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "EXT-SRV", "name": map[string]string{"it": "Lavorazioni esterne"}, "item_type": "service", "unit": "lump",
			"vat_rate_bp": 2200, "revenue_class_id": str(classes[0].(map[string]any), "id"), "price_includes_vat": false,
		})
		prop := o.expect(t, 201, "POST", M+"/yard/work-types", map[string]any{"code": "PROPULSION", "name": map[string]string{"it": "Propulsione"}})
		vessel := str(o.expect(t, 201, "POST", M+"/vessels", map[string]any{
			"name": "Maestrale", "loa_cm": 1200, "beam_cm": 400, "draft_cm": 150, "hull_type": "monohull_sail",
		}), "id")
		cust := w.mailCustomer(t, "Bianchi", "bianchi@example.test", "it")

		// Quote with the package only, accepted from the panel: the order and the advance in the same transaction.
		q := o.expect(t, 201, "POST", M+"/yard/quotes", map[string]any{
			"customer_id": cust, "vessel_id": vessel, "selections": []map[string]any{{"package_id": str(pkg, "id")}},
		})
		o.expect(t, 200, "POST", M+"/yard/quotes/"+str(q, "id")+"/accept", map[string]any{}, "If-Match", etagOf(q["version"]))
		var orderID string
		for _, it := range jsonList(o.expect(t, 200, "GET", M+"/yard/work-orders", nil), "items") {
			if m := it.(map[string]any); str(m, "quote_id") == str(q, "id") {
				orderID = str(m, "id")
			}
		}
		if orderID == "" {
			t.Fatal("no order from the accepted quote")
		}
		W := M + "/yard/work-orders/" + orderID
		bills := o.expect(t, 200, "GET", W+"/billings", nil)
		if g, _ := num(bills, "charged_gross_cents"); g != 62315 {
			t.Fatalf("advance %v", bills)
		}
		tasks := map[string]map[string]any{}
		for _, it := range jsonList(o.expect(t, 200, "GET", W+"/tasks", nil), "items") {
			tasks[str(it.(map[string]any), "title")] = it.(map[string]any)
		}
		hand, paint := tasks["Carteggiatura"], tasks["Antivegetativa"]
		if hand == nil || paint == nil {
			t.Fatalf("tasks %v", tasks)
		}

		// Employees: Marco is the yard user (phone), Luca and Giulia are recorded by the office.
		quals := map[string]string{}
		for _, it := range jsonList(o.expect(t, 200, "GET", M+"/yard/qualifications", nil), "items") {
			quals[str(it.(map[string]any), "code")] = str(it.(map[string]any), "id")
		}
		var yardUser string
		for _, m := range jsonList(o.expect(t, 200, "GET", M+"/members", nil), "items") {
			if mm := m.(map[string]any); str(mm, "email") == "yard@porto.test" {
				yardUser = str(mm, "user_id")
			}
		}
		emp := func(code, first string, cost int, user, qual string) string {
			body := map[string]any{"code": code, "first_name": first, "last_name": "Test", "hourly_cost_cents": cost, "qualification_ids": []string{qual}}
			if user != "" {
				body["user_id"] = user
			}
			return str(o.expect(t, 201, "POST", M+"/yard/employees", body), "id")
		}
		luca := emp("LUC", "Luca", 2200, "", quals["hand"])
		marco := emp("MAR", "Marco", 2400, yardUser, quals["hand"])
		giulia := emp("GIU", "Giulia", 2600, "", quals["painter"])
		o.expect(t, 200, "PUT", M+"/yard/planner/assignments", map[string]any{"changes": []map[string]any{{
			"task_id": str(hand, "id"), "employee_id": marco, "work_date": "2027-06-01", "hours_milli": 4000, "task_version": hand["version"],
		}}})

		w.setClock(t, time.Date(2027, 6, 1, 17, 0, 0, 0, time.UTC))
		entry := func(employee, task, ref string, hours int) map[string]any {
			return o.expect(t, 201, "POST", M+"/timesheets", map[string]any{"client_ref": ref, "employee_id": employee, "task_id": task, "hours_milli": hours})
		}
		e1 := entry(luca, str(hand, "id"), "fa-luca", 5000)
		e3 := entry(giulia, str(paint, "id"), "fa-giulia", 5500)
		// Marco records 4 h on the phone offline: the queued call is replayed twice, one entry.
		raw, _ := json.Marshal(map[string]any{"client_ref": "fa-marco", "task_id": str(hand, "id"), "hours_milli": 4000, "work_date": "2027-06-01"})
		rs := hammer(t, 2, func() (response, error) {
			return yard.replayUntilDone(http.MethodPost, M+"/timesheets", raw, "application/json", "Idempotency-Key", "fa-marco-key-0001",
				offlineHeader, "2027-06-01T16:00:00Z")
		})
		assertSameBodies(t, rs, http.StatusCreated)
		e2 := rs[0].json(t)
		if n := len(jsonList(o.expect(t, 200, "GET", M+"/timesheets?work_order_id="+orderID, nil), "items")); n != 3 {
			t.Fatalf("%d entries", n)
		}
		var approve []map[string]any
		for _, e := range []map[string]any{e1, e2, e3} {
			approve = append(approve, map[string]any{"id": str(e, "id"), "version": e["version"]})
		}
		o.expect(t, 200, "POST", M+"/timesheets/approve", map[string]any{"entries": approve})

		// Materials and the external cost.
		o.expect(t, 201, "POST", M+"/stock-movements", map[string]any{"work_order_id": orderID, "item_id": items["MAT-AVEG"], "kind": "issue", "qty_milli": 6000})
		o.expect(t, 201, "POST", M+"/stock-movements", map[string]any{"work_order_id": orderID, "item_id": items["MAT-AVEG"], "kind": "return", "qty_milli": 1000})
		o.expect(t, 201, "POST", W+"/external-costs", map[string]any{
			"supplier_name": "Eliche Srl", "description": "Lucidatura elica", "work_type_id": str(prop, "id"), "cost_cents": 15000,
			"item_id": str(ext, "id"),
		})

		// The final account = §9.4, and the §9.3 equations on the API numbers.
		acc := o.expect(t, 200, "GET", W+"/account", nil)
		want := map[string][4]int64{ // qty, revenue, cost, vat
			items["SRV-ALAG"]: {12000, 21600, 0, 4752}, items["SRV-LAV"]: {56100, 22440, 5610, 4937},
			items["LAB-HAND"]: {9000, 34200, 20600, 7524}, items["LAB-PAINT"]: {5500, 24750, 14300, 5445},
			items["MAT-AVEG"]: {5000, 47500, 29000, 10450}, items["SRV-VARO"]: {12000, 21600, 0, 4752}, str(ext, "id"): {1000, 18000, 15000, 3960},
		}
		var sumRev, sumCost, sumVAT, sumDelta int64
		lines := jsonList(acc, "lines")
		if len(lines) != len(want) {
			t.Fatalf("%d lines: %v", len(lines), lines)
		}
		for _, it := range lines {
			l := it.(map[string]any)
			qv, _ := num(l, "qty_milli")
			rv, _ := num(l, "revenue_cents")
			cv, _ := num(l, "cost_cents")
			vv, _ := num(l, "vat_cents")
			dv, _ := num(l, "delta_net_cents")
			if got := [4]int64{qv, rv, cv, vv}; got != want[str(l, "item_id")] {
				t.Errorf("line %s = %v, want %v", str(l, "key"), got, want[str(l, "item_id")])
			}
			sumRev, sumCost, sumVAT, sumDelta = sumRev+rv, sumCost+cv, sumVAT+vv, sumDelta+dv
		}
		tot := acc["totals"].(map[string]any)
		for k, v := range map[string]int64{
			"revenue_cents": 190090, "cost_cents": 84510, "margin_cents": 105580, "margin_bp": 5554, "vat_cents": 41820, "gross_cents": 231910,
			"hours_labour_milli": 14500, "cost_labour_cents": 34900, "cost_material_cents": 29000, "cost_external_cents": 15000,
		} {
			if got, _ := num(tot, k); got != v {
				t.Errorf("%s = %d, want %d", k, got, v)
			}
		}
		rl, _ := num(tot, "revenue_labour_cents")
		rm, _ := num(tot, "revenue_material_cents")
		rsv, _ := num(tot, "revenue_service_cents")
		rx, _ := num(tot, "revenue_external_cents")
		if sumRev != 190090 || rl+rm+rsv+rx != sumRev || sumCost != 84510 || sumVAT != 41820 {
			t.Fatalf("equations 1-2: Σ lines %d / sources %d", sumRev, rl+rm+rsv+rx)
		}
		var wtRev, wtMargin int64
		for _, it := range jsonList(acc, "by_work_type") {
			r, _ := num(it.(map[string]any), "revenue_cents")
			m, _ := num(it.(map[string]any), "margin_cents")
			wtRev, wtMargin = wtRev+r, wtMargin+m
		}
		quoteNet, _ := num(acc, "quote_net_cents")
		delta, _ := num(acc, "delta_net_cents")
		if wtRev != 190090 || wtMargin != 105580 || quoteNet != 170260 || delta != 19830 || sumDelta != delta {
			t.Fatalf("by work type %d/%d, quote %d, delta %d (Σ %d)", wtRev, wtMargin, quoteNet, delta, sumDelta)
		}

		// Complete and bill the final: advance + final = target gross.
		for _, tk := range []map[string]any{hand, paint} {
			o.expect(t, 200, "POST", M+"/yard/tasks/"+str(tk, "id")+"/progress", map[string]any{"client_ref": "fa-done-" + str(tk, "id")[:8], "done": true})
		}
		ord := o.expect(t, 200, "GET", W, nil)
		if bp, _ := num(ord, "completion_bp"); bp != 10000 || str(ord, "status") != "in_progress" {
			t.Fatalf("order %v", ord)
		}
		o.expect(t, 200, "POST", W+"/transition", map[string]any{"action": "complete"}, "If-Match", etagOf(ord["version"]))
		prev := o.expect(t, 200, "POST", W+"/billings?dry_run=true", map[string]any{"kind": "final"})
		if g, _ := num(prev, "gross_cents"); g != 169595 {
			t.Fatalf("preview %v", prev)
		}
		fin := o.expect(t, 201, "POST", W+"/billings", map[string]any{"kind": "final"}, "Idempotency-Key", "fa-final-0001")
		finalCharge := str(fin, "charge_id")
		if g, _ := num(fin, "gross_cents"); g != 169595 || finalCharge == "" {
			t.Fatalf("final %v", fin)
		}
		bills = o.expect(t, 200, "GET", W+"/billings", nil)
		if g, _ := num(bills, "charged_gross_cents"); g != 231910 {
			t.Fatalf("Σ charges %v", bills)
		}
		o.expect(t, 200, "POST", W+"/transition", map[string]any{"action": "deliver"}, "If-Match", etagOf(o.expect(t, 200, "GET", W, nil)["version"]))

		// Export, then a late entry billed as a conguaglio; the exported final charge is unchanged.
		o.expect(t, 201, "POST", M+"/charge-exports", map[string]any{"period_from": "2027-06-01", "period_to": "2027-06-30"}, "Idempotency-Key", "fa-export-0001")
		before := o.expect(t, 200, "GET", M+"/charges/"+finalCharge, nil)
		if str(before, "invoicing_status") != "exported" {
			t.Fatalf("final charge not exported: %v", before["invoicing_status"])
		}
		late := entry(luca, str(hand, "id"), "fa-late", 500)
		if late["late"] != true {
			t.Fatalf("late entry %v", late)
		}
		o.expect(t, 200, "POST", M+"/timesheets/approve", map[string]any{"entries": []map[string]any{{"id": str(late, "id"), "version": late["version"]}}})
		adj := o.expect(t, 201, "POST", W+"/billings", map[string]any{"kind": "adjustment"})
		if g, _ := num(adj, "gross_cents"); g != 2318 || str(adj, "corrects_charge_id") != finalCharge {
			t.Fatalf("conguaglio %v", adj)
		}
		after := o.expect(t, 200, "GET", M+"/charges/"+finalCharge, nil)
		bt, _ := num(before, "total_cents")
		at, _ := num(after, "total_cents")
		if bt != at || bt != 169595 {
			t.Fatalf("exported charge changed: %d → %d", bt, at)
		}
		vs, err := store.CheckYardInvariants(t.Context(), db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
		}
	})
}
