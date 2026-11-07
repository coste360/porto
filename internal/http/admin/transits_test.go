//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestTransitHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.dock, ds.M
	body := map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-10", "departure": "2027-07-13",
		"placements": []any{seg(ds.berths[0], "2027-07-10", "2027-07-12")}, "expected_arrival_time": "16:30",
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/transits", map[string]any{"vessel_id": ds.vessel, "arrival": "2027-07-10", "departure": "2027-07-09"}, nil)
	c.expect(t, 400, "bad_request", "POST", M+"/transits", map[string]any{"vessel_id": ds.vessel, "arrival": "10/07/2027", "departure": "2027-07-11"}, nil)
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/transits", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/transits", body, nil)
	c.expect(t, 403, "forbidden", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "arrival": "2027-07-20", "departure": "2027-07-21", "placements": []any{seg(ds.berths[1], "2027-07-20", "2027-07-20")},
		"fit_override": map[string]any{"reason": "deroga"},
	}, nil)
	c.expect(t, 422, "customer_required", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "arrival": "2027-07-20", "departure": "2027-07-21", "placements": []any{seg(ds.berths[1], "2027-07-20", "2027-07-20")},
		"confirm": map[string]any{"reference": "x"},
	}, nil)
	c.expect(t, 422, "fit_blocked", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.big, "arrival": "2027-07-20", "departure": "2027-07-21", "placements": []any{seg(ds.berths[1], "2027-07-20", "2027-07-20")},
	}, nil)
	// The owner may override, with the reason audited on the allocation.
	var ov api.Contract
	ds.owner.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.big, "arrival": "2027-07-20", "departure": "2027-07-21", "placements": []any{seg(ds.berths[1], "2027-07-20", "2027-07-20")},
		"fit_override": map[string]any{"reason": "misure in deroga"},
	}, &ov)
	if ov.Allocations[0].FitOverride == nil || ov.Allocations[0].FitOverride.Reason != "misure in deroga" {
		t.Fatalf("override %+v", ov.Allocations[0])
	}
	// A new vessel declared at the desk.
	var nv api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"new_vessel": map[string]any{"name": "Scirocco", "loa_cm": 900, "beam_cm": 300, "hull_type": "rib"},
		"arrival":    "2027-07-20", "departure": "2027-07-21", "placements": []any{seg(ds.berths[2], "2027-07-20", "2027-07-20")},
	}, &nv)
	if nv.Vessel.Name != "Scirocco" || nv.Status != "held" {
		t.Fatalf("new vessel %+v", nv)
	}

	var tr api.Contract
	r := c.expect(t, 201, "", "POST", M+"/transits", body, &tr, "Idempotency-Key", "transit-key-00000001")
	if tr.Status != "held" || tr.Kind != "transit" || r.header.Get("ETag") != etag(tr.Version) || tr.ExpectedArrivalTime == nil || *tr.ExpectedArrivalTime != "16:30" {
		t.Fatalf("held %+v", tr)
	}
	// Same key: replayed; another key: the berth is taken.
	var replay api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", body, &replay, "Idempotency-Key", "transit-key-00000001")
	if replay.Id != tr.Id {
		t.Fatal("not replayed")
	}
	c.expect(t, 409, "resource_taken", "POST", M+"/transits", body, nil, "Idempotency-Key", "transit-key-00000002")

	T := M + "/transits/" + tr.Id
	c.expect(t, 428, "precondition_required", "POST", T+"/confirm", map[string]any{}, nil)
	c.expect(t, 412, "version_conflict", "POST", T+"/confirm", map[string]any{}, nil, "If-Match", etag(tr.Version+1))
	c.expect(t, 409, "invalid_transition", "POST", T+"/no-show", nil, nil, "If-Match", etag(tr.Version))
	c.expect(t, 200, "", "POST", T+"/confirm", map[string]any{"reference": "POS 77", "pay_at_departure": true}, &tr, "If-Match", etag(tr.Version))
	if tr.Status != "confirmed" || tr.ConfirmationSource == nil || *tr.ConfirmationSource != "manual" || tr.ConfirmationRef == nil || !tr.PayAtDeparture {
		t.Fatalf("confirmed %+v", tr)
	}
	// The reference is shown to desk roles only.
	var asYard api.Contract
	ds.owner.expect(t, 200, "", "GET", M+"/contracts/"+tr.Id, nil, &asYard)
	if asYard.ConfirmationRef == nil {
		t.Fatal("owner does not see the reference")
	}
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/transits/"+tr.Id+"/confirm", map[string]any{}, nil, "If-Match", etag(tr.Version))

	// Stay: extension on a taken night is 409; a free extension reprices.
	var other api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-14", "departure": "2027-07-15",
		"placements": []any{seg(ds.berths[0], "2027-07-14", "2027-07-14")},
	}, &other)
	c.expect(t, 409, "resource_taken", "POST", T+"/stay", map[string]any{"departure": "2027-07-15"}, nil, "If-Match", etag(tr.Version))
	var stay api.StayChangeResult
	c.expect(t, 200, "", "POST", T+"/stay", map[string]any{"departure": "2027-07-14"}, &stay, "If-Match", etag(tr.Version))
	if stay.Contract.EndsOn != "2027-07-14" || stay.PriceEffect.DeltaCents != 3420 || stay.PriceEffect.NewTotalCents != 4*3420 {
		t.Fatalf("stay %+v", stay.PriceEffect)
	}
	tr = stay.Contract
	c.expect(t, 200, "", "PATCH", T, map[string]any{"notes": "arriva tardi", "expected_arrival_time": "22:00"}, &tr, "If-Match", etag(tr.Version))
	c.expect(t, 400, "validation_failed", "PATCH", T, map[string]any{"expected_arrival_time": "25:00"}, nil, "If-Match", etag(tr.Version))

	var page api.ContractPage
	c.expect(t, 200, "", "GET", M+"/transits?status=confirmed&arrival_from=2027-07-01&arrival_to=2027-07-31", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Id != tr.Id {
		t.Fatalf("transits %+v", page.Items)
	}
	c.expect(t, 200, "", "GET", M+"/transits?on=2027-07-11", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("on %+v", page.Items)
	}
	c.expect(t, 200, "", "POST", T+"/cancel", map[string]any{"reason": "maltempo"}, &tr, "If-Match", etag(tr.Version))
	if tr.Status != "cancelled" {
		t.Fatalf("cancelled %+v", tr)
	}
	c.expect(t, 409, "invalid_transition", "POST", T+"/cancel", map[string]any{}, nil, "If-Match", etag(tr.Version))
	ds.yard.expect(t, 403, "forbidden", "GET", M+"/transits", nil, nil)
}
