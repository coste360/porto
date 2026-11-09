package sensor

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

var (
	// reExternalID is the vendor id (DevEUI, serial) of a device: the device_id of the protocol.
	reExternalID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	// reCode is an operator label of a device or a gateway.
	reCode = regexp.MustCompile(`^[A-Z0-9_-]{1,32}$`)

	deviceKinds = []string{
		store.SensorKindUltrasonic, store.SensorKindRadar, store.SensorKindMagnetometer, store.SensorKindInfrared,
		store.SensorKindCamera, store.SensorKindOther,
	}
	gatewayKinds = []string{store.SensorGatewayLoRaWAN, store.SensorGatewayNBIoT, store.SensorGatewayMQTTBridge, store.SensorGatewayOther}
)

// minSegmentCm is the shortest dynamic-area segment a device may watch (m12-plan §3.1).
const minSegmentCm = 100

func validateDeviceFields(v *auth.ValidationError, code, kind, model, firmware string) {
	if !reCode.MatchString(code) {
		v.Add("code", "invalid")
	}
	if !slices.Contains(deviceKinds, kind) {
		v.Add("kind", "invalid")
	}
	if utf8.RuneCountInString(model) > 64 {
		v.Add("model", "too_long")
	}
	if utf8.RuneCountInString(firmware) > 64 {
		v.Add("firmware", "too_long")
	}
}

// placement is the resource, segment, socket and map position requested for a device.
type placement struct {
	ResourceType, ResourceID string
	FromCm, ToCm             *int64
	Socket                   string
	XCm, YCm                 *int64
	// skipActiveCheck: an inactive device may stay on an inactive resource and never counts for overlaps.
	skipActiveCheck bool
}

// placeDevice validates the placement against the resource of the marina (m12-plan §3.1) and writes resource,
// segment, socket, map and position into d. The resource unique index (one active device per berth or socket)
// is enforced by the database; segment overlaps are checked here under the area lock.
func (s *Service) placeDevice(ctx context.Context, tx bun.Tx, scope store.MarinaScope, d *store.SensorDevice, pl placement) error {
	switch pl.ResourceType {
	case store.SensorResourceBerth, store.SensorResourceDynamicArea, store.SensorResourcePedestal:
	default:
		v := &auth.ValidationError{}
		v.Add("resource_type", "invalid")
		return v
	}
	if pl.ResourceType == store.SensorResourceDynamicArea {
		// Serialise segment checks of one area (PostgreSQL row lock; SQLite has one writer).
		if _, err := store.LockDynamicArea(ctx, tx, scope, pl.ResourceID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrInvalidResource
			}
			return err
		}
	}
	res, err := store.LoadSensorResource(ctx, tx, scope, pl.ResourceType, pl.ResourceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidResource
		}
		return err
	}
	if !res.Active && !pl.skipActiveCheck {
		return ErrInvalidResource
	}
	if (pl.FromCm != nil || pl.ToCm != nil) && pl.ResourceType != store.SensorResourceDynamicArea {
		return ErrInvalidResource
	}
	if pl.Socket != "" && pl.ResourceType != store.SensorResourcePedestal {
		return ErrInvalidResource
	}
	var x, y int64
	switch pl.ResourceType {
	case store.SensorResourceBerth, store.SensorResourcePedestal:
		x, y = res.XCm, res.YCm
		if pl.Socket != "" && !slices.Contains(res.Sockets, pl.Socket) {
			return ErrInvalidResource
		}
	case store.SensorResourceDynamicArea:
		if res.AreaKind != "linear" || pl.FromCm == nil || pl.ToCm == nil {
			return ErrInvalidResource
		}
		from, to, length := *pl.FromCm, *pl.ToCm, res.ToCm-res.FromCm
		if from < 0 || from >= to || to > length || to-from < minSegmentCm {
			return ErrInvalidResource
		}
		if !pl.skipActiveCheck {
			segs, err := store.ActiveSensorSegments(ctx, tx, scope, res.ID)
			if err != nil {
				return err
			}
			for _, sg := range segs {
				if sg.DeviceID != d.ID && from < sg.ToCm && sg.FromCm < to {
					return ErrSegmentOverlap
				}
			}
		}
		x, y = segmentMidpoint(res, from, to)
	}
	m, err := store.GetMap(ctx, tx, scope, res.MapID)
	if err != nil {
		return err
	}
	if pl.ResourceType == store.SensorResourceDynamicArea && x == 0 && y == 0 {
		x, y = m.WidthCm/2, m.HeightCm/2
	}
	if pl.XCm != nil {
		x = *pl.XCm
	}
	if pl.YCm != nil {
		y = *pl.YCm
	}
	v := &auth.ValidationError{}
	if x < 0 || x > m.WidthCm {
		v.Add("x_cm", "out_of_range")
	}
	if y < 0 || y > m.HeightCm {
		v.Add("y_cm", "out_of_range")
	}
	if err := v.Err(); err != nil {
		return err
	}
	d.ResourceType, d.ResourceID, d.MapID, d.XCm, d.YCm = res.Type, res.ID, res.MapID, x, y
	d.FromCm, d.ToCm, d.PedestalSocket = nil, nil, ""
	if pl.ResourceType == store.SensorResourceDynamicArea {
		f, t := *pl.FromCm, *pl.ToCm
		d.FromCm, d.ToCm = &f, &t
	}
	if pl.ResourceType == store.SensorResourcePedestal {
		d.PedestalSocket = pl.Socket
	}
	return nil
}

// segmentMidpoint is the point of the area axis (its structure polyline, from the area's offset) at the middle
// of the segment; (0, 0) when the area has no usable structure axis.
func segmentMidpoint(res *store.SensorResource, from, to int64) (int64, int64) {
	if len(res.StructurePoint) < 2 {
		return 0, 0
	}
	pts := make([]geo.Point, 0, len(res.StructurePoint))
	for _, p := range res.StructurePoint {
		pts = append(pts, geo.Point{X: geo.Cm(p.X), Y: geo.Cm(p.Y)})
	}
	pt, _, err := layout.PointAt(pts, geo.Cm(res.FromCm+(from+to)/2))
	if err != nil {
		return 0, 0
	}
	return int64(pt.X), int64(pt.Y)
}

// checkBinding verifies that the gateway or source a device is bound to exists in the marina (422 otherwise).
func (s *Service) checkBinding(ctx context.Context, tx bun.IDB, scope store.MarinaScope, gatewayID, sourceID string) error {
	v := &auth.ValidationError{}
	if gatewayID != "" {
		if err := store.SensorExists(ctx, tx, scope, store.SensorTableGateways, gatewayID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("gateway_id", "not_found")
		}
	}
	if sourceID != "" {
		if err := store.SensorExists(ctx, tx, scope, store.SensorTableSources, sourceID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("source_id", "not_found")
		}
	}
	return v.Err()
}
