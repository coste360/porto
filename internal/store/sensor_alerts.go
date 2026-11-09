package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Sensor alert writes and reads (M12, m12-plan §8.3; owned by RECONCILE). One open alert per (device, kind) is the
// partial unique index sensor_alerts_open_uidx (INV-104): InsertSensorAlertIfNew relies on it with ON CONFLICT DO
// NOTHING, never on a "check then insert".

// UnclearedSensorAlerts returns the alerts of the marina whose condition is not cleared (any status), ordered by
// device, kind and id.
func UnclearedSensorAlerts(ctx context.Context, db bun.IDB, s MarinaScope) ([]SensorAlert, error) {
	var out []SensorAlert
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("cleared_at IS NULL").
		OrderExpr("device_id, kind, id").Scan(ctx)
	return out, err
}

// OpenSensorAlertsOf returns the open alerts of the devices (all devices of the marina when ids is empty).
func OpenSensorAlertsOf(ctx context.Context, db bun.IDB, s MarinaScope, deviceIDs []string) ([]SensorAlert, error) {
	var out []SensorAlert
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", SensorAlertOpen)
	if len(deviceIDs) > 0 {
		q = q.Where("device_id IN (?)", bun.List(validIDs(deviceIDs)))
	}
	err := q.OrderExpr("device_id, kind").Scan(ctx)
	return out, err
}

// InsertSensorAlertIfNew opens an alert unless (device, kind, condition_key) is already recorded without
// cleared_at (any status) or another alert of (device, kind) is open (unique index). It reports whether the row
// was inserted.
func InsertSensorAlertIfNew(ctx context.Context, tx bun.IDB, s MarinaScope, a *SensorAlert) (bool, error) {
	n, err := tx.NewSelect().Table("sensor_alerts").Where("marina_id = ?", s.ID()).Where("device_id = ?", a.DeviceID).
		Where("kind = ?", a.Kind).Where("condition_key = ?", a.ConditionKey).Where("cleared_at IS NULL").Count(ctx)
	if err != nil || n > 0 {
		return false, err
	}
	if a.ID == "" {
		a.ID = NewID()
	}
	a.MarinaID = s.ID()
	if a.Status == "" {
		a.Status = SensorAlertOpen
	}
	if len(a.Refs) == 0 {
		a.Refs = JSON("{}")
	}
	if a.Version == 0 {
		a.Version = 1
	}
	a.OpenedAt, a.CreatedAt, a.UpdatedAt = UTC(a.OpenedAt), UTC(a.CreatedAt), UTC(a.UpdatedAt)
	res, err := tx.NewInsert().Model(a).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows == 1, err
}

// ClearSensorAlertCondition sets cleared_at on the uncleared alerts of (device, kind) and resolves the open one. It
// returns the rows that were open (now resolved), for the events.
func ClearSensorAlertCondition(ctx context.Context, tx bun.IDB, s MarinaScope, deviceID, kind string, now time.Time) ([]SensorAlert, error) {
	var open []SensorAlert
	if err := tx.NewSelect().Model(&open).Where("marina_id = ?", s.ID()).Where("device_id = ?", deviceID).
		Where("kind = ?", kind).Where("status = ?", SensorAlertOpen).Scan(ctx); err != nil {
		return nil, err
	}
	now = UTC(now)
	if _, err := tx.NewUpdate().Table("sensor_alerts").Set("status = ?", SensorAlertResolved).
		Set("version = version + 1").Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).
		Where("device_id = ?", deviceID).Where("kind = ?", kind).Where("status = ?", SensorAlertOpen).Exec(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.NewUpdate().Table("sensor_alerts").Set("cleared_at = ?", now).Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("device_id = ?", deviceID).Where("kind = ?", kind).
		Where("cleared_at IS NULL").Exec(ctx); err != nil {
		return nil, err
	}
	for i := range open {
		open[i].Status, open[i].ClearedAt, open[i].Version = SensorAlertResolved, &now, open[i].Version+1
	}
	return open, nil
}

// UpdateSensorAlertDecision writes the decision of an alert (status, cleared_at, decided_*, outcome_ref, note) with
// optimistic locking on expected; ErrVersionMismatch when the row moved.
func UpdateSensorAlertDecision(ctx context.Context, tx bun.IDB, s MarinaScope, a *SensorAlert, expected int, now time.Time) error {
	now = UTC(now)
	res, err := tx.NewUpdate().Table("sensor_alerts").
		Set("status = ?", a.Status).Set("cleared_at = ?", a.ClearedAt).Set("decided_at = ?", a.DecidedAt).
		Set("decided_by_user_id = ?", nullStr(a.DecidedByUserID)).Set("outcome_ref = ?", nullStr(a.OutcomeRef)).
		Set("note = ?", a.Note).Set("version = ?", expected+1).Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", a.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrVersionMismatch
	}
	a.Version, a.UpdatedAt = expected+1, now
	return nil
}

// SensorAlertFilter selects alerts (Status "" = every status).
type SensorAlertFilter struct {
	Status, Kind, MapID, DeviceID, ResourceID string
	OpenedFrom, OpenedTo                      *time.Time
}

// ListSensorAlerts returns a page of alerts newest first (ids are UUIDv7: id order is creation order).
func ListSensorAlerts(ctx context.Context, db bun.IDB, s MarinaScope, f SensorAlertFilter, cursor string, limit int) (Page[SensorAlert], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[SensorAlert]{}, err
	}
	rows := []SensorAlert{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.MapID != "" {
		q = q.Where("map_id = ?", f.MapID)
	}
	if f.DeviceID != "" {
		q = q.Where("device_id = ?", f.DeviceID)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_id = ?", f.ResourceID)
	}
	if f.OpenedFrom != nil {
		q = q.Where("COALESCE(cleared_at, opened_at) >= ?", UTC(*f.OpenedFrom))
	}
	if f.OpenedTo != nil {
		q = q.Where("opened_at < ?", UTC(*f.OpenedTo))
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[SensorAlert]{}, err
	}
	return pageOf(rows, limit, func(r SensorAlert) string { return r.ID }), nil
}

// SensorAlertsTouching returns the alerts of resources opened before to and not cleared before from (history,
// report), ordered by opened_at.
func SensorAlertsTouching(ctx context.Context, db bun.IDB, s MarinaScope, resourceIDs []string, from, to time.Time) ([]SensorAlert, error) {
	ids := validIDs(resourceIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	var out []SensorAlert
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("resource_id IN (?)", bun.List(ids)).
		Where("opened_at < ?", UTC(to)).Where("(cleared_at IS NULL OR cleared_at >= ?)", UTC(from)).
		OrderExpr("opened_at, id").Scan(ctx)
	return out, err
}

// CheckSensorAlertInvariants runs Q42 (at most one open alert per (device, kind), no open alert with cleared_at)
// and Q43 (presences.source = sensor only on annual mooring contracts: none on a transit, on another mooring term
// or without a contract, INV-100) on one marina (m12-plan §15).
func CheckSensorAlertInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	type dup struct {
		DeviceID string `bun:"device_id"`
		Kind     string `bun:"kind"`
		N        int    `bun:"n"`
	}
	var dups []dup
	if err := db.NewRaw(`SELECT device_id, kind, COUNT(*) AS n FROM sensor_alerts WHERE marina_id = ? AND status = 'open'
		GROUP BY device_id, kind HAVING COUNT(*) > 1`, marinaID).Scan(ctx, &dups); err != nil {
		return nil, err
	}
	for _, d := range dups {
		out = append(out, Violation{Query: "Q42", IDs: []string{d.DeviceID}, Detail: d.Kind})
	}
	var ids []string
	if err := db.NewRaw(`SELECT id FROM sensor_alerts WHERE marina_id = ? AND status = 'open' AND cleared_at IS NOT NULL`,
		marinaID).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out = append(out, Violation{Query: "Q42", IDs: []string{id}, Detail: "open_cleared"})
	}
	ids = nil
	if err := db.NewRaw(`SELECT p.id FROM presences AS p JOIN contracts AS c ON c.id = p.contract_id
		WHERE p.marina_id = ? AND p.source = 'sensor' AND c.kind = 'transit'`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out = append(out, Violation{Query: "Q43", IDs: []string{id}, Detail: "transit"})
	}
	// Q43 also for sensor presences without a contract or on a mooring contract that is not annual.
	ids = nil
	if err := db.NewRaw(`SELECT p.id FROM presences AS p LEFT JOIN contracts AS c ON c.id = p.contract_id
		WHERE p.marina_id = ? AND p.source = 'sensor' AND (c.id IS NULL OR (c.kind <> 'transit' AND
		(c.kind <> 'mooring' OR c.term IS NULL OR c.term <> 'annual')))`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out = append(out, Violation{Query: "Q43", IDs: []string{id}, Detail: "not_annual_mooring"})
	}
	// Q43 also for presences without contract_id but on a transit allocation.
	ids = nil
	if err := db.NewRaw(`SELECT p.id FROM presences AS p JOIN allocations AS a ON a.id = p.allocation_id
		WHERE p.marina_id = ? AND p.source = 'sensor' AND a.kind = 'transit'`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		out = append(out, Violation{Query: "Q43", IDs: []string{id}, Detail: "transit_allocation"})
	}
	return out, nil
}
