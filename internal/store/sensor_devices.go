package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

// Device writes and reads of the M12 sensor service (owned by INGEST, m12-plan §3.1). Sensors never write
// allocations, occupancy rows, releases, contracts or charges (INV-100).

// SensorDeviceFilter filters the devices of a marina (empty fields = any).
type SensorDeviceFilter struct {
	MapID, ResourceType, ResourceID string
	Active                          *bool
}

// ListSensorDevicesOf returns the devices of the marina matching f, by code then id.
func ListSensorDevicesOf(ctx context.Context, db bun.IDB, s MarinaScope, f SensorDeviceFilter) ([]SensorDevice, error) {
	out := []SensorDevice{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if f.MapID != "" {
		q = q.Where("map_id = ?", f.MapID)
	}
	if f.ResourceType != "" {
		q = q.Where("resource_type = ?", f.ResourceType)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_id = ?", f.ResourceID)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	err := q.OrderExpr("code, id").Scan(ctx)
	return out, err
}

// InsertSensorDevice inserts a device. A unique violation is returned unchanged: SensorUniqueIsResource tells a
// second device on a berth or socket from a taken code or external id.
func InsertSensorDevice(ctx context.Context, db bun.IDB, d *SensorDevice) error {
	d.CreatedAt, d.UpdatedAt = UTC(d.CreatedAt), UTC(d.UpdatedAt)
	_, err := db.NewInsert().Model(d).Exec(ctx)
	return err
}

// UpdateSensorDevice writes every column of d when its version equals expected (ErrVersionMismatch otherwise,
// ErrNotFound for another marina's id) and sets d.Version to expected + 1.
func UpdateSensorDevice(ctx context.Context, db bun.IDB, s MarinaScope, d *SensorDevice, expected int) error {
	d.UpdatedAt = UTC(d.UpdatedAt)
	res, err := db.NewUpdate().Model(d).ExcludeColumn("id", "marina_id", "created_at", "version").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", d.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetSensorDevice(ctx, db, s, d.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	d.Version = expected + 1
	return nil
}

// SensorUniqueIsResource reports whether a unique violation comes from the "one active device per berth or
// pedestal socket" index (sensor_devices_resource_uidx) rather than a code, external id or key id.
func SensorUniqueIsResource(err error) bool {
	if pe, ok := errors.AsType[pgdriver.Error](err); ok {
		return pe.Field('n') == "sensor_devices_resource_uidx"
	}
	return err != nil && strings.Contains(err.Error(), "sensor_devices_resource_uidx")
}

// SensorDeviceByKeyID returns the device whose current or previous key id is keyID, in any marina (the ingestion
// resolves the marina from the key, INV-102). ErrNotFound when none.
func SensorDeviceByKeyID(ctx context.Context, db bun.IDB, keyID string) (*SensorDevice, error) {
	if keyID == "" {
		return nil, ErrNotFound
	}
	d := new(SensorDevice)
	if err := db.NewSelect().Model(d).Where("key_id = ? OR prev_key_id = ?", keyID, keyID).Limit(1).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// SensorDevicesByExternalIDs returns the devices of the marina with the given external ids, keyed by external id.
func SensorDevicesByExternalIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]*SensorDevice, error) {
	out := make(map[string]*SensorDevice, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		var rows []SensorDevice
		if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("external_id IN (?)", bun.List(chunk)).Scan(ctx); err != nil {
			return nil, err
		}
		for i := range rows {
			out[rows[i].ExternalID] = &rows[i]
		}
	}
	return out, nil
}

// SensorDevicesByIDs returns the devices of the marina with the given ids, keyed by id.
func SensorDevicesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]*SensorDevice, error) {
	out := make(map[string]*SensorDevice, len(ids))
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		var rows []SensorDevice
		if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(chunk)).Scan(ctx); err != nil {
			return nil, err
		}
		for i := range rows {
			out[rows[i].ID] = &rows[i]
		}
	}
	return out, nil
}

// SensorSegment is an active dynamic-area segment of a device.
type SensorSegment struct {
	DeviceID string `bun:"id"`
	FromCm   int64  `bun:"from_cm"`
	ToCm     int64  `bun:"to_cm"`
}

// ActiveSensorSegments returns the active device segments of a dynamic area (the caller checks overlaps).
func ActiveSensorSegments(ctx context.Context, db bun.IDB, s MarinaScope, areaID string) ([]SensorSegment, error) {
	var out []SensorSegment
	err := db.NewSelect().Table("sensor_devices").Column("id", "from_cm", "to_cm").
		Where("marina_id = ?", s.ID()).Where("resource_type = ?", SensorResourceDynamicArea).Where("resource_id = ?", areaID).
		Where("active = ?", true).OrderExpr("from_cm").Scan(ctx, &out)
	return out, err
}

// SensorResource is what a device may watch: a berth, a dynamic area or a pedestal of the marina, with what the
// service needs to validate the binding and place the device on the map.
type SensorResource struct {
	Type, ID, MapID, Code string
	Active                bool
	// Berth: anchor; pedestal: position.
	XCm, YCm int64
	// Dynamic area: kind (linear or surface), offsets on the structure and the structure axis.
	AreaKind       string
	FromCm, ToCm   int64
	StructurePoint []SensorPoint
	// Pedestal: socket codes.
	Sockets []string
}

// SensorPoint is a map point (cm).
type SensorPoint struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

// LoadSensorResource returns a resource of the marina (ErrNotFound when it does not exist there).
func LoadSensorResource(ctx context.Context, db bun.IDB, s MarinaScope, typ, id string) (*SensorResource, error) {
	switch typ {
	case SensorResourceBerth:
		b, err := BerthByID(ctx, db, s, id)
		if err != nil {
			return nil, err
		}
		return &SensorResource{Type: typ, ID: b.ID, MapID: b.MapID, Code: b.Code, Active: b.Active, XCm: b.AnchorXCm, YCm: b.AnchorYCm}, nil
	case SensorResourceDynamicArea:
		a, err := DynamicAreaByID(ctx, db, s, id)
		if err != nil {
			return nil, err
		}
		r := &SensorResource{Type: typ, ID: a.ID, MapID: a.MapID, Code: a.Code, Active: a.Active, AreaKind: a.Kind}
		if a.FromCm != nil && a.ToCm != nil {
			r.FromCm, r.ToCm = *a.FromCm, *a.ToCm
		}
		if a.StructureID != "" {
			var geom JSON
			err := db.NewSelect().Table("harbor_structures").Column("geometry").
				Where("marina_id = ?", s.ID()).Where("id = ?", a.StructureID).Scan(ctx, &geom)
			if err != nil && !errors.Is(notFound(err), ErrNotFound) {
				return nil, err
			}
			var g struct {
				Points []SensorPoint `json:"points"`
			}
			if len(geom) > 0 && json.Unmarshal(geom, &g) == nil {
				r.StructurePoint = g.Points
			}
		}
		return r, nil
	case SensorResourcePedestal:
		if !ValidID(id) {
			return nil, ErrNotFound
		}
		p := new(Pedestal)
		if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
			return nil, notFound(err)
		}
		r := &SensorResource{Type: typ, ID: p.ID, MapID: p.MapID, Code: p.Code, Active: true, XCm: p.XCm, YCm: p.YCm}
		var sockets []struct {
			Code string `json:"code"`
		}
		if len(p.Sockets) > 0 && json.Unmarshal(p.Sockets, &sockets) == nil {
			for _, so := range sockets {
				r.Sockets = append(r.Sockets, so.Code)
			}
		}
		return r, nil
	}
	return nil, ErrNotFound
}

// SensorResourceCodes returns the codes of the berths, dynamic areas and pedestals of the marina by id (device
// lists show the resource code).
func SensorResourceCodes(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	out := map[string]string{}
	for _, table := range []string{"berths", "dynamic_areas", "pedestals"} {
		var rows []struct {
			ID   string `bun:"id"`
			Code string `bun:"code"`
		}
		if err := db.NewSelect().Table(table).Column("id", "code").Where("marina_id = ?", s.ID()).Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = r.Code
		}
	}
	return out, nil
}

// PedestalBerthCounts returns, per pedestal of the marina, the number of active berths it serves in total and per
// socket ("<pedestal>|<socket>"): a pedestal device is informational unless it maps to exactly one berth.
func PedestalBerthCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	var rows []struct {
		PedestalID string `bun:"pedestal_id"`
		Socket     string `bun:"pedestal_socket"`
	}
	if err := db.NewSelect().Table("berths").ColumnExpr("pedestal_id, COALESCE(pedestal_socket, '') AS pedestal_socket").
		Where("marina_id = ?", s.ID()).Where("active = ?", true).Where("pedestal_id IS NOT NULL").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.PedestalID]++
		if r.Socket != "" {
			out[r.PedestalID+"|"+r.Socket]++
		}
	}
	return out, nil
}

// SensorDeviceCounts returns the number of devices bound to each gateway or source of the marina (by id).
func SensorDeviceCounts(ctx context.Context, db bun.IDB, s MarinaScope, column string) (map[string]int, error) {
	if column != "gateway_id" && column != "source_id" {
		return nil, errors.New("store: bad binding column")
	}
	var rows []struct {
		ID string `bun:"bid"`
		N  int    `bun:"n"`
	}
	if err := db.NewSelect().Table("sensor_devices").ColumnExpr(column+" AS bid").ColumnExpr("COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Where(column+" IS NOT NULL").GroupExpr(column).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.ID] = r.N
	}
	return out, nil
}

// SensorKeyRow is the key columns shared by devices and gateways.
type SensorKeyRow struct {
	KeyID, KeyHash, KeyVerifierEnc             string
	KeyIssuedAt                                *time.Time
	PrevKeyID, PrevKeyHash, PrevKeyVerifierEnc string
	PrevValidUntil                             *time.Time
}

// SensorDeviceCodeTaken reports whether another device of the marina (id excluded) has the code or the external id
// (the unique indexes stay the guarantee; this gives a deterministic error when several constraints would fail).
func SensorDeviceCodeTaken(ctx context.Context, db bun.IDB, s MarinaScope, id, code, externalID string) (bool, error) {
	n, err := db.NewSelect().Table("sensor_devices").Where("marina_id = ?", s.ID()).Where("id <> ?", id).
		Where("(code = ? OR external_id = ?)", code, externalID).Count(ctx)
	return n > 0, err
}

// SensorSeedBerth, SensorSeedArea and SensorSeedPedestal are what the demo seed needs to place devices.
type SensorSeedBerth struct {
	ID          string `bun:"id"`
	Code        string `bun:"code"`
	StructureID string `bun:"structure_id"`
}

// SensorSeedArea is an active linear dynamic area.
type SensorSeedArea struct {
	ID     string `bun:"id"`
	FromCm int64  `bun:"from_cm"`
	ToCm   int64  `bun:"to_cm"`
}

// SensorSeedPedestal is a pedestal with the socket of the berth it serves ("" when none).
type SensorSeedPedestal struct {
	ID     string `bun:"id"`
	Socket string `bun:"socket"`
}

// SensorSeedCandidates returns the active berths (by structure then code), the active linear areas and the
// pedestals of the marina (each with one socket used by a berth, when any) for the demo seed.
func SensorSeedCandidates(ctx context.Context, db bun.IDB, s MarinaScope) ([]SensorSeedBerth, []SensorSeedArea, []SensorSeedPedestal, error) {
	var berths []SensorSeedBerth
	if err := db.NewSelect().Table("berths").ColumnExpr("id, code, COALESCE(CAST(structure_id AS TEXT), '') AS structure_id").
		Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("structure_id, code").Scan(ctx, &berths); err != nil {
		return nil, nil, nil, err
	}
	var areas []SensorSeedArea
	if err := db.NewSelect().Table("dynamic_areas").Column("id", "from_cm", "to_cm").Where("marina_id = ?", s.ID()).
		Where("active = ?", true).Where("kind = ?", "linear").Where("from_cm IS NOT NULL AND to_cm IS NOT NULL").
		OrderExpr("code").Scan(ctx, &areas); err != nil {
		return nil, nil, nil, err
	}
	var peds []SensorSeedPedestal
	if err := db.NewSelect().TableExpr("pedestals AS p").ColumnExpr("p.id").
		ColumnExpr("COALESCE((SELECT MIN(b.pedestal_socket) FROM berths AS b WHERE b.pedestal_id = p.id AND b.active = ? AND b.pedestal_socket IS NOT NULL), '') AS socket", true).
		Where("p.marina_id = ?", s.ID()).OrderExpr("p.code").Scan(ctx, &peds); err != nil {
		return nil, nil, nil, err
	}
	return berths, areas, peds, nil
}
