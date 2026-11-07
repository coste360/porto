//go:build integration

package acceptance

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M5_LedgerReconciles (m5-plan §11.4, PAYMENTS): a quarter of desk activity through the API only —
// annual contracts with the M4 policy (instalments issued by the job and charged by the billing hooks), transits
// confirmed with a desk payment or paid later, manual payments with partial payments and overpayments, credit
// applications, match reversals and refunds, settlements through the M4 routes — then, for every customer, the
// statement closing balance = the API balance = Σ residuals of open charges − credit = Σ charge totals − Σ payment
// amounts + Σ refunds (adjustment credits are their negative charges), Σ matches = Σ paid amounts, every instalment
// paid ⇔ its charge paid, and the ledger invariant queries find nothing.
//
// The year-long generator of CHARGES-EXPORT (m5_sim_test.go) is not in the tree yet: this scenario has its own
// deterministic driver (PCG 51, 5) and switches to the shared generator when it lands.
func TestAcceptance_M5_LedgerReconciles(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 12)
		o, M, b := w.owner, w.M, w.harbour.Berths
		rng := rand.New(rand.NewPCG(51, 5)) //nolint:gosec // deterministic scenario
		w.contractSettings(t, map[string]any{"signature_driver": "paper", "require_paper_scan": false, "default_policy": m4Policy})

		var customers []string
		for i := range 6 {
			customers = append(customers, w.customer(t, fmt.Sprintf("Cliente%d", i)))
		}
		vessels := map[string]string{}
		for i, c := range customers {
			vessels[c] = w.vessel(t, fmt.Sprintf("Barca%d", i), 1000+i*20, 350)
		}
		// Four annual contracts on the first berths.
		for i := range 4 {
			c := customers[i]
			o.expect(t, 201, "POST", M+"/contracts", map[string]any{
				"term": "annual", "customer_id": c, "vessel_id": vessels[c], "starts_on": "2027-06-01",
				"placements": []any{berthPlacement(b[i], "2027-06-01", "2028-05-31")}, "activate": true,
			})
		}
		payOpen := func(customer string, amount int64, method string) map[string]any {
			return o.expect(t, 201, "POST", M+"/payments", map[string]any{
				"customer_id": customer, "kind": "payment", "method": method, "amount_cents": amount, "auto_match": "oldest_due_first",
			})
		}
		methods := []string{"bank_transfer", "card_pos", "cheque"}
		day := time.Date(2027, 6, 1, 8, 0, 0, 0, time.UTC)
		for d := range 92 {
			w.setClock(t, day.AddDate(0, 0, d))
			w.h.runJobs("installments.issue")
			today := day.AddDate(0, 0, d).Format("2006-01-02")
			// A transit every other day on the last berths.
			if d%2 == 0 {
				c := customers[4+rng.IntN(2)]
				berth := b[4+rng.IntN(8)]
				dep := day.AddDate(0, 0, d+2).Format("2006-01-02")
				last := day.AddDate(0, 0, d+1).Format("2006-01-02")
				r := o.do(t, "POST", M+"/transits", map[string]any{
					"vessel_id": vessels[c], "customer_id": c, "arrival": today, "departure": dep,
					"placements": []any{berthPlacement(berth, today, last)},
				})
				if r.status == 201 {
					tr := r.json(t)
					conf := map[string]any{}
					if rng.IntN(3) > 0 {
						conf["payment"] = map[string]any{"method": methods[rng.IntN(2)]}
					}
					o.expect(t, 200, "POST", M+"/transits/"+str(tr, "id")+"/confirm", conf, "If-Match", etagOf(tr["version"]))
				}
			}
			// Payments: partial, full, overpayments.
			if rng.IntN(3) == 0 {
				c := customers[rng.IntN(len(customers))]
				payOpen(c, int64(1_000+rng.IntN(150_000)), methods[rng.IntN(len(methods))])
			}
			// Credit applications.
			if d%10 == 5 {
				c := customers[rng.IntN(len(customers))]
				if r := o.do(t, "POST", M+"/customers/"+c+"/credits/apply", map[string]any{}); r.status != 200 && r.problemType() != "credit_insufficient" {
					t.Fatalf("apply credit: %d %s", r.status, r.body)
				}
			}
			// Reversals and refunds.
			if d%13 == 7 {
				page := o.expect(t, 200, "GET", M+"/payments?method=bank_transfer", nil)
				for _, it := range jsonList(page, "items") {
					p := o.expect(t, 200, "GET", M+"/payments/"+str(it.(map[string]any), "id"), nil)
					ms := jsonList(p, "matches")
					if len(ms) == 0 {
						continue
					}
					m := ms[0].(map[string]any)
					if m["reversed"] == true || str(m, "reverses_match_id") != "" {
						continue
					}
					amt, _ := num(m, "amount_cents")
					refund := min(amt, 2_000)
					o.expect(t, 201, "POST", M+"/payments/"+str(p, "id")+"/refunds", map[string]any{
						"amount_cents": refund, "reason": "storno", "unmatch": []any{map[string]any{"match_id": str(m, "id"), "amount_cents": refund}},
					})
					break
				}
			}
			// Settlements through the M4 route.
			if d%15 == 3 {
				page := o.expect(t, 200, "GET", M+"/installments?status=issued", nil)
				for _, it := range jsonList(page, "items") {
					im := it.(map[string]any)
					o.expect(t, 200, "POST", M+"/installments/"+str(im, "id")+"/settle", map[string]any{"method": "bank_transfer"}, "If-Match", etagOf(im["version"]))
					break
				}
			}
		}

		ctx := context.Background()
		scope, err := store.ResolveMarinaScope(ctx, db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		vs, err := store.CheckPaymentsInvariants(ctx, db.DB, scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("ledger invariants %+v %v", vs, err)
		}
		var payments int
		for _, c := range customers {
			st := o.expect(t, 200, "GET", M+"/customers/"+c+"/statement?from=2000-01-01", nil)
			closing, _ := num(st, "closing_cents")
			dash := o.expect(t, 200, "GET", M+"/customers/"+c+"/dashboard", nil)
			balance, _ := num(dash, "balance_cents")
			if closing != balance {
				t.Fatalf("customer %s: statement %d ≠ balance %d", c, closing, balance)
			}
			// Σ residuals of open charges − credit, from the scadenzario and the credits.
			recs := o.expect(t, 200, "GET", M+"/receivables?customer_id="+c+"&status=open&limit=200", nil)
			var residuals int64
			for _, it := range jsonList(recs, "items") {
				v, _ := num(it.(map[string]any), "residual_cents")
				residuals += v
			}
			cr := o.expect(t, 200, "GET", M+"/customers/"+c+"/credits", nil)
			credit, _ := num(cr, "credit_cents")
			if balance != residuals-credit {
				t.Fatalf("customer %s: balance %d ≠ residuals %d − credit %d", c, balance, residuals, credit)
			}
			// Σ charge totals − Σ payment amounts + Σ refunds (adjustment credits excluded: their charge counts).
			var totals, paid, refunded, deposits int64
			if err := db.NewSelect().TableExpr("charges").ColumnExpr("COALESCE(SUM(total_cents), 0)").Where("marina_id = ?", w.marina).
				Where("customer_id = ?", c).Where("payment_status <> 'cancelled'").Scan(ctx, &totals); err != nil {
				t.Fatal(err)
			}
			if err := db.NewSelect().TableExpr("payments").ColumnExpr("COALESCE(SUM(amount_cents), 0)").Where("marina_id = ?", w.marina).
				Where("customer_id = ?", c).Where("kind IN ('payment','credit')").Where("source <> 'adjustment'").Scan(ctx, &paid); err != nil {
				t.Fatal(err)
			}
			if err := db.NewSelect().TableExpr("refunds AS r").Join("JOIN payments AS p ON p.id = r.payment_id").
				ColumnExpr("COALESCE(SUM(r.amount_cents), 0)").Where("r.marina_id = ?", w.marina).Where("p.customer_id = ?", c).
				Where("p.kind IN ('payment','credit')").Where("r.status <> 'failed'").Scan(ctx, &refunded); err != nil {
				t.Fatal(err)
			}
			if err := db.NewSelect().TableExpr("payment_matches AS m").Join("JOIN payments AS p ON p.id = m.payment_id").
				ColumnExpr("COALESCE(SUM(m.amount_cents), 0)").Where("m.marina_id = ?", w.marina).Where("p.customer_id = ?", c).
				Where("p.kind = 'deposit'").Scan(ctx, &deposits); err != nil {
				t.Fatal(err)
			}
			if got := totals - paid + refunded - deposits; got != balance {
				t.Fatalf("customer %s: Σ totals %d − Σ payments %d + Σ refunds %d = %d ≠ balance %d", c, totals, paid, refunded, got, balance)
			}
			n := o.expect(t, 200, "GET", M+"/payments?customer_id="+c+"&limit=200", nil)
			payments += len(jsonList(n, "items"))
		}
		t.Logf("payments %d", payments)
		if payments < 20 {
			t.Fatalf("too few payments in the scenario: %d", payments)
		}
		var matches, paidSum int64
		if err := db.NewSelect().TableExpr("payment_matches").ColumnExpr("COALESCE(SUM(amount_cents), 0)").Where("marina_id = ?", w.marina).Scan(ctx, &matches); err != nil {
			t.Fatal(err)
		}
		if err := db.NewSelect().TableExpr("charges").ColumnExpr("COALESCE(SUM(paid_amount_cents), 0)").Where("marina_id = ?", w.marina).Scan(ctx, &paidSum); err != nil {
			t.Fatal(err)
		}
		if matches != paidSum {
			t.Fatalf("Σ matches %d ≠ Σ paid %d", matches, paidSum)
		}
		// Every instalment paid has a settlement through a payment (no manual settlement in M5).
		var manual int
		if err := db.NewSelect().TableExpr("installments").ColumnExpr("COUNT(*)").Where("marina_id = ?", w.marina).
			Where("status = 'paid'").Where("settlement_source <> 'payment'").Scan(ctx, &manual); err != nil {
			t.Fatal(err)
		}
		if manual != 0 {
			t.Fatalf("%d instalments paid outside payments", manual)
		}
	})
}
