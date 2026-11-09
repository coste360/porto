//go:build integration

package admin_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/http/api"
)

// TestSensorWatchHandlers_Scenario: the Sensori map, alerts and their actions, the histories and the sensors report
// through HTTP: statuses, problem types, ETag/If-Match and the cross-marina 404 of every watch route.
func TestSensorWatchHandlers_Scenario(t *testing.T) {
	s := newHarborSrv(t)
	ctx := context.Background()
	o, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "owner@porto.test", Name: "Owner", Password: harborPW, Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-w"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "other@porto.test", Name: "Other", Password: harborPW, Organization: "Org2", MarinaName: "Altra", MarinaSlug: "marina-x"})
	if err != nil {
		t.Fatal(err)
	}
	h := contracttest.InsertHarbour(t, s.app.DB, o.MarinaID, time.Now().UTC(), 3)
	c, other := s.login(t, "owner@porto.test"), s.login(t, "other@porto.test")
	M, X := "/api/admin/v1/marinas/"+o.MarinaID, "/api/admin/v1/marinas/"+x.MarinaID

	var created api.SensorDeviceWithKey
	c.expect(t, 201, "", "POST", M+"/sensors/devices", map[string]any{"external_id": "A1X", "code": "S-A1", "kind": "ultrasonic", "resource_type": "berth", "resource_id": h.Berths[0]}, &created)
	dev := created.Device.Id
	// The berth has been occupied for an hour without an allocation (the state ingest leaves: a change and the row).
	now := time.Now().UTC().Truncate(time.Millisecond)
	since := now.Add(-time.Hour)
	if _, err := s.app.DB.ExecContext(ctx, `INSERT INTO sensor_state_changes (id, marina_id, device_id, resource_type, resource_id,
		map_id, from_state, to_state, at, reason, created_at) VALUES (?, ?, ?, 'berth', ?, ?, 'unknown', 'occupied', ?, 'readings', ?)`,
		"01900000-0000-7000-8000-00000000c001", o.MarinaID, dev, h.Berths[0], h.MapID, since, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.app.DB.ExecContext(ctx, `INSERT INTO sensor_device_states (device_id, marina_id, physical, physical_since,
		candidate_count, last_observed_at, last_contact_at, battery_pct, firmware, updated_at) VALUES (?, ?, 'occupied', ?, 0, ?, ?, 90, '', ?)
		ON CONFLICT (device_id) DO UPDATE SET physical = 'occupied', physical_since = EXCLUDED.physical_since,
		last_observed_at = EXCLUDED.last_observed_at, last_contact_at = EXCLUDED.last_contact_at, battery_pct = 90`,
		dev, o.MarinaID, since, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := s.app.SensorWatch.RunReconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// Map.
	var m api.SensorMap
	c.expect(t, 200, "", "GET", M+"/sensors/map", nil, &m)
	if m.MapId != h.MapID || len(m.Entries) != 1 || !m.Entries[0].Discrepancy || m.Entries[0].Physical != "occupied" ||
		m.Entries[0].Health == nil || len(m.Entries[0].OpenAlerts) != 1 {
		t.Fatalf("map %+v", m)
	}
	yesterday := calendar.In(now, time.UTC).AddDays(-2).String()
	var pm api.SensorMap
	c.expect(t, 200, "", "GET", M+"/sensors/map?map_id="+h.MapID+"&date="+yesterday, nil, &pm)
	if pm.Date == nil || pm.Entries[0].Health != nil {
		t.Fatalf("past map %+v", pm)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/sensors/map?date=nope", nil, nil)
	c.expect(t, 404, "not_found", "GET", M+"/sensors/map?map_id=01900000-0000-7000-8000-0000000000ff", nil, nil)
	other.expect(t, 404, "not_found", "GET", M+"/sensors/map", nil, nil)

	// Alerts.
	var list api.SensorAlertList
	c.expect(t, 200, "", "GET", M+"/sensors/alerts", nil, &list)
	if len(list.Items) != 1 || list.Items[0].Kind != "unexpected_occupied" || list.Items[0].ResourceCode != "A1" || list.Items[0].DeviceCode != "S-A1" {
		t.Fatalf("alerts %+v", list)
	}
	alert := list.Items[0]
	if len(alert.AllowedActions) != 1 || alert.AllowedActions[0] != "dismiss" {
		t.Fatalf("actions %+v", alert.AllowedActions)
	}
	c.expect(t, 200, "", "GET", M+"/sensors/alerts?status=dismissed", nil, &list)
	if len(list.Items) != 0 {
		t.Fatal("status filter")
	}
	var got api.SensorAlert
	r := c.expect(t, 200, "", "GET", M+"/sensors/alerts/"+alert.Id, nil, &got)
	if r.header.Get("ETag") != `"1"` {
		t.Fatalf("etag %q", r.header.Get("ETag"))
	}
	other.expect(t, 404, "not_found", "GET", M+"/sensors/alerts/"+alert.Id, nil, nil)
	other.expect(t, 404, "not_found", "GET", X+"/sensors/alerts/"+alert.Id, nil, nil)
	other.expect(t, 404, "not_found", "POST", X+"/sensors/alerts/"+alert.Id+"/actions", map[string]any{"action": "dismiss"}, nil, "If-Match", etag(1))
	c.expect(t, 428, "precondition_required", "POST", M+"/sensors/alerts/"+alert.Id+"/actions", map[string]any{"action": "dismiss"}, nil)
	c.expect(t, 412, "version_conflict", "POST", M+"/sensors/alerts/"+alert.Id+"/actions", map[string]any{"action": "dismiss"}, nil, "If-Match", etag(7))
	c.expect(t, 422, "alert_action_not_allowed", "POST", M+"/sensors/alerts/"+alert.Id+"/actions", map[string]any{"action": "create_task"}, nil, "If-Match", etag(1))
	r = c.expect(t, 200, "", "POST", M+"/sensors/alerts/"+alert.Id+"/actions", map[string]any{"action": "dismiss", "note": "barca del cantiere"}, &got, "If-Match", etag(1))
	if got.Status != "dismissed" || r.header.Get("ETag") != `"2"` || got.Note == nil {
		t.Fatalf("dismiss %s", r.body)
	}

	// History.
	c.expect(t, 400, "validation_failed", "GET", M+"/sensors/history", nil, nil)
	other.expect(t, 404, "not_found", "GET", M+"/sensors/history", nil, nil)
	var hist api.SensorHistory
	c.expect(t, 200, "", "GET", M+"/sensors/history?resource_type=berth&resource_id="+h.Berths[0]+"&include_readings=true", nil, &hist)
	if len(hist.Changes) != 1 || len(hist.Nights) != 8 || len(hist.Alerts) != 1 || hist.Readings == nil {
		t.Fatalf("history %+v", hist)
	}
	c.expect(t, 404, "not_found", "GET", M+"/sensors/history?resource_type=berth&resource_id=01900000-0000-7000-8000-0000000000fe", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/sensors/history?resource_type=berth&resource_id="+h.Berths[0]+"&from=2027-01-01&to=2027-03-01", nil, nil)
	c.expect(t, 200, "", "GET", M+"/sensors/devices/"+dev+"/history", nil, &hist)
	if hist.DeviceIds == nil || len(*hist.DeviceIds) != 1 {
		t.Fatalf("device history %+v", hist)
	}
	other.expect(t, 404, "not_found", "GET", M+"/sensors/devices/"+dev+"/history", nil, nil)
	other.expect(t, 404, "not_found", "GET", X+"/sensors/devices/"+dev+"/history", nil, nil)

	// Report.
	var rep api.SensorsReport
	c.expect(t, 200, "", "GET", M+"/reports/sensors", nil, &rep)
	if rep.Report != "sensors" || rep.View != "discrepancies" || rep.Totals.NightsCompared == nil {
		t.Fatalf("report %+v", rep)
	}
	c.expect(t, 422, "invalid_report_filter", "GET", M+"/reports/sensors?kind=nope", nil, nil)
	other.expect(t, 404, "not_found", "GET", M+"/reports/sensors?kind=nope", nil, nil)
	r = c.expect(t, 200, "", "GET", M+"/reports/sensors?format=csv&group_by=kind", nil, nil)
	if !strings.Contains(r.header.Get("Content-Type"), "csv") {
		t.Fatalf("csv %s", r.header.Get("Content-Type"))
	}
	// Event payloads never carry the note (INV-70).
	var payloads []string
	if err := s.app.DB.NewSelect().Table("outbox_events").Column("payload").Where("type = 'sensor.anomaly'").Scan(ctx, &payloads); err != nil {
		t.Fatal(err)
	}
	for _, p := range payloads {
		if strings.Contains(p, "cantiere") {
			t.Fatal("note in an event")
		}
	}
}
