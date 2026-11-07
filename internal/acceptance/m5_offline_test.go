//go:build integration

package acceptance

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M5_OfflineManualOnly (m5-plan §11.4, §0.13): a marina without any provider settings row runs the
// whole path with the manual driver alone — charge → payment request (bank details, no online button) → reminders
// → manual payment → refund → "da fatturare" export → invoice reconciliation. No provider is configured, so no
// outbound call can happen; the payment page never offers an online payment.
func TestAcceptance_M5_OfflineManualOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		w.contractSettings(t, map[string]any{"bank_details": map[string]any{"iban": "IT60X0542811101000000123456", "holder": "Marina Banchina Srl"}})
		water, _ := w.saleItems(t)
		bianchi := w.mailCustomer(t, "Bianchi", "bianchi@example.test", "it")
		ch := o.expect(t, 201, "POST", M+"/charges", map[string]any{
			"origin_type": "manual", "customer_id": bianchi, "due_on": "2027-06-03",
			"lines": []map[string]any{{"item_id": water, "qty_milli": 10_000, "unit_cents": 12_00}},
		})
		total := int64(ch["total_cents"].(float64))
		pr := o.expect(t, 201, "POST", M+"/payment-requests", map[string]any{"customer_id": bianchi, "charge_ids": []string{str(ch, "id")}, "send": true})
		url := str(pr, "url")
		tok := url[strings.LastIndex(url, "/")+1:]
		page := w.h.cookieClient().expect(t, 200, "GET", "/api/public/v1/banchina/pay/"+tok, nil)
		if page["online"] != false || page["bank"] == nil || int64(page["amount_cents"].(float64)) != total {
			t.Fatalf("pay page %v", page)
		}
		if r := w.h.cookieClient().do(t, "POST", "/api/public/v1/banchina/pay/"+tok+"/checkout", map[string]any{"method": "card"}); r.status != 422 || r.problemType() != "provider_disabled" {
			t.Fatalf("checkout offline %d %s", r.status, r.body)
		}
		// Reminders: due date passed → the due level is sent.
		w.setClock(t, time.Date(2027, 6, 4, 8, 0, 0, 0, time.UTC))
		run := o.expect(t, 200, "POST", M+"/reminder-runs", nil)
		if c, _ := run["counts"].(map[string]any); c == nil || c["sent"] == nil {
			t.Fatalf("reminder run %v", run)
		}
		if again := o.expect(t, 200, "POST", M+"/reminder-runs", nil); str(again, "id") != str(run, "id") {
			t.Fatal("second run of the day")
		}
		// Manual payment (bank transfer), overpaid by 5 €, then the excess refunded.
		pay := o.expect(t, 201, "POST", M+"/payments", map[string]any{
			"customer_id": bianchi, "kind": "payment", "method": "bank_transfer", "amount_cents": total + 5_00,
			"matches": []map[string]any{{"charge_id": str(ch, "id"), "amount_cents": total}},
		}, "Idempotency-Key", "offline-payment-0001")
		o.expect(t, 201, "POST", M+"/payments/"+str(pay, "id")+"/refunds", map[string]any{"amount_cents": 5_00, "method": "bank_transfer", "reason": "eccedenza"}, "Idempotency-Key", "offline-refund-0001")
		if got := o.expect(t, 200, "GET", M+"/charges/"+str(ch, "id"), nil); str(got, "payment_status") != "paid" {
			t.Fatalf("charge %v", got)
		}
		if st := w.h.cookieClient().expect(t, 200, "GET", "/api/public/v1/banchina/pay/"+tok+"/status", nil); str(st, "status") != "open" && str(st, "status") != "paid" {
			t.Fatalf("status %v", st)
		}
		// Export and reconciliation.
		exp := o.expect(t, 201, "POST", M+"/charge-exports", map[string]any{"period_from": "2027-06-01", "period_to": "2027-06-30"}, "Idempotency-Key", "offline-export-0001")
		if exp["charge_count"] == float64(0) {
			t.Fatalf("export %v", exp)
		}
		o.expect(t, 200, "POST", M+"/invoice-reconciliations", map[string]any{
			"dry_run": false, "rows": []map[string]any{{"charge_id": str(ch, "id"), "invoice_number": "FT-1", "invoice_date": "2027-06-30"}},
		})
		if got := o.expect(t, 200, "GET", M+"/charges/"+str(ch, "id"), nil); str(got, "invoicing_status") != "invoiced" {
			t.Fatalf("invoiced %v", str(got, "invoicing_status"))
		}
		n, _ := db.NewSelect().TableExpr("payment_provider_settings").Count(t.Context())
		m, _ := db.NewSelect().TableExpr("payment_intents").Count(t.Context())
		if n != 0 || m != 0 {
			t.Fatalf("provider rows %d intents %d", n, m)
		}
		scope, _ := store.SystemMarinaScope(t.Context(), db.DB, w.marina)
		if vs, err := store.CheckBillingInvariants(t.Context(), db.DB, scope); err != nil || len(vs) != 0 {
			t.Fatalf("invariants %+v %v", vs, err)
		}
	})
}
