//go:build integration

package maintenance

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMaintenance_CreateSensorTaskTx: the task of a confirmed sensor alert (element_type sensor, source sensor) is
// created once per alert, also under concurrent confirms; a role without maintenance permissions is refused.
func TestMaintenance_CreateSensorTaskTx(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		now := store.UTC(m.Clock.Now())
		dev := &store.SensorDevice{
			ID: store.NewID(), MarinaID: m.MarinaID, ExternalID: "EXT1", Code: "S-A1", Kind: store.SensorKindRadar,
			ResourceType: store.SensorResourceBerth, ResourceID: m.Berths[0], MapID: m.MapID, XCm: 1000, YCm: 1200, Active: true,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		m.Insert(t, dev)
		alertID := store.NewID()
		in := SensorTaskInput{DeviceID: dev.ID, DeviceCode: dev.Code, MapID: m.MapID, XCm: 1000, YCm: 1200, AlertID: alertID, AlertKind: "device_silent"}
		create := func(p *auth.Principal, in SensorTaskInput) (*store.MaintenanceTask, error) {
			var out *store.MaintenanceTask
			err := db.RunInTx(ctxT(), func(ctx context.Context, tx bun.Tx) error {
				var err error
				out, err = m.s.CreateSensorTaskTx(ctx, tx, m.scope, p, in)
				return err
			})
			return out, err
		}
		var wg sync.WaitGroup
		ids := make([]string, 100)
		for i := range 100 {
			wg.Go(func() {
				task, err := create(m.Owner, in)
				if err != nil {
					t.Errorf("create: %v", err)
					return
				}
				ids[i] = task.ID
			})
		}
		wg.Wait()
		for _, id := range ids {
			if id != ids[0] {
				t.Fatal("several tasks for one alert")
			}
		}
		task, err := store.GetMaintenanceTask(ctxT(), db.DB, m.scope, ids[0])
		if err != nil || task.ElementType != store.ElementSensor || task.Source != store.TaskSourceSensor || task.Priority != "normal" ||
			task.Title != "Sensore S-A1 senza segnale" || task.MapID != m.MapID {
			t.Fatalf("task %v %+v", err, task)
		}
		if m.AuditCount(t, "maintenance_task.create") != 1 {
			t.Fatal("audit")
		}
		viewer := m.AddUser(t, "viewer@porto.test", auth.RoleViewer, m.OrgID, m.MarinaID)
		in.AlertID = store.NewID()
		if _, err := create(viewer, in); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		in.AlertKind = "unexpected_occupied"
		if _, err := create(m.Owner, in); !errors.Is(err, ErrElementInvalid) {
			t.Fatalf("kind: %v", err)
		}
		in.AlertKind, in.Priority = "battery_low", "panic"
		if _, err := create(m.Owner, in); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("priority: %v", err)
		}
	})
}
