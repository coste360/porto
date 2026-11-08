//go:build integration

package admin_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestUtilityHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M

	// Settings: defaults with version 1, If-Match.
	var set api.UtilitySettings
	c.expect(t, 200, "", "GET", M+"/utility-settings", nil, &set)
	if set.Mode != "with_installment" || set.Version != 1 {
		t.Fatalf("settings %+v", set)
	}
	c.expect(t, 428, "precondition_required", "PUT", M+"/utility-settings", map[string]any{"mode": "monthly"}, nil)
	c.expect(t, 200, "", "PUT", M+"/utility-settings", map[string]any{"mode": "monthly", "power_daily_threshold_milli": 50000}, &set, "If-Match", etag(1))
	if set.Mode != "monthly" || set.Version != 2 {
		t.Fatalf("settings %+v", set)
	}
	ds.dock.expect(t, 403, "forbidden", "PUT", M+"/utility-settings", map[string]any{"mode": "monthly"}, nil, "If-Match", etag(2))
	ds.other.expect(t, 404, "not_found", "GET", M+"/utility-settings", nil, nil)

	// Meters.
	var m api.UtilityMeter
	c.expect(t, 201, "", "POST", M+"/utility-meters", map[string]any{
		"code": "A1-E", "kind": "power", "unit": "kwh", "resource_type": "berth", "resource_id": ds.berths[0],
	}, &m)
	c.expect(t, 409, "code_taken", "POST", M+"/utility-meters", map[string]any{"code": "A1-E", "kind": "power", "unit": "kwh"}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/utility-meters", map[string]any{"code": "H", "kind": "hours", "unit": "kwh"}, nil)
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/utility-meters", map[string]any{"code": "D", "kind": "water", "unit": "m3"}, nil)
	var mp api.UtilityMeterPage
	ds.dock.expect(t, 200, "", "GET", M+"/utility-meters?route=dock&q=a1", nil, &mp)
	if len(mp.Items) != 1 || mp.Items[0].MultiplierMilli != 1000 {
		t.Fatalf("dock meters %+v", mp)
	}
	c.expect(t, 200, "", "PATCH", M+"/utility-meters/"+m.Id, map[string]any{"serial": "SN-9", "rollover_milli": nil}, &m, "If-Match", etag(int(m.Version)))
	if m.Serial == nil || *m.Serial != "SN-9" {
		t.Fatalf("patched meter %+v", m)
	}
	ds.other.expect(t, 404, "not_found", "GET", M+"/utility-meters/"+m.Id, nil, nil)

	// Readings (dock, offline): the replay returns the stored reading.
	var r1, r2 api.MeterReading
	body := map[string]any{"meter_id": m.Id, "value": "1234,5", "client_ref": "rd-1"}
	ds.dock.expect(t, 201, "", "POST", M+"/meter-readings", body, &r1)
	ds.dock.expect(t, 201, "", "POST", M+"/meter-readings", body, &r2)
	if r1.Id != r2.Id || r1.ValueMilli != 1_234_500 || r1.Status != "valid" {
		t.Fatalf("readings %+v / %+v", r1, r2)
	}
	ds.dock.expect(t, 400, "validation_failed", "POST", M+"/meter-readings", map[string]any{"meter_id": m.Id, "value": "1,2345"}, nil)
	ds.yard.expect(t, 403, "forbidden", "POST", M+"/meter-readings", map[string]any{"meter_id": m.Id, "value": "1"}, nil)
	ds.other.expect(t, 404, "not_found", "POST", M+"/meter-readings", body, nil)
	var low api.MeterReading
	ds.dock.expect(t, 201, "", "POST", M+"/meter-readings", map[string]any{"meter_id": m.Id, "value": "1000", "client_ref": "rd-2"}, &low)
	if low.Status != "anomaly" || len(low.AnomalyCodes) != 1 || low.AnomalyCodes[0] != "negative" {
		t.Fatalf("anomaly %+v", low)
	}
	var rp api.MeterReadingPage
	ds.dock.expect(t, 200, "", "GET", M+"/meter-readings?meter_id="+m.Id, nil, &rp)
	if len(rp.Items) != 2 || rp.Items[0].Id != low.Id {
		t.Fatalf("readings page %+v", rp)
	}

	// Photo (dependent offline op) and link.
	png := harborPNG(t)
	P := M + "/meter-readings/" + r1.Id + "/photo"
	if rr := ds.dock.raw(t, "POST", P, "text/plain", strings.NewReader("x")); rr.status != 415 {
		t.Fatalf("text photo: %d", rr.status)
	}
	var ph1, ph2 api.MeterReadingPhoto
	for _, ph := range []*api.MeterReadingPhoto{&ph1, &ph2} {
		rr := ds.dock.raw(t, "POST", P, "image/png", bytes.NewReader(png))
		if rr.status != 200 {
			t.Fatalf("photo %d %s", rr.status, rr.body)
		}
		mntJSON(t, rr.body, ph)
	}
	if ph1.FileId != ph2.FileId {
		t.Fatal("the same photo was stored twice")
	}
	var link api.FileLink
	ds.dock.expect(t, 200, "", "GET", P, nil, &link)
	ds.other.expect(t, 404, "not_found", "GET", P, nil, nil)
	if rr := ds.dock.raw(t, "GET", link.Url, "", nil); rr.status != 200 || rr.header.Get("Content-Type") == "" {
		t.Fatalf("signed photo download: %d", rr.status)
	}
	ds.dock.expect(t, 403, "download_link_expired", "GET", P+"/file?exp=1&sig=x", nil, nil)
	ds.other.expect(t, 404, "not_found", "GET", link.Url, nil, nil)

	// Office review: confirm, void.
	c.expect(t, 409, "invalid_transition", "POST", M+"/meter-readings/"+r1.Id+"/confirm", nil, nil, "If-Match", etag(int(r1.Version)+1))
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/meter-readings/"+low.Id+"/confirm", nil, nil, "If-Match", etag(int(low.Version)))
	c.expect(t, 200, "", "POST", M+"/meter-readings/"+low.Id+"/void", map[string]any{"reason": "errore di lettura"}, &low, "If-Match", etag(int(low.Version)))
	if low.Status != "void" {
		t.Fatalf("void %+v", low)
	}

	// Consumptions and charging.
	var cp api.UtilityConsumptionPage
	c.expect(t, 200, "", "GET", M+"/utility-consumptions?kind=power", nil, &cp)
	ds.dock.expect(t, 403, "forbidden", "GET", M+"/utility-consumptions", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/utility-consumptions?status=paid", nil, nil)
	var run api.UtilityChargeRunResult
	c.expect(t, 200, "", "POST", M+"/utility-charges/run", nil, &run)
	if run.Charges == nil || run.Skipped == nil {
		t.Fatalf("run %+v", run)
	}
	ds.other.expect(t, 404, "not_found", "POST", M+"/utility-charges/run", nil, nil)
	c.expect(t, 404, "not_found", "POST", M+"/utility-charges/run?contract_id=01900000-0000-7000-8000-000000000000", nil, nil)

	for _, rt := range []struct{ method, path string }{
		{"GET", "/utility-meters"},
		{"POST", "/utility-meters"},
		{"PATCH", "/utility-meters/" + m.Id},
		{"GET", "/meter-readings"},
		{"POST", "/meter-readings/" + r1.Id + "/confirm"},
		{"POST", "/meter-readings/" + r1.Id + "/void"},
		{"GET", "/utility-consumptions"},
		{"PUT", "/utility-settings"},
	} {
		ds.other.expect(t, 404, "not_found", rt.method, M+rt.path, map[string]any{}, nil, "If-Match", etag(1))
	}
}
