//go:build integration

package admin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/http/api"
)

// deskSetup is the pricing setup plus a harbour (berths A1..A3 of 12 × 4 m, a 60 m linear area, a parking), a
// vessel, a customer and an active 2027 list for transits and annual contracts, all in marina M.
type deskSetup struct {
	*pricingSetup
	berths   []string
	area     string
	land     string
	vessel   string
	big      string // 15 m: does not fit the berths
	customer string
}

func newDeskSetup(t *testing.T) *deskSetup {
	t.Helper()
	ps := newPricingSetup(t)
	ds := &deskSetup{pricingSetup: ps}
	h := contracttest.InsertHarbour(t, ps.s.app.DB, ps.marinaID, time.Now().UTC(), 3)
	ds.berths, ds.area, ds.land = h.Berths, h.Area, h.Lands[0]
	c, M := ps.owner, ps.M
	var v api.Vessel
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Libeccio", "loa_cm": 1134, "beam_cm": 380, "hull_type": "monohull_sail"}, &v)
	ds.vessel = v.Id
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Bora", "loa_cm": 1500, "beam_cm": 450, "hull_type": "monohull_motor"}, &v)
	ds.big = v.Id
	var cu struct {
		Customer api.Customer `json:"customer"`
	}
	c.expect(t, 201, "", "POST", M+"/customers", map[string]any{"kind": "person", "first_name": "Mario", "last_name": "Rossi"}, &cu)
	ds.customer = cu.Customer.Id

	// Items and the 2027 list.
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	var trn, orm api.Item
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night",
		"revenue_class_id": classes.Items[1].Id, "price_includes_vat": true,
	}, &trn)
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "ORM-ANN", "name": map[string]string{"it": "Annuale"}, "item_type": "berth_fee", "unit": "year",
		"revenue_class_id": classes.Items[1].Id, "price_includes_vat": true,
	}, &orm)
	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"transit", "annual"},
	}, &doc)
	body := saveBody(doc)
	body["settings"] = map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"}, "min_beam_ratio": map[string]int{"num": 1, "den": 3},
		"primary_items": map[string]string{"transit": trn.Id, "annual": orm.Id},
	}
	body["periods"], body["bands"] = []any{}, []any{}
	body["rules"] = []map[string]any{
		{"key": "r1", "stage": "base", "name": "Transito", "item_id": trn.Id, "basis": "per_meter", "unit": "night", "unit_cents": 300, "scope": map[string]any{"contract_types": []string{"transit"}}},
		{"key": "r2", "stage": "base", "name": "Annuale", "item_id": orm.Id, "basis": "flat", "unit": "year", "amount_cents": 480000, "scope": map[string]any{"contract_types": []string{"annual"}}},
	}
	var res api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, body, &res, "If-Match", etag(doc.Version))
	c.expect(t, 200, "", "POST", M+"/price-lists/"+doc.Id+"/activate", map[string]any{}, nil, "If-Match", etag(res.Document.Version))
	return ds
}

func seg(id, first, last string) map[string]any {
	return map[string]any{"resource_type": "berth", "resource_id": id, "first_night": first, "last_night": last}
}

func TestContractHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	body := map[string]any{
		"term": "annual", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-03-01",
		"placements":      []any{seg(ds.berths[0], "2027-03-01", "2028-02-29")},
		"extra_resources": []any{map[string]any{"land_resource_id": ds.land}},
	}
	// Validation, permissions, other marina.
	c.expect(t, 400, "validation_failed", "POST", M+"/contracts", map[string]any{"term": "weekly", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-03-01", "placements": []any{}}, nil)
	c.expect(t, 400, "bad_request", "POST", M+"/contracts", map[string]any{"term": "annual", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-02-30", "placements": []any{}}, nil)
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/contracts", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/contracts", body, nil)
	big := map[string]any{
		"term": "annual", "customer_id": ds.customer, "vessel_id": ds.big, "starts_on": "2027-03-01",
		"placements": []any{seg(ds.berths[1], "2027-03-01", "2028-02-29")},
	}
	c.expect(t, 422, "fit_blocked", "POST", M+"/contracts", big, nil)

	var ct api.Contract
	r := c.expect(t, 201, "", "POST", M+"/contracts", body, &ct)
	if ct.Status != "draft" || !strings.HasPrefix(ct.Code, "ORM-") || len(ct.Allocations) != 2 || ct.HoldExpiresAt == nil || r.header.Get("ETag") != etag(ct.Version) ||
		ct.Customer == nil || ct.Customer.DisplayName == nil || ct.Breakdown == nil || ct.TotalCents != 480000 {
		t.Fatalf("draft %+v", ct)
	}
	c.expect(t, 409, "resource_taken", "POST", M+"/contracts", body, nil)
	P := M + "/contracts/" + ct.Id
	// Reads: dock yes (contracts.read), yard no.
	ds.dock.expect(t, 200, "", "GET", P, nil, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", P, nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/contracts/"+ct.Id, nil, nil)
	var page api.ContractPage
	c.expect(t, 200, "", "GET", M+"/contracts?status=draft&q=libeccio", nil, &page)
	if len(page.Items) != 1 || len(page.Items[0].Resources) != 2 {
		t.Fatalf("list %+v", page.Items)
	}
	c.expect(t, 200, "", "GET", M+"/contracts?q="+ct.Code, nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("search by code %+v", page.Items)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/contracts?active_on=domani", nil, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", M+"/contracts", nil, nil)

	// Patch, activate, cancel (invalid), terminate.
	c.expect(t, 428, "precondition_required", "PATCH", P, map[string]any{"notes": "x"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", P, map[string]any{"notes": "x"}, nil, "If-Match", etag(ct.Version+5))
	c.expect(t, 200, "", "PATCH", P, map[string]any{"notes": "pontile nord", "policy": map[string]any{"version": 1, "reprice_on_move": true}}, &ct, "If-Match", etag(ct.Version))
	if ct.Policy == nil || !*ct.Policy.RepriceOnMove || ct.Notes == nil {
		t.Fatalf("patched %+v", ct)
	}
	c.expect(t, 200, "", "POST", P+"/activate", nil, &ct, "If-Match", etag(ct.Version))
	if ct.Status != "active" || !ct.SignedOnPaper || ct.Allocations[0].Kind != "contract" {
		t.Fatalf("active %+v", ct)
	}
	c.expect(t, 409, "invalid_transition", "POST", P+"/activate", nil, nil, "If-Match", etag(ct.Version))
	c.expect(t, 409, "invalid_transition", "POST", P+"/cancel", map[string]any{}, nil, "If-Match", etag(ct.Version))
	c.expect(t, 400, "validation_failed", "POST", P+"/terminate", map[string]any{"effective_on": "2027-06-01", "reason": ""}, nil, "If-Match", etag(ct.Version))
	c.expect(t, 200, "", "POST", P+"/terminate", map[string]any{"effective_on": "2027-06-01", "reason": "vendita"}, &ct, "If-Match", etag(ct.Version))
	if ct.Status != "terminated" || ct.TerminatedOn == nil || *ct.TerminatedOn != "2027-06-01" {
		t.Fatalf("terminated %+v", ct)
	}

	// A draft is cancelled.
	body["placements"] = []any{seg(ds.berths[2], "2027-03-01", "2028-02-29")}
	delete(body, "extra_resources")
	c.expect(t, 201, "", "POST", M+"/contracts", body, &ct)
	c.expect(t, 200, "", "POST", M+"/contracts/"+ct.Id+"/cancel", map[string]any{"reason": "ripensamento"}, &ct, "If-Match", etag(ct.Version))
	if ct.Status != "cancelled" || ct.Allocations[0].Status != "cancelled" {
		t.Fatalf("cancelled %+v", ct)
	}
}
