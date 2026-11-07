//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestInquiryHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.dock, ds.M
	c.expect(t, 400, "validation_failed", "POST", M+"/inquiries", map[string]any{"purpose": "transit", "source": "phone", "arrival": "2027-08-01", "departure": "2027-08-03"}, nil)
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/inquiries", map[string]any{"purpose": "transit", "source": "phone", "arrival": "2027-08-01", "departure": "2027-08-03", "loa_cm": 1000, "beam_cm": 350}, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/inquiries", map[string]any{"purpose": "transit", "source": "phone", "arrival": "2027-08-01", "departure": "2027-08-03", "loa_cm": 1000, "beam_cm": 350}, nil)
	var in api.Inquiry
	c.expect(t, 201, "", "POST", M+"/inquiries", map[string]any{
		"purpose": "transit", "source": "phone", "arrival": "2027-08-01", "departure": "2027-08-03", "loa_cm": 2400, "beam_cm": 650,
		"outcome": "declined_no_space", "notes": "chiede un posto da 24 m",
	}, &in)
	if in.Outcome != "declined_no_space" || in.LoaCm != 2400 || in.Channel != "desk" {
		t.Fatalf("inquiry %+v", in)
	}
	var withVessel api.Inquiry
	c.expect(t, 201, "", "POST", M+"/inquiries", map[string]any{
		"purpose": "contract", "term": "annual", "source": "desk", "arrival": "2027-08-01", "departure": "2028-08-01",
		"vessel_id": ds.vessel, "customer_id": ds.customer,
	}, &withVessel)
	if withVessel.LoaCm != 1134 || withVessel.Vessel == nil || withVessel.Customer == nil || withVessel.Outcome != "open" {
		t.Fatalf("with vessel %+v", withVessel)
	}
	I := M + "/inquiries/" + in.Id
	c.expect(t, 428, "precondition_required", "PATCH", I, map[string]any{"outcome": "lost"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", I, map[string]any{"outcome": "lost"}, nil, "If-Match", etag(in.Version+1))
	c.expect(t, 400, "validation_failed", "PATCH", I, map[string]any{"outcome": "maybe"}, nil, "If-Match", etag(in.Version))
	c.expect(t, 200, "", "PATCH", I, map[string]any{"outcome": "lost"}, &in, "If-Match", etag(in.Version))
	var page api.InquiryPage
	c.expect(t, 200, "", "GET", M+"/inquiries?outcome=lost&from=2027-07-01&to=2027-08-31", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Id != in.Id {
		t.Fatalf("list %+v", page.Items)
	}
	ds.yard.expect(t, 403, "forbidden", "GET", M+"/inquiries", nil, nil)
	ds.other.expect(t, 404, "not_found", "PATCH", ds.otherM+"/inquiries/"+in.Id, map[string]any{"outcome": "lost"}, nil, "If-Match", etag(in.Version))
}
