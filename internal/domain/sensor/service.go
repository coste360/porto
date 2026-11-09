// Package sensor is the M12 occupancy sensor service (spec 24.2-24.3; m12-plan §3-§7): devices on berths,
// linear dynamic-area segments and pedestals, gateways and polled sources with their keys, settings, the ingestion
// of signed readings (Ingest) and the jobs that poll sources, mark silent devices unknown and purge raw readings.
//
// Sensors never write allocations, occupancy rows, releases, contracts or charges (INV-100): the reconciliation in
// sensor/watch turns physical states into alerts and proposals. Ingestion resolves the marina from the
// authenticated key, never from the payload (INV-102). Keys are random, shown once, stored as a SHA-256 hash and a
// KeyRing-encrypted verifier, compared in constant time (INV-105). Raw readings are neither audited nor evented; a
// confirmed state change writes sensor_state_changes and sensor.state_changed in the ingest transaction.
package sensor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Service is the sensor service.
type Service struct {
	d       Deps
	metrics *Metrics
	// polling holds the ids of the sources being polled (the job and pollSensorSourceNow never poll one source
	// twice at once: ErrSourceBusy).
	polling sync.Map
	// egress is the anti-SSRF client handed to the polled sources.
	egress ext.HTTPDoer
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.RawRetention <= 0 {
		d.RawRetention = 30 * 24 * time.Hour
	}
	egress := d.HTTP
	if egress == nil {
		egress = event.NewEgressClient(event.EgressPolicy{}, pollTimeout)
	}
	return &Service{d: d, metrics: &Metrics{}, egress: egress}
}

// Metrics returns the write-path counters (m12-plan §14.2 L6).
func (s *Service) Metrics() *Metrics { return s.metrics }

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// authorize checks perm on the marina (another marina is auth.ErrNotFound) and, for each (table, id) pair of refs,
// that the row exists in the marina, so another marina's ids are 404 before anything else.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission, refs ...string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	if err != nil {
		return scope, err
	}
	for i := 0; i+1 < len(refs); i += 2 {
		if err := store.SensorExists(ctx, s.d.DB.DB, scope, refs[i], refs[i+1]); err != nil {
			return scope, notFound(err)
		}
	}
	return scope, nil
}

// Drivers returns the registered polled sources and their configuration keys.
func (s *Service) Drivers() []Driver {
	out := make([]Driver, 0, len(s.d.Sources))
	for _, src := range s.d.Sources {
		pub, sec := src.ConfigKeys()
		out = append(out, Driver{Code: src.Code(), PublicKeys: pub, Secret: sec})
	}
	return out
}

func (s *Service) loadViewCtx(ctx context.Context, db bun.IDB, scope store.MarinaScope) (viewCtx, error) {
	set, err := store.LoadSensorSettings(ctx, db, scope)
	if err != nil {
		return viewCtx{}, err
	}
	codes, err := store.SensorResourceCodes(ctx, db, scope)
	if err != nil {
		return viewCtx{}, err
	}
	peds, err := store.PedestalBerthCounts(ctx, db, scope)
	if err != nil {
		return viewCtx{}, err
	}
	return viewCtx{settings: set, codes: codes, pedestals: peds, now: s.d.Clock.Now()}, nil
}

func (s *Service) oneDeviceView(ctx context.Context, db bun.IDB, scope store.MarinaScope, d *store.SensorDevice) (*DeviceView, error) {
	vc, err := s.loadViewCtx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	st, err := store.GetSensorDeviceState(ctx, db, scope, d.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	v := s.deviceView(d, st, vc)
	return &v, nil
}

// ---- devices (map.view to read, harbor.configure to write) ----

// ListDevices lists the devices of the marina by code with their state and health.
func (s *Service) ListDevices(ctx context.Context, p *auth.Principal, marinaID string, f DeviceFilter, cursor string, limit int) (store.Page[DeviceView], error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView)
	if err != nil {
		return store.Page[DeviceView]{}, err
	}
	after, err := store.DecodeCursor(cursor)
	if err != nil {
		return store.Page[DeviceView]{}, err
	}
	limit = store.ClampLimit(limit)
	db := s.d.DB.DB
	rows, err := store.ListSensorDevicesOf(ctx, db, scope, store.SensorDeviceFilter{
		MapID: f.MapID, ResourceType: f.ResourceType, ResourceID: f.ResourceID, Active: f.Active,
	})
	if err != nil {
		return store.Page[DeviceView]{}, err
	}
	states, err := store.SensorStatesOf(ctx, db, scope)
	if err != nil {
		return store.Page[DeviceView]{}, err
	}
	vc, err := s.loadViewCtx(ctx, db, scope)
	if err != nil {
		return store.Page[DeviceView]{}, err
	}
	start := 0
	if after != "" {
		start = len(rows)
		for i := range rows {
			if rows[i].ID == after {
				start = i + 1
				break
			}
		}
	}
	page := store.Page[DeviceView]{Items: []DeviceView{}}
	for i := start; i < len(rows); i++ {
		v := s.deviceView(&rows[i], states[rows[i].ID], vc)
		if f.Health != "" && v.Health != f.Health {
			continue
		}
		if len(page.Items) == limit {
			page.NextCursor = store.EncodeCursor(page.Items[limit-1].ID)
			break
		}
		page.Items = append(page.Items, v)
	}
	return page, nil
}

// GetDevice returns a device.
func (s *Service) GetDevice(ctx context.Context, p *auth.Principal, marinaID, deviceID string) (*DeviceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView, store.SensorTableDevices, deviceID)
	if err != nil {
		return nil, err
	}
	d, err := store.GetSensorDevice(ctx, s.d.DB.DB, scope, deviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return s.oneDeviceView(ctx, s.d.DB.DB, scope, d)
}

// CreateDevice registers a device and issues its key (returned once).
func (s *Service) CreateDevice(ctx context.Context, p *auth.Principal, marinaID string, in DeviceInput) (*DeviceWithKey, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	in.ExternalID, in.Code = strings.TrimSpace(in.ExternalID), strings.TrimSpace(in.Code)
	if !reExternalID.MatchString(in.ExternalID) {
		v.Add("external_id", "invalid")
	}
	validateDeviceFields(v, in.Code, in.Kind, in.Model, in.Firmware)
	if in.GatewayID != "" && in.SourceID != "" {
		v.Add("source_id", "conflict")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *DeviceWithKey
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d := &store.SensorDevice{
			ID: store.NewID(), MarinaID: scope.ID(), ExternalID: in.ExternalID, Code: in.Code, Kind: in.Kind, Model: in.Model,
			Firmware: in.Firmware, GatewayID: in.GatewayID, SourceID: in.SourceID, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.placeDevice(ctx, tx, scope, d, placement{
			ResourceType: in.ResourceType, ResourceID: in.ResourceID, FromCm: in.FromCm, ToCm: in.ToCm, Socket: in.PedestalSocket,
			XCm: in.XCm, YCm: in.YCm,
		}); err != nil {
			return err
		}
		if err := s.checkBinding(ctx, tx, scope, d.GatewayID, d.SourceID); err != nil {
			return err
		}
		if taken, err := store.SensorDeviceCodeTaken(ctx, tx, scope, d.ID, d.Code, d.ExternalID); err != nil || taken {
			if err != nil {
				return err
			}
			return ErrCodeTaken
		}
		k, err := s.issueKey(ingestauth.DevicePrefix, aadDevice+d.ID)
		if err != nil {
			return err
		}
		deviceKeyCols(d).set(k, now)
		if err := store.InsertSensorDevice(ctx, tx, d); err != nil {
			return mapDeviceUnique(err)
		}
		if err := store.EnsureSensorDeviceStates(ctx, tx, scope.ID(), []string{d.ID}, now); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_device.create", "sensor_device", d.ID, deviceDiff(d)); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_device.key_issue", "sensor_device", d.ID, map[string]any{"key_id": d.KeyID}); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{DeviceID: d.ID, MapID: d.MapID, Change: "created"}); err != nil {
			return err
		}
		view, err := s.oneDeviceView(ctx, tx, scope, d)
		if err != nil {
			return err
		}
		out = &DeviceWithKey{Device: *view, Key: k.plain}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateDevice updates a device (If-Match). A new resource or segment writes a state change to unknown
// (device_moved) and resets the hysteresis.
func (s *Service) UpdateDevice(ctx context.Context, p *auth.Principal, marinaID, deviceID string, ifMatch int, in DevicePatch) (*DeviceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableDevices, deviceID)
	if err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *DeviceView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d, err := store.LockSensorDevice(ctx, tx, scope, deviceID)
		if err != nil {
			return notFound(err)
		}
		if d.Version != ifMatch {
			return ErrVersionMismatch
		}
		// Always lock the state row too (device row, then state row): an ingest holding it commits first, and one
		// waiting for it re-reads the device after this commit (lock then re-check, INV-102).
		if err := store.EnsureSensorDeviceStates(ctx, tx, scope.ID(), []string{d.ID}, now); err != nil {
			return err
		}
		if _, err := store.LockSensorDeviceStates(ctx, tx, scope.ID(), []string{d.ID}); err != nil {
			return err
		}
		old := *d
		code, kind, model, firmware := d.Code, d.Kind, d.Model, d.Firmware
		if in.Code != nil {
			code = strings.TrimSpace(*in.Code)
		}
		if in.Kind != nil {
			kind = *in.Kind
		}
		if in.Model != nil {
			model = *in.Model
		}
		if in.Firmware != nil {
			firmware = *in.Firmware
		}
		v := &auth.ValidationError{}
		validateDeviceFields(v, code, kind, model, firmware)
		if in.Binding != nil && in.Binding.GatewayID != "" && in.Binding.SourceID != "" {
			v.Add("binding", "conflict")
		}
		if err := v.Err(); err != nil {
			return err
		}
		d.Code, d.Kind, d.Model, d.Firmware = code, kind, model, firmware
		if code != old.Code {
			if taken, err := store.SensorDeviceCodeTaken(ctx, tx, scope, d.ID, d.Code, d.ExternalID); err != nil || taken {
				if err != nil {
					return err
				}
				return ErrCodeTaken
			}
		}
		pl := placement{ResourceType: d.ResourceType, ResourceID: d.ResourceID, FromCm: d.FromCm, ToCm: d.ToCm, Socket: d.PedestalSocket, XCm: in.XCm, YCm: in.YCm}
		resourceChanged := false
		if in.ResourceType != nil && *in.ResourceType != d.ResourceType {
			pl.ResourceType, resourceChanged = *in.ResourceType, true
			pl.FromCm, pl.ToCm, pl.Socket = nil, nil, ""
		}
		if in.ResourceID != nil && *in.ResourceID != d.ResourceID {
			pl.ResourceID, resourceChanged = *in.ResourceID, true
		}
		if in.FromCm != nil {
			pl.FromCm = in.FromCm
		}
		if in.ToCm != nil {
			pl.ToCm = in.ToCm
		}
		if in.PedestalSocket != nil {
			pl.Socket = strings.TrimSpace(*in.PedestalSocket)
		}
		moved := resourceChanged || !eqPtr(pl.FromCm, d.FromCm) || !eqPtr(pl.ToCm, d.ToCm) || pl.Socket != d.PedestalSocket
		placed := in.XCm != nil || in.YCm != nil
		if placed && !moved {
			// A new position on the same resource: the coordinate not given keeps its value.
			if pl.XCm == nil {
				pl.XCm = &d.XCm
			}
			if pl.YCm == nil {
				pl.YCm = &d.YCm
			}
		}
		active := d.Active
		if in.Active != nil {
			active = *in.Active
		}
		reactivated := active && !old.Active
		d.Active = active
		if moved || placed || reactivated {
			if !moved && !placed {
				pl.XCm, pl.YCm = &d.XCm, &d.YCm
			}
			pl.skipActiveCheck = !active
			if err := s.placeDevice(ctx, tx, scope, d, pl); err != nil {
				return err
			}
		}
		if in.Binding != nil {
			d.GatewayID, d.SourceID = in.Binding.GatewayID, in.Binding.SourceID
			if err := s.checkBinding(ctx, tx, scope, d.GatewayID, d.SourceID); err != nil {
				return err
			}
		}
		d.UpdatedAt = now
		if err := store.UpdateSensorDevice(ctx, tx, scope, d, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return mapDeviceUnique(err)
		}
		if moved {
			if err := s.resetMoved(ctx, tx, scope, &old, now); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_device.update", "sensor_device", d.ID, deviceDiff(d)); err != nil {
			return err
		}
		change := "updated"
		if old.Active && !d.Active {
			change = "deactivated"
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{DeviceID: d.ID, MapID: d.MapID, Change: change}); err != nil {
			return err
		}
		out, err = s.oneDeviceView(ctx, tx, scope, d)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resetMoved writes the device_moved change of a reassigned device on its old resource and resets the
// hysteresis (m12-plan §3.1, H15). The caller holds the device row lock.
func (s *Service) resetMoved(ctx context.Context, tx bun.Tx, scope store.MarinaScope, old *store.SensorDevice, now time.Time) error {
	if err := store.EnsureSensorDeviceStates(ctx, tx, scope.ID(), []string{old.ID}, now); err != nil {
		return err
	}
	states, err := store.LockSensorDeviceStates(ctx, tx, scope.ID(), []string{old.ID})
	if err != nil {
		return err
	}
	st := states[old.ID]
	if st == nil {
		return fmt.Errorf("sensor: state row of %s missing", old.ID)
	}
	hs, ch := hysteresis.Moved(toHysteresis(st), now)
	fromHysteresis(st, hs)
	st.UpdatedAt = now
	if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
		return err
	}
	if ch != nil {
		return s.writeChange(ctx, tx, scope, old, *ch, now)
	}
	return nil
}

// RotateDeviceKey issues a new key (returned once); the previous one verifies for graceHours (0..168).
func (s *Service) RotateDeviceKey(ctx context.Context, p *auth.Principal, marinaID, deviceID string, ifMatch, graceHours int) (*DeviceWithKey, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableDevices, deviceID)
	if err != nil {
		return nil, err
	}
	if graceHours < 0 || graceHours > 168 {
		v := &auth.ValidationError{}
		v.Add("grace_hours", "out_of_range")
		return nil, v
	}
	now := store.UTC(s.d.Clock.Now())
	var out *DeviceWithKey
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d, err := store.LockSensorDevice(ctx, tx, scope, deviceID)
		if err != nil {
			return notFound(err)
		}
		if d.Version != ifMatch {
			return ErrVersionMismatch
		}
		k, err := s.issueKey(ingestauth.DevicePrefix, aadDevice+d.ID)
		if err != nil {
			return err
		}
		cols := deviceKeyCols(d)
		// An unusable current key (restored elsewhere) cannot verify: no grace for it.
		grace := time.Duration(graceHours) * time.Hour
		if s.keyStatus(cols, aadDevice+d.ID, now) == KeyUnusable {
			grace = 0
		}
		cols.rotate(k, now, grace)
		d.UpdatedAt = now
		if err := store.UpdateSensorDevice(ctx, tx, scope, d, ifMatch); err != nil {
			return mapVersion(err)
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_device.key_rotate", "sensor_device", d.ID, map[string]any{
			"key_id": d.KeyID, "prev_key_id": d.PrevKeyID, "grace_hours": graceHours,
		}); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{DeviceID: d.ID, MapID: d.MapID, Change: "key_rotated"}); err != nil {
			return err
		}
		view, err := s.oneDeviceView(ctx, tx, scope, d)
		if err != nil {
			return err
		}
		out = &DeviceWithKey{Device: *view, Key: k.plain}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeDeviceKey clears the current and previous key of a device.
func (s *Service) RevokeDeviceKey(ctx context.Context, p *auth.Principal, marinaID, deviceID string, ifMatch int) (*DeviceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableDevices, deviceID)
	if err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *DeviceView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d, err := store.LockSensorDevice(ctx, tx, scope, deviceID)
		if err != nil {
			return notFound(err)
		}
		if d.Version != ifMatch {
			return ErrVersionMismatch
		}
		deviceKeyCols(d).revoke()
		d.UpdatedAt = now
		if err := store.UpdateSensorDevice(ctx, tx, scope, d, ifMatch); err != nil {
			return mapVersion(err)
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_device.key_revoke", "sensor_device", d.ID, nil); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{DeviceID: d.ID, MapID: d.MapID, Change: "key_revoked"}); err != nil {
			return err
		}
		out, err = s.oneDeviceView(ctx, tx, scope, d)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- gateways (harbor.configure) ----

func (s *Service) gatewayView(g *store.SensorGateway, counts map[string]int, now time.Time) GatewayView {
	return GatewayView{SensorGateway: *g, KeyStatus: s.keyStatus(gatewayKeyCols(g), aadGateway+g.ID, now), DeviceCount: counts[g.ID]}
}

func (s *Service) oneGatewayView(ctx context.Context, db bun.IDB, scope store.MarinaScope, g *store.SensorGateway) (*GatewayView, error) {
	counts, err := store.SensorDeviceCounts(ctx, db, scope, "gateway_id")
	if err != nil {
		return nil, err
	}
	v := s.gatewayView(g, counts, s.d.Clock.Now())
	return &v, nil
}

// ListGateways lists the gateways of the marina by code.
func (s *Service) ListGateways(ctx context.Context, p *auth.Principal, marinaID string) ([]GatewayView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListSensorGateways(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	counts, err := store.SensorDeviceCounts(ctx, s.d.DB.DB, scope, "gateway_id")
	if err != nil {
		return nil, err
	}
	now := s.d.Clock.Now()
	out := make([]GatewayView, 0, len(rows))
	for i := range rows {
		out = append(out, s.gatewayView(&rows[i], counts, now))
	}
	return out, nil
}

func validateGateway(v *auth.ValidationError, code, name, kind string, checkCode bool) {
	if checkCode && !reCode.MatchString(code) {
		v.Add("code", "invalid")
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > 80 {
		v.Add("name", "invalid")
	}
	if !slices.Contains(gatewayKinds, kind) {
		v.Add("kind", "invalid")
	}
}

// CreateGateway registers a gateway and issues its key (returned once).
func (s *Service) CreateGateway(ctx context.Context, p *auth.Principal, marinaID string, in GatewayInput) (*GatewayWithKey, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	v := &auth.ValidationError{}
	validateGateway(v, in.Code, in.Name, in.Kind, true)
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *GatewayWithKey
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		g := &store.SensorGateway{
			ID: store.NewID(), MarinaID: scope.ID(), Code: in.Code, Name: in.Name, Kind: in.Kind, Active: true, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		k, err := s.issueKey(ingestauth.GatewayPrefix, aadGateway+g.ID)
		if err != nil {
			return err
		}
		gatewayKeyCols(g).set(k, now)
		if err := store.InsertSensorGateway(ctx, tx, g); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_gateway.create", "sensor_gateway", g.ID, map[string]any{
			"code": g.Code, "kind": g.Kind, "key_id": g.KeyID,
		}); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{GatewayID: g.ID, Change: "created"}); err != nil {
			return err
		}
		view, err := s.oneGatewayView(ctx, tx, scope, g)
		if err != nil {
			return err
		}
		out = &GatewayWithKey{Gateway: *view, Key: k.plain}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// gatewayTx locks a gateway, checks If-Match, applies fn, saves, audits and emits sensor.changed.
func (s *Service) gatewayTx(ctx context.Context, p *auth.Principal, scope store.MarinaScope, gatewayID string, ifMatch int, action, change string,
	fn func(g *store.SensorGateway, now time.Time) (map[string]any, error),
) (*GatewayView, error) {
	now := store.UTC(s.d.Clock.Now())
	var out *GatewayView
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		g, err := store.LockSensorGateway(ctx, tx, scope, gatewayID)
		if err != nil {
			return notFound(err)
		}
		if g.Version != ifMatch {
			return ErrVersionMismatch
		}
		diff, err := fn(g, now)
		if err != nil {
			return err
		}
		g.UpdatedAt = now
		if err := store.UpdateSensorGateway(ctx, tx, scope, g, ifMatch); err != nil {
			return mapVersion(err)
		}
		if err := s.audit(ctx, tx, p, scope, action, "sensor_gateway", g.ID, diff); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{GatewayID: g.ID, Change: change}); err != nil {
			return err
		}
		out, err = s.oneGatewayView(ctx, tx, scope, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateGateway updates a gateway (If-Match).
func (s *Service) UpdateGateway(ctx context.Context, p *auth.Principal, marinaID, gatewayID string, ifMatch int, in GatewayPatch) (*GatewayView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableGateways, gatewayID)
	if err != nil {
		return nil, err
	}
	change := "updated"
	return s.gatewayTx(ctx, p, scope, gatewayID, ifMatch, "sensor_gateway.update", change, func(g *store.SensorGateway, _ time.Time) (map[string]any, error) {
		if in.Name != nil {
			g.Name = strings.TrimSpace(*in.Name)
		}
		if in.Kind != nil {
			g.Kind = *in.Kind
		}
		if in.Active != nil {
			g.Active = *in.Active
		}
		v := &auth.ValidationError{}
		validateGateway(v, g.Code, g.Name, g.Kind, false)
		if err := v.Err(); err != nil {
			return nil, err
		}
		return map[string]any{"name": g.Name, "kind": g.Kind, "active": g.Active}, nil
	})
}

// RotateGatewayKey issues a new gateway key with a grace period for the previous one.
func (s *Service) RotateGatewayKey(ctx context.Context, p *auth.Principal, marinaID, gatewayID string, ifMatch, graceHours int) (*GatewayWithKey, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableGateways, gatewayID)
	if err != nil {
		return nil, err
	}
	if graceHours < 0 || graceHours > 168 {
		v := &auth.ValidationError{}
		v.Add("grace_hours", "out_of_range")
		return nil, v
	}
	var plain string
	view, err := s.gatewayTx(ctx, p, scope, gatewayID, ifMatch, "sensor_gateway.key_rotate", "key_rotated", func(g *store.SensorGateway, now time.Time) (map[string]any, error) {
		k, err := s.issueKey(ingestauth.GatewayPrefix, aadGateway+g.ID)
		if err != nil {
			return nil, err
		}
		cols := gatewayKeyCols(g)
		grace := time.Duration(graceHours) * time.Hour
		if s.keyStatus(cols, aadGateway+g.ID, now) == KeyUnusable {
			grace = 0
		}
		cols.rotate(k, now, grace)
		plain = k.plain
		return map[string]any{"key_id": g.KeyID, "prev_key_id": g.PrevKeyID, "grace_hours": graceHours}, nil
	})
	if err != nil {
		return nil, err
	}
	return &GatewayWithKey{Gateway: *view, Key: plain}, nil
}

// RevokeGatewayKey clears the gateway keys.
func (s *Service) RevokeGatewayKey(ctx context.Context, p *auth.Principal, marinaID, gatewayID string, ifMatch int) (*GatewayView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableGateways, gatewayID)
	if err != nil {
		return nil, err
	}
	return s.gatewayTx(ctx, p, scope, gatewayID, ifMatch, "sensor_gateway.key_revoke", "key_revoked", func(g *store.SensorGateway, _ time.Time) (map[string]any, error) {
		gatewayKeyCols(g).revoke()
		return nil, nil
	})
}

// ---- sources (harbor.configure) ----

func (s *Service) driver(code string) ext.SensorSource {
	for _, src := range s.d.Sources {
		if src.Code() == code {
			return src
		}
	}
	return nil
}

func (s *Service) sourceView(src *store.SensorSource, counts map[string]int) SourceView {
	v := SourceView{SensorSource: *src, ConfigSet: src.ConfigEnc != "", ConfigPublic: map[string]string{}, DeviceCount: counts[src.ID]}
	v.ConfigEnc = ""
	if drv := s.driver(src.Code); drv != nil && src.ConfigEnc != "" {
		if cfg, err := s.decryptConfig(src); err == nil {
			pub, _ := drv.ConfigKeys()
			for _, k := range pub {
				if val, ok := cfg[k]; ok {
					v.ConfigPublic[k] = val
				}
			}
		}
	}
	return v
}

func (s *Service) decryptConfig(src *store.SensorSource) (map[string]string, error) {
	cfg := map[string]string{}
	if src.ConfigEnc == "" {
		return cfg, nil
	}
	plain, err := s.d.Keys.DecryptString(src.ConfigEnc, aadSource+src.ID)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// encryptConfig keeps only the keys the driver declares and encrypts them ("" for an empty config).
func (s *Service) encryptConfig(v *auth.ValidationError, drv ext.SensorSource, id string, cfg map[string]string) (string, error) {
	pub, sec := drv.ConfigKeys()
	allowed := append(append([]string(nil), pub...), sec...)
	keep := map[string]string{}
	for k, val := range cfg {
		if !slices.Contains(allowed, k) {
			v.Add("config."+k, "unknown")
			continue
		}
		if utf8.RuneCountInString(val) > 1024 {
			v.Add("config."+k, "too_long")
			continue
		}
		keep[k] = val
	}
	if len(keep) == 0 {
		return "", nil
	}
	b, err := json.Marshal(keep)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(b), aadSource+id)
}

func (s *Service) oneSourceView(ctx context.Context, db bun.IDB, scope store.MarinaScope, src *store.SensorSource) (*SourceView, error) {
	counts, err := store.SensorDeviceCounts(ctx, db, scope, "source_id")
	if err != nil {
		return nil, err
	}
	v := s.sourceView(src, counts)
	return &v, nil
}

// ListSources lists the polled sources of the marina.
func (s *Service) ListSources(ctx context.Context, p *auth.Principal, marinaID string) ([]SourceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListSensorSources(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	counts, err := store.SensorDeviceCounts(ctx, s.d.DB.DB, scope, "source_id")
	if err != nil {
		return nil, err
	}
	out := make([]SourceView, 0, len(rows))
	for i := range rows {
		out = append(out, s.sourceView(&rows[i], counts))
	}
	return out, nil
}

// CreateSource configures a polled source of a registered driver.
func (s *Service) CreateSource(ctx context.Context, p *auth.Principal, marinaID string, in SourceInput) (*SourceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	v := &auth.ValidationError{}
	drv := s.driver(in.Code)
	if drv == nil {
		v.Add("code", "unknown_driver")
	}
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 80 {
		v.Add("name", "invalid")
	}
	if in.PollSeconds == 0 {
		in.PollSeconds = 60
	}
	if in.PollSeconds < 15 || in.PollSeconds > 3600 {
		v.Add("poll_seconds", "out_of_range")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	enabled := in.Enabled == nil || *in.Enabled
	var out *SourceView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		src := &store.SensorSource{
			ID: store.NewID(), MarinaID: scope.ID(), Code: in.Code, Name: in.Name, PollSeconds: in.PollSeconds, Enabled: enabled,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		enc, err := s.encryptConfig(v, drv, src.ID, in.Config)
		if err != nil {
			return err
		}
		if err := v.Err(); err != nil {
			return err
		}
		src.ConfigEnc = enc
		if err := store.InsertSensorSource(ctx, tx, src); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_source.create", "sensor_source", src.ID, map[string]any{
			"code": src.Code, "name": src.Name, "poll_seconds": src.PollSeconds, "enabled": src.Enabled, "config_keys": sortedKeys(in.Config),
		}); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{SourceID: src.ID, Change: "created"}); err != nil {
			return err
		}
		out, err = s.oneSourceView(ctx, tx, scope, src)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateSource updates a source (If-Match).
func (s *Service) UpdateSource(ctx context.Context, p *auth.Principal, marinaID, sourceID string, ifMatch int, in SourcePatch) (*SourceView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableSources, sourceID)
	if err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *SourceView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		src, err := store.LockSensorSource(ctx, tx, scope, sourceID)
		if err != nil {
			return notFound(err)
		}
		if src.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if in.Name != nil {
			src.Name = strings.TrimSpace(*in.Name)
			if n := utf8.RuneCountInString(src.Name); n < 1 || n > 80 {
				v.Add("name", "invalid")
			}
		}
		if in.PollSeconds != nil {
			src.PollSeconds = *in.PollSeconds
			if src.PollSeconds < 15 || src.PollSeconds > 3600 {
				v.Add("poll_seconds", "out_of_range")
			}
		}
		if in.Enabled != nil {
			src.Enabled = *in.Enabled
		}
		diff := map[string]any{"name": src.Name, "poll_seconds": src.PollSeconds, "enabled": src.Enabled}
		if in.Config != nil {
			drv := s.driver(src.Code)
			if drv == nil {
				v.Add("config", "unknown_driver")
			} else {
				enc, err := s.encryptConfig(v, drv, src.ID, in.Config)
				if err != nil {
					return err
				}
				src.ConfigEnc = enc
				diff["config_keys"] = sortedKeys(in.Config)
			}
		}
		if err := v.Err(); err != nil {
			return err
		}
		src.UpdatedAt = now
		if err := store.UpdateSensorSource(ctx, tx, scope, src, ifMatch); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return mapVersion(err)
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_source.update", "sensor_source", src.ID, diff); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{SourceID: src.ID, Change: "updated"}); err != nil {
			return err
		}
		out, err = s.oneSourceView(ctx, tx, scope, src)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PollSourceNow polls a source at once through Ingest.
func (s *Service) PollSourceNow(ctx context.Context, p *auth.Principal, marinaID, sourceID string) (*PollResult, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure, store.SensorTableSources, sourceID)
	if err != nil {
		return nil, err
	}
	return s.pollSource(ctx, scope.ID(), sourceID, true)
}

// ---- settings ----

func (s *Service) settingsView(set *store.SensorSettings) *Settings {
	return &Settings{SensorSettings: *set, RawRetentionDays: int(s.d.RawRetention / (24 * time.Hour))}
}

// GetSettings returns the settings (defaults, version 0, when never saved) (map.view).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	set, err := store.LoadSensorSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	return s.settingsView(set), nil
}

// PutSettings saves the settings (harbor.configure; If-Match 0 for the first save).
func (s *Service) PutSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*Settings, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	rng := func(field string, n, lo, hi int) {
		if n < lo || n > hi {
			v.Add(field, "out_of_range")
		}
	}
	rng("hysteresis_readings", in.HysteresisReadings, 1, 10)
	rng("hysteresis_minutes", in.HysteresisMinutes, 1, 60)
	rng("offline_after_minutes", in.OfflineAfterMinutes, 5, 1440)
	rng("silent_alert_minutes", in.SilentAlertMinutes, 30, 10080)
	if in.SilentAlertMinutes < in.OfflineAfterMinutes {
		v.Add("silent_alert_minutes", "below_offline")
	}
	rng("battery_low_pct", in.BatteryLowPct, 1, 90)
	rng("unexpected_after_minutes", in.UnexpectedAfterMinutes, 0, 240)
	rng("departure_cutoff_minutes", in.DepartureCutoffMinutes, 0, 1439)
	rng("presence_out_after_hours", in.PresenceOutAfterHours, 1, 168)
	rng("release_proposal_after_days", in.ReleaseProposalAfterDays, 1, 60)
	rng("release_proposal_nights", in.ReleaseProposalNights, 1, 60)
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := store.UTC(s.d.Clock.Now())
	var out *Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LoadSensorSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		if cur.Version != ifMatch {
			return ErrVersionMismatch
		}
		row := &store.SensorSettings{
			MarinaID: scope.ID(), HysteresisReadings: in.HysteresisReadings, HysteresisMinutes: in.HysteresisMinutes,
			OfflineAfterMinutes: in.OfflineAfterMinutes, SilentAlertMinutes: in.SilentAlertMinutes, BatteryLowPct: in.BatteryLowPct,
			UnexpectedAfterMinutes: in.UnexpectedAfterMinutes, DepartureCutoffMinutes: in.DepartureCutoffMinutes,
			PresenceEffectEnabled: in.PresenceEffectEnabled, PresenceOutAfterHours: in.PresenceOutAfterHours,
			ReleaseProposalAfterDays: in.ReleaseProposalAfterDays, ReleaseProposalNights: in.ReleaseProposalNights,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.SaveSensorSettings(ctx, tx, row, ifMatch); err != nil {
			return mapVersion(err)
		}
		if err := s.audit(ctx, tx, p, scope, "sensor_settings.update", "sensor_settings", scope.ID(), in); err != nil {
			return err
		}
		if err := s.changed(ctx, tx, scope, event.SensorChangedData{Change: "settings"}); err != nil {
			return err
		}
		out = s.settingsView(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- helpers ----

func (s *Service) audit(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, action, entity, id string, diff any) error {
	actor := audit.System
	if p != nil {
		actor = p.Actor()
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: actor, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, data event.SensorChangedData) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.SensorChanged, data)
	return err
}

func mapVersion(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func mapDeviceUnique(err error) error {
	if store.IsUniqueViolation(err) {
		if store.SensorUniqueIsResource(err) {
			return ErrResourceHasSensor
		}
		return ErrCodeTaken
	}
	return err
}

func deviceDiff(d *store.SensorDevice) map[string]any {
	return map[string]any{
		"code": d.Code, "kind": d.Kind, "resource_type": d.ResourceType, "resource_id": d.ResourceID, "from_cm": d.FromCm,
		"to_cm": d.ToCm, "pedestal_socket": d.PedestalSocket, "gateway_id": d.GatewayID, "source_id": d.SourceID, "active": d.Active,
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func eqPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
