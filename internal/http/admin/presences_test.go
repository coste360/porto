//go:build integration

package admin_test

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestPresenceHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.dock, ds.M
	var tr api.Contract
	c.expect(t, 201, "", "POST", M+"/transits", map[string]any{
		"vessel_id": ds.vessel, "customer_id": ds.customer, "arrival": "2027-07-10", "departure": "2027-07-12",
		"placements": []any{seg(ds.berths[0], "2027-07-10", "2027-07-11")}, "confirm": map[string]any{},
	}, &tr)

	c.expect(t, 400, "validation_failed", "POST", M+"/presences", map[string]any{"vessel_id": ds.vessel, "event": "docked"}, nil)
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/presences", map[string]any{"vessel_id": ds.vessel, "event": "arrived"}, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/presences", map[string]any{"vessel_id": ds.vessel, "event": "arrived"}, nil)
	var res api.PresenceResult
	c.expect(t, 201, "", "POST", M+"/presences", map[string]any{"vessel_id": ds.vessel, "event": "arrived", "notes": "ok"}, &res, "Idempotency-Key", "presence-key-0000001")
	if res.Presence.Event != "arrived" || res.Presence.Vessel.Id != ds.vessel {
		t.Fatalf("presence %+v", res)
	}
	c.expect(t, 409, "invalid_transition", "POST", M+"/presences", map[string]any{"vessel_id": ds.vessel, "event": "arrived"}, nil, "Idempotency-Key", "presence-key-0000002")

	// Photo: raw body, link, file.
	P := M + "/presences/" + res.Presence.Id + "/photo"
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if r := c.raw(t, "POST", P, "text/plain", strings.NewReader("x")); r.status != 415 {
		t.Fatalf("text photo: %d %s", r.status, r.body)
	}
	ds.yard.raw(t, "POST", P, "image/png", bytes.NewReader(buf.Bytes()))
	if r := c.raw(t, "POST", P, "image/png", bytes.NewReader(buf.Bytes())); r.status != 200 {
		t.Fatalf("upload: %d %s", r.status, r.body)
	}
	var link api.DocumentFileUrl
	c.expect(t, 200, "", "GET", P, nil, &link)
	ds.yard.expect(t, 403, "forbidden", "GET", P, nil, nil)
	if r := c.raw(t, "GET", strings.TrimPrefix(link.Url, ""), "", nil); r.status != 200 || r.header.Get("Content-Type") != "image/png" {
		t.Fatalf("file: %d", r.status)
	}
	if r := ds.owner.raw(t, "GET", link.Url, "", nil); r.status == 200 {
		t.Fatal("link usable by another user")
	}

	var page api.PresencePage
	ds.yard.expect(t, 200, "", "GET", M+"/presences?date=2027-07-10", nil, &page)
	_ = page
	var mv api.Movements
	ds.yard.expect(t, 200, "", "GET", M+"/movements?date=2027-07-10", nil, &mv)
	if mv.Date != "2027-07-10" || len(mv.ArrivalsExpected) != 1 || mv.ArrivalsExpected[0].Contract == nil {
		t.Fatalf("movements %+v", mv)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/movements?date=oggi", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/presences/"+res.Presence.Id+"/photo", nil, nil)

	// Unexpected arrival with a declared vessel.
	var ua api.PresenceResult
	c.expect(t, 201, "", "POST", M+"/presences/unexpected-arrival", map[string]any{
		"new_vessel": map[string]any{"name": "Grecale", "loa_cm": 800, "beam_cm": 280, "hull_type": "rib"},
		"target":     map[string]any{"resource_type": "berth", "resource_id": ds.berths[2]},
	}, &ua)
	if ua.Contract == nil || ua.Contract.Status != "held" || ua.Contract.Customer != nil {
		t.Fatalf("unexpected %+v", ua.Contract)
	}
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/presences/unexpected-arrival", map[string]any{"vessel_id": ds.vessel, "target": map[string]any{"resource_type": "berth", "resource_id": ds.berths[1]}}, nil)
}
