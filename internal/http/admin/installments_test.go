//go:build integration

package admin_test

import (
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// activeAnnual creates an active annual contract on a berth and returns it.
func (ds *deskSetup) activeAnnual(t *testing.T, berth, starts, last string) api.Contract {
	t.Helper()
	var ct api.Contract
	ds.owner.expect(t, 201, "", "POST", ds.M+"/contracts", map[string]any{
		"term": "annual", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": starts,
		"placements": []any{seg(berth, starts, last)}, "activate": true,
	}, &ct)
	return ct
}

func TestInstallmentHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	ct := ds.activeAnnual(t, ds.berths[0], "2027-06-02", "2028-06-01")
	C := M + "/contracts/" + ct.Id
	var plan api.InstallmentPlan
	c.expect(t, 200, "", "GET", C+"/installments", nil, &plan)
	if plan.SumCents != plan.TotalCents || len(plan.Items) != 1 || plan.Items[0].Status != "issued" {
		t.Fatalf("plan %+v", plan)
	}
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/contracts/"+ct.Id+"/installments", nil, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", C+"/installments", nil, nil)
	var page api.InstallmentPage
	c.expect(t, 200, "", "GET", M+"/installments?status=issued", nil, &page)
	if len(page.Items) != 1 || page.Items[0].ContractCode == nil {
		t.Fatalf("scadenzario %+v", page.Items)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/installments?from=yesterday", nil, nil)
	r := c.expect(t, 200, "", "GET", M+"/installments?format=csv", nil, nil)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") || !strings.Contains(string(r.body), "contratto;") {
		t.Fatalf("csv %q", r.body)
	}
	it := plan.Items[0]
	I := M + "/installments/" + it.Id
	settle := map[string]any{"method": "bank_transfer", "reference": "CRO 1"}
	ds.dock.expect(t, 403, "forbidden", "POST", I+"/settle", settle, nil, "If-Match", etag(it.Version))
	c.expect(t, 428, "precondition_required", "POST", I+"/settle", settle, nil)
	c.expect(t, 412, "version_conflict", "POST", I+"/settle", settle, nil, "If-Match", etag(it.Version+3))
	var paid api.Installment
	c.expect(t, 200, "", "POST", I+"/settle", settle, &paid, "If-Match", etag(it.Version))
	if paid.Status != "paid" || paid.SettlementReference == nil || *paid.SettlementReference != "CRO 1" {
		t.Fatalf("paid %+v", paid)
	}
	c.expect(t, 409, "invalid_transition", "POST", I+"/settle", settle, nil, "If-Match", etag(paid.Version))
	c.expect(t, 409, "conflict", "POST", M+"/installments/settle", map[string]any{"items": []any{map[string]any{"id": it.Id, "version": 0}}, "method": "cash"}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/installments/issue", map[string]any{"ids": []string{}}, nil)
	var ir api.IssueResult
	c.expect(t, 200, "", "POST", M+"/installments/issue", map[string]any{"ids": []string{it.Id}}, &ir)
	if ir.Issued != 0 || ir.Skipped != 1 {
		t.Fatalf("issue %+v", ir)
	}
	var rr api.RemindResult
	c.expect(t, 200, "", "POST", M+"/installments/remind", map[string]any{"ids": []string{it.Id}}, &rr)
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/installments/"+it.Id+"/settle", settle, nil, "If-Match", etag(paid.Version))
	c.expect(t, 400, "validation_failed", "POST", C+"/installments/replan", map[string]any{"plan": "someday"}, nil, "If-Match", etag(ct.Version))

	// Suspend and resume.
	var s api.Contract
	c.expect(t, 400, "validation_failed", "POST", C+"/suspend", map[string]any{"reason": ""}, nil, "If-Match", etag(ct.Version))
	c.expect(t, 200, "", "POST", C+"/suspend", map[string]any{"reason": "morosità"}, &s, "If-Match", etag(ct.Version))
	if s.Status != "suspended" || s.SuspendedAt == nil {
		t.Fatalf("suspended %+v", s.Status)
	}
	c.expect(t, 409, "invalid_transition", "POST", C+"/suspend", map[string]any{"reason": "x"}, nil, "If-Match", etag(s.Version))
	c.expect(t, 200, "", "POST", C+"/resume", nil, &s, "If-Match", etag(s.Version))
	c.expect(t, 409, "invalid_transition", "POST", C+"/request-signature", map[string]any{}, nil, "If-Match", etag(s.Version))
	// Renewal: no 2028 list → 422 no_price.
	c.expect(t, 422, "no_price", "POST", C+"/renew", map[string]any{}, nil, "If-Match", etag(s.Version))
	ds.dock.expect(t, 403, "forbidden", "POST", C+"/renew", map[string]any{}, nil, "If-Match", etag(s.Version))
}
