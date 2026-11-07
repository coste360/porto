//go:build integration

package acceptance

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// saleItems creates two gross-priced catalogue items (water 10 %, fuel 22 %) through the API.
func (w *deskWorld) saleItems(t *testing.T) (water, fuel string) {
	t.Helper()
	o, M := w.owner, w.M
	classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
	class := str(jsonList(classes, "items")[0].(map[string]any), "id")
	mk := func(code string, rate int) string {
		it := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": code, "name": map[string]string{"it": code}, "item_type": "product", "unit": "piece",
			"revenue_class_id": class, "price_includes_vat": true, "vat_rate_bp": rate,
		})
		return str(it, "id")
	}
	return mk("ACQUA", 1000), mk("GASOLIO", 2200)
}

// TestAcceptance_M5_CashDay (m5-plan §11.4): two registers with sessions, 120 counter sales with and without a
// commercial document, a cash refund, movements, the close with counted amounts ⇒ Z report totals = summary;
// corrispettivi of the day per rate = Σ fiscal documents; receipts 80/58 mm HTML and A4 PDF.
func TestAcceptance_M5_CashDay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		water, fuel := w.saleItems(t)
		cust := w.customer(t, "Banco")
		regs := o.expect(t, 200, "GET", M+"/cash-registers", nil)
		if items := jsonList(regs, "items"); len(items) != 1 || str(items[0].(map[string]any), "code") != "main" {
			t.Fatalf("default register %v", regs)
		}
		dock := o.expect(t, 201, "POST", M+"/cash-registers", map[string]any{"code": "banchina", "name": "Banchina"})
		main := o.expect(t, 201, "POST", M+"/cash-sessions", map[string]any{"float_cents": 100_00})
		dockSess := o.expect(t, 201, "POST", M+"/cash-sessions", map[string]any{"register_id": str(dock, "id"), "float_cents": 50_00})
		if r := o.do(t, "POST", M+"/cash-sessions", map[string]any{"float_cents": 0}); r.status != 409 || r.problemType() != "cash_session_open" {
			t.Fatalf("second open %d %s", r.status, r.body)
		}
		var cashPayment string
		var docTotal int64
		for i := range 120 {
			reg := ""
			if i%2 == 1 {
				reg = str(dock, "id")
			}
			method := "cash"
			if i%4 == 3 {
				method = "card_pos"
			}
			body := map[string]any{
				"lines": []map[string]any{
					{"item_id": water, "qty_milli": 1000, "unit_cents": 2_00},
					{"item_id": fuel, "qty_milli": int64(1+i%3) * 1000, "unit_cents": 1_83},
				},
				"payment": map[string]any{"method": method, "register_id": reg}, "fiscal_document": i%2 == 0, "receipt": "print",
			}
			if i%2 == 1 {
				body["customer_id"] = cust
			}
			res := o.expect(t, 201, "POST", M+"/counter-sales", body, "Idempotency-Key", fmt.Sprintf("sale-%04d-acc", i))
			if fd, ok := res["fiscal_document"].(map[string]any); ok {
				docTotal += int64(fd["total_cents"].(float64))
				if i == 0 {
					cashPayment = str(res["payment"].(map[string]any), "id")
				}
			}
		}
		// A cash refund of the first sale (reverse the match, then refund) lands in the main session.
		pay := o.expect(t, 200, "GET", M+"/payments/"+cashPayment, nil)
		match := jsonList(pay, "matches")[0].(map[string]any)
		amount := int64(pay["amount_cents"].(float64))
		o.expect(t, 201, "POST", M+"/payments/"+cashPayment+"/refunds", map[string]any{
			"amount_cents": amount, "method": "cash", "reason": "reso",
			"unmatch": []map[string]any{{"match_id": str(match, "id"), "amount_cents": amount}},
		}, "Idempotency-Key", "refund-first-sale")
		o.expect(t, 201, "POST", M+"/cash-sessions/"+str(main, "id")+"/movements", map[string]any{"kind": "out", "amount_cents": 30_00, "reason": "versamento"})
		for _, s := range []map[string]any{main, dockSess} {
			cur := o.expect(t, 200, "GET", M+"/cash-sessions/"+str(s, "id"), nil)
			sum := cur["summary"].(map[string]any)
			var counted []map[string]any
			for _, e := range jsonList(sum, "expected") {
				em := e.(map[string]any)
				counted = append(counted, map[string]any{"method": em["method"], "amount_cents": em["amount_cents"]})
			}
			closed := o.expect(t, 200, "POST", M+"/cash-sessions/"+str(s, "id")+"/close", map[string]any{"counted": counted}, "If-Match", etagOf(cur["version"]))
			cs := closed["summary"].(map[string]any)
			if cs["difference_cents"] != float64(0) || str(closed, "status") != "closed" {
				t.Fatalf("close %v", cs)
			}
			z := o.do(t, "GET", M+"/cash-sessions/"+str(s, "id")+"/z-report?format=html", nil)
			if z.status != 200 || bytes.Contains(z.body, []byte("<script")) || !bytes.Contains(z.body, []byte(euroOf(cs["expected"]))) {
				t.Fatalf("z report %d", z.status)
			}
			if pdf := o.do(t, "GET", M+"/cash-sessions/"+str(s, "id")+"/z-report?format=pdf", nil); pdf.status != 200 || !bytes.HasPrefix(pdf.body, []byte("%PDF")) {
				t.Fatalf("z pdf %d", pdf.status)
			}
		}
		day := str(main, "business_date")
		tot := o.expect(t, 200, "GET", M+"/fiscal/daily-totals?from="+day+"&to="+day, nil)
		var rows int64
		for _, r := range jsonList(tot, "rows") {
			rows += int64(r.(map[string]any)["gross_cents"].(float64))
		}
		if int64(tot["total_cents"].(float64)) != docTotal-amount || rows != docTotal-amount {
			t.Fatalf("corrispettivi total %v rows %d, documents %d − refund %d", tot["total_cents"], rows, docTotal, amount)
		}
		if csv := o.do(t, "GET", M+"/fiscal/daily-totals?from="+day+"&to="+day+"&format=csv", nil); csv.status != 200 || !bytes.Contains(csv.body, []byte("Totale")) {
			t.Fatalf("csv %d", csv.status)
		}
		for _, f := range []string{"80", "58"} {
			r := o.do(t, "GET", M+"/payments/"+cashPayment+"/receipt?format="+f, nil)
			if r.status != 200 || !bytes.Contains(r.body, []byte("Non valido ai fini fiscali")) || bytes.Contains(r.body, []byte("<script")) {
				t.Fatalf("receipt %s: %d", f, r.status)
			}
		}
		if r := o.do(t, "GET", M+"/payments/"+cashPayment+"/receipt?format=a4", nil); r.status != 200 || !bytes.HasPrefix(r.body, []byte("%PDF")) {
			t.Fatalf("receipt a4 %d %s", r.status, r.body)
		}
		// The dock role collects cash only.
		dockClient := w.clients["dock"]
		if r := dockClient.do(t, "POST", M+"/counter-sales", map[string]any{
			"customer_id": cust, "lines": []map[string]any{{"item_id": water, "qty_milli": 1000}}, "payment": map[string]any{"method": "card_pos"},
			"fiscal_document": false, "receipt": "none",
		}); r.status != 403 {
			t.Fatalf("dock card sale %d", r.status)
		}
		scope, _ := store.SystemMarinaScope(t.Context(), db.DB, w.marina)
		if vs, err := store.CheckBillingInvariants(t.Context(), db.DB, scope); err != nil || len(vs) != 0 {
			t.Fatalf("invariants %+v %v", vs, err)
		}
	})
}

// euroOf returns the Italian amount of the cash line of a summary list ("123,45").
func euroOf(list any) string {
	for _, e := range list.([]any) {
		m := e.(map[string]any)
		if m["method"] == "cash" {
			c := int64(m["amount_cents"].(float64))
			return fmt.Sprintf("%d,%02d", c/100, c%100)
		}
	}
	return ""
}
