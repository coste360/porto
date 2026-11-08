package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// Maintenance occupancy (m9-plan §8.3, D24, D27): a scheduled task whose plan requires the element free, or that
// takes it out of service, books maintenance nights on berths and land resources (a structure on the berths
// chosen) through occupancy.AllocateTx in the same transaction. Vessels on those nights are never displaced: the
// schedule fails with *ConflictsError and nothing is written. Completing never releases nights; Release and Cancel
// (online, office) do.

// maxScheduleNights bounds a schedule.
const maxScheduleNights = 366

func actorOf(p *auth.Principal) audit.Actor { return p.Actor() }

// AllocationIDsOf decodes the allocation_ids column of a task.
func AllocationIDsOf(t *store.MaintenanceTask) []string {
	var ids []string
	_ = json.Unmarshal(t.AllocationIDs, &ids)
	return ids
}

// occupyTargets returns the resources a schedule books: the berth or land resource of the task, the chosen berths
// of a structure; none for the other elements.
func occupyTargets(ctx context.Context, db bun.IDB, scope store.MarinaScope, t *store.MaintenanceTask, berthIDs []string) ([]engine.AllocSpec, error) {
	switch t.ElementType {
	case store.ElementBerth:
		return []engine.AllocSpec{{ResourceType: engine.ResourceType(store.ResourceBerth), ResourceID: t.ElementID}}, nil
	case store.ElementLandResource:
		return []engine.AllocSpec{{ResourceType: engine.ResourceType(store.ResourceLand), ResourceID: t.ElementID}}, nil
	case store.ElementStructure:
		if len(berthIDs) == 0 {
			return nil, nil
		}
		own, err := store.BerthsOfStructure(ctx, db, scope, t.ElementID)
		if err != nil {
			return nil, err
		}
		var out []engine.AllocSpec
		seen := map[string]bool{}
		for _, id := range berthIDs {
			if !slices.Contains(own, id) {
				return nil, ErrElementInvalid
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, engine.AllocSpec{ResourceType: engine.ResourceType(store.ResourceBerth), ResourceID: id})
		}
		return out, nil
	}
	if len(berthIDs) > 0 {
		return nil, ErrElementInvalid
	}
	return nil, nil
}

// conflictsOf lists the vessel allocations (anything but this task's maintenance) holding the nights to book.
func conflictsOf(ctx context.Context, tx bun.IDB, scope store.MarinaScope, specs []engine.AllocSpec, from, to calendar.Date, own []string) ([]Conflict, error) {
	var out []Conflict
	for _, sp := range specs {
		owners, err := store.OccupancyOwners(ctx, tx, scope, string(sp.ResourceType), sp.ResourceID, from, to, nil, own)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, o := range owners {
			if seen[o.AllocationID] {
				continue
			}
			seen[o.AllocationID] = true
			a, err := store.GetAllocation(ctx, tx, scope, o.AllocationID)
			if err != nil {
				return nil, err
			}
			c := Conflict{
				AllocationID: a.ID, VesselID: a.VesselID, ResourceType: a.ResourceType, ResourceID: a.ResourceID,
				From: maxDate(from, a.FirstNight), To: minDate(to, a.LastNight),
			}
			if a.ContractLineID != "" {
				if cid, err := contractOfLine(ctx, tx, scope, a.ContractLineID); err == nil {
					c.ContractID = cid
				}
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func contractOfLine(ctx context.Context, db bun.IDB, scope store.MarinaScope, lineID string) (string, error) {
	var id string
	err := db.NewSelect().TableExpr("contract_lines").ColumnExpr("CAST(contract_id AS TEXT)").
		Where("marina_id = ?", scope.ID()).Where("id = ?", lineID).Scan(ctx, &id)
	return id, err
}

func maxDate(a, b calendar.Date) calendar.Date {
	if a.After(b) {
		return a
	}
	return b
}

func minDate(a, b calendar.Date) calendar.Date {
	if a.Before(b) {
		return a
	}
	return b
}

// releaseFrom drops the nights ≥ from of the task's active maintenance allocations (allocations starting on or
// after from are cancelled).
func (s *Service) releaseFrom(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t *store.MaintenanceTask, from calendar.Date) error {
	for _, id := range AllocationIDsOf(t) {
		err := s.d.Occupancy.TruncateTx(ctx, tx, scope, id, from)
		if err != nil && !errors.Is(err, occupancy.ErrAllocationNotActive) {
			return err
		}
	}
	return nil
}

// Schedule schedules the nights [From, To]; with requires_free (plan) or out of service on a berth or land
// resource (a structure: the chosen berths) it books maintenance nights in the same transaction, replacing the
// task's previous ones. Vessels on the nights make it fail with *ConflictsError (up to three alternatives each)
// and nothing is written (maintenance.manage, If-Match).
func (s *Service) Schedule(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, in ScheduleInput) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	if in.To.Before(in.From) || in.From.DaysUntil(in.To)+1 > maxScheduleNights {
		v := &auth.ValidationError{}
		v.Add("to_night", "invalid")
		return nil, v
	}
	var conflicts []Conflict
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		conflicts = nil
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		next, err := Next(t.Status, ActionSchedule)
		if err != nil {
			return err
		}
		if in.OutOfService != nil {
			t.OutOfService = *in.OutOfService
		}
		requiresFree := false
		if t.PlanID != "" {
			plan, err := store.GetMaintenancePlan(ctx, tx, scope, t.PlanID)
			if err != nil {
				return err
			}
			requiresFree = plan.RequiresFree
		}
		var specs []engine.AllocSpec
		if requiresFree || t.OutOfService || len(in.OccupyBerthIDs) > 0 {
			if specs, err = occupyTargets(ctx, tx, scope, t, in.OccupyBerthIDs); err != nil {
				return err
			}
		}
		own := AllocationIDsOf(t)
		// The previous nights of the task are released first (a reschedule replaces them), from today or from the
		// new first night when earlier: the nights already held stay as history.
		releaseAt := s.today(ctx, tx, scope)
		if in.From.Before(releaseAt) {
			releaseAt = in.From
		}
		if err := s.releaseFrom(ctx, tx, scope, t, releaseAt); err != nil {
			return err
		}
		if len(specs) > 0 {
			if conflicts, err = conflictsOf(ctx, tx, scope, specs, in.From, in.To, own); err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return errConflicts
			}
		}
		ids := make([]string, 0, len(specs))
		now := s.now()
		for _, sp := range specs {
			sp.First, sp.Last = in.From, in.To
			a, err := s.d.Occupancy.AllocateTx(ctx, tx, scope, now, occupancy.NewAllocation{
				Kind: occupancy.KindMaintenance, Spec: sp, UserID: p.UserID,
			})
			if err != nil {
				var ce *occupancy.ConflictError
				if errors.As(err, &ce) {
					for _, aid := range ce.AllocationIDs {
						conflicts = append(conflicts, Conflict{
							AllocationID: aid, ResourceType: ce.ResourceType, ResourceID: ce.ResourceID, From: ce.From, To: ce.To,
						})
					}
					return errConflicts
				}
				return err
			}
			ids = append(ids, a.ID)
		}
		t.Status, t.ScheduledFrom, t.ScheduledTo = next, in.From, in.To
		t.AllocationIDs = store.MustJSON(append(own, ids...))
		return s.saveTask(ctx, tx, scope, p, t, int(version), "maintenance_task.schedule", map[string]any{
			"from": in.From.String(), "to": in.To.String(), "allocations": len(ids), "out_of_service": t.OutOfService,
		})
	})
	if errors.Is(err, errConflicts) {
		// Alternatives are searched after the rollback, outside the write transaction.
		for i := range conflicts {
			c := &conflicts[i]
			if c.VesselID == "" {
				if a, err := store.GetAllocation(ctx, s.d.DB.DB, scope, c.AllocationID); err == nil {
					c.VesselID = a.VesselID
				}
			}
			if c.VesselID != "" {
				alts := s.d.Occupancy.Alternatives(ctx, p, marinaID, c.VesselID, c.From, c.To)
				if len(alts) > 3 {
					alts = alts[:3]
				}
				c.Alternatives = alts
			}
		}
		return nil, &ConflictsError{Conflicts: conflicts}
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}

var errConflicts = errors.New("maintenance: conflicts")

// Release releases the maintenance nights after lastNight (nil = every night from today on) (maintenance.manage,
// online, If-Match). The task keeps its status; its scheduled end follows.
func (s *Service) Release(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, lastNight *calendar.Date) (*TaskView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(t.Version) != version {
			return ErrVersionMismatch
		}
		if t.Status == store.TaskCancelled || t.Status == store.TaskOpen {
			return ErrInvalidTransition
		}
		from := s.today(ctx, tx, scope)
		if lastNight != nil {
			from = lastNight.AddDays(1)
		}
		if err := s.releaseFrom(ctx, tx, scope, t, from); err != nil {
			return err
		}
		if !t.ScheduledTo.IsZero() && !t.ScheduledTo.Before(from) {
			t.ScheduledTo = from.AddDays(-1)
			if !t.ScheduledFrom.IsZero() && t.ScheduledTo.Before(t.ScheduledFrom) {
				t.ScheduledTo = t.ScheduledFrom
			}
		}
		return s.saveTask(ctx, tx, scope, p, t, int(version), "maintenance_task.release", map[string]any{"from": from.String()})
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, id)
}
