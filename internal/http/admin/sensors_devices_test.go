//go:build integration

package admin_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/http/api"
)

func TestSensorDeviceHandlers_Scenario(t *testing.T) {
	s := newHarborSrv(t)
	ctx := context.Background()
	o, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "owner@porto.test", Name: "Owner", Password: harborPW, Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-s"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "other@porto.test", Name: "Other", Password: harborPW, Organization: "Org2", MarinaName: "Altra", MarinaSlug: "marina-t"})
	if err != nil {
		t.Fatal(err)
	}
	h := contracttest.InsertHarbour(t, s.app.DB, o.MarinaID, time.Now().UTC(), 3)
	c, other := s.login(t, "owner@porto.test"), s.login(t, "other@porto.test")
	M, X := "/api/admin/v1/marinas/"+o.MarinaID, "/api/admin/v1/marinas/"+x.MarinaID
	var mem struct {
		UserID string `json:"user_id"`
	}
	c.expect(t, 201, "", "POST", M+"/members", map[string]any{"email": "dock@porto.test", "name": "dock", "role": "dock"}, &mem)
	if err := s.app.Auth.SetPassword(ctx, mem.UserID, harborPW); err != nil {
		t.Fatal(err)
	}
	dock := s.login(t, "dock@porto.test")

	// Devices.
	dev := map[string]any{"external_id": "A81758FFFE0312AB", "code": "S-A1", "kind": "ultrasonic", "resource_type": "berth", "resource_id": h.Berths[0]}
	var created api.SensorDeviceWithKey
	r := c.expect(t, 201, "", "POST", M+"/sensors/devices", dev, &created)
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("ETag") != `"1"` || !strings.HasPrefix(created.Key, "po_sns_") ||
		created.Device.KeyStatus != "active" || created.Device.Physical != "unknown" || created.Device.ResourceCode != "A1" || created.Device.Health != "ok" {
		t.Fatalf("create %s", r.body)
	}
	if created.Device.KeyId == nil || !strings.HasPrefix(created.Key, *created.Device.KeyId+"_") {
		t.Fatal("key id")
	}
	c.expect(t, 409, "code_taken", "POST", M+"/sensors/devices", dev, nil)
	dev2 := map[string]any{"external_id": "X2", "code": "S-X2", "kind": "radar", "resource_type": "berth", "resource_id": h.Berths[0]}
	c.expect(t, 409, "resource_has_sensor", "POST", M+"/sensors/devices", dev2, nil)
	dev2["resource_type"], dev2["resource_id"], dev2["from_cm"], dev2["to_cm"] = "dynamic_area", h.Area, 0, 50
	c.expect(t, 422, "invalid_sensor_resource", "POST", M+"/sensors/devices", dev2, nil)
	dev2["to_cm"] = 1000
	var seg api.SensorDeviceWithKey
	c.expect(t, 201, "", "POST", M+"/sensors/devices", dev2, &seg)
	if seg.Device.FromCm == nil || *seg.Device.ToCm != 1000 {
		t.Fatalf("segment %+v", seg.Device)
	}
	dev2["external_id"], dev2["code"], dev2["from_cm"], dev2["to_cm"] = "X3", "S-X3", 500, 1500
	c.expect(t, 422, "segment_overlap", "POST", M+"/sensors/devices", dev2, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/sensors/devices", map[string]any{"external_id": "bad id", "code": "x", "kind": "radar", "resource_type": "berth", "resource_id": h.Berths[1]}, nil)
	dock.expect(t, 403, "forbidden", "POST", M+"/sensors/devices", map[string]any{"external_id": "D", "code": "D", "kind": "radar", "resource_type": "berth", "resource_id": h.Berths[2]}, nil)

	var list api.SensorDeviceList
	dock.expect(t, 200, "", "GET", M+"/sensors/devices?limit=1", nil, &list)
	if len(list.Items) != 1 || list.Items[0].Code != "S-A1" || list.NextCursor == nil {
		t.Fatalf("list %+v", list)
	}
	dock.expect(t, 200, "", "GET", M+"/sensors/devices?cursor="+*list.NextCursor, nil, &list)
	if len(list.Items) != 1 || list.Items[0].Code != "S-X2" || list.NextCursor != nil {
		t.Fatalf("page 2 %+v", list)
	}
	dock.expect(t, 200, "", "GET", M+"/sensors/devices?resource_type=dynamic_area&health=ok&active=true", nil, &list)
	if len(list.Items) != 1 {
		t.Fatalf("filtered %+v", list)
	}
	id := created.Device.Id
	var got api.SensorDevice
	r = dock.expect(t, 200, "", "GET", M+"/sensors/devices/"+id, nil, &got)
	if r.header.Get("ETag") != `"1"` || strings.Contains(string(r.body), created.Key) {
		t.Fatalf("get %s", r.body)
	}
	other.expect(t, 404, "not_found", "GET", M+"/sensors/devices/"+id, nil, nil)
	other.expect(t, 404, "not_found", "GET", X+"/sensors/devices/"+id, nil, nil)

	c.expect(t, 428, "precondition_required", "PATCH", M+"/sensors/devices/"+id, map[string]any{"model": "UX"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/sensors/devices/"+id, map[string]any{"model": "UX"}, nil, "If-Match", etag(9))
	r = c.expect(t, 200, "", "PATCH", M+"/sensors/devices/"+id, map[string]any{"model": "UX", "resource_id": h.Berths[1], "binding": map[string]any{}}, &got, "If-Match", etag(1))
	if got.Model != "UX" || got.ResourceId != h.Berths[1] || r.header.Get("ETag") != `"2"` {
		t.Fatalf("patch %s", r.body)
	}
	var rot api.SensorDeviceWithKey
	r = c.expect(t, 200, "", "POST", M+"/sensors/devices/"+id+"/rotate-key", map[string]any{"grace_hours": 2}, &rot, "If-Match", etag(2))
	if r.header.Get("Cache-Control") != "no-store" || rot.Key == created.Key || rot.Device.KeyStatus != "rotating" || rot.Device.PrevValidUntil == nil {
		t.Fatalf("rotate %s", r.body)
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/sensors/devices/"+id+"/rotate-key", map[string]any{"grace_hours": 200}, nil, "If-Match", etag(3))
	var revoked api.SensorDevice
	c.expect(t, 200, "", "POST", M+"/sensors/devices/"+id+"/revoke-key", nil, &revoked, "If-Match", etag(3))
	if revoked.KeyStatus != "revoked" || revoked.KeyId != nil || revoked.KeyIssuedAt != nil {
		t.Fatalf("revoke %+v", revoked)
	}

	// Gateways.
	var gw api.SensorGatewayWithKey
	r = c.expect(t, 201, "", "POST", M+"/sensors/gateways", map[string]any{"code": "GW1", "name": "LoRa", "kind": "lorawan"}, &gw)
	if r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(gw.Key, "po_gw_") {
		t.Fatalf("gateway %s", r.body)
	}
	c.expect(t, 409, "code_taken", "POST", M+"/sensors/gateways", map[string]any{"code": "GW1", "name": "LoRa", "kind": "lorawan"}, nil)
	dock.expect(t, 403, "forbidden", "GET", M+"/sensors/gateways", nil, nil)
	var gws api.SensorGatewayList
	c.expect(t, 200, "", "GET", M+"/sensors/gateways", nil, &gws)
	if len(gws.Items) != 1 || strings.Contains(string(c.do(t, "GET", M+"/sensors/gateways", nil).body), gw.Key) {
		t.Fatalf("gateways %+v", gws)
	}
	var g api.SensorGateway
	c.expect(t, 200, "", "PATCH", M+"/sensors/gateways/"+gw.Gateway.Id, map[string]any{"name": "LoRa 2"}, &g, "If-Match", etag(1))
	c.expect(t, 200, "", "POST", M+"/sensors/gateways/"+gw.Gateway.Id+"/rotate-key", nil, nil, "If-Match", etag(2))
	c.expect(t, 200, "", "POST", M+"/sensors/gateways/"+gw.Gateway.Id+"/revoke-key", nil, &g, "If-Match", etag(3))
	if g.KeyStatus != "revoked" {
		t.Fatalf("gateway revoke %+v", g)
	}
	other.expect(t, 404, "not_found", "PATCH", X+"/sensors/gateways/"+gw.Gateway.Id, map[string]any{}, nil, "If-Match", etag(4))

	// Sources (the fake driver is registered in dev).
	var srcs api.SensorSourceList
	c.expect(t, 200, "", "GET", M+"/sensors/sources", nil, &srcs)
	if len(srcs.Drivers) != 1 || srcs.Drivers[0].Code != "fake" || len(srcs.Drivers[0].SecretKeys) != 1 {
		t.Fatalf("drivers %+v", srcs.Drivers)
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/sensors/sources", map[string]any{"code": "acme", "name": "Acme"}, nil)
	var src api.SensorSource
	r = c.expect(t, 201, "", "POST", M+"/sensors/sources", map[string]any{"code": "fake", "name": "Demo", "config": map[string]string{"label": "nord", "token": "s3cr3t-token"}}, &src)
	if !src.ConfigSet || src.ConfigPublic["label"] != "nord" || strings.Contains(string(r.body), "s3cr3t-token") {
		t.Fatalf("source %s", r.body)
	}
	c.expect(t, 200, "", "PATCH", M+"/sensors/sources/"+src.Id, map[string]any{"poll_seconds": 30}, &src, "If-Match", etag(1))
	var poll api.SensorSourcePollResult
	c.expect(t, 200, "", "POST", M+"/sensors/sources/"+src.Id+"/poll", nil, &poll)
	other.expect(t, 404, "not_found", "POST", X+"/sensors/sources/"+src.Id+"/poll", nil, nil)

	// Settings.
	var set api.SensorSettings
	r = dock.expect(t, 200, "", "GET", M+"/sensors/settings", nil, &set)
	if r.header.Get("ETag") != `"0"` || set.HysteresisReadings != 2 || set.RawRetentionDays != 30 {
		t.Fatalf("settings %s", r.body)
	}
	in := map[string]any{
		"hysteresis_readings": 3, "hysteresis_minutes": 5, "offline_after_minutes": 30, "silent_alert_minutes": 180, "battery_low_pct": 20,
		"unexpected_after_minutes": 15, "departure_cutoff_minutes": 720, "presence_effect_enabled": true, "presence_out_after_hours": 6,
		"release_proposal_after_days": 3, "release_proposal_nights": 7,
	}
	dock.expect(t, 403, "forbidden", "PUT", M+"/sensors/settings", in, nil, "If-Match", etag(0))
	c.expect(t, 428, "precondition_required", "PUT", M+"/sensors/settings", in, nil)
	r = c.expect(t, 200, "", "PUT", M+"/sensors/settings", in, &set, "If-Match", etag(0))
	if r.header.Get("ETag") != `"1"` || set.HysteresisReadings != 3 || !set.PresenceEffectEnabled {
		t.Fatalf("put %s", r.body)
	}
	c.expect(t, 412, "version_conflict", "PUT", M+"/sensors/settings", in, nil, "If-Match", etag(0))
	in["battery_low_pct"] = 95
	c.expect(t, 400, "validation_failed", "PUT", M+"/sensors/settings", in, nil, "If-Match", etag(1))
	other.expect(t, 404, "not_found", "GET", M+"/sensors/settings", nil, nil)
}
