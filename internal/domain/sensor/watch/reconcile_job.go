package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// RunReconcile is the job sensors.reconcile (m12-plan §8.4): per marina with active devices, one transaction that
// locks the sensor_settings row, loads the snapshot, evaluates the rules and applies the actions. A marina that
// fails is logged (ids only) and does not stop the others; the first error is returned.
func (s *Service) RunReconcile(ctx context.Context) error {
	start := s.d.Clock.Now() // wall time of the run for the metrics (L6) with the real clock
	defer func() {
		d := s.d.Clock.Now().Sub(start).Nanoseconds()
		s.metrics.Runs.Add(1)
		s.metrics.RunNano.Add(d)
		for {
			cur := s.metrics.MaxNano.Load()
			if d <= cur || s.metrics.MaxNano.CompareAndSwap(cur, d) {
				break
			}
		}
	}()
	ids, err := store.MarinasWithActiveSensors(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if _, err := s.ReconcileMarina(ctx, scope); err != nil {
			s.d.Log.Warn("sensors reconcile failed", "marina_id", id, "error", err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

// ReconcileResult counts what one reconcile run of a marina did.
type ReconcileResult struct {
	Opened, Resolved, Presences int
}

// ReconcileMarina runs the rules once on one marina (the caller has a system scope: the job, the seed, tests).
func (s *Service) ReconcileMarina(ctx context.Context, scope store.MarinaScope) (ReconcileResult, error) {
	var res ReconcileResult
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res = ReconcileResult{}
		now := s.now()
		settings, err := store.LockSensorSettings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		sn, err := s.loadSnapshot(ctx, tx, scope, store.SensorWatchDeviceFilter{}, now, settings)
		if err != nil {
			return err
		}
		uncleared, err := store.UnclearedSensorAlerts(ctx, tx, scope)
		if err != nil {
			return err
		}
		devByID := map[string]store.SensorWatchDevice{}
		for _, d := range sn.devices {
			devByID[d.ID] = d
		}
		for _, a := range reconcile.Evaluate(sn.input(alertKeys(uncleared))) {
			switch {
			case a.Clear != nil:
				rows, err := store.ClearSensorAlertCondition(ctx, tx, scope, a.Clear.DeviceID, a.Clear.Kind, now)
				if err != nil {
					return err
				}
				for i := range rows {
					if err := s.alertEvent(ctx, tx, scope, audit.System, "sensor_alert.resolve", &rows[i]); err != nil {
						return err
					}
					res.Resolved++
				}
			case a.Open != nil:
				d, ok := devByID[a.Open.DeviceID]
				if !ok {
					continue
				}
				row := &store.SensorAlert{
					Kind: a.Open.Kind, DeviceID: d.ID, ResourceType: d.ResourceType, ResourceID: d.ResourceID, MapID: d.MapID,
					ConditionKey: a.Open.ConditionKey, Refs: refsJSON(a.Open.Refs), OpenedAt: now, CreatedAt: now, UpdatedAt: now,
				}
				ok, err := store.InsertSensorAlertIfNew(ctx, tx, scope, row)
				if err != nil {
					return err
				}
				if ok {
					if err := s.alertEvent(ctx, tx, scope, audit.System, "sensor_alert.open", row); err != nil {
						return err
					}
					res.Opened++
					s.metrics.Openings.Add(1)
				}
			case a.Presence != nil:
				done, err := s.recordSensorPresence(ctx, tx, scope, a.Presence)
				if err != nil {
					return err
				}
				if done {
					res.Presences++
				}
			}
		}
		return nil
	})
	return res, err
}

// recordSensorPresence writes the automatic presence of R3/R3b inside a savepoint: a refusal (another writer moved
// the vessel, a transit, a validation) rolls back only the presence, never the alerts of the run.
func (s *Service) recordSensorPresence(ctx context.Context, tx bun.Tx, scope store.MarinaScope, rp *reconcile.RecordPresence) (bool, error) {
	if s.d.Presence == nil {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT sensor_presence"); err != nil {
		return false, err
	}
	_, err := s.d.Presence.RecordSensorTx(ctx, tx, scope, rp.VesselID, rp.AllocationID, rp.Event, rp.At)
	if err != nil {
		if _, rerr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT sensor_presence"); rerr != nil {
			return false, rerr
		}
		if _, rerr := tx.ExecContext(ctx, "RELEASE SAVEPOINT sensor_presence"); rerr != nil {
			return false, rerr
		}
		if !errors.Is(err, presence.ErrInvalidTransition) && !errors.Is(err, presence.ErrSensorNotAllowed) {
			s.d.Log.Warn("sensor presence refused", "marina_id", scope.ID(), "vessel_id", rp.VesselID, "event", rp.Event, "error", err)
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT sensor_presence"); err != nil {
		return false, err
	}
	return true, nil
}

// refsJSON stores the refs of an alert (ids and enums only).
func refsJSON(r reconcile.Refs) store.JSON {
	out := store.SensorAlertRefs{
		AllocationID: r.AllocationID, ContractID: r.ContractID, VesselID: r.VesselID, ReleaseID: r.ReleaseID,
		PresenceID: r.PresenceID, Resold: r.Resold, PhysicalSince: r.PhysicalSince,
	}
	if r.ProposedFrom != nil {
		out.ProposedFrom = r.ProposedFrom.String()
	}
	if r.ProposedTo != nil {
		out.ProposedTo = r.ProposedTo.String()
	}
	return store.MustJSON(out)
}

func parseRefs(j store.JSON) store.SensorAlertRefs {
	var out store.SensorAlertRefs
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

// alertEvent writes the audit row and the sensor.anomaly event of an alert transition (ids and enums only).
func (s *Service) alertEvent(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, action string, a *store.SensorAlert) error {
	if s.d.Audit != nil {
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: actor, Action: action, EntityType: "sensor_alert",
			EntityID: a.ID, Diff: map[string]any{"kind": a.Kind, "status": a.Status, "device_id": a.DeviceID, "condition_key": a.ConditionKey},
		}); err != nil {
			return fmt.Errorf("audit %s: %w", action, err)
		}
	}
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.SensorAnomaly, event.SensorAnomalyData{
		AlertID: a.ID, Kind: a.Kind, Status: a.Status, DeviceID: a.DeviceID, ResourceType: a.ResourceType,
		ResourceIDs: []string{a.ResourceID}, MapID: a.MapID,
	})
	return err
}
