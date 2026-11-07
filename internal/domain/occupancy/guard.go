package occupancy

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/store"
)

// Guard returns the harbour guard (harbor.OccupancyGuard, m3-plan §3.7) backed by this service.
func (s *Service) Guard() harbor.OccupancyGuard { return guard{s: s} }

type guard struct{ s *Service }

// CheckChanges refuses deleting a berth, dynamic area or land resource, or reshaping a dynamic area (kind, cell,
// margin, mooring mode, axis, outline, blocked cells, grid rotation), when an active allocation of that element
// has nights from today on (409 in_use with the element ids). Berth and land dimension changes are allowed:
// misfits show as the vessel_exceeds_limits anomaly. Deleting an element with only past rows is refused by the
// RESTRICT foreign keys of the occupancy tables (history is kept).
func (g guard) CheckChanges(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mapID string, changes []harbor.ElementChange) error {
	byType := map[string][]string{}
	for _, c := range changes {
		if c.Removed || (c.GeometryChanged && c.Kind == store.ResourceArea) {
			byType[c.Kind] = append(byType[c.Kind], c.ID)
		}
	}
	if len(byType) == 0 {
		return nil
	}
	loc, err := marinaLocation(ctx, tx, scope)
	if err != nil {
		return err
	}
	today := Today(g.s.d.Clock, loc)
	var refs []string
	for _, t := range []string{store.ResourceBerth, store.ResourceArea, store.ResourceLand} {
		ids, err := store.HasCurrentAllocations(ctx, tx, scope, t, byType[t], today)
		if err != nil {
			return err
		}
		refs = append(refs, ids...)
	}
	if len(refs) > 0 {
		return &harbor.ElementInUseError{References: refs}
	}
	return nil
}
