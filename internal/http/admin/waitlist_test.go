//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestWaitlistHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	body := map[string]any{"customer_id": ds.customer, "vessel_id": ds.vessel, "term": "annual", "requested_from": "2027-06-01"}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/waitlist", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/waitlist", body, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/waitlist", map[string]any{"customer_id": ds.customer, "term": "annual", "requested_from": "2027-06-01"}, nil)
	var w api.WaitlistEntry
	c.expect(t, 201, "", "POST", M+"/waitlist", body, &w)
	if w.LoaCm != 1134 || w.Status != "waiting" {
		t.Fatalf("entry %+v", w)
	}
	c.expect(t, 200, "", "PATCH", M+"/waitlist/"+w.Id, map[string]any{"priority": 5}, &w, "If-Match", etag(w.Version))
	var page api.WaitlistPage
	c.expect(t, 200, "", "GET", M+"/waitlist?status=waiting", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Priority != 5 {
		t.Fatalf("list %+v", page)
	}
	ds.other.expect(t, 404, "not_found", "PATCH", ds.otherM+"/waitlist/"+w.Id, map[string]any{"priority": 1}, nil, "If-Match", etag(w.Version))
	q := map[string]any{
		"kind": "mooring", "term": "annual", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-06-10", "ends_on": "2028-06-10",
		"options": []any{map[string]any{"hold": false, "placements": []any{seg(ds.berths[2], "2027-06-10", "2028-06-09")}}},
	}
	var quote api.Quote
	c.expect(t, 201, "", "POST", M+"/waitlist/"+w.Id+"/quote", q, &quote)
	c.expect(t, 200, "", "GET", M+"/waitlist?status=offered", nil, &page)
	if len(page.Items) != 1 || page.Items[0].OfferedQuoteId == nil || *page.Items[0].OfferedQuoteId != quote.Id {
		t.Fatalf("offered %+v", page.Items)
	}
}
