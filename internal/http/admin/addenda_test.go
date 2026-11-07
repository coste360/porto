//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestAddendumHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	ct := ds.activeAnnual(t, ds.berths[0], "2027-06-02", "2028-06-01")
	A := M + "/contracts/" + ct.Id + "/addenda"
	body := map[string]any{"kind": "price_change", "effective_on": "2027-07-01", "payload": map[string]any{"new_total_cents": 500000, "reason": "accordo"}}
	ds.dock.expect(t, 403, "forbidden", "POST", A, body, nil)
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/contracts/"+ct.Id+"/addenda", body, nil)
	c.expect(t, 400, "validation_failed", "POST", A, map[string]any{"kind": "price_change", "effective_on": "2030-01-01", "payload": map[string]any{}}, nil)
	var ad api.Addendum
	c.expect(t, 201, "", "POST", A, body, &ad)
	if ad.AmountDeltaCents != 500000-ct.TotalCents || ad.Status != "draft" {
		t.Fatalf("addendum %+v", ad)
	}
	c.expect(t, 412, "version_conflict", "POST", A+"/"+ad.Id+"/activate", nil, nil, "If-Match", etag(ad.Version+2))
	c.expect(t, 200, "", "POST", A+"/"+ad.Id+"/activate", nil, &ad, "If-Match", etag(ad.Version))
	if ad.Status != "active" {
		t.Fatalf("activated %+v", ad)
	}
	c.expect(t, 409, "invalid_transition", "POST", A+"/"+ad.Id+"/cancel", nil, nil, "If-Match", etag(ad.Version))
	var list api.AddendumList
	c.expect(t, 200, "", "GET", A, nil, &list)
	var plan api.InstallmentPlan
	c.expect(t, 200, "", "GET", M+"/contracts/"+ct.Id+"/installments", nil, &plan)
	if len(list.Items) != 1 || plan.TotalCents != 500000 || plan.SumCents != plan.TotalCents {
		t.Fatalf("after addendum %+v %+v", list, plan)
	}
}
