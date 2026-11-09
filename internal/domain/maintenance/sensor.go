package maintenance

import (
	"context"
	"fmt"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/store"
)

// SensorTaskInput is the maintenance task proposed by a sensor alert (M12, m12-plan §8.3, D15): a silent or
// low-battery device. AlertID makes the creation idempotent (one task per alert).
type SensorTaskInput struct {
	DeviceID   string
	DeviceCode string
	MapID      string
	XCm, YCm   int64
	AlertID    string
	AlertKind  string // device_silent, battery_low
	Priority   string // low, normal (default), high, urgent
}

// sensorTaskTitles are the task titles per alert kind (Italian, the source language of the data; the device code
// is an operator label, never personal data).
var sensorTaskTitles = map[string]string{
	"device_silent": "Sensore %s senza segnale",
	"battery_low":   "Sensore %s: batteria scarica",
}

// sensorClientRef is the dedupe key of the task of an alert (maintenance_tasks_client_ref_uidx).
func sensorClientRef(alertID string) string { return "sensor-alert:" + alertID }

// CreateSensorTaskTx creates the task of a confirmed sensor alert inside the caller's transaction
// (element_type sensor, source sensor; maintenance.manage or maintenance.report, checked again here through p):
// audit and maintenance event in the same transaction. A second call for the same alert returns the first task
// (client_ref "sensor-alert:<alert_id>" on the unique index, so concurrent confirms create one task).
func (s *Service) CreateSensorTaskTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, in SensorTaskInput) (*store.MaintenanceTask, error) {
	if p == nil || (!p.Can(scope.OrgID(), scope.ID(), auth.PermMaintenanceManage) && !p.Can(scope.OrgID(), scope.ID(), auth.PermMaintenanceReport)) {
		return nil, auth.ErrForbidden
	}
	title, ok := sensorTaskTitles[in.AlertKind]
	if !ok || !store.ValidID(in.AlertID) || !store.ValidID(in.DeviceID) {
		return nil, ErrElementInvalid
	}
	priority := in.Priority
	if priority == "" {
		priority = "normal"
	}
	if !slices.Contains(priorities, priority) {
		v := &auth.ValidationError{}
		v.Add("priority", "invalid")
		return nil, v.Err()
	}
	ref := sensorClientRef(in.AlertID)
	if prev, err := store.MaintenanceTaskByClientRef(ctx, tx, scope, ref); err == nil {
		return prev, nil
	}
	if _, err := store.GetSensorDevice(ctx, tx, scope, in.DeviceID); err != nil {
		return nil, ErrElementInvalid
	}
	now := s.now()
	x, y := in.XCm, in.YCm
	t := &store.MaintenanceTask{
		ID: store.NewID(), ElementType: store.ElementSensor, ElementID: in.DeviceID, Source: store.TaskSourceSensor,
		Title: fmt.Sprintf(title, in.DeviceCode), Priority: priority, Status: store.TaskOpen, MapID: in.MapID,
		PositionXCm: &x, PositionYCm: &y, ReportedByUserID: p.UserID, ClientRef: ref, CreatedAt: now, UpdatedAt: now,
	}
	if in.MapID != "" {
		ok, err := mapExists(ctx, tx, scope, in.MapID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrElementInvalid
		}
	}
	inserted, err := store.InsertMaintenanceTaskIfAbsent(ctx, tx, scope, t)
	if err != nil {
		if store.IsForeignKeyViolation(err) {
			return nil, ErrElementInvalid
		}
		return nil, err
	}
	if !inserted {
		return store.MaintenanceTaskByClientRef(ctx, tx, scope, ref)
	}
	if _, err := deadline.SyncMaintenanceTask(ctx, tx, scope, t, now); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, p.Actor(), "maintenance_task.create", "maintenance_task", t.ID,
		map[string]any{"source": t.Source, "element_type": t.ElementType, "priority": t.Priority, "alert_id": in.AlertID}); err != nil {
		return nil, err
	}
	if err := s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status); err != nil {
		return nil, err
	}
	return t, nil
}
