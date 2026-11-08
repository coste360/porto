//go:build integration

package admin_test

import (
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/qrsvg"
)

// TestTransitQr_PayloadAndScope: the SVG encodes PT1:<contract_id> (same bitmap as qrsvg.Render of the payload),
// contracts that are not transits and other marinas are 404, roles without contracts.read or transits.desk 403.
func TestTransitQr_PayloadAndScope(t *testing.T) {
	ds := newDeskSetup(t)
	M := ds.M
	var tr api.Contract
	ds.dock.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-10", "departure": "2027-07-12",
		"placements": []any{seg(ds.berths[0], "2027-07-10", "2027-07-11")},
	}, &tr)
	Q := M + "/transits/" + tr.Id + "/qr"
	want, err := qrsvg.Render("PT1:" + tr.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*harborClient{ds.dock, ds.owner} {
		r := c.raw(t, "GET", Q, "", nil)
		if r.status != 200 || r.header.Get("Content-Type") != "image/svg+xml" || string(r.body) != want {
			t.Fatalf("qr: %d %s %q", r.status, r.header.Get("Content-Type"), r.body)
		}
		if strings.Contains(string(r.body), "<script") || strings.Contains(string(r.body), "http://www.w3.org/1999/xlink") {
			t.Fatalf("svg has active content: %s", r.body)
		}
	}
	ds.yard.expect(t, 403, "forbidden", "GET", Q, nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/transits/"+tr.Id+"/qr", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", Q, nil, nil)
	ds.dock.expect(t, 404, "not_found", "GET", M+"/transits/not-an-id/qr", nil, nil)

	// A mooring contract has no transit QR.
	var ct api.Contract
	ds.owner.expect(t, 201, "", "POST", M+"/contracts", map[string]any{
		"term": "annual", "customer_id": ds.customer, "vessel_id": ds.vessel, "starts_on": "2027-03-01",
		"placements": []any{seg(ds.berths[1], "2027-03-01", "2028-02-29")},
	}, &ct)
	ds.owner.expect(t, 404, "not_found", "GET", M+"/transits/"+ct.Id+"/qr", nil, nil)
}
