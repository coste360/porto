//go:build integration

package sensor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestDevices_CreateAndValidate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "A81758FFFE0312AB", 0)
		id, ok := ingestauth.KeyIDOf(d.Key)
		if !ok || !strings.HasPrefix(d.Key, "po_sns_") || len(d.Key) != 67 || d.Device.KeyID != id {
			t.Fatalf("key %q id %q", d.Key, d.Device.KeyID)
		}
		if d.Device.KeyHash != ingestauth.HashKey(d.Key) || d.Device.KeyStatus != KeyActive {
			t.Fatal("hash or status")
		}
		if plain, err := e.Keys.DecryptString(d.Device.KeyVerifierEnc, "sensor-key:"+d.Device.ID); err != nil || plain != d.Key {
			t.Fatal("verifier")
		}
		if d.Device.XCm != 1000 || d.Device.YCm != 1000 || d.Device.MapID != e.harbour.MapID || d.Device.ResourceCode != "A1" {
			t.Fatalf("placement %+v", d.Device.SensorDevice)
		}
		if d.Device.Health != HealthOK || d.Device.State == nil || d.Device.State.Physical != store.SensorUnknown {
			t.Fatalf("health %s state %+v", d.Device.Health, d.Device.State)
		}
		if e.AuditCount(t, "sensor_device.create") != 1 || e.AuditCount(t, "sensor_device.key_issue") != 1 || len(e.Events(t, event.SensorChanged)) != 1 {
			t.Fatal("audit or event")
		}
		in := func(mod func(*DeviceInput)) error {
			i := DeviceInput{ExternalID: "X1", Code: "X1", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[1]}
			mod(&i)
			_, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, i)
			return err
		}
		cases := []struct {
			name string
			mod  func(*DeviceInput)
			want error
			fld  string
		}{
			{"code taken", func(i *DeviceInput) { i.Code = "S-A1" }, ErrCodeTaken, ""},
			{"external id taken", func(i *DeviceInput) { i.ExternalID = "A81758FFFE0312AB" }, ErrCodeTaken, ""},
			{"berth has a sensor", func(i *DeviceInput) { i.ResourceID = e.harbour.Berths[0] }, ErrResourceHasSensor, ""},
			{"bad code", func(i *DeviceInput) { i.Code = "lower" }, auth.ErrInvalidInput, "code"},
			{"bad external id", func(i *DeviceInput) { i.ExternalID = "a b" }, auth.ErrInvalidInput, "external_id"},
			{"bad kind", func(i *DeviceInput) { i.Kind = "laser" }, auth.ErrInvalidInput, "kind"},
			{"both bindings", func(i *DeviceInput) { i.GatewayID, i.SourceID = store.NewID(), store.NewID() }, auth.ErrInvalidInput, "source_id"},
			{"unknown gateway", func(i *DeviceInput) { i.GatewayID = store.NewID() }, auth.ErrInvalidInput, "gateway_id"},
			{"unknown berth", func(i *DeviceInput) { i.ResourceID = store.NewID() }, ErrInvalidResource, ""},
			{"segment on a berth", func(i *DeviceInput) { i.FromCm, i.ToCm = i64(0), i64(200) }, ErrInvalidResource, ""},
			{"socket on a berth", func(i *DeviceInput) { i.PedestalSocket = "S1" }, ErrInvalidResource, ""},
			{"area without segment", func(i *DeviceInput) { i.ResourceType, i.ResourceID = store.SensorResourceDynamicArea, e.harbour.Area }, ErrInvalidResource, ""},
			{"segment too short", func(i *DeviceInput) {
				i.ResourceType, i.ResourceID, i.FromCm, i.ToCm = store.SensorResourceDynamicArea, e.harbour.Area, i64(0), i64(99)
			}, ErrInvalidResource, ""},
			{"segment past the area", func(i *DeviceInput) {
				i.ResourceType, i.ResourceID, i.FromCm, i.ToCm = store.SensorResourceDynamicArea, e.harbour.Area, i64(5900), i64(6001)
			}, ErrInvalidResource, ""},
			{"unknown socket", func(i *DeviceInput) {
				i.ResourceType, i.ResourceID, i.PedestalSocket = store.SensorResourcePedestal, e.pedestal, "S9"
			}, ErrInvalidResource, ""},
			{"position outside the map", func(i *DeviceInput) { i.XCm = i64(-1) }, auth.ErrInvalidInput, "x_cm"},
		}
		for _, c := range cases {
			err := in(c.mod)
			if !errors.Is(err, c.want) || (c.fld != "" && !isField(err, c.fld)) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// Segments: midpoint on the structure axis, overlap refused, adjacency allowed.
		seg, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "SEG1", Code: "L1-A", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceDynamicArea,
			ResourceID: e.harbour.Area, FromCm: i64(0), ToCm: i64(1000),
		})
		if err != nil {
			t.Fatal(err)
		}
		if seg.Device.XCm != 1500 || seg.Device.YCm != 3000 || seg.Device.ResourceCode != "L1" {
			t.Fatalf("segment position %d,%d", seg.Device.XCm, seg.Device.YCm)
		}
		if err := in(func(i *DeviceInput) {
			i.ResourceType, i.ResourceID, i.FromCm, i.ToCm = store.SensorResourceDynamicArea, e.harbour.Area, i64(900), i64(1500)
		}); !errors.Is(err, ErrSegmentOverlap) {
			t.Fatalf("overlap: %v", err)
		}
		if err := in(func(i *DeviceInput) {
			i.ResourceType, i.ResourceID, i.FromCm, i.ToCm = store.SensorResourceDynamicArea, e.harbour.Area, i64(1000), i64(1500)
		}); err != nil {
			t.Fatalf("adjacent segment: %v", err)
		}
		// Pedestal: S1 maps to one berth, no socket on a pedestal with two berths is informational.
		ps, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "PED1", Code: "C1-S1", Kind: store.SensorKindMagnetometer, ResourceType: store.SensorResourcePedestal,
			ResourceID: e.pedestal, PedestalSocket: "S1",
		})
		if err != nil || ps.Device.Informational || ps.Device.XCm != 1200 {
			t.Fatalf("pedestal socket: %v %+v", err, ps)
		}
		pn, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "PED2", Code: "C1", Kind: store.SensorKindMagnetometer, ResourceType: store.SensorResourcePedestal, ResourceID: e.pedestal,
		})
		if err != nil || !pn.Device.Informational {
			t.Fatalf("pedestal without socket: %v %+v", err, pn)
		}
		if _, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "PED3", Code: "C1-S1B", Kind: store.SensorKindMagnetometer, ResourceType: store.SensorResourcePedestal,
			ResourceID: e.pedestal, PedestalSocket: "S1",
		}); !errors.Is(err, ErrResourceHasSensor) {
			t.Fatalf("second device on a socket: %v", err)
		}
		// Lists, filters, cursor.
		page, err := e.s.ListDevices(ctx, e.Owner, e.MarinaID, DeviceFilter{}, "", 2)
		if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
			t.Fatalf("page 1: %v %d", err, len(page.Items))
		}
		all := map[string]bool{}
		for cur := ""; ; {
			p, err := e.s.ListDevices(ctx, e.Owner, e.MarinaID, DeviceFilter{}, cur, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range p.Items {
				all[it.Code] = true
			}
			if p.NextCursor == "" {
				break
			}
			cur = p.NextCursor
		}
		if len(all) != 5 {
			t.Fatalf("listed %v", all)
		}
		f, err := e.s.ListDevices(ctx, e.Owner, e.MarinaID, DeviceFilter{ResourceType: store.SensorResourceDynamicArea}, "", 0)
		if err != nil || len(f.Items) != 2 {
			t.Fatalf("filter: %v %d", err, len(f.Items))
		}
		checkInvariants(t, db)
	})
}

func TestDevices_UpdateMoveAndPermissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, "S-A1", "DEV1", 0)
		now := e.Clock.Now()
		e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-2 * time.Minute)), State: "occupied"},
			reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-time.Minute)), State: "occupied"})
		if st := e.state(t, d.Device.ID); st.Physical != store.SensorOccupied {
			t.Fatalf("physical %s", st.Physical)
		}
		if _, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, 7, DevicePatch{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		model := "UX-2"
		v, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, 1, DevicePatch{Model: &model})
		if err != nil || v.Model != "UX-2" || v.Version != 2 || len(e.changes(t, d.Device.ID)) != 1 {
			t.Fatalf("update: %v", err)
		}
		// Move to A2: device_moved on the old resource, hysteresis reset.
		b2 := e.harbour.Berths[1]
		v, err = e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, 2, DevicePatch{ResourceID: &b2})
		if err != nil {
			t.Fatal(err)
		}
		if v.ResourceID != b2 || v.XCm != 1500 || v.ResourceCode != "A2" {
			t.Fatalf("moved %+v", v.SensorDevice)
		}
		cs := e.changes(t, d.Device.ID)
		if len(cs) != 2 || cs[1].Reason != store.SensorReasonDeviceMoved || cs[1].ResourceID != e.harbour.Berths[0] || cs[1].ToState != store.SensorUnknown {
			t.Fatalf("changes %+v", cs)
		}
		if st := e.state(t, d.Device.ID); st.Physical != store.SensorUnknown || st.Candidate != "" {
			t.Fatalf("state %+v", st)
		}
		// A late reading from before the move is stored but ignored.
		res := e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now.Add(-30 * time.Second)), State: "free"})
		if res.Accepted != 1 || e.state(t, d.Device.ID).Candidate != "" {
			t.Fatal("late reading changed the state")
		}
		// Deactivate: event deactivated, readings refused per item.
		off := false
		if _, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, d.Device.ID, 3, DevicePatch{Active: &off}); err != nil {
			t.Fatal(err)
		}
		r := e.post(t, d.Key, reading{DeviceID: "DEV1", ObservedAt: ts(now), State: "free"})
		if r.Accepted != 0 || len(r.Rejected) != 1 || r.Rejected[0].Code != ItemDeviceInactive {
			t.Fatalf("inactive %+v", r)
		}
		// Permissions: a viewer reads, cannot write; another marina is 404.
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.s.GetDevice(ctx, viewer, e.MarinaID, d.Device.ID); err != nil {
			t.Fatalf("viewer read: %v", err)
		}
		if _, err := e.s.CreateDevice(ctx, viewer, e.MarinaID, DeviceInput{}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer write: %v", err)
		}
		_, otherMarina, other := e.OtherOrganization(t)
		if _, err := e.s.GetDevice(ctx, other, otherMarina, d.Device.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
		if _, err := e.s.RotateDeviceKey(ctx, other, otherMarina, d.Device.ID, 4, 24); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina rotate: %v", err)
		}
		checkInvariants(t, db)
	})
}

func TestGatewaysSourcesSettings(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "LoRa nord", Kind: store.SensorGatewayLoRaWAN})
		if err != nil || !strings.HasPrefix(g.Key, "po_gw_") || len(g.Key) != 66 || g.Gateway.KeyStatus != KeyActive {
			t.Fatalf("gateway: %v %+v", err, g)
		}
		if _, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW1", Name: "x", Kind: store.SensorGatewayOther}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("code taken: %v", err)
		}
		dev, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "GWD1", Code: "G1", Kind: store.SensorKindRadar, ResourceType: store.SensorResourceBerth,
			ResourceID: e.harbour.Berths[2], GatewayID: g.Gateway.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		list, err := e.s.ListGateways(ctx, e.Owner, e.MarinaID)
		if err != nil || len(list) != 1 || list[0].DeviceCount != 1 {
			t.Fatalf("list %v %+v", err, list)
		}
		name := "LoRa sud"
		gv, err := e.s.UpdateGateway(ctx, e.Owner, e.MarinaID, g.Gateway.ID, 1, GatewayPatch{Name: &name})
		if err != nil || gv.Name != name || gv.Version != 2 {
			t.Fatalf("update %v", err)
		}
		rot, err := e.s.RotateGatewayKey(ctx, e.Owner, e.MarinaID, g.Gateway.ID, 2, 1)
		if err != nil || rot.Gateway.KeyStatus != KeyRotating || rot.Gateway.PrevKeyID != g.Gateway.KeyID {
			t.Fatalf("rotate %v %+v", err, rot)
		}
		if _, err := e.s.RotateGatewayKey(ctx, e.Owner, e.MarinaID, g.Gateway.ID, 3, 169); !isField(err, "grace_hours") {
			t.Fatalf("grace range: %v", err)
		}
		rv, err := e.s.RevokeGatewayKey(ctx, e.Owner, e.MarinaID, g.Gateway.ID, 3)
		if err != nil || rv.KeyStatus != KeyRevoked || rv.KeyID != "" || rv.PrevKeyID != "" {
			t.Fatalf("revoke %v", err)
		}
		// Binding patch: unbind from the gateway.
		if _, err := e.s.UpdateDevice(ctx, e.Owner, e.MarinaID, dev.Device.ID, 1, DevicePatch{Binding: &Binding{}}); err != nil {
			t.Fatal(err)
		}
		// Sources: only registered drivers, secrets never returned.
		if _, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "nope", Name: "x"}); !isField(err, "code") {
			t.Fatalf("unknown driver: %v", err)
		}
		if _, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "fake", Name: "x", Config: map[string]string{"bogus": "1"}}); !isField(err, "config.bogus") {
			t.Fatalf("unknown key: %v", err)
		}
		src, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{
			Code: "fake", Name: "Vendor", Config: map[string]string{"label": "nord", "token": "tok-secret-123"},
		})
		if err != nil || !src.ConfigSet || src.ConfigPublic["label"] != "nord" || src.ConfigPublic["token"] != "" || src.ConfigEnc != "" || src.PollSeconds != 60 {
			t.Fatalf("source %v %+v", err, src)
		}
		if _, err := e.s.CreateSource(ctx, e.Owner, e.MarinaID, SourceInput{Code: "fake", Name: "Vendor"}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("name taken: %v", err)
		}
		ps := 10
		if _, err := e.s.UpdateSource(ctx, e.Owner, e.MarinaID, src.ID, 1, SourcePatch{PollSeconds: &ps}); !isField(err, "poll_seconds") {
			t.Fatalf("poll range: %v", err)
		}
		srcs, err := e.s.ListSources(ctx, e.Owner, e.MarinaID)
		if err != nil || len(srcs) != 1 {
			t.Fatal(err)
		}
		if contains(e.logs.String(), "tok-secret-123") {
			t.Fatal("source secret logged")
		}
		var rows []store.AuditEntry
		if err := db.NewSelect().Model(&rows).Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if strings.Contains(string(r.Diff), "tok-secret-123") {
				t.Fatal("source secret audited")
			}
		}
		// Settings: defaults (version 0), first save with 0, then version 1.
		set, err := e.s.GetSettings(ctx, e.Owner, e.MarinaID)
		if err != nil || set.Version != 0 || set.HysteresisReadings != 2 || set.RawRetentionDays != 30 {
			t.Fatalf("defaults %v %+v", err, set)
		}
		in := SettingsInput{
			HysteresisReadings: 3, HysteresisMinutes: 4, OfflineAfterMinutes: 20, SilentAlertMinutes: 60, BatteryLowPct: 15,
			UnexpectedAfterMinutes: 10, DepartureCutoffMinutes: 600, PresenceEffectEnabled: true, PresenceOutAfterHours: 5,
			ReleaseProposalAfterDays: 2, ReleaseProposalNights: 5,
		}
		bad := in
		bad.SilentAlertMinutes = 10
		if _, err := e.s.PutSettings(ctx, e.Owner, e.MarinaID, 0, bad); !isField(err, "silent_alert_minutes") {
			t.Fatalf("validation: %v", err)
		}
		if _, err := e.s.PutSettings(ctx, e.Owner, e.MarinaID, 1, in); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale first save: %v", err)
		}
		saved, err := e.s.PutSettings(ctx, e.Owner, e.MarinaID, 0, in)
		if err != nil || saved.Version != 1 || !saved.PresenceEffectEnabled {
			t.Fatalf("save %v", err)
		}
		in.HysteresisReadings = 2
		saved, err = e.s.PutSettings(ctx, e.Owner, e.MarinaID, 1, in)
		if err != nil || saved.Version != 2 {
			t.Fatalf("second save %v", err)
		}
		if len(e.Events(t, event.SensorChanged)) < 8 {
			t.Fatalf("events %d", len(e.Events(t, event.SensorChanged)))
		}
	})
}
