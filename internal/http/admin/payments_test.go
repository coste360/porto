//go:build integration

package admin_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// TestPaymentHandlers_Scenario: the ledger operations of m5-plan §7.3.2 through the real router (statuses,
// problem types, permissions, another marina).
func TestPaymentHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	// An annual contract: its deposit instalment is issued at activation and charged by the billing hook.
	ct := ds.activeAnnual(t, ds.berths[0], "2027-06-02", "2028-06-01")
	var recs api.ReceivablePage
	c.expect(t, 200, "", "GET", M+"/receivables?contract_id="+ct.Id+"&status=open", nil, &recs)
	if len(recs.Items) != 1 || recs.Items[0].Kind != "charge" || recs.Totals.TotalCents != recs.Items[0].ResidualCents {
		t.Fatalf("receivables %+v", recs)
	}
	ch := recs.Items[0]
	ds.dock.expect(t, 403, "forbidden", "GET", M+"/receivables", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", M+"/receivables", nil, nil)
	c.expect(t, 400, "bad_request", "GET", M+"/receivables?due_from=ieri", nil, nil)
	r := c.expect(t, 200, "", "GET", M+"/receivables?format=csv", nil, nil)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") || !strings.Contains(string(r.body), *ch.Number) {
		t.Fatalf("scadenzario csv %q", r.body)
	}
	r = c.expect(t, 200, "", "GET", M+"/receivables?format=xlsx", nil, nil)
	if !bytes.HasPrefix(r.body, []byte("PK")) {
		t.Fatal("scadenzario xlsx")
	}

	// Record: validation, permissions, match above the charge, then a payment with a remainder as credit.
	body := map[string]any{"customer_id": ds.customer, "kind": "payment", "method": "bank_transfer", "amount_cents": ch.ResidualCents + 1000}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/payments", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/payments", body, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/payments", map[string]any{"kind": "payment", "method": "bank_transfer", "amount_cents": 0}, nil)
	over := map[string]any{
		"customer_id": ds.customer, "kind": "payment", "method": "bank_transfer", "amount_cents": ch.ResidualCents + 1000,
		"matches": []any{map[string]any{"charge_id": ch.Id, "amount_cents": ch.ResidualCents + 1}},
	}
	c.expect(t, 409, "match_exceeds", "POST", M+"/payments", over, nil)
	body["auto_match"] = "oldest_due_first"
	body["reference"] = "CRO 778"
	var pay api.Payment
	r = c.expect(t, 201, "", "POST", M+"/payments", body, &pay, "Idempotency-Key", "pay-handler-0001")
	if r.header.Get("ETag") == "" || pay.UnallocatedCents != 1000 || pay.Matches == nil || len(*pay.Matches) != 1 || pay.HasReference == nil || !*pay.HasReference {
		t.Fatalf("payment %+v", pay)
	}
	if strings.Contains(string(r.body), "CRO 778") {
		t.Fatal("the staff reference leaked")
	}
	P := M + "/payments/" + pay.Id
	c.expect(t, 200, "", "GET", P, nil, &pay)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/payments/"+pay.Id, nil, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", P, nil, nil)
	var page api.PaymentPage
	c.expect(t, 200, "", "GET", M+"/payments?unallocated=true&method=bank_transfer", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Id != pay.Id {
		t.Fatalf("payments %+v", page.Items)
	}

	// Refunds, reversals and deposits.
	c.expect(t, 409, "refund_exceeds", "POST", P+"/refunds", map[string]any{"amount_cents": 2000, "reason": "errore"}, nil)
	c.expect(t, 400, "validation_failed", "POST", P+"/refunds", map[string]any{"amount_cents": 100, "reason": ""}, nil)
	var ref api.Refund
	c.expect(t, 201, "", "POST", P+"/refunds", map[string]any{"amount_cents": 400, "reason": "eccedenza"}, &ref)
	if ref.Status != "succeeded" || ref.Kind != "refund" {
		t.Fatalf("refund %+v", ref)
	}
	var refunds api.RefundPage
	c.expect(t, 200, "", "GET", M+"/refunds?payment_id="+pay.Id, nil, &refunds)
	if len(refunds.Items) != 1 {
		t.Fatalf("refunds %+v", refunds)
	}
	c.expect(t, 409, "not_deposit", "POST", P+"/retain", map[string]any{"amount_cents": 100, "reason": "chiavi"}, nil)
	m := (*pay.Matches)[0]
	ds.dock.expect(t, 403, "forbidden", "POST", P+"/matches/"+m.Id+"/reverse", nil, nil)
	c.expect(t, 200, "", "POST", P+"/matches/"+m.Id+"/reverse", nil, &pay)
	c.expect(t, 409, "already_reversed", "POST", P+"/matches/"+m.Id+"/reverse", nil, nil)
	c.expect(t, 200, "", "POST", P+"/matches", map[string]any{"matches": []any{map[string]any{"charge_id": ch.Id, "amount_cents": 100}}}, &pay)
	c.expect(t, 409, "match_exceeds", "POST", P+"/matches", map[string]any{"matches": []any{map[string]any{"charge_id": ch.Id, "amount_cents": ch.ResidualCents}}}, nil)

	// Credits.
	C := M + "/customers/" + ds.customer
	var credit api.CustomerCredit
	c.expect(t, 200, "", "GET", C+"/credits", nil, &credit)
	if credit.CreditCents != pay.UnallocatedCents || len(credit.Payments) != 1 {
		t.Fatalf("credit %+v", credit)
	}
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/customers/"+ds.customer+"/credits", nil, nil)
	var granted api.Payment
	c.expect(t, 201, "", "POST", C+"/credits", map[string]any{"amount_cents": 500, "reason": "disagio", "source": "goodwill"}, &granted)
	if granted.Kind != "credit" || granted.Method != "credit_note" {
		t.Fatalf("granted %+v", granted)
	}
	c.expect(t, 422, "release_not_resold", "POST", C+"/credits", map[string]any{"amount_cents": 500, "reason": "x", "source": "release", "release_id": ct.Id}, nil)
	var applied api.ApplyCreditResult
	c.expect(t, 200, "", "POST", C+"/credits/apply", map[string]any{}, &applied)
	if applied.MatchedCents <= 0 {
		t.Fatalf("applied %+v", applied)
	}
	var credits api.CreditPage
	c.expect(t, 200, "", "GET", M+"/credits", nil, &credits)
	ds.dock.expect(t, 403, "forbidden", "GET", M+"/credits", nil, nil)

	// Statement, dashboard, ageing.
	var st api.Statement
	c.expect(t, 200, "", "GET", C+"/statement?from=2026-01-01", nil, &st)
	if len(st.Rows) == 0 {
		t.Fatalf("statement %+v", st)
	}
	r = c.expect(t, 200, "", "GET", C+"/statement?format=csv", nil, nil)
	if !strings.Contains(string(r.body), "saldo_finale") {
		t.Fatalf("statement csv %q", r.body)
	}
	if r := c.do(t, "GET", C+"/statement?format=pdf", nil); r.status == 200 && !bytes.HasPrefix(r.body, []byte("%PDF")) {
		t.Fatal("statement pdf")
	}
	c.expect(t, 400, "bad_request", "GET", C+"/statement?to=domani", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/customers/"+ds.customer+"/statement", nil, nil)
	var dash api.CustomerDashboard
	c.expect(t, 200, "", "GET", C+"/dashboard", nil, &dash)
	if dash.BalanceCents == nil || dash.Arrears == nil || len(dash.Contracts) != 1 || len(dash.Vessels) != 0 && dash.Vessels[0].Id == "" {
		t.Fatalf("dashboard %+v", dash)
	}
	var dockDash api.CustomerDashboard
	ds.dock.expect(t, 200, "", "GET", C+"/dashboard", nil, &dockDash)
	if dockDash.BalanceCents != nil || dockDash.RecentPayments != nil {
		t.Fatal("billing sections without charges.manage")
	}
	var ageing api.AgeingReport
	c.expect(t, 200, "", "GET", M+"/receivables/ageing", nil, &ageing)
	r = c.expect(t, 200, "", "GET", M+"/receivables/ageing?format=csv", nil, nil)
	if !strings.Contains(string(r.body), "Totale") {
		t.Fatal("ageing csv")
	}

	// Payment lens.
	var maps struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	c.expect(t, 200, "", "GET", M+"/maps", nil, &maps)
	if len(maps.Items) == 0 {
		t.Fatal("no map")
	}
	var lens api.MapPaymentStatus
	ds.dock.expect(t, 200, "", "GET", M+"/maps/"+maps.Items[0].ID+"/payment-status?date=2027-06-10", nil, &lens)
	if len(lens.Resources) != 1 || lens.Resources[0].ResidualCents != nil {
		t.Fatalf("lens %+v", lens)
	}
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/maps/"+maps.Items[0].ID+"/payment-status", nil, nil)

	// Mandates and worklist.
	c.expect(t, 404, "not_found", "POST", M+"/mandates/"+ct.Id+"/settle", map[string]any{"through": "2027-06-30", "mode": "credit"}, nil)
	var wl api.WorkItemPage
	c.expect(t, 200, "", "GET", M+"/worklist", nil, &wl)
	c.expect(t, 404, "not_found", "PATCH", M+"/worklist/late_payment/"+ct.Id, map[string]any{"status": "done"}, nil)
	c.expect(t, 404, "not_found", "PATCH", M+"/worklist/bogus/"+ct.Id, map[string]any{"status": "done"}, nil)
	ds.other.expect(t, 404, "not_found", "GET", M+"/worklist", nil, nil)
}

// TestTransitHandlers_ConfirmWithPayment: the M3 confirm route with a desk payment (m5-plan §8.2).
func TestTransitHandlers_ConfirmWithPayment(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	var tr api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-01", "departure": "2027-07-04",
		"placements": []any{seg(ds.berths[1], "2027-07-01", "2027-07-03")},
	}, &tr)
	conf := map[string]any{"payment": map[string]any{"method": "card_pos"}}
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/transits/"+tr.Id+"/confirm", conf, nil, "If-Match", etag(tr.Version))
	var done api.Contract
	c.expect(t, 200, "", "POST", M+"/transits/"+tr.Id+"/confirm", conf, &done, "If-Match", etag(tr.Version))
	if done.Status != "confirmed" {
		t.Fatalf("transit %s", done.Status)
	}
	var page api.PaymentPage
	c.expect(t, 200, "", "GET", M+"/payments?method=card_pos", nil, &page)
	if len(page.Items) != 1 || page.Items[0].UnallocatedCents != 0 || page.Items[0].AllocatedCents == 0 {
		t.Fatalf("desk payment %+v", page.Items)
	}
}
