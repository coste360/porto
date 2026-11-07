package occupancy

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// Owner occupancy of berth rights (M4, m4-plan §4.1, §15 D15–D16). The rights service is the only caller: it
// authorises, links the allocations to the right (berth_right_allocations) and writes audit and outbox in its
// transaction. These primitives only write allocations and rows, so INV-01 stays a database guarantee.

// OwnerAllocation is the input of AllocateOwnerTx: the nights [First, Last] of a berth, carried by the primary
// authorised vessel of the right (nil: no vessel, the existing allocations_block_check allows it).
type OwnerAllocation struct {
	BerthID     string
	VesselID    *string
	First, Last calendar.Date
	FitOverride *FitOverride
	UserID      string
}

// AllocateOwnerTx writes an owner allocation (kind owner, no expiry, berth only) and its rows. The fit is
// checked only when a vessel is set (INV-07). A night already taken is a *ConflictError (409 resource_taken):
// nothing is written. Expired holds on the nights are reclaimed first (INV-05). Owner allocations never resell
// releases.
func (s *Service) AllocateOwnerTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a OwnerAllocation) (*store.Allocation, error) {
	if a.Last.Before(a.First) {
		return nil, invalid("last_night", "before_first_night")
	}
	r, err := loadResource(ctx, tx, scope, engine.ResourceBerth, a.BerthID)
	if err != nil {
		return nil, err
	}
	if !r.Active {
		return nil, &PlacementError{Reason: PlacementNotAllowed}
	}
	spec := engine.AllocSpec{ResourceType: engine.ResourceBerth, ResourceID: a.BerthID, First: a.First, Last: a.Last}
	var lv layout.Vessel
	var override *FitOverride
	if a.VesselID != nil && *a.VesselID != "" {
		v, err := loadVessel(ctx, tx, scope, *a.VesselID)
		if err != nil {
			return nil, err
		}
		lv = v.Layout
		_, rules, err := settings(ctx, tx, scope, now)
		if err != nil {
			return nil, err
		}
		if _, override, err = checkFit(v, r, nil, a.Last, rules, a.FitOverride); err != nil {
			return nil, err
		}
	}
	rows, err := engine.Plan(spec, r.Area, lv)
	if err != nil {
		if pe := placementError(err); pe != nil {
			return nil, pe
		}
		return nil, err
	}
	row := &store.Allocation{
		ID: store.NewID(), ResourceType: string(engine.ResourceBerth), ResourceID: a.BerthID, MapID: r.MapID,
		FirstNight: a.First, LastNight: a.Last, Kind: string(KindOwner), Status: store.AllocActive,
		CreatedByUserID: a.UserID, Version: 1, CreatedAt: store.UTC(now), UpdatedAt: store.UTC(now),
	}
	if a.VesselID != nil {
		row.VesselID = *a.VesselID
	}
	if override != nil {
		row.FitOverride = store.MustJSON(override)
	}
	if err := store.InsertAllocation(ctx, tx, scope, row); err != nil {
		return nil, err
	}
	if err := insertPlan(ctx, tx, scope, now, row, rows); err != nil {
		return nil, err
	}
	return row, nil
}

// OwnerSegment is an active owner allocation of a right after a split.
type OwnerSegment struct {
	AllocationID string
	First, Last  calendar.Date
}

// RemoveOwnerNightsTx removes the nights [from, to] from an active owner allocation (a mandate, §15 D16): the
// allocation keeps the nights before from (or is cancelled when from is its first night) and, when it continues
// after to, a new owner allocation with the same vessel takes the nights after to. It returns the new tail
// allocation, or nil when there is none. The nights in between have no rows: the berth is sellable.
func (s *Service) RemoveOwnerNightsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocID string, from, to calendar.Date) (*store.Allocation, error) {
	if to.Before(from) {
		return nil, invalid("to", "before_from")
	}
	a, err := lockActive(ctx, tx, scope, allocID)
	if err != nil {
		return nil, err
	}
	if a.Kind != store.AllocKindOwner {
		return nil, invalid("allocation_id", "not_owner")
	}
	if to.Before(a.FirstNight) || from.After(a.LastNight) {
		return nil, nil
	}
	last := a.LastNight
	vessel := a.VesselID
	override := a.FitOverride
	if !from.After(a.FirstNight) {
		if err := s.cancelLocked(ctx, tx, scope, now, a, StatusCancelled); err != nil {
			return nil, err
		}
	} else if err := truncateLocked(ctx, tx, scope, now, a, from); err != nil {
		return nil, err
	}
	if !to.Before(last) {
		return nil, nil
	}
	tail := OwnerAllocation{BerthID: a.ResourceID, First: to.AddDays(1), Last: last, UserID: a.CreatedByUserID}
	if vessel != "" {
		tail.VesselID = &vessel
	}
	if len(override) > 0 {
		o := unmarshalJSON[FitOverride](override)
		tail.FitOverride = &o
	}
	return s.AllocateOwnerTx(ctx, tx, scope, now, tail)
}

// IsOwnerAllocation reports whether the allocation is an active owner allocation (used by the rights service to
// skip rows already ended by another path).
func IsOwnerAllocation(a *store.Allocation) bool {
	return a != nil && a.Kind == store.AllocKindOwner && a.Status == store.AllocActive
}
