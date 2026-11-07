//go:build integration

package admin_test

import (
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestQuoteHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	body := map[string]any{
		"kind": "transit", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-08-01", "ends_on": "2027-08-04",
		"options": []any{
			map[string]any{"label": "Pontile A", "hold": true, "placements": []any{seg(ds.berths[0], "2027-08-01", "2027-08-03")}},
			map[string]any{"label": "Pontile A bis", "hold": true, "placements": []any{seg(ds.berths[1], "2027-08-01", "2027-08-03")}},
		},
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/quotes", map[string]any{"kind": "mooring", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-08-01", "ends_on": "2027-08-04", "options": []any{}}, nil)
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/quotes", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/quotes", body, nil)
	var q api.Quote
	r := c.expect(t, 201, "", "POST", M+"/quotes", body, &q)
	if !strings.HasPrefix(q.Code, "PRV-") || q.Status != "draft" || len(q.Options) != 2 || q.Options[0].Placements[0].AllocationId == nil ||
		q.Options[0].TotalCents != 3*3420 || q.Options[0].Breakdown == nil || r.header.Get("ETag") != etag(q.Version) {
		t.Fatalf("quote %+v", q)
	}
	// Holds block other sales.
	ds.dock.expect(t, 409, "resource_taken", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "arrival": "2027-08-02", "departure": "2027-08-03", "placements": []any{seg(ds.berths[1], "2027-08-02", "2027-08-02")},
	}, nil)
	Q := M + "/quotes/" + q.Id
	ds.dock.expect(t, 200, "", "GET", Q, nil, nil)
	ds.yard.expect(t, 403, "forbidden", "GET", Q, nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/quotes/"+q.Id, nil, nil)
	var page api.QuotePage
	c.expect(t, 200, "", "GET", M+"/quotes?status=draft&kind=transit", nil, &page)
	if len(page.Items) != 1 || page.Items[0].OptionsCount != 2 || page.Items[0].MinTotalCents == nil {
		t.Fatalf("quotes %+v", page.Items)
	}
	c.expect(t, 428, "precondition_required", "PATCH", Q, body, nil)
	c.expect(t, 200, "", "PATCH", Q, body, &q, "If-Match", etag(q.Version))
	c.expect(t, 200, "", "POST", Q+"/send", nil, &q, "If-Match", etag(q.Version))
	if q.Status != "sent" || q.SentAt == nil {
		t.Fatalf("sent %+v", q)
	}
	c.expect(t, 409, "invalid_transition", "PATCH", Q, body, nil, "If-Match", etag(q.Version))
	c.expect(t, 400, "validation_failed", "POST", Q+"/accept", map[string]any{"option_id": "01900000-0000-7000-8000-000000000009"}, nil, "If-Match", etag(q.Version))
	var acc api.QuoteAcceptResult
	c.expect(t, 200, "", "POST", Q+"/accept", map[string]any{"option_id": q.Options[1].Id, "confirm": map[string]any{"reference": "bonifico"}}, &acc, "If-Match", etag(q.Version))
	if acc.Quote.Status != "accepted" || acc.Contract.Status != "confirmed" || acc.Contract.QuoteId == nil || acc.Contract.TotalCents != q.Options[1].TotalCents {
		t.Fatalf("accepted %+v / %+v", acc.Quote, acc.Contract)
	}
	c.expect(t, 409, "invalid_transition", "POST", Q+"/decline", map[string]any{}, nil, "If-Match", etag(acc.Quote.Version))
	// Option 1's hold is gone: the berth sells.
	ds.dock.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "arrival": "2027-08-02", "departure": "2027-08-03", "placements": []any{seg(ds.berths[0], "2027-08-02", "2027-08-02")},
	}, nil)

	// A declined quote.
	body["options"] = []any{map[string]any{"hold": false, "placements": []any{seg(ds.berths[2], "2027-08-01", "2027-08-03")}}}
	c.expect(t, 201, "", "POST", M+"/quotes", body, &q)
	c.expect(t, 200, "", "POST", M+"/quotes/"+q.Id+"/decline", map[string]any{"reason": "prezzo"}, &q, "If-Match", etag(q.Version))
	if q.Status != "declined" {
		t.Fatalf("declined %+v", q)
	}
}
